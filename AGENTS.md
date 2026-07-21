# AGENTS.md — Krugo Bot

## Проект

Krugo Bot — Telegram-бот на Go для автоматической обработки заявок менеджеров Кругосвет. Принимает шаблонные заявки в группе, выполняет изменения в PocketBase (договоры, поставщики, платежи, возвраты, корректировки, финансы). Работает на VPS 147.45.158.152 через Dokploy.

Два пути выполнения:
1. **Детерминированный typed-command путь** (Stage 2) — финансовые операции (платежи, возвраты, оплаты поставщику, корректировки, финансы) парсятся Go-кодом и выполняются через `commands.Executor` напрямую в PB. Создаёт pending-запросы, которые бухгалтер подтверждает в вебе.
2. **Hermes путь** (legacy) — свободное редактирование поставщиков через Hermes Agent (DeepSeek). Только для НЕ-approved заявок.

## Стек

- Go 1.26, `gopkg.in/telebot.v3`, SQLite (WAL)
- DeepSeek `deepseek-v4-flash` (быстрая модель, Hermes Agent)
- `nousresearch/hermes-agent:latest` (s6-overlay, Telegram gateway, hermes-proxy)
- PocketBase: `https://striking-wisdom-production.up.railway.app` (договоры, поставщики, заявки, платежи)
- Hono backend: `https://contracts-backend-production-f1a9.up.railway.app` (public `/rates` для курсов валют)
- Docker Compose → Dokploy

## Архитектура

```
Telegram → krugo-bot (Go, host net)
             ├─ typed path: commands.Parse → commands.Executor → PocketBase (create-pending)
             │                ↕ /rates (Hono, public, для exchange_rate_kgs)
             │                ↕ SQLite (dedup + request record + /status)
             │
             └─ legacy path: hermes-proxy (HTTP) → Hermes Agent → PocketBase (supplier edits only)
                  ↕ SQLite                       ↕ volume hermes-data         ↕ DeepSeek v4-flash
             TELEGRAM_ALLOWED_USERS           HERMES_BRIDGE_KEY            DEEPSEEK_API_KEY
```

## Ключевые файлы

| Файл | Назначение |
|---|---|
| `cmd/bot/main.go` | Основной бот: config, pb.Client, executor, allowlist |
| `cmd/hermes-proxy/main.go` | HTTP-прокси к Hermes (auth, mutex, 15m timeout) |
| `internal/commands/types.go` | Типизированные Action + Command + Validate |
| `internal/commands/parse.go` | Детерминированный парсер шаблонов → Command |
| `internal/commands/execute.go` | Executor: валидация инвариантов + PB create-pending |
| `internal/commands/errors.go` | Типизированные ошибки |
| `internal/pb/client.go` | PB REST клиент (superuser, CRUD, lookup, thread-safe auth) |
| `internal/rules/detector.go` | Детектор заявок: ≥2 из 36 маркеров |
| `internal/hermes/bridge.go` | BridgeClient: POST в proxy, возврат сырого текста |
| `internal/telegram/handlers.go` | handleText (routing typed/legacy), executeCommand, /status, /history |
| `internal/telegram/keyboards.go` | Inline-кнопки + 7 шаблонов заявок |
| `internal/telegram/validate.go` | Валидация шаблона (заполненность + contract link) |
| `hermes/skills/contracts/SKILL.md` | Hermes skill v4.0.0: supplier edits, finance/DELETE forbidden on approved |
| `internal/config/config.go` | Config: TG, PB, BACKEND_URL, allowed users |
| `internal/storage/sqlite.go` | SQLite: requests (dedup по chat_id+message_id) |
| `docker-compose.bot.yml` | Bot Compose (host net, PB_URL/BACKEND_URL required) |
| `docker-compose.hermes.yml` | Hermes + proxy, общий volume, ro skill mount |

## Обязательные env

| Переменная | Назначение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Токен krugo-bot |
| `OPENAI_API_KEY` | Ключ DeepSeek (Hermes Agent) |
| `TELEGRAM_ALLOWED_USERS` | Telegram ID через запятую (доступ) |
| `HERMES_BRIDGE_KEY` | Bearer auth для hermes-proxy |
| `PB_URL` | PocketBase URL |
| `PB_USER` | PB superuser email |
| `PB_PASS` | PB superuser пароль |
| `BACKEND_URL` | Hono backend (НЕ baza.krugo.tours — там SPA). `https://contracts-backend-production-f1a9.up.railway.app` |

## Поддерживаемые операции (MVP Stage 2)

### Typed path (детерминированный, create-pending)

| Операция | Action | PB collection | Бухгалтер подтверждает |
|---|---|---|---|
| Клиентский платёж | `create_payment` | `payments` (status=pending) | Да (capture rate snapshot) |
| Возврат клиенту | `create_client_refund` | `client_refunds` (status=pending) | Да |
| Оплата поставщику | `create_operator_request` | `operator_payment_requests` (status=pending) | Да (atomic commit) |
| Корректировка поставщика | `create_app_correction` | `application_corrections` (status=pending) | Да |
| Отмена поставщика | `cancel_application` | `application_corrections` (type=cancellation, status=pending) | Да |
| Изменение финансов | `create_finance_change` | `finance_change_requests` (status=pending) | Да |

### Hermes path (legacy, supplier edits only)

| Операция | Action | Условие |
|---|---|---|
| Изменить поставщика | `change_contract` | Только НЕ-approved applications |
| Добавить поставщика | `change_contract` | Новая заявка (safe) |

### Недоступно через бот

| Операция | Причина |
|---|---|
| Отмена договора | Требует Hono `/api/cancellation-settlements/create` (FormData + consent files) |
| Подтверждение платежа | `POST /api/payments/confirm/:id` (только бухгалтер в вебе) |
| Одобрение возвратов/корректировок/фин-запросов | PB updateRule `decision` (только бухгалтер) |
| Смешанные заявки (поставщики + финансы) | Ambiguous — отправьте отдельными сообщениями |

## MVP-ограничения

- **Ownership**: глобальный Telegram allowlist, без per-user PB identity (Stage 1 — deferred). Любой allowlisted юзер может target любой договор. `created_by` relation НЕ заполняется — author tag пишется в `comment`/`reason`.
- **Superuser bypass**: executor авторизуется как PB superuser → collection rules не применяются, part of request hooks имеют `isSuperuserRequest()` bypass. Executor проверяет все инварианты сам в Go: contract active, application active+approved, stale-change detection, currency, duplicate-pending.
- **Только create-pending**: бот никогда не подтверждает/одобряет. Бухгалтер работает в `baza.krugo.tours/bookkeeping`.

## Статус (21.07.2026)

| Компонент | Статус |
|---|---|
| krugo-bot: детектор, allowlist, hermes bridge | Done |
| hermes-proxy: auth, mutex, timeout | Done |
| Hermes Agent: DeepSeek v4-flash, gateway | Done |
| contracts skill v4.0.0: supplier edits, finance/DELETE forbidden | Done |
| Typed command layer: parse + executor + pb client | Done |
| MVP operations: payment, refund, operator, correction, cancel, finance | Done |
| Dedup + /status для обоих путей | Done |
| Hermes skill guard: approved-app refuse, finance PATCH forbid | Done |
| Тесты: parse, pb client (race), executor (race) | Done |
| Деплой Dokploy (оба сервиса) | Done |
| Per-user PB identity (Stage 1) | Deferred |
| Бухгалтерские команды (confirm/approve в боте) | Future |
| Сделки (deals) в боте | Future |
| Read-only команды (/payments, /finance, /pending) | Future |

## Для AI-агентов

При масштабных изменениях (новые пакеты, смена архитектуры, зависимости) — обнови этот файл.
