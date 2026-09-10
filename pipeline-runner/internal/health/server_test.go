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

// 위임 실행 pod 는 이 엔드포인트로만 진행률을 노출한다. 핸들러 미주입/정보 없음/정상 3가지를
// 구분해야 agent 가 "아직 시작 안 함"과 "실패"를 혼동하지 않는다.
func TestMonitoringHandler(t *testing.T) {
	s := NewServer(0, "batch")

	// 주입 전: 503
	w := httptest.NewRecorder()
	s.monitoringHandler(w, httptest.NewRequest(http.MethodGet, "/monitoring", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("no provider: code = %d, want 503", w.Code)
	}

	// nil 반환: 404
	s.SetMonitoringHandler(func() any { return nil })
	w = httptest.NewRecorder()
	s.monitoringHandler(w, httptest.NewRequest(http.MethodGet, "/monitoring", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("nil info: code = %d, want 404", w.Code)
	}

	// 정상: 200 + JSON 본문
	s.SetMonitoringHandler(func() any {
		return map[string]any{"execution_id": "e1", "total_records": 42}
	})
	w = httptest.NewRecorder()
	s.monitoringHandler(w, httptest.NewRequest(http.MethodGet, "/monitoring", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["execution_id"] != "e1" {
		t.Errorf("execution_id = %v, want e1", body["execution_id"])
	}
}

func TestMonitoringHandlerMethodNotAllowed(t *testing.T) {
	s := NewServer(0, "batch")
	w := httptest.NewRecorder()
	s.monitoringHandler(w, httptest.NewRequest(http.MethodPost, "/monitoring", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want 405", w.Code)
	}
}

// 실행이 죽었는데 /ready 가 200 을 주면 파드가 Ready 로 남아 좀비가 된다.
//
// 이 응답은 streaming Deployment 의 readinessProbe 가 읽는다(job_manager.go 의
// ReadinessProbe → /ready). Ready 가 유지되면 agent 의 failIfStuckUnhealthy 가
// Healthy()==true 로 판정해 회수 대상에서 빠진다 — 실측 9분 좀비의 마지막 고리다.
func TestReadyHandler_NotReadyOnNonRunningStatus(t *testing.T) {
	for _, status := range []string{"starting", "error", "stopping", "completed"} {
		t.Run(status, func(t *testing.T) {
			s := NewServer(0, "streaming")
			s.SetStatus(status)

			rec := httptest.NewRecorder()
			s.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status=%q 에서 /ready 가 %d — 503 이어야 파드가 NotReady 가 되고 회수된다",
					status, rec.Code)
			}
		})
	}
}

// running 일 때만 Ready — 방어가 정상 실행을 죽이면 안 된다.
func TestReadyHandler_ReadyWhileRunning(t *testing.T) {
	s := NewServer(0, "streaming")
	s.SetStatus("running")

	rec := httptest.NewRecorder()
	s.readyHandler(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("running 인데 /ready 가 %d — 정상 실행이 NotReady 로 죽는다", rec.Code)
	}
}
