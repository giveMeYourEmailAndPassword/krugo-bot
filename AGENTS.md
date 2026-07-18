# AGENTS.md — Krugo Bot

## Проект

Krugo Bot — Telegram-бот на Go + Hermes Agent для автоматической обработки заявок менеджеров Кругосвет. Бот принимает шаблонные заявки в группе, передаёт в Hermes Agent, который выполняет изменения в PocketBase (договоры, поставщики, заявки). Работает на VPS 147.45.158.152 через Dokploy.

## Стек

- Go 1.26, `gopkg.in/telebot.v3`, SQLite (WAL)
- DeepSeek `deepseek-v4-flash` (быстрая модель)
- `nousresearch/hermes-agent:latest` (s6-overlay, Telegram gateway, hermes-proxy)
- PocketBase: `https://striking-wisdom-production.up.railway.app` (договоры, поставщики, заявки)
- Docker Compose → Dokploy

## Архитектура

```
Telegram → krugo-bot (Go, host net) → hermes-proxy (HTTP, host net) → Hermes Agent → PocketBase API
             ↕ SQLite                       ↕ volume hermes-data         ↕ DeepSeek v4-flash
        TELEGRAM_ALLOWED_USERS           HERMES_BRIDGE_KEY            DEEPSEEK_API_KEY
```

## Ключевые файлы

| Файл | Назначение |
|---|---|
| `cmd/bot/main.go` | Основной бот: allowlist, Hermes bridge |
| `cmd/hermes-proxy/main.go` | HTTP-прокси к Hermes (auth, mutex, 15m timeout) |
| `internal/rules/detector.go` | Детектор заявок: ≥2 из 15 маркеров |
| `internal/hermes/bridge.go` | BridgeClient: POST в proxy, возврат сырого текста |
| `internal/telegram/handlers.go` | handleText, handleCallback, /status, allowlist |
| `hermes/skills/contracts/SKILL.md` | Hermes skill v2.1: providers → applications → contracts |
| `docker-compose.bot.yml` | Bot Compose (host net, TELEGRAM_ALLOWED_USERS required) |
| `docker-compose.hermes.yml` | Hermes + proxy, общий volume, ro skill mount |

## Деплой

Dokploy → Projects: `krugosvet-helper` (bot) + `krugosvet-hermes` (agent).
Обязательные env: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USERS`, `OPENAI_API_KEY`, `HERMES_BRIDGE_KEY`, `PB_URL`, `PB_USER`, `PB_PASS`.

## Статус (18.07.2026)

| Компонент | Статус |
|---|---|
| krugo-bot: детектор, allowlist, hermes bridge | Done |
| hermes-proxy: auth, mutex, timeout | Done |
| Hermes Agent: DeepSeek v4-flash, gateway | Done |
| contracts skill v2.1: providers → applications → contracts | Done |
| PocketBase: полный цикл PATCH application + contract | Done |
| Деплой Dokploy (оба сервиса) | Done |
| Аудит изменений (bot backend) | Next |

## Следующий этап — Аудит изменений

**Бэкенд (krugo-bot), не Hermes:**

- После каждого успешного PATCH записывать в SQLite: request ID, contract ID, application ID, old/new JSON, Telegram user, timestamp
- Коллекция `contract_audit_log` в PocketBase — опционально (серверные хуки не срабатывают при API PATCH)
- Команда `/history HERMES-XXXX` — показать все изменения по заявке
- Формат: `[дата] @user: договор X, поле Y: значение А → значение Б`

## Для AI-агентов

При масштабных изменениях (новые пакеты, смена архитектуры, зависимости) — обнови этот файл.
