# Krugo Bot — Telegram Hermes AI ассистент

Telegram-бот с ИИ-анализатором Hermes для автоматической обработки клиентских заявок в рабочих группах.

## План разработки

### Этап 1 — Фундамент (готово)
- [x] Go-модуль и структура проекта
- [x] Конфигурация из переменных окружения
- [x] SQLite-хранилище с авто-миграцией
- [x] Структурированное логирование (slog)

### Этап 2 — Ядро (готово)
- [x] Локальный детектор заявок по ключевым словам
- [x] Модель заявки + статусный lifecycle
- [x] Hermes AI analyzer (OpenAI-compatible API, промпт, schema)

### Этап 3 — Telegram-бот (готово)
- [x] Интеграция `telebot.v3`: приём сообщений, callback-запросы
- [x] Обработка: детекция → ack → async AI-анализ → публикация статуса
- [x] Inline-кнопки: «Передать в dev», «Нужно уточнение», «Назначить», «Закрыть»

### Этап 4 — Интеграции (в плане)
- [ ] Интеграция с Jira / Linear / Notion / внутренней системой задач
- [ ] Назначение ответственного через кнопку «Назначить»
- [ ] История изменений статусов

### Этап 5 — Production (в плане)
- [ ] Миграция с SQLite на PostgreSQL
- [ ] Метрики и healthcheck-эндпоинт
- [ ] Админ-панель (веб)

## Быстрый старт (локально)

```bash
cp .env.example .env
# отредактировать .env: TELEGRAM_BOT_TOKEN, OPENAI_API_KEY
set -a && source .env && set +a
go run ./cmd/bot
```

### Переменные окружения

| Переменная | Обязательна | По умолчанию | Описание |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | да | — | Токен бота от @BotFather |
| `OPENAI_API_KEY` | нет | — | Ключ OpenAI / совместимого API |
| `OPENAI_BASE_URL` | нет | `https://api.openai.com/v1` | Базовый URL AI-провайдера |
| `AI_MODEL` | нет | `gpt-4o-mini` | Модель для анализа заявок |
| `DATABASE_PATH` | нет | `./data/hermes.db` (локально) | Путь к файлу SQLite |
| `BOT_ENV` | нет | `development` | `development` / `production` |

## Деплой на Railway

1. Запушить репозиторий на GitHub
2. Создать проект в Railway, подключить репозиторий
3. Railway подхватит `railway.json` и `Dockerfile`
4. **Обязательно:** создать volume и примонтировать в `/app/data` (SQLite)

   ```bash
   railway volume add -m /app/data
   ```

5. Задать переменные окружения в Railway:
   - `TELEGRAM_BOT_TOKEN`
   - `OPENAI_API_KEY`
   - `BOT_ENV=production`

## Настройка Telegram-бота

1. Создать бота через [@BotFather](https://t.me/BotFather)
2. Добавить бота в рабочую группу
3. **Отключить privacy mode** через BotFather (`/setprivacy` → Disable) — иначе бот не видит сообщения без `/команд`
4. Выдать боту права на чтение сообщений

## Архитектура

```
┌─────────────┐     ┌────────────────┐     ┌──────────────┐
│ Telegram    │────▶│ krugo-bot      │────▶│ Hermes AI    │
│ Group       │◀────│ (Go + SQLite)  │◀────│ (OpenAI API) │
└─────────────┘     └────────────────┘     └──────────────┘
                           │
                           ▼
                    ┌──────────────┐
                    │ Railway      │
                    │ + Volume     │
                    │ /app/data    │
                    └──────────────┘
```

## Статусы заявок

| Статус | Описание |
|---|---|
| `received` | Заявка получена |
| `in_progress` | Hermes анализирует |
| `needs_clarification` | Нужны уточнения |
| `ready_for_review` | Готово к проверке |
| `ready_for_dev` | Можно передавать в разработку |
| `assigned` | Назначен ответственный |
| `done` | Закрыто |
| `rejected` | Отклонено |
