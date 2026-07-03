package logging

import (
	"log/slog"
	"testing"
)

func TestLevelFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"bogus", slog.LevelInfo},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", c.env)
			if got := levelFromEnv(); got != c.want {
				t.Errorf("LOG_LEVEL=%q → %v, want %v", c.env, got, c.want)
			}
		})
	}
}

func TestSetup_SetsDefaultWithService(t *testing.T) {
	Setup("test-svc")
	// SetDefault 이후 slog.Default()가 동작하는지(패닉 없이) 확인.
	slog.Info("smoke", "k", "v")
}
