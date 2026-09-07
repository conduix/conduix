// Package health 헬스체크 HTTP 서버
// K8s liveness/readiness probe 대응
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Status Runner 상태
type Status struct {
	Mode      string    `json:"mode"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Uptime    string    `json:"uptime"`
}

// Server 헬스체크 서버
type Server struct {
	port      int
	server    *http.Server
	status    string
	mode      string
	startedAt time.Time
	mu        sync.RWMutex
	// commandFn 은 POST /commands 로 받은 제어명령(stop/pause/resume)을 처리한다.
	// executor 의존을 피하려 콜백으로 주입한다(runStreaming 이 GroupExecutor 를 연결).
	commandFn func(cmd string) error
	// monitoringFn 은 GET /monitoring 응답 본문을 만든다. 위임 실행(batch Job/streaming pod)은
	// agent 프로세스 밖에서 도므로 agent 의 GroupExecutor 가 nil 이고, agent 는 이 엔드포인트로
	// pod 에 직접 물어봐야 실시간 진행률을 알 수 있다. executor 의존을 피해 콜백으로 주입한다.
	monitoringFn func() any
}

// SetMonitoringHandler 모니터링 정보 제공자 주입. batch/streaming 공통으로 GroupExecutor 를 연결한다.
func (s *Server) SetMonitoringHandler(fn func() any) {
	s.mu.Lock()
	s.monitoringFn = fn
	s.mu.Unlock()
}

// SetCommandHandler 제어명령 핸들러 주입. streaming 모드에서 GroupExecutor 제어를 연결한다.
func (s *Server) SetCommandHandler(fn func(cmd string) error) {
	s.mu.Lock()
	s.commandFn = fn
	s.mu.Unlock()
}

// NewServer 헬스체크 서버 생성
func NewServer(port int, mode string) *Server {
	return &Server{
		port:      port,
		mode:      mode,
		status:    "starting",
		startedAt: time.Now(),
	}
}

// Start 서버 시작 (비블로킹)
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)
	mux.HandleFunc("/commands", s.commandHandler)
	mux.HandleFunc("/monitoring", s.monitoringHandler)

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
		}
	}()

	return nil
}

// Stop 서버 종료
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

// SetStatus 상태 업데이트
func (s *Server) SetStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// healthHandler liveness probe
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	resp := Status{
		Mode:      s.mode,
		Status:    status,
		StartedAt: s.startedAt,
		Uptime:    time.Since(s.startedAt).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if status == "error" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(resp)
}

// commandHandler POST /commands {"command":"stop|pause|resume"} — streaming pod 제어.
// CP/agent 가 REST 로 호출해 이 pod 의 GroupExecutor 를 제어한다.
func (s *Server) commandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"decode: %s"}`, err.Error())
		return
	}
	s.mu.RLock()
	fn := s.commandFn
	s.mu.RUnlock()
	if fn == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"command handler not set (not streaming mode?)"}`)
		return
	}
	if err := fn(body.Command); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"success":true}`)
}

// monitoringHandler GET /monitoring — 이 pod 가 실행 중인 파이프라인의 실시간 진행 정보.
// agent 가 label(conduix.io/execution-id)로 이 pod 를 찾아 1회 pull 한다(주기 polling 아님).
func (s *Server) monitoringHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	fn := s.monitoringFn
	s.mu.RUnlock()
	if fn == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"monitoring provider not set"}`)
		return
	}
	info := fn()
	if info == nil {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"no monitoring info"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		slog.Error("monitoring encode failed", "error", err)
	}
}

// readyHandler readiness probe
func (s *Server) readyHandler(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	if status != "running" {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"ready":false,"status":"%s"}`, status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ready":true}`)
}
