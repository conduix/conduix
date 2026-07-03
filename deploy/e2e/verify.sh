#!/usr/bin/env bash
# 로컬 E2E 데이터 흐름 검증.
# 샘플 워크플로우 "[bulk] MySQL → MySQL" 를 API 로 트리거하고, mock 타깃 MySQL 의
# targetdb.orders_out 에 행이 적재되었는지 확인한다. (소스→stage→싱크 관통 검증)
#
# 인증: OAuth 없이 로컬에서 JWT 를 직접 서명해 사용한다.
#   JWT_SECRET 은 values-e2e.yaml 의 secrets.jwtSecret 과 일치해야 한다(HS256).
set -euo pipefail

NS="${1:-conduix-e2e}"
RELEASE="${2:-conduix}"
WORKFLOW_NAME="${E2E_WORKFLOW:-[bulk] MySQL → MySQL}"
FWD_PORT="${E2E_FWD_PORT:-38080}"
API="http://localhost:${FWD_PORT}"

log() { printf '\033[36m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL:\033[0m %s\n' "$*" >&2; exit 1; }

# JWT_SECRET 은 반드시 클러스터 Secret 에서 읽는다.
# (셸에 상속된 JWT_SECRET 값이 클러스터와 다르면 서명 불일치로 401 → 신뢰 불가.)
JWT_SECRET="$(kubectl -n "$NS" get secret "${RELEASE}-control-plane-secrets" \
  -o jsonpath='{.data.JWT_SECRET}' 2>/dev/null | base64 -d)"
[ -n "$JWT_SECRET" ] || fail "클러스터에서 JWT_SECRET 을 읽지 못함 (secret ${RELEASE}-control-plane-secrets)"

# control-plane 은 ClusterIP 이므로 검증 동안 임시 port-forward 를 띄운다.
FWD_PID=""
cleanup() { [ -n "$FWD_PID" ] && kill "$FWD_PID" 2>/dev/null || true; }
trap cleanup EXIT
start_forward() {
  kubectl -n "$NS" port-forward "svc/${RELEASE}-control-plane" "${FWD_PORT}:8080" >/dev/null 2>&1 &
  FWD_PID=$!
  for _ in $(seq 1 20); do
    curl -fsS "$API/health" >/dev/null 2>&1 && return 0
    sleep 1
  done
  fail "port-forward 로 API($API) 연결 실패"
}
log "control-plane port-forward 시작 (:$FWD_PORT)"
start_forward

# --- HS256 JWT 서명 (admin role, 1h 만료) ---
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
mint_jwt() {
  local now exp header payload signing sig
  now=$(date +%s); exp=$((now + 3600))
  header='{"alg":"HS256","typ":"JWT"}'
  payload="{\"sub\":\"e2e-admin\",\"email\":\"admin@example.com\",\"role\":\"admin\",\"iat\":${now},\"exp\":${exp}}"
  signing="$(printf '%s' "$header" | b64url).$(printf '%s' "$payload" | b64url)"
  sig=$(printf '%s' "$signing" | openssl dgst -binary -sha256 -hmac "$JWT_SECRET" | b64url)
  printf '%s.%s' "$signing" "$sig"
}

TOKEN="$(mint_jwt)"
AUTH="Authorization: Bearer $TOKEN"

log "샘플 워크플로우 조회: $WORKFLOW_NAME"
WORKFLOWS_JSON="$(curl -fsS -H "$AUTH" "$API/api/v1/workflows")" || fail "workflow 목록 조회 실패 (인증/JWT_SECRET 불일치 가능)"
WF_ID="$(printf '%s' "$WORKFLOWS_JSON" | python3 -c '
import sys, json
name = sys.argv[1]
data = json.load(sys.stdin).get("data", [])
m = [w for w in data if w.get("name") == name]
print(m[0]["id"] if m else "")
' "$WORKFLOW_NAME")"
[ -n "$WF_ID" ] || fail "워크플로우 '$WORKFLOW_NAME' 없음. seed 가 돌았는지 확인."
log "workflow id: $WF_ID"

log "워크플로우 트리거"
TRIG="$(curl -fsS -X POST -H "$AUTH" "$API/api/v1/workflows/$WF_ID/trigger")" || fail "trigger 실패"
EXEC_ID="$(printf '%s' "$TRIG" | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["id"])')"
log "execution id: $EXEC_ID"

log "실행 완료 대기 (최대 120s)"
for i in $(seq 1 40); do
  ST="$(curl -fsS -H "$AUTH" "$API/api/v1/workflows/$WF_ID/executions/$EXEC_ID" \
        | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["status"])')"
  case "$ST" in
    completed) log "실행 상태: completed"; break ;;
    error|stopped) fail "실행 실패 상태: $ST" ;;
    *) printf '.'; sleep 3 ;;
  esac
  [ "$i" -eq 40 ] && fail "타임아웃: 마지막 상태=$ST"
done

log "mock 타깃 DB 검증: targetdb.orders_out 행 수"
POD="$(kubectl -n "$NS" get pod -l app.kubernetes.io/component=mock-mysql -o jsonpath='{.items[0].metadata.name}')"
[ -n "$POD" ] || fail "mock-mysql pod 없음"
COUNT="$(kubectl -n "$NS" exec "$POD" -- \
  mysql -uroot -prootpassword -N -B -e 'SELECT COUNT(*) FROM targetdb.orders_out' 2>/dev/null)" \
  || fail "mock-mysql 쿼리 실패"

log "orders_out 행 수: $COUNT"
if [ "${COUNT:-0}" -gt 0 ]; then
  printf '\033[32mPASS:\033[0m 소스→stage→싱크 데이터 흐름 확인 (orders_out=%s rows)\n' "$COUNT"
else
  fail "orders_out 가 비어있음 — 데이터가 싱크까지 흐르지 않음"
fi
