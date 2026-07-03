#!/usr/bin/env bash
# 로컬 E2E realtime 데이터 흐름 검증 (Kafka → MySQL).
# "[cdc] Kafka → MySQL" 워크플로우를 /start 로 기동한 뒤(agent 상주 스트리밍),
# cdc.orders 토픽에 메시지를 produce → mock 타깃 MySQL(targetdb.orders_cdc) 적재 확인 → /stop.
#
# Kafka source 기본 offset 은 latest 라 "start 이후 도착 메시지"만 읽는다.
# 따라서 start → produce 순서가 중요하다.
set -euo pipefail

NS="${1:-conduix-e2e}"
RELEASE="${2:-conduix}"
FWD_PORT="${E2E_FWD_PORT:-38080}"
API="http://localhost:${FWD_PORT}"
WORKFLOW_NAME="${E2E_WORKFLOW:-[cdc] Kafka → MySQL}"
TOPIC="cdc.orders"

log() { printf '\033[36m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL:\033[0m %s\n' "$*" >&2; exit 1; }

JWT_SECRET="$(kubectl -n "$NS" get secret "${RELEASE}-control-plane-secrets" \
  -o jsonpath='{.data.JWT_SECRET}' 2>/dev/null | base64 -d)"
[ -n "$JWT_SECRET" ] || fail "JWT_SECRET 읽기 실패"

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
mint_jwt() {
  local now header payload signing sig
  now=$(date +%s)
  header='{"alg":"HS256","typ":"JWT"}'
  payload="{\"sub\":\"e2e-admin\",\"role\":\"admin\",\"iat\":${now},\"exp\":$((now+900))}"
  signing="$(printf '%s' "$header" | b64url).$(printf '%s' "$payload" | b64url)"
  sig=$(printf '%s' "$signing" | openssl dgst -binary -sha256 -hmac "$JWT_SECRET" | b64url)
  printf '%s.%s' "$signing" "$sig"
}
AUTH="Authorization: Bearer $(mint_jwt)"

FWD_PID=""
cleanup() { [ -n "$FWD_PID" ] && kill "$FWD_PID" 2>/dev/null || true; }
trap cleanup EXIT
kubectl -n "$NS" port-forward "svc/${RELEASE}-control-plane" "${FWD_PORT}:8080" >/dev/null 2>&1 &
FWD_PID=$!
for _ in $(seq 1 20); do curl -fsS "$API/health" >/dev/null 2>&1 && break; sleep 1; done

KPOD=$(kubectl -n "$NS" get pod -l app.kubernetes.io/component=mock-kafka -o jsonpath='{.items[0].metadata.name}')
MPOD=$(kubectl -n "$NS" get pod -l app.kubernetes.io/component=mock-mysql --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')
[ -n "$KPOD" ] && [ -n "$MPOD" ] || fail "mock-kafka/mock-mysql pod 없음"

log "타깃 테이블 초기화 (targetdb.orders_cdc)"
kubectl -n "$NS" exec "$MPOD" -- mysql -uroot -prootpassword -e "TRUNCATE targetdb.orders_cdc" 2>/dev/null || true

log "워크플로우 조회: $WORKFLOW_NAME"
WF_ID="$(curl -fsS -H "$AUTH" "$API/api/v1/workflows" | python3 -c '
import sys,json
name=sys.argv[1]; d=json.load(sys.stdin).get("data",[])
m=[w for w in d if w.get("name")==name]; print(m[0]["id"] if m else "")' "$WORKFLOW_NAME")"
[ -n "$WF_ID" ] || fail "워크플로우 '$WORKFLOW_NAME' 없음"
log "workflow id: $WF_ID"

log "워크플로우 /start (realtime 스트리밍 기동)"
curl -fsS -X POST -H "$AUTH" "$API/api/v1/workflows/$WF_ID/start" >/dev/null || fail "start 실패"

# consumer group 이 실제로 조인(Stable, member>=1)할 때까지 대기해야 한다.
# kafka source 기본 offset 이 latest 라, group join 완료 전에 produce 하면 그 메시지를 놓친다.
log "consumer group 조인 대기 (최대 40s)"
for i in $(seq 1 20); do
  MEMBERS=$(kubectl -n "$NS" exec "$KPOD" -- kafka-consumer-groups.sh --bootstrap-server localhost:9092 \
    --describe --group conduix-sample --state 2>/dev/null | grep -v "_encode\|_decode" | awk '/Stable/{print $NF}')
  [ "${MEMBERS:-0}" -ge 1 ] && { log "group joined (members=$MEMBERS)"; break; }
  printf '.'; sleep 2
done
printf '\n'
sleep 3  # join 후 fetch 시작 안정화

# SQL 싱크는 batch_size(기본 100) 단위로만 flush 하고 주기적 time-flush 가 없다.
# 소량이면 실행 중 flush 되지 않고 종료 시에만 나가는데, 종료(stop) 경로는 취소된
# context 로 flush 를 시도해 실패한다(실제 결함). realtime 정상 시나리오는 지속 유입이므로
# 배치를 채우도록 120건을 produce → 실행 중 flush 트리거로 검증한다.
MSG_COUNT=120
log "cdc.orders 토픽에 ${MSG_COUNT}건 produce (batch_size 초과 → 실행 중 flush)"
# 메시지는 로컬에서 생성(payload_json 이스케이프를 셸에서 안 깨뜨리려고 로컬 파일로 만들어 stdin 전달).
# python json.dumps 로 payload_json(중첩 JSON 문자열)을 올바르게 이스케이프한다.
MSGFILE="$(mktemp)"
trap 'rm -f "$MSGFILE"' RETURN
python3 - "$MSG_COUNT" > "$MSGFILE" <<'PY'
import json, sys
n = int(sys.argv[1])
for i in range(1, n + 1):
    payload = json.dumps({"user": {"email": f"u{i}@example.com"}, "event": {"type": "order.created"}})
    print(json.dumps({"id": i, "first_name": f"U{i}", "last_name": "K", "status": "NEW", "payload_json": payload}))
PY
kubectl -n "$NS" exec -i "$KPOD" -- kafka-console-producer.sh --bootstrap-server localhost:9092 --topic "$TOPIC" < "$MSGFILE" 2>/dev/null
log "produce 완료 (${MSG_COUNT}건)"

log "적재 대기 및 확인 (최대 40s, batch_size 100 flush 기대)"
COUNT=0
for i in $(seq 1 20); do
  COUNT="$(kubectl -n "$NS" exec "$MPOD" -- mysql -uroot -prootpassword -N -B \
    -e "SELECT COUNT(*) FROM targetdb.orders_cdc" 2>/dev/null | tr -d '[:space:]')"
  [ "${COUNT:-0}" -ge 100 ] && break
  printf '.'; sleep 2
done
printf '\n'

log "워크플로우 /stop"
curl -fsS -X POST -H "$AUTH" "$API/api/v1/workflows/$WF_ID/stop" >/dev/null 2>&1 || \
  printf '\033[33mWARN:\033[0m stop 호출 실패(무시)\n'

log "orders_cdc 행 수: ${COUNT:-0}"
if [ "${COUNT:-0}" -ge 100 ]; then
  printf '\033[32mPASS:\033[0m Kafka → stage → MySQL realtime 흐름 확인 (orders_cdc=%s rows)\n' "$COUNT"
else
  fail "orders_cdc 에 배치 flush 가 안 됨 (got ${COUNT:-0}, expected >=100)"
fi
