# Conduix - Root Makefile
# 전체 프로젝트 빌드 및 관리

ifndef BUILD_TAG
    BUILD_TAG=$(shell git branch --show-current 2>/dev/null || echo "dev")
endif

BUILD_TIME=$(shell TZ=Asia/Seoul date '+%Y-%m-%dT%H:%M:%S%z')
BUILD_UNIX_TIME=$(shell date '+%s')
GITHASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
OS=$(shell go env GOOS)
ARCH=$(shell go env GOARCH)

# 디렉토리
SHARED := ./shared
PIPELINE_CORE := ./pipeline-core
PIPELINE_AGENT := ./pipeline-agent
CONTROL_PLANE := ./control-plane
WEB_UI := ./web-ui
BUILD_DIR := ./build
TARGET_DIR := ./target

# Docker
DOCKER_COMPOSE := docker-compose
DOCKER_REGISTRY ?= conduix

.PHONY: all build clean deps test lint fmt help
.PHONY: build-go build-web build-core build-agent build-control-plane
.PHONY: run dev infra-up infra-down docker-build docker-push
.PHONY: package release check tidy

# ============================================================================
# 기본 타겟
# ============================================================================

all: deps build ## 의존성 설치 및 전체 빌드

## 버전 정보
version: ## 빌드 정보 출력
	@echo "Build Tag:  $(BUILD_TAG)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Hash:   $(GITHASH)"
	@echo "OS/Arch:    $(OS)/$(ARCH)"

# ============================================================================
# 의존성
# ============================================================================

deps: deps-go deps-web ## 모든 의존성 설치

deps-go: ## Go 의존성 설치
	@echo "==> Go 의존성 설치 중..."
	cd $(SHARED) && go mod download
	cd $(PIPELINE_CORE) && go mod download
	cd $(PIPELINE_AGENT) && go mod download
	cd $(CONTROL_PLANE) && go mod download

deps-web: ## Web UI 의존성 설치
	@echo "==> Web UI 의존성 설치 중..."
	cd $(WEB_UI) && npm install

tidy: ## 모든 Go 모듈 go mod tidy 실행
	@echo "==> Go 모듈 정리 중..."
	cd $(SHARED) && go mod tidy
	cd $(PIPELINE_CORE) && go mod tidy
	cd $(PIPELINE_AGENT) && go mod tidy
	cd $(CONTROL_PLANE) && go mod tidy

# ============================================================================
# 빌드
# ============================================================================

build: build-go build-web ## 전체 빌드

build-go: build-core build-agent build-control-plane ## 모든 Go 바이너리 빌드

build-core: ## pipeline-core 빌드
	@echo "==> pipeline-core 빌드 중..."
	cd $(PIPELINE_CORE) && $(MAKE) build

build-agent: ## pipeline-agent 빌드
	@echo "==> pipeline-agent 빌드 중..."
	cd $(PIPELINE_AGENT) && $(MAKE) build

build-control-plane: ## control-plane 빌드
	@echo "==> control-plane 빌드 중..."
	cd $(CONTROL_PLANE) && $(MAKE) build

build-web: ## web-ui 빌드
	@echo "==> web-ui 빌드 중..."
	cd $(WEB_UI) && $(MAKE) build

build-linux: ## Linux 바이너리 빌드 (모든 Go 모듈)
	@echo "==> Linux 빌드 중..."
	cd $(PIPELINE_CORE) && $(MAKE) build-linux
	cd $(PIPELINE_AGENT) && $(MAKE) build-linux
	cd $(CONTROL_PLANE) && $(MAKE) build-linux

# ============================================================================
# 테스트
# ============================================================================

test: test-go test-web ## 전체 테스트 실행

test-go: ## Go 테스트 실행
	@echo "==> Go 테스트 실행 중..."
	cd $(SHARED) && go test -v ./...
	cd $(PIPELINE_CORE) && go test -v ./...
	cd $(PIPELINE_AGENT) && go test -v ./...
	cd $(CONTROL_PLANE) && go test -v ./...

test-web: ## Web UI 테스트 실행
	@echo "==> Web UI 테스트 실행 중..."
	cd $(WEB_UI) && npm run test || true

test-coverage: ## 커버리지 리포트 생성
	@echo "==> 커버리지 리포트 생성 중..."
	cd $(PIPELINE_CORE) && $(MAKE) test-coverage
	cd $(PIPELINE_AGENT) && $(MAKE) test-coverage
	cd $(CONTROL_PLANE) && $(MAKE) test-coverage

test-race: ## 레이스 감지 테스트
	@echo "==> 레이스 감지 테스트 중..."
	cd $(SHARED) && go test -race ./...
	cd $(PIPELINE_CORE) && go test -race ./...
	cd $(PIPELINE_AGENT) && go test -race ./...
	cd $(CONTROL_PLANE) && go test -race ./...

# ============================================================================
# 코드 품질
# ============================================================================

lint: lint-go lint-web ## 전체 린트 실행

lint-go: ## Go 린트 실행
	@echo "==> Go 린트 실행 중..."
	@for dir in $(SHARED) $(PIPELINE_CORE) $(PIPELINE_AGENT) $(CONTROL_PLANE); do \
		echo "Linting $$dir..."; \
		cd $$dir && golangci-lint run && cd ..; \
	done

lint-web: ## Web UI 린트 실행
	@echo "==> Web UI 린트 실행 중..."
	cd $(WEB_UI) && npm run lint || true

fmt: fmt-go fmt-web ## 전체 포맷팅

fmt-go: ## Go 포맷팅
	@echo "==> Go 포맷팅 중..."
	@if command -v gofumpt >/dev/null 2>&1; then \
		find . -name "*.go" -not -path "./web-ui/*" | xargs gofumpt -w -l; \
	else \
		gofmt -w -l $$(find . -name "*.go" -not -path "./web-ui/*"); \
	fi

fmt-web: ## Web UI 포맷팅
	cd $(WEB_UI) && npm run format || true

vet: ## Go vet 실행
	@echo "==> Go vet 실행 중..."
	cd $(SHARED) && go vet ./...
	cd $(PIPELINE_CORE) && go vet ./...
	cd $(PIPELINE_AGENT) && go vet ./...
	cd $(CONTROL_PLANE) && go vet ./...

check: vet lint test ## 전체 체크 (vet + lint + test)

# ============================================================================
# 개발 모드
# ============================================================================

dev: infra-up ## 개발 모드 시작 (인프라 + 안내)
	@echo ""
	@echo "============================================"
	@echo "  인프라 준비 완료!"
	@echo "============================================"
	@echo ""
	@echo "각 서비스를 별도 터미널에서 실행하세요:"
	@echo ""
	@echo "  터미널 1: make run-control-plane"
	@echo "  터미널 2: make run-agent"
	@echo "  터미널 3: make run-web"
	@echo "  터미널 4: make run-pipeline (선택)"
	@echo ""
	@echo "또는 각 모듈 디렉토리에서:"
	@echo "  cd control-plane && make run"
	@echo "  cd pipeline-agent && make run"
	@echo "  cd web-ui && make dev"
	@echo ""

run-control-plane: ## Control Plane 실행
	cd $(CONTROL_PLANE) && $(MAKE) run-local

run-agent: ## Pipeline Agent 실행
	cd $(PIPELINE_AGENT) && $(MAKE) run-local

run-web: ## Web UI 개발 서버 실행
	cd $(WEB_UI) && $(MAKE) dev

run-pipeline: ## 샘플 파이프라인 실행
	cd $(PIPELINE_CORE) && $(MAKE) run

# ============================================================================
# 인프라 관리
# ============================================================================

infra-up: ## 인프라 시작 (MySQL, Redis)
	@echo "==> 인프라 시작 중 (MySQL, Redis)..."
	$(DOCKER_COMPOSE) up -d mysql redis
	@echo "==> MySQL: localhost:3306"
	@echo "==> Redis: localhost:6379"
	@echo ""
	@echo "MySQL 준비 대기 중..."
	@sleep 5
	@echo "==> 인프라 준비 완료"

infra-down: ## 인프라 중지
	@echo "==> 인프라 중지 중..."
	$(DOCKER_COMPOSE) down

infra-logs: ## 인프라 로그 보기
	$(DOCKER_COMPOSE) logs -f mysql redis

infra-reset: ## 인프라 초기화 (데이터 삭제)
	@echo "==> 인프라 초기화 중 (모든 데이터 삭제)..."
	$(DOCKER_COMPOSE) down -v
	@echo "==> 초기화 완료"

# ============================================================================
# Docker
# ============================================================================

docker-build: ## 모든 Docker 이미지 빌드
	@echo "==> Docker 이미지 빌드 중..."
	docker build -f deploy/docker/Dockerfile.control-plane -t $(DOCKER_REGISTRY)/control-plane:$(BUILD_TAG) .
	docker build -f deploy/docker/Dockerfile.agent -t $(DOCKER_REGISTRY)/agent:$(BUILD_TAG) .
	docker build -f deploy/docker/Dockerfile.web-ui -t $(DOCKER_REGISTRY)/web-ui:$(BUILD_TAG) .
	@echo "==> Docker 이미지 빌드 완료"
	@docker images | grep $(DOCKER_REGISTRY)

docker-push: ## Docker 이미지 푸시
	@echo "==> Docker 이미지 푸시 중..."
	docker push $(DOCKER_REGISTRY)/control-plane:$(BUILD_TAG)
	docker push $(DOCKER_REGISTRY)/agent:$(BUILD_TAG)
	docker push $(DOCKER_REGISTRY)/web-ui:$(BUILD_TAG)

up: ## Docker Compose로 전체 실행
	$(DOCKER_COMPOSE) up -d

down: ## Docker Compose 중지
	$(DOCKER_COMPOSE) down

logs: ## Docker Compose 로그 보기
	$(DOCKER_COMPOSE) logs -f

ps: ## Docker Compose 상태 확인
	$(DOCKER_COMPOSE) ps

# ============================================================================
# 데이터베이스
# ============================================================================

migrate: ## DB 마이그레이션 실행
	cd $(CONTROL_PLANE) && $(MAKE) migrate

migrate-local: ## 로컬 DB 마이그레이션
	cd $(CONTROL_PLANE) && $(MAKE) run-migrate

# ============================================================================
# 패키지 및 릴리스
# ============================================================================

package: build ## 배포 패키지 생성
	@echo "==> 배포 패키지 생성 중..."
	mkdir -p $(TARGET_DIR)
	cd $(PIPELINE_CORE) && $(MAKE) package
	cd $(PIPELINE_AGENT) && $(MAKE) package
	cd $(CONTROL_PLANE) && $(MAKE) package
	cd $(WEB_UI) && $(MAKE) package
	@echo "==> 패키지 생성 완료: $(TARGET_DIR)/"
	@ls -la */target/*.tar.gz 2>/dev/null || true

release: check package docker-build ## 릴리스 빌드 (체크 + 패키지 + Docker)
	@echo "==> 릴리스 빌드 완료"
	@echo "    Tag: $(BUILD_TAG)"
	@echo "    Time: $(BUILD_TIME)"

# ============================================================================
# 정리
# ============================================================================

clean: ## 빌드 아티팩트 정리
	@echo "==> 빌드 아티팩트 정리 중..."
	rm -rf $(BUILD_DIR)
	rm -rf $(TARGET_DIR)
	cd $(SHARED) && $(MAKE) clean
	cd $(PIPELINE_CORE) && $(MAKE) clean
	cd $(PIPELINE_AGENT) && $(MAKE) clean
	cd $(CONTROL_PLANE) && $(MAKE) clean
	cd $(WEB_UI) && $(MAKE) clean

clean-all: clean ## 전체 정리 (node_modules 포함)
	@echo "==> 전체 정리 중..."
	cd $(WEB_UI) && $(MAKE) clean-all
	go clean -cache -testcache -modcache

# ============================================================================
# 유틸리티
# ============================================================================

tools: ## 개발 도구 설치
	@echo "==> 개발 도구 설치 중..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/swaggo/swag/cmd/swag@latest
	go install github.com/vektra/mockery/v2@latest
	@echo "==> 개발 도구 설치 완료"

update-deps: ## 모든 의존성 업데이트
	@echo "==> 의존성 업데이트 중..."
	cd $(SHARED) && go get -u ./... && go mod tidy
	cd $(PIPELINE_CORE) && go get -u ./... && go mod tidy
	cd $(PIPELINE_AGENT) && go get -u ./... && go mod tidy
	cd $(CONTROL_PLANE) && go get -u ./... && go mod tidy
	cd $(WEB_UI) && npm update

gen-types: ## TypeScript 타입 생성
	cd $(WEB_UI) && $(MAKE) gen-types

# ============================================================================
# 도움말
# ============================================================================

help: ## 옵션 보기
	@echo "Conduix 빌드 시스템"
	@echo ""
	@echo "사용법: make [target]"
	@echo ""
	@echo "주요 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "각 모듈별 도움말:"
	@echo "  cd shared && make help"
	@echo "  cd pipeline-core && make help"
	@echo "  cd pipeline-agent && make help"
	@echo "  cd control-plane && make help"
	@echo "  cd web-ui && make help"
