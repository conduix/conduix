# Conduix - Control Plane Module Makefile
# 운영툴 백엔드 API 서버

ifndef BUILD_TAG
    BUILD_TAG=$(shell git branch --show-current 2>/dev/null || echo "dev")
endif

BUILD_TIME=$(shell TZ=Asia/Seoul date '+%Y-%m-%dT%H:%M:%S%z')
BUILD_UNIX_TIME=$(shell date '+%s')
GITHASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

MODULE_NAME=control-plane
BINARY=control-plane
BUILD_DIR=../build
PACKAGE_DIR=$(MODULE_NAME)
PACKAGE_TAR=$(MODULE_NAME)-$(BUILD_TAG)-$(OS)-$(ARCH).tar.gz
TARGET_DIR=target

# ldflags for version info
LDFLAGS=-X main.version=$(BUILD_TAG) -X main.buildTime=$(BUILD_TIME) -X main.gitHash=$(GITHASH)

# 환경변수 기본값
DB_HOST?=localhost
DB_PORT?=3306
DB_USER?=vpuser
DB_PASSWORD?=vppassword
DB_NAME?=conduix
REDIS_ADDR?=localhost:6379
JWT_SECRET?=dev-secret-key
PORT?=8080

.PHONY: all build clean test lint fmt tidy vet check run help package docker-build migrate swagger

all: build

## 빌드
build: ## 바이너리 빌드
	@echo "==> [$(MODULE_NAME)] 빌드 중..."
	@echo "    Tag: $(BUILD_TAG)"
	@echo "    Time: $(BUILD_TIME)"
	@echo "    Hash: $(GITHASH)"
	@mkdir -p $(BUILD_DIR)
	GOOS=$(OS) GOARCH=$(ARCH) go build -o $(BUILD_DIR)/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/server
	@echo "==> [$(MODULE_NAME)] 빌드 완료: $(BUILD_DIR)/$(BINARY)"

build-linux: ## Linux 바이너리 빌드
	@echo "==> [$(MODULE_NAME)] Linux 빌드 중..."
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 -ldflags "$(LDFLAGS)" ./cmd/server

build-all: build build-linux ## 모든 플랫폼 빌드

## 의존성 관리
tidy: ## go mod tidy 실행
	@echo "==> [$(MODULE_NAME)] 의존성 정리 중..."
	go mod tidy

deps: tidy ## 의존성 다운로드
	@echo "==> [$(MODULE_NAME)] 의존성 다운로드 중..."
	go mod download

## 실행
run: ## 서버 실행
	@echo "==> [$(MODULE_NAME)] 실행 중..."
	@echo "    DB: $(DB_HOST):$(DB_PORT)/$(DB_NAME)"
	@echo "    Redis: $(REDIS_ADDR)"
	@echo "    Port: $(PORT)"
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME) REDIS_ADDR=$(REDIS_ADDR) JWT_SECRET=$(JWT_SECRET) \
		go run ./cmd/server -port $(PORT)

run-debug: ## 디버그 모드로 실행
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME) REDIS_ADDR=$(REDIS_ADDR) JWT_SECRET=$(JWT_SECRET) \
		go run ./cmd/server -port $(PORT) -debug

run-local: ## 로컬 개발용 실행 (docker-compose 인프라 사용)
	DB_HOST=localhost DB_PORT=3306 DB_USER=vpuser DB_PASSWORD=vppassword \
	DB_NAME=conduix REDIS_ADDR=localhost:6379 JWT_SECRET=dev-secret-key \
		go run ./cmd/server -port 8080

run-migrate: ## 마이그레이션과 함께 실행
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME) REDIS_ADDR=$(REDIS_ADDR) JWT_SECRET=$(JWT_SECRET) \
		go run ./cmd/server -port $(PORT) -migrate

## 데이터베이스
migrate: ## DB 마이그레이션 실행
	@echo "==> [$(MODULE_NAME)] 마이그레이션 실행 중..."
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME) \
		go run ./cmd/server -migrate

migrate-dry: ## 마이그레이션 드라이런 (실행 안함)
	@echo "==> [$(MODULE_NAME)] 마이그레이션 드라이런..."
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME) \
		go run ./cmd/server -migrate -dry-run

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

test-integration: ## 통합 테스트 실행 (DB/Redis 필요)
	@echo "==> [$(MODULE_NAME)] 통합 테스트 실행 중..."
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) \
	DB_NAME=$(DB_NAME)_test REDIS_ADDR=$(REDIS_ADDR) \
		go test -v -tags=integration ./...

## Swagger
swagger: ## Swagger 문서 생성
	@echo "==> [$(MODULE_NAME)] Swagger 문서 생성 중..."
	@if command -v swag >/dev/null 2>&1; then \
		swag init -g cmd/server/main.go -o docs; \
	else \
		echo "swag not installed. Run: go install github.com/swaggo/swag/cmd/swag@latest"; \
	fi

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

## Mock 생성
mock: ## mockery로 mock 생성
	@echo "==> [$(MODULE_NAME)] Mock 생성 중..."
	@if command -v mockery >/dev/null 2>&1; then \
		mockery --all; \
	else \
		echo "mockery not installed. Run: go install github.com/vektra/mockery/v2@latest"; \
	fi

## 패키지
package: build ## 배포 패키지 생성
	@echo "==> [$(MODULE_NAME)] 패키지 생성 중..."
	mkdir -p $(TARGET_DIR)/$(PACKAGE_DIR)
	cp $(BUILD_DIR)/$(BINARY) $(TARGET_DIR)/$(PACKAGE_DIR)/
	cp -r migrations $(TARGET_DIR)/$(PACKAGE_DIR)/ 2>/dev/null || true
	tar -C $(TARGET_DIR) -zcvf $(TARGET_DIR)/$(PACKAGE_TAR) $(PACKAGE_DIR)
	@echo "==> 패키지 생성 완료: $(TARGET_DIR)/$(PACKAGE_TAR)"

## Docker
docker-build: ## Docker 이미지 빌드
	@echo "==> [$(MODULE_NAME)] Docker 이미지 빌드 중..."
	CGO_ENABLED=0 go build -a -ldflags "-w -s $(LDFLAGS)" -o $(BINARY) ./cmd/server

## 정리
clean: ## 빌드 아티팩트 정리
	@echo "==> [$(MODULE_NAME)] 정리 중..."
	rm -rf $(TARGET_DIR)
	rm -f $(BUILD_DIR)/$(BINARY)*
	rm -f coverage.out coverage.html
	rm -rf docs
	go clean -testcache -cache

## 진단
diagnostic: ## gopls 진단 실행
	@gopls check $$(find . -name "*.go")

## API 테스트
api-health: ## 헬스체크 API 호출
	@curl -s http://localhost:$(PORT)/health | jq . || echo "Server not running"

api-ready: ## 준비 상태 API 호출
	@curl -s http://localhost:$(PORT)/ready | jq . || echo "Server not running"

api-pipelines: ## 파이프라인 목록 조회 (인증 필요)
	@curl -s -H "Authorization: Bearer $(TOKEN)" http://localhost:$(PORT)/api/v1/pipelines | jq .

## 도움말
help: ## 옵션 보기
	@echo "$(MODULE_NAME) 모듈 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
