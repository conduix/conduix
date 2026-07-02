# Conduix 남은 작업 체크리스트

> 2026-07-02 기준. 브랜치: `feat/platform-value-improvements`
> 이번 세션 완료분: 보안 수정, Redis dedup, actor 엔진 제거, agent 워크플로우 제어+claim,
> YAML export/import(템플릿), stage 실행 통합, pause/resume, retry, 관측성(Prometheus+slog).

## 진행 중: 실패 처리 & 복원력 (claim 재배치 + 실패 카운트/서킷브레이커 + DLQ)

설계 원칙(사용자 지시):
- DLQ는 문제 해결이 아니라 적재일 뿐 → 근본은 **실패 카운트 관리 + 서킷 브레이크**.
- **로그 + 실패 카운트는 필수.**
- 실패가 임계 이상 쌓이면 계속 도는 게 오히려 부하/문제 → **설정 기준으로 서킷 브레이크 → 작업 에러 종료.**
- **DLQ 설정이 있으면** 실패 내역을 DLQ로 적재. **DLQ 여부와 무관하게 서킷 브레이크는 설정으로 제어.**
- 추가 방어/안전운영 기법 조사·적용.

- [x] **claim 재배치(고아 실행 감지)**: SchedulerService.detectStaleExecutions — 주기적으로 running 실행 중 살아있는 에이전트 하트비트에 없는(고아) 실행을 failed로 전이 + 로그. 조용한 유실 방지. 완전 자동 재실행은 checkpoint 중복 위험으로 보류(failed 전이로 인지 가능하게). 판정 로직 테스트.
- [x] **실패 카운트 + 서킷 브레이크**: `failureGuard`(pkg/executor/failure_guard.go) — 연속/누적 실패 카운트, CircuitBreakerPolicy 임계 초과 시 서킷 오픈 → 실행 에러 종료. record/batch 양 경로 적용. 테스트 4건.
- [x] **DLQ 연동**: FailurePolicy.DLQ 설정 시 실패 레코드를 DLQOutput으로 적재. 서킷과 독립 동작.
- [x] **구조화 로그**: 실패/서킷트립 이벤트를 workflow_id/pipeline_id 상관키 포함 slog로 기록. Prometheus circuit_tripped_total 메트릭.
- [x] 추가 방어: retry에 **지수 백오프 + jitter**(backoffWithJitter, 상한 5분) — 동시 실패 시 재시도 몰림(thundering herd) 방지. 테스트.
- [x] 추가 방어: executeGroup 고루틴 **panic recover** — 한 파이프라인 panic이 에이전트 전체를 죽이지 않고 실행을 failed로 보고.
- [ ] 추가 방어(후속): 실행 타임아웃 상한 설정화(현재 하드코딩 10분)

## 남은 작업 (우선순위)

- [x] **목록 페이지 YAML 가져오기 다이얼로그**: Workflows 목록에 "YAML 가져오기" 버튼 + 다이얼로그(YAML 붙여넣기 + project_id override). 재사용 기능 완결(상세=export/clone, 목록=import).
- [x] **PostgreSQL CDC 처리**: 실제 pglogrepl 구현은 무거운 투자(새 의존성+PG 복제 인프라)라 실사용 확인 전 보류. 대신 NewCDCSource에서 postgres/미지원 드라이버를 **생성 시점 조기 거부** + 명확한 대안 안내(Kafka/Debezium 경유). "실행 후 미동작 발견" 방지. 테스트 갱신.
- [ ] **e2e 통합 테스트** (L): MySQL+Redis+control-plane+agent 기동 후 실제 워크플로우 실행 검증
- [~] **stage 하드코딩 완전통일** (보류 결정): 조사 결과 stream registry에 select/exclude/schema_validate가 없고 filter/remap 시맨틱도 executor와 상이. 흡수하려면 stream에 3개 stage 신규 + remap/filter 시맨틱 확장 필요 → 회귀 위험 큼, 순이득 작음. executor 5개 case가 canonical(우선 적용)이라 divergence는 잠재 리스크일 뿐 실버그 아님. 강제 통합은 "과도한 추상화" 리스크로 보류. (통합하려면 stream 쪽을 executor 시맨틱에 맞춰 확장하는 방향)
- [ ] **나머지 fmt.Printf → slog 전면 전환** (M, 선택): 추적 핵심은 이미 커버, 나머지는 저가치

## 완료 (이번 세션)

- [x] 보안: CORS/OAuth redirect/타입단언/pagination
- [x] Redis dedup 실구현 + 설정화
- [x] actor 실행 엔진 제거 (~4,700 LOC)
- [x] agent 워크플로우 제어 + SETNX claim (double-execution 수정)
- [x] 워크플로우 YAML export/import (= 템플릿)
- [x] stage 실행 통합 (28개 + native가 워크플로우에서 실행)
- [x] pause/resume (real pause gate)
- [x] retry (FailureActionRetry 구현)
- [x] web-ui: pause/resume·처리량·YAML export/clone
- [x] 관측성: Prometheus 메트릭 + 구조화 로깅
