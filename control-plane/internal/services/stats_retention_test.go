package services

import (
	"testing"
)

// 보관 기간은 env 로 조절 가능해야 하고, 잘못된 값이 통계를 전부 지우면 안 된다.
func TestStatsRetentionDaysFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"미설정 — 기본값", "", defaultStatsRetentionDays},
		{"정상 값", "30", 30},
		{"0 — 무시(전량 삭제 방지)", "0", defaultStatsRetentionDays},
		{"음수 — 무시", "-7", defaultStatsRetentionDays},
		{"파싱 실패 — 무시", "forever", defaultStatsRetentionDays},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				t.Setenv("STATS_RETENTION_DAYS", "")
			} else {
				t.Setenv("STATS_RETENTION_DAYS", tc.env)
			}
			if got := statsRetentionDaysFromEnv(); got != tc.want {
				t.Fatalf("statsRetentionDaysFromEnv() = %d, want %d", got, tc.want)
			}
		})
	}
}
