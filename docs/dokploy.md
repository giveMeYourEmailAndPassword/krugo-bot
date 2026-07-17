# Деплой на Dokploy

## 1. krugo-bot (Telegram-бот)

Dokploy сам подхватит `docker-compose.yml` из репозитория.

### Переменные окружения (в Dokploy → проект → Environment):

| Переменная | Значение |
|---|---|
| `TELEGRAM_BOT_TOKEN` | токен от @BotFather |
| `OPENAI_API_KEY` | ключ DeepSeek API |
| `OPENAI_BASE_URL` | `https://api.deepseek.com` |
| `AI_MODEL` | `deepseek-v4-flash` |
| `BOT_ENV` | `production` |
| `DATABASE_PATH` | `/app/data/hermes.db` (авто) |

### Volume

Volume `bot-data` примонтирован к `/app/data` — SQLite не теряется при рестарте.

## 2. Hermes Agent

Когда Hermes Agent будет готов — раскомментировать секцию `hermes-agent` в `docker-compose.yml`. Потребуется:

- Docker-образ Hermes Agent
- `HERMES_API_KEY` — ключ доступа к агенту
- Доступ к Docker-сокету (если агент запускает задачи в контейнерах)

Бот нужно будет обновить: добавить HTTP-клиент для вызова Hermes Agent API (создание run, отслеживание статуса, получение результата).

## Порядок деплоя

1. Зайти в Dokploy → Projects → New Project
2. Выбрать GitHub-репозиторий `giveMeYourEmailAndPassword/krugo-bot`
3. Dokploy найдёт `docker-compose.yml` и развернёт `krugo-bot`
4. Задать переменные окружения
5. После деплоя проверить логи: `docker compose logs -f krugo-bot`
