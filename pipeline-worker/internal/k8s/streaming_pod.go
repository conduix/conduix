package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 상주 realtime 파드와 통신하는 경로.
//
// 예전에는 파드가 실행마다 떠서 execution-id 라벨로 찾았다. 이제 파드는 하나이고 여러
// 실행을 담으므로, 파드는 component 라벨로 찾고 실행은 요청 본문·쿼리로 지정한다.

// streamingPodRequestTimeout 은 파드 REST 호출 상한이다.
// 실행 배정은 파이프라인 시작을 포함하지 않는다(파드가 고루틴으로 띄우고 바로 응답).
const streamingPodRequestTimeout = 10 * time.Second

// StreamingPodURL 은 상주 realtime 파드의 REST URL 을 만든다.
//
// execution-id 라벨이 아니라 component 라벨로 찾는다 — 상주 파드는 특정 실행에
// 속하지 않는다. running 이고 IP 가 배정된 파드만 대상이다.
func (m *JobManager) StreamingPodURL(ctx context.Context, namespace, path string) (string, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}
	selector := fmt.Sprintf("app.kubernetes.io/component=streaming-runner,%s=%s",
		labelManagedByKey, managedByValue)
	pods, err := m.client.Clientset().CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list streaming pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			return fmt.Sprintf("http://%s:%d%s", pod.Status.PodIP, streamingHealthPort, path), nil
		}
	}
	return "", fmt.Errorf("no running streaming pod with IP in namespace %s", namespace)
}

// AssignExecution 은 상주 파드에 realtime 실행을 배정한다.
//
// 예전에는 실행 정보를 env 로 넣어 파드를 새로 띄웠다. env 는 프로세스당 하나뿐이라
// 두 번째 실행을 담을 수 없어, 실행마다 파드가 생기는 구조가 됐다.
func (m *JobManager) AssignExecution(ctx context.Context, namespace string, cmd any) error {
	url, err := m.StreamingPodURL(ctx, namespace, "/executions")
	if err != nil {
		return err
	}

	body, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal execution command: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, streamingPodRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create assign request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("assign execution to streaming pod: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("streaming pod rejected execution (%d): %s", resp.StatusCode, string(msg))
	}
	return nil
}

// StreamingPodExecutions 는 상주 파드가 현재 들고 있는 실행 목록을 조회한다.
//
// heartbeat 와 회수 판정의 근거다. 예전에는 "Deployment 목록 = 실행 목록" 이었지만
// 파드가 하나가 되면서 그 등식이 깨졌다 — 파드에 직접 물어봐야 한다.
func (m *JobManager) StreamingPodExecutions(ctx context.Context, namespace string) ([]string, error) {
	url, err := m.StreamingPodURL(ctx, namespace, "/monitoring")
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, streamingPodRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query streaming pod executions: %w", err)
	}
	defer resp.Body.Close()

	// 실행이 하나도 없으면 파드는 404 를 준다 — 오류가 아니라 "빈 파드" 다.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("streaming pod returned %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	// 실행이 여러 개면 배열, 하나면 단일 객체로 온다(단일 실행 파드 호환).
	var list []struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal(raw, &list); err == nil {
		out := make([]string, 0, len(list))
		for _, e := range list {
			if e.ExecutionID != "" {
				out = append(out, e.ExecutionID)
			}
		}
		return out, nil
	}

	var single struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("decode streaming pod monitoring: %w", err)
	}
	if single.ExecutionID == "" {
		return nil, nil
	}
	return []string{single.ExecutionID}, nil
}

// SendStreamingCommand 는 상주 파드의 특정 실행에 제어 명령을 보낸다.
//
// executionID 를 본문에 실어야 한다 — 파드가 여러 실행을 담으므로 이것이 없으면
// 어느 실행을 멈출지 알 수 없다(예전에는 stop 이 프로세스 전체를 죽였다).
func (m *JobManager) SendStreamingCommand(ctx context.Context, namespace, executionID, command string) error {
	url, err := m.StreamingPodURL(ctx, namespace, "/commands")
	if err != nil {
		return err
	}

	body, _ := json.Marshal(map[string]string{
		"command":      command,
		"execution_id": executionID,
	})

	reqCtx, cancel := context.WithTimeout(ctx, streamingPodRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send %s to streaming pod: %w", command, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("streaming pod rejected %s (%d): %s", command, resp.StatusCode, string(msg))
	}
	return nil
}
