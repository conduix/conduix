package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	// K8s CPU/메모리 limit(cgroup)을 Go 런타임에 반영: GOMAXPROCS/GOMEMLIMIT 자동 설정.
	_ "github.com/KimMachineGun/automemlimit"
	_ "go.uber.org/automaxprocs"

	"github.com/conduix/conduix/pipeline-worker/api"
	"github.com/conduix/conduix/pipeline-worker/internal/agent"
	"github.com/conduix/conduix/shared/logging"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// Batch 모드 확인 (환경변수 기반)
	if os.Getenv("EXECUTION_MODE") == "batch" {
		runBatchMode()
		return
	}

	// 기존 Agent 모드
	runAgentMode()
}

// runBatchMode Kubernetes Job용 1회 실행 모드
func runBatchMode() {
	logging.Setup("pipeline-worker")
	slog.Info("starting in batch mode")

	batchRunner, err := agent.NewBatchRunner()
	if err != nil {
		slog.Error("failed to create batch runner", "error", err)
		os.Exit(1)
	}

	if err := batchRunner.Run(); err != nil {
		slog.Error("batch execution failed", "error", err)
		os.Exit(1)
	}

	slog.Info("batch execution completed successfully")
}

// runAgentMode 기존 상주 Agent 모드
func runAgentMode() {
	// 명령행 인자 파싱
	agentID := flag.String("id", "", "Agent ID (default: auto-generated)")
	clusterID := flag.String("cluster-id", "", "Cluster ID this agent belongs to")
	controlPlaneURL := flag.String("control-plane", "http://localhost:8080", "Control plane URL")
	redisHost := flag.String("redis-host", "localhost", "Redis host")
	redisPort := flag.Int("redis-port", 6379, "Redis port")
	apiPort := flag.Int("port", 8081, "API server port")
	showVersion := flag.Bool("version", false, "Show version")

	flag.Parse()

	if *showVersion {
		fmt.Printf("Conduix Pipeline Agent %s (built: %s)\n", version, buildTime)
		os.Exit(0)
	}

	logging.Setup("pipeline-worker")

	// 환경변수에서 설정 읽기
	if env := os.Getenv("AGENT_ID"); env != "" && *agentID == "" {
		*agentID = env
	}
	if env := os.Getenv("CLUSTER_ID"); env != "" && *clusterID == "" {
		*clusterID = env
	}
	if env := os.Getenv("CONTROL_PLANE_URL"); env != "" {
		*controlPlaneURL = env
	}
	if env := os.Getenv("REDIS_HOST"); env != "" {
		*redisHost = env
	}

	// 에이전트 설정
	cfg := &agent.Config{
		ID:                *agentID,
		ClusterID:         *clusterID,
		ControlPlaneURL:   *controlPlaneURL,
		RedisHost:         *redisHost,
		RedisPort:         *redisPort,
		HeartbeatInterval: 10 * time.Second,
		// batch 위임 시 이 worker가 자기 cluster에 만들 K8s Job 설정
		Namespace:   os.Getenv("NAMESPACE"),
		RunnerImage: os.Getenv("RUNNER_IMAGE"), // pipeline-batch-job 이미지
	}

	// reconcile 백스톱 주기(선택). 미지정 시 in-code 기본값(60s). pub/sub 유실 복구 지연 튜닝용.
	if env := os.Getenv("AGENT_RECONCILE_INTERVAL_SEC"); env != "" {
		if sec, err := strconv.Atoi(env); err == nil && sec > 0 {
			cfg.ReconcileInterval = time.Duration(sec) * time.Second
		}
	}

	// 에이전트 생성
	a, err := agent.NewAgent(cfg)
	if err != nil {
		slog.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	// 에이전트 시작
	if err := a.Start(); err != nil {
		slog.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	// API 서버 시작
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	handler := api.NewHandler(a)
	handler.RegisterRoutes(router)

	go func() {
		addr := fmt.Sprintf(":%d", *apiPort)
		slog.Info("API server listening", "addr", addr)
		if err := router.Run(addr); err != nil {
			slog.Error("API server error", "error", err)
		}
	}()

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	slog.Info("received signal, stopping agent", "signal", sig.String())

	if err := a.Stop(); err != nil {
		slog.Error("failed to stop agent", "error", err)
		os.Exit(1)
	}

	slog.Info("agent stopped")
}
