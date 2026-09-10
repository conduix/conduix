package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-runner/internal/config"
	"github.com/conduix/conduix/shared/types"
)

// realtime 은 원래 무한 실행이지만 스스로 끝나는 경로가 있다(DDL 방어의 schema_changed,
// 소스 오류). 그것을 종료로 인식하지 못하면 파드가 좀비로 남는다(실측 9분 잔존).
//
// 판정은 "계속 사는 상태" 화이트리스트로 한다 — 새 종료 상태가 추가돼도 좀비가 되지 않게.
func TestStreamingEnded(t *testing.T) {
	cases := []struct {
		status types.WorkflowStatus
		want   bool
		why    string
	}{
		{types.PipelineGroupStatusRunning, false, "정상 실행 중"},
		{types.PipelineGroupStatusPaused, false, "resume 대기 — 종료로 보면 일시정지가 파드 종료가 된다"},
		{"", false, "아직 상태 미기록"},
		{types.PipelineGroupStatusError, true, "실패 — 보고하고 종료해야 한다"},
		{types.PipelineGroupStatusStopped, true, "정지"},
		{types.PipelineGroupStatusCompleted, true, "완료"},
		{types.PipelineGroupStatusIdle, true, "실행 중이 아님"},
		{"schema_changed", true, "미등록 종료 상태도 종료로 봐야 좀비가 안 된다"},
	}

	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := streamingEnded(tc.status); got != tc.want {
				t.Errorf("streamingEnded(%q) = %v, want %v — %s", tc.status, got, tc.want, tc.why)
			}
		})
	}
}

// 사유가 파이프라인 결과에만 담기는 경우가 실재한다(DDL 정지가 그렇다).
// 그때 빈 문자열을 보내면 UI 에 "실패했지만 이유 없음" 이 뜬다.
func TestStreamingErrorMessage(t *testing.T) {
	t.Run("그룹 메시지를 우선한다", func(t *testing.T) {
		exec := &types.PipelineGroupExecution{
			Status:       types.PipelineGroupStatusError,
			ErrorMessage: "group level failure",
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "p1", ErrorMessage: "pipeline level"},
			},
		}
		if got := streamingErrorMessage(exec); got != "group level failure" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("그룹 메시지가 없으면 파이프라인 사유를 쓴다 — DDL 정지 실측 경로", func(t *testing.T) {
		exec := &types.PipelineGroupExecution{
			Status: types.PipelineGroupStatusError,
			PipelineResults: []types.PipelineExecutionResult{
				{PipelineName: "restrooms-cdc", ErrorMessage: "schema change (DDL) detected"},
			},
		}
		got := streamingErrorMessage(exec)
		if got != "pipeline restrooms-cdc: schema change (DDL) detected" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("사유를 못 찾아도 status 는 전달한다", func(t *testing.T) {
		exec := &types.PipelineGroupExecution{Status: types.PipelineGroupStatusStopped}
		if got := streamingErrorMessage(exec); got == "" {
			t.Error("빈 문자열이면 UI 가 이유를 표시할 수 없다")
		}
	})

	t.Run("정상 완료는 사유가 없다", func(t *testing.T) {
		exec := &types.PipelineGroupExecution{Status: types.PipelineGroupStatusCompleted}
		if got := streamingErrorMessage(exec); got != "" {
			t.Errorf("완료에 에러 메시지가 붙었다: %q", got)
		}
	})
}

// streaming 은 batch 와 엔드포인트가 다르다 — batch 의 CallbackURL 은 주입되지 않으므로
// agent in-process 경로와 같은 result 엔드포인트를 써야 한다. 경로가 틀리면 404 로
// 보고가 조용히 실패하고 좀비가 유지된다.
func TestSendStreamingResult_PostsToExecutionResultEndpoint(t *testing.T) {
	var gotPath string
	var gotBody types.GroupExecutionResult

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		_ = json.NewDecoder(req.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	r := &Runner{
		cfg: &config.RunnerConfig{
			ControlPlaneURL: srv.URL,
			WorkflowID:      "wf-1",
			ExecutionID:     "ex-1",
			AgentID:         "agent-7",
		},
		httpClient: srv.Client(),
	}

	exec := &types.PipelineGroupExecution{
		Status:        types.PipelineGroupStatusError,
		TotalRecords:  3,
		FailedRecords: 0,
		PipelineResults: []types.PipelineExecutionResult{
			{PipelineName: "restrooms-cdc", ErrorMessage: "schema change (DDL) detected"},
		},
	}

	if err := r.sendStreamingResult(time.Now().Add(-time.Minute), exec); err != nil {
		t.Fatalf("sendStreamingResult: %v", err)
	}

	wantPath := "/api/v1/workflows/wf-1/executions/ex-1/result"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody.Status != types.PipelineGroupStatusError {
		t.Errorf("status = %q, want error", gotBody.Status)
	}
	if gotBody.ErrorMessage == "" {
		t.Error("error_message 가 비었다 — UI 가 사유를 표시할 수 없다")
	}
	// 어느 노드에서 죽었는지 남아야 분산 현황을 볼 수 있다.
	if gotBody.AgentID != "agent-7" {
		t.Errorf("agent_id = %q, want agent-7", gotBody.AgentID)
	}
	if gotBody.CompletedAt == nil {
		t.Error("completed_at 이 없으면 실행이 끝난 것으로 안 보인다")
	}
}

// control-plane URL 이 없으면 보고할 곳이 없다 — 조용히 성공하면 안 된다.
func TestSendStreamingResult_ErrorsWithoutControlPlaneURL(t *testing.T) {
	r := &Runner{cfg: &config.RunnerConfig{WorkflowID: "wf-1", ExecutionID: "ex-1"}, httpClient: http.DefaultClient}
	err := r.sendStreamingResult(time.Now(), &types.PipelineGroupExecution{Status: types.PipelineGroupStatusError})
	if err == nil {
		t.Fatal("URL 이 없는데 성공으로 반환되면 보고 누락을 못 알아챈다")
	}
}

// 4xx/5xx 를 성공으로 보면 보고 누락을 놓친다.
func TestSendStreamingResult_ReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	r := &Runner{
		cfg:        &config.RunnerConfig{ControlPlaneURL: srv.URL, WorkflowID: "wf-1", ExecutionID: "ex-1"},
		httpClient: srv.Client(),
	}
	if err := r.sendStreamingResult(time.Now(), &types.PipelineGroupExecution{Status: types.PipelineGroupStatusError}); err == nil {
		t.Fatal("500 응답이 성공으로 처리됐다")
	}
}
