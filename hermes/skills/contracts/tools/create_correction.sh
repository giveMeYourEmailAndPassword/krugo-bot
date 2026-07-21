#!/usr/bin/env bash
# create_correction.sh — создаёт pending корректировку суммы approved заявки поставщика.
# JSON: {"operation_id":"...", "contract_id":"...", "provider":"ANEX",
#        "number":"111222", "old_amount":85, "new_amount":80,
#        "old_currency":"USD", "new_currency":"USD", "reason":"скидка"}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
PROVIDER_NAME=$(echo "$INPUT" | jq -r '.provider // empty')
APP_NUMBER=$(echo "$INPUT" | jq -r '.number // empty')
OLD_AMOUNT=$(echo "$INPUT" | jq -r '.old_amount // empty')
NEW_AMOUNT=$(echo "$INPUT" | jq -r '.new_amount // empty')
OLD_CURRENCY=$(echo "$INPUT" | jq -r '.old_currency // "USD"')
NEW_CURRENCY=$(echo "$INPUT" | jq -r '.new_currency // "USD"')
REASON=$(echo "$INPUT" | jq -r '.reason // "Корректировка суммы"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$PROVIDER_NAME" ] && { echo "ERROR: provider" >&2; exit 1; }
[ -z "$NEW_AMOUNT" ] && { echo "ERROR: new_amount" >&2; exit 1; }

# Validate new_amount is a positive number
jq -ne --arg v "$NEW_AMOUNT" '$v | tonumber > 0' > /dev/null 2>&1 || {
  echo "ERROR: new_amount должен быть числом > 0" >&2; exit 1; }

OLD_CURRENCY=$(echo "$OLD_CURRENCY" | tr '[:lower:]' '[:upper:]')
NEW_CURRENCY=$(echo "$NEW_CURRENCY" | tr '[:lower:]' '[:upper:]')

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
  echo "OK (idempotent): корректировка $PRIOR_ID уже создана (pending)"
  exit 0
fi

# Fresh branch
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

# Find application
APP_JSON=$(pb_find_application "$TOKEN" "$CONTRACT_ID" "$PROVIDER_NAME" "$APP_NUMBER") || exit 1
APP_ID=$(echo "$APP_JSON" | jq -r '.id')
CUR_AMOUNT=$(echo "$APP_JSON" | jq -r '.amount')
CUR_CURRENCY=$(echo "$APP_JSON" | jq -r '.currency')
APP_DELETED=$(echo "$APP_JSON" | jq -r '.is_deleted')
APP_STATUS=$(echo "$APP_JSON" | jq -r '.status')
APP_FINANCE=$(echo "$APP_JSON" | jq -r '.finance_status')

# Enforce app state: must be active, not deleted, finance approved (or legacy empty)
[ "$APP_DELETED" = "true" ] && { echo "ERROR: заявка поставщика удалена" >&2; exit 1; }
case "$APP_STATUS" in
  cancelled|refunded) echo "ERROR: заявка поставщика $APP_STATUS — нельзя корректировать" >&2; exit 1 ;;
esac
case "$APP_FINANCE" in
  approved|"") ;;  # OK
  *) echo "ERROR: заявка поставщика не подтверждена (finance_status: $APP_FINANCE). Используйте редактирование договора" >&2; exit 1 ;;
esac

# Stale check: if old_amount provided, must match current (tolerance 0.01)
# awk always exits 0, prints 1 (stale) or 0 (ok) — safe under set -e
if [ -n "$OLD_AMOUNT" ] && [ "$OLD_AMOUNT" != "null" ]; then
  STALE=$(awk -v old="$OLD_AMOUNT" -v cur="$CUR_AMOUNT" \
    'BEGIN { d = (old > cur) ? old-cur : cur-old; print (d > 0.01) ? 1 : 0 }')
  if [ "$STALE" = "1" ]; then
    echo "ERROR: сумма поставщика уже изменилась (текущая: $CUR_AMOUNT $CUR_CURRENCY). Создайте новую корректировку" >&2; exit 1
  fi
fi

# Use current values for old_amount/old_currency
OLD_AMOUNT="$CUR_AMOUNT"
OLD_CURRENCY="$CUR_CURRENCY"

# Check for existing pending correction (upsert)
EXISTING=$(pb_list "$TOKEN" "application_corrections" \
  "application_id=\"$APP_ID\" && status=\"pending\"" 1) || true
EXISTING_ID=$(echo "$EXISTING" | jq -r '.items[0].id // empty')

PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --arg aid "$APP_ID" \
  --argjson old_amt "$OLD_AMOUNT" --argjson new_amt "$NEW_AMOUNT" \
  --arg old_cur "$OLD_CURRENCY" --arg new_cur "$NEW_CURRENCY" \
  --arg reason "$REASON" \
  '{contract_id:$cid, application_id:$aid, type:"correction", field:"amount",
    old_amount:$old_amt, new_amount:$new_amt,
    old_currency:$old_cur, new_currency:$new_cur,
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

pb_audit "$TOKEN" "$CONTRACT_ID" "create_correction" \
  "Корректировка $PROVIDER_NAME: $OLD_AMOUNT → $NEW_AMOUNT $NEW_CURRENCY"
echo "OK: корректировка $CORR_ID создана (pending, $PROVIDER_NAME: $OLD_AMOUNT → $NEW_AMOUNT)"
