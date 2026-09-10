package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 실측 사고: control-plane 의 라우트 그룹 변수명이 internal 이라 URL 에도 internal 을
// 넣어 /api/v1/internal/workflows/... 로 보냈고, 실제 prefix 는 /api/v1/workflows 여서
// 404 가 됐다. 보고 실패는 실행을 막지 않으므로(의도된 설계) 조용히 유실됐고,
// control-plane 은 이 실행을 "아무도 접수하지 않음" 으로 오판할 수 있었다.
func TestReportExecutionClaim_UsesResultEndpointPrefix(t *testing.T) {
	var gotPath, gotMethod, gotCT string
	body := map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Agent{
		ID:              "agent-1",
		controlPlaneURL: srv.URL,
		ctx:             context.Background(),
		httpClient:      srv.Client(),
	}
	a.reportExecutionClaim("wf-1", "ex-1", "conduix-rt-ex-1")

	want := "/api/v1/workflows/wf-1/executions/ex-1/claim"
	if gotPath != want {
		t.Errorf("path = %q, want %q — 결과 보고와 같은 prefix 여야 한다", gotPath, want)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if body["agent_id"] != "agent-1" {
		t.Errorf("agent_id = %q — 누가 접수했는지 없으면 판정에 쓸 수 없다", body["agent_id"])
	}
	if body["delegated_to"] != "conduix-rt-ex-1" {
		t.Errorf("delegated_to = %q — 어디로 위임됐는지 추적 불가", body["delegated_to"])
	}
}

// 보고 실패가 실행을 막아서는 안 된다 — 실행은 이미 시작됐고, 결과 콜백이 최종 상태를 정정한다.
func TestReportExecutionClaim_SurvivesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &Agent{
		ID:              "agent-1",
		controlPlaneURL: srv.URL,
		ctx:             context.Background(),
		httpClient:      srv.Client(),
	}
	// panic 하지 않고 반환해야 한다
	a.reportExecutionClaim("wf-1", "ex-1", "dep-1")
}

// control-plane URL 이 없는 구성(로컬 단독 실행)에서 호출해도 안전해야 한다.
func TestReportExecutionClaim_NoopWithoutControlPlane(t *testing.T) {
	a := &Agent{ID: "agent-1", ctx: context.Background(), httpClient: http.DefaultClient}
	a.reportExecutionClaim("wf-1", "ex-1", "dep-1")
}

// executionID 가 비면 보고할 대상이 없다.
func TestReportExecutionClaim_NoopWithoutExecutionID(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Agent{
		ID:              "agent-1",
		controlPlaneURL: srv.URL,
		ctx:             context.Background(),
		httpClient:      srv.Client(),
	}
	a.reportExecutionClaim("wf-1", "", "dep-1")
	if called {
		t.Error("executionID 가 비었는데 요청을 보냈다")
	}
}

// 파드가 곧 사라질 수 있으므로 보고가 오래 매달려 있으면 안 된다.
func TestClaimReportTimeout_IsShort(t *testing.T) {
	if claimReportTimeout > 10*time.Second {
		t.Errorf("claimReportTimeout = %s — 실행 흐름을 오래 붙잡는다", claimReportTimeout)
	}
}
