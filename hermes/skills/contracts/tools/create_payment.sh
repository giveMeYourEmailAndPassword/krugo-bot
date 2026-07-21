#!/usr/bin/env bash
# create_payment.sh — создаёт pending клиентский платёж.
#
# JSON через stdin:
#   {"operation_id":"KRUGOSVET-123:payment", "contract_id":"...",
#    "amount":500, "currency":"USD", "method":"наличные",
#    "date":"2026-07-21", "comment":"аванс"}
#
# Idempotency: operation_id обязателен. При повторном вызове с тем же
# operation_id: auth + GET сохранённой записи + verify полей. Если запись
# валидна → OK. Если отсутствует/bad → hard error (НЕ создавать новую).
# Весь цикл (check→POST→save→verify) под одним flock.
#
# stdout: OK-строка или ERROR (exit 1).

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

# ── Read stdin ──
INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
if [ -z "$OPERATION_ID" ]; then
  echo "ERROR: operation_id не указан (нужен для idempotency)" >&2; exit 1
fi

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
AMOUNT=$(echo "$INPUT" | jq -r '.amount // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // empty')
METHOD_NAME=$(echo "$INPUT" | jq -r '.method // empty')
PAYMENT_DATE=$(echo "$INPUT" | jq -r '.date // empty')
COMMENT=$(echo "$INPUT" | jq -r '.comment // "Платёж по договору"')

# ── Validate input ──
[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id не указан" >&2; exit 1; }
[ -z "$AMOUNT" ] && { echo "ERROR: amount не указан" >&2; exit 1; }
[ -z "$CURRENCY" ] && { echo "ERROR: currency не указан" >&2; exit 1; }
[ -z "$METHOD_NAME" ] && { echo "ERROR: method не указан" >&2; exit 1; }
[ -z "$PAYMENT_DATE" ] && { echo "ERROR: date не указан" >&2; exit 1; }

CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')
case "$CURRENCY" in
  USD|EUR|KGS) ;;
  *) echo "ERROR: валюта должна быть USD, EUR или KGS (получено: $CURRENCY)" >&2; exit 1 ;;
esac

# ── Idempotency: single flock for entire operation ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

# ── Auth (needed for both branches) ──
TOKEN=$(pb_auth) || exit 1

# ── Idempotent branch: journal exists → GET + verify, no new POST ──
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")
  VERIFY=$(pb_get "$TOKEN" "payments" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена в PB (journal повреждён)" >&2
    exit 1
  }
  V_STATUS=$(echo "$VERIFY" | jq -r '.status')
  V_OFFICE=$(echo "$VERIFY" | jq -r '.office_id')
  V_METHOD=$(echo "$VERIFY" | jq -r '.payment_method_id')
  V_RATE=$(echo "$VERIFY" | jq -r '.exchange_rate_kgs')
  [ "$V_STATUS" != "pending" ] && { echo "ERROR: idempotent retry — статус=$V_STATUS (ожидается pending)" >&2; exit 1; }
  [ -z "$V_OFFICE" ] || [ "$V_OFFICE" = "null" ] && { echo "ERROR: idempotent retry — office_id пустой" >&2; exit 1; }
  [ -z "$V_METHOD" ] || [ "$V_METHOD" = "null" ] && { echo "ERROR: idempotent retry — payment_method_id пустой" >&2; exit 1; }
  [ -z "$V_RATE" ] || [ "$V_RATE" = "null" ] || [ "$V_RATE" = "0" ] && { echo "ERROR: idempotent retry — exchange_rate_kgs=$V_RATE" >&2; exit 1; }
tool_trace "create_payment" "$OPERATION_ID" "$PRIOR_ID"
  echo "OK (idempotent): платёж $PRIOR_ID уже создан (pending, rate=$V_RATE, office=$V_OFFICE)"
  exit 0
fi

# ── Fresh branch: prechecks → POST → atomic save → verify → audit ──

# Check contract + get office
OFFICE_ID=$(pb_check_contract "$TOKEN" "$CONTRACT_ID") || exit 1

# Resolve payment method
METHOD_ID=$(pb_resolve_method "$TOKEN" "$METHOD_NAME" "$CURRENCY") || exit 1

# Get exchange rate
RATE_KGS=$(pb_get_rate "$CURRENCY") || exit 1

# POST payment
PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" \
  --argjson amount "$AMOUNT" \
  --arg currency "$CURRENCY" \
  --arg mid "$METHOD_ID" \
  --arg oid "$OFFICE_ID" \
  --argjson rate "$RATE_KGS" \
  --arg comment "$COMMENT" \
  --arg pdate "$PAYMENT_DATE" \
  '{
    contract_id: $cid,
    amount: $amount,
    currency: $currency,
    payment_method_id: $mid,
    office_id: $oid,
    exchange_rate_kgs: $rate,
    status: "pending",
    is_confirmed: false,
    comment: $comment,
    payment_date: $pdate
  }')

RESULT=$(pb_create "$TOKEN" "payments" "$PAYLOAD") || exit 1
PAYMENT_ID=$(echo "$RESULT" | jq -r '.id // empty')
[ -z "$PAYMENT_ID" ] && { echo "ERROR: PB не вернул id платежа" >&2; exit 1; }

# ── Atomic journal save IMMEDIATELY after POST ──
# Crash here → retry finds no journal → re-POST. If PB already has the
# record, the tool's precheck or PB hooks should catch the duplicate.
# (Future: PB unique index on a client-generated idempotency key.)
journal_save_atomic "$JOURNAL_FILE" "$PAYMENT_ID"

# ── GET-verify ──
VERIFY=$(pb_get "$TOKEN" "payments" "$PAYMENT_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
V_OFFICE=$(echo "$VERIFY" | jq -r '.office_id')
V_METHOD=$(echo "$VERIFY" | jq -r '.payment_method_id')
V_RATE=$(echo "$VERIFY" | jq -r '.exchange_rate_kgs')

[ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS (ожидается pending)" >&2; exit 1; }
[ -z "$V_OFFICE" ] || [ "$V_OFFICE" = "null" ] && { echo "ERROR: office_id пустой" >&2; exit 1; }
[ -z "$V_METHOD" ] || [ "$V_METHOD" = "null" ] && { echo "ERROR: payment_method_id пустой" >&2; exit 1; }
[ -z "$V_RATE" ] || [ "$V_RATE" = "null" ] || [ "$V_RATE" = "0" ] && { echo "ERROR: exchange_rate_kgs=$V_RATE" >&2; exit 1; }

# ── Audit ──
pb_audit "$TOKEN" "$CONTRACT_ID" "create_payment" \
  "Платёж $AMOUNT $CURRENCY (rate=$RATE_KGS, method=$METHOD_NAME)"

tool_trace "create_payment" "$OPERATION_ID" "$PAYMENT_ID"
echo "OK: платёж $PAYMENT_ID создан (pending, $AMOUNT $CURRENCY, rate=$RATE_KGS, office=$OFFICE_ID)"
