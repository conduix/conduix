# 파이프라인 플러그인 아키텍처 V2

## 기존 계획의 문제점

### Agent + gRPC 방식의 한계

```
┌─ 기존 Agent 방식 ───────────────────────────────────────────┐
│                                                             │
│  Agent Pod (상주, 파이프라인 실행 엔진)                      │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  빌트인 Stages (컴파일됨)                             │   │
│  │       ↓                                              │   │
│  │  GRPCPluginHost ──── gRPC ────→ Plugin Pod          │   │
│  │       ↓                            (Python)          │   │
│  │  매 레코드마다 네트워크 호출                          │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  문제:                                                      │
│  1. gRPC 오버헤드: ~100-200μs/record (대용량 처리 병목)     │
│  2. Go Stage 추가: 여전히 재컴파일 + 재배포 필요            │
│  3. 복잡한 아키텍처: Agent + Plugin 서버 둘 다 관리         │
└─────────────────────────────────────────────────────────────┘
```

---

## 새로운 아키텍처

### 핵심 변경

| 구분 | 기존 | 새로운 |
|------|------|--------|
| Agent 역할 | 파이프라인 실행 엔진 | 클러스터 매니저 |
| 파이프라인 실행 | Agent 내부에서 | Job/Deployment로 |
| Stage 확장 | gRPC 플러그인 | 컨테이너 이미지 |
| 데이터 처리 | Agent가 직접 | Pipeline Runner가 |

### 전체 아키텍처

```
┌─────────────────────────────────────────────────────────────┐
│  Control Plane (중앙 관리)                                  │
│                                                             │
│  - 파이프라인/플러그인 정의 관리                            │
│  - 스케줄링 결정                                            │
│  - 전체 클러스터 현황 대시보드                              │
│  - Web UI 제공                                              │
└─────────────────────────────────────────────────────────────┘
          │                              │
          │ REST API                     │ REST API
          ↓                              ↓
┌─────────────────────────┐    ┌─────────────────────────┐
│  Cluster A              │    │  Cluster B              │
│                         │    │                         │
│  ┌───────────────────┐  │    │  ┌───────────────────┐  │
│  │ Agent (HA x2)     │  │    │  │ Agent (HA x2)     │  │
│  │                   │  │    │  │                   │  │
│  │ 역할:             │  │    │  │ 역할:             │  │
│  │ - Job/Deployment  │  │    │  │ - Job/Deployment  │  │
│  │   생성/삭제       │  │    │  │   생성/삭제       │  │
│  │ - 리소스 모니터링 │  │    │  │ - 리소스 모니터링 │  │
│  │ - 상태 보고       │  │    │  │ - 상태 보고       │  │
│  │ - 헬스체크        │  │    │  │ - 헬스체크        │  │
│  └─────────┬─────────┘  │    │  └─────────┬─────────┘  │
│            │            │    │            │            │
│            ↓ 생성/관리   │    │            ↓ 생성/관리   │
│  ┌───────────────────┐  │    │  ┌───────────────────┐  │
│  │ Pipeline Runners  │  │    │  │ Pipeline Runners  │  │
│  │                   │  │    │  │                   │  │
│  │ - Job (배치)      │  │    │  │ - Job (배치)      │  │
│  │ - Deployment      │  │    │  │ - Deployment      │  │
│  │   (스트리밍)      │  │    │  │   (스트리밍)      │  │
│  └───────────────────┘  │    │  └───────────────────┘  │
└─────────────────────────┘    └─────────────────────────┘
```

---

## Agent 역할 상세

### 기존 vs 새로운 역할

```
┌─ 기존 Agent ────────────────────────────────────────────────┐
│  파이프라인 실행 엔진                                        │
│  - Input/Stage/Output 직접 실행                             │
│  - 데이터 처리                                              │
│  - Stage 내장 (Go 컴파일)                                   │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─ 새로운 Agent ──────────────────────────────────────────────┐
│  클러스터 매니저 (데이터 처리 안 함)                         │
│                                                             │
│  1. Control Plane 통신                                      │
│     - 파이프라인 실행 명령 수신                             │
│     - 상태/메트릭 보고                                      │
│                                                             │
│  2. K8s 리소스 관리                                         │
│     - Job 생성/삭제 (배치 파이프라인)                       │
│     - Deployment 생성/삭제 (스트리밍 파이프라인)            │
│     - CronJob 관리 (스케줄 파이프라인)                      │
│                                                             │
│  3. 모니터링                                                │
│     - 클러스터 리소스 (CPU/Memory/Pod 수)                   │
│     - Job/Deployment 상태                                   │
│     - 리소스 부족 알림                                      │
│                                                             │
│  4. HA 구성                                                 │
│     - Leader Election (2개 Pod)                             │
│     - 자동 Failover                                         │
└─────────────────────────────────────────────────────────────┘
```

### Agent 없이 가능한가?

| 기능 | Agent 없이 | Agent 있으면 | 비고 |
|------|-----------|-------------|------|
| Job 생성 | Control Plane → K8s API 직접 | Agent가 대행 | 네트워크/인증 문제 |
| 모니터링 | Prometheus 별도 구축 | Agent가 수집/보고 | 통합 대시보드 |
| 멀티 클러스터 | kubeconfig 관리 복잡 | Agent가 내부 접근 | 방화벽 통과 |

**결론**: Agent 필요 (역할만 변경)

---

## 파이프라인 실행 모드

### 데이터 연동 패턴

| 패턴 | 설명 | 실행 방식 |
|------|------|----------|
| **배치 전용** | 주기적 대량 처리 | K8s Job / CronJob |
| **실시간 전용** | Kafka 상시 consume | K8s Deployment |
| **배치 + 실시간** | 주기적 배치 + 사이사이 실시간 동기화 | CronJob + Deployment |

### 실행 모드별 구현

```yaml
# 1. 배치 전용 (매시간 실행)
apiVersion: conduix.io/v1
kind: Pipeline
metadata:
  name: hourly-batch-sync
spec:
  mode: batch
  schedule: "0 * * * *"  # 매시간

  input:
    type: sql
    config:
      query: "SELECT * FROM orders WHERE updated_at > ?"

  stages:
    - type: transform
      ...

  outputs:
    - type: elasticsearch
      ...
```

```yaml
# 2. 실시간 전용 (상시 실행)
apiVersion: conduix.io/v1
kind: Pipeline
metadata:
  name: realtime-events
spec:
  mode: streaming
  replicas: 2  # 병렬 처리

  input:
    type: kafka
    config:
      brokers: ["kafka:9092"]
      topic: events
      consumerGroup: realtime-processor

  stages:
    - type: filter
      ...

  outputs:
    - type: kafka
      config:
        topic: processed-events
```

```yaml
# 3. 배치 + 실시간 (대용량 동기화)
apiVersion: conduix.io/v1
kind: Pipeline
metadata:
  name: hybrid-sync
spec:
  mode: hybrid

  # 배치: 매일 새벽 전체 동기화
  batch:
    schedule: "0 2 * * *"
    input:
      type: sql
      config:
        query: "SELECT * FROM large_table"
    parallelism: 10  # 병렬 Job

  # 실시간: CDC로 변경분 동기화
  streaming:
    replicas: 2
    input:
      type: kafka
      config:
        topic: cdc-changes

  # 공통 처리
  stages:
    - type: transform
      ...

  outputs:
    - type: elasticsearch
      ...
```

### K8s 리소스 매핑

```
┌─────────────────────────────────────────────────────────────┐
│  Pipeline Mode          →    K8s Resource                  │
├─────────────────────────────────────────────────────────────┤
│  batch (일회성)         →    Job                           │
│  batch (스케줄)         →    CronJob                       │
│  streaming              →    Deployment + Service          │
│  hybrid.batch           →    CronJob                       │
│  hybrid.streaming       →    Deployment                    │
└─────────────────────────────────────────────────────────────┘
```

---

## Pipeline Runner (실제 실행 컴포넌트)

### 구조

```
┌─────────────────────────────────────────────────────────────┐
│  Pipeline Runner Container                                  │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Pipeline Runner (Go Binary)                          │  │
│  │                                                       │  │
│  │  1. 설정 로드 (ConfigMap/Secret)                      │  │
│  │  2. Input 시작                                        │  │
│  │  3. Stage Chain 실행                                  │  │
│  │     - 빌트인: 직접 호출                               │  │
│  │     - 플러그인: subprocess 또는 같은 이미지 내 실행    │  │
│  │  4. Output으로 전송                                   │  │
│  │  5. Checkpoint 저장 (스트리밍)                        │  │
│  └───────────────────────────────────────────────────────┘  │
│                                                             │
│  환경변수:                                                  │
│  - PIPELINE_CONFIG: 파이프라인 설정 (JSON)                 │
│  - STAGE_CONFIGS: Stage별 설정                             │
│  - CHECKPOINT_PATH: 체크포인트 저장 위치                   │
└─────────────────────────────────────────────────────────────┘
```

### 이미지 구성 방식

**Option A: 단일 통합 이미지**
```
conduix/pipeline-runner:v1.0.0
├── 빌트인 Stages (filter, remap, contract...)
├── 빌트인 Inputs (kafka, sql, file...)
├── 빌트인 Outputs (elasticsearch, kafka, s3...)
└── 플러그인 로더
```

**Option B: 베이스 + 플러그인 이미지**
```dockerfile
# 사용자 커스텀 이미지
FROM conduix/pipeline-runner:v1.0.0

# 플러그인 추가
COPY --from=mycompany/ml-stage:1.0 /plugins/ml-stage /plugins/
COPY --from=mycompany/custom-output:1.0 /plugins/custom-output /plugins/
```

---

## 플러그인 시스템

### External Plugin Repository (conduix-plugins)

사용자가 Conduix 소스 코드를 수정하지 않고 커스텀 Stage를 추가할 수 있는 별도 템플릿 저장소입니다.

```
conduix-plugins/                    # 사용자가 fork해서 사용하는 템플릿
├── stages/
│   └── my-custom-stage/
│       ├── stage.go                # Stage 로직 (Go)
│       └── plugin.yaml             # 메타데이터 + UI 스키마
│
├── main.go                         # 플러그인 진입점
├── go.mod
├── Dockerfile
│
├── charts/                         # Helm Chart
│   └── conduix-plugins/
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
│           ├── deployment.yaml     # Pipeline Runner 이미지로 사용
│           ├── configmap.yaml      # plugin.yaml들 마운트
│           └── job-register.yaml   # 플러그인 자동 등록 Hook
│
├── argocd/
│   ├── application.yaml            # ArgoCD Application
│   └── applicationset.yaml         # 멀티 클러스터 배포
│
└── .github/
    └── workflows/
        └── build.yml               # CI/CD
```

### K8s 배포 및 Control Plane 통합

```
┌─────────────────────────────────────────────────────────────┐
│  1. conduix-plugins 배포 (Helm/ArgoCD)                       │
│                                                             │
│     helm install my-plugins ./charts/conduix-plugins        │
│        또는                                                  │
│     kubectl apply -f argocd/application.yaml                │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Helm post-install hook (Job)                            │
│                                                             │
│     for f in /plugins/*.yaml; do                            │
│       curl -X POST "http://conduix-control-plane/api/plugins"│
│         -d '{"image": "...", "schema": ...}'                │
│     done                                                    │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Control Plane DB 저장                                    │
│                                                             │
│     plugins 테이블:                                          │
│     ┌────────┬───────────────────────────┬────────────────┐ │
│     │ name   │ image                      │ stage_type    │ │
│     ├────────┼───────────────────────────┼────────────────┤ │
│     │ my-pl  │ myregistry/conduix-plugins│ my-custom-stage│ │
│     └────────┴───────────────────────────┴────────────────┘ │
│                                                             │
│     plugin_stages 테이블:                                    │
│     ┌──────────────────┬────────────────┬─────────────────┐ │
│     │ stage_type       │ display_name   │ schema (JSON)   │ │
│     ├──────────────────┼────────────────┼─────────────────┤ │
│     │ my-custom-stage  │ 커스텀 Stage   │ {properties:...}│ │
│     └──────────────────┴────────────────┴─────────────────┘ │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Web UI에서 Stage 사용                                    │
│                                                             │
│     GET /api/stages                                          │
│     {                                                        │
│       "builtin": ["filter", "remap", ...],                  │
│       "plugins": [                                          │
│         {                                                   │
│           "type": "my-custom-stage",                        │
│           "displayName": "커스텀 Stage",                     │
│           "source": "myregistry/conduix-plugins:v1.0.0",    │
│           "schema": { ... JSON Schema ... }                 │
│         }                                                   │
│       ]                                                     │
│     }                                                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  5. 파이프라인 실행 시                                        │
│                                                             │
│     Pipeline 설정:                                           │
│     stages:                                                 │
│       - type: my-custom-stage  ← 커스텀 Stage 사용           │
│         config:                                             │
│           threshold: 0.8                                    │
│                                                             │
│     Agent가 Job 생성 시:                                     │
│       - Control Plane에서 stage_type → image 조회            │
│       - 해당 이미지로 Pipeline Runner 실행                   │
│         image: myregistry/conduix-plugins:v1.0.0            │
└─────────────────────────────────────────────────────────────┘
```

### Helm Chart 예시

```yaml
# charts/conduix-plugins/templates/job-register.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .Release.Name }}-register-plugins
  annotations:
    helm.sh/hook: post-install,post-upgrade
    helm.sh/hook-weight: "0"
    helm.sh/hook-delete-policy: hook-succeeded
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: register
          image: curlimages/curl:latest
          command:
            - /bin/sh
            - -c
            - |
              for f in /plugins/*.yaml; do
                echo "Registering plugin from $f"
                curl -X POST \
                  "http://{{ .Values.controlPlane.service }}:{{ .Values.controlPlane.port }}/api/plugins" \
                  -H "Content-Type: application/json" \
                  -H "Authorization: Bearer $API_TOKEN" \
                  -d "{
                    \"name\": \"{{ .Release.Name }}\",
                    \"version\": \"{{ .Chart.AppVersion }}\",
                    \"image\": \"{{ .Values.image.repository }}:{{ .Values.image.tag }}\",
                    \"schema\": $(cat $f | jq -c)
                  }"
              done
          env:
            - name: API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.controlPlane.secretName }}
                  key: api-token
          volumeMounts:
            - name: plugin-schemas
              mountPath: /plugins
      volumes:
        - name: plugin-schemas
          configMap:
            name: {{ .Release.Name }}-schemas
```

```yaml
# charts/conduix-plugins/templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-schemas
data:
{{- range $path, $_ := .Files.Glob "stages/*/plugin.yaml" }}
  {{ $path | base }}: |-
{{ $.Files.Get $path | indent 4 }}
{{- end }}
```

### ArgoCD Application 예시

```yaml
# argocd/application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: conduix-plugins
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/mycompany/conduix-plugins.git
    targetRevision: main
    path: charts/conduix-plugins
    helm:
      values: |
        image:
          repository: myregistry/conduix-plugins
          tag: v1.0.0
        controlPlane:
          service: conduix-control-plane
          port: 8080
  destination:
    server: https://kubernetes.default.svc
    namespace: conduix
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### 플러그인 패키지 구조

```
my-stage-plugin/
├── plugin.yaml          # 메타데이터 + UI 스키마
├── Dockerfile           # 컨테이너 빌드
└── src/
    └── main.py          # Stage 로직 (또는 main.go, index.js 등)
```

### plugin.yaml 스키마

```yaml
apiVersion: conduix.io/v1
kind: StagePlugin
metadata:
  name: ml-anomaly-detector
  version: 1.0.0
  description: ML 기반 이상치 탐지 Stage
  author: mycompany

spec:
  # 컨테이너 이미지
  image: mycompany/ml-anomaly-detector:1.0.0

  # Stage 타입 정보
  stage:
    type: ml-anomaly
    category: transform
    displayName: "ML 이상치 탐지"
    icon: analytics
    color: "#4CAF50"

  # UI 자동 생성용 스키마 (JSON Schema 기반)
  configSchema:
    type: object
    properties:
      threshold:
        type: number
        title: "이상치 임계값"
        description: "0~1 사이 값, 높을수록 민감"
        default: 0.8
        minimum: 0
        maximum: 1
        ui:widget: slider

      model:
        type: string
        title: "모델 선택"
        enum: ["isolation_forest", "autoencoder", "lstm"]
        enumNames: ["Isolation Forest", "AutoEncoder", "LSTM"]
        default: "isolation_forest"

      features:
        type: array
        title: "분석 대상 필드"
        items:
          type: string
        minItems: 1
        ui:widget: fieldSelector

    required:
      - threshold
      - model
      - features
```

### Stage 구현 예시 (Python)

```python
# src/main.py
import os
import json
import sys
from sklearn.ensemble import IsolationForest

def process_batch(records: list, config: dict) -> list:
    """배치 단위로 레코드 처리"""
    threshold = config.get("threshold", 0.8)
    features = config.get("features", [])

    X = [[r.get(f, 0) for f in features] for r in records]

    model = IsolationForest(contamination=1-threshold)
    scores = model.fit_predict(X)

    for record, score in zip(records, scores):
        record["anomaly_score"] = float(score)
        record["is_anomaly"] = score == -1

    return records

def main():
    config = json.loads(os.environ.get("STAGE_CONFIG", "{}"))
    batch_size = config.get("batchSize", 100)

    batch = []
    for line in sys.stdin:
        record = json.loads(line)
        batch.append(record)

        if len(batch) >= batch_size:
            for r in process_batch(batch, config):
                print(json.dumps(r))
            batch = []

    if batch:
        for r in process_batch(batch, config):
            print(json.dumps(r))

if __name__ == "__main__":
    main()
```

---

## Control Plane 통합

### 데이터베이스 스키마

```sql
-- 플러그인 레지스트리 (conduix-plugins 단위)
CREATE TABLE plugins (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,    -- "my-company-plugins"
    version VARCHAR(50) NOT NULL,         -- "v1.0.0"
    image VARCHAR(500) NOT NULL,          -- "myregistry/conduix-plugins:v1.0.0"
    description TEXT,
    source_repo VARCHAR(500),             -- Git 저장소 URL (optional)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- 플러그인이 제공하는 Stage 목록
CREATE TABLE plugin_stages (
    id VARCHAR(36) PRIMARY KEY,
    plugin_id VARCHAR(36) NOT NULL,
    stage_type VARCHAR(100) NOT NULL UNIQUE,  -- "ml-anomaly"
    category VARCHAR(50),                      -- "transform"
    display_name VARCHAR(255),                 -- "ML 이상치 탐지"
    description TEXT,
    config_schema JSON NOT NULL,               -- JSON Schema for UI form
    icon VARCHAR(100),
    color VARCHAR(20),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
);

-- 클러스터/Agent 관리
CREATE TABLE clusters (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    status ENUM('active', 'inactive', 'error') DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE agents (
    id VARCHAR(36) PRIMARY KEY,
    cluster_id VARCHAR(36) NOT NULL,
    hostname VARCHAR(255),
    is_leader BOOLEAN DEFAULT FALSE,
    last_heartbeat TIMESTAMP,
    status ENUM('active', 'inactive', 'error') DEFAULT 'active',
    metrics JSON,  -- CPU, Memory, Pod count 등
    FOREIGN KEY (cluster_id) REFERENCES clusters(id)
);

-- 파이프라인 실행 관리
CREATE TABLE pipeline_runs (
    id VARCHAR(36) PRIMARY KEY,
    pipeline_id VARCHAR(36) NOT NULL,
    cluster_id VARCHAR(36) NOT NULL,
    mode ENUM('batch', 'streaming') NOT NULL,
    k8s_resource_type ENUM('job', 'cronjob', 'deployment') NOT NULL,
    k8s_resource_name VARCHAR(255),
    status ENUM('pending', 'running', 'completed', 'failed') DEFAULT 'pending',
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    metrics JSON,  -- records processed, errors 등
    FOREIGN KEY (cluster_id) REFERENCES clusters(id)
);
```

### API 엔드포인트

#### 플러그인 관리 API

```
# 플러그인 관리
GET    /api/plugins              # 등록된 플러그인 목록
POST   /api/plugins              # 플러그인 등록 (Helm hook이 호출)
GET    /api/plugins/:name        # 플러그인 상세
PUT    /api/plugins/:name        # 플러그인 업데이트
DELETE /api/plugins/:name        # 플러그인 삭제 (연관 Stage도 삭제)
GET    /api/plugins/:name/stages # 특정 플러그인의 Stage 목록
```

#### Stage 스키마 API (빌트인 + 플러그인 통합)

```
GET    /api/stages               # 전체 Stage 목록 (빌트인 + 플러그인)
GET    /api/stages/:type/schema  # 특정 Stage의 JSON Schema
```

#### Response 예시

```json
// GET /api/stages
{
  "builtin": [
    {
      "type": "filter",
      "displayName": "Filter",
      "category": "transform",
      "description": "조건에 따라 레코드 필터링"
    },
    {
      "type": "remap",
      "displayName": "Remap",
      "category": "transform",
      "description": "필드 변환 및 매핑"
    }
  ],
  "plugins": [
    {
      "type": "ml-anomaly",
      "displayName": "ML 이상치 탐지",
      "category": "transform",
      "description": "ML 모델 기반 이상치 탐지",
      "pluginName": "my-company-plugins",
      "pluginImage": "myregistry/conduix-plugins:v1.0.0"
    }
  ]
}
```

```json
// GET /api/stages/ml-anomaly/schema
{
  "type": "ml-anomaly",
  "displayName": "ML 이상치 탐지",
  "pluginImage": "myregistry/conduix-plugins:v1.0.0",
  "configSchema": {
    "type": "object",
    "properties": {
      "threshold": {
        "type": "number",
        "title": "이상치 임계값",
        "default": 0.8,
        "minimum": 0,
        "maximum": 1
      },
      "model": {
        "type": "string",
        "title": "모델 선택",
        "enum": ["isolation_forest", "autoencoder"],
        "default": "isolation_forest"
      }
    },
    "required": ["threshold", "model"]
  },
  "uiSchema": {
    "threshold": {
      "ui:widget": "slider"
    }
  }
}
```

#### 플러그인 등록 Request 예시

```json
// POST /api/plugins (Helm hook이 호출)
{
  "name": "my-company-plugins",
  "version": "v1.0.0",
  "image": "myregistry/conduix-plugins:v1.0.0",
  "stages": [
    {
      "type": "ml-anomaly",
      "displayName": "ML 이상치 탐지",
      "category": "transform",
      "configSchema": { ... }
    },
    {
      "type": "custom-enricher",
      "displayName": "커스텀 Enricher",
      "category": "enrich",
      "configSchema": { ... }
    }
  ]
}
```

# 클러스터/Agent 관리
GET    /api/clusters
POST   /api/clusters
GET    /api/clusters/:id
GET    /api/clusters/:id/agents
GET    /api/clusters/:id/resources  # 리소스 현황

# Agent → Control Plane (Agent가 호출)
POST   /api/agents/heartbeat
POST   /api/agents/metrics
POST   /api/agents/events  # Job 완료, 에러 등

# 파이프라인 실행
POST   /api/pipelines/:id/run
GET    /api/pipelines/:id/runs
GET    /api/pipeline-runs/:id
DELETE /api/pipeline-runs/:id  # 실행 중지
```

---

## Web UI 동적 폼 렌더링

### 구현 방식

```tsx
// StageConfigForm.tsx
import Form from '@rjsf/mui';

function StageConfigForm({ stageType, config, onChange }) {
  const { data: schema } = useQuery(['stageSchema', stageType], () =>
    api.get(`/api/stages/${stageType}/schema`)
  );

  if (!schema) return <Loading />;

  return (
    <Form
      schema={schema.configSchema}
      uiSchema={schema.uiSchema}
      formData={config}
      onChange={(e) => onChange(e.formData)}
    />
  );
}
```

### 스키마 → UI 자동 생성

```
┌─────────────────────────────────────────────────────────────┐
│  plugin.yaml의 configSchema                                 │
│                                                             │
│  threshold:                                                 │
│    type: number                                             │
│    title: "이상치 임계값"                                    │
│    default: 0.8                                             │
│    minimum: 0                                               │
│    maximum: 1                                               │
│    ui:widget: slider                                        │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  자동 생성된 UI                                              │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  이상치 임계값                                       │   │
│  │  [========●===========] 0.8                         │   │
│  │   0                    1                            │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## 구현 로드맵

### Phase 1: Control Plane - 플러그인/클러스터 관리 기반 ✅ 완료

**목표**: 플러그인 등록, 클러스터/Agent 관리를 위한 DB 스키마 및 API 구현

| 작업 | 파일 | 설명 | 상태 |
|------|------|------|------|
| 1.1 DB 모델 | `control-plane/pkg/models/models.go` | Plugin, PluginStage 모델 | ✅ |
| 1.2 DB 모델 | `control-plane/pkg/models/models.go` | Agent 모델 확장 (Leader Election) | ✅ |
| 1.3 DB 모델 | `control-plane/pkg/models/models.go` | PipelineRun 모델 확장 (K8s 실행 정보) | ✅ |
| 1.4 플러그인 API | `control-plane/internal/api/handlers/plugin_handler.go` | 플러그인 CRUD (Upsert 지원) | ✅ |
| 1.5 Stage API | `control-plane/internal/api/handlers/stage_handler.go` | 빌트인+플러그인 Stage 목록 | ✅ |
| 1.6 클러스터 API | `control-plane/internal/api/handlers/cluster.go` | 기존 구현 활용 | ✅ |
| 1.7 라우트 등록 | `control-plane/internal/api/routes.go` | API 라우트 추가 | ✅ |
| 1.8 테스트 | `control-plane/internal/api/handlers/plugin_handler_test.go` | 유닛 테스트 | ✅ |

**구현 완료 내역**:
- Plugin/PluginStage 모델: Helm post-install 훅에서 등록 가능
- Agent 모델 확장: Leader Election 필드 추가 (IsLeader, LeaderSince, LeaderLeaseTTL)
- PipelineRun 모델 확장: K8s 리소스 추적 필드 추가 (ExecutionMode, K8sResourceType, K8sResourceName, RunnerImage)
- 플러그인 API: POST로 Upsert 지원 (같은 이름 등록 시 업데이트)
- Stage API: /api/v1/stages에서 빌트인+플러그인 Stage 통합 조회
- 테스트: 8개 테스트 케이스 모두 PASS

### Phase 2: Agent - 클러스터 매니저 역할 변경

**목표**: Agent를 파이프라인 실행 엔진에서 클러스터 매니저로 변경

| 작업 | 파일 | 설명 | 상태 |
|------|------|------|------|
| 2.1 K8s 클라이언트 | `pipeline-agent/internal/k8s/client.go` | K8s API 클라이언트 | ✅ |
| 2.2 Job 관리자 | `pipeline-agent/internal/k8s/job_manager.go` | Job/CronJob 생성/삭제 | ✅ |
| 2.3 Deployment 관리자 | `pipeline-agent/internal/k8s/deployment_manager.go` | Deployment 생성/삭제 | ✅ |
| 2.4 리소스 모니터링 | `pipeline-agent/internal/monitor/resource_monitor.go` | CPU/Memory/Pod 모니터링 | ✅ |
| 2.5 Leader Election | `pipeline-agent/internal/leader/election.go` | HA를 위한 리더 선출 | ✅ |
| 2.6 Control Plane 통신 | `pipeline-agent/internal/controlplane/client.go` | 상태 보고, 명령 수신 | ✅ |
| 2.7 테스트 | `pipeline-agent/internal/k8s/*_test.go` 등 | 유닛 테스트 (커버리지 72.8%) | ✅ |

### Phase 3: Pipeline Runner 개발

**목표**: Job/Deployment로 실행되는 독립 파이프라인 실행기 개발

| 작업 | 파일 | 설명 | 상태 |
|------|------|------|------|
| 3.1 Runner 진입점 | `pipeline-runner/cmd/runner/main.go` | CLI 진입점 | ✅ |
| 3.2 Runner 코어 | `pipeline-runner/internal/runner/runner.go` | 파이프라인 실행 로직 | ✅ |
| 3.3 설정 로더 | `pipeline-runner/internal/config/loader.go` | ConfigMap/환경변수 로드 | ✅ |
| 3.4 Checkpoint | `pipeline-runner/internal/checkpoint/manager.go` | 오프셋 저장/복구 | ✅ |
| 3.5 Dockerfile | `pipeline-runner/Dockerfile` | 컨테이너 이미지 빌드 | ✅ |
| 3.6 테스트 | `pipeline-runner/internal/config/*_test.go` 등 | 유닛 테스트 | ✅ |

### Phase 4: 플러그인 시스템 및 SDK

**목표**: 외부 사용자가 커스텀 Stage를 개발할 수 있는 SDK 제공

| 작업 | 파일 | 설명 | 상태 |
|------|------|------|------|
| 4.1 Stage 인터페이스 | `pipeline-core/pkg/plugin/stage_interface.go` | 플러그인 Stage 인터페이스 | ✅ |
| 4.2 Stage 레지스트리 | `pipeline-core/pkg/plugin/registry.go` | 빌트인 + 플러그인 통합 | ✅ |
| 4.3 Subprocess Stage | `pipeline-core/pkg/plugin/subprocess.go` | stdin/stdout JSON Lines | ✅ |
| 4.4 테스트 | `pipeline-core/pkg/plugin/registry_test.go` | 유닛 테스트 (9개) | ✅ |
| 4.5 conduix-plugins 템플릿 | 별도 저장소 | Helm Chart + 예제 (문서화 완료) | ✅ |

### Phase 5: Web UI

**목표**: 플러그인 관리 및 동적 폼 렌더링

| 작업 | 파일 | 설명 | 상태 |
|------|------|------|------|
| 5.1 플러그인 목록 | `web-ui/src/pages/Plugins.tsx` | 플러그인 관리 페이지 | ✅ |
| 5.2 동적 폼 | `web-ui/src/components/DynamicStageForm.tsx` | JSON Schema 기반 폼 | ✅ |
| 5.3 타입/API | `web-ui/src/types/plugin.ts`, `services/pluginApi.ts` | 타입 + API 서비스 | ✅ |
| 5.4 실행 모드 설정 | `web-ui/src/components/PipelineModeSelector.tsx` | 배치/스트리밍 선택 | ✅ |
| 5.5 라우팅/네비 | `web-ui/src/App.tsx`, `Layout/MainLayout.tsx` | 라우트 + 메뉴 추가 | ✅ |
| 5.6 i18n | `web-ui/src/i18n/locales/{en,ko}.json` | 한/영 번역 | ✅ |

### Phase 6: 테스트 및 문서화

| 작업 | 설명 | 상태 |
|------|------|------|
| 6.1 E2E 테스트 | `docs/E2E_TEST_SCENARIOS.md` | ✅ |
| 6.2 플러그인 개발 가이드 | `docs/PLUGIN_DEVELOPMENT_GUIDE.md` | ✅ |
| 6.3 운영 가이드 | V2 계획 문서 내 포함 | ✅ |

---

## 마이그레이션 전략

### 기존 Agent와 공존

```
┌─────────────────────────────────────────────────────────────┐
│  Phase 1-2: 기존 Agent 유지                                 │
│                                                             │
│  - 기존 파이프라인: Agent가 실행 (변경 없음)                 │
│  - 새 파이프라인: Job/Deployment로 실행 (선택적)            │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Phase 3-4: 점진적 전환                                     │
│                                                             │
│  - 기존 파이프라인을 하나씩 Job 방식으로 전환               │
│  - Agent 역할 축소 (모니터링만)                             │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Phase 5-6: 완전 전환                                       │
│                                                             │
│  - Agent: 클러스터 매니저 역할만                            │
│  - 모든 파이프라인: Job/Deployment로 실행                   │
└─────────────────────────────────────────────────────────────┘
```

---

## 결론

### 핵심 변경 요약

1. **Agent 역할 변경**: 파이프라인 실행 → 클러스터 매니저
2. **실행 방식**: Job(배치) / Deployment(스트리밍) / 하이브리드
3. **플러그인**: 컨테이너 이미지 + plugin.yaml (UI 자동 생성)
4. **HA**: Agent 2개 Pod, Leader Election

### 장점

- 네트워크 오버헤드 없음 (gRPC 제거)
- 언어 무관 (컨테이너 기반)
- 무중단 배포 (새 이미지로 새 Job)
- UI 코드 수정 없이 폼 자동 생성
- K8s 네이티브 (리소스 관리, 스케일링)
- 멀티 클러스터 지원 (Agent가 각 클러스터에서 동작)

### 다음 단계

1. 상세 설계 리뷰
2. Agent 리팩토링 시작 (Phase 1)
3. Pipeline Runner 프로토타입
