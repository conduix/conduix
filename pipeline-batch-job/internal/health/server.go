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
