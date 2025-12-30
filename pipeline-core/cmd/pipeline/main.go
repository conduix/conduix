package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
	"github.com/conduix/conduix/pipeline-core/pkg/pipeline"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// 명령행 인자 파싱
	configPath := flag.String("c", "", "파이프라인 설정 파일 경로")
	configFile := flag.String("config", "", "파이프라인 설정 파일 경로 (-c 별칭)")
	showVersion := flag.Bool("version", false, "버전 출력")
	validate := flag.Bool("validate", false, "설정 검증 후 종료")

	flag.Parse()

	if *showVersion {
		fmt.Printf("Conduix Pipeline Core %s (built: %s)\n", version, buildTime)
		os.Exit(0)
	}

	// 설정 파일 경로 결정
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = *configFile
	}
	if cfgPath == "" {
		fmt.Fprintln(os.Stderr, "Error: config file path is required")
		fmt.Fprintln(os.Stderr, "Usage: pipeline -c <config.yaml>")
		os.Exit(1)
	}

	// 설정 로드 (v2)
	cfg, err := config.LoadConfigV2(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "설정 로드 실패: %v\n", err)
		os.Exit(1)
	}

	if *validate {
		fmt.Println("설정이 유효합니다")
		os.Exit(0)
	}

	log.Printf("파이프라인 시작: %s (모드: %s)", cfg.Name, cfg.Mode)

	// 파이프라인 생성
	p, err := pipeline.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "파이프라인 생성 실패: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = p.Close() }()

	// 컨텍스트 (시그널 처리)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 시그널 핸들링
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("시그널 수신: %v, 종료 중...", sig)
		cancel()
	}()

	// 파이프라인 실행
	if err := p.Run(ctx); err != nil {
		if err == context.Canceled {
			log.Println("파이프라인이 취소되었습니다")
		} else {
			fmt.Fprintf(os.Stderr, "파이프라인 실행 실패: %v\n", err)
			os.Exit(1)
		}
	}

	// 통계 출력
	stats := p.Stats()
	log.Printf("=== 파이프라인 완료 ===")
	log.Printf("총 레코드: %d", stats.TotalRecords)
	log.Printf("처리됨: %d", stats.ProcessedCount)
	log.Printf("필터링됨: %d", stats.FilteredCount)
	log.Printf("중복: %d", stats.DuplicateCount)
	log.Printf("오류: %d", stats.ErrorCount)
	log.Printf("실행시간: %v", stats.EndTime.Sub(stats.StartTime))
}
