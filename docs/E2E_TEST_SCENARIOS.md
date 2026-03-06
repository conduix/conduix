# E2E Test Scenarios

Plugin Architecture V2 전체 플로우 검증을 위한 E2E 테스트 시나리오입니다.

## 전제 조건

- Kubernetes 클러스터 (Colima 또는 Kind)
- MySQL 8.0 + Redis 7.0
- Control Plane, Pipeline Agent 실행 중
- `conduix/pipeline-runner:latest` 이미지 빌드됨

## Scenario 1: Plugin Lifecycle (플러그인 등록~삭제)

### 1.1 플러그인 등록

```bash
# POST /api/v1/plugins - 플러그인 등록 (Upsert)
curl -X POST http://localhost:8080/api/v1/plugins \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-plugin",
    "version": "1.0.0",
    "image": "conduix/test-plugin:1.0.0",
    "description": "E2E 테스트용 플러그인",
    "stages": [
      {
        "type": "test-transform",
        "displayName": "Test Transform",
        "category": "transform",
        "configSchema": {
          "type": "object",
          "properties": {
            "multiplier": { "type": "number", "default": 2 }
          },
          "required": ["multiplier"]
        }
      }
    ]
  }'
```

**검증 포인트:**
- [ ] 200 OK 응답
- [ ] `plugins` 테이블에 레코드 생성
- [ ] `plugin_stages` 테이블에 Stage 레코드 생성

### 1.2 Stage 목록 조회

```bash
# GET /api/v1/stages - 빌트인 + 플러그인 통합 목록
curl http://localhost:8080/api/v1/stages
```

**검증 포인트:**
- [ ] `builtin` 배열에 filter, remap 등 포함
- [ ] `plugins` 배열에 `test-transform` 포함
- [ ] `pluginName`, `pluginImage` 필드 존재

### 1.3 Stage Schema 조회

```bash
# GET /api/v1/stages/test-transform/schema
curl http://localhost:8080/api/v1/stages/test-transform/schema
```

**검증 포인트:**
- [ ] `configSchema.properties.multiplier` 존재
- [ ] `pluginImage` = `conduix/test-plugin:1.0.0`

### 1.4 플러그인 업데이트 (Upsert)

```bash
# 같은 name으로 POST → 업데이트
curl -X POST http://localhost:8080/api/v1/plugins \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-plugin",
    "version": "1.1.0",
    "image": "conduix/test-plugin:1.1.0",
    "stages": [
      {
        "type": "test-transform",
        "displayName": "Test Transform v2",
        "category": "transform",
        "configSchema": {
          "type": "object",
          "properties": {
            "multiplier": { "type": "number", "default": 3 },
            "mode": { "type": "string", "enum": ["fast", "slow"] }
          }
        }
      }
    ]
  }'
```

**검증 포인트:**
- [ ] version이 1.1.0으로 업데이트
- [ ] Stage schema에 `mode` 필드 추가됨

### 1.5 플러그인 삭제

```bash
curl -X DELETE http://localhost:8080/api/v1/plugins/test-plugin
```

**검증 포인트:**
- [ ] 200 OK
- [ ] `plugin_stages` 연관 레코드도 CASCADE 삭제
- [ ] Stage 목록에서 `test-transform` 제거됨

---

## Scenario 2: Batch Pipeline Execution (K8s Job)

### 전제
- Agent가 K8s 클러스터에 연결됨
- Pipeline Runner 이미지가 클러스터에서 접근 가능

### 2.1 워크플로우 생성 (배치 모드)

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "name": "e2e-batch-test",
    "type": "batch",
    "project_id": "<project_id>",
    "pipelines": [{
      "id": "batch-pipeline",
      "name": "Batch Pipeline",
      "input": {
        "type": "sql",
        "config": {
          "connection_string": "postgres://...",
          "query": "SELECT * FROM test_data LIMIT 100"
        }
      },
      "stages": [
        { "type": "filter", "config": { "condition": ".status == \"active\"" } }
      ],
      "outputs": [{
        "name": "es-output",
        "type": "elasticsearch",
        "config": {
          "addresses": ["http://elasticsearch:9200"],
          "index": "e2e-test"
        }
      }]
    }]
  }'
```

### 2.2 워크플로우 실행

```bash
curl -X POST http://localhost:8080/api/v1/workflows/<id>/execute
```

**검증 포인트:**
- [ ] Agent가 K8s Job 생성
- [ ] Job Pod가 Pipeline Runner 이미지 사용
- [ ] `pipeline_runs` 테이블에 status=running 레코드
- [ ] Job 완료 후 status=completed
- [ ] Elasticsearch에 데이터 인덱싱됨

### 2.3 Job 상태 확인

```bash
kubectl get jobs -n conduix -l conduix.io/workflow-id=<workflow_id>
kubectl logs job/<job-name> -n conduix
```

---

## Scenario 3: Streaming Pipeline (K8s Deployment)

### 3.1 스트리밍 워크플로우 생성

```bash
curl -X POST http://localhost:8080/api/v1/workflows \
  -d '{
    "name": "e2e-streaming-test",
    "type": "realtime",
    "pipelines": [{
      "id": "stream-pipeline",
      "input": {
        "type": "kafka",
        "config": {
          "brokers": ["kafka:9092"],
          "topics": ["test-events"],
          "group_id": "e2e-consumer"
        }
      },
      "stages": [
        { "type": "remap", "config": { "mappings": { "ts": ".created_at" } } }
      ],
      "outputs": [{
        "name": "es-output",
        "type": "elasticsearch",
        "config": {
          "addresses": ["http://elasticsearch:9200"],
          "index": "e2e-stream"
        }
      }]
    }]
  }'
```

### 3.2 워크플로우 시작

```bash
curl -X POST http://localhost:8080/api/v1/workflows/<id>/start
```

**검증 포인트:**
- [ ] Agent가 K8s Deployment 생성
- [ ] Deployment replicas=1
- [ ] Health check 프로브 설정됨 (port 8082)
- [ ] Pipeline Runner Pod Running 상태

### 3.3 데이터 흐름 확인

```bash
# Kafka에 테스트 메시지 발행
kafka-console-producer --broker-list kafka:9092 --topic test-events <<EOF
{"id": 1, "name": "test", "created_at": "2026-01-01T00:00:00Z"}
EOF

# Elasticsearch에서 확인
curl http://elasticsearch:9200/e2e-stream/_search?q=id:1
```

### 3.4 워크플로우 중지

```bash
curl -X POST http://localhost:8080/api/v1/workflows/<id>/stop
```

**검증 포인트:**
- [ ] Deployment 삭제됨
- [ ] Pipeline Runner Pod Terminated

---

## Scenario 4: Agent Leader Election

### 4.1 Agent 2개 Pod 실행

```bash
kubectl scale deployment conduix-agent -n conduix --replicas=2
```

### 4.2 리더 선출 확인

```bash
# Lease 확인
kubectl get lease conduix-agent-leader -n conduix -o yaml

# Agent 로그에서 리더 선출 확인
kubectl logs -l app=conduix-agent -n conduix | grep "leader"
```

**검증 포인트:**
- [ ] 1개 Agent만 isLeader=true
- [ ] 리더만 Job/Deployment 생성
- [ ] 리더 실패 시 다른 Agent가 리더 인수

### 4.3 리더 장애 시뮬레이션

```bash
# 리더 Pod 삭제
kubectl delete pod <leader-pod-name> -n conduix
```

**검증 포인트:**
- [ ] 15초 내 새 리더 선출
- [ ] 기존 Job/Deployment 영향 없음

---

## Scenario 5: Resource Monitoring

### 5.1 클러스터 리소스 확인

```bash
# Agent heartbeat로 전달된 메트릭 확인
curl http://localhost:8080/api/v1/clusters/<id>/agents
```

**검증 포인트:**
- [ ] node_count, cpu_capacity, memory_capacity 값 존재
- [ ] running_pods, runner_pods 카운트 정확
- [ ] 30초 간격으로 메트릭 갱신

---

## Scenario 6: Web UI Plugin Management

### 6.1 플러그인 페이지 접근

1. `http://localhost:30000/plugins` 접근
2. 플러그인 목록 로드 확인

**검증 포인트:**
- [ ] 통계 카드: 전체 플러그인, 활성 플러그인, 전체 Stage
- [ ] DataGrid에 등록된 플러그인 표시

### 6.2 플러그인 등록 (UI)

1. "플러그인 등록" 버튼 클릭
2. 이름, 버전, 이미지 입력
3. Stage 추가 → type, displayName, category 입력
4. "생성" 클릭

**검증 포인트:**
- [ ] 다이얼로그 정상 렌더링
- [ ] Stage 추가/삭제 동작
- [ ] 성공 알림 표시
- [ ] 목록 자동 새로고침

### 6.3 동적 폼 렌더링

1. 파이프라인 Stage 편집 시 플러그인 Stage 선택
2. JSON Schema 기반 폼 자동 생성 확인

**검증 포인트:**
- [ ] number type → Number TextField 렌더링
- [ ] enum → Select 렌더링
- [ ] ui:widget=slider → Slider 렌더링
- [ ] required 필드 표시
- [ ] 기본값 적용

---

## 자동화 테스트 실행 가이드

### 환경 준비

```bash
# 1. Colima + K8s 시작
colima start --arch arm64 --kubernetes

# 2. 인프라 구성
make infra-up

# 3. DB 마이그레이션
make migrate

# 4. 서비스 시작
make run-control-plane &
make run-agent &
```

### 테스트 데이터 정리

```bash
# 테스트 후 정리
kubectl delete jobs -n conduix -l conduix.io/managed-by=conduix-agent
kubectl delete deployments -n conduix -l conduix.io/managed-by=conduix-agent
curl -X DELETE http://localhost:8080/api/v1/plugins/test-plugin
```
