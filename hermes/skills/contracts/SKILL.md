---
name: contracts
description: "Update contracts: providers, applications, contracts"
version: 2.3.0
author: Krugo-Bot
metadata:
  hermes:
    tags: [contracts, pocketbase]
---

# Работа с договорами

База: `$PB_URL`. Superuser: `$PB_USER`/`$PB_PASS`.
Режим: выполняй сразу, без подтверждения.

## Токен

```bash
TOKEN=$(jq -n --arg u "$PB_USER" --arg p "$PB_PASS" '{identity: $u, password: $p}' | curl -s -X POST "$PB_URL/api/collections/_superusers/auth-with-password" -H "Content-Type: application/json" -d @- | jq -r '.token')
```

## Порядок

### 1. GET договора — узнай текущие значения
```bash
cid="ID_ДОГОВОРА"
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq "{tour_operator, netto_price, brutto_price}"
curl -s -G "$PB_URL/api/collections/applications/records" --data-urlencode "filter=(contract_id=\"$cid\")" -H "Authorization: Bearer $TOKEN" | jq ".items[] | {id, number, amount, currency, provider_id}"
```

### 2. Найти нового поставщика (только если меняется)
```bash
curl -s "$PB_URL/api/collections/providers/records?perPage=100" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.name | test(\"ИМЯ\"; \"i\")) | {id, name}"
```

### 3. Собрать PATCH — ТОЛЬКО указанные в заявке поля

**НЕ включай поля, которые не просили менять.** Собери payload изменившихся полей.

Для application (если меняется поставщик/номер/сумма):
```bash
# Пример: только номер и сумма
jq -n '{"number":"НОВЫЙ", "amount":НОВАЯ_СУММА}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

Для contract (если меняется нетто/брутто/туроператор):
```bash
# Пример: только нетто
jq -n '{"netto_price":НОВАЯ_СУММА}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-

# Пример: нетто + брутто
jq -n '{"netto_price":92, "brutto_price":110}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 4. Проверка — GET после PATCH

```bash
curl -s "$PB_URL/api/collections/applications/records/ID_ЗАЯВКИ?expand=provider_id" -H "Authorization: Bearer $TOKEN" | jq "{number, amount, provider: .expand.provider_id.name}"
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq "{tour_operator, netto_price, brutto_price}"
```

## Коллекции
- `providers` — поставщики (id, name)
- `applications` — заявки (contract_id, provider_id, number, amount, currency)
- `contracts` — договоры (tour_operator, netto_price, brutto_price, price_split)
