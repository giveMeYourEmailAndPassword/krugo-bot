#!/usr/bin/env bash
# add_supplier.sh — добавляет нового поставщика (application) к договору.
# JSON: {"operation_id":"...", "contract_id":"...", "provider":"KOMPAS",
#        "number":"222222", "amount":45, "currency":"USD", "is_primary":false}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
PROVIDER_NAME=$(echo "$INPUT" | jq -r '.provider // empty')
APP_NUMBER=$(echo "$INPUT" | jq -r '.number // empty')
AMOUNT=$(echo "$INPUT" | jq -r '.amount // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // "USD"')
IS_PRIMARY=$(echo "$INPUT" | jq -r '.is_primary // false')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$PROVIDER_NAME" ] && { echo "ERROR: provider" >&2; exit 1; }
[ -z "$AMOUNT" ] && { echo "ERROR: amount" >&2; exit 1; }

# Validate amount is positive number
jq -ne --arg v "$AMOUNT" '$v | tonumber > 0' > /dev/null 2>&1 || {
  echo "ERROR: amount должен быть числом > 0" >&2; exit 1; }

CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')
case "$CURRENCY" in
  USD|EUR|KGS) ;;
  *) echo "ERROR: валюта должна быть USD, EUR или KGS" >&2; exit 1 ;;
esac

# ── Idempotency ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

TOKEN=$(pb_auth) || exit 1

# Idempotent branch
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")
  VERIFY=$(pb_get "$TOKEN" "applications" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена" >&2; exit 1; }
  V_DELETED=$(echo "$VERIFY" | jq -r '.is_deleted')
  [ "$V_DELETED" = "true" ] && { echo "ERROR: idempotent retry — заявка удалена" >&2; exit 1; }
tool_trace "add_supplier" "$OPERATION_ID" "$PRIOR_ID"
  echo "OK (idempotent): поставщик $PRIOR_ID уже добавлен"
  exit 0
fi

# Fresh branch
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

# Resolve provider
PROVIDER_ID=$(pb_resolve_provider "$TOKEN" "$PROVIDER_NAME") || exit 1

# Business dedup: check if application with same contract_id + provider_id + number already exists
EXISTING=$(pb_list "$TOKEN" "applications" \
  "contract_id=\"$CONTRACT_ID\" && provider_id=\"$PROVIDER_ID\" && number=\"$APP_NUMBER\" && is_deleted!=true" 5) || true
EXISTING_COUNT=$(echo "$EXISTING" | jq -r '.totalItems // 0')
if [ "$EXISTING_COUNT" -gt 0 ]; then
  EXISTING_ID=$(echo "$EXISTING" | jq -r '.items[0].id')
  echo "ERROR: заявка поставщика $PROVIDER_NAME ($APP_NUMBER) уже существует: $EXISTING_ID" >&2; exit 1
fi

# Create application
PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --arg pid "$PROVIDER_ID" \
  --arg num "$APP_NUMBER" --argjson amount "$AMOUNT" \
  --arg currency "$CURRENCY" --argjson is_primary "$IS_PRIMARY" \
  '{contract_id:$cid, provider_id:$pid, number:$num,
    amount:$amount, currency:$currency, type:"supplier",
    is_primary:$is_primary, status:"active", finance_status:"approved"}')

RESULT=$(pb_create "$TOKEN" "applications" "$PAYLOAD") || exit 1
APP_ID=$(echo "$RESULT" | jq -r '.id // empty')
[ -z "$APP_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

journal_save_atomic "$JOURNAL_FILE" "$APP_ID"

# GET-verify
VERIFY=$(pb_get "$TOKEN" "applications" "$APP_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
V_DELETED=$(echo "$VERIFY" | jq -r '.is_deleted')
[ "$V_STATUS" != "active" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }
[ "$V_DELETED" = "true" ] && { echo "ERROR: is_deleted=true" >&2; exit 1; }

# If is_primary, update contract tour_operator
if [ "$IS_PRIMARY" = "true" ]; then
  pb_update "$TOKEN" "contracts" "$CONTRACT_ID" \
    "{\"tour_operator\":\"$PROVIDER_NAME\"}" > /dev/null || true
fi

pb_audit "$TOKEN" "$CONTRACT_ID" "add_supplier" \
  "Добавлен поставщик $PROVIDER_NAME ($AMOUNT $CURRENCY, заявка $APP_NUMBER)"
tool_trace "add_supplier" "$OPERATION_ID" "$APP_ID"
echo "OK: поставщик $APP_ID добавлен ($PROVIDER_NAME, $AMOUNT $CURRENCY, заявка $APP_NUMBER)"
