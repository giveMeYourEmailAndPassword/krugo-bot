#!/usr/bin/env bash
# cancel_supplier.sh — создаёт pending отмену approved заявки поставщика.
# JSON: {"operation_id":"...", "contract_id":"...", "provider":"KOMPAS",
#        "number":"222222", "reason":"отказ от услуги"}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
PROVIDER_NAME=$(echo "$INPUT" | jq -r '.provider // empty')
APP_NUMBER=$(echo "$INPUT" | jq -r '.number // empty')
REASON=$(echo "$INPUT" | jq -r '.reason // "Отмена поставщика"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$PROVIDER_NAME" ] && { echo "ERROR: provider" >&2; exit 1; }

# ── Idempotency ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

TOKEN=$(pb_auth) || exit 1

# Idempotent branch
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")
  VERIFY=$(pb_get "$TOKEN" "application_corrections" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена" >&2; exit 1; }
  V_STATUS=$(echo "$VERIFY" | jq -r '.status')
  [ "$V_STATUS" != "pending" ] && { echo "ERROR: idempotent retry — статус=$V_STATUS" >&2; exit 1; }
  echo "OK (idempotent): отмена $PRIOR_ID уже создана (pending)"
  exit 0
fi

# Fresh branch
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

APP_JSON=$(pb_find_application "$TOKEN" "$CONTRACT_ID" "$PROVIDER_NAME" "$APP_NUMBER") || exit 1
APP_ID=$(echo "$APP_JSON" | jq -r '.id')
CUR_AMOUNT=$(echo "$APP_JSON" | jq -r '.amount')
CUR_CURRENCY=$(echo "$APP_JSON" | jq -r '.currency')

# Check for existing pending correction (upsert)
EXISTING=$(pb_list "$TOKEN" "application_corrections" \
  "application_id=\"$APP_ID\" && status=\"pending\"" 1) || true
EXISTING_ID=$(echo "$EXISTING" | jq -r '.items[0].id // empty')

# Cancellation: old_amount = new_amount = current (PB rejects 0)
PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --arg aid "$APP_ID" \
  --argjson amt "$CUR_AMOUNT" --arg cur "$CUR_CURRENCY" \
  --arg reason "$REASON" \
  '{contract_id:$cid, application_id:$aid, type:"cancellation", field:"amount",
    old_amount:$amt, new_amount:$amt,
    old_currency:$cur, new_currency:$cur,
    status:"pending", reason:$reason}')

if [ -n "$EXISTING_ID" ]; then
  RESULT=$(pb_update "$TOKEN" "application_corrections" "$EXISTING_ID" "$PAYLOAD") || exit 1
  CORR_ID=$(echo "$RESULT" | jq -r '.id // empty')
else
  RESULT=$(pb_create "$TOKEN" "application_corrections" "$PAYLOAD") || exit 1
  CORR_ID=$(echo "$RESULT" | jq -r '.id // empty')
fi
[ -z "$CORR_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

journal_save_atomic "$JOURNAL_FILE" "$CORR_ID"

# GET-verify
VERIFY=$(pb_get "$TOKEN" "application_corrections" "$CORR_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
[ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }

pb_audit "$TOKEN" "$CONTRACT_ID" "cancel_supplier" \
  "Отмена поставщика $PROVIDER_NAME ($CUR_AMOUNT $CUR_CURRENCY)"
echo "OK: отмена $CORR_ID создана (pending, $PROVIDER_NAME)"
