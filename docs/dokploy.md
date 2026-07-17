# Деплой на Dokploy

## Сервисы (независимые)

| Сервис | Профиль | Статус |
|---|---|---|
| `krugo-bot` | default | Готов |
| `hermes` | `hermes` | Требует ручной настройки gateway |

Интеграция между ними не реализована.

## krugo-bot (default)

```bash
docker compose up krugo-bot -d
```

Переменные: `TELEGRAM_BOT_TOKEN`, `OPENAI_API_KEY`, `BOT_ENV=production`.

Volume: `bot-data` → `/app/data` (SQLite).

## Hermes Agent (`--profile hermes`)

```bash
docker compose --profile hermes up hermes -d
```

После запуска — **в отдельном терминале** настроить Telegram-шлюз. Токен вводится в wizard, не через env:

```bash
docker compose exec hermes hermes gateway setup
```

Gateway запускается как s6-сервис внутри контейнера (persistent). Требует **отдельный Telegram-токен**, не тот же что у krugo-bot.

Volume: `hermes-data` → `/opt/data`.

## Деплой в Dokploy

1. Projects → New → GitHub-репо
2. Environment → задать переменные
3. Deploy
