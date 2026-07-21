#!/usr/bin/env bash
# create_operator_request.sh — создаёт pending запрос на оплату поставщику.
# JSON: {"operation_id":"chat:msg:operator:1", "contract_id":"...",
#        "provider":"ANEX", "number":"111222", "amount":4500,
#        "currency":"USD", "type":"полный остаток", "comment":"..."}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id не указан" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
PROVIDER_NAME=$(echo "$INPUT" | jq -r '.provider // empty')
APP_NUMBER=$(echo "$INPUT" | jq -r '.number // empty')
AMOUNT=$(echo "$INPUT" | jq -r '.amount // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // empty')
TYPE_TEXT=$(echo "$INPUT" | jq -r '.type // "полный остаток"')
COMMENT=$(echo "$INPUT" | jq -r '.comment // "Оплата поставщику"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$PROVIDER_NAME" ] && { echo "ERROR: provider" >&2; exit 1; }
[ -z "$AMOUNT" ] && { echo "ERROR: amount" >&2; exit 1; }
[ -z "$CURRENCY" ] && { echo "ERROR: currency" >&2; exit 1; }

CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')
REQUEST_TYPE="full_remaining"
IS_PREPAYMENT="false"
case "$(echo "$TYPE_TEXT" | tr '[:upper:]' '[:lower:]')" in
  *аванс*) REQUEST_TYPE="advance"; IS_PREPAYMENT="true" ;;
esac

# ── Idempotency: single flock ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

TOKEN=$(pb_auth) || exit 1

# Idempotent branch: journal exists → GET + verify
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")
  VERIFY=$(pb_get "$TOKEN" "operator_payment_requests" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена" >&2; exit 1; }
  V_STATUS=$(echo "$VERIFY" | jq -r '.status')
  [ "$V_STATUS" != "pending" ] && { echo "ERROR: idempotent retry — статус=$V_STATUS" >&2; exit 1; }
tool_trace "create_operator_request" "$OPERATION_ID" "$PRIOR_ID"
  echo "OK (idempotent): запрос на оплату $PRIOR_ID уже создан (pending)"
  exit 0
fi

# Fresh branch
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

# Find application
APP_JSON=$(pb_find_application "$TOKEN" "$CONTRACT_ID" "$PROVIDER_NAME" "$APP_NUMBER") || exit 1
APP_ID=$(echo "$APP_JSON" | jq -r '.id')
APP_CURRENCY=$(echo "$APP_JSON" | jq -r '.currency')

# Currency must match application
if [ -n "$APP_CURRENCY" ] && [ "$APP_CURRENCY" != "$CURRENCY" ]; then
  echo "ERROR: валюта запроса ($CURRENCY) должна совпадать с валютой заявки ($APP_CURRENCY)" >&2; exit 1
fi

# Check for existing open request
EXISTING=$(pb_list "$TOKEN" "operator_payment_requests" \
  "application_id=\"$APP_ID\" && (status=\"pending\" || status=\"partially_paid\")" 1) || true
EXISTING_COUNT=$(echo "$EXISTING" | jq -r '.totalItems // 0')
if [ "$EXISTING_COUNT" -gt 0 ]; then
  echo "ERROR: у заявки уже есть открытый запрос на оплату" >&2; exit 1
fi

PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --arg aid "$APP_ID" \
  --arg rtype "$REQUEST_TYPE" --argjson prepay "$IS_PREPAYMENT" \
  --argjson amount "$AMOUNT" --arg currency "$CURRENCY" \
  --arg comment "$COMMENT" \
  '{contract_id:$cid, application_id:$aid, request_type:$rtype,
    is_prepayment:$prepay, requested_amount:$amount, currency:$currency,
    status:"pending", comment:$comment}')

RESULT=$(pb_create "$TOKEN" "operator_payment_requests" "$PAYLOAD") || exit 1
REQ_ID=$(echo "$RESULT" | jq -r '.id // empty')
[ -z "$REQ_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

# Atomic save immediately after POST
journal_save_atomic "$JOURNAL_FILE" "$REQ_ID"

# GET-verify
VERIFY=$(pb_get "$TOKEN" "operator_payment_requests" "$REQ_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
[ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }

pb_audit "$TOKEN" "$CONTRACT_ID" "create_operator_request" \
  "Оплата поставщику $PROVIDER_NAME: $AMOUNT $CURRENCY ($REQUEST_TYPE)"
tool_trace "create_operator_request" "$OPERATION_ID" "$REQ_ID"
echo "OK: запрос на оплату $REQ_ID создан (pending, $PROVIDER_NAME, $AMOUNT $CURRENCY)"
