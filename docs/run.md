# Запуск бота

## Переменные окружения

Создать `.env` в корне проекта:

```env
TELEGRAM_BOT_TOKEN=токен_от_BotFather
OPENAI_API_KEY=ключ_OpenAI
DATABASE_PATH=./data/hermes.db
BOT_ENV=development
```

`.env` в `.gitignore` — в git не попадёт.

## Тестовый бот (только Telegram, без БД и Hermes)

```bash
cd /Users/amantur/Documents/work/krugo-bot
set -a; source .env; set +a

# Собрать
go build -o /tmp/krugo-testbot ./cmd/testbot

# Запустить с логами в файл
/tmp/krugo-testbot 2>&1 | tee krugo-bot.log
```

Проверить:
- `/start` → ответит приветствием
- любое сообщение → `Сообщение получено`
- `Ctrl+C` → остановка

## Полный бот (с БД и Hermes AI)

```bash
cd /Users/amantur/Documents/work/krugo-bot
set -a; source .env; set +a
go run ./cmd/bot
```

Требует:
- `OPENAI_API_KEY` — иначе анализ заявок не сработает
- `data/` — создаётся автоматически, там SQLite (путь из `DATABASE_PATH`)

Логи: структурированный `slog` (text в dev, JSON в production).

## Пересборка тестового бота после изменений

```bash
cd /Users/amantur/Documents/work/krugo-bot
go build -o /tmp/krugo-testbot ./cmd/testbot
```
