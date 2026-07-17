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
| `TELEGRAM_ALLOWED_USERS` | Доступ к Hermes |

## Порядок

1. Dokploy → Projects → New → GitHub-репо
2. Environment → задать переменные
3. Deploy
4. `docker compose run --rm hermes setup`
5. `docker compose run --rm hermes gateway setup`
