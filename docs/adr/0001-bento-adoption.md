# ADR-0001: Bento(Benthos fork) 채택과 실제 사용 범위

- **Status**: Accepted (제한적 — 의존성 유지, 커넥터는 자체 구현)
- **작성**: 2026-07-06 (사후 정리 ADR — 원 결정은 프로젝트 초기)
- **근거 수준**: 대안 비교는 문서화됨(`archive/technical-design-review.md`). "채택 후 자체 구현으로 선회한" 결정은 **당시 기록 없음** → 코드/의존성 상태로 사후 정리.

## Context (문제)

데이터 파이프라인의 커넥터(Kafka/SQL/S3/ES 등 소스·싱크)를 **직접 다 구현하면 비용이 크고 검증 부담이 크다.** 검증된 오픈소스 커넥터 자산을 레버리지하고 싶었다.

동시에 **라이선스 리스크**가 있었다: Benthos는 Redpanda에 인수되며 라이선스가 바뀌었다. 프로덕트에 그대로 의존하면 라이선스 제약을 받는다. (프로젝트 정책: AGPL 등 제약 라이선스 회피 — 예: MinIO 사용 금지.)

## Decision (선택)

**Bento(`github.com/warpstreamlabs/bento`, MIT fork of Benthos)를 의존성으로 채택**하되, 실제 커넥터는 **자체 Go 구현**을 canonical로 둔다.

- 의존성: `pipeline-core/go.mod`의 `warpstreamlabs/bento` — 라이선스 안전한 MIT fork.
- Bento 연동 코드는 `pipeline-core/pkg/adapter/bento/`의 **어댑터 계층에 한정**(MessageConverter, InputAdapter). 향후 Bento 커넥터/Bloblang을 필요 시 흡수할 수 있는 접점.
- 실제 운영 커넥터는 자체 구현: `source/`(kafka, sql, http, cdc, mqtt, pubsub 등 16종), `output/`(sql, es, kafka, mongodb, s3, gcs, bigquery 등 9종). segmentio/kafka-go, amqp091-go, paho-mqtt 등 개별 검증된 라이브러리 사용.

## Alternatives (대안)

`archive/technical-design-review.md`에 기록된 초기 비교:

| 방안 | 채택 | 이유 |
|------|------|------|
| Vector(Rust) 바이너리 래핑 | ❌ | 외부 프로세스 IPC 오버헤드, Go 세밀 제어 불가 |
| Bento를 실행 엔진으로 직접 사용 | ❌ | 자체 Actor/실행 모델 설계 포기하게 됨 |
| 순수 자체 구현(Actor + 커넥터 직접) | ❌ | 커넥터 구현·검증 비용 높음 |
| **하이브리드(자체 실행 + Bento 어댑터)** | ✅ | 실행은 자체 제어, 커넥터는 레버리지 여지 |

## Consequences (결과·트레이드오프)

**긍정:**
- 라이선스 안전(MIT). Benthos 라이선스 변경 리스크 회피.
- 커넥터를 자체 구현하므로 Go 런타임에서 세밀한 제어(배치·병렬·체크포인트·복원력)가 가능. 별도 프로세스 IPC 없음.

**부정 / 현실:**
- **Bento 의존성은 현재 사실상 미사용에 가깝다.** 어댑터 계층(`adapter/bento/`)만 존재하고, 실운영 경로는 전부 자체 커넥터다. 즉 "Bento의 검증된 커넥터를 쓴다"는 초기 명제는 **아직 실현되지 않았다** — 자체 16 source/9 sink로 커버 중.
- Bento 커넥터를 직접 노출하려면 어댑터를 실제 파이프라인 경로에 연결하는 추가 작업이 필요하다.

## 재검토 트리거

- Bento 어댑터를 실제로 안 쓴다면(계속 자체 구현으로 충분하다면), 의존성 유지 비용 대비 이득을 재평가 → 의존성 제거 또는 어댑터 실연결 중 택일.
- 새 커넥터 요구가 많아지고 Bento 생태계가 그걸 이미 제공한다면 → 어댑터 실연결로 전환 검토.

## Evidence (근거)

- `pipeline-core/go.mod` — `warpstreamlabs/bento` 의존성.
- `pipeline-core/pkg/adapter/bento/adapter.go` — 어댑터 계층(현재 유일한 Bento 접점).
- `pipeline-core/pkg/source/`, `pkg/output/` — 자체 커넥터 구현(canonical).
- `docs/archive/technical-design-review.md` — 초기 대안 비교(Vector/Bento/Actor/하이브리드).
- **기록 없음**: "어댑터만 두고 자체 구현으로 간" 전환 시점·근거는 커밋/문서에 명시되지 않음. 위 Consequences는 현 코드 상태에서 사후 정리한 것.
