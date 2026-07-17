# AGENTS.md — Krugo Bot

## Проект

Krugo Bot — Telegram-бот на Go для автоматической обработки клиентских заявок с ИИ-анализатором Hermes. Бот читает шаблонные заявки в рабочей группе, классифицирует через OpenAI-совместимое API, публикует статус и даёт менеджерам inline-кнопки для смены статуса.

## Стек

- **Язык:** Go 1.26
- **Telegram:** `gopkg.in/telebot.v3`
- **БД:** SQLite (`mattn/go-sqlite3`, WAL mode)
- **AI:** OpenAI-совместимое API (модель по умолчанию `gpt-4o-mini`)
- **Деплой:** Docker → Railway

## Структура

```
cmd/
  bot/main.go        — полный бот (БД + Hermes + Telegram)
  testbot/main.go    — минимальный бот для проверки Telegram-соединения
internal/
  config/config.go   — загрузка env-переменных
  logger/logger.go   — slog: text в dev, JSON в production
  storage/
    storage.go       — интерфейс Store
    sqlite.go        — SQLite-реализация + авто-миграция
  tasks/request.go   — модель Request, константы статусов
  rules/detector.go  — LooksLikeRequest (≥2 ключевых слов → заявка)
  hermes/
    analyzer.go      — HTTP-клиент к AI API
    prompt.go        — системный промпт
    schema.go        — AnalysisResponse
  telegram/
    handlers.go      — обработка сообщений и callback-запросов
    keyboards.go     — inline-клавиатуры
migrations/          — SQL-миграции (для справки; авто-миграция в коде)
docs/
  run.md             — инструкция по запуску
```

## Статусы заявок

```
received → in_progress → needs_clarification / ready_for_review / ready_for_dev → assigned → done / rejected
```

## Команды

```bash
# Локальный запуск полного бота
set -a; source .env; set +a
go run ./cmd/bot

# Тестовый бот (только Telegram)
go run ./cmd/testbot

# Сборка
go build ./...

# Линтер
go vet ./...

# Docker
docker build -t krugo-bot .
```

## Ключевые решения

- **Long polling**, не вебхуки — проще для MVP, не нужен публичный URL
- **SQLite, не PostgreSQL** — ноль конфигурации, одна реплика, Railway volume для персистентности
- **Async AI-анализ** — бот сначала отвечает «принял», затем в горутине вызывает AI
- **HTML-экранирование** — все поля из пользовательского ввода/AI экранируются перед отправкой в Telegram (ParseMode HTML)

## Railway

- Volume `/app/data` **обязателен** — без него SQLite теряется при рестарте
- `railway volume add -m /app/data`
- Переменные: `TELEGRAM_BOT_TOKEN`, `OPENAI_API_KEY`, `BOT_ENV=production`
