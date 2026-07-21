#!/usr/bin/env bash
# test_tools.sh — функциональные тесты инструментов Hermes против mock PB.
#
# Проверяет: точный collection/payload, нет 2-го POST при idempotent retry,
# side-effect recovery, error paths.
#
# Запуск: bash tests/test_tools.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOOLS_DIR="$SCRIPT_DIR/../hermes/skills/contracts/tools"
TMPDIR=$(mktemp -d)
export STATE_FILE="$TMPDIR/state.json"
trap 'rm -rf "$TMPDIR"; kill $MOCK_PID 2>/dev/null || true' EXIT

PASS=0
FAIL=0
ERRORS=""

# ── Start mock servers ──
python3 "$SCRIPT_DIR/mock_pb_server.py" &
MOCK_PID=$!
sleep 1.5

# Verify mock is up
curl -sf http://127.0.0.1:18090/api/collections/_superusers/auth-with-password \
  -X POST -H "Content-Type: application/json" -d '{"identity":"x","password":"x"}' > /dev/null

# ── Helpers ──
run_tool() {
  echo "$2" | PB_URL="http://127.0.0.1:18090" \
    PB_USER="mock" PB_PASS="mock" \
    BACKEND_URL="http://127.0.0.1:18091" \
    HERMES_HOME="$TMPDIR" \
    bash "$TOOLS_DIR/$1" 2>&1
}

# run_tool_rc — same as run_tool but captures exit code (for error-path tests)
# Sets OUT (stdout) and RC (exit code). Caller must use `set +e` before.
run_tool_rc() {
  OUT=$(echo "$2" | PB_URL="http://127.0.0.1:18090" \
    PB_USER="mock" PB_PASS="mock" \
    BACKEND_URL="http://127.0.0.1:18091" \
    HERMES_HOME="$TMPDIR" \
    bash "$TOOLS_DIR/$1" 2>&1)
  RC=$?
}

count_posts() {
  jq "[.log[] | select(.method==\"POST\" and .collection==\"$1\")] | length" "$STATE_FILE"
}

last_post_body() {
  jq -r "[.log[] | select(.method==\"POST\" and .collection==\"$1\")] | last | .body | tojson" "$STATE_FILE" 2>/dev/null
}

count_patch() {
  jq "[.log[] | select(.method==\"PATCH\" and .collection==\"$1\"$2)] | length" "$STATE_FILE"
}
last_patch_body() {
  jq -r "[.log[] | select(.method==\"PATCH\" and .collection==\"$1\")] | last | .body | tojson" "$STATE_FILE" 2>/dev/null
}

assert_eq() {
  if [ "$2" = "$3" ]; then
    echo "  PASS: $1 — $3"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $1 — expected '$3', got '$2'"
    FAIL=$((FAIL+1))
    ERRORS="$ERRORS\n  $1: expected '$3', got '$2'"
  fi
}

assert_ne() {
  if [ "$2" != "$3" ]; then
    echo "  PASS: $1 — not $3 (got $2)"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $1 — expected != '$3', got '$2'"
    FAIL=$((FAIL+1))
    ERRORS="$ERRORS\n  $1: expected != '$3', got '$2'"
  fi
}

assert_contains() {
  if echo "$2" | grep -q "$3"; then
    echo "  PASS: $1 — contains '$3'"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $1 — expected '$3' in '$2'"
    FAIL=$((FAIL+1))
    ERRORS="$ERRORS\n  $1: expected '$3' in '$2'"
  fi
}

# ── Tests ──

echo "=== 1. create_payment.sh — fresh ==="
OUT=$(run_tool "create_payment.sh" '{"operation_id":"100:200:pay:1","contract_id":"q9wynhz1pi4tpvh","amount":500,"currency":"USD","method":"наличные","date":"2026-07-21","comment":"тест"}')
assert_contains "payment OK" "$OUT" "OK: платёж"
assert_contains "payment trace" "$OUT" "TOOL_TRACE create_payment"
assert_eq "payment POST count" "$(count_posts payments)" "1"
BODY=$(last_post_body payments)
assert_contains "payment status=pending" "$BODY" '"status":"pending"'
assert_contains "payment is_confirmed=false" "$BODY" '"is_confirmed":false'
assert_contains "payment rate=87.8" "$BODY" '"exchange_rate_kgs":87.8'
assert_contains "payment office" "$BODY" '"office_id":"h00d7lg15350gz8"'
assert_contains "payment method" "$BODY" '"payment_method_id":"on3y22ok00pb60j"'

echo ""
echo "=== 2. create_payment.sh — idempotent ==="
OUT=$(run_tool "create_payment.sh" '{"operation_id":"100:200:pay:1","contract_id":"q9wynhz1pi4tpvh","amount":500,"currency":"USD","method":"наличные","date":"2026-07-21","comment":"тест"}')
assert_contains "payment idempotent" "$OUT" "OK (idempotent)"
assert_contains "payment idempotent trace" "$OUT" "TOOL_TRACE create_payment"
assert_eq "payment no 2nd POST" "$(count_posts payments)" "1"

echo ""
echo "=== 3. create_payment.sh — KGS ==="
OUT=$(run_tool "create_payment.sh" '{"operation_id":"100:200:pay:2","contract_id":"q9wynhz1pi4tpvh","amount":50000,"currency":"KGS","method":"наличные","date":"2026-07-21"}')
assert_contains "payment KGS OK" "$OUT" "OK: платёж"
assert_contains "payment KGS trace" "$OUT" "TOOL_TRACE create_payment"
BODY=$(last_post_body payments)
assert_contains "payment KGS rate=1" "$BODY" '"exchange_rate_kgs":1'

echo ""
echo "=== 4. create_refund.sh — fresh ==="
OUT=$(run_tool "create_refund.sh" '{"operation_id":"100:200:refund:1","contract_id":"q9wynhz1pi4tpvh","amount":200,"currency":"USD","reason":"переплата","date":"2026-07-21"}')
assert_contains "refund OK" "$OUT" "OK: возврат"
assert_contains "refund trace" "$OUT" "TOOL_TRACE create_refund"
assert_eq "refund POST count" "$(count_posts client_refunds)" "1"
BODY=$(last_post_body client_refunds)
assert_contains "refund status=pending" "$BODY" '"status":"pending"'
assert_contains "refund reason=overpayment" "$BODY" '"reason":"overpayment"'

echo ""
echo "=== 5. create_refund.sh — idempotent ==="
OUT=$(run_tool "create_refund.sh" '{"operation_id":"100:200:refund:1","contract_id":"q9wynhz1pi4tpvh","amount":200,"currency":"USD","reason":"переплата","date":"2026-07-21"}')
assert_contains "refund idempotent" "$OUT" "OK (idempotent)"
assert_contains "refund idempotent trace" "$OUT" "TOOL_TRACE create_refund"
assert_eq "refund no 2nd POST" "$(count_posts client_refunds)" "1"

echo ""
echo "=== 6. create_operator_request.sh — fresh ==="
OUT=$(run_tool "create_operator_request.sh" '{"operation_id":"100:200:op:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","amount":110,"currency":"USD","type":"полный остаток"}')
assert_contains "operator OK" "$OUT" "OK: запрос на оплату"
assert_contains "operator trace" "$OUT" "TOOL_TRACE create_operator_request"
assert_eq "operator POST count" "$(count_posts operator_payment_requests)" "1"
BODY=$(last_post_body operator_payment_requests)
assert_contains "operator status=pending" "$BODY" '"status":"pending"'
assert_contains "operator type=full_remaining" "$BODY" '"request_type":"full_remaining"'

echo ""
echo "=== 7. create_operator_request.sh — idempotent ==="
OUT=$(run_tool "create_operator_request.sh" '{"operation_id":"100:200:op:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","amount":110,"currency":"USD","type":"полный остаток"}')
assert_contains "operator idempotent" "$OUT" "OK (idempotent)"
assert_contains "operator idempotent trace" "$OUT" "TOOL_TRACE create_operator_request"
assert_eq "operator no 2nd POST" "$(count_posts operator_payment_requests)" "1"

echo ""
echo "=== 8. create_correction.sh — fresh ==="
OUT=$(run_tool "create_correction.sh" '{"operation_id":"100:200:corr:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","new_amount":110,"reason":"скидка"}')
assert_contains "correction OK" "$OUT" "OK: корректировка"
assert_contains "correction trace" "$OUT" "TOOL_TRACE create_correction"
assert_eq "correction POST count" "$(count_posts application_corrections)" "1"
BODY=$(last_post_body application_corrections)
assert_contains "correction status=pending" "$BODY" '"status":"pending"'
assert_contains "correction type=correction" "$BODY" '"type":"correction"'

echo ""
echo "=== 9. create_correction.sh — idempotent ==="
OUT=$(run_tool "create_correction.sh" '{"operation_id":"100:200:corr:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","new_amount":110,"reason":"скидка"}')
assert_contains "correction idempotent" "$OUT" "OK (idempotent)"
assert_contains "correction idempotent trace" "$OUT" "TOOL_TRACE create_correction"
assert_eq "correction no 2nd POST" "$(count_posts application_corrections)" "1"

echo ""
echo "=== 10. cancel_supplier.sh — fresh ==="
OUT=$(run_tool "cancel_supplier.sh" '{"operation_id":"100:200:cancel:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","reason":"отказ"}')
assert_contains "cancel OK" "$OUT" "OK: отмена"
assert_contains "cancel trace" "$OUT" "TOOL_TRACE cancel_supplier"
# Cancel upserts existing pending correction via PATCH — check PATCH body
BODY=$(last_patch_body application_corrections)
assert_contains "cancel type=cancellation" "$BODY" '"type":"cancellation"'
echo ""
echo "=== 11. cancel_supplier.sh — idempotent ==="
BEFORE=$(count_posts application_corrections)
OUT=$(run_tool "cancel_supplier.sh" '{"operation_id":"100:200:cancel:1","contract_id":"q9wynhz1pi4tpvh","provider":"ANEX","number":"100221","reason":"отказ"}')
assert_contains "cancel idempotent" "$OUT" "OK (idempotent)"
assert_contains "cancel idempotent trace" "$OUT" "TOOL_TRACE cancel_supplier"
assert_eq "cancel no 2nd POST" "$(count_posts application_corrections)" "$BEFORE"

echo ""
echo "=== 12. create_finance_request.sh — fresh + side-effect ==="
OUT=$(run_tool "create_finance_request.sh" '{"operation_id":"100:200:fin:1","contract_id":"q9wynhz1pi4tpvh","field":"netto_price","new_value":155,"currency":"USD","reason":"доплата"}')
assert_contains "finance OK" "$OUT" "OK: фин-запрос"
assert_contains "finance trace" "$OUT" "TOOL_TRACE create_finance_request"
assert_eq "finance POST count" "$(count_posts finance_change_requests)" "1"
BODY=$(last_post_body finance_change_requests)
assert_contains "finance field" "$BODY" '"field":"netto_price"'
assert_contains "finance status" "$BODY" '"status":"pending"'
PATCH_COUNT=$(count_patch contracts " and .body.finance_status==\"pending\"")
assert_eq "finance side-effect PATCH" "$PATCH_COUNT" "1"

echo ""
echo "=== 13. create_finance_request.sh — idempotent + side-effect repair ==="
# Simulate crash: reset contract finance_status to approved via mock PATCH API
curl -s -X PATCH "http://127.0.0.1:18090/api/collections/contracts/records/q9wynhz1pi4tpvh" \
  -H "Content-Type: application/json" -d '{"finance_status":"approved"}' > /dev/null
PATCH_BEFORE=$(count_patch contracts " and .body.finance_status==\"pending\"")
OUT=$(run_tool "create_finance_request.sh" '{"operation_id":"100:200:fin:1","contract_id":"q9wynhz1pi4tpvh","field":"netto_price","new_value":155,"currency":"USD","reason":"доплата"}')
assert_contains "finance idempotent" "$OUT" "OK (idempotent)"
assert_contains "finance idempotent trace" "$OUT" "TOOL_TRACE create_finance_request"
assert_eq "finance no 2nd POST" "$(count_posts finance_change_requests)" "1"
PATCH_AFTER=$(count_patch contracts " and .body.finance_status==\"pending\"")
assert_eq "finance side-effect repaired" "$PATCH_AFTER" "$((PATCH_BEFORE + 1))"

echo ""
echo "=== 14. add_supplier.sh — fresh ==="
OUT=$(run_tool "add_supplier.sh" '{"operation_id":"100:200:add:1","contract_id":"q9wynhz1pi4tpvh","provider":"KOMPAS","number":"300222","amount":45,"currency":"USD","is_primary":false}')
assert_contains "add OK" "$OUT" "OK: поставщик"
assert_contains "add trace" "$OUT" "TOOL_TRACE add_supplier"
assert_eq "add POST count" "$(count_posts applications)" "1"
BODY=$(last_post_body applications)
assert_contains "add type=supplier" "$BODY" '"type":"supplier"'
assert_contains "add status=active" "$BODY" '"status":"active"'

echo ""
echo "=== 15. add_supplier.sh — idempotent ==="
BEFORE=$(count_posts applications)
OUT=$(run_tool "add_supplier.sh" '{"operation_id":"100:200:add:1","contract_id":"q9wynhz1pi4tpvh","provider":"KOMPAS","number":"300222","amount":45,"currency":"USD","is_primary":false}')
assert_contains "add idempotent" "$OUT" "OK (idempotent)"
assert_contains "add idempotent trace" "$OUT" "TOOL_TRACE add_supplier"
assert_eq "add no 2nd POST" "$(count_posts applications)" "$BEFORE"

echo ""
echo "=== 16. change_supplier.sh — fresh ==="
OUT=$(run_tool "change_supplier.sh" '{"operation_id":"100:200:change:1","contract_id":"q9wynhz1pi4tpvh","old_provider":"ANEX","old_number":"100221","new_provider":"KOMPAS","new_number":"300333","new_amount":110,"currency":"USD","reason":"замена"}')
assert_contains "change OK" "$OUT" "OK"
assert_contains "change trace" "$OUT" "TOOL_TRACE change_supplier"
echo ""
echo "=== 17. change_supplier.sh — idempotent ==="
OUT=$(run_tool "change_supplier.sh" '{"operation_id":"100:200:change:1","contract_id":"q9wynhz1pi4tpvh","old_provider":"ANEX","old_number":"100221","new_provider":"KOMPAS","new_number":"300333","new_amount":110,"currency":"USD","reason":"замена"}')
assert_contains "change idempotent" "$OUT" "OK (idempotent)"
assert_contains "change idempotent trace" "$OUT" "TOOL_TRACE change_supplier"

echo ""
echo "=== 18. Error: missing operation_id ==="
set +e
run_tool_rc "create_payment.sh" '{"contract_id":"q9wynhz1pi4tpvh","amount":500,"currency":"USD","method":"наличные","date":"2026-07-21"}'
set -e
assert_ne "no op_id rc!=0" "$RC" "0"
assert_contains "no op_id" "$OUT" "ERROR: operation_id"

echo ""
echo "=== 19. Error: bad currency ==="
set +e
run_tool_rc "create_payment.sh" '{"operation_id":"100:200:pay:99","contract_id":"q9wynhz1pi4tpvh","amount":500,"currency":"RUB","method":"наличные","date":"2026-07-21"}'
set -e
assert_ne "bad currency rc!=0" "$RC" "0"
assert_contains "bad currency" "$OUT" "ERROR: валюта"

echo ""
echo "=== 20. Error: bad finance field ==="
set +e
run_tool_rc "create_finance_request.sh" '{"operation_id":"100:200:fin:99","contract_id":"q9wynhz1pi4tpvh","field":"bad_field","new_value":100}'
set -e
assert_ne "bad field rc!=0" "$RC" "0"
assert_contains "bad field" "$OUT" "ERROR: field"

echo ""
echo "=========================================="
echo "PASS: $PASS  FAIL: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo -e "FAILURES:$ERRORS"
  exit 1
fi
echo "ALL TESTS PASSED"
