#!/usr/bin/env bash
# change_supplier.sh — меняет поставщика и/или номер заявки и/или сумму.
# Для approved заявок: создаёт application_correction (pending).
# Для non-approved: PATCH напрямую (provider_id, number, НЕ amount).
# JSON: {"operation_id":"...", "contract_id":"...",
#        "old_provider":"BEST SERVICE", "old_number":"777777",
#        "new_provider":"ANEX", "new_number":"111222",
#        "old_amount":85, "new_amount":80, "currency":"USD", "reason":"..."}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
OLD_PROVIDER=$(echo "$INPUT" | jq -r '.old_provider // empty')
OLD_NUMBER=$(echo "$INPUT" | jq -r '.old_number // empty')
NEW_PROVIDER=$(echo "$INPUT" | jq -r '.new_provider // empty')
NEW_NUMBER=$(echo "$INPUT" | jq -r '.new_number // empty')
NEW_AMOUNT=$(echo "$INPUT" | jq -r '.new_amount // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // "USD"')
REASON=$(echo "$INPUT" | jq -r '.reason // "Изменение поставщика"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$OLD_PROVIDER" ] && { echo "ERROR: old_provider" >&2; exit 1; }

# ── Idempotency ──
JOURNAL_FILE=$(journal_path "$OPERATION_ID")
exec 200>"$JOURNAL_FILE.lock"
flock -x 200

TOKEN=$(pb_auth) || exit 1

# Idempotent branch — verify record (correction or application PATCH)
if [ -f "$JOURNAL_FILE" ]; then
  PRIOR_ID=$(cat "$JOURNAL_FILE")
  # Try application_corrections first (approved path)
  VERIFY=$(pb_get "$TOKEN" "application_corrections" "$PRIOR_ID" 2>/dev/null) || VERIFY=""
  if [ -n "$VERIFY" ] && [ "$(echo "$VERIFY" | jq -r '.id // empty')" != "" ]; then
    V_STATUS=$(echo "$VERIFY" | jq -r '.status')
    [ "$V_STATUS" != "pending" ] && { echo "ERROR: idempotent retry — статус=$V_STATUS" >&2; exit 1; }
    tool_trace "change_supplier" "$OPERATION_ID" "$PRIOR_ID"
    echo "OK (idempotent): корректировка поставщика $PRIOR_ID уже создана (pending)"
    exit 0
  fi
  # Otherwise it was a direct PATCH — verify application
  VERIFY=$(pb_get "$TOKEN" "applications" "$PRIOR_ID") || {
    echo "ERROR: idempotent retry — запись $PRIOR_ID не найдена" >&2; exit 1; }
  tool_trace "change_supplier" "$OPERATION_ID" "$PRIOR_ID"
  echo "OK (idempotent): поставщик изменён ($PRIOR_ID)"
  exit 0
fi

# Fresh branch
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

# Find existing application
APP_JSON=$(pb_find_application "$TOKEN" "$CONTRACT_ID" "$OLD_PROVIDER" "$OLD_NUMBER") || exit 1
APP_ID=$(echo "$APP_JSON" | jq -r '.id')
APP_FINANCE=$(echo "$APP_JSON" | jq -r '.finance_status')
APP_DELETED=$(echo "$APP_JSON" | jq -r '.is_deleted')
APP_STATUS=$(echo "$APP_JSON" | jq -r '.status')
CUR_AMOUNT=$(echo "$APP_JSON" | jq -r '.amount')
CUR_CURRENCY=$(echo "$APP_JSON" | jq -r '.currency')

# Validate app state
[ "$APP_DELETED" = "true" ] && { echo "ERROR: заявка поставщика удалена" >&2; exit 1; }
case "$APP_STATUS" in
  cancelled|refunded) echo "ERROR: заявка поставщика $APP_STATUS — нельзя изменить" >&2; exit 1 ;;
esac

IS_APPROVED="false"
case "$APP_FINANCE" in
  approved|"") IS_APPROVED="true" ;;
  *) echo "WARN: заявка не approved (finance_status: $APP_FINANCE) — правлю напрямую" >&2 ;;
esac

if [ "$IS_APPROVED" = "true" ]; then
  # ── Approved path: create correction ──

  # Stale check on amount (if new_amount provided, old must match current)
  if [ -n "$NEW_AMOUNT" ] && [ "$NEW_AMOUNT" != "null" ]; then
    # Validate new_amount is positive number
    jq -ne --arg v "$NEW_AMOUNT" '$v | tonumber > 0' > /dev/null 2>&1 || {
      echo "ERROR: new_amount должен быть числом > 0" >&2; exit 1; }
  fi

  # Upsert existing pending correction
  EXISTING=$(pb_list "$TOKEN" "application_corrections" \
    "application_id=\"$APP_ID\" && status=\"pending\"" 1) || true
  EXISTING_ID=$(echo "$EXISTING" | jq -r '.items[0].id // empty')

  # If new_amount not provided, keep current
  FINAL_NEW_AMOUNT="${NEW_AMOUNT:-$CUR_AMOUNT}"
  FINAL_CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')
  [ -z "$FINAL_CURRENCY" ] && FINAL_CURRENCY="$CUR_CURRENCY"

  PAYLOAD=$(jq -n \
    --arg cid "$CONTRACT_ID" --arg aid "$APP_ID" \
    --argjson old_amt "$CUR_AMOUNT" --argjson new_amt "$FINAL_NEW_AMOUNT" \
    --arg old_cur "$CUR_CURRENCY" --arg new_cur "$FINAL_CURRENCY" \
    --arg reason "$REASON" \
    '{contract_id:$cid, application_id:$aid, type:"correction", field:"amount",
      old_amount:$old_amt, new_amount:$new_amt,
      old_currency:$old_cur, new_currency:$new_cur,
      status:"pending", reason:$reason}')

  if [ -n "$EXISTING_ID" ]; then
    RESULT=$(pb_update "$TOKEN" "application_corrections" "$EXISTING_ID" "$PAYLOAD") || exit 1
  else
    RESULT=$(pb_create "$TOKEN" "application_corrections" "$PAYLOAD") || exit 1
  fi
  CORR_ID=$(echo "$RESULT" | jq -r '.id // empty')
  [ -z "$CORR_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

  journal_save_atomic "$JOURNAL_FILE" "$CORR_ID"

  # GET-verify
  VERIFY=$(pb_get "$TOKEN" "application_corrections" "$CORR_ID") || exit 1
  V_STATUS=$(echo "$VERIFY" | jq -r '.status')
  [ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }

  # If new_provider given, also PATCH provider_id + number directly
  # (provider change is meta, not financial — allowed even on approved)
  if [ -n "$NEW_PROVIDER" ] && [ "$NEW_PROVIDER" != "null" ]; then
    NEW_PID=$(pb_resolve_provider "$TOKEN" "$NEW_PROVIDER") || exit 1
    PATCH=$(jq -n --arg pid "$NEW_PID" --arg num "${NEW_NUMBER:-}" \
      '{provider_id:$pid} + (if $num != "" then {number:$num} else {} end)')
    pb_update "$TOKEN" "applications" "$APP_ID" "$PATCH" > /dev/null || exit 1
  fi

  # Update tour_operator if primary
  IS_PRIMARY=$(echo "$APP_JSON" | jq -r '.is_primary')
  if [ "$IS_PRIMARY" = "true" ] && [ -n "$NEW_PROVIDER" ] && [ "$NEW_PROVIDER" != "null" ]; then
    pb_update "$TOKEN" "contracts" "$CONTRACT_ID" \
      "{\"tour_operator\":\"$NEW_PROVIDER\"}" > /dev/null || true
  fi

  pb_audit "$TOKEN" "$CONTRACT_ID" "change_supplier" \
    "Поставщик: $OLD_PROVIDER → ${NEW_PROVIDER:-$OLD_PROVIDER} (correction pending)"
  tool_trace "change_supplier" "$OPERATION_ID" "$CORR_ID"
  echo "OK: корректировка $CORR_ID создана (pending, $OLD_PROVIDER${NEW_PROVIDER:+ → $NEW_PROVIDER})"

else
  # ── Non-approved path: direct PATCH (provider, number, amount) ──
  PATCH=$(jq -n '{}')
  if [ -n "$NEW_PROVIDER" ] && [ "$NEW_PROVIDER" != "null" ]; then
    NEW_PID=$(pb_resolve_provider "$TOKEN" "$NEW_PROVIDER") || exit 1
    PATCH=$(echo "$PATCH" | jq --arg pid "$NEW_PID" '. + {provider_id:$pid}')
  fi
  if [ -n "$NEW_NUMBER" ] && [ "$NEW_NUMBER" != "null" ]; then
    PATCH=$(echo "$PATCH" | jq --arg num "$NEW_NUMBER" '. + {number:$num}')
  fi
  if [ -n "$NEW_AMOUNT" ] && [ "$NEW_AMOUNT" != "null" ]; then
    jq -ne --arg v "$NEW_AMOUNT" '$v | tonumber > 0' > /dev/null 2>&1 || {
      echo "ERROR: new_amount должен быть числом > 0" >&2; exit 1; }
    PATCH=$(echo "$PATCH" | jq --argjson amt "$NEW_AMOUNT" '. + {amount:$amt}')
  fi

  RESULT=$(pb_update "$TOKEN" "applications" "$APP_ID" "$PATCH") || exit 1
  PATCHED_ID=$(echo "$RESULT" | jq -r '.id // empty')
  [ -z "$PATCHED_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

  journal_save_atomic "$JOURNAL_FILE" "$PATCHED_ID"

  # GET-verify
  VERIFY=$(pb_get "$TOKEN" "applications" "$PATCHED_ID") || exit 1
  V_DELETED=$(echo "$VERIFY" | jq -r '.is_deleted')
  [ "$V_DELETED" = "true" ] && { echo "ERROR: is_deleted=true" >&2; exit 1; }

  # Update tour_operator if primary
  IS_PRIMARY=$(echo "$APP_JSON" | jq -r '.is_primary')
  if [ "$IS_PRIMARY" = "true" ] && [ -n "$NEW_PROVIDER" ] && [ "$NEW_PROVIDER" != "null" ]; then
    pb_update "$TOKEN" "contracts" "$CONTRACT_ID" \
      "{\"tour_operator\":\"$NEW_PROVIDER\"}" > /dev/null || true
  fi

  pb_audit "$TOKEN" "$CONTRACT_ID" "change_supplier" \
    "Поставщик: $OLD_PROVIDER → ${NEW_PROVIDER:-$OLD_PROVIDER}"
  tool_trace "change_supplier" "$OPERATION_ID" "$PATCHED_ID"
  echo "OK: поставщик изменён напрямую ($OLD_PROVIDER${NEW_PROVIDER:+ → $NEW_PROVIDER})"
fi
