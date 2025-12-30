package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/conduix/conduix/pipeline-agent/api"
	"github.com/conduix/conduix/pipeline-agent/internal/agent"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// 명령행 인자 파싱
	agentID := flag.String("id", "", "Agent ID (default: auto-generated)")
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

	// 환경변수에서 설정 읽기
	if env := os.Getenv("AGENT_ID"); env != "" && *agentID == "" {
		*agentID = env
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
		ControlPlaneURL:   *controlPlaneURL,
		RedisHost:         *redisHost,
		RedisPort:         *redisPort,
		HeartbeatInterval: 10 * time.Second,
	}

	// 에이전트 생성
	a, err := agent.NewAgent(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}

	// 에이전트 시작
	if err := a.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting agent: %v\n", err)
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
		fmt.Printf("API server listening on %s\n", addr)
		if err := router.Run(addr); err != nil {
			fmt.Fprintf(os.Stderr, "API server error: %v\n", err)
		}
	}()

	// 시그널 대기
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("\nReceived signal: %v\n", sig)

	// 에이전트 종료
	fmt.Println("Stopping agent...")
	if err := a.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Agent stopped")
}
