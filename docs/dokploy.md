# Деплой на Dokploy

## Сервисы

| Сервис | Статус |
|---|---|
| `krugo-bot` | Готов |
| `hermes` | Требует `gateway setup` |

Интеграция между ними не реализована. Разные Telegram-токены — конфликта нет.

## krugo-bot

Переменные: `TELEGRAM_BOT_TOKEN`, `OPENAI_API_KEY`, `BOT_ENV=production`.

Volume: `bot-data` → `/app/data` (SQLite).

## Hermes Agent

**Первый запуск** — настроить Telegram-шлюз (токен вводится в wizard):

```bash
docker compose run --rm hermes gateway setup
```

**Рабочий запуск** — `docker compose up -d` (оба сервиса).

**Безопасность:** задать `TELEGRAM_ALLOWED_USERS` (Telegram ID через запятую).

Требует **отдельный Telegram-токен**, не тот же что у krugo-bot.

## Переменные окружения (Dokploy → Environment)

| Переменная | Назначение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Токен krugo-bot |
| `OPENAI_API_KEY` | Ключ DeepSeek |
| `TELEGRAM_ALLOWED_USERS` | Кому разрешён доступ к Hermes |

## Деплой

1. Dokploy → Projects → New → GitHub-репо
2. Environment → задать переменные
3. Deploy
4. После деплоя: `docker compose run --rm hermes gateway setup`
