# Conduix - Web UI Module Makefile
# 운영툴 프론트엔드 (React + TypeScript + Vite)

ifndef BUILD_TAG
    BUILD_TAG=$(shell git branch --show-current 2>/dev/null || echo "dev")
endif

BUILD_TIME=$(shell TZ=Asia/Seoul date '+%Y-%m-%dT%H:%M:%S%z')
GITHASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

MODULE_NAME=web-ui
DIST_DIR=dist
PACKAGE_TAR=$(MODULE_NAME)-$(BUILD_TAG).tar.gz
TARGET_DIR=target

# 패키지 매니저 (npm 또는 pnpm)
PKG_MANAGER?=npm

# API 서버 URL
VITE_API_URL?=http://localhost:8080

.PHONY: all build clean test lint fmt deps dev help package docker-build

all: build

## 의존성 설치
deps: ## 의존성 설치
	@echo "==> [$(MODULE_NAME)] 의존성 설치 중..."
	$(PKG_MANAGER) install

deps-clean: ## 의존성 재설치 (node_modules 삭제 후)
	@echo "==> [$(MODULE_NAME)] 의존성 재설치 중..."
	rm -rf node_modules
	$(PKG_MANAGER) install

## 빌드
build: ## 프로덕션 빌드
	@echo "==> [$(MODULE_NAME)] 빌드 중..."
	@echo "    Tag: $(BUILD_TAG)"
	@echo "    Time: $(BUILD_TIME)"
	@echo "    API URL: $(VITE_API_URL)"
	VITE_API_URL=$(VITE_API_URL) VITE_BUILD_TAG=$(BUILD_TAG) VITE_BUILD_TIME=$(BUILD_TIME) \
		$(PKG_MANAGER) run build
	@echo "==> [$(MODULE_NAME)] 빌드 완료: $(DIST_DIR)/"

build-analyze: ## 번들 분석과 함께 빌드
	@echo "==> [$(MODULE_NAME)] 번들 분석 빌드 중..."
	$(PKG_MANAGER) run build -- --analyze

## 개발 모드
dev: ## 개발 서버 실행
	@echo "==> [$(MODULE_NAME)] 개발 서버 실행 중..."
	@echo "    API URL: $(VITE_API_URL)"
	VITE_API_URL=$(VITE_API_URL) $(PKG_MANAGER) run dev

dev-host: ## 개발 서버 실행 (네트워크 접근 허용)
	VITE_API_URL=$(VITE_API_URL) $(PKG_MANAGER) run dev -- --host

preview: ## 프로덕션 빌드 미리보기
	$(PKG_MANAGER) run preview

## 테스트
test: ## 테스트 실행
	@echo "==> [$(MODULE_NAME)] 테스트 실행 중..."
	$(PKG_MANAGER) run test

test-watch: ## 테스트 워치 모드
	$(PKG_MANAGER) run test -- --watch

test-coverage: ## 커버리지 리포트 생성
	@echo "==> [$(MODULE_NAME)] 커버리지 리포트 생성 중..."
	$(PKG_MANAGER) run test -- --coverage

test-ui: ## Vitest UI로 테스트
	$(PKG_MANAGER) run test -- --ui

## E2E 테스트
e2e: ## E2E 테스트 실행 (Playwright/Cypress)
	@echo "==> [$(MODULE_NAME)] E2E 테스트 실행 중..."
	$(PKG_MANAGER) run e2e || echo "E2E test script not configured"

## 코드 품질
lint: ## ESLint 실행
	@echo "==> [$(MODULE_NAME)] 린트 실행 중..."
	$(PKG_MANAGER) run lint

lint-fix: ## ESLint 자동 수정
	$(PKG_MANAGER) run lint -- --fix

fmt: ## Prettier 포맷팅
	@echo "==> [$(MODULE_NAME)] 포맷팅 중..."
	$(PKG_MANAGER) run format || npx prettier --write "src/**/*.{ts,tsx,css,json}"

type-check: ## TypeScript 타입 체크
	@echo "==> [$(MODULE_NAME)] 타입 체크 중..."
	$(PKG_MANAGER) run type-check || npx tsc --noEmit

check: lint type-check test ## 전체 체크 (lint + type-check + test)

## 코드 생성
gen-types: ## API 타입 생성 (OpenAPI)
	@echo "==> [$(MODULE_NAME)] API 타입 생성 중..."
	@if [ -f ../shared/proto/openapi.yaml ]; then \
		npx openapi-typescript ../shared/proto/openapi.yaml -o src/types/api.ts; \
	else \
		echo "OpenAPI spec not found"; \
	fi

gen-icons: ## 아이콘 컴포넌트 생성
	@echo "==> [$(MODULE_NAME)] 아이콘 생성 중..."
	$(PKG_MANAGER) run gen:icons || echo "Icon generation script not configured"

## 패키지
package: build ## 배포 패키지 생성
	@echo "==> [$(MODULE_NAME)] 패키지 생성 중..."
	mkdir -p $(TARGET_DIR)
	tar -zcvf $(TARGET_DIR)/$(PACKAGE_TAR) $(DIST_DIR)
	@echo "==> 패키지 생성 완료: $(TARGET_DIR)/$(PACKAGE_TAR)"

## Docker
docker-build: build ## Docker 이미지용 빌드
	@echo "==> [$(MODULE_NAME)] Docker 빌드 완료"

## Storybook
storybook: ## Storybook 실행
	$(PKG_MANAGER) run storybook || echo "Storybook not configured"

storybook-build: ## Storybook 빌드
	$(PKG_MANAGER) run build-storybook || echo "Storybook not configured"

## 정리
clean: ## 빌드 아티팩트 정리
	@echo "==> [$(MODULE_NAME)] 정리 중..."
	rm -rf $(DIST_DIR)
	rm -rf $(TARGET_DIR)
	rm -rf coverage
	rm -rf .turbo

clean-all: clean ## 전체 정리 (node_modules 포함)
	rm -rf node_modules
	rm -rf .vite

## 업데이트
update: ## 의존성 업데이트 체크
	@echo "==> [$(MODULE_NAME)] 업데이트 가능한 패키지:"
	$(PKG_MANAGER) outdated || true

update-deps: ## 의존성 업데이트
	$(PKG_MANAGER) update

## 보안
audit: ## 보안 취약점 검사
	@echo "==> [$(MODULE_NAME)] 보안 검사 중..."
	$(PKG_MANAGER) audit

audit-fix: ## 보안 취약점 자동 수정
	$(PKG_MANAGER) audit fix

## 도움말
help: ## 옵션 보기
	@echo "$(MODULE_NAME) 모듈 명령어:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
