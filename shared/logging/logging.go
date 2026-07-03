// Package logging은 서버 바이너리 공통 slog 초기화를 제공한다.
// K8s 로그 수집에 적합한 JSON 핸들러를 기본으로 하고, 레벨은 LOG_LEVEL env로 제어한다.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup은 프로세스 전역 기본 로거를 JSON 핸들러로 설정한다.
// LOG_LEVEL(debug|info|warn|error, 기본 info)로 최소 레벨을 조절한다.
// service는 모든 로그에 붙는 상관키(service=<name>)로, 멀티서비스 로그를 구분한다.
func Setup(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: levelFromEnv(),
	})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)
	return logger
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
