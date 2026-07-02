// Pipeline Runner - K8s Job/Deployment 실행기
// 환경변수로 설정을 받아 파이프라인을 실행하는 독립 바이너리
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/conduix/conduix/pipeline-batch-job/internal/config"
	"github.com/conduix/conduix/pipeline-batch-job/internal/runner"
)

func main() {
	fmt.Println("[pipeline-batch-job] Starting...")

	// 설정 로드
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pipeline-batch-job] Configuration error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[pipeline-batch-job] Mode=%s, WorkflowID=%s\n", cfg.Mode, cfg.WorkflowID)

	// 시그널 핸들링 (graceful shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("[pipeline-batch-job] Received signal: %v, shutting down...\n", sig)
		cancel()
	}()

	// Runner 실행
	r := runner.New(cfg)
	if err := r.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[pipeline-batch-job] Execution error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("[pipeline-batch-job] Finished.")
}
