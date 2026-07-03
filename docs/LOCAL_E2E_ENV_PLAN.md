# 로컬 K8s 통합 테스트 환경 — 구축 계획 (이어가기용)

> 목적: 로컬에서 **모든 모듈을 유기적으로** 쉽게 띄워 테스트. mock 데이터 소스 포함.
> 상태: **구현·검증 완료** (2026-07-03). MySQL→MySQL, REST→MySQL 파이프라인이 mock 소스→stage→싱크로 실제 데이터 관통 확인.
> 브랜치: `feat/platform-value-improvements`.

## 완료 요약 (Phase 1~4 ✅)

`make e2e-up` → colima 이미지 4종 빌드 → helm install(mock 4종 포함) → `make e2e-verify` 로 검증.
검증 결과: `targetdb.orders_out` 에 mock 소스 데이터가 변환(price 문자열→숫자, status 소문자화)되어 적재됨.

### E2E 도중 발견·수정한 실제 코드 버그 (테스트 환경 문제 아님)
1. **web-ui Dockerfile Node 18 고정** → Vite 8 빌드 불가(`CustomEvent`). `node:22-alpine` 로 수정. (프로덕션 이미지 빌드도 깨져 있던 상태)
2. **`TriggerNow` 가 `TargetClusterID` 미설정** → Redis broadcast 채널로 새어 agent(cluster:default:execute 구독) 미수신. cluster 확정+주입으로 수정. (`/start` 는 정상이었음 → 본질적 중복을 `services.ResolveExecutionCluster` 로 일원화)
3. **`is_default=true` cluster 생성 경로 부재** → cluster_id 미지정 워크플로우가 영구 실행 불가. agent 첫 cluster 자동 생성 시 default 지정.
4. **orphan 처리 시 `workflow.status="running"` 미정리** → 재트리거 영구 차단. idle 복구 추가.
5. **migration Job `command` 경로/플래그 불일치**(`/app/control-plane -migrate` vs 실제 `control-plane-server --migrate`) → exec 실패. ENTRYPOINT + `args: [--migrate]` 로 수정.
6. **`--migrate` 가 exit 안 함**(서버로 진행) → migration Job 무한 대기. migrate-only 모드 exit 추가.
7. **pipeline-batch-job Dockerfile `plugin-sdk` 미복사** → `go mod download` 실패. 복사 + arm64 크로스컴파일 추가.
8. **agent `RUNNER_IMAGE` 미주입** → batch 실행 시 "runner image is required". helm values+deployment 로 주입.
9. **runtime cgroup limit 미인식** → 3개 서버 바이너리에 `automaxprocs`/`automemlimit` blank import. (control-plane 로그로 GOMAXPROCS/GOMEMLIMIT 적용 확인)

## 확정된 결정 (사용자 선택)

- **K8s 런타임**: **기존 Colima K8s 재사용**
  - ArgoCD/operator 경로는 피하고 **helm으로 직접 배포**(경량).
  - 로컬 빌드 이미지는 colima 데몬에 로드/푸시 필요.
  - 기존 고정 NodePort 재사용: Web UI `30000`, Control Plane API `30080`.
- **Mock 데이터 소스 4종 모두 구성**:
  1. **Mock REST API** — 샘플 파이프라인의 REST 소스용. 경량 서버(nginx static JSON 또는 작은 Go)로 고정 JSON 응답. `seed.go`의 `api.example.com` placeholder 대체.
  2. **Mock MySQL (소스 데이터)** — MySQL + `init.sql`로 `orders`/`events` 등 seed 테이블·행 주입. bulk MySQL→MySQL 파이프라인이 실제 row를 읽어 변환.
  3. **Mock Kafka (토픽+메시지)** — Kafka + seed 메시지 프로듀서(Job/init). cdc Kafka→MySQL 샘플용.
  4. **Elasticsearch (싱크)** — 샘플이 ES로 적재 → index 조회로 결과 검증.

## 이미 있는 자산 (재사용) — 근거 포함

| 자산 | 위치 | 상태 |
|------|------|------|
| docker-compose 로컬 스택 | `docker-compose.yml` (mysql:8.0@3307, redis:7@6379, control-plane@8080, agent, web-ui@3000:80; profile `with-kafka`/`with-elk`) | 완전 |
| Helm 차트 | `deploy/helm/conduix/` (templates 17개, values.yaml) | 완전 |
| 로컬 values | `deploy/helm/conduix/values-local.yaml`, `values-local-kafka.yaml` | 완전 |
| 4개 모듈 Dockerfile | `deploy/docker/Dockerfile.control-plane`(8080), `Dockerfile.agent`(8081), `Dockerfile.web-ui`(80/nginx), `pipeline-batch-job/Dockerfile`(8082) | 완전 |
| K8s migration Job | `deploy/helm/conduix/templates/migration-job.yaml` (post-install hook, wait-for-mysql, `--migrate`) | 완전 |
| DB 스키마 | GORM auto-migrate (`control-plane cmd/server`, `--migrate`) | 완전 |
| MySQL init | `deploy/docker/mysql/init.sql` (DB 생성만; 테이블은 GORM) | 부분 |
| Seed(샘플 6종) | `control-plane/internal/seed/seed.go` — bulk 3(MySQL→MySQL, REST→MySQL, REST→PostgreSQL) + cdc 3(REST polling, Kafka, MySQL CDC), js_script stage 3종. **접속정보 placeholder(api.example.com, localhost:3306)** | 부분 |
| Makefile 로컬 타깃 | `infra-up`(mysql+redis), `dev`, `docker-build`, `up`/`down` | 완전 |

## 새로 만들 것 (gap)

1. **Mock REST API 컨테이너** — 예: `GET /users`, `GET /events` → 고정 JSON 배열. seed.go의 REST 소스 URL과 일치.
2. **Mock 소스 MySQL** — `init.sql`에 `orders`/`events` CREATE + INSERT(수십~백 행).
3. **Mock Kafka + seed 프로듀서** — topic `events`에 메시지 N건 주입(Job 또는 init container).
4. **Elasticsearch 싱크** (선택 검증용).
5. **단일 진입점** — `make e2e-up` / `make e2e-down` (colima 이미지 로드 → helm install(mock 포함) → port-forward/NodePort 안내 → 검증).
6. **seed.go placeholder → 클러스터 DNS 프리셋** — 예: `http://mock-rest.conduix.svc:8080`, `mock-mysql.conduix.svc:3306`, `mock-kafka.conduix.svc:9092`. (환경/values로 주입, 하드코딩 지양 — Registry/설정 분리 원칙.)
7. **E2E 검증 체크리스트** — 배포 후 각 샘플 파이프라인 실행 → 결과 확인 항목.

## 구현 순서(안)

- **Phase 1** ✅: mock 4종을 `deploy/helm/conduix/templates/mocks.yaml` 에 추가(`mocks.enabled` 게이트, 프로덕션 기본 false). `values.yaml` 에 `mocks` 블록.
- **Phase 2** ✅: `seed.go` 를 `SEED_*` env 로 파라미터화(`loadEndpoints`/`envOr`). 미설정 시 placeholder 폴백(프로덕션 불변). DNS 는 `configmaps.yaml` 이 릴리스명에서 파생해 주입. 단위테스트 `seed_endpoints_test.go`.
- **Phase 3** ✅: `make e2e-up`/`e2e-down`/`e2e-images`/`e2e-status`/`e2e-verify`/`e2e-restart` (Makefile). colima docker 데몬 공유라 별도 load 불필요. `values-e2e.yaml` 신규.
- **Phase 4** (진행중): `deploy/e2e/verify.sh` — HS256 JWT 자가서명(OAuth 우회, JWT_SECRET=values-e2e 와 일치) → 샘플 트리거 → mock 타깃 DB 행 확인. **실배포 검증 남음**.

### 구현 산출물
| 파일 | 내용 |
|------|------|
| `deploy/helm/conduix/templates/mocks.yaml` | mock-rest(nginx JSON), mock-mysql(seed+타깃테이블), mock-kafka(KRaft+seed Job), mock-es |
| `deploy/helm/conduix/templates/configmaps.yaml` | `mocks.enabled` 시 `SEED_MOCK_*` env 주입 (릴리스명 파생 DNS) |
| `deploy/helm/conduix/values.yaml` | `mocks:{enabled:false,...}` 게이트 |
| `deploy/helm/conduix/values-e2e.yaml` | 인프라+mock+앱(NodePort 30000/30080, tag=e2e) |
| `control-plane/internal/seed/seed.go` | `endpoints`/`loadEndpoints`/`envOr`, 파이프라인 접속정보 파라미터화 |
| `control-plane/internal/seed/seed_endpoints_test.go` | 폴백/오버라이드 단위테스트 (통과) |
| `deploy/e2e/verify.sh` | E2E 데이터 흐름 검증 스크립트 |
| `Makefile` | `e2e-*` 타깃 |

### 알려진 제약
- REST→PostgreSQL 샘플은 mock Postgres 가 없어 E2E 미검증(placeholder 유지). 나머지 5종은 mock 대상 존재.
- seed 는 최초 마이그레이션 1회만 실행 → `e2e-down`(namespace+PVC 삭제) 후 `e2e-up` 해야 SEED_* 반영된 샘플이 새로 시딩됨.
- Colima 2 CPU/8Gi 여유 빠듯(추정 스택 ~5Gi). Pending 발생 시 Bitnami request 하향 필요.

## 주의/리스크

- **"쉽게"가 최우선 제약**: operator/ArgoCD 경유 금지, helm 직접. mock은 무거운 실인프라 대신 최소 seed만.
- Kafka는 리소스가 무거움 — Colima 리소스 여유 확인 필요(현 문서상 replica 최소 권장).
- seed 접속정보 하드코딩 금지: mock DNS는 values/env로 주입.
- 배포 후 반드시 **실제 파이프라인 1개 이상을 돌려** 소스→싱크 데이터 흐름을 확인해야 "완료".

## 셸 노이즈 이슈(별건, 압축 실패 원인 추정)
- 세션 내내 `setValueForKeyFakeAssocArray:27: command not found: _encode` 로그가 모든 Bash 출력에 붙음 → 사용자 zsh 프로필 초기화에서 발생 추정. 깨진 유니코드가 섞여 `/compact`가 400(invalid high surrogate)로 실패. 근본 수정 시 향후 출력 깨끗해짐.
