# Деплой на Dokploy

## Сервисы (независимые)

| Сервис | Профиль | Статус |
|---|---|---|
| `krugo-bot` | default | Готов — Go-бот + DeepSeek, детектор заявок |
| `hermes` | `hermes` | Готов — требует ручной настройки gateway |

Интеграция между ними ещё не реализована.

## krugo-bot (default)

```bash
docker compose up krugo-bot -d
```

Переменные:
- `TELEGRAM_BOT_TOKEN` — токен бота
- `OPENAI_API_KEY` — ключ DeepSeek
- `BOT_ENV=production`

Volume: `bot-data` → `/app/data` (SQLite)

## Hermes Agent (`--profile hermes`)

```bash
docker compose --profile hermes up hermes -d
```

После первого запуска настроить gateway:

```bash
docker compose exec hermes hermes gateway setup
docker compose exec hermes hermes gateway start
```

Требует **отдельный Telegram-токен** (`HERMES_TELEGRAM_TOKEN`), не тот же что у krugo-bot.

Volume: `hermes-data` → `/opt/data`

## Деплой в Dokploy

1. Projects → New → GitHub-репо
2. Environment → задать переменные
3. Deploy
