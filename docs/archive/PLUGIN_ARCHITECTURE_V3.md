# Plugin Architecture V3 - Go Native Build System

## 개요

플러그인 Stage를 Go 네이티브 바이너리로 빌드하여 Pipeline Runner에서 gRPC로 실행하는 아키텍처.

### V2 → V3 변경 이유
- V2 (Docker 컨테이너): Stage 하나에 컨테이너 하나 → 과도한 오버헤드
- Wasm: 외부 모듈 import 불가 → 각 회사별 커스텀 모듈 사용 불가
- Lua/스크립트: 네이티브 대비 10~100배 느림 → 플랫폼 사용 이유 없음
- **V3 (Go 네이티브)**: 외부 모듈 자유 + 네이티브 성능 + 프로세스 격리

## 아키텍처

```
┌─ Web UI ──────────────────────────────────────────┐
│  Monaco Editor (Go 자동완성)                         │
│  go.mod 편집기 (외부 모듈 추가)                       │
│  빌드 로그 실시간 스트리밍 (SSE)                       │
│  "빌드 & 등록" 버튼                                   │
└──────────┬────────────────────────────────────────┘
           │ POST /api/v1/plugins/build
           │ { name, version, files: {"main.go":"...", "go.mod":"..."} }
           ▼
┌─ Control Plane ──────────────────────────────────┐
│  1. 소스 코드 검증 (금지 패키지, 크기 제한)            │
│  2. Builder Service 호출                            │
│  3. 빌드 결과 수신 (바이너리 + 메타데이터)              │
│  4. 바이너리 저장 (MySQL BLOB 또는 OCI Registry)     │
│  5. plugin + plugin_stages 레코드 생성               │
└──────────┬────────────────────────────────────────┘
           │
           ▼
┌─ Builder Service (별도 Pod) ──────────────────────┐
│  1. 임시 디렉토리에 소스 파일 + go.mod 생성            │
│  2. go mod download (모듈 캐시 /go/pkg/mod 활용)    │
│  3. CGO_ENABLED=0 go build -o plugin.bin           │
│  4. 바이너리 + 빌드 로그 반환                          │
│                                                    │
│  보안:                                              │
│  - 빌드당 타임아웃 60초                               │
│  - 리소스 제한 (CPU 1core, Memory 2GB)              │
│  - 네트워크: Go 모듈 프록시만 허용 (GOPROXY)           │
│  - 금지 패키지: os/exec, syscall, unsafe 등          │
│  - 소스 코드 크기 제한: 1MB                           │
└──────────────────────────────────────────────────┘
           │
           ▼ 바이너리 (수 MB)
┌─ Storage ────────────────────────────────────────┐
│  기본: MySQL BLOB (plugin_binaries 테이블)          │
│  옵션: OCI Registry (oras-go로 push/pull)          │
└──────────────────────────────────────────────────┘
           │
           ▼ Pipeline 실행 시
┌─ Pipeline Runner ────────────────────────────────┐
│  Built-in Stage: Go 네이티브 (filter, remap...)    │
│  Plugin Stage:                                    │
│    1. 저장소에서 바이너리 다운로드 (로컬 캐시)          │
│    2. 바이너리를 실행 파일로 저장 + 실행 권한           │
│    3. HashiCorp go-plugin으로 서브프로세스 실행       │
│    4. gRPC over unix socket 통신                   │
│    ┌──────────────────────────────────┐           │
│    │ Plugin 프로세스 (별도 PID)         │           │
│    │ ProcessBatch([]Record) → []Record │          │
│    │ 배치 1000건 단위 처리              │           │
│    │ 크래시 시 자동 재시작               │           │
│    └──────────────────────────────────┘           │
└──────────────────────────────────────────────────┘
```

## Phase 구성

### Phase 1: Plugin SDK + Interface 정의
- `conduix-plugin-sdk` Go 모듈 생성
- Stage interface 정의 (gRPC protobuf)
- HashiCorp go-plugin 연동 boilerplate

### Phase 2: Builder Service
- Go 빌드 서비스 (별도 바이너리/Pod)
- 소스 코드 → 바이너리 빌드 파이프라인
- 보안: 금지 패키지 검사, 타임아웃, 리소스 제한
- 빌드 로그 스트리밍 (SSE)

### Phase 3: 바이너리 저장소
- MySQL BLOB 저장소 구현
- OCI Registry 저장소 구현 (oras-go)
- StorageBackend 인터페이스 + 설정 기반 전환

### Phase 4: Pipeline Runner 연동
- go-plugin 기반 Plugin 로더
- gRPC 서비스 구현 (ProcessBatch)
- 바이너리 캐싱, 프로세스 라이프사이클 관리

### Phase 5: Control Plane API
- POST /api/v1/plugins/build (빌드 요청)
- GET /api/v1/plugins/build/:id/logs (빌드 로그 SSE)
- GET /api/v1/plugins/:name/binary (바이너리 다운로드)
- 기존 CRUD API 확장

### Phase 6: Web UI
- Monaco Editor (Go 언어 지원)
- go.mod 편집기
- 빌드 로그 실시간 표시
- Plugin 관리 페이지 개선

## 데이터 모델

### plugin_binaries 테이블 (MySQL BLOB 저장)
```sql
CREATE TABLE plugin_binaries (
    id          VARCHAR(36) PRIMARY KEY,
    plugin_id   VARCHAR(36) NOT NULL,
    version     VARCHAR(50) NOT NULL,
    platform    VARCHAR(20) NOT NULL DEFAULT 'linux/arm64',  -- GOOS/GOARCH
    binary_data LONGBLOB NOT NULL,                           -- 빌드된 바이너리
    checksum    VARCHAR(64) NOT NULL,                        -- SHA256
    size_bytes  BIGINT NOT NULL,
    created_at  DATETIME(3),
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE,
    UNIQUE KEY idx_plugin_version_platform (plugin_id, version, platform)
);
```

### plugin_builds 테이블 (빌드 이력)
```sql
CREATE TABLE plugin_builds (
    id          VARCHAR(36) PRIMARY KEY,
    plugin_id   VARCHAR(36),
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending, building, success, failed
    source_code TEXT NOT NULL,                            -- main.go 소스
    go_mod      TEXT,                                    -- go.mod 내용
    build_log   TEXT,                                    -- 빌드 출력 로그
    error       TEXT,                                    -- 에러 메시지
    duration_ms INT,                                     -- 빌드 소요 시간
    created_at  DATETIME(3),
    updated_at  DATETIME(3)
);
```

## Plugin SDK

사용자가 작성하는 플러그인 코드 예시:

```go
package main

import (
    "github.com/conduix/conduix/plugin-sdk"
    "github.com/mycompany/internal-module"  // 회사 내부 모듈 OK
)

type MyTransform struct{}

func (t *MyTransform) Init(config map[string]any) error {
    return nil
}

func (t *MyTransform) ProcessBatch(records []sdk.Record) ([]sdk.Record, error) {
    for i, r := range records {
        // 회사 내부 모듈 사용
        enriched := internal.Enrich(r.Data)
        records[i].Data = enriched
    }
    return records, nil
}

func (t *MyTransform) Close() error {
    return nil
}

func main() {
    sdk.Serve(&MyTransform{})  // go-plugin 서버 시작
}
```

## gRPC Interface

```protobuf
syntax = "proto3";
package conduix.plugin.v1;

service StagePlugin {
    rpc Init(InitRequest) returns (InitResponse);
    rpc ProcessBatch(ProcessBatchRequest) returns (ProcessBatchResponse);
    rpc Close(CloseRequest) returns (CloseResponse);
}

message Record {
    map<string, bytes> data = 1;  // 필드명 → JSON 값
    map<string, string> metadata = 2;
}

message InitRequest {
    map<string, string> config = 1;
}

message InitResponse {
    string error = 1;
}

message ProcessBatchRequest {
    repeated Record records = 1;
}

message ProcessBatchResponse {
    repeated Record records = 1;
    string error = 2;
}

message CloseRequest {}
message CloseResponse {
    string error = 1;
}
```

## 성능 목표

| 시나리오 | 목표 |
|---------|------|
| 빌드 시간 (캐시 있음) | < 5초 |
| 빌드 시간 (캐시 없음) | < 30초 |
| 배치 처리 (1000건) | < 1ms (gRPC 오버헤드 포함) |
| 초당 처리량 | > 100만 건 |
| 바이너리 크기 | < 50MB |
| Plugin 시작 시간 | < 500ms |

## 보안

### 빌드 시 보안
- 금지 패키지 목록: `os/exec`, `syscall`, `unsafe`, `plugin`, `cgo`
- 소스코드 정적 분석 (AST 파싱으로 import 검사)
- 빌드 타임아웃: 60초
- 빌드 컨테이너 리소스 제한: CPU 1 core, Memory 2GB
- GOPROXY를 통한 모듈 다운로드만 허용

### 실행 시 보안
- 별도 프로세스 격리 (PID, 메모리 분리)
- go-plugin의 프로토콜 핸드셰이크 (magic cookie)
- 실행 타임아웃 (레코드 배치당 10초)
- 크래시 시 자동 재시작 + 재시작 횟수 제한

## 설정

```yaml
# control-plane config
plugin:
  storage:
    type: mysql          # mysql 또는 oci
    oci:
      registry: ghcr.io/conduix/plugins
      # credentials: from k8s imagePullSecret
  build:
    timeout: 60s
    max_source_size: 1MB
    blocked_imports:
      - os/exec
      - syscall
      - unsafe
      - plugin
    goproxy: https://proxy.golang.org,direct
```
