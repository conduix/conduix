# Conduix - Pipeline Agent Module Makefile
# 파이프라인 에이전트 (파이프라인 실행 및 관리)

ifndef BUILD_TAG
    BUILD_TAG=$(shell git branch --show-current 2>/dev/null || echo "dev")
endif

BUILD_TIME=$(shell TZ=Asia/Seoul date '+%Y-%m-%dT%H:%M:%S%z')
BUILD_UNIX_TIME=$(shell date '+%s')
GITHASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

MODULE_NAME=pipeline-agent
BINARY=agent
BUILD_DIR=../build
PACKAGE_DIR=$(MODULE_NAME)
PACKAGE_TAR=$(MODULE_NAME)-$(BUILD_TAG)-$(OS)-$(ARCH).tar.gz
TARGET_DIR=target

# ldflags for version info
LDFLAGS=-X main.version=$(BUILD_TAG) -X main.buildTime=$(BUILD_TIME) -X main.gitHash=$(GITHASH)

# 환경변수 기본값
REDIS_ADDR?=localhost:6379
CONTROL_PLANE_URL?=http://localhost:8080

.PHONY: all build clean test lint fmt tidy vet check run help package docker-build

all: build

## 빌드
build: ## 바이너리 빌드
	@echo "==> [$(MODULE_NAME)] 빌드 중..."
	@echo "    Tag: $(BUILD_TAG)"
	@echo "    Time: $(BUILD_TIME)"
	@echo "    Hash: $(GITHASH)"
	@mkdir -p $(BUILD_DIR)
	GOOS=$(OS) GOARCH=$(ARCH) go build -o $(BUILD_DIR)/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/agent
	@echo "==> [$(MODULE_NAME)] 빌드 완료: $(BUILD_DIR)/$(BINARY)"

build-linux: ## Linux 바이너리 빌드
	@echo "==> [$(MODULE_NAME)] Linux 빌드 중..."
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 -ldflags "$(LDFLAGS)" ./cmd/agent

build-all: build build-linux ## 모든 플랫폼 빌드

## 의존성 관리
tidy: ## go mod tidy 실행
	@echo "==> [$(MODULE_NAME)] 의존성 정리 중..."
	go mod tidy

deps: tidy ## 의존성 다운로드
	@echo "==> [$(MODULE_NAME)] 의존성 다운로드 중..."
	go mod download

## 실행
run: ## 에이전트 실행
	@echo "==> [$(MODULE_NAME)] 실행 중..."
	@echo "    Redis: $(REDIS_ADDR)"
	@echo "    Control Plane: $(CONTROL_PLANE_URL)"
	REDIS_ADDR=$(REDIS_ADDR) CONTROL_PLANE_URL=$(CONTROL_PLANE_URL) \
		go run ./cmd/agent

run-debug: ## 디버그 모드로 실행
	REDIS_ADDR=$(REDIS_ADDR) CONTROL_PLANE_URL=$(CONTROL_PLANE_URL) \
		go run ./cmd/agent -debug

run-local: ## 로컬 개발용 실행 (docker-compose 인프라 사용)
	REDIS_ADDR=localhost:6379 CONTROL_PLANE_URL=http://localhost:8080 \
		go run ./cmd/agent

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

test-integration: ## 통합 테스트 실행 (Redis 필요)
	@echo "==> [$(MODULE_NAME)] 통합 테스트 실행 중..."
	REDIS_ADDR=$(REDIS_ADDR) go test -v -tags=integration ./...

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

## 패키지
package: build ## 배포 패키지 생성
	@echo "==> [$(MODULE_NAME)] 패키지 생성 중..."
	mkdir -p $(TARGET_DIR)/$(PACKAGE_DIR)
	cp $(BUILD_DIR)/$(BINARY) $(TARGET_DIR)/$(PACKAGE_DIR)/
	tar -C $(TARGET_DIR) -zcvf $(TARGET_DIR)/$(PACKAGE_TAR) $(PACKAGE_DIR)
	@echo "==> 패키지 생성 완료: $(TARGET_DIR)/$(PACKAGE_TAR)"

## Docker
docker-build: ## Docker 이미지 빌드
	@echo "==> [$(MODULE_NAME)] Docker 이미지 빌드 중..."
	CGO_ENABLED=0 go build -a -ldflags "-w -s $(LDFLAGS)" -o $(BINARY) ./cmd/agent

## 정리
clean: ## 빌드 아티팩트 정리
	@echo "==> [$(MODULE_NAME)] 정리 중..."
	rm -rf $(TARGET_DIR)
	rm -f $(BUILD_DIR)/$(BINARY)*
	rm -f coverage.out coverage.html
	go clean -testcache -cache

## 진단
diagnostic: ## gopls 진단 실행
	@gopls check $$(find . -name "*.go")

## 헬스체크
health: ## 에이전트 헬스체크 (실행 중인 경우)
	@curl -s http://localhost:8081/health | jq . || echo "Agent not running"

status: ## 에이전트 상태 조회
	@curl -s http://localhost:8081/status | jq . || echo "Agent not running"

## 도움말
help: ## 옵션 보기
	@echo "$(MODULE_NAME) 모듈 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
