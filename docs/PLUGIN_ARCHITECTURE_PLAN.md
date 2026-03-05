# 파이프라인 플랫폼 확장성 개선 계획

## 문제 정의

### 현재 상황
```
Input/Stage/Output 추가 → Go 코드 수정 → 빌드 → 이미지 생성 → K8s 배포
```

### 구체적 문제점

1. **하드코딩된 컴포넌트**
   - `sink.go`, `source.go`의 switch-case 구조
   - 새 타입 추가 시 코어 코드 수정 필요
   - 컴파일 타임에만 확정되는 타입 시스템

2. **무중단 변경 불가**
   - 기능 추가/수정/삭제에 빌드+배포 필수
   - 테스트도 전체 파이프라인 재시작 필요
   - 실험적 기능 적용이 리스크

3. **높은 진입장벽**
   - Go 개발 능력 필요 (stage 추가)
   - React/TypeScript 필요 (UI 커스터마이징)
   - 전체 빌드/테스트/배포 파이프라인 이해 필요

4. **미래 확장성 우려**
   - 새로운 DB 연동 (DuckDB, QuestDB, TimescaleDB, ...)
   - 새로운 프로토콜 (gRPC streaming, WebSocket, GraphQL subscription)
   - 조직별 커스텀 변환 로직

---

## 해결 방안 비교 분석

### 1. Go Plugin System (.so 파일)

```go
// plugin/mysql_sink.so
type MySQLSink struct { ... }
func (s *MySQLSink) Process(records []Record) error { ... }

// 런타임 로드
p, _ := plugin.Open("plugins/mysql_sink.so")
sym, _ := p.Lookup("NewSink")
```

**장점:**
- Native Go 성능 (오버헤드 거의 없음)
- 타입 안전성 유지
- 기존 코드 구조와 호환

**단점:**
- **CGO 필요**: Linux에서만 동작, macOS/Windows 지원 어려움
- **빌드 복잡성**: 플러그인도 동일 Go 버전으로 빌드 필요
- **배포 복잡성**: .so 파일 버전 관리 필요
- 무중단 교체 여전히 어려움

**사례:** Terraform Providers (초기), Kong Gateway

**결론:** ❌ 실질적 이점 부족, 크로스 플랫폼 문제

---

### 2. Embedded Scripting (Lua/JavaScript)

```yaml
stages:
  - name: custom_transform
    type: lua
    script: |
      function process(record)
        record.processed_at = os.time()
        if record.status == "active" then
          return record
        end
        return nil -- filter out
      end
```

**장점:**
- **무중단 변경**: 스크립트 리로드만으로 적용
- **낮은 진입장벽**: Lua/JS는 상대적으로 쉬움
- **즉시 테스트**: 설정 변경 → 즉시 실행

**단점:**
- **성능 저하**: 10-100x 느림 (단순 로직 기준)
- **복잡한 로직 제한**: DB 연동, 네트워크 호출 등 제한적
- **디버깅 어려움**: 스크립트 에러 추적 복잡
- **생태계 제한**: 외부 라이브러리 사용 어려움

**사례:**
- Nginx + Lua (OpenResty)
- Redis Lua Scripting
- Vector (VRL - Vector Remap Language)

**Go 구현:**
```go
import "github.com/yuin/gopher-lua"

func runLuaScript(script string, record map[string]any) (map[string]any, error) {
    L := lua.NewState()
    defer L.Close()
    // record를 Lua 테이블로 변환
    // 스크립트 실행
    // 결과 반환
}
```

**결론:** ⚠️ 단순 변환에는 적합, 복잡한 Input/Output에는 부적합

---

### 3. External Process (Sidecar/Subprocess)

```
┌─────────────┐    stdin/stdout    ┌─────────────┐
│  Pipeline   │ ◄────────────────► │   Python    │
│  Engine     │    JSON Lines      │   Plugin    │
└─────────────┘                    └─────────────┘
```

```python
# python_plugin.py
import sys, json

for line in sys.stdin:
    record = json.loads(line)
    # 처리 로직
    record['enriched'] = True
    print(json.dumps(record))
```

**장점:**
- **언어 무관**: Python, Node.js, Ruby 등 자유
- **완전한 생태계 접근**: pip, npm 패키지 사용 가능
- **격리**: 플러그인 크래시가 엔진 영향 안 함
- **Hot reload**: 프로세스 재시작으로 변경 적용

**단점:**
- **IPC 오버헤드**: JSON 직렬화/역직렬화 비용
- **프로세스 관리**: 자식 프로세스 생명주기 관리 필요
- **메모리 사용량**: 언어 런타임 별도 로드
- **레이턴시**: 프로세스 간 통신 지연

**성능 영향:**
- 단순 레코드 처리: ~5-20x 느림
- 배치 처리로 완화 가능: 1000건 단위 처리 시 오버헤드 분산

**사례:**
- Telegraf exec input/output
- Logstash (Ruby-based, JRuby)
- FluentBit external plugins

**결론:** ⚠️ 유연하지만 성능 트레이드오프 명확

---

### 4. gRPC/HTTP Plugin Protocol

```protobuf
// plugin.proto
service Plugin {
    rpc GetSchema(Empty) returns (Schema);
    rpc Process(RecordBatch) returns (RecordBatch);
    rpc Healthcheck(Empty) returns (HealthStatus);
}

message RecordBatch {
    repeated Record records = 1;
}
```

```
┌─────────────┐     gRPC      ┌─────────────┐
│  Pipeline   │ ◄───────────► │   Plugin    │
│  Engine     │   (binary)    │   Server    │
└─────────────┘               └─────────────┘
                                    │
                              ┌─────┴─────┐
                              │  Python   │
                              │  Go/Rust  │
                              │  Any Lang │
                              └───────────┘
```

**장점:**
- **언어 무관**: gRPC SDK 있는 모든 언어
- **네트워크 분리**: 다른 머신에서도 실행 가능
- **명확한 계약**: protobuf 스키마로 인터페이스 정의
- **효율적 직렬화**: JSON 대비 ~10x 빠른 protobuf
- **양방향 스트리밍**: 대용량 데이터 처리 효율적

**단점:**
- **네트워크 오버헤드**: localhost여도 소켓 통신 비용
- **운영 복잡성**: 플러그인 서버 배포/관리 필요
- **연결 관리**: 재연결, 타임아웃, 회로 차단기 필요

**성능 영향:**
- 단순 처리: ~2-5x 느림
- 배치 + 스트리밍: 거의 네이티브에 근접

**사례:**
- HashiCorp go-plugin (Terraform, Vault, Nomad)
- Telegraf execd (gRPC mode)
- Vector (내부적으로 이와 유사)

**결론:** ✅ 성능과 유연성 균형이 좋음

---

### 5. WebAssembly (WASM) Runtime

```rust
// plugin.rs -> plugin.wasm
#[no_mangle]
pub fn process(input: &[u8]) -> Vec<u8> {
    let records: Vec<Record> = deserialize(input);
    // 처리 로직
    serialize(&processed)
}
```

```go
// Go에서 WASM 실행
import "github.com/tetratelabs/wazero"

func runWasmPlugin(wasmBytes []byte, input []byte) ([]byte, error) {
    r := wazero.NewRuntime(ctx)
    mod, _ := r.Instantiate(ctx, wasmBytes)
    result, _ := mod.ExportedFunction("process").Call(ctx, ...)
    return result, nil
}
```

**장점:**
- **Near-native 성능**: JIT 컴파일로 ~80% 네이티브 속도
- **다언어 지원**: Rust, C, Go, AssemblyScript 등
- **강력한 샌드박스**: 메모리/CPU 제한, 시스템 콜 차단
- **이식성**: 같은 .wasm이 어디서든 실행

**단점:**
- **생태계 미성숙**: Go에서 WASM 런타임 안정성 개선 중
- **제한된 I/O**: 파일, 네트워크 직접 접근 어려움
- **개발 경험**: Rust/C 필요, 디버깅 어려움
- **사이즈 제한**: 현재 메모리 4GB 제한 (64비트 진행 중)

**Go WASM 런타임:**
- `wazero`: Pure Go, CGO 불필요, 활발한 개발
- `wasmtime-go`: CGO 필요, 성능 최고

**사례:**
- Envoy Proxy WASM filters
- Fermyon Spin
- Shopify Functions

**결론:** ✅ 미래 지향적, 현재는 보조 옵션

---

## 추천 하이브리드 아키텍처

### 설계 원칙

1. **계층화된 확장**: 빌트인 → gRPC 플러그인 → WASM
2. **점진적 도입**: 기존 코드 유지하며 플러그인 인터페이스 추가
3. **성능 최적화**: 핫 패스는 네이티브, 커스텀은 플러그인

### 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                     Pipeline Engine (Go)                         │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                 Plugin Manager                             │  │
│  │  - Registry: 플러그인 등록/조회                            │  │
│  │  - Loader: 플러그인 로드/언로드                            │  │
│  │  - Health: 헬스체크, 재연결                                │  │
│  └───────────────────────────────────────────────────────────┘  │
│           │                    │                    │            │
│           ▼                    ▼                    ▼            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   Built-in      │  │   gRPC Plugin   │  │   WASM Runtime  │  │
│  │   Adapter       │  │   Adapter       │  │   Adapter       │  │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘  │
│           │                    │                    │            │
│           ▼                    ▼                    ▼            │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │              Unified Plugin Interface                        ││
│  │                                                              ││
│  │  type Plugin interface {                                     ││
│  │      Name() string                                           ││
│  │      Type() PluginType  // input, stage, output              ││
│  │      Schema() *PluginSchema                                  ││
│  │      Init(config map[string]any) error                       ││
│  │      Process(ctx context.Context, batch []Record) ([]Record, error) ││
│  │      Close() error                                           ││
│  │  }                                                           ││
│  │                                                              ││
│  │  type PluginSchema struct {                                  ││
│  │      ConfigSchema  JSONSchema  // 설정 스키마                ││
│  │      InputSchema   JSONSchema  // 입력 레코드 스키마         ││
│  │      OutputSchema  JSONSchema  // 출력 레코드 스키마         ││
│  │  }                                                           ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

### 플러그인 설정 예시

```yaml
# pipeline.yaml
pipeline:
  input:
    type: kafka                    # 빌트인
    config:
      brokers: ["localhost:9092"]

  stages:
    - name: filter_active
      type: filter                 # 빌트인
      config:
        condition: ".status == 'active'"

    - name: custom_enrich
      type: grpc                   # gRPC 플러그인
      plugin:
        address: localhost:50051   # 또는 plugin-sidecar:50051
        name: my-enricher
      config:
        api_key: "${API_KEY}"

    - name: fast_transform
      type: wasm                   # WASM 플러그인
      plugin:
        path: /plugins/transform.wasm
        # 또는 registry: registry.conduix.io/plugins/transform:1.0
      config:
        mode: fast

  output:
    - type: elasticsearch          # 빌트인
      config:
        addresses: ["http://es:9200"]

    - type: grpc                   # gRPC 플러그인 (외부 DB)
      plugin:
        address: localhost:50052
        name: custom-db-sink
      config:
        connection_string: "..."
```

### gRPC 플러그인 프로토콜

```protobuf
// conduix/plugin/v1/plugin.proto

syntax = "proto3";
package conduix.plugin.v1;

service PluginService {
    // 메타데이터
    rpc GetInfo(GetInfoRequest) returns (GetInfoResponse);
    rpc GetSchema(GetSchemaRequest) returns (GetSchemaResponse);

    // 생명주기
    rpc Init(InitRequest) returns (InitResponse);
    rpc Close(CloseRequest) returns (CloseResponse);

    // 처리 (배치)
    rpc Process(ProcessRequest) returns (ProcessResponse);

    // 처리 (스트리밍) - 대용량
    rpc ProcessStream(stream ProcessRequest) returns (stream ProcessResponse);

    // 헬스체크
    rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
}

message Record {
    bytes data = 1;        // JSON 또는 MessagePack
    string source = 2;
    int64 timestamp = 3;
    map<string, string> metadata = 4;
}

message ProcessRequest {
    repeated Record records = 1;
    map<string, bytes> context = 2;  // 파이프라인 컨텍스트
}

message ProcessResponse {
    repeated Record records = 1;
    repeated Error errors = 2;
    map<string, bytes> metrics = 3;
}
```

### Python 플러그인 SDK 예시

```python
# conduix_plugin/sdk.py
from abc import ABC, abstractmethod
from concurrent import futures
import grpc
from conduix.plugin.v1 import plugin_pb2, plugin_pb2_grpc

class PluginBase(ABC):
    @abstractmethod
    def name(self) -> str:
        pass

    @abstractmethod
    def plugin_type(self) -> str:  # "input", "stage", "output"
        pass

    @abstractmethod
    def schema(self) -> dict:
        pass

    @abstractmethod
    def init(self, config: dict) -> None:
        pass

    @abstractmethod
    def process(self, records: list[dict]) -> list[dict]:
        pass

    def close(self) -> None:
        pass

class PluginServicer(plugin_pb2_grpc.PluginServiceServicer):
    def __init__(self, plugin: PluginBase):
        self.plugin = plugin

    def Process(self, request, context):
        records = [json.loads(r.data) for r in request.records]
        results = self.plugin.process(records)
        return plugin_pb2.ProcessResponse(
            records=[plugin_pb2.Record(data=json.dumps(r).encode())
                     for r in results]
        )

def serve(plugin: PluginBase, port: int = 50051):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    plugin_pb2_grpc.add_PluginServiceServicer_to_server(
        PluginServicer(plugin), server
    )
    server.add_insecure_port(f'[::]:{port}')
    server.start()
    server.wait_for_termination()
```

```python
# my_enricher_plugin.py
from conduix_plugin import PluginBase, serve
import httpx

class MyEnricherPlugin(PluginBase):
    def name(self) -> str:
        return "my-enricher"

    def plugin_type(self) -> str:
        return "stage"

    def schema(self) -> dict:
        return {
            "config": {
                "type": "object",
                "properties": {
                    "api_key": {"type": "string"},
                    "api_url": {"type": "string", "default": "https://api.example.com"}
                },
                "required": ["api_key"]
            }
        }

    def init(self, config: dict) -> None:
        self.client = httpx.Client(
            base_url=config.get("api_url"),
            headers={"Authorization": f"Bearer {config['api_key']}"}
        )

    def process(self, records: list[dict]) -> list[dict]:
        # 배치로 외부 API 호출
        ids = [r.get("user_id") for r in records]
        response = self.client.post("/batch-lookup", json={"ids": ids})
        enrichments = {e["id"]: e for e in response.json()["data"]}

        for record in records:
            if record.get("user_id") in enrichments:
                record["user_info"] = enrichments[record["user_id"]]

        return records

    def close(self) -> None:
        self.client.close()

if __name__ == "__main__":
    serve(MyEnricherPlugin(), port=50051)
```

---

## 구현 로드맵

### Phase 1: 플러그인 인터페이스 정의 (2주)

**목표:** 통합 플러그인 인터페이스 설계 및 프로토버프 정의

**작업:**
1. `Plugin` 인터페이스 정의 (`pipeline-core/pkg/plugin/interface.go`)
2. gRPC protobuf 스키마 정의 (`proto/plugin/v1/plugin.proto`)
3. 플러그인 매니저 기본 구조 (`pipeline-core/pkg/plugin/manager.go`)
4. 레지스트리 인터페이스 정의

**결과물:**
- `shared/types/plugin.go` - 공통 타입
- `pipeline-core/pkg/plugin/` - 플러그인 코어
- `proto/plugin/v1/` - gRPC 정의

### Phase 2: 빌트인 어댑터 (1주)

**목표:** 기존 Input/Stage/Output을 플러그인 인터페이스로 래핑

**작업:**
1. `BuiltinAdapter` 구현
2. 기존 `sink.go`, `source.go`의 switch-case를 레지스트리 패턴으로 변경
3. 하위 호환성 유지

**결과물:**
- `pipeline-core/pkg/plugin/builtin/` - 빌트인 어댑터
- 기존 코드 리팩토링

### Phase 3: gRPC 플러그인 어댑터 (2주)

**목표:** gRPC 기반 외부 플러그인 지원

**작업:**
1. gRPC 클라이언트 어댑터 구현
2. 연결 풀링, 재연결, 회로 차단기
3. 배치 처리 최적화
4. 헬스체크 및 메트릭

**결과물:**
- `pipeline-core/pkg/plugin/grpc/` - gRPC 어댑터
- 연결 관리, 에러 처리

### Phase 4: Python SDK (2주)

**목표:** Python으로 플러그인 개발 가능하게

**작업:**
1. Python gRPC SDK 생성 (protoc)
2. `conduix-plugin` PyPI 패키지
3. 예제 플러그인 (enricher, custom DB)
4. 로컬 테스트 도구

**결과물:**
- `sdk/python/` - Python SDK
- `examples/python-plugins/` - 예제
- PyPI 패키지 배포

### Phase 5: 플러그인 관리 UI (1주)

**목표:** Web UI에서 플러그인 관리

**작업:**
1. 플러그인 목록 페이지
2. 플러그인 상태 모니터링
3. 플러그인 설정 폼 (JSON Schema 기반)

**결과물:**
- `web-ui/src/pages/Plugins.tsx`
- 플러그인 관리 API

### Phase 6: WASM 런타임 (선택, 2-3주)

**목표:** WASM 플러그인 지원 (실험적)

**작업:**
1. wazero 통합
2. WASM 플러그인 로더
3. Rust/AssemblyScript SDK

**결과물:**
- `pipeline-core/pkg/plugin/wasm/`
- 예제 WASM 플러그인

---

## 배포 아키텍처

### Sidecar 패턴 (권장)

```yaml
# K8s Deployment
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: pipeline-agent
          image: conduix/agent:latest

        - name: python-plugins
          image: conduix/python-plugins:latest
          ports:
            - containerPort: 50051
          env:
            - name: PLUGINS
              value: "my-enricher,custom-db-sink"
```

### Standalone 패턴 (대규모)

```yaml
# 별도 서비스로 배포
apiVersion: v1
kind: Service
metadata:
  name: enricher-plugin
spec:
  ports:
    - port: 50051
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: enricher-plugin
spec:
  replicas: 3  # 스케일 아웃 가능
```

---

## 성능 고려사항

### 예상 오버헤드

| 구분 | 빌트인 | gRPC 플러그인 | WASM |
|------|--------|---------------|------|
| 레이턴시 (per record) | ~1µs | ~50-100µs | ~5-10µs |
| 배치 처리 (1000 records) | ~1ms | ~10-50ms | ~5-10ms |
| 메모리 | 없음 | 연결당 ~1MB | 인스턴스당 ~10MB |

### 최적화 전략

1. **배치 크기 튜닝**: gRPC는 1000-10000 단위 배치가 효율적
2. **연결 풀링**: gRPC 연결 재사용
3. **비동기 처리**: 병렬 플러그인 호출
4. **캐싱**: 플러그인 결과 캐싱 (enrichment 등)

---

## 결론

### 즉시 적용 가능한 개선

1. **gRPC 플러그인 시스템**: 가장 현실적인 솔루션
   - 언어 무관 (Python 우선)
   - 성능과 유연성 균형
   - 검증된 패턴 (Terraform, Telegraf)

2. **점진적 마이그레이션**: 기존 코드 유지하면서 플러그인 인터페이스 추가

### 미래 고려사항

1. **WASM**: 생태계 성숙 시 고려 (1-2년 후)
2. **Embedded Lua**: 단순 변환에 선택적 사용
3. **플러그인 마켓플레이스**: 커뮤니티 플러그인 공유

### 예상 일정

- Phase 1-4: 7주 (핵심 기능)
- Phase 5-6: 3-4주 (선택적)
- 총: 10-11주 (약 2.5개월)
