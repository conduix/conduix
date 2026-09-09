package k8s

import (
	"testing"
	"time"
)

// Deployment 는 RestartPolicy=Always 라 pod 이 못 떠도 계속 존재한다.
// 존재 여부만 보면 "살아있다"로 오판하므로 준비 상태로 판정해야 한다.
func TestStreamingExecution_Healthy(t *testing.T) {
	tests := []struct {
		name    string
		ready   int32
		desired int32
		want    bool
	}{
		{"준비 완료", 1, 1, true},
		{"CrashLoop — 하나도 못 뜸", 0, 1, false},
		{"부분 준비", 1, 2, false},
		{"초과 준비(롤링 중)", 2, 1, true},
		{"desired 0 — 판정 불가", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := StreamingExecution{ReadyReplicas: tc.ready, DesiredReplicas: tc.desired}
			if got := s.Healthy(); got != tc.want {
				t.Fatalf("Healthy() = %v, want %v (ready=%d desired=%d)", got, tc.want, tc.ready, tc.desired)
			}
		})
	}
}

// CreatedAt 은 유예 판정의 기준이라 반드시 채워져야 한다.
func TestStreamingExecution_CreatedAtCarriesAge(t *testing.T) {
	created := time.Now().Add(-10 * time.Minute)
	s := StreamingExecution{CreatedAt: created}
	if age := time.Since(s.CreatedAt); age < 9*time.Minute {
		t.Fatalf("age too small: %v", age)
	}
}
