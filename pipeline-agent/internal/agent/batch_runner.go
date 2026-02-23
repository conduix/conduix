package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/executor"
	"github.com/conduix/conduix/pipeline-core/pkg/link"
	"github.com/conduix/conduix/shared/types"
)

// BatchRunner Kubernetes Job용 1회 실행 러너
type BatchRunner struct {
	executionID     string
	workflowID      string
	pipelinesConfig string
	callbackURL     string
	controlPlaneURL string
	httpClient      *http.Client
}

// NewBatchRunner 환경변수에서 설정을 읽어 BatchRunner 생성
func NewBatchRunner() (*BatchRunner, error) {
	executionID := os.Getenv("EXECUTION_ID")
	if executionID == "" {
		return nil, fmt.Errorf("EXECUTION_ID environment variable is required")
	}

	workflowID := os.Getenv("WORKFLOW_ID")
	if workflowID == "" {
		return nil, fmt.Errorf("WORKFLOW_ID environment variable is required")
	}

	pipelinesConfig := os.Getenv("PIPELINES_CONFIG")
	if pipelinesConfig == "" {
		return nil, fmt.Errorf("PIPELINES_CONFIG environment variable is required")
	}

	callbackURL := os.Getenv("CALLBACK_URL")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if callbackURL == "" && controlPlaneURL == "" {
		return nil, fmt.Errorf("CALLBACK_URL or CONTROL_PLANE_URL environment variable is required")
	}

	// controlPlaneURL로 callbackURL 구성
	if callbackURL == "" && controlPlaneURL != "" {
		callbackURL = fmt.Sprintf("%s/api/v1/internal/job-result", controlPlaneURL)
	}

	return &BatchRunner{
		executionID:     executionID,
		workflowID:      workflowID,
		pipelinesConfig: pipelinesConfig,
		callbackURL:     callbackURL,
		controlPlaneURL: controlPlaneURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Run 배치 실행 시작
func (r *BatchRunner) Run() error {
	startTime := time.Now()
	podName := os.Getenv("HOSTNAME") // Kubernetes에서 Pod 이름

	fmt.Printf("[BatchRunner] Starting execution: workflow=%s, execution=%s\n",
		r.workflowID, r.executionID)

	// 파이프라인 설정 파싱
	var workflowConfig types.Workflow
	if err := json.Unmarshal([]byte(r.pipelinesConfig), &workflowConfig); err != nil {
		// 단순 PipelinesConfig JSON 배열일 수도 있음
		var pipelines []types.WorkflowPipeline
		if err2 := json.Unmarshal([]byte(r.pipelinesConfig), &pipelines); err2 != nil {
			return r.sendErrorResult(startTime, podName, fmt.Errorf("failed to parse pipelines config: %w", err))
		}
		workflowConfig = types.Workflow{
			ID:        r.workflowID,
			Name:      fmt.Sprintf("batch-%s", r.executionID[:8]),
			Type:      types.WorkflowTypeBatch,
			Pipelines: pipelines,
		}
	}

	// Link Client 생성 (파이프라인 연결용)
	var linkClient *link.Client
	if r.controlPlaneURL != "" {
		linkClient = link.NewClient(r.controlPlaneURL)
	}

	// GroupExecutor로 파이프라인 실행
	var opts []executor.GroupExecutorOption
	if linkClient != nil {
		opts = append(opts, executor.WithLinkClient(linkClient))
	}

	groupExecutor := executor.NewGroupExecutor(&workflowConfig, opts...)

	// 타임아웃 설정 (환경변수에서 읽거나 기본 1시간)
	timeoutSeconds := getEnvInt("TIMEOUT_SECONDS", 3600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// 실행 시작
	_, err := groupExecutor.Start(ctx, "batch-job")
	if err != nil {
		return r.sendErrorResult(startTime, podName, fmt.Errorf("failed to start execution: %w", err))
	}

	// 완료 대기
	fmt.Println("[BatchRunner] Waiting for execution to complete...")

	result, err := r.waitForCompletion(ctx, groupExecutor, startTime, podName)
	if err != nil {
		return r.sendErrorResult(startTime, podName, err)
	}

	// 결과 전송
	return r.sendResult(result)
}

// waitForCompletion 실행 완료 대기
func (r *BatchRunner) waitForCompletion(
	ctx context.Context,
	exec *executor.GroupExecutor,
	startTime time.Time,
	podName string,
) (*types.JobExecutionResult, error) {

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = exec.Stop()
			return nil, fmt.Errorf("execution timed out")

		case <-ticker.C:
			currentExec := exec.Execution()
			if currentExec == nil {
				continue
			}

			// 완료 상태 확인
			switch currentExec.Status {
			case types.PipelineGroupStatusCompleted:
				completedAt := time.Now()
				return &types.JobExecutionResult{
					ExecutionID:     r.executionID,
					WorkflowID:      r.workflowID,
					JobName:         fmt.Sprintf("batch-%s", r.executionID[:8]),
					PodName:         podName,
					Status:          types.JobStatusCompleted,
					PipelineResults: currentExec.PipelineResults,
					TotalRecords:    currentExec.TotalRecords,
					FailedRecords:   currentExec.FailedRecords,
					StartedAt:       startTime,
					CompletedAt:     completedAt,
					DurationMs:      completedAt.Sub(startTime).Milliseconds(),
				}, nil

			case types.PipelineGroupStatusError, types.PipelineGroupStatusStopped:
				completedAt := time.Now()
				return &types.JobExecutionResult{
					ExecutionID:     r.executionID,
					WorkflowID:      r.workflowID,
					JobName:         fmt.Sprintf("batch-%s", r.executionID[:8]),
					PodName:         podName,
					Status:          types.JobStatusFailed,
					PipelineResults: currentExec.PipelineResults,
					TotalRecords:    currentExec.TotalRecords,
					FailedRecords:   currentExec.FailedRecords,
					ErrorMessage:    currentExec.ErrorMessage,
					StartedAt:       startTime,
					CompletedAt:     completedAt,
					DurationMs:      completedAt.Sub(startTime).Milliseconds(),
				}, nil
			}
		}
	}
}

// sendResult 결과를 Control Plane으로 전송
func (r *BatchRunner) sendResult(result *types.JobExecutionResult) error {
	fmt.Printf("[BatchRunner] Sending result: status=%s, records=%d\n",
		result.Status, result.TotalRecords)

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.callbackURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send result: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("[BatchRunner] Result sent successfully to %s\n", r.callbackURL)
	return nil
}

// sendErrorResult 에러 결과 전송
func (r *BatchRunner) sendErrorResult(startTime time.Time, podName string, execErr error) error {
	completedAt := time.Now()
	result := &types.JobExecutionResult{
		ExecutionID:  r.executionID,
		WorkflowID:   r.workflowID,
		JobName:      fmt.Sprintf("batch-%s", r.executionID[:8]),
		PodName:      podName,
		Status:       types.JobStatusFailed,
		ErrorMessage: execErr.Error(),
		StartedAt:    startTime,
		CompletedAt:  completedAt,
		DurationMs:   completedAt.Sub(startTime).Milliseconds(),
	}

	if err := r.sendResult(result); err != nil {
		fmt.Printf("[BatchRunner] Failed to send error result: %v\n", err)
		return fmt.Errorf("execution failed: %w, and failed to report: %v", execErr, err)
	}

	return execErr
}

// getEnvInt 환경변수를 int로 읽기 (기본값 지원)
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}

	var result int
	if _, err := fmt.Sscanf(val, "%d", &result); err != nil {
		return defaultVal
	}
	return result
}
