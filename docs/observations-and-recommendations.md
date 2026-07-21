# Наблюдения и рекомендации по расширению Krugo Bot

> Дата: 2026-07-21. Основано на аудите `krugo-bot` и полного исследования доменов проекта `contracts`.

## 1. Текущее состояние бота

### Что бот умеет сейчас
- Принимает шаблонные заявки в Telegram-группе (детектор `≥2 маркеров` из ~23).
- Валидирует шаблон (заполненность + наличие ссылки `baza.krugo.tours/contracts/<id>`).
- Создаёт запись в локальной SQLite (`requests`) с ID `KRUGOSVET-<n>`.
- Через `hermes-proxy` вызывает Hermes Agent, который выполняет PATCH в PocketBase:
  - `applications` — изменить/добавить/удалить поставщика (provider_id, number, amount).
  - `contracts` — `tour_operator`, `netto_price`, `brutto_price`.
  - `contract_audit_log` — пишет запись аудита.
- Inline-кнопки: `ready_for_dev`, `needs_clarification`, `assigned`, `done`.
- Команды: `/status KRUGOSVET-XXXXX`, `/history <contract_id>` (читает `contract_audit_log` из PB).

### Что бот НЕ умеет (но менеджеры делают в вебе)
- Регистрировать клиентские платежи.
- Запрашивать/подтверждать оплаты поставщикам.
- Создать возврат клиенту.
- Создать корректировку approved-заявки поставщика.
- Изменить brutto/netto на подтверждённом договоре (через finance_change_request).
- Отменить договор (cancellation settlement).
- Работать со сделками (claim, смена этапа).
- Видетьpending-запросы на подтверждение.
- Подтверждать что-либо как бухгалтер.

---

## 2. Критическая проблема безопасности: superuser без identity

### Как сейчас работает авторизация

```
Telegram → krugo-bot → hermes-proxy → Hermes Agent → PocketBase (PB_USER/PB_PASS = superuser)
```

Hermes-агент авторизуется в PocketBase как **superuser** (`_superusers` коллекция). Superuser-запросы:
- **Обходят PB rules** (`listRule`/`createRule`/`updateRule`/`deleteRule` не применяются).
- Часть guards имеет явный `isSuperuserRequest()` bypass (например, `currency_valuation_guard`, `financial_review_guard`, `operator_payments` direct-write block, `payroll_ledger_guard`).
- **НО**: SQL-триггеры (`immutable_payments`, `settled_payments`) выполняются на уровне БД — superuser их не обходит.
- Model hooks (`onRecordAfterCreateSuccess` и т.д.) тоже выполняются для superuser (не имеют bypass).

### Почему это проблема

1. **Нет атрибуции действий конкретному менеджеру.** Hermes пишет от superuser. Поле `created_by` либо null, либо фиксированное. Невозможно определить, кто именно внёс изменение — любой allowlisted Telegram-юзер действует от одного технического аккаунта.

2. **Нет enforcement прав.** Даже если маппить `telegram_id → pb_user_id` и проставлять `created_by`, это даёт лишь **атрибуцию**, но не **авторизацию**. PB-rules не применяются к superuser-запросу, поэтому `financial_review_guard` (который блокирует менеджеру подтверждение платежа) не сработает. Любой allowlisted пользователь через ошибку/инъекцию в промпте может выполнить любую операцию, включая подтверждение платежа или изменение approved-финансов.

3. **LLM не должен enforce authorization.** Hermes (DeepSeek) парсит свободный текст и решает, какой API вызвать. Это non-deterministic — нельзя гарантировать, что промпт-инъекция не заставит модель вызвать `/api/payments/confirm` вместо `create`. Авторизация — инвариант системы, он должен быть детерминированным.

4. **Расхождение с веб-интерфейсом.** В `baza.krugo.tours` менеджер ограничен PB-rules + хуками. Через бота тот же менеджер технически может всё. Это нарушает единый контракт безопасности.

### Рекомендация: детерминированный backend-adapter

**Архитектура:**
```
Telegram → krugo-bot → [Hermes: парсит намерение → типизированная команда JSON]
                ↓
          [Go backend-adapter в krugo-bot]:
            1. Маппит Telegram-ID → PB user (через PB auth или таблицу маппинга)
            2. Авторизуется в PB от имени этого юзера (НЕ superuser)
            3. Проверяет роль (manager/senior/bookkeeper/admin)
            4. Вызывает правильный endpoint согласно роли:
               - PB REST (rules + hooks применяются) — для create-pending операций
               - Hono backend (contracts-backend) — для confirm/approve/cancel
               - PB custom routes (/api/operator-payments/commit и т.д.)
            5. Возвращает результат в бота
```

**Ключевые принципы:**
- **Hermes = парсер намерения.** На выходе — типизированная JSON-команда (`{action: "create_payment", contract_id, amount, currency, ...}`), а не bash-скрипт. Бот валидирует схему команды детерминированно.
- **Adapter = enforcement.** Go-код проверяет роль и вызывает endpoint. Если менеджер прислал «подтвердить платёж» — adapter видит роль=manager и отказывает (или создаёт запрос на подтверждение).
- **Identity per-request.** Каждый запрос бота авторизуется в PB от конкретного PB-юзера. PB-rules и хуки применяются нативно — тот же контракт, что в вебе.
- **Superuser только для read-only.** `/history`, `/payments`, `/finance` (просмотр) — можно superuser, т.к. read. Мутации — только от реального юзера.

**Что для этого нужно:**
1. Таблица `telegram_users` в SQLite: `telegram_id, pb_user_id, pb_email, pb_password_hash` (или токен).
2. Go-функция `authAsPBUser(telegramID) → pb token` (через `POST /api/collections/users/auth-with-password`).
3. Go-adapter с switch по типу команды → вызов нужного endpoint с токеном юзера.
4. Hermes skill переписать: парсит заявку → emits JSON-команду, а не выполняет curl.

---

## 3. Полная карта операций: что бот может добавить

### Категория A — менеджерские (create-pending, бухгалтер подтвердит в вебе)

Эти операции менеджер может делать сам через PB (rules разрешают `relatedContract`). Бот создаёт pending-запрос, бухгалтер подтверждает в `/bookkeeping`.

| # | Операция | PB collection | Endpoint | Шаблон |
|---|---|---|---|---|
| A1 | Регистрация клиентского платежа | `payments` | `pb.payments.create({status:'pending'})` | «Заявка на платёж» |
| A2 | Редактирование pending-платежа | `payments` | `pb.payments.update` (нефин. поля) | «Заявка на правку платежа» |
| A3 | Удаление pending-платежа | `payments` | `pb.payments.update({is_deleted:true})` | «Заявка на удаление платежа» |
| A4 | Возврат клиенту | `client_refunds` | `pb.client_refunds.create({status:'pending'})` | «Заявка на возврат» |
| A5 | Запрос на оплату поставщику | `operator_payment_requests` | `pb.operator_payment_requests.create({status:'pending'})` | «Заявка на оплату поставщику» |
| A6 | Корректировка суммы поставщика (approved app) | `application_corrections` | `pb.application_corrections.create({type:'correction', status:'pending'})` | «Заявка на корректировку поставщика» |
| A7 | Отмена поставщика (approved app) | `application_corrections` | `pb.application_corrections.create({type:'cancellation', status:'pending'})` | «Заявка на отмену поставщика» |
| A8 | Изменение brutto/netto (approved contract) | `finance_change_requests` | `pb.finance_change_requests.create({status:'pending'})` | «Заявка на изменение финансов» |
| A9 | Отмена договора | `finance_change_requests` (cancellation_settlement) | `POST /api/cancellation-settlements/create` (Hono) | «Заявка на отмену договора» |

### Категория B — бухгалтерские (approve/confirm, только для бухгалтеров)

Требуют роль bookkeeper/admin/senior. Бот должен проверять роль Telegram-юзера.

| # | Операция | Endpoint | Примечание |
|---|---|---|---|
| B1 | Подтвердить платёж | `POST /api/payments/confirm/:id` (Hono) | Capture rate snapshot, immutable |
| B2 | Отклонить платёж | `pb.payments.update({status:'rejected'})` | `canReview` gate |
| B3 | Одобрить возврат клиента | `pb.client_refunds.update({status:'approved'})` | `decision` gate |
| B4 | Отклонить возврат | `pb.client_refunds.update({status:'rejected'})` | `decision` gate |
| B5 | Одобрить корректировку поставщика | `pb.application_corrections.update({status:'approved'})` | `decision` gate + applies to app |
| B6 | Одобрить фин. запрос (brutto/netto) | `pb.finance_change_requests.update({status:'approved'})` | `decision` gate |
| B7 | Одобрить отмену договора | `POST /api/cancellation-settlements/approve` (PB custom) | atomic, accountant/admin only |
| B8 | Записать оплату поставщику | `POST /api/operator-payments/commit` (PB custom) | atomic, accountantOnly |

### Категория C — менеджерские immediate (без approval)

| # | Операция | Endpoint | Примечание |
|---|---|---|---|
| C1 | Забрать сделку (claim) | `PATCH /api/deals/:id {manager:self}` (Hono) | owner/unassigned |
| C2 | Сменить этап сделки | `PATCH /api/deals/:id {stage}` (Hono) | owner only |
| C3 | Создать/изменить поставщика (non-approved app) | `pb.applications.create/update` | текущий skill, только для не-approved |
| C4 | Создать поставщика в providers | `pb.providers.create` | если нового нет в справочнике |

### Категория D — read-only (для всех)

| # | Операция | Источник |
|---|---|---|
| D1 | `/payments <contract_id>` | `pb.payments.getList(filter=contract_id)` |
| D2 | `/refunds <contract_id>` | `pb.client_refunds.getList` |
| D3 | `/operators <contract_id>` | `pb.operator_payments + operator_payment_requests` |
| D4 | `/finance <contract_id>` | `contracts + applications + finance_status + pending requests` |
| D5 | `/deal <id>` | `GET /api/deals/:id` (Hono) |
| D6 | `/pending` | Все pending-запросы менеджера (платежи, возвраты, корректировки, фин. запросы) |
| D7 | `/inbox` (бухгалтер) | Все pending-запросы на подтверждение |

---

## 4. Рекомендации по реализации

### Этап 1 — Backend adapter (фундамент)

Без этого любое расширение небезопасно. Порядок:

1. **Таблица `telegram_users`** в SQLite:
   ```sql
   CREATE TABLE telegram_users (
     telegram_id INTEGER PRIMARY KEY,
     pb_user_id TEXT NOT NULL,
     pb_email TEXT NOT NULL,
     pb_password TEXT NOT NULL,  -- или pb_token, обновляемый
     role TEXT,                   -- cache: manager/senior/bookkeeper/admin
     office_id TEXT,
     updated_at DATETIME
   );
   ```

2. **Go-adapter `internal/adapter/pb_client.go`**:
   - `AuthUser(telegramID) → (pbToken, role, officeID, error)` — POST `/api/collections/users/auth-with-password`, кэш токена.
   - `CallCommand(cmd TypedCommand, telegramID) → Result` — switch по `cmd.Action`, вызывает нужный endpoint с токеном юзера.

3. **Типизированные команды** (`internal/commands/types.go`):
   ```go
   type TypedCommand struct {
     Action      string  // "create_payment", "approve_refund", ...
     ContractID  string
     Amount      float64
     Currency    string
     // ... поля по типу команды
   }
   ```

4. **Hermes skill переписать** — парсит заявку → emits JSON `TypedCommand`, НЕ выполняет curl. Бот валидирует схему → передаёт в adapter.

### Этап 2 — Менеджерские шаблоны (категория A)

- Расширить `docs/manager-template.md` новыми шаблонами (платёж, возврат, оплата поставщику, корректировка, отмена).
- Расширить `internal/rules/detector.go` маркерами: «платёж», «возврат», «оплату поставщику», «корректировка», «отмена договора».
- Новый Hermes skill (или расширить существующий) для парсинга этих шаблонов в типизированные команды.
- Adapter реализует create-pending для каждой команды категории A.

### Этап 3 — Бухгалтерские команды (категория B)

- `TELEGRAM_BOOKKEEPER_USERS` env (отдельный allowlist).
- Команды `/confirm`, `/approve`, `/reject` — только для бухгалтеров.
- Adapter проверяет роль перед вызовом Hono/PB custom endpoints.

### Этап 4 — Read-only команды (категория D)

- `/payments`, `/refunds`, `/operators`, `/finance`, `/deal`, `/pending`, `/inbox`.
- Можно через superuser (read-only безопасно).

### Этап 5 — Сделки (категория C)

- `PATCH /api/deals/:id` через Hono backend.
- `BACKEND_URL` env + auth.

---

## 5. Риски и технический долг

### Безопасность
- **Текущий superuser-flow — недопустим для новых финансовых операций.** Подтверждение платежа, отмена договора, оплата поставщику — всё это через superuser обходит approval-gate. Нужно внедрить adapter (Этап 1) до любого расширения.
- **Промпт-инъекция в Hermes.** Менеджер может написать заявку, которая заставит Hermes вызвать нежелательный endpoint. Типизированные команды + server-side валидация решают это.
- **Валюта hardcoded USD в skill.** Если менеджер укажет сумму в EUR/KGS — запишется как USD. Нужно парсить валюту из заявки.

### Корректность
- **Нет валидации ownership.** Бот не проверяет, что менеджер владеет договором (`created_by`/`created_by_2`). В PB rules это есть, но superuser обходит. Adapter от реального юзера решает.
- **Нет валидации payment_method.** `payment_method_id` — relation, нужно resolve имя метода в ID.
- **Нет валидации currency enum.** PB принимает только USD/EUR/KGS.
- **Mixed-currency netto.** `recalcContractNetto` пропускает mixed-currency — фронтенд пересчитывает. Бот должен учитывать это при отображении финансов.

### Долг
- `internal/hermes/analyzer.go` + `prompt.go` + `schema.go` — мёртвый код (старый прямой-OpenAI путь). Не используется из `main.go`. Удалить.
- `config.Load()` требует `OPENAI_API_KEY` (config.go:36-38), но он нигде не используется. Мёртвый обязательный конфиг. Убрать требование.
- `main.go:64` `_ = tgBot` — намёк на незавершённый рефакторинг.
- `generateID` (`UnixNano()%100000`) — коллизии при высокой частоте, ID не сортируем.
- Документация: `docs/plans.md` и `README.md` говорят `HERMES-XXXX`, код — `KRUGOSVET-<n>`. Расхождение.

### Аудит
- Hermes пишет в `contract_audit_log`, но система уже имеет `finance_journal` (автоматически из PB-хуков) + `bookkeeping_audit_log`. При работе через adapter от реального юзера PB-хуки будут сами писать в `finance_journal` — дублировать `contract_audit_log` не нужно.
- План в `AGENTS.md` (SQLite `audit_log` + GET before → PATCH → GET after → diff → insert) устарел — PB-хуки уже делают это автоматически, если мутации идут от реального юзера, а не superuser.

---

## 6. Приоритеты

1. **Backend adapter** — без него ничего финансового делать нельзя. Фундамент.
2. **Менеджерские шаблоны категории A** — основная ценность для менеджеров.
3. **Read-only команды** — быстрая победа, можно через superuser.
4. **Бухгалтерские команды** — после того как adapter готов и roles настроены.
5. **Сделки** — отдельный домен, можно позже.
6. **Очистка мёртвого кода** — параллельно, не блокирует.
