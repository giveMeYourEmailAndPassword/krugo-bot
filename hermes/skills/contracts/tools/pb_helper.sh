#!/usr/bin/env bash
# pb_helper.sh — общие функции для инструментов Hermes.
# Источник: подключается через `source pb_helper.sh`.
# Все curl-вызовы используют --fail-with-body и проверяют HTTP-код.

# pb_auth — авторизация, возвращает token через stdout.
pb_auth() {
  local resp status token
  resp=$(curl -sS --fail-with-body -X POST "$PB_URL/api/collections/_superusers/auth-with-password" \
    -H "Content-Type: application/json" \
    -d "{\"identity\":\"$PB_USER\",\"password\":\"$PB_PASS\"}" 2>&1) || {
    echo "ERROR: auth failed: $resp" >&2
    return 1
  }
  token=$(echo "$resp" | jq -r '.token // empty')
  if [ -z "$token" ]; then
    echo "ERROR: auth returned no token: $resp" >&2
    return 1
  fi
  echo "$token"
}

# JOURNAL_DIR — persistent idempotency journal.
# Each tool saves {sha256(operation_id) → record_id} atomically after POST.
JOURNAL_DIR="${HERMES_HOME:-/opt/data}/.journal"
mkdir -p "$JOURNAL_DIR" 2>/dev/null || true

# journal_path <operation_id> — returns safe file path (sha256 of op_id).
# Prevents path traversal (e.g. "../../etc/passwd" as operation_id).
journal_path() {
  local hash
  hash=$(printf '%s' "$1" | sha256sum | cut -d' ' -f1)
  echo "$JOURNAL_DIR/$hash"
}

# journal_save_atomic <file_path> <record_id> — atomic write via tmp+mv.
# Call immediately after successful POST, BEFORE GET-verify/audit,
# so a crash between POST and journal-save is recoverable on retry
# (retry finds no journal → re-POST → PB idempotency/unique-index rejects
# the duplicate, or the tool re-creates and overwrites journal).
journal_save_atomic() {
  local file="$1" record_id="$2"
  local tmp="${file}.tmp.$$"
  echo "$record_id" > "$tmp"
  mv -f "$tmp" "$file"
}

# pb_get <token> <collection> <id> — GET one record, stdout=JSON.
pb_get() {
  local token="$1" collection="$2" id="$3"
  local resp
  resp=$(curl -sS --fail-with-body \
    "$PB_URL/api/collections/$collection/records/$id" \
    -H "Authorization: Bearer $token" 2>&1) || {
    echo "ERROR: GET $collection/$id failed: $resp" >&2
    return 1
  }
  echo "$resp"
}

# pb_list <token> <collection> <filter> [perPage] — GET list, stdout=JSON.
pb_list() {
  local token="$1" collection="$2" filter="$3" perPage="${4:-50}"
  local url
  url="$PB_URL/api/collections/$collection/records?perPage=$perPage"
  if [ -n "$filter" ]; then
    url="${url}&filter=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "$filter")"
  fi
  local resp
  resp=$(curl -sS --fail-with-body "$url" -H "Authorization: Bearer $token" 2>&1) || {
    echo "ERROR: LIST $collection failed: $resp" >&2
    return 1
  }
  echo "$resp"
}

# pb_create <token> <collection> <json> — POST record, stdout=JSON.
pb_create() {
  local token="$1" collection="$2" json="$3"
  local resp
  resp=$(curl -sS --fail-with-body -X POST \
    "$PB_URL/api/collections/$collection/records" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -d "$json" 2>&1) || {
    echo "ERROR: CREATE $collection failed: $resp" >&2
    return 1
  }
  echo "$resp"
}

# pb_update <token> <collection> <id> <json> — PATCH record, stdout=JSON.
pb_update() {
  local token="$1" collection="$2" id="$3" json="$4"
  local resp
  resp=$(curl -sS --fail-with-body -X PATCH \
    "$PB_URL/api/collections/$collection/records/$id" \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -d "$json" 2>&1) || {
    echo "ERROR: UPDATE $collection/$id failed: $resp" >&2
    return 1
  }
  echo "$resp"
}

# pb_audit <token> <contract_id> <action> <description> — write audit log.
pb_audit() {
  local token="$1" cid="$2" action="$3" desc="$4"
  local json
  json=$(jq -n \
    --arg cid "$cid" --arg action "$action" --arg desc "$desc" \
    '{"contract_id": $cid, "action": $action, "description": $desc}')
  pb_create "$token" "contract_audit_log" "$json" > /dev/null || true
}

# pb_get_rate <currency> — получить exchange_rate_kgs из /rates.
# KGS=1, USD=usd.sell, EUR=eur.sell. stdout=number.
pb_get_rate() {
  local currency="$1"
  currency=$(echo "$currency" | tr '[:lower:]' '[:upper:]')
  case "$currency" in
    KGS) echo "1"; return 0 ;;
  esac
  if [ -z "$BACKEND_URL" ]; then
    echo "ERROR: BACKEND_URL not set — cannot get $currency rate" >&2
    return 1
  fi
  local key resp rate
  key=$(echo "$currency" | tr '[:upper:]' '[:lower:]')
  resp=$(curl -sS --fail-with-body "$BACKEND_URL/rates" 2>&1) || {
    echo "ERROR: /rates failed: $resp" >&2; return 1
  }
  rate=$(echo "$resp" | jq -r ".${key}.sell")
  if [ -z "$rate" ] || [ "$rate" = "null" ] || [ "$rate" = "0" ]; then
    echo "ERROR: rate for $currency is empty/zero in /rates response" >&2; return 1
  fi
  echo "$rate"
}

# pb_check_contract <token> <contract_id> — GET + validate active.
# stdout=office_id. Returns error if cancelled/deleted/rejected.
pb_check_contract() {
  local token="$1" cid="$2"
  local contract
  contract=$(pb_get "$token" "contracts" "$cid") || return 1
  local cancelled deleted rejected office
  cancelled=$(echo "$contract" | jq -r '.is_cancelled')
  deleted=$(echo "$contract" | jq -r '.is_deleted')
  rejected=$(echo "$contract" | jq -r '.is_rejected')
  office=$(echo "$contract" | jq -r '.office')
  if [ "$cancelled" = "true" ]; then
    echo "ERROR: договор отменён" >&2; return 1
  fi
  if [ "$deleted" = "true" ]; then
    echo "ERROR: договор удалён" >&2; return 1
  fi
  if [ "$rejected" = "true" ]; then
    echo "ERROR: договор отклонён" >&2; return 1
  fi
  if [ -z "$office" ] || [ "$office" = "null" ]; then
    echo "ERROR: у договора не указан офис" >&2; return 1
  fi
  echo "$office"
}

# pb_resolve_method <token> <method_name> <currency> — find payment_method_id.
pb_resolve_method() {
  local token="$1" name="$2" currency="$3"
  local search
  case "$name" in
    наличные|Наличные|наличными) search="Наличные $(echo "$currency" | tr '[:lower:]' '[:upper:]')" ;;
    *) search="$name" ;;
  esac
  local resp method_id
  resp=$(pb_list "$token" "payment_methods" "" 100) || return 1
  method_id=$(echo "$resp" | jq -r --arg s "$search" \
    '.items[] | select(.is_active==true) | select(.name==$s or .short_name==$s) | .id' | head -1)
  if [ -z "$method_id" ]; then
    method_id=$(echo "$resp" | jq -r --arg s "$name" \
      '.items[] | select(.is_active==true) | select(.name | test($s; "i")) | .id' | head -1)
  fi
  if [ -z "$method_id" ]; then
    echo "ERROR: способ оплаты '$name' не найден" >&2; return 1
  fi
  echo "$method_id"
}

# pb_resolve_provider <token> <name> — find provider_id by name.
pb_resolve_provider() {
  local token="$1" name="$2"
  local resp
  resp=$(pb_list "$token" "providers" "" 200) || return 1
  local pid
  pid=$(echo "$resp" | jq -r --arg n "$name" \
    '.items[] | select(.is_active != false) | select(.name==$n) | .id' | head -1)
  if [ -z "$pid" ]; then
    pid=$(echo "$resp" | jq -r --arg n "$name" \
      '.items[] | select(.is_active != false) | select(.name | test($n; "i")) | .id' | head -1)
  fi
  if [ -z "$pid" ]; then
    echo "ERROR: поставщик '$name' не найден" >&2; return 1
  fi
  echo "$pid"
}

# pb_find_application <token> <contract_id> <provider_name> [number]
# — find application by contract + provider (+ optional number).
# stdout=application JSON. Returns error if not found.
pb_find_application() {
  local token="$1" cid="$2" pname="$3" number="${4:-}"
  local pid
  pid=$(pb_resolve_provider "$token" "$pname") || return 1
  local filter="contract_id=\"$cid\" && provider_id=\"$pid\" && is_deleted!=true"
  if [ -n "$number" ]; then
    filter="$filter && number=\"$number\""
  fi
  local resp
  resp=$(pb_list "$token" "applications" "$filter" 50) || return 1
  local total
  total=$(echo "$resp" | jq -r '.totalItems')
  if [ "$total" = "0" ]; then
    echo "ERROR: заявка поставщика '$pname'${number:+ ($number)} не найдена" >&2; return 1
  fi
  if [ "$total" -gt 1 ]; then
    # Prefer primary
    local primary
    primary=$(echo "$resp" | jq -r '.items[] | select(.is_primary==true) | .id' | head -1)
    if [ -n "$primary" ]; then
      echo "$resp" | jq ".items[] | select(.id==\"$primary\")"
      return 0
    fi
    echo "ERROR: несколько заявок для '$pname'${number:+ ($number)} — укажите номер" >&2; return 1
  fi
  echo "$resp" | jq '.items[0]'
}
