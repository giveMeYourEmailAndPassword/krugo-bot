---
name: contracts
description: "Update contracts with numbered supplier blocks: изменить/добавить/удалить"
version: 3.0.0
author: Krugo-Bot
metadata:
  hermes:
    tags: [contracts, pocketbase]
---

# Работа с договорами

База: `$PB_URL`. Superuser: `$PB_USER`/`$PB_PASS`. Режим: выполняй сразу.

## Токен

```bash
TOKEN=$(jq -n --arg u "$PB_USER" --arg p "$PB_PASS" '{identity: $u, password: $p}' | curl -s -X POST "$PB_URL/api/collections/_superusers/auth-with-password" -H "Content-Type: application/json" -d @- | jq -r '.token')
```

## Структура

- `applications` — заявки: provider_id, number, amount, currency, is_primary, contract_id
- `contracts` — договор: tour_operator, netto_price, brutto_price
- У договора может быть НЕСКОЛЬКО applications

## Парсинг заявки

### Блоки поставщиков

```
Поставщик #N: изменить
  Был: НАЗВАНИЕ
  Стал: НАЗВАНИЕ
  Номер заявки был: XXXXXX
  Номер заявки стал: XXXXXX
  Сумма была: ЧИСЛО
  Сумма стала: ЧИСЛО

Поставщик #N: добавить
  Название: НАЗВАНИЕ
  Номер заявки: XXXXXX
  Сумма: ЧИСЛО

Поставщик #N: удалить
  Был: НАЗВАНИЕ
  Номер заявки был: XXXXXX
```

### Финансы договора

```
Нетто договора было: ЧИСЛО
Нетто договора стало: ЧИСЛО
Брутто договора было: ЧИСЛО
Брутто договора стало: ЧИСЛО
```

## Порядок

### 1. GET текущего состояния

```bash
cid="ID_ДОГОВОРА"
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq "{tour_operator, netto_price, brutto_price}"
curl -s -G "$PB_URL/api/collections/applications/records" --data-urlencode "filter=(contract_id=\"$cid\")" --data-urlencode "expand=provider_id" -H "Authorization: Bearer $TOKEN" | jq ".items[] | {id, number, amount, is_primary, provider: .expand.provider_id.name}"
```

### 2. Для каждого блока «Поставщик #N»

**Идентификация:** найти application по `номеру заявки И поставщику`. Если несколько — ошибка, не продолжай.

**изменить** — PATCH только указанных полей:
```bash
# Новый поставщик + номер + сумма
jq -n '{"provider_id":"НОВЫЙ_ID", "number":"НОМЕР", "amount":СУММА}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/APP_ID" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-

# Только номер
jq -n '{"number":"НОМЕР"}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/APP_ID" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

**добавить** — POST:
```bash
jq -n '{"contract_id":"ID_ДОГОВОРА", "provider_id":"ID", "number":"НОМЕР", "amount":СУММА, "currency":"USD", "type":"supplier", "is_primary":false, "status":"active"}' | curl -s -X POST "$PB_URL/api/collections/applications/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

**удалить** — DELETE:
```bash
curl -s -X DELETE "$PB_URL/api/collections/applications/records/APP_ID" -H "Authorization: Bearer $TOKEN"
```

### 3. Найти поставщика в providers

```bash
curl -s "$PB_URL/api/collections/providers/records?perPage=100" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.name | test(\"ИМЯ\"; \"i\")) | {id, name}"
```
Если найдено 0 или >1 — ошибка. После успешного PATCH основного поставщика также обнови `contracts.tour_operator` на его имя.

### 4. Договор — PATCH только если «Нетто/Брутто договора» указаны

```bash
jq -n '{"netto_price":95, "brutto_price":120}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 6. Аудит — записать каждое изменение

После ВСЕХ PATCH/POST/DELETE запиши в `contract_audit_log`:

```bash
jq -n --arg cid "$cid" --arg action "update_supplier" --arg old "СТАРОЕ" --arg new "НОВОЕ" \
  '{"contract_id": $cid, "action": $action, "old_value": $old, "new_value": $new}' | \
curl -s -X POST "$PB_URL/api/collections/contract_audit_log/records" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

Формат записи:
- `contract_id` — ID договора
- `action` — что сделано: `update_supplier`, `update_amount`, `update_netto`, `update_brutto`, `add_supplier`, `delete_supplier`
- `old_value` — JSON: `{"field": "tour_operator", "was": "BEST SERVICE"}`
- `new_value` — JSON: `{"field": "tour_operator", "became": "ANEX"}`
- `description` — текстовое описание: «Поставщик: BEST SERVICE → ANEX»

На каждое изменение — отдельная запись.

### 7. Проверка

