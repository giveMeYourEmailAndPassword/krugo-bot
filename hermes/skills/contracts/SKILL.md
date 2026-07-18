---
name: contracts
description: "Update contracts: providers, applications, contracts"
version: 2.2.0
author: Krugo-Bot
metadata:
  hermes:
    tags: [contracts, pocketbase]
---

# Работа с договорами

База: `$PB_URL`. Superuser: `$PB_USER`/`$PB_PASS`.

## Режим: выполняй сразу, без подтверждения

## Токен (каждый запрос)

```bash
TOKEN=$(jq -n --arg u "$PB_USER" --arg p "$PB_PASS" '{identity: $u, password: $p}' | curl -s -X POST "$PB_URL/api/collections/_superusers/auth-with-password" -H "Content-Type: application/json" -d @- | jq -r '.token')
```

## Порядок

### 1. Найти поставщика в providers (если меняется)
```bash
curl -s "$PB_URL/api/collections/providers/records?perPage=100" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.name | test(\"ИМЯ\"; \"i\")) | {id, name}"
```

### 2. Найти application по contract_id
```bash
cid="ID_ДОГОВОРА"
curl -s -G "$PB_URL/api/collections/applications/records" --data-urlencode "filter=(contract_id=\"$cid\")" -H "Authorization: Bearer $TOKEN" | jq ".items[] | {id, number, amount, currency, provider_id}"
```

### 3. PATCH application
```bash
jq -n '{"provider_id":"ID", "number":"НОМЕР", "amount":СУММА}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 4. PATCH contract — ВСЕ изменяемые поля разом
```bash
jq -n '{"netto_price":СУММА, "brutto_price":СУММА, "tour_operator":"ИМЯ"}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 5. Проверка
```bash
curl -s "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ?expand=provider_id" -H "Authorization: Bearer $TOKEN" | jq "{number, amount, currency, provider: .expand.provider_id.name}"
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq "{tour_operator, netto_price, brutto_price}"
```
