---
name: contracts
description: "Полная работа с договорами: поставщики, финансы, платежи, возвраты, оплаты, корректировки. Создаёт pending-запросы для approved-заявок."
version: 5.1.0
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

## ⚠️ КРИТИЧЕСКИЕ ПРАВИЛА

1. **НИКОГДА не пиши в поле `notes` договора.** Это JSON-массив. Запись строки ломает фронтенд (`notes.map is not a function`).
2. **НИКОГДА не PATCH `brutto_price`/`netto_price`/`actual_netto_price` напрямую.** Только через `finance_change_requests` (pending).
3. **НИКОГДА не DELETE applications.** Только soft-cancel (`status=cancelled, finance_status=cancelled, is_deleted=true`) или `application_corrections` (type=cancellation).
4. **Проверяй `finance_status` перед каждым действием:**
   - Договор: `is_cancelled`/`is_deleted`/`is_rejected` → СТОП.
   - Application: `finance_status=approved` (или пусто=approved) → НЕ правь сумму напрямую, создай correction.
5. **Для платежей ВСЕГДА resolve `payment_method_id` и `office_id` и `exchange_rate_kgs`.** Не стави 0 или пустую строку.

## Структура коллекций

- `contracts` — tour_operator, netto_price, brutto_price, tour_amount_currency, finance_status, is_cancelled, is_deleted, is_rejected, **office** (relation)
- `applications` — provider_id, number, amount, currency, is_primary, contract_id, status, finance_status, is_deleted
- `payments` — contract_id, amount, currency, **payment_method_id** (relation), **office_id** (relation), **exchange_rate_kgs** (num), status, is_confirmed, comment, payment_date, change_amount, change_currency
- `client_refunds` — contract_id, amount, currency, refund_date, reason, comment, status
- `operator_payment_requests` — contract_id, application_id, request_type, is_prepayment, requested_amount, currency, status
- `application_corrections` — contract_id, application_id, type, field, old_amount, new_amount, old_currency, new_currency, status, reason
- `finance_change_requests` — contract_id, field, old_value, new_value, currency, status, reason
- `providers` — name, is_active
- `payment_methods` — name, short_name, is_active
- `contract_audit_log` — contract_id, action, old_value, new_value, description

## Порядок действий

### 1. GET договора и всех заявок

```bash
cid="ID_ДОГОВОРА"
CONTRACT=$(curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN")
echo "$CONTRACT" | jq "{finance_status, is_cancelled, is_deleted, is_rejected, brutto_price, netto_price, tour_amount_currency, tour_operator, office}"
curl -s -G "$PB_URL/api/collections/applications/records" --data-urlencode "filter=(contract_id=\"$cid\")" --data-urlencode "expand=provider_id" -H "Authorization: Bearer $TOKEN" | jq ".items[] | {id, number, amount, currency, status, finance_status, is_primary, is_deleted, provider: .expand.provider_id.name}"
```

Проверь статусы (правила выше). Если хоть один флаг = true → СТОП, сообщи об ошибке.

### 2. Обработай каждую секцию шаблона

---

#### Поставщик #N: изменить

```
Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX
  Номер заявки был: 777777
  Номер заявки стал: 111222
  Сумма: 85 → 80
```

1. Найти application по `contract_id + number (был) + provider name (был)`
2. Найти нового provider_id по имени «Стал» в `providers`:
```bash
curl -s "$PB_URL/api/collections/providers/records?perPage=200" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.name | test(\"ИМЯ\"; \"i\")) | {id, name}"
```
3. Проверить `finance_status` найденной заявки:
   - **approved** (или пусто) → создать `application_correction`:
```bash
jq -n --arg cid "$cid" --arg aid "APP_ID" '{"contract_id": $cid, "application_id": $aid, "type":"correction", "field":"amount", "old_amount":85, "new_amount":80, "old_currency":"USD", "new_currency":"USD", "status":"pending", "reason":"Изменение поставщика"}' | curl -s -X POST "$PB_URL/api/collections/application_corrections/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```
   - **не approved** → PATCH напрямую (только provider_id + number, НЕ amount):
```bash
jq -n '{"provider_id":"НОВЫЙ_ID", "number":"НОВЫЙ_НОМЕР"}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/APP_ID" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```
4. Если это основной поставщик (is_primary) → обнови `contracts.tour_operator` на новое имя:
```bash
jq -n '{tour_operator:"НОВОЕ_ИМЯ"}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

---

#### Поставщик #N: добавить

```
Поставщик #2: добавить
  Название: KOMPAS
  Номер заявки: 222222
  Сумма: 45
  Валюта: USD
```

1. Найти provider_id по имени в `providers`
2. POST в `applications`:
```bash
jq -n '{"contract_id":"ID", "provider_id":"PID", "number":"222222", "amount":45, "currency":"USD", "type":"supplier", "is_primary":false, "status":"active", "finance_status":"approved"}' | curl -s -X POST "$PB_URL/api/collections/applications/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

---

#### Поставщик #N: отменить

```
Поставщик #3: отменить
  Был: KOMPAS
  Номер заявки: 222222
```

1. Найти application по `contract_id + number + provider name`
2. Если **approved** → создать `application_correction` (type=cancellation):
```bash
jq -n --arg cid "$cid" --arg aid "APP_ID" '{"contract_id": $cid, "application_id": $aid, "type":"cancellation", "field":"amount", "old_amount":45, "new_amount":45, "old_currency":"USD", "new_currency":"USD", "status":"pending", "reason":"Отмена поставщика"}' | curl -s -X POST "$PB_URL/api/collections/application_corrections/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```
   (old_amount = new_amount = текущая сумма, НЕ 0 — PB reject'ит 0)
3. Если **не approved** → PATCH напрямую:
```bash
jq -n '{"status":"cancelled", "finance_status":"cancelled", "is_deleted":true}' | curl -s -X PATCH "$PB_URL/api/collections/applications/records/APP_ID" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

---

#### Нетто/Брутто договора

```
Нетто договора: 80 → 95
Брутто договора: 100 → 120
Валюта: USD
```

Для КАЖДОГО поля — отдельный `finance_change_requests`:
```bash
# Нетто
jq -n '{"contract_id":"ID", "field":"netto_price", "old_value":80, "new_value":95, "currency":"USD", "status":"pending", "reason":"Изменение финансов"}' | curl -s -X POST "$PB_URL/api/collections/finance_change_requests/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
# Брутто
jq -n '{"contract_id":"ID", "field":"brutto_price", "old_value":100, "new_value":120, "currency":"USD", "status":"pending", "reason":"Изменение финансов"}' | curl -s -X POST "$PB_URL/api/collections/finance_change_requests/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

Затем PATCH договор — только `finance_status`:
```bash
jq -n '{"finance_status":"pending"}' | curl -s -X PATCH "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

**НЕ PATCH brutto_price/netto_price напрямую!** Только через finance_change_requests.

---

#### Платёж клиента

```
Платёж клиента:
  Сумма: 500
  Валюта: USD
  Способ: наличные
  Дата: 2026-07-21
  Комментарий: аванс
```

**ОБЯЗАТЕЛЬНО выполни все 3 шага до POST:**

1. Resolve `payment_method_id` — ищи по валюте + названию:
```bash
curl -s "$PB_URL/api/collections/payment_methods/records?perPage=100" -H "Authorization: Bearer $TOKEN" | jq ".items[] | select(.is_active==true) | {id, name, short_name}"
```
Маппинг: «наличные USD» → `Наличные USD`, «наличные KGS» → `Наличные KGS`, «наличные EUR» → `Наличные EUR`. Если способ «наличные» + валюта USD → выбери `Наличные USD`.

2. Получи `office_id` из договора:
```bash
curl -s "$PB_URL/api/collections/contracts/records/$cid" -H "Authorization: Bearer $TOKEN" | jq -r '.office'
```

3. Получи `exchange_rate_kgs`:
```bash
curl -s "$BACKEND_URL/rates" | jq '.usd.sell, .eur.sell'
```
- KGS → `exchange_rate_kgs: 1`
- USD → `exchange_rate_kgs: <usd.sell>` (например 87.8)
- EUR → `exchange_rate_kgs: <eur.sell>` (например 100.7)

4. POST в `payments`:
```bash
jq -n \
  --arg cid "$cid" \
  --arg method_id "PAYMENT_METHOD_ID" \
  --arg office_id "OFFICE_ID" \
  --arg comment "аванс | от: @user (tg:123)" \
  '{
    "contract_id": $cid,
    "amount": 500,
    "currency": "USD",
    "payment_method_id": $method_id,
    "office_id": $office_id,
    "exchange_rate_kgs": 87.8,
    "status": "pending",
    "is_confirmed": false,
    "comment": $comment,
    "payment_date": "2026-07-21"
  }' | curl -s -X POST "$PB_URL/api/collections/payments/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

**НЕ отправляй `exchange_rate_kgs: 0`!** Всегда получай реальный курс.
**НЕ отправляй `office_id: ""`!** Всегда бери из договора.

---

#### Возврат клиенту

```
Возврат клиенту:
  Сумма: 30000
  Валюта: KGS
  Причина: отмена тура
  Дата: 2026-07-21
```

POST в `client_refunds`:
```bash
jq -n \
  --arg cid "$cid" \
  '{
    "contract_id": $cid,
    "amount": 30000,
    "currency": "KGS",
    "refund_date": "2026-07-21",
    "reason": "cancellation",
    "comment": "Возврат клиенту | от: @user",
    "status": "pending"
  }' | curl -s -X POST "$PB_URL/api/collections/client_refunds/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

reason маппинг: «отмен» → `cancellation`, «переплат» → `overpayment`, «частичн» → `partial_refund`, иначе → `other`.

---

#### Оплата поставщику

```
Оплата поставщику:
  Поставщик: ANEX
  Номер заявки: 111222
  Сумма: 4500
  Валюта: USD
  Тип: полный остаток
```

1. Найти application по `contract_id + provider name + number`
2. POST в `operator_payment_requests`:
```bash
jq -n \
  --arg cid "$cid" \
  --arg aid "APP_ID" \
  '{
    "contract_id": $cid,
    "application_id": $aid,
    "request_type": "full_remaining",
    "is_prepayment": false,
    "requested_amount": 4500,
    "currency": "USD",
    "status": "pending",
    "comment": "Оплата поставщику | от: @user"
  }' | curl -s -X POST "$PB_URL/api/collections/operator_payment_requests/records" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

Тип: «полный остаток» → `full_remaining`, «аванс» → `advance` (is_prepayment=true).

---

### 3. Аудит

После ВСЕХ операций запиши в `contract_audit_log`:
```bash
jq -n --arg cid "$cid" --arg action "update" --arg desc "Описание изменений" \
  '{"contract_id": $cid, "action": $action, "description": $desc}' | \
curl -s -X POST "$PB_URL/api/collections/contract_audit_log/records" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d @-
```

### 4. Проверка

Повторный GET договора и заявок, покажи текущее состояние.

## Формат ответа

Покажи что сделано по каждой секции:
- «Поставщик #1: создана корректировка (pending) — бухгалтер одобрит»
- «Поставщик #2: добавлен»
- «Платёж: создан 500 USD (pending, курс 87.8) — бухгалтер подтвердит»
- «Нетто: создан фин-запрос (pending)»
- Если ошибка: «Поставщик ANEX не найден в справочнике»
