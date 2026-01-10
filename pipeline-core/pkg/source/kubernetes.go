package source

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// podCheckpoint Pod별 체크포인트 (마지막 처리 타임스탬프)
type podCheckpoint struct {
	lastTimestamp time.Time
	lineCount     int64
}

// KubernetesSource Kubernetes Pod 로그 소스
type KubernetesSource struct {
	namespace     string
	podSelector   string
	podNames      []string
	containerName string
	follow        bool
	sinceSeconds  int64
	tailLines     int64
	kubeconfig    string
	context       string
	logFormat     string // auto, json, text
	logPattern    *regexp.Regexp

	clientset   *kubernetes.Clientset
	checkpoints map[string]*podCheckpoint // key: "namespace/pod/container"
	mu          sync.RWMutex
}

// NewKubernetesSource Kubernetes 소스 생성
func NewKubernetesSource(cfg config.SourceV2) (*KubernetesSource, error) {
	var logPattern *regexp.Regexp
	var err error

	// 로그 패턴 컴파일
	if cfg.K8sLogPattern != "" {
		logPattern, err = regexp.Compile(cfg.K8sLogPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid log_pattern regex: %w", err)
		}
	}

	logFormat := cfg.K8sLogFormat
	if logFormat == "" {
		logFormat = "auto" // 기본값: 자동 감지
	}

	return &KubernetesSource{
		namespace:     cfg.K8sNamespace,
		podSelector:   cfg.K8sPodSelector,
		podNames:      cfg.K8sPodNames,
		containerName: cfg.K8sContainerName,
		follow:        cfg.K8sFollow,
		sinceSeconds:  cfg.K8sSinceSeconds,
		tailLines:     cfg.K8sTailLines,
		kubeconfig:    cfg.K8sKubeconfig,
		context:       cfg.K8sContext,
		logFormat:     logFormat,
		logPattern:    logPattern,
		checkpoints:   make(map[string]*podCheckpoint),
	}, nil
}

func (s *KubernetesSource) Name() string {
	return "kubernetes"
}

func (s *KubernetesSource) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg *rest.Config
	var err error

	if s.kubeconfig != "" {
		// 외부 클러스터: kubeconfig 파일 사용
		loadingRules := &clientcmd.ClientConfigLoadingRules{
			ExplicitPath: s.kubeconfig,
		}
		configOverrides := &clientcmd.ConfigOverrides{}
		if s.context != "" {
			configOverrides.CurrentContext = s.context
		}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		cfg, err = kubeConfig.ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	} else {
		// In-cluster 설정 (ServiceAccount 사용)
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	s.clientset = clientset
	return nil
}

func (s *KubernetesSource) Read(ctx context.Context) (<-chan Record, <-chan error) {
	records := make(chan Record, 100)
	errs := make(chan error, 10)

	go func() {
		defer close(records)
		defer close(errs)

		s.mu.RLock()
		clientset := s.clientset
		s.mu.RUnlock()

		if clientset == nil {
			errs <- fmt.Errorf("kubernetes client not initialized")
			return
		}

		// Pod 목록 조회
		pods, err := s.listPods(ctx, clientset)
		if err != nil {
			errs <- fmt.Errorf("failed to list pods: %w", err)
			return
		}

		if len(pods) == 0 {
			errs <- fmt.Errorf("no pods found matching selector: %s", s.podSelector)
			return
		}

		var wg sync.WaitGroup

		// 각 Pod의 로그 스트리밍 (재연결 로직 포함)
		for _, pod := range pods {
			wg.Add(1)
			go func(p corev1.Pod) {
				defer wg.Done()
				s.streamPodLogsWithRetry(ctx, clientset, p, records, errs)
			}(pod)
		}

		wg.Wait()
	}()

	return records, errs
}

func (s *KubernetesSource) listPods(ctx context.Context, clientset *kubernetes.Clientset) ([]corev1.Pod, error) {
	// 특정 Pod 이름이 지정된 경우
	if len(s.podNames) > 0 {
		var pods []corev1.Pod
		for _, name := range s.podNames {
			pod, err := clientset.CoreV1().Pods(s.namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get pod %s: %w", name, err)
			}
			pods = append(pods, *pod)
		}
		return pods, nil
	}

	// Label selector로 Pod 조회
	listOpts := metav1.ListOptions{}
	if s.podSelector != "" {
		listOpts.LabelSelector = s.podSelector
	}

	podList, err := clientset.CoreV1().Pods(s.namespace).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}

	// Running 상태의 Pod만 필터링
	var runningPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			runningPods = append(runningPods, pod)
		}
	}

	return runningPods, nil
}

// streamPodLogsWithRetry 재연결 및 재시도 로직이 포함된 로그 스트리밍
func (s *KubernetesSource) streamPodLogsWithRetry(ctx context.Context, clientset *kubernetes.Clientset, pod corev1.Pod, records chan<- Record, errs chan<- error) {
	containerName := s.containerName
	if containerName == "" && len(pod.Spec.Containers) > 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	checkpointKey := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, containerName)

	// 재시도 설정
	maxRetries := 10
	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second
	retryCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 스트리밍 실행
		err := s.streamPodLogs(ctx, clientset, pod, containerName, checkpointKey, records)

		if err == nil {
			// 정상 종료 (follow=false인 경우)
			return
		}

		// context 취소인 경우 종료
		if ctx.Err() != nil {
			return
		}

		// 재시도 가능한 에러인지 확인
		retryCount++
		if retryCount > maxRetries {
			select {
			case errs <- fmt.Errorf("max retries exceeded for pod %s: %w", pod.Name, err):
			default:
			}
			return
		}

		// Exponential backoff
		delay := baseDelay * time.Duration(1<<uint(retryCount-1))
		if delay > maxDelay {
			delay = maxDelay
		}

		select {
		case errs <- fmt.Errorf("reconnecting to pod %s (attempt %d/%d) after error: %v", pod.Name, retryCount, maxRetries, err):
		default:
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// streamPodLogs 실제 로그 스트리밍 (체크포인트 기반)
func (s *KubernetesSource) streamPodLogs(ctx context.Context, clientset *kubernetes.Clientset, pod corev1.Pod, containerName, checkpointKey string, records chan<- Record) error {
	// 체크포인트 확인
	s.mu.RLock()
	checkpoint := s.checkpoints[checkpointKey]
	s.mu.RUnlock()

	// PodLogOptions 구성
	opts := &corev1.PodLogOptions{
		Container:  containerName,
		Follow:     s.follow,
		Timestamps: true, // 타임스탬프 포함 (체크포인트용 필수)
	}

	if checkpoint != nil && !checkpoint.lastTimestamp.IsZero() {
		// 체크포인트가 있으면 해당 시점 이후부터 가져오기
		// 1나노초 추가하여 마지막 로그 중복 방지
		sinceTime := metav1.NewTime(checkpoint.lastTimestamp.Add(1 * time.Nanosecond))
		opts.SinceTime = &sinceTime
	} else {
		// 초기 연결: tail_lines 또는 since_seconds 사용
		if s.sinceSeconds > 0 {
			opts.SinceSeconds = &s.sinceSeconds
		}
		if s.tailLines > 0 {
			opts.TailLines = &s.tailLines
		}
	}

	// 로그 스트림 요청
	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to get log stream for pod %s: %w", pod.Name, err)
	}
	defer stream.Close()

	reader := bufio.NewReader(stream)

	// 현재 체크포인트의 라인 카운트 가져오기
	var lineNum int64
	if checkpoint != nil {
		lineNum = checkpoint.lineCount
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if !s.follow {
					return nil // 배치 모드에서는 정상 종료
				}
				// follow 모드에서 EOF는 연결 끊김을 의미
				return fmt.Errorf("stream EOF, reconnecting")
			}
			return fmt.Errorf("error reading log: %w", err)
		}

		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}

		lineNum++

		// 타임스탬프와 메시지 분리 (K8s 로그 형식: "2024-01-01T00:00:00.000000000Z message")
		timestampStr, message := parseK8sLogLine(line)

		// 타임스탬프 파싱 및 체크포인트 업데이트
		var logTime time.Time
		if timestampStr != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, timestampStr); parseErr == nil {
				logTime = parsed
				// 체크포인트 업데이트
				s.updateCheckpoint(checkpointKey, logTime, lineNum)
			}
		}

		// 레코드 생성
		data := s.parseLogMessage(message)

		// 메타데이터 추가
		data["_pod_name"] = pod.Name
		data["_pod_namespace"] = pod.Namespace
		data["_container_name"] = containerName
		data["_timestamp"] = timestampStr
		data["_node_name"] = pod.Spec.NodeName

		// 레이블 추가
		if len(pod.Labels) > 0 {
			data["_labels"] = pod.Labels
		}

		record := Record{
			Data: data,
			Metadata: Metadata{
				Source:    "kubernetes",
				Origin:    fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, containerName),
				Offset:    timestampStr, // 타임스탬프를 오프셋으로 사용
				Timestamp: time.Now().UnixMilli(),
			},
		}

		select {
		case records <- record:
		case <-ctx.Done():
			return nil
		}
	}
}

// updateCheckpoint 체크포인트 업데이트
func (s *KubernetesSource) updateCheckpoint(key string, timestamp time.Time, lineCount int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkpoints[key] = &podCheckpoint{
		lastTimestamp: timestamp,
		lineCount:     lineCount,
	}
}

// GetCheckpoint 특정 Pod의 체크포인트 조회 (디버깅/모니터링용)
func (s *KubernetesSource) GetCheckpoint(namespace, podName, containerName string) (time.Time, int64) {
	key := fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if cp := s.checkpoints[key]; cp != nil {
		return cp.lastTimestamp, cp.lineCount
	}
	return time.Time{}, 0
}

// parseLogMessage 로그 메시지를 파싱하여 map으로 변환
func (s *KubernetesSource) parseLogMessage(message string) map[string]any {
	switch s.logFormat {
	case "json":
		return s.parseJSONLog(message)
	case "text":
		return s.parseTextLog(message)
	case "auto":
		fallthrough
	default:
		// 자동 감지: JSON으로 시작하면 JSON 파싱 시도
		if strings.HasPrefix(strings.TrimSpace(message), "{") {
			if data := s.parseJSONLog(message); len(data) > 1 || data["message"] != message {
				return data
			}
		}
		return s.parseTextLog(message)
	}
}

// parseJSONLog JSON 형식 로그 파싱
func (s *KubernetesSource) parseJSONLog(message string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(message), &data); err != nil {
		// JSON 파싱 실패 시 원본 메시지 반환
		return map[string]any{"message": message}
	}
	return data
}

// parseTextLog 텍스트 형식 로그 파싱 (패턴 또는 일반 텍스트)
func (s *KubernetesSource) parseTextLog(message string) map[string]any {
	// 커스텀 패턴이 있는 경우
	if s.logPattern != nil {
		if match := s.logPattern.FindStringSubmatch(message); match != nil {
			data := make(map[string]any)
			// Named groups 추출
			for i, name := range s.logPattern.SubexpNames() {
				if i != 0 && name != "" && i < len(match) {
					data[name] = match[i]
				}
			}
			if len(data) > 0 {
				data["_raw"] = message
				return data
			}
		}
	}

	// 공통 로그 형식 자동 감지
	// 형식 1: "LEVEL timestamp message" (e.g., "INFO 2024-01-01 10:00:00 Starting server")
	// 형식 2: "timestamp LEVEL message" (e.g., "2024-01-01 10:00:00 INFO Starting server")
	// 형식 3: "[LEVEL] message" (e.g., "[INFO] Starting server")

	data := make(map[string]any)

	// 형식 3: [LEVEL] message
	if strings.HasPrefix(message, "[") {
		if idx := strings.Index(message, "]"); idx > 0 && idx < 10 {
			level := strings.ToUpper(strings.TrimPrefix(message[1:idx], " "))
			if isLogLevel(level) {
				data["level"] = level
				data["message"] = strings.TrimSpace(message[idx+1:])
				return data
			}
		}
	}

	// 공백으로 분리하여 분석
	parts := strings.Fields(message)
	if len(parts) >= 2 {
		// 형식 1: LEVEL timestamp... message
		if isLogLevel(parts[0]) {
			data["level"] = strings.ToUpper(parts[0])
			data["message"] = strings.Join(parts[1:], " ")
			return data
		}

		// 형식 2: timestamp... LEVEL message (최대 3번째까지 확인)
		for i := 1; i < len(parts) && i <= 3; i++ {
			if isLogLevel(parts[i]) {
				data["level"] = strings.ToUpper(parts[i])
				data["message"] = strings.Join(parts[i+1:], " ")
				if i > 0 {
					data["log_timestamp"] = strings.Join(parts[:i], " ")
				}
				return data
			}
		}
	}

	// 기본: 원본 메시지
	data["message"] = message
	return data
}

// isLogLevel 문자열이 로그 레벨인지 확인
func isLogLevel(s string) bool {
	upper := strings.ToUpper(s)
	switch upper {
	case "TRACE", "DEBUG", "INFO", "WARN", "WARNING", "ERROR", "FATAL", "PANIC", "CRITICAL":
		return true
	}
	return false
}

// parseK8sLogLine K8s 로그 라인에서 타임스탬프와 메시지 분리
func parseK8sLogLine(line string) (timestamp string, message string) {
	// K8s 로그 형식: "2024-01-01T00:00:00.000000000Z message"
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", line
}

func (s *KubernetesSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientset = nil
	s.checkpoints = make(map[string]*podCheckpoint)
	return nil
}

// GetPodList 현재 조건에 맞는 Pod 목록 반환 (디버깅/모니터링용)
func (s *KubernetesSource) GetPodList(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	clientset := s.clientset
	s.mu.RUnlock()

	if clientset == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}

	pods, err := s.listPods(ctx, clientset)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, pod := range pods {
		names = append(names, fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
	}
	return names, nil
}
