# Conduix Plugin Development Guide

외부 사용자가 Conduix에 커스텀 Stage를 추가하기 위한 플러그인 개발 가이드입니다.

## 개요

Conduix 플러그인 시스템은 **컨테이너 이미지 + JSON Schema** 기반으로 동작합니다.
- 언어 무관: Go, Python, Node.js 등 어떤 언어로든 개발 가능
- stdin/stdout JSON Lines 프로토콜로 Pipeline Runner와 통신
- Control Plane에 등록하면 Web UI에서 자동으로 설정 폼이 생성됩니다

## 아키텍처

```
┌─────────────────────────────────────────────────────┐
│  Pipeline Runner Container                           │
│                                                      │
│  Built-in Stages (filter, remap, etc.)              │
│       ↓                                              │
│  Plugin Stage (subprocess)                           │
│       stdin → JSON line → Process → JSON line → stdout│
│       ↓                                              │
│  Output (elasticsearch, kafka, etc.)                │
└─────────────────────────────────────────────────────┘
```

## Quick Start

### 1. 프로젝트 구조 생성

```
my-plugin/
├── plugin.yaml          # 메타데이터 + UI 스키마
├── Dockerfile           # 컨테이너 빌드
└── src/
    └── main.py          # Stage 로직
```

### 2. plugin.yaml 작성

```yaml
apiVersion: conduix.io/v1
kind: StagePlugin
metadata:
  name: my-custom-stage
  version: 1.0.0
  description: 커스텀 데이터 변환 Stage

spec:
  image: myregistry/my-plugin:1.0.0

  stage:
    type: my-custom-transform
    category: transform         # transform, filter, enrich, output
    displayName: "커스텀 변환"
    icon: transform
    color: "#4CAF50"

  configSchema:
    type: object
    properties:
      threshold:
        type: number
        title: "임계값"
        description: "처리 임계값 (0~1)"
        default: 0.5
        minimum: 0
        maximum: 1
      mode:
        type: string
        title: "처리 모드"
        enum: ["fast", "accurate"]
        enumNames: ["빠른 처리", "정확한 처리"]
        default: "fast"
      fields:
        type: array
        title: "대상 필드"
        items:
          type: string
        minItems: 1
    required:
      - threshold
      - fields
```

### 3. Stage 로직 구현 (Python 예시)

```python
#!/usr/bin/env python3
"""
Conduix Plugin Stage - stdin/stdout JSON Lines 프로토콜
- stdin: JSON line 1개 = 레코드 1건
- stdout: 처리 결과 JSON line
"""
import json
import os
import sys

def process_record(record: dict, config: dict) -> dict:
    """단일 레코드 처리"""
    threshold = config.get("threshold", 0.5)
    fields = config.get("fields", [])

    for field in fields:
        if field in record:
            value = record[field]
            if isinstance(value, (int, float)) and value > threshold:
                record[f"{field}_flag"] = True

    return record

def main():
    # 환경변수에서 설정 로드
    config = json.loads(os.environ.get("STAGE_CONFIG", "{}"))

    # stdin에서 JSON line 읽기 → 처리 → stdout으로 출력
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            record = json.loads(line)
            result = process_record(record, config)
            print(json.dumps(result), flush=True)
        except json.JSONDecodeError as e:
            # 파싱 실패 시 에러를 stderr로 출력하고 원본 패스스루
            print(f"JSON parse error: {e}", file=sys.stderr)
            print(line, flush=True)

if __name__ == "__main__":
    main()
```

### 4. Go 언어 예시

```go
package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"
)

func main() {
    configJSON := os.Getenv("STAGE_CONFIG")
    var config map[string]any
    json.Unmarshal([]byte(configJSON), &config)

    scanner := bufio.NewScanner(os.Stdin)
    scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

    for scanner.Scan() {
        var record map[string]any
        if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
            fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
            continue
        }

        // 처리 로직
        record["processed"] = true

        output, _ := json.Marshal(record)
        fmt.Println(string(output))
    }
}
```

### 5. Dockerfile

```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY src/ .

# 필요한 패키지 설치
# RUN pip install -r requirements.txt

ENTRYPOINT ["python3", "main.py"]
```

### 6. Control Plane에 등록

#### 방법 A: API로 직접 등록

```bash
curl -X POST http://conduix-control-plane:8080/api/v1/plugins \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_TOKEN" \
  -d '{
    "name": "my-plugin",
    "version": "1.0.0",
    "image": "myregistry/my-plugin:1.0.0",
    "stages": [
      {
        "type": "my-custom-transform",
        "displayName": "커스텀 변환",
        "category": "transform",
        "configSchema": {
          "type": "object",
          "properties": {
            "threshold": {
              "type": "number",
              "title": "임계값",
              "default": 0.5
            }
          },
          "required": ["threshold"]
        }
      }
    ]
  }'
```

#### 방법 B: Helm Chart로 자동 등록

`conduix-plugins` 템플릿 저장소를 fork하여 사용합니다.

```bash
helm install my-plugins ./charts/conduix-plugins \
  --set image.repository=myregistry/my-plugin \
  --set image.tag=1.0.0 \
  --set controlPlane.service=conduix-control-plane \
  --set controlPlane.port=8080
```

Helm post-install hook이 자동으로 Control Plane API를 호출하여 플러그인을 등록합니다.

## JSON Schema → Web UI 매핑

| JSON Schema 속성 | 렌더링 UI |
|---|---|
| `type: string` | TextField |
| `type: number` | Number TextField |
| `type: boolean` | Switch |
| `type: string` + `enum` | Select/Dropdown |
| `type: array` + `items.type: string` | 쉼표 구분 TextField |
| `type: number` + `ui:widget: slider` | Slider |
| `type: string` + `ui:widget: textarea` | Multiline TextField |

### 지원되는 ui:widget 값

- `slider`: 숫자 슬라이더 (minimum/maximum 필수)
- `textarea`: 여러 줄 텍스트 입력
- `fieldSelector`: 데이터 필드 선택기 (향후 지원)

## 파이프라인에서 사용

등록된 플러그인 Stage는 파이프라인 설정에서 바로 사용할 수 있습니다.

```yaml
pipelines:
  - id: my-pipeline
    input:
      type: kafka
      config:
        brokers: ["localhost:9092"]
        topics: ["events"]
    stages:
      - type: filter
        config:
          condition: ".status == 'active'"
      - type: my-custom-transform    # 플러그인 Stage
        config:
          threshold: 0.8
          mode: "accurate"
          fields: ["score", "confidence"]
    outputs:
      - type: elasticsearch
        config:
          addresses: ["http://es:9200"]
          index: "processed-events"
```

Agent가 파이프라인 실행 시 플러그인 Stage의 이미지를 자동으로 조회하여 Pipeline Runner를 구성합니다.

## 통신 프로토콜

### JSON Lines (JSONL)

- **입력** (stdin): 한 줄에 하나의 JSON 객체
- **출력** (stdout): 한 줄에 하나의 JSON 객체
- **에러** (stderr): 로그 메시지 (Pipeline Runner가 수집)

```
← stdin:  {"id": 1, "name": "Alice", "score": 0.9}
→ stdout: {"id": 1, "name": "Alice", "score": 0.9, "score_flag": true}

← stdin:  {"id": 2, "name": "Bob", "score": 0.3}
→ stdout: {"id": 2, "name": "Bob", "score": 0.3}
```

### 규칙

1. **1:1 매핑**: 입력 1줄 → 출력 1줄 (순서 유지)
2. **즉시 flush**: 출력 후 반드시 flush (`flush=True` in Python, `\n` in Go)
3. **에러 처리**: 파싱 실패 시 원본을 그대로 패스스루하거나 stderr로 에러 출력
4. **버퍼 크기**: 최대 1MB/레코드 (Pipeline Runner의 기본 스캐너 버퍼)

## 테스트

### 로컬 테스트

```bash
# 입력 데이터 준비
echo '{"id":1,"value":0.9}' | STAGE_CONFIG='{"threshold":0.5,"fields":["value"]}' python3 src/main.py

# 여러 레코드 테스트
cat <<EOF | STAGE_CONFIG='{"threshold":0.5,"fields":["value"]}' python3 src/main.py
{"id":1,"value":0.9}
{"id":2,"value":0.3}
{"id":3,"value":0.7}
EOF
```

### Docker 테스트

```bash
docker build -t my-plugin:test .

echo '{"id":1,"value":0.9}' | docker run -i -e STAGE_CONFIG='{"threshold":0.5,"fields":["value"]}' my-plugin:test
```

## 배포 체크리스트

- [ ] plugin.yaml에 configSchema 정의
- [ ] stdin/stdout JSON Lines 프로토콜 구현
- [ ] 에러 시 stderr 출력 (stdout 오염 방지)
- [ ] Dockerfile 작성 및 이미지 빌드
- [ ] 로컬 테스트 통과
- [ ] 컨테이너 레지스트리에 푸시
- [ ] Control Plane에 플러그인 등록
- [ ] Web UI에서 Stage 설정 폼 확인
