#!/usr/bin/env bash
# create_refund.sh — создаёт pending возврат клиенту.
# JSON: {"operation_id":"...", "contract_id":"...", "amount":30000,
#        "currency":"KGS", "reason":"отмена тура", "date":"2026-07-21", "comment":"..."}
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/pb_helper.sh"

INPUT=$(cat)
OPERATION_ID=$(echo "$INPUT" | jq -r '.operation_id // empty')
[ -z "$OPERATION_ID" ] && { echo "ERROR: operation_id не указан" >&2; exit 1; }

CONTRACT_ID=$(echo "$INPUT" | jq -r '.contract_id // empty')
AMOUNT=$(echo "$INPUT" | jq -r '.amount // empty')
CURRENCY=$(echo "$INPUT" | jq -r '.currency // empty')
REASON_TEXT=$(echo "$INPUT" | jq -r '.reason // empty')
REFUND_DATE=$(echo "$INPUT" | jq -r '.date // empty')
COMMENT=$(echo "$INPUT" | jq -r '.comment // "Возврат клиенту"')

[ -z "$CONTRACT_ID" ] && { echo "ERROR: contract_id" >&2; exit 1; }
[ -z "$AMOUNT" ] && { echo "ERROR: amount" >&2; exit 1; }
[ -z "$CURRENCY" ] && { echo "ERROR: currency" >&2; exit 1; }
[ -z "$REFUND_DATE" ] && { echo "ERROR: date" >&2; exit 1; }

CURRENCY=$(echo "$CURRENCY" | tr '[:lower:]' '[:upper:]')
REASON="other"
case "$(echo "$REASON_TEXT" | tr '[:upper:]' '[:lower:]')" in
  *отмен*) REASON="cancellation" ;;
  *переплат*) REASON="overpayment" ;;
  *частичн*) REASON="partial_refund" ;;
esac

# ── Idempotency under single flock ──
JOURNAL_FILE="$JOURNAL_DIR/$OPERATION_ID"
exec 200>"$JOURNAL_FILE.lock"
flock -x 200
if [ -f "$JOURNAL_FILE" ]; then
  echo "OK (idempotent): возврат $(cat "$JOURNAL_FILE") уже создан ранее"
  exit 0
fi

TOKEN=$(pb_auth) || exit 1
pb_check_contract "$TOKEN" "$CONTRACT_ID" > /dev/null || exit 1

PAYLOAD=$(jq -n \
  --arg cid "$CONTRACT_ID" --argjson amount "$AMOUNT" --arg currency "$CURRENCY" \
  --arg reason "$REASON" --arg rdate "$REFUND_DATE" --arg comment "$COMMENT" \
  '{contract_id:$cid, amount:$amount, currency:$currency,
    refund_date:$rdate, reason:$reason, comment:$comment, status:"pending"}')

RESULT=$(pb_create "$TOKEN" "client_refunds" "$PAYLOAD") || exit 1
REFUND_ID=$(echo "$RESULT" | jq -r '.id // empty')
[ -z "$REFUND_ID" ] && { echo "ERROR: PB не вернул id" >&2; exit 1; }

# GET-verify
VERIFY=$(pb_get "$TOKEN" "client_refunds" "$REFUND_ID") || exit 1
V_STATUS=$(echo "$VERIFY" | jq -r '.status')
[ "$V_STATUS" != "pending" ] && { echo "ERROR: статус=$V_STATUS" >&2; exit 1; }

pb_audit "$TOKEN" "$CONTRACT_ID" "create_refund" "Возврат $AMOUNT $CURRENCY ($REASON)"
echo "$REFUND_ID" > "$JOURNAL_FILE"
echo "OK: возврат $REFUND_ID создан (pending, $AMOUNT $CURRENCY, reason=$REASON)"
