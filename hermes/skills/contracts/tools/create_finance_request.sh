#!/usr/bin/env bash
# create_finance_request.sh — создаёт pending запрос на изменение финансов договора.
# JSON: {"operation_id":"...", "contract_id":"...", "field":"netto_price",
#        "old_value":123, "new_value":155, "currency":"USD", "reason":"доплата"}
# Одно поле за вызов (field: brutto_price | netto_price | actual_netto_price).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
FIELD=$(echo "$INPUT" | jq -r '.field // empty')
OLD_VALUE=$(echo "$INPUT" | jq -r '.old_value // empty')
NEW_VALUE=$(echo "$INPUT" | jq -r '.new_value // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // "USD"')
REASON=$(echo "$INPUT" | jq -r '.reason // "Изменение финансов"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$FIELD" ] && { echo "ERROR: field" >&2; exit 1; }
[ -z "$NEW_VALUE" ] && { echo "ERROR: new_value" >&2; exit 1; }

case "$FIELD" in
  brutto_price|netto_price|actual_netto_price) ;;
  *) echo "ERROR: field должен быть brutto_price, netto_price или actual_netto_price" >&2; exit 1 ;;
esac

# Validate new_value is a positive number
jq -ne --arg v "$NEW_VALUE" '$v | tonumber > 0' > /dev/null 2>&1 || {
  echo "ERROR: new_value должен быть числом > 0" >&2; exit 1; }

CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')

# ── Idempotency ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

TOKEN=$(pb_auth) || exit 1

# ── Idempotent branch: verify BOTH request AND contract side-effect ──
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")

  # Verify request exists + pending
  VERIFY=$(pb_get "$TOKEN" "finance_change_requests" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена" >&2; exit 1; }
  V_STATUS=$(echo "$VERIFY" | jq -r '.status')
  [ "$V_STATUS" != "pending" ] && { echo "ERROR: idempotent retry — статус=$V_STATUS" >&2; exit 1; }

  # Verify contract finance_status=pending (secondary side-effect)
  # If crash happened after POST but before PATCH, repair it now
  CONTRACT=$(pb_get "$TOKEN" "contracts" "$CONTRACT_ID") || exit 1
  C_FS=$(echo "$CONTRACT" | jq -r '.finance_status')
  if [ "$C_FS" != "pending" ]; then
    pb_update "$TOKEN" "contracts" "$CONTRACT_ID" '{"finance_status":"pending"}' > /dev/null || {
      echo "ERROR: idempotent retry — не удалось перевести договор в pending" >&2; exit 1; }
  fi

  echo "OK (idempotent): фин-запрос $PRIOR_ID уже создан (pending, договор в pending)"
  exit 0
fi

# ── Fresh branch ──
CONTRACT=$(pb_get "$TOKEN" "contracts" "$CONTRACT_ID") || exit 1
IS_CANCELLED=$(echo "$CONTRACT" | jq -r '.is_cancelled')
IS_DELETED=$(echo "$CONTRACT" | jq -r '.is_deleted')
IS_REJECTED=$(echo "$CONTRACT" | jq -r '.is_rejected')
[ "$IS_CANCELLED" = "true" ] && { echo "ERROR: договор отменён" >&2; exit 1; }
[ "$IS_DELETED" = "true" ] && { echo "ERROR: договор удалён" >&2; exit 1; }
[ "$IS_REJECTED" = "true" ] && { echo "ERROR: договор отклонён" >&2; exit 1; }

# Finance gate: only create for approved/paid/empty contracts
FS=$(echo "$CONTRACT" | jq -r '.finance_status')
case "$FS" in
  approved|paid|"") ;;
  *) echo "ERROR: финансы уже на рассмотрении или отклонены (статус: $FS)" >&2; exit 1 ;;
esac

# Read current value for old_value
CUR_VALUE=$(echo "$CONTRACT" | jq -r ".$FIELD // 0")

# Stale check: if old_value provided, must match current (tolerance 0.01)
# awk always exits 0, prints 1 (stale) or 0 (ok) — safe under set -e
if [ -n "$OLD_VALUE" ] && [ "$OLD_VALUE" != "null" ]; then
  STALE=$(awk -v old="$OLD_VALUE" -v cur="$CUR_VALUE" \
    'BEGIN { d = (old > cur) ? old-cur : cur-old; print (d > 0.01) ? 1 : 0 }')
  if [ "$STALE" = "1" ]; then
    echo "ERROR: $FIELD уже изменилось (текущее: $CUR_VALUE). Создайте новый запрос" >&2; exit 1
  fi
fi
OLD_VALUE="$CUR_VALUE"

# Create finance_change_request
PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --arg field "$FIELD" \
  --argjson old_v "$OLD_VALUE" --argjson new_v "$NEW_VALUE" \
  --arg currency "$CURRENCY" --arg reason "$REASON" \
  '{contract_id:$cid, field:$field, old_value:$old_v, new_value:$new_v,
    currency:$currency, status:"pending", reason:$reason}')

RESULT=$(pb_create "$TOKEN" "finance_change_requests" "$PAYLOAD") || exit 1
FCR_ID=$(echo "$RESULT" | jq -r '.id // empty')
[ -z "$FCR_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

# Atomic save immediately after POST
journal_save_atomic "$JOURNAL_FILE" "$FCR_ID"

# GET-verify request
VERIFY=$(pb_get "$TOKEN" "finance_change_requests" "$FCR_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
[ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }

# PATCH contract finance_status=pending (secondary side-effect)
# If crash here, idempotent retry will repair it (see branch above)
pb_update "$TOKEN" "contracts" "$CONTRACT_ID" '{"finance_status":"pending"}' > /dev/null || {
  echo "ERROR: фин-запрос создан ($FCR_ID), но не удалось перевести договор в pending" >&2; exit 1; }

pb_audit "$TOKEN" "$CONTRACT_ID" "create_finance_request" \
  "$FIELD: $OLD_VALUE → $NEW_VALUE $CURRENCY"
echo "OK: фин-запрос $FCR_ID создан (pending, $FIELD: $OLD_VALUE → $NEW_VALUE, договор в pending)"
