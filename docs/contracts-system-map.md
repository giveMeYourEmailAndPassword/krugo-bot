# Полная система взаимодействия договоров в проекте `contracts`

> Дата: 2026-07-21. Исследование на основе исходного кода `/Users/amantur/Documents/work/contracts`:
> `pb_schema.json`, `pb_hooks/*`, `pb_migrations/*`, `src/api/*`, `src/lib/*`, `backend/src/*`, `AGENTS.md`, `docs/*`.

## Оглавление
1. [Архитектура](#1-архитектура)
2. [Коллекции PocketBase](#2-коллекции-pocketbase)
3. [Роли и доступ](#3-роли-и-доступ)
4. [Жизненный цикл договора](#4-жизненный-цикл-договора)
5. [Клиентские платежи](#5-клиентские-платежи)
6. [Оплаты поставщикам](#6-оплаты-поставщикам)
7. [Возвраты клиентам](#7-возвраты-клиентам)
8. [Корректировки заявок поставщиков](#8-корректировки-заявок-поставщиков)
9. [Финансовые запросы (brutto/netto)](#9-финансовые-запросы-bruttonetto)
10. [Отмена договора (cancellation settlement)](#10-отмена-договора-cancellation-settlement)
11. [Сделки (deals)](#11-сделки-deals)
12. [Зарплата (payroll)](#12-зарплата-payroll)
13. [Ledger и аудит](#13-ledger-и-аудит)
14. [Инварианты и gotchas](#14-инварианты-и-gotchas)

---

## 1. Архитектура

```
browser → baza.krugo.tours (Vite/React SPA)
             ├─→ contracts-backend (Hono) → PocketBase, Bitrix24, WhatCRM, valuta, Bakai API
             ├─→ PocketBase directly ← mutations, auth, realtime
             └─→ API valuta directly ← exchange rates for UI

browser → krugosvet.kg (Next.js 15, Pages Router)
             └─→ PocketBase directly ← public cabinet (no auth, SSR)
```

**Railway services:**
- `website` — Vite SPA (фронтенд для менеджеров/бухгалтеров).
- `database-pb` — PocketBase (built from `Dockerfile.pb`,PB hooks из `pb_hooks/`).
- `contracts-backend` — Hono aggregation backend.
- `API valuta` — курсы валют.

**PB hooks** (`pb_hooks/*.pb.js`) — деплоятся вместе с PocketBase. Реализуют:
- Guards (запрет прямых записей, approval-gate, immutability).
- Авто-аудит (`finance_journal`).
- Авто-пересчёт (`recalcContractNetto`).
- Custom routes (`/api/operator-payments/commit`, `/api/payroll/salary/commit`, `/api/cancellation-settlements/approve`).

**Backend Hono** (`backend/src/`) — агрегация, custom endpoints, внешние интеграции:
- `/api/payments/confirm/:id` — подтверждение платежа (capture rate snapshot).
- `/api/qr/generate`, `/api/qr/:contractId` — QR-коды Bakai.
- `/webhook` — Bakai webhook → pending payment.
- `/api/deals/*` — сделки (sync с Bitrix24, kanban, биржа).
- `/api/cancellation-settlements/create` — создание cancellation settlement.
- `/api/chat/*` — WhatsApp (WhatCRM), Bitrix Open Lines.

---

## 2. Коллекции PocketBase

### `contracts` (`pbc_1836797931`)

**Права** (финальные, из `1784214000_global_accountant_access.js`):
- list/view: `(admin@krugosvet.kg && is_rejected) OR (не admin-email && ownContract)`.
- create: `staff` (active + любая роль).
- update: `ownContract` (accountant OR created_by=self OR created_by_2=self OR senior+office).
- delete: `admin OR (senior && office ?= self.office)`.

**Поля:**

| Группа | Поля |
|---|---|
| **Деньги** | `brutto_price` (num), `netto_price` (num, projection — synced by `recalcContractNetto`), `actual_netto_price` (text), `actual_brutto_price` (text), `actual_netto_updated_at` (text), `override_netto` (legacy), `manual_netto` (legacy bool) |
| **Валюта/тур** | `tour_amount_currency` (select USD/EUR/KGS), `tour_operator` (text, decoupled label), `tour_country`, `hotel`, `food`, `includes`, `start_date`, `end_date`, `fly_date`, `room_type`, `tour_members` (json), `tour_id` |
| **Клиент** | `client_name`, `address`, `phone`, `email`, `passport_id`, `passport_origin`, `passport_activeto`, `phone_2` |
| **Lifecycle** | `is_deleted` (bool), `is_done` (bool), `is_paid` (bool, presentable), `is_rejected` (bool), `rejected_price` (num), `is_cancelled` (bool — **runtime, не в pb_schema.json**), `exclusive` (bool), `sale_type` (text, mini-deal) |
| **Связи** | `office` → offices, `created_by` → users (primary manager), `created_by_2` → users (second manager, 50/50 commission), `payments` → payments (legacy mirror, maxSelect 999) |
| **Номер** | `contracts_num` (text, immutable post-creation via `contracts_guard.pb.js`) |
| **Finance approval** | `finance_status` (select pending/approved/rejected), `finance_pending_brutto`, `finance_pending_netto`, `finance_pending_actual_netto`, `finance_pending_actual_netto_comment`, `finance_submitted_by` → users, `finance_submitted_at`, `finance_approved_by` → users, `finance_approved_at`, `finance_rejection_reason` |
| **Operator submission** | `operator_submission_status` (select pending/submitted/paid/refunded — **runtime**) |
| **Прочее** | `comment`, `notes` (json), `price_split` (json, legacy archive), `applications` (json, legacy mirror), `printed_at` (date) |

**Enum'ы:**
- `tour_amount_currency`: USD, EUR, KGS
- `finance_status`: pending, approved, rejected
- `operator_submission_status`: pending, submitted, paid, refunded

### `applications` (`pbc_2689671926`)

**Права:**
- list/view/create/update: `relatedContract` (accountant OR contract.created_by=self OR created_by_2=self OR senior+office).
- delete: `relatedContract` (но бизнес-логика требует soft-cancel, не delete).

**Поля:**
- `contract_id` (relation→contracts, **required**)
- `provider_id` (relation→providers, **required**)
- `number` (text ≤100)
- `amount` (number, **required**, min 0)
- `currency` (select KGS/USD/EUR, **required**)
- `type` (select tour_operator/supplier/visa/insurance/hotel/transfer/other, **required** — но `tour_operator` deprecated; все новые = `supplier`)
- `is_primary` (bool)
- `status` (select active/cancelled/refunded)
- `finance_status` (select approved/pending/rejected/cancelled)
- `payment_status` (select submitted/paid/refunded + empty — **runtime, DERIVED from operator_payments**)
- `notes` (text ≤500)
- `is_deleted` (bool)
- `created`/`updated` (autodate)

**Index:** `idx_applications_contract_active ON (contract_id, is_deleted, finance_status, status)`

### `payments` (`pbc_631030571`) — клиентские платежи

**Права:**
- list/view/create/update: `relatedContract` (accountant OR contract.created_by/created_by_2=self OR senior+office).
- delete: `admin OR (senior && contract_id.office ?= self.office)`.

**Поля:**
- `comment` (text, **required**)
- `contract_id` (relation→contracts, **required**)
- `amount` (num, **required**)
- `currency` (select USD/EUR/KGS)
- `change_amount` / `change_currency` (сдача)
- `created_by` (relation→users)
- `payment_method_id` (relation→payment_methods `pbc_568792081`)
- `office_id` (relation→offices)
- `receiver__requisite` (text)
- `payment_date` (date)
- `exchange_rate_kgs` (num, snapshot at creation)
- `receipt` / `receipt_kkm` (file×99)
- `is_deleted` (bool — soft delete)
- `status` (select pending/confirmed/rejected)
- `is_confirmed` (bool)
- `confirmed_by` (relation→users)
- `confirmed_at` (date)
- `rejection_reason` (text)
- `qr_source` (select ['qr']) + `contract_qr_id` (relation→contract_qr) + `bakai_transaction_id` + `elqr_id` (text, unique partial index)
- **Valuation snapshot** (server-written, immutable once `accepted_rate_timestamp` set): `contract_currency`, `accepted_rate`, `accepted_converted_amount`, `accepted_rate_timestamp`, `accepted_rate_source`, `accepted_raw_rates` (json), `final_*` counterparts, `revalued_at`, `revalued_by`

**Indexes:** `idx_payments_elqr_id` UNIQUE (partial, elqr_id!=''), `idx_payments_contract_active_created`, `idx_payments_contract_status_date`.

**SQL triggers** (migration `1783965600`):
- `settled_payments_insert/update/delete` — блокируют CRUD платежей по settled contract.
- `immutable_payments` — abort изменения contract_id/amount/change_amount/currency/contract_currency/accepted_* после установки `accepted_rate_timestamp`.

### `ledger_entries` (`pbc_3289777763`) — регистр двойной записи

**Права:**
- list/view: `staff`.
- create/update: `decision` (admin/senior/bookkeeper/accountant).
- delete: `admin`.

**Поля:**
- `office_id`, `account_id` (relation→accounts), `account_name`, `account_kind` (select cash/bank_transfer/mobile_banking/card/expense/encashment/virtual/operator)
- `source_type` (select: payment/expense/encashment/transfer/opening_balance/operator_payment/operator_refund/client_refund/payroll/adjustment/office_transfer/application)
- `source_id` (text), `source_line` (text) — **idempotency key**: UNIQUE INDEX `idx_ledger_source_line` ON (source_type, source_id, source_line)
- `direction` (in/out), `amount`, `signed_amount`, `currency` (KGS/USD/EUR)
- `entry_date`, `status` (posted/void)
- `counterparty`, `note`, `created_by`, `posted_at`, `contract_id`

### `operator_payments` (`pbc_741930521`) — оплаты поставщикам

**Права:** list/view: `relatedContract`; create/update: `accountantOnly`; delete: `admin`.
**Direct REST writes REJECTED** at request hook — только через custom route `/api/operator-payments/commit`.

**Поля** (часть в pb_schema.json, часть — runtime в `schema_bootstrap_lib.js`):
- `contract_id`, `application_id`, `request_id` (relation→operator_payment_requests), `operation_id` (idempotency)
- `amount`, `currency`, `payment_date`, `account_id`, `office_id`
- `application_amount`, `application_currency` (snapshot)
- `voided` (bool), `void_reason`, `voided_at`, `voided_by`
- `comment`, `created_by`

### `operator_refunds` (`pbc_4258225215`) — возвраты от поставщиков

**Права:** как operator_payments. Direct REST writes REJECTED.
**Поля** (runtime в `schema_bootstrap_lib.js`): `operation_id`, `contract_id`, `office_id`, `account_id`, `application_id`, `amount`, `currency`, `refund_date`, `reason`, `comment`, `created_by`, `voided`+`void_*`.

### `operator_payment_requests` (runtime) — заявки на оплату поставщику

**Права:** list/view/create/update: `relatedContract` (manager может create для своего контракта).

**Поля:**
- `contract_id`, `application_id`, `request_type` (select full_remaining/advance), `is_prepayment` (bool)
- `requested_amount`, `status` (select pending/partially_paid/paid/rejected/cancelled)
- `snapshot` (json), `paid_amount`
- `reviewed_by`/`reviewed_at`, `rejection_reason`

### `client_refunds` (`pbc_187894579`) — возвраты клиентам

**Права:**
- list/view/create: `relatedContract`.
- update: `decision && relatedContract` (менеджер НЕ может approve/reject).
- delete: `admin`.

**Поля:**
- `contract_id` (required), `application_id`, `amount` (required), `currency` (KGS/USD/EUR, required)
- `refund_date` (required), `reason` (select cancellation/overpayment/partial_refund/other)
- `comment`, `receipt` (file), `payment_method_id`
- `status` (select pending/approved/rejected, required)
- `rejection_reason`, `created_by`/`reviewed_by`
- `cancellation_request_id` (relation→finance_change_requests, runtime + unique index)

### `application_corrections` (`pbc_647443222`) — корректировки заявок

**Права:**
- list/view/create: `relatedContract`.
- update: `decision && relatedContract` (manager НЕ может approve/reject).
- delete: `admin`.

**Поля:**
- `contract_id` (required), `application_id` (relation→applications, required)
- `field` (text, required), `old_amount`/`new_amount`, `old_currency`/`new_currency`
- `status` (select pending/approved/rejected, required)
- `reason`, `created_by`/`reviewed_by`, `type` (select correction/cancellation)

### `finance_change_requests` (`pbc_4023921900`) — фин. запросы на изменение договора

**Права:**
- list/view/create: `staff`.
- update: `(active) && ((accountant OR senior) OR (self=created_by && status='pending'))`.
- delete: `admin OR (self=created_by && status='pending')`.

**Поля:**
- `contract_id` (required), `field` (select brutto_price/netto_price/actual_netto_price + runtime `cancellation_settlement`)
- `old_value`/`new_value` (num), `currency`, `reason`
- `status` (select pending/approved/rejected + runtime `pending_admin`)
- `created_by`/`reviewed_by`, `reviewed_at`, `rejection_reason`, `contract_snapshot` (json)

### `contract_audit_log` (`pbc_2975560841`) — журнал изменений (legacy, используется krugo-bot)

**Права:** list/view/create: `role != null`. update/delete: `null` (append-only).

**Поля:** `contract_id` (relation, required), `user_id` (relation), `action` (text, required), `old_value`/`new_value` (json), `description` (text), `created`.

### `finance_journal` (`pbc_1115285115`) — аудит финансов (автоматический)

**Права:** list/view: `auth.id != ''`. create/update/delete: `null` (только PB hooks пишут).

**Поля:** `contract_id`, `application_id`, `domain` (select application/contract/payment/operator/ledger), `event_type`, `status` (posted/pending/approved/rejected/void), `changes` (json), `old_amount`/`new_amount`, `old_currency`/`new_currency`, `reason`, `note`, `created_by`/`reviewed_by`, `reviewed_at`, `related_*_id`, `application_snapshot`/`contract_snapshot` (json).

### `deals` (`pbc_612317808`) — сделки из Bitrix24

**Права:** list/view: `role != null`. create/update/delete: `null` (только backend Hono mutates).

**Поля:** `title`, `stage` (text — 8 kanban + 8 closed), `bitrix_stage_id`, `bitrix_deal_id` (num), `bitrix_category_id` (maps to office), `bitrix_chat_id`, `manager` → users, `office` → offices, `contact_name/phone/email`, `contact` → contacts, `source` (whatsapp/instagram/facebook/other), `source_detail`, `amount`, `currency`, `closed`, `closed_at`, `lost_reason`, `bitrix_created/modified`, `last_activity`, `synced_at`.

### `manager_payrolls` (runtime, НЕ в pb_schema.json)

**Права:**
- list/view: `(active) && (manager_id=self OR accountant)`.
- create/update: `accountantOnly` (bookkeeper/admin).
- delete: `admin`.

**Поля:** `manager_id` (relation→users, required), `office_id`, `period` (YYYY-MM, required), `status` (draft/paid, required), `payout_kind` (salary/advance/advance_reversal), commission/earnings fields (usd/kgs), `rate`, `bonus_kgs`, `allowance_kgs`, `social_fund_kgs`, `gross_total_kgs`, `advance_deduction_kgs`, `total_kgs`, `payroll_policy_id`, `payroll_policy_effective_from`, `personal_rate_override`, `office_compensation_mode`, `social_fund_enabled`, `advance_allocations` (json), `operation_key` (idempotency), `reversal_of_id`, `contract_count`, `contract_lines`/`leadership_lines`/`bonus_lines` (json, immutable snapshot), `payout_note`, `payout_account_id`, `payout_account_name`, `payout_reference`, `calculated_at`, `paid_at`, `paid_by`.

**Indexes:** UNIQUE draft per (manager_id, period) WHERE status='draft'; UNIQUE operation_key WHERE != ''.

### `payroll_policies` (runtime)

**Поля:** `manager_id` (required), `effective_from` (YYYY-MM, required), `personal_rate` (0.01-1, required), `office_compensation_mode` (required), `fixed_allowance_kgs`, `note`, `created_by`.
**Права:** read: manager-self OR accountant; create/update/delete: admin.

### `providers` (`pbc_2249077391`), `offices` (`pbc_1680310448`), `payment_methods` (`pbc_568792081`), `accounts` (`pbc_2324088501`), `office_bookkeepers` (`pbc_1815310662`), `users` (`_pb_users_auth_`)

Справочные коллекции. `office_bookkeepers` — связка user↔office, даёт **глобальный** бухгалтерский доступ (не scoped по офису). `users` — role (admin/senior/manager/null), isBookkeeper (bool), status (active/fired), office.

---

## 3. Роли и доступ

### Роли

| Роль | Права |
|---|---|
| **admin** | Всё. Bypass всех scope-ограничений. |
| **senior** | Scoped по офису: видит/правит контракты своего офиса, подтверждает платежи своего офиса, не может approve cancellation_settlement (только accountant/admin). Один senior на офис (`senior_manager_guard`). |
| **manager** | Scoped по своим контрактам (`created_by`/`created_by_2=self`). Создаёт pending-запросы, не подтверждает ничего. |
| **null** (pending) | Может войти, но `staff` rules deny данные. |
| **isBookkeeper** (флаг, orthogonal) | Глобальный бухгалтерский доступ ко всем офисам. |
| **office_bookkeepers** (assignment) | То же что isBookkeeper — **глобальный** доступ, `office_id` это accounting dimension, не permission boundary. |
| **fired** (status) | Не может auth (`user_access.pb.js`), delete-rules обёрнуты в `active` guard. |

### Rule templates (финальные, `1784214000_global_accountant_access.js`)

```
active      = @request.auth.id != "" && @request.auth.status != "fired"
assignment  = @request.auth.office_bookkeepers_via_user_id.id ?!= ""
accountant  = (role="admin" || isBookkeeper=true || assignment)
staff       = (active) && (role in {admin,senior,manager} || isBookkeeper || assignment)
decision    = (active) && (role in {admin,senior} || isBookkeeper || assignment)
ownContract = (active) && (accountant || created_by=self || created_by_2=self || (role=senior && office?=self.office))
relatedContract = (active) && (accountant || contract_id.created_by=self || contract_id.created_by_2=self || (role=senior && contract_id.office?=self.office))
accountantOnly   = (active) && accountant
```

### Менеджер: CAN vs CANNOT

**CAN:**
- Создать/обновить (non-financial) свои контракты.
- Создать платежи (pending), возвраты (pending), корректировки (pending), фин. запросы (pending), заявки на оплату поставщику (pending).
- Обновить/soft-delete pending-платёж (своего контракта).
- Создать/обновить non-approved applications (своего контракта).
- Soft-cancel non-approved applications.
- Создать provider в справочнике.
- Забрать сделку (claim), двигать свою сделку по этапам.
- Удалить свой pending finance_change_request.

**CANNOT:**
- Подтвердить платёж (`POST /api/payments/confirm/:id` — manager 403).
- Отклонить платёж (`canReview` gate).
- Одобрить/отклонить возврат, корректировку, фин. запрос (`decision` gate).
- Записать оплату поставщику (direct PB write rejected; `/api/operator-payments/commit` — accountantOnly).
- Отменить договор напрямую (через cancellation_settlement, accountant/admin approve).
- Изменить brutto/netto/actual_netto на approved договоре (`financial_review_guard` strips).
- Удалить контракт, платёж, корректировку, возврат.
- Любые операции с зарплатой (`accountantOnly`).
- Менять `payment_status` (auto-derived, guard rejects).
- Менять `contracts_num` после создания (`contracts_guard`).
- Править чужие контракты.

---

## 4. Жизненный цикл договора

### Создание

```
useCreateContractMutation (src/api/index.ts):
  1. Генерирует contracts_num "№XXXXXXX"
  2. pb.contracts.create({
       ...values,
       finance_status: 'approved',        // авто-approved на create
       finance_submitted_by/at,
       finance_approved_by/at
     })
  3. Если price_split непустой → replaceApplicationsFromPriceSplit(contractId, priceSplit, tourOperator)
     - resolve provider_id по canonical name map
     - void ledger + soft-cancel apps не в desired set
     - upsert kept apps (status=active, finance_status=approved, re-post ledger)
     - create new apps (finance_status=approved + postApplicationExpectedLedger)
     - final idempotent ledger pass
```

### Редактирование (manager)

```
useUpdateContractMutation (src/api/index.ts):
  wasApproved = finance_status === 'approved' || !finance_status  // empty = approved (legacy)
  hasFinanceChanges = brutto/netto/actual_netto changed

  sanitizeContractUpdateValues:
    if wasApproved → strip brutto/netto/actual_netto (нельзя менять напрямую)

  if wasApproved && hasFinanceChanges:
    createFinanceChangeRequests (per-field pending)
    update contract with finance_status='pending' + finance_submitted_by/at
  elif !wasApproved && hasFinanceChanges:
    update contract with finance_status='pending' + submitted_by/at + clear approved_by/at
  else:
    direct update (non-financial fields)
```

### Finance approval (bookkeeper)

```
useApproveContractFinanceMutation (src/api/index.ts):
  1. recomputes netto from active apps via getApplicationsNetto (live rates)
  2. GUARDS:
     - brutto <= 0 → throw 'Брутто не может быть нулевым'
     - netto <= 0  → throw 'Нетто не может быть нулевым'
     - netto > brutto → throw 'Нетто не может превышать брутто'
  3. pb.contracts.update({
       brutto_price: approvedBrutto,
       netto_price: approvedNetto,
       finance_status: 'approved',
       finance_approved_by/at,
       manual_netto: false, override_netto: 0,
       clear finance_pending_*
     })
  4. mirror to actual_netto_history + contract_finance_changes

useRejectContractFinanceMutation:
  pb.contracts.update({finance_status: 'rejected', finance_rejection_reason: reason})
```

PB hook `financial_review_guard.handleContractUpdate`: non-reviewer НЕ может установить `finance_status` в 'approved'/'rejected', не может задать `finance_approved_by/at`, не может менять brutto/netto/actual_netto на approved контракте.

### Netto (source of truth)

- **Netto = сумма approved applications** (через `getApplicationsNetto` с конвертацией валют).
- `contracts.netto_price` — projection, synced by `recalcContractNetto` (PB hook на app CRUD).
- `recalcContractNetto` **пропускает mixed-currency** контракты — фронтенд пересчитывает.
- Legacy `price_split`, `manual_netto`, `override_netto` — archive-only, не drive accounting.

### Finance status states

| `finance_status` | Значение |
|---|---|
| `approved` (или empty = approved) | Договор подтверждён, brutto/netto зафиксированы |
| `pending` | Менеджер запросил изменение, ждёт бухгалтера |
| `rejected` | Бухгалтер отклонил запрос |

---

## 5. Клиентские платежи

### Создание платежа (manager)

```
usePaymentMutation (src/api/index.ts):
  pb.payments.create(buildPaymentCreatePayload({
    comment, amount, currency, contract_id, created_by,
    exchange_rate_kgs, payment_method_id?, office_id?,
    receiver__requisite?, payment_date,
    change_amount/change_currency?,    // сдача
    status: 'pending',
    is_confirmed: false
  }))
```

PB hook `financial_review_guard.handlePaymentCreate`: non-reviewer НЕ может создать платёж со status≠pending или is_confirmed=true.

### Подтверждение платежа (bookkeeper/senior)

**Только через Hono backend** `POST /api/payments/confirm/:paymentId`:

```
1. Auth: admin/isBookkeeper → allowed; office_bookkeepers → allowed (global); senior → office match; manager → 403
2. Load payment + contract via SUPERUSER pb
3. Block: deleted, cancelled contract, already-confirmed (idempotent if same-currency)
4. Only pending/blank status allowed
5. Capture ONE raw rate snapshot (from valuta)
6. acceptedRate = pay→contract currency
   changeRate = change_currency→contract currency (if change>0)
   acceptedConvertedAmount = amount×rate − changeAmount×changeRate  (must be >0)
7. Write via SUPERUSER:
     status='confirmed', is_confirmed=true, confirmed_by, confirmed_at,
     contract_currency, accepted_rate, accepted_converted_amount,
     accepted_rate_timestamp, accepted_rate_source='valuta', accepted_raw_rates
8. Frontend then calls postPaymentLedgerEntry(id) → ledger posted
```

**Что блокируется после confirm:**
- SQL trigger `immutable_payments` — amount/currency/contract_id/change_amount/contract_currency/accepted_* immutable.
- `currency_valuation_guard` — non-superuser не может менять valuation fields.
- `financial_review_guard` — confirmed платёж: amount/currency/contract_id/method/office/is_deleted locked.

### Отклонение платежа (bookkeeper/senior)

```
useRejectPayment:
  pb.payments.update({status:'rejected', is_confirmed:false, rejection_reason, confirmed_by, confirmed_at})
  → voidLedgerForSource('payment', id)
  → audit payment_rejected
```

### Bakai QR → pending payment (автоматически)

```
POST /webhook (backend/src/bakai-webhook.ts):
  1. Validate X-Bakai-Webhook-Ingress token
  2. Parse BakaiSuccessCallback: accountNo, currencyID=417 (KGS), amount>0, elqrID, comment, createdDate
  3. Match contract_qr by comment + status='active'
  4. Reject if contract is_cancelled
  5. Idempotency: payments.getFirstListItem(elqr_id=…) — 404 = proceed
  6. Receipt lock: webhook_logs.create({id: sha256(elqr_id)[:15]})
  7. Create payment: {contract_id, contract_qr_id, amount, currency:'KGS', status:'pending', qr_source:'qr', bakai_transaction_id, elqr_id, payment_date}
  → payment starts as PENDING, never confirmed by webhook
```

### Ledger posting (client-side, post-confirm)

```
postPaymentLedgerEntry(paymentId) (src/api/bookkeeping.ts):
  guards: status==='confirmed' && !is_deleted && within BOOKKEEPING_CUTOVER_DATE (2026-05-01)
  buildPaymentLedgerMovements(payment):
    - {sourceLine:'in', direction:'in', +amount}         // main
    - {sourceLine:'change', direction:'out', -changeAmount}  // if change_amount>0
  upsertLedgerEntry per movement (idempotent via (source_type, source_id, source_line))
  source_type='payment', source_id=payment.id
```

### PB hooks on payments

| Hook | Когда | Что делает |
|---|---|---|
| `currency_valuation_guard` | create/update/delete | Contract not settled; non-superuser не может confirm/write valuation; immutable once accepted_rate_timestamp set |
| `financial_review_guard` | create/update | Non-reviewer не может create confirmed, не может менять status/is_confirmed/confirmed_by/at; confirmed payment financial fields locked |
| `finance_journal` | afterUpdate (transition to confirmed) | `event_type='payment_posted'` audit row |
| `operator_payments.pb.js` | — | НЕ syncs от client payments (только operator payments) |

---

## 6. Оплаты поставщикам

### Заявка на оплату (manager → pending)

```
pb.operator_payment_requests.create({
  contract_id, application_id,
  request_type: 'full_remaining' | 'advance',
  is_prepayment: bool,
  requested_amount, status: 'pending',
  created_by, snapshot
})
```

PB hook `operator_payment_requests.pb.js.handleCreateRequest`: validates client 100% paid OR approved cancellation settlement OR explicit prepayment request, currency match.

### Выполнение оплаты (bookkeeper → atomic)

**Direct PB write REJECTED** (`operator_payments.pb.js.rejectDirectWriteRequest`). Только через custom route:

```
POST /api/operator-payments/commit (PB custom route, runInTransaction):
  Auth: canPay() = admin OR isBookkeeper OR office_bookkeepers assignment (GLOBAL)
  1. validatePaymentAccounting (recheck totals inside transaction — overpayment race protection)
  2. operatorPaymentEligibility: 100% client payment OR approved cancellation settlement OR prepayment request
  3. Idempotent on operation_id
  4. Create operator_payments record
  5. syncAfterSave:
     - recomputeApplication: application.payment_status = 'paid' (when effectivePaid >= expected)
     - recomputeRequestsForApplication: operator_payment_requests.status (pending/partially_paid/paid)
     - post ledger_entries (source_type='operator_payment')
  6. journal partial/paid events
```

### Void операторской оплаты

```
POST /api/operator-payments/{id}/void (PB custom route):
  voided=true, void_reason, voided_at, voided_by
  syncPaymentAfterVoid → recompute application.payment_status, recompute requests, void ledger
```

### Возврат от поставщика

```
POST /api/operator-refunds/commit (PB custom route, atomic):
  Same pattern: direct write rejected, idempotent on operation_id
  syncRefundAfterVoid → recompute, post ledger (source_type='operator_refund', direction='in')
```

### `application.payment_status` — DERIVED

`operator_payments_lib.syncAfterSave` recomputes `application.payment_status`:
- `paid` — когда effectivePaid >= expected
- `submitted` — когда есть partial payment
- empty — нет оплат

**Manager НЕ может установить `payment_status` вручную** — `financial_review_guard.handleApplicationUpdate` rejects any `payment_status` change ('рассчитывается автоматически').

---

## 7. Возвраты клиентам

### Создание (manager → pending)

```
pb.client_refunds.create({
  contract_id, application_id?, amount, currency,
  refund_date, reason (cancellation/overpayment/partial_refund/other),
  comment?, payment_method_id?, receipt?,
  status: 'pending', created_by
})
```

### Одобрение (bookkeeper/senior)

```
useApproveClientRefund({refundId, paymentMethodId?}):
  pb.client_refunds.update({
    status: 'approved',
    payment_method_id: paymentMethodId (если не был задан — Dialog for method selection),
    reviewed_by, reviewed_at
  })
  → postClientRefundLedgerEntry (source_type='client_refund', direction='out', signed_amount=-amount)
  → audit
```

PB hook `financial_review_guard.handleRefundUpdate`: non-reviewer не может менять status/reviewed_by/rejection_reason/payment_method_id.

PB hook `finance_journal.handleClientRefundApproved`: voids linked payment's ledger entries (voidPaymentLedger) + `event_type='payment_refunded'`.

PB hook `currency_valuation_guard`: contract not settled; non-superuser не может write valuation fields.

### Отклонение (bookkeeper/senior)

```
pb.client_refunds.update({status:'rejected', rejection_reason, reviewed_by, reviewed_at})
```

---

## 8. Корректировки заявок поставщиков

Approved applications **нельзя править напрямую** (`financial_review_guard.handleApplicationUpdate` blocks amount/currency/status/finance_status/is_deleted changes на approved apps). Менеджер создаёт correction → бухгалтер одобряет.

### Создание (manager → pending)

```
useCreateApplicationCorrection:
  pb.application_corrections.create({
    contract_id, application_id,
    type: 'correction' | 'cancellation',
    field: 'amount',
    old_amount, new_amount, old_currency, new_currency,
    status: 'pending', created_by, reason?
  })
  → upsert if existing pending (dedup)
```

### Типы

- **correction** — изменить amount/currency. После approve: `application.amount=new_amount, currency, status='active', finance_status='approved'`, ledger reposted.
- **cancellation** — отменить поставщика. После approve: `application.status='cancelled', finance_status='cancelled', is_deleted=true`, ledger voided. **amount НЕ = 0** (PB rejects 0 for required number).

### Одобрение (bookkeeper/senior)

```
useApproveApplicationCorrection:
  1. Проверить что это latest pending (reject if not)
  2. If cancellation:
       pb.applications.update({status:'cancelled', finance_status:'cancelled', is_deleted:true})
       voidLedgerForSource('application', appId)
  3. If correction:
       pb.applications.update({amount:new_amount, currency, status:'active', finance_status:'approved', is_deleted:false})
       postApplicationExpectedLedger(appId)
  4. pb.application_corrections.update({status:'approved', reviewed_by, reviewed_at})
  5. recalcContractNetto (auto by PB hook)
  → rollback on failure
```

PB hook `financial_review_guard.handleCorrectionUpdate`: non-reviewer не может менять status/reviewed_by/rejection_reason.

### Отклонение (bookkeeper/senior)

```
useRejectApplicationCorrection:
  pb.application_corrections.update({status:'rejected', rejection_reason, reviewed_by})
  if was new application → cancel it too
```

---

## 9. Финансовые запросы (brutto/netto)

Менеджер НЕ может напрямую изменить brutto/netto/actual_netto на **approved** договоре (`sanitizeContractUpdateValues` strips + `financial_review_guard` blocks). Менеджер создаёт finance_change_request → бухгалтер одобряет.

### Создание (manager → pending)

```
createFinanceChangeRequests (src/api/index.ts) — вызывается из useUpdateContractMutation:
  For each changed field (brutto_price, netto_price, actual_netto_price):
    pb.finance_change_requests.create({
      contract_id, field, old_value, new_value, currency, reason,
      status: 'pending', created_by
    })
  pb.contracts.update({finance_status:'pending', finance_submitted_by/at})
```

### Поля `field` enum

- `brutto_price` — изменение брутто
- `netto_price` — изменение нетто
- `actual_netto_price` — изменение фактического нетто
- `cancellation_settlement` — отмена договора (отдельный флоу, см. §10)

### Одобрение (bookkeeper/senior)

```
useApproveFinanceRequest(requestId):
  1. Validate latest, check stale
  2. pb.contracts.update({[field]: new_value})  // applies to contract
  3. pb.finance_change_requests.update({status:'approved', reviewed_by, reviewed_at})
  4. Auto-reject sibling pending on same contract+field
  5. Mirror to contract_finance_changes
```

PB hook `financial_review_guard.handleFinanceChangeUpdate`: non-reviewer не может менять status/reviewed_by/reviewed_at.

PB hook `cancellation_settlement_guard.handleFinanceRequestUpdate`: для `cancellation_settlement` — строже: accountant/admin only (NOT senior).

### Отклонение (bookkeeper/senior)

```
useRejectFinanceRequest({requestId, reason, correctedValue?}):
  pb.finance_change_requests.update({status:'rejected', rejection_reason})
  if correctedValue for brutto/netto → create+auto-approve corrected request
```

---

## 10. Отмена договора (cancellation settlement)

Менеджер **никогда** не отменяет договор напрямую. Создаёт `finance_change_requests` с `field='cancellation_settlement'` → бухгалтер/admin одобряет → atomic операция.

### Создание (manager → backend)

```
POST /api/cancellation-settlements/create (backend/src/cancellation-settlements.ts):
  Server-owned: reloads all financial sources via SUPERUSER pb,
  captures one exchange-rate generation,
  rebuilds immutable canonical snapshot,
  creates finance_change_requests (field='cancellation_settlement') with FormData
  (contractId, details JSON, consentFiles)
  → Browser never provides financial totals — only choices (supplier residuals, refund payout, transfers, evidence)
```

PB hook `cancellation_settlement_guard.handleFinanceRequestCreate`: cancellation_settlement требует superuser (backend) creation. Если creator = manager, must own contract AND every transfer destination contract.

### Одобрение (accountant/admin — НЕ senior)

```
POST /api/cancellation-settlements/approve (PB custom route, runInTransaction):
  Auth: canReview = admin OR isBookkeeper OR office_bookkeepers assignment (NOT senior)
  Idempotent on operation_key

  1. validateRequestEnvelope (only request_id, payment_method_id, operation_key allowed — no financial values from client)
  2. validateCancellationSnapshotStrict (recompute everything server-side: payments, refunds, transfers, applications, contract)
  3. applyApplications: each application → finalAmount or cancelled, upsert ledger (void on cancel)
  4. applyContract: is_cancelled=true, is_done, finance_status='approved', brutto/netto frozen
  5. ensureRefund: create/validate client_refunds (status='approved', cancellation_request_id, accepted_rate snapshot)
  6. ensureRefundLedger: post ledger_entries (source_type='client_refund', direction='out')
  7. ensureTransfers: create contract_credit_transfers (status='approved')
  8. ensureAudit: write bookkeeping_audit_log (cancellation_settlement_approved)
  → ONE transaction
```

PB hook `cancellation_settlement_guard.handleFinanceRequestUpdate`: sensitive fields immutable; status change requires canReview; approved+requires_admin_approval → isAdmin; pending_admin→other → isAdmin. Always rejects approve/pending_admin transitions via request hook ('use the protected server command' — atomic approve bypasses request hook).

**Важно:** `is_cancelled` (НЕ `is_deleted`). Deletion убрало бы retained commission из reports. Отменённый контракт остаётся в financial reporting/payroll с retained commission, но не может получать payments, new QR, Bakai webhook payments.

PB hook `cancellation_settlement_guard` (contracts after-update): revoke QR на cancelled contract.

---

## 11. Сделки (deals)

### Синхронизация

Bitrix24 deal pipeline mirrored in PocketBase. 24K deals, 5-min sync.

```
cron (backend/src/cron.ts): runSyncPull(6) every 5 min
  → syncOneDeal → upsert deal in PB
Webhook: POST /api/deals/sync-from-bitrix (shared secret)
```

### Этапы (8 kanban + 8 closed)

**Kanban:** unprocessed, awaiting_specialist, in_work, tour_selection, office_visit, contract_payment, client_not_responding, manager_not_responding.

**Closed:** won, lost, lost_competitor, spam, not_our_business, archive, trip_postponed, wants_hot_deals.

### Биржа (unclaimed marketplace)

`mode=free`: unassigned deals in unprocessed/awaiting_specialist (`manager=''`).
`mode=my`: own deals.
`all=true` (seniors/admins): all assigned deals.

### Мутации (через Hono backend, НЕ PB directly)

PB rules: create/update/delete = `null` — только backend Hono mutates.

```
PATCH /api/deals/:id (backend/src/deals.ts):
  body: {stage?, manager?, notes?}
  Auth: owner OR senior OR unassigned
  → updates PB + syncs Bitrix (crm.deal.update STAGE_ID/ASSIGNED_BY_ID)
```

- **Claim** (забрать): `PATCH /api/deals/:id {manager: self}`.
- **Смена этапа**: `PATCH /api/deals/:id {stage: 'tour_selection'}`.
- Manager видит только свои deals; senior — свой офис; admin — все.
- Manager НЕ может: удалить deal, создать deal, переназначить другому менеджеру.

### Чаты

- **WhatsApp** (WhatCRM API, 2 instances: Krugosvet line 7 + Betravel line 13): `GET /api/chat/phone-map`, `GET /:phone/messages`, `POST /:phone/send`.
- **Bitrix Open Lines** (IG/TG/WA): `GET /api/chat/bitrix/:dealId/sessions`, `/:dealId/messages`, `POST /:dealId/send`.
- **Whispers** (SQLite, internal notes): `GET/POST/DELETE /api/deals/:id/whispers` (optional Bitrix imbot sync).

---

## 12. Зарплата (payroll)

### Eligibility

Контракт входит в payroll менеджера X за период P когда ALL:
1. `contract.created_by === X || created_by_2 === X`
2. `!is_deleted && finance_status ∈ {approved, paid} && brutto_price > 0`
3. `deriveFullPaymentState.isFullyPaid === true` AND `fullPaymentAt` starts with period P

**Latest-date-confirmed-payments-reach-100% rule:** sums confirmed payments + approved refunds в per-day delta map, converting via `resolvePaymentValuation` (immutable `accepted_converted_amount`). Walks days chronologically, tracking `balance >= brutto_price`. Refund drops below 100% → `fullPaymentAt` resets; later top-up → new date. Returns **latest date where 100% reached and retained**.

### Commission formula

```
saleMonthCommissionUsd (fixed by sale month = printed_at || created, sliced YYYY-MM):
  < $1000  → 20%
  ≥ $1000  → 30%
  > $3000  → 30% + 10,000 KGS bonus (once per sale period)

personalRateOverride (from payroll_policy) overrides tier rate.

Second manager (created_by_2): commission / 2 (50/50 split).

Social-fund deduction: if enabled && earningsKgs+bonusKgs+allowanceKgs > 6000 → minus 6,000 KGS (once per manager/month).

Senior office leadership: senior additionally earns on ALL office contracts.
  Rate: 10% if office plan met, else 3% (fixed by sale month).
  officeCompensationMode ∈ {none, fixed_allowance} disables; fixed_allowance pays fixed_allowance_kgs.
```

### Operations (bookkeeper/admin only — `accountantOnly`)

```
POST /api/payroll/salary/commit (PB custom route, runInTransaction):
  1. payrollAuthorizeOffice (admin/isBookkeeper/office_bookkeepers, not fired)
  2. requireExactDraftMarker (expected_updated/calculated_at match, else 409)
  3. payrollCanonicalManager (role manager/senior, not fired, has office)
  4. payrollCanonicalAccount (KGS, active, non-virtual, office match)
  5. validatePayrollSnapshot (recompute all totals, match stored)
  6. rejectDuplicateContracts (contract/leadership/bonus/social-fund/allowance double-payment)
  7. Set status='paid', paid_at, paid_by
  8. upsertPayrollLedger (source_type='payroll', direction='out', currency='KGS')
  9. writePayrollAudit (bookkeeping_audit_log)
  Idempotent on operation_key

POST /api/payroll/advances/commit — create paid advance record
POST /api/payroll/advances/{id}/reverse — reverse outstanding advance
```

**Direct REST writes of `status='paid'` REJECTED** (`rejectDirectPaidPayroll`). Paid records cannot be edited/deleted (`handleModelUpdate/Delete` throw).

`payroll_ledger_guard`: blocks direct REST mutation of `ledger_entries` with `source_type='payroll'` (superuser bypass OK for reconciliation).

---

## 13. Ledger и аудит

### Ledger (`ledger_entries`)

Двойная запись. Idempotency key: `(source_type, source_id, source_line)`.

| Source | source_type | direction | Когда posted |
|---|---|---|---|
| Client payment | `payment` | `in` (+amount) | post-confirm |
| Client payment change | `payment` | `out` (-changeAmount) | post-confirm (if change>0) |
| Application (expected) | `application` | `out` (-amount) | app CRUD |
| Operator payment | `operator_payment` | `out` | commit |
| Operator refund | `operator_refund` | `in` | commit |
| Client refund | `client_refund` | `out` (-amount) | approve |
| Payroll | `payroll` | `out` (salary/advance) / `in` (reversal) | commit |

`voidLedgerForSource(sourceType, sourceId)` — sets all posted entries to void. Used on reject, soft-delete, cancel.

### Аудит (3 коллекции)

| Коллекция | Кто пишет | Назначение |
|---|---|---|
| `finance_journal` | PB hooks (auto) | Append-only аудит: application CRUD, corrections, finance approvals, payment confirmed, refund approved, finance requests |
| `contract_audit_log` | Frontend `logAuditEntry` + krugo-bot skill | Журнал изменений полей договора (legacy, используется ботом) |
| `bookkeeping_audit_log` | PB hooks (atomic commands) | Аудит бухгалтерских операций: payroll paid, cancellation approved, operator payment committed |

**`finance_journal` ≠ `ledger_entries`**: finance_journal — audit trail (events); ledger_entries — double-entry register (movements with void capability).

---

## 14. Инварианты и gotchas

### Инварианты

1. **Netto = approved applications sum** (с конвертацией валют). `contracts.netto_price` — projection.
2. **`applications` count == posted `ledger_entries` where source_type='application'**.
3. **Idempotency**: payments unique on `elqr_id`; ledger unique on `(source_type, source_id, source_line)`; webhook receipt lock on `webhook_logs.id = sha256(elqr_id)[:15]`; payroll unique on `operation_key`.
4. **Approved apps immutable to managers**: amount/currency/status/finance_status/is_deleted changes blocked.
5. **`payment_status` auto-derived** from operator_payments — manual set rejected.
6. **Empty `finance_status` = approved** (legacy compat in `getApplicationsNetto` and `deriveStates`).
7. **`contracts_num` immutable** post-creation.
8. **Paid payroll records immutable** — cannot edit/delete, only via atomic commands.
8. **One senior per office** (`senior_manager_guard`).
9. **Fired users cannot auth** (`user_access.pb.js`), delete-rules wrapped in `active` guard.
10. **Accountants are global** — `office_bookkeepers` assignment grants company-wide access.
11. **Cancellation uses `is_cancelled` (NOT `is_deleted`)** — deletion drops retained commission from reports.
12. **Zero brutto/netto blocked on approval** — `useApproveContractFinanceMutation` throws.
13. **Netto > brutto blocked** — negative commission = data error.

### Gotchas

- **PB `0` is "blank"** — required number fields reject 0 via REST. Cancellation sets status/finance_status, NOT amount=0.
- **`applications.type` required** even though `tour_operator` deprecated — always set `supplier` for new apps.
- **Mixed-currency contracts**: `recalcContractNetto` skips — frontend recompute via `getApplicationsNetto` with live rates.
- **`tour_operator` decoupled from applications** (2026-05-25 refactor) — text label on contract, not a cost entry.
- **Bookkeeping cutover 2026-05-01** — pre-cutover payments are archive, ledger not posted.
- **`VITE_BACKEND_URL` is build-time** — baked into JS bundle, set on Railway env.
- **`contract_qr` rejects `sort` by `created`/`updated`** — use `-expires_at` instead.
- **PB `ledger_entries` sort by `-entry_date`** — `-created` caused 400.
- **`railway up` deploys to linked service** — always `railway link -s <service>` before deploy.
- **RUB is not supported in bookkeeping** — don't add RUB accounts without updating all currency unions.
- **Netto drifts from applications** — `recalcContractNetto` auto-updates on app CRUD, but mixed-currency recomputed by frontend only.
- **Backend routers need `Hono<Env>`** — missing generic → TS compilation fails → Railway deploys silently broken.
- **ReplaceApplicationsFromPriceSplit → canonical write** — don't edit `contracts.price_split` directly; it's archive.
- **Zero-amount price_split → no apps** — users fill netto_price but leave price_split at 0 → no applications → netto drift.
- **PocketBase `0` is "blank"** — required number fields reject 0.
- **`contract_audit_log` not created on contract create** — only on operations reject/uncancel/reassign (no direct hook on contracts create).
- **`pb_schema.json` understates real rules** — runtime fields added by `schema_bootstrap_lib.js` (`is_cancelled`, `payment_status`, `operator_submission_status`, `manager_payrolls`, `payroll_policies`). Migrations supersede base rules. Always cross-reference migrations.
- **Local→Railway edge flap**: requests to `*.up.railway.app` intermittently fail with HTTP 000. Workaround: `curl --resolve <host>:443:<ip>` (PB IP `69.46.46.63`).
