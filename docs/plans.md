# План разработки Krugo Bot

## Текущая стадия: интеграция Hermes + PocketBase (18.07.2026)

### ✅ Этап 1 — Фундамент
- Go-модуль, структура проекта
- Конфигурация из env-переменных
- SQLite-хранилище с авто-миграцией (WAL mode)
- Структурированное логирование (slog)

### ✅ Этап 2 — Ядро
- Локальный детектор заявок (15 маркеров, ≥2 для срабатывания)
- Модель заявки + статусы (включая hermes_responded, hermes_failed)
- Команда `/status HERMES-XXXX`
- TELEGRAM_ALLOWED_USERS — обязательный allowlist

### ✅ Этап 3 — Telegram-бот + Hermes Agent
- `telebot.v3`: long polling, приём сообщений, callback, allowlist
- Hermes Agent: `nousresearch/hermes-agent:latest` (DeepSeek v4-flash, s6-overlay)
- Hermes gateway Telegram (второй бот)
- hermes-proxy: HTTP API к Hermes (auth, mutex, 15m timeout)

### ✅ Этап 4 — PocketBase интеграция
- contracts skill v2.1: providers → applications → contracts
- Полный цикл: поиск поставщика, поиск заявки, PATCH application + contract, проверка
- Auto-execute: без подтверждения, сразу действие + отчёт

### ✅ Инфраструктура
- Dokploy: два Compose (bot + hermes)
- `network_mode: host` для обхода VPS firewall
- Persistent volumes: `bot-data`, `hermes-data`
- Git-репозиторий, `.gitignore`, AGENTS.md, `.omp/AGENTS.md`

---

## Следующий этап — Аудит изменений

### Бэкенд-аудит (krugo-bot, не Hermes)
- [ ] SQLite таблица `audit_log`: request_id, contract_id, app_id, old_json, new_json, user, timestamp
- [ ] Запись при каждом успешном PATCH (GET before → GET after → diff → insert)
- [ ] Команда `/history HERMES-XXXX` — все изменения по заявке
- [ ] Формат вывода: `[дата] @user: договор X / поле Y: А → Б`

### Дальше
- [ ] Workspace volume для Hermes (доступ к репозиторию проекта)
- [ ] Интеграция с Jira / Linear / внутренней системой
- [ ] Миграция SQLite → PostgreSQL при росте
- [ ] Healthcheck, метрики, тесты
