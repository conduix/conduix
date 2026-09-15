// Package health 헬스체크 HTTP 서버
// K8s liveness/readiness probe 대응
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
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
	//
	// executionID 를 함께 받는다: streaming pod 는 상주하며 여러 실행을 수용하므로
	// "어느 실행을 멈출지" 를 알아야 한다. 예전에는 이 인자가 없어 stop 하나가
	// 프로세스 전체를 죽였다(실행 1개 = 파드 1개 전제).
	// 비어 있으면 단일 실행 파드로 보고 그 하나에 적용한다(구 agent 호환).
	commandFn func(executionID, cmd string) error
	// assignFn 은 POST /executions 로 받은 실행 배정을 처리한다.
	// 상주 파드가 새 realtime 실행을 고루틴으로 받아들이는 경로다.
	assignFn func(body []byte) error
	// monitoringFn 은 GET /monitoring 응답 본문을 만든다. 위임 실행(batch Job/streaming pod)은
	// agent 프로세스 밖에서 도므로 agent 의 GroupExecutor 가 nil 이고, agent 는 이 엔드포인트로
	// pod 에 직접 물어봐야 실시간 진행률을 알 수 있다. executor 의존을 피해 콜백으로 주입한다.
	// executionID 인자: 상주 파드가 여러 실행을 담으므로 어느 것을 볼지 지정한다.
	// 빈 문자열이면 파드의 대표 실행(단일 실행 파드 호환) 또는 전체를 반환한다.
	monitoringFn func(executionID string) any
}

// SetMonitoringHandler 모니터링 정보 제공자 주입. batch/streaming 공통으로 GroupExecutor 를 연결한다.
func (s *Server) SetMonitoringHandler(fn func(executionID string) any) {
	s.mu.Lock()
	s.monitoringFn = fn
	s.mu.Unlock()
}

// SetCommandHandler 제어명령 핸들러 주입. streaming 모드에서 GroupExecutor 제어를 연결한다.
func (s *Server) SetCommandHandler(fn func(executionID, cmd string) error) {
	s.mu.Lock()
	s.commandFn = fn
	s.mu.Unlock()
}

// SetAssignHandler 실행 배정 핸들러 주입. 상주 streaming pod 가 새 실행을 받는다.
func (s *Server) SetAssignHandler(fn func(body []byte) error) {
	s.mu.Lock()
	s.assignFn = fn
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
	mux.HandleFunc("/executions", s.executionsHandler)

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
		// ExecutionID 가 있으면 그 실행만 제어한다. 상주 파드는 여러 실행을 담으므로
		// 이것이 없으면 어느 것을 멈출지 알 수 없다. 비면 단일 실행 파드로 간주한다.
		ExecutionID string `json:"execution_id,omitempty"`
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
	if err := fn(body.ExecutionID, body.Command); err != nil {
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
	// ?execution_id= 로 파드 안의 특정 실행을 지정한다. 없으면 대표/전체를 돌려준다.
	info := fn(r.URL.Query().Get("execution_id"))
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

// executionsHandler POST /executions — 상주 streaming pod 에 새 realtime 실행을 배정한다.
//
// 예전에는 실행마다 Deployment 를 만들어 env 로 config 를 넣었다. 상주 파드는 env 를
// 다시 읽을 수 없으므로 실행 명령을 REST 로 받는다.
func (s *Server) executionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	fn := s.assignFn
	s.mu.RUnlock()
	if fn == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"assign handler not set (not streaming mode?)"}`)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxAssignBodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"read body: %s"}`, err.Error())
		return
	}
	if err := fn(body); err != nil {
		// 이미 도는 실행의 재전송은 오류가 아니다 — 명령이 at-least-once 로 올 수 있다.
		if strings.Contains(err.Error(), "already running") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true,"already_running":true}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"success":true}`)
}

// 실행 명령 본문 상한. 파이프라인 설정이 커도 이 정도면 충분하고,
// 무제한이면 잘못된 요청 하나가 파드 메모리를 먹는다.
const maxAssignBodyBytes = 8 << 20 // 8MB

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
