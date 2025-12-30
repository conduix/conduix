// 필터 타입 생성기
// 백엔드 필터 정의를 프론트엔드 TypeScript 파일로 생성
//
// 사용법:
//
//	go run cmd/gen-filter-types/main.go
//
// 또는 go generate:
//
//	//go:generate go run cmd/gen-filter-types/main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conduix/conduix/pipeline-core/pkg/filter"
)

func main() {
	outputDir := flag.String("o", "../web-ui/src/types/generated", "출력 디렉토리")
	format := flag.String("f", "ts", "출력 형식 (ts, json)")
	flag.Parse()

	registry := filter.Global()

	switch *format {
	case "ts":
		generateTypeScript(registry, *outputDir)
	case "json":
		generateJSON(registry, *outputDir)
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 형식: %s\n", *format)
		os.Exit(1)
	}
}

func generateTypeScript(registry *filter.FilterRegistry, outputDir string) {
	// 디렉토리 생성
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "디렉토리 생성 실패: %v\n", err)
		os.Exit(1)
	}

	// TypeScript 파일 생성
	content := registry.ToTypeScript()
	outputPath := filepath.Join(outputDir, "filter-operators.ts")

	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "파일 쓰기 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("생성됨: %s\n", outputPath)
}

func generateJSON(registry *filter.FilterRegistry, outputDir string) {
	// 디렉토리 생성
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "디렉토리 생성 실패: %v\n", err)
		os.Exit(1)
	}

	// JSON 파일 생성
	content, err := registry.ToJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON 생성 실패: %v\n", err)
		os.Exit(1)
	}

	outputPath := filepath.Join(outputDir, "filter-operators.json")

	if err := os.WriteFile(outputPath, content, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "파일 쓰기 실패: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("생성됨: %s\n", outputPath)
}
