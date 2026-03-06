package leader

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Callbacks 리더 선출 이벤트 콜백
type Callbacks struct {
	OnStartedLeading func(ctx context.Context) // 리더가 되었을 때
	OnStoppedLeading func()                    // 리더를 잃었을 때
	OnNewLeader      func(identity string)     // 새 리더가 선출되었을 때
}

// Election K8s Lease 기반 리더 선출
type Election struct {
	clientset kubernetes.Interface
	namespace string
	name      string // Lease 리소스 이름
	identity  string // 현재 Pod 식별자
	callbacks Callbacks
	cancel    context.CancelFunc
	isLeader  bool
}

// Config 리더 선출 설정
type Config struct {
	Namespace     string        // Lease 네임스페이스
	LeaseName     string        // Lease 리소스 이름 (예: conduix-agent-leader)
	Identity      string        // Pod 고유 식별자 (보통 HOSTNAME)
	LeaseDuration time.Duration // 임대 기간 (기본: 15초)
	RenewDeadline time.Duration // 갱신 기한 (기본: 10초)
	RetryPeriod   time.Duration // 재시도 주기 (기본: 2초)
}

// DefaultConfig 기본 설정 반환
func DefaultConfig(namespace string) Config {
	identity, _ := os.Hostname()
	return Config{
		Namespace:     namespace,
		LeaseName:     "conduix-agent-leader",
		Identity:      identity,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
	}
}

// NewElection 리더 선출 인스턴스 생성
func NewElection(clientset kubernetes.Interface, cfg Config, callbacks Callbacks) *Election {
	return &Election{
		clientset: clientset,
		namespace: cfg.Namespace,
		name:      cfg.LeaseName,
		identity:  cfg.Identity,
		callbacks: callbacks,
	}
}

// Start 리더 선출 시작 (블로킹하지 않음)
func (e *Election) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	go e.run(ctx)
}

// Stop 리더 선출 중지
func (e *Election) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// IsLeader 현재 리더 여부 반환
func (e *Election) IsLeader() bool {
	return e.isLeader
}

// Identity 현재 Pod 식별자 반환
func (e *Election) Identity() string {
	return e.identity
}

// run 리더 선출 실행
func (e *Election) run(ctx context.Context) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      e.name,
			Namespace: e.namespace,
		},
		Client: e.clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: e.identity,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				e.isLeader = true
				fmt.Printf("[LeaderElection] %s became leader\n", e.identity)
				if e.callbacks.OnStartedLeading != nil {
					e.callbacks.OnStartedLeading(ctx)
				}
			},
			OnStoppedLeading: func() {
				e.isLeader = false
				fmt.Printf("[LeaderElection] %s lost leadership\n", e.identity)
				if e.callbacks.OnStoppedLeading != nil {
					e.callbacks.OnStoppedLeading()
				}
			},
			OnNewLeader: func(identity string) {
				fmt.Printf("[LeaderElection] New leader elected: %s\n", identity)
				if e.callbacks.OnNewLeader != nil {
					e.callbacks.OnNewLeader(identity)
				}
			},
		},
	})
}
