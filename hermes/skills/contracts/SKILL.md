---
name: contracts
description: "Полная работа с договорами через детерминированные скрипты-инструменты. Hermes вызывает скрипты, не raw curl."
version: 6.0.0
author: Krugo-Bot
metadata:
  hermes:
    tags: [contracts, pocketbase]
---

# Работа с договорами — скрипты-инструменты

## ⚠️ ГЛАВНОЕ ПРАВИЛО

**НЕ ДЕЛАЙ прямые curl-запросы к PocketBase для создания/изменения записей.**
**ТОЛЬКО вызывай скрипты из `/opt/data/skills/contracts/tools/`.**

Скрипты enforce'ют все правила: GET-проверка статусов, resolve relations,
курс валют, pending-статус, идемпотентность, audit. Твоя задача — разобрать
шаблон заявки и вызвать правильные скрипты с правильными аргументами.

**Единственные разрешённые curl-запросы — GET (чтение) для проверки статусов.**

## Скрипты-инструменты

Все скрипты принимают JSON через stdin. Каждый вызов ДОЛЖЕН включать
`operation_id` вида `<chat_id>:<message_id>:<секция>:<индекс>` — он передаётся
в промпте как "operation_id prefix".

### add_supplier.sh
Добавляет поставщика к договору (новая заявка).
```bash
echo '{"operation_id":"PREFIX:add:1", "contract_id":"ID",
  "provider":"KOMPAS", "number":"222222", "amount":45,
  "currency":"USD", "is_primary":false}' | /opt/data/skills/contracts/tools/add_supplier.sh
```

### change_supplier.sh
Меняет поставщика/номер/сумму. Для approved → correction (pending).
Для non-approved → прямой PATCH.
```bash
echo '{"operation_id":"PREFIX:change:1", "contract_id":"ID",
  "old_provider":"BEST SERVICE", "old_number":"777777",
  "new_provider":"ANEX", "new_number":"111222",
  "new_amount":80, "currency":"USD", "reason":"скидка"}' | /opt/data/skills/contracts/tools/change_supplier.sh
```

### cancel_supplier.sh
Создаёт pending отмену approved заявки поставщика.
```bash
echo '{"operation_id":"PREFIX:cancel:1", "contract_id":"ID",
  "provider":"KOMPAS", "number":"222222",
  "reason":"отказ от услуги"}' | /opt/data/skills/contracts/tools/cancel_supplier.sh
```

### create_correction.sh
Создаёт pending корректировку суммы approved заявки.
```bash
echo '{"operation_id":"PREFIX:corr:1", "contract_id":"ID",
  "provider":"ANEX", "number":"111222",
  "new_amount":80, "reason":"скидка"}' | /opt/data/skills/contracts/tools/create_correction.sh
```

### create_payment.sh
Создаёт pending клиентский платёж. Скрипт САМ берёт office из договора,
rate из /rates, resolve method по названию.
```bash
echo '{"operation_id":"PREFIX:pay:1", "contract_id":"ID",
  "amount":500, "currency":"USD", "method":"наличные",
  "date":"2026-07-21", "comment":"аванс"}' | /opt/data/skills/contracts/tools/create_payment.sh
```

### create_refund.sh
Создаёт pending возврат клиенту.
```bash
echo '{"operation_id":"PREFIX:refund:1", "contract_id":"ID",
  "amount":200, "currency":"USD", "reason":"переплата",
  "date":"2026-07-21"}' | /opt/data/skills/contracts/tools/create_refund.sh
```

### create_operator_request.sh
Создаёт pending запрос на оплату поставщику.
```bash
echo '{"operation_id":"PREFIX:op:1", "contract_id":"ID",
  "provider":"ANEX", "number":"111222", "amount":4500,
  "currency":"USD", "type":"полный остаток"}' | /opt/data/skills/contracts/tools/create_operator_request.sh
```

### create_finance_request.sh
Создаёт pending запрос на изменение финансов. **Одно поле за вызов.**
```bash
echo '{"operation_id":"PREFIX:fin:1", "contract_id":"ID",
  "field":"netto_price", "new_value":155,
  "currency":"USD", "reason":"доплата"}' | /opt/data/skills/contracts/tools/create_finance_request.sh
```

## Порядок действий

1. **GET договора** — проверь статусы (разрешённый curl):
```bash
TOKEN=$(curl -sf -X POST "$PB_URL/api/collections/_superusers/auth-with-password" \
  -H "Content-Type: application/json" \
  -d "{\"identity\":\"$PB_USER\",\"password\":\"$PB_PASS\"}" | jq -r '.token')
curl -sf "$PB_URL/api/collections/contracts/records/CID" -H "Authorization: Bearer $TOKEN" | \
  jq '{finance_status, is_cancelled, is_deleted, is_rejected, brutto_price, netto_price, tour_operator, office}'
curl -sf -G "$PB_URL/api/collections/applications/records" \
  --data-urlencode 'filter=(contract_id="CID")' --data-urlencode 'expand=provider_id' \
  -H "Authorization: Bearer $TOKEN" | jq '.items[] | {id, number, amount, currency, status, finance_status, is_primary, provider: .expand.provider_id.name}'
```

2. **Проверь статусы:**
   - `is_cancelled`/`is_deleted`/`is_rejected` = true → СТОП, сообщи об ошибке.
   - Запиши `finance_status` каждой заявки — нужно для выбора скрипта.

3. **Для каждой секции шаблона вызови скрипт:**

   | Секция шаблона | Скрипт |
   |---|---|
   | Поставщик #N: изменить | `change_supplier.sh` |
   | Поставщик #N: добавить | `add_supplier.sh` |
   | Поставщик #N: отменить | `cancel_supplier.sh` |
   | Сумма поставщика: A → B (approved) | `create_correction.sh` |
   | Нетто/Брутто договора: A → B | `create_finance_request.sh` (один выз на поле) |
   | Платёж клиента | `create_payment.sh` |
   | Возврат клиенту | `create_refund.sh` |
   | Оплата поставщику | `create_operator_request.sh` |

4. **operation_id** для каждого вызова: `PREFIX:секция:индекс`.
   Например: `12345:67890:pay:1`, `12345:67890:change:1`, `12345:67890:fin:1`.

5. Покажи результат по каждой секции:
   - «Поставщик #1: создана корректировка (pending) — бухгалтер одобрит»
   - «Платёж: создан 500 USD (rate=87.8) — бухгалтер подтвердит»
   - «Нетто: создан фин-запрос (pending)»
   - Если скрипт вернул ERROR — покажи сообщение.

## ⚠️ ЗАПРЕЩЕНО

- ❌ Прямой POST/PATCH/DELETE к PB (минуя скрипты)
- ❌ Запись в поле `notes` договора (это JSON-массив, ломает фронтенд)
- ❌ PATCH `brutto_price`/`netto_price` напрямую (только через `create_finance_request.sh`)
- ❌ DELETE applications (только `cancel_supplier.sh`)
- ❌ `exchange_rate_kgs: 0` (скрипт берёт реальный курс)
- ❌ `office_id: ""` (скрипт берёт из договора)
- ❌ `status: "confirmed"` (скрипт ставит `pending`)

## Переменные окружения

Скрипты используют: `PB_URL`, `PB_USER`, `PB_PASS`, `BACKEND_URL`.
Они заданы в docker-compose. Тебе не нужно их передавать.
