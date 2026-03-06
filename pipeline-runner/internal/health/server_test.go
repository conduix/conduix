package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthHandler(t *testing.T) {
	s := NewServer(0, "batch")
	s.SetStatus("running")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var status Status
	json.NewDecoder(w.Body).Decode(&status)
	if status.Mode != "batch" {
		t.Errorf("expected mode batch, got %s", status.Mode)
	}
	if status.Status != "running" {
		t.Errorf("expected status running, got %s", status.Status)
	}
}

func TestHealthHandlerError(t *testing.T) {
	s := NewServer(0, "streaming")
	s.SetStatus("error")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.healthHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestReadyHandler(t *testing.T) {
	s := NewServer(0, "batch")

	// starting 상태에서는 not ready
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	s.readyHandler(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for starting, got %d", w.Code)
	}

	// running 상태에서는 ready
	s.SetStatus("running")
	w = httptest.NewRecorder()
	s.readyHandler(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for running, got %d", w.Code)
	}
}

func TestStartAndStop(t *testing.T) {
	s := NewServer(0, "batch") // port 0 = random available port

	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestSetStatus(t *testing.T) {
	s := NewServer(0, "batch")

	if s.status != "starting" {
		t.Errorf("expected initial status starting, got %s", s.status)
	}

	s.SetStatus("running")
	s.mu.RLock()
	status := s.status
	s.mu.RUnlock()

	if status != "running" {
		t.Errorf("expected running, got %s", status)
	}
}
