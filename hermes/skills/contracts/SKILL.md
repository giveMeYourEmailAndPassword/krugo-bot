---
name: contracts
description: "Update contracts via PocketBase: providers, applications, contracts"
version: 2.1.0
author: Krugo-Bot
metadata:
  hermes:
    tags: [contracts, pocketbase]
---

# Работа с договорами (PocketBase)

База: `$PB_URL`. Superuser: `$PB_USER`/`$PB_PASS`.

## Главное правило

**Всегда сначала ищи application по contract_id.** UI читает `applications`, а не `contracts.price_split`. Меняй оба: application + contract.

## Токен (каждый запрос)

```bash
TOKEN=$(jq -n --arg u "$PB_USER" --arg p "$PB_PASS" '{identity: $u, password: $p}' | curl -s -X POST "$PB_URL/api/collections/_superusers/auth-with-password" -H "Content-Type: application/json" -d @- | jq -r '.token')
```

## Порядок

### 1. Найти поставщика в providers
```bash
curl -s "$PB_URL/api/collections/providers/records?perPage=100" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.name | test(\"ИМЯ\"; \"i\")) | {id, name}"
```

### 2. Найти application по contract_id (ВАЖНО: URL-encode фильтр)
```bash
cid="ID_ДОГОВОРА"
curl -s -G "$PB_URL/api/collections/applications/records" \
  --data-urlencode "filter=(contract_id=\"$cid\")" \
  -H "Authorization: Bearer $TOKEN" | jq ".items[] | {id, number, amount, currency, provider_id}"
```

### 3. PATCH application
```bash
jq -n '{"provider_id":"ID_ПОСТАВЩИКА", "number":"НОМЕР", "amount":СУММА}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 4. PATCH contract (netto_price + tour_operator)
```bash
jq -n '{"netto_price":СУММА, "tour_operator":"ИМЯ"}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 5. Проверка
```bash
# Application
curl -s "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ?expand=provider_id" -H "Authorization: Bearer $TOKEN" | jq "{number, amount, provider: .expand.provider_id.name}"
# Contract
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq "{tour_operator, netto_price}"
```
