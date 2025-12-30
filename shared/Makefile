# Conduix - Shared Module Makefile
# 공유 타입 및 유틸리티 라이브러리

ifndef BUILD_TAG
    BUILD_TAG=$(shell git branch --show-current 2>/dev/null || echo "dev")
endif

BUILD_TIME=$(shell TZ=Asia/Seoul date '+%Y-%m-%dT%H:%M:%S%z')
GITHASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

MODULE_NAME=shared

.PHONY: all build clean test lint fmt tidy vet check help

all: build

## 빌드 (라이브러리이므로 컴파일 체크만)
build: ## 빌드 실행 (컴파일 체크)
	@echo "==> [$(MODULE_NAME)] 빌드 중..."
	go build ./...
	@echo "==> [$(MODULE_NAME)] 빌드 완료"

## 의존성 관리
tidy: ## go mod tidy 실행
	@echo "==> [$(MODULE_NAME)] 의존성 정리 중..."
	go mod tidy

deps: tidy ## 의존성 다운로드
	@echo "==> [$(MODULE_NAME)] 의존성 다운로드 중..."
	go mod download

## 테스트
test: ## 테스트 실행
	@echo "==> [$(MODULE_NAME)] 테스트 실행 중..."
	go test -v -cover -shuffle=on ./...

test-short: ## 짧은 테스트만 실행
	go test -v -short ./...

test-race: ## 레이스 감지 테스트
	go test -v -race ./...

test-coverage: ## 커버리지 리포트 생성
	@echo "==> [$(MODULE_NAME)] 커버리지 리포트 생성 중..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "==> coverage.html 생성 완료"

## 코드 품질
lint: ## golangci-lint 실행
	@echo "==> [$(MODULE_NAME)] 린트 실행 중..."
	golangci-lint run

vet: ## go vet 실행
	@echo "==> [$(MODULE_NAME)] vet 실행 중..."
	go vet ./...

fmt: ## gofumpt 포맷팅
	@echo "==> [$(MODULE_NAME)] 포맷팅 중..."
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w -l $$(find . -name "*.go"); \
	else \
		go fmt ./...; \
	fi

check: vet lint test ## 전체 체크 (vet + lint + test)

## 정리
clean: ## 캐시 정리
	@echo "==> [$(MODULE_NAME)] 정리 중..."
	go clean -testcache -cache
	rm -f coverage.out coverage.html

## 진단
diagnostic: ## gopls 진단 실행
	@gopls check $$(find . -name "*.go")

## 도움말
help: ## 옵션 보기
	@echo "$(MODULE_NAME) 모듈 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
