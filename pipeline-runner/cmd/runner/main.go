// Pipeline Runner - K8s Job/Deployment 실행기
// 환경변수로 설정을 받아 파이프라인을 실행하는 독립 바이너리
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/conduix/conduix/pipeline-runner/internal/config"
	"github.com/conduix/conduix/pipeline-runner/internal/runner"
	"github.com/conduix/conduix/shared/logging"

	// K8s 메모리 limit(cgroup)을 GOMEMLIMIT 에 반영. CPU 쪽(GOMAXPROCS)은 Go 1.25+ 런타임이
	// cgroup CPU limit 을 직접 읽어 정하므로 automaxprocs 를 뺐다 — 단, limit 이 설정돼 있어야
	// 동작한다(위임 실행 pod 은 JobConfig 기본값으로 limit 을 받는다).
	_ "github.com/KimMachineGun/automemlimit"
)

func main() {
	logging.Setup("pipeline-runner")
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
