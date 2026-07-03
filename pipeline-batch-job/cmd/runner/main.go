// Pipeline Runner - K8s Job/Deployment 실행기
// 환경변수로 설정을 받아 파이프라인을 실행하는 독립 바이너리
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/conduix/conduix/pipeline-batch-job/internal/config"
	"github.com/conduix/conduix/pipeline-batch-job/internal/runner"
	"github.com/conduix/conduix/shared/logging"

	// K8s CPU/메모리 limit(cgroup)을 Go 런타임에 반영: GOMAXPROCS/GOMEMLIMIT 자동 설정.
	_ "github.com/KimMachineGun/automemlimit"
	_ "go.uber.org/automaxprocs"
)

func main() {
	logging.Setup("pipeline-batch-job")
	slog.Info("starting")

	// 설정 로드
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	slog.Info("config loaded", "mode", cfg.Mode, "workflow_id", cfg.WorkflowID)

	// 시그널 핸들링 (graceful shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig.String())
		cancel()
	}()

	// Runner 실행
	r := runner.New(cfg)
	if err := r.Run(ctx); err != nil {
		slog.Error("execution error", "error", err)
		os.Exit(1)
	}

	slog.Info("finished")
}
