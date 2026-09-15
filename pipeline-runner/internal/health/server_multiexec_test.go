package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// streaming pod 는 상주하며 여러 실행을 담는다. 그래서 제어 명령이 "어느 실행"인지
// 실어야 한다 — 예전에는 이 인자가 없어 stop 하나가 프로세스 전체를 죽였고,
// 같은 파드의 다른 realtime 실행까지 함께 끊겼다.
func TestCommandHandler_PassesExecutionID(t *testing.T) {
	var gotExec, gotCmd string
	s := NewServer(0, "streaming")
	s.SetCommandHandler(func(executionID, cmd string) error {
		gotExec, gotCmd = executionID, cmd
		return nil
	})

	body := `{"command":"stop","execution_id":"ex-42"}`
	w := httptest.NewRecorder()
	s.commandHandler(w, httptest.NewRequest(http.MethodPost, "/commands", strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	if gotExec != "ex-42" {
		t.Errorf("execution_id = %q, want ex-42 — 없으면 어느 실행을 멈출지 알 수 없다", gotExec)
	}
	if gotCmd != "stop" {
		t.Errorf("command = %q, want stop", gotCmd)
	}
}

// execution_id 없는 구 요청도 받아야 한다(단일 실행 파드 호환).
func TestCommandHandler_AllowsMissingExecutionID(t *testing.T) {
	var gotExec string
	s := NewServer(0, "streaming")
	s.SetCommandHandler(func(executionID, _ string) error { gotExec = executionID; return nil })

	w := httptest.NewRecorder()
	s.commandHandler(w, httptest.NewRequest(http.MethodPost, "/commands",
		strings.NewReader(`{"command":"pause"}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	if gotExec != "" {
		t.Errorf("execution_id = %q, want empty — 핸들러가 단일 실행으로 해석해야 한다", gotExec)
	}
}

// 모니터링도 실행별로 지정할 수 있어야 한다. 아니면 파드에 여러 실행이 있을 때
// 엉뚱한 실행의 수치를 보여준다.
func TestMonitoringHandler_PassesExecutionIDFromQuery(t *testing.T) {
	var got string
	s := NewServer(0, "streaming")
	s.SetMonitoringHandler(func(executionID string) any {
		got = executionID
		return map[string]any{"execution_id": executionID}
	})

	w := httptest.NewRecorder()
	s.monitoringHandler(w, httptest.NewRequest(http.MethodGet, "/monitoring?execution_id=ex-7", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	if got != "ex-7" {
		t.Errorf("execution_id = %q, want ex-7", got)
	}
}

// 상주 파드는 실행을 REST 로 배정받는다. env 는 프로세스당 하나뿐이라
// 두 번째 실행을 넣을 수단이 없었다.
func TestExecutionsHandler_AssignsExecution(t *testing.T) {
	var gotBody []byte
	s := NewServer(0, "streaming")
	s.SetAssignHandler(func(b []byte) error { gotBody = b; return nil })

	payload := `{"execution_id":"ex-1","workflow_id":"wf-1"}`
	w := httptest.NewRecorder()
	s.executionsHandler(w, httptest.NewRequest(http.MethodPost, "/executions", strings.NewReader(payload)))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("핸들러가 받은 본문이 JSON 이 아니다: %v", err)
	}
	if decoded["execution_id"] != "ex-1" {
		t.Errorf("execution_id = %v, want ex-1", decoded["execution_id"])
	}
}

// 명령은 at-least-once 로 재전송될 수 있다. 이미 도는 실행의 재배정을 오류로 만들면
// agent 가 불필요한 재시도·실패 보고를 한다.
func TestExecutionsHandler_DuplicateIsNotError(t *testing.T) {
	s := NewServer(0, "streaming")
	s.SetAssignHandler(func([]byte) error { return ErrDuplicateForTest })

	w := httptest.NewRecorder()
	s.executionsHandler(w, httptest.NewRequest(http.MethodPost, "/executions",
		strings.NewReader(`{"execution_id":"ex-1"}`)))

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 — 중복 배정은 정상 처리여야 한다", w.Code)
	}
	if !strings.Contains(w.Body.String(), "already_running") {
		t.Errorf("응답에 already_running 표시가 없다: %s", w.Body.String())
	}
}

// 배정 핸들러가 없으면(batch 모드) 503 을 준다.
func TestExecutionsHandler_UnavailableWithoutHandler(t *testing.T) {
	s := NewServer(0, "batch")
	w := httptest.NewRecorder()
	s.executionsHandler(w, httptest.NewRequest(http.MethodPost, "/executions", strings.NewReader(`{}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", w.Code)
	}
}

// ErrDuplicateForTest 는 registry 의 중복 판정 문구를 흉내낸다.
// server 는 executor 에 의존하지 않으므로 문자열로 판정한다.
var ErrDuplicateForTest = &dupErr{}

type dupErr struct{}

func (e *dupErr) Error() string { return "execution already running in this pod" }
