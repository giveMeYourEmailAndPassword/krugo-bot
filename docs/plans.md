# План разработки Krugo Bot

## Текущая стадия: MVP (этапы 1–3 завершены)

### ✅ Этап 1 — Фундамент
- Go-модуль, структура проекта
- Конфигурация из env-переменных (Telegram, DeepSeek, БД)
- SQLite-хранилище с авто-миграцией (WAL mode)
- Структурированное логирование (slog)

### ✅ Этап 2 — Ядро
- Локальный детектор заявок (`LooksLikeRequest`, ≥2 маркеров)
- Модель заявки + 8 статусов lifecycle
- AI-анализатор: DeepSeek API, системный промпт Hermes, JSON mode
- Команда `/status HERMES-XXXX`

### ✅ Этап 3 — Telegram-бот
- `telebot.v3`: long polling, приём сообщений, callback-запросы
- Flow: детекция → ack → async DeepSeek → статус с кнопками
- Inline-кнопки: «Передать в dev», «Уточнение», «Назначить», «Закрыть»

### ✅ Инфраструктура
- Dockerfile (multi-stage, alpine)
- Railway config (`railway.json`)
- Git-репозиторий, `.gitignore`

---

## Что осталось

### Этап 4 — Hermes Agent (серверный)
- [ ] Установка Hermes на сервер
- [ ] Интеграция: бот → Hermes Agent API (create run)
- [ ] Отслеживание статуса: `queued → running → completed/failed`
- [ ] Возврат результата из проекта в чат
- [ ] Разделение: AI-анализ заявки vs статус выполнения в проекте

### Этап 5 — Интеграции
- [ ] Jira / Linear / Notion / внутренняя система задач
- [ ] Назначение ответственного через кнопку «Назначить»
- [ ] Дубликаты: проверка существующих заявок перед созданием

### Этап 6 — Production
- [ ] Миграция с SQLite на PostgreSQL (при росте нагрузки)
- [ ] Healthcheck-эндпоинт для Railway
- [ ] Метрики (Prometheus)
- [ ] Тесты
