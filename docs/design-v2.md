# 데이터 파이프라인 v2 설계

## 1. 요구사항 정리

### 1.1 파이프라인 종류

| 종류 | 특징 | 상태 관리 |
|-----|------|----------|
| **배치 (Batch)** | 항상 최신 데이터로 전체 처리 | 상태 없음, 멱등성 보장 |
| **실시간 (Realtime)** | 이벤트 스트림 처리 | 중복 제거, Upsert 지원 |

### 1.2 실시간 파이프라인 특수 요구사항

1. **중복 이벤트 무시**: 이미 처리된 이벤트 ID는 스킵
2. **Upsert 로직**: UPDATE 이벤트인데 CREATE가 없으면 → CREATE로 처리

### 1.3 데이터 소스

| 소스 | 배치 | 실시간 | 비고 |
|-----|------|-------|------|
| File | ✅ | ✅ | JSON, CSV, Line |
| SQL | ✅ | ✅ | MySQL, PostgreSQL |
| HTTP (단순) | ✅ | ✅ | GET/POST |
| HTTP (페이징) | ✅ | - | next_url 따라 반복 |
| HTTP (인증) | ✅ | ✅ | Basic, Bearer, OAuth2 |
| Kafka | - | ✅ | Consumer Group |

### 1.4 데이터 저장

- **Stub 처리**: 실제 저장소는 별도 플랫폼에서 구현
- 파이프라인은 변환된 데이터를 출력만 함

---

## 2. 설정 형식 (YAML)

```yaml
# 파이프라인 기본 정보
name: "user-sync-pipeline"
type: batch | realtime

# 데이터 소스
source:
  type: file | sql | http | kafka
  # ... 소스별 설정

# 실시간 전용 설정
realtime:
  id_field: "event_id"          # 중복 체크용 ID 필드
  event_type_field: "type"      # CREATE/UPDATE/DELETE 구분 필드
  dedup_storage: redis          # 중복 체크 저장소
  dedup_ttl: 24h                # 중복 ID 보관 기간

# 처리 단계
steps:
  - name: step1
    transform: "..."
  - name: step2
    filter: "..."

# 출력 (Stub)
output:
  type: stub
  log_level: info               # 출력 로깅 레벨
```

---

## 3. 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                     Data Pipeline v2                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                      Source Layer                        │    │
│  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐          │    │
│  │  │ File │ │ SQL  │ │ HTTP │ │HTTP+ │ │Kafka │          │    │
│  │  │      │ │      │ │      │ │Paging│ │      │          │    │
│  │  └──────┘ └──────┘ └──────┘ └──────┘ └──────┘          │    │
│  └────────────────────────┬────────────────────────────────┘    │
│                           │                                      │
│                           ▼                                      │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              Pipeline Core (Mode-based)                  │    │
│  │                                                          │    │
│  │   [Batch Mode]              [Realtime Mode]             │    │
│  │   - 전체 데이터 처리         - 중복 체크 (Dedup)          │    │
│  │   - 멱등성 보장              - Upsert 로직               │    │
│  │   - 상태 없음                - 이벤트 순서 처리           │    │
│  │                                                          │    │
│  └────────────────────────┬────────────────────────────────┘    │
│                           │                                      │
│                           ▼                                      │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    Process Steps                         │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐              │    │
│  │  │Transform │→ │  Filter  │→ │Transform │→ ...         │    │
│  │  └──────────┘  └──────────┘  └──────────┘              │    │
│  └────────────────────────┬────────────────────────────────┘    │
│                           │                                      │
│                           ▼                                      │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    Output (Stub)                         │    │
│  │  - 로그 출력                                             │    │
│  │  - 메트릭 수집                                           │    │
│  │  - 콜백 호출 (향후 실제 저장소 연동용)                    │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 데이터 소스 상세

### 4.1 File Source

```yaml
source:
  type: file
  path: "/data/users.json"       # 단일 파일
  paths:                         # 또는 여러 파일
    - "/data/*.json"
  format: json | csv | lines     # 파일 형식
  csv_header: true               # CSV 헤더 유무
```

### 4.2 SQL Source

```yaml
source:
  type: sql
  driver: mysql | postgres
  dsn: "user:pass@tcp(host:3306)/db"
  query: "SELECT * FROM users WHERE updated_at > ?"
  params:
    - "${LAST_SYNC_TIME}"
  # 배치용: 증분 쿼리
  incremental:
    column: "updated_at"
    state_key: "users_last_sync"
```

### 4.3 HTTP Source (단순)

```yaml
source:
  type: http
  url: "https://api.example.com/users"
  method: GET
  headers:
    Content-Type: "application/json"
```

### 4.4 HTTP Source (페이징)

```yaml
source:
  type: http
  url: "https://api.example.com/users"
  method: GET
  pagination:
    type: next_url              # next_url | offset | cursor
    next_field: "next_page_url" # 응답에서 다음 URL 필드
    data_field: "data"          # 실제 데이터 필드
    max_pages: 100              # 최대 페이지 (무한 루프 방지)
```

### 4.5 HTTP Source (인증)

```yaml
source:
  type: http
  url: "https://api.example.com/users"
  auth:
    type: basic | bearer | oauth2
    # Basic Auth
    username: "${HTTP_USER}"
    password: "${HTTP_PASS}"
    # Bearer Token
    token: "${API_TOKEN}"
    # OAuth2
    client_id: "${OAUTH_CLIENT_ID}"
    client_secret: "${OAUTH_CLIENT_SECRET}"
    token_url: "https://auth.example.com/token"
    scopes: ["read:users"]
```

### 4.6 Kafka Source

```yaml
source:
  type: kafka
  brokers:
    - "kafka:9092"
  topics:
    - "user-events"
  group_id: "pipeline-consumer"
  start_offset: earliest | latest
```

---

## 5. 실시간 파이프라인 로직

### 5.1 이벤트 구조

```json
{
  "event_id": "evt_12345",
  "event_type": "CREATE | UPDATE | DELETE",
  "entity_id": "user_001",
  "timestamp": "2025-01-01T00:00:00Z",
  "data": { ... }
}
```

### 5.2 중복 제거 (Deduplication)

```
이벤트 수신
    │
    ▼
┌─────────────────┐
│ event_id 확인   │
│ (Redis/Memory) │
└────────┬────────┘
         │
    ┌────┴────┐
    │ 중복?   │
    └────┬────┘
         │
    Yes ─┴─ No
     │      │
     ▼      ▼
   Skip   Process
           │
           ▼
      event_id 저장
      (TTL 적용)
```

### 5.3 Upsert 로직

```
이벤트 타입 확인
    │
    ▼
┌─────────────────┐
│ UPDATE 이벤트?  │
└────────┬────────┘
         │
    Yes ─┴─ No
     │      │
     ▼      ▼
┌─────────┐  정상 처리
│기존 데이터│
│  존재?   │
└────┬────┘
     │
Yes ─┴─ No
 │      │
 ▼      ▼
UPDATE  CREATE로
처리    변환 후 처리
```

---

## 6. Output (Stub)

```yaml
output:
  type: stub
  log_level: debug | info | warn | error
  log_format: json | text

  # 메트릭
  metrics:
    enabled: true
    prefix: "pipeline"

  # 콜백 (향후 확장용)
  callback:
    enabled: false
    url: "http://data-platform/ingest"
```

### Stub 동작

1. **로그 출력**: 처리된 데이터를 로그로 출력
2. **메트릭 수집**: 처리 건수, 에러 수, 지연시간 등
3. **콜백 호출**: (옵션) 외부 시스템에 알림

---

## 7. 처리 단계 (Steps)

```yaml
steps:
  # 변환
  - name: parse
    transform: |
      root = this
      root.full_name = this.first_name + " " + this.last_name

  # 필터링
  - name: active_only
    filter: '.status == "active"'

  # 샘플링
  - name: sample
    sample: 0.1

  # 필드 선택
  - name: select_fields
    select:
      - id
      - name
      - email

  # 필드 제외
  - name: remove_sensitive
    exclude:
      - password
      - ssn
```

---

## 8. 구현 순서

1. **설정 파서**: 새 YAML 형식 파싱
2. **소스 구현**: File → SQL → HTTP → Kafka
3. **파이프라인 코어**: Batch/Realtime 모드 분기
4. **중복 제거**: Redis 기반 dedup
5. **Upsert 로직**: 이벤트 타입별 처리
6. **Stub 출력**: 로그 + 메트릭
7. **테스트 및 문서화**
