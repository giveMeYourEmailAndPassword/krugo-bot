# Деплой на Dokploy

## Сервисы

| Сервис | Статус |
|---|---|
| `krugo-bot` | Готов |
| `hermes` | Требует setup |

Разные Telegram-токены — конфликта нет.

## krugo-bot

Переменные: `TELEGRAM_BOT_TOKEN`, `OPENAI_API_KEY`, `BOT_ENV=production`.

Volume: `bot-data` → `/app/data` (SQLite).

## Hermes Agent

**Первый запуск** — настроить provider и Telegram (токен вводится в wizard):

```bash
docker compose run --rm hermes setup
docker compose run --rm hermes gateway setup
```

**Рабочий запуск:**

```bash
docker compose up -d
```

**Безопасность:** задать `TELEGRAM_ALLOWED_USERS` (Telegram ID через запятую).

Требует **отдельный Telegram-токен**, не тот же что у krugo-bot.

## Переменные (Dokploy → Environment)

| Переменная | Назначение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Токен krugo-bot |
| `OPENAI_API_KEY` | Ключ DeepSeek |
| `TELEGRAM_ALLOWED_USERS` | Telegram ID через запятую (доступ) |
| `HERMES_BRIDGE_KEY` | Bearer auth для hermes-proxy |
| `PB_URL` | PocketBase URL (striking-wisdom-production.up.railway.app) |
| `PB_USER` | PB superuser email |
| `PB_PASS` | PB superuser пароль |
| `BACKEND_URL` | contracts-backend (Hono) — **НЕ baza.krugo.tours** (там SPA). Рабочий: `https://contracts-backend-production-f1a9.up.railway.app` |

⚠️ `BACKEND_URL` должен указывать на Hono backend, не на фронтенд. `/rates` на фронтенде вернёт HTML, и USD/EUR платежи упадут на JSON decode.

## Порядок

1. Dokploy → Projects → New → GitHub-репо
2. Environment → задать переменные
3. Deploy
4. `docker compose run --rm hermes setup`
5. `docker compose run --rm hermes gateway setup`
