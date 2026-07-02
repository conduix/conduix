# 로컬 K8s 통합 테스트 환경 — 구축 계획 (이어가기용)

> 목적: 로컬에서 **모든 모듈을 유기적으로** 쉽게 띄워 테스트. mock 데이터 소스 포함.
> 상태: **설계 확정, 구현 착수 전** (2026-07-03). 대화 압축 실패로 새 세션에서 이어감.
> 브랜치: `feat/platform-value-improvements` (직전 커밋 `941983b`).

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

- **Phase 1**: mock 컨테이너 3종(REST/MySQL/Kafka) + ES를 helm 차트에 서브차트/템플릿으로 추가(로컬 values에서만 켬).
- **Phase 2**: seed.go(또는 values)에서 mock 서비스 DNS를 쓰도록 파이프라인 접속정보 파라미터화.
- **Phase 3**: `make e2e-up` 단일 타깃 — colima 이미지 로드 + `helm upgrade --install -f values-local.yaml`(+mock) + 상태 대기 + 접속 안내.
- **Phase 4**: 검증 스크립트/체크리스트 — 샘플 파이프라인 실행하고 mock 데이터가 소스→stage→싱크로 흐르는지 확인.

## 주의/리스크

- **"쉽게"가 최우선 제약**: operator/ArgoCD 경유 금지, helm 직접. mock은 무거운 실인프라 대신 최소 seed만.
- Kafka는 리소스가 무거움 — Colima 리소스 여유 확인 필요(현 문서상 replica 최소 권장).
- seed 접속정보 하드코딩 금지: mock DNS는 values/env로 주입.
- 배포 후 반드시 **실제 파이프라인 1개 이상을 돌려** 소스→싱크 데이터 흐름을 확인해야 "완료".

## 셸 노이즈 이슈(별건, 압축 실패 원인 추정)
- 세션 내내 `setValueForKeyFakeAssocArray:27: command not found: _encode` 로그가 모든 Bash 출력에 붙음 → 사용자 zsh 프로필 초기화에서 발생 추정. 깨진 유니코드가 섞여 `/compact`가 400(invalid high surrogate)로 실패. 근본 수정 시 향후 출력 깨끗해짐.
