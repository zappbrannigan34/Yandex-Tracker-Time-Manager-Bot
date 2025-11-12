# Yandex Tracker Time Manager

Автоматическое списание времени в Yandex Tracker с учётом производственного календаря РФ.

---

## Возможности

- ✅ Автоматическое списание до 8 часов в рабочий день
- ✅ Интеграция с производственным календарём РФ (isdayoff.ru)
- ✅ Поддержка ежедневных и еженедельных задач с фиксированным временем
- ✅ **Случайное распределение времени на задачи с доски** (board_tasks)
- ✅ Распределение оставшегося времени по открытым задачам
- ✅ Рандомизация времени ±1% для естественности
- ✅ 3 режима работы: daemon, cron/Task Scheduler, CLI
- ✅ **Детальные отчёты:** списки worklogs по дням/неделям/месяцам
- ✅ Автоматическое обновление IAM токенов
- ✅ Fallback на локальный календарь при недоступности API
- ✅ State management для еженедельных задач
- ✅ Поддержка Windows/Linux/macOS/Docker
- ✅ Поддержка SSO/Cloud Organizations
- ✅ **Month-to-date tracking:** статистика за месяц с дефицитом часов
- ✅ **Backfill:** автоматическое заполнение пропущенных дней

---

## 🚀 Quick Start

### Вариант 1: Скачать готовый бинарник (Recommended)

**Перейти на [Releases](https://github.com/zappbrannigan34/Yandex-Tracker-Time-Manager-Bot/releases/latest) и скачать бинарник для вашей платформы:**

- **Windows:** `time-tracker-bot_<version>_windows_amd64.zip`
- **Linux:** `time-tracker-bot_<version>_linux_amd64.tar.gz`
- **macOS (Intel):** `time-tracker-bot_<version>_darwin_amd64.tar.gz`
- **macOS (Apple Silicon):** `time-tracker-bot_<version>_darwin_arm64.tar.gz`

**Для Windows также доступен daemon бинарник с GUI subsystem:**
- `time-tracker-bot-daemon.exe` - запускается без консольного окна

### Вариант 2: Собрать из исходников

### Предварительные требования

- **Go 1.21+** (для сборки)
- **yc CLI** (Yandex Cloud CLI для получения IAM токенов)

### Linux/macOS:

```bash
# 1. Установить yc CLI
curl -sSL https://storage.yandexcloud.net/yandexcloud-yc/install.sh | bash

# 2. Авторизоваться в Yandex Cloud
yc init
# Для SSO: yc init --federation-id=YOUR_FEDERATION_ID

# 3. Склонировать репозиторий
git clone https://github.com/zappbrannigan34/Yandex-Tracker-Time-Manager-Bot
cd Yandex-Tracker-Time-Manager-Bot

# 4. Собрать
go build -o time-tracker-bot ./cmd/time-tracker-bot

# 5. Создать config.yaml из примера
cp config.example.yaml config.yaml

### Общая настройка (после установки)

```bash
# 1. Установить yc CLI (если ещё не установлен)
# Linux/macOS:
curl -sSL https://storage.yandexcloud.net/yandexcloud-yc/install.sh | bash

# Windows:
# iex (New-Object System.Net.WebClient).DownloadString('https://storage.yandexcloud.net/yandexcloud-yc/install.ps1')

# 2. Авторизоваться в Yandex Cloud
yc init
# Для SSO: yc init --federation-id=YOUR_FEDERATION_ID

# 3. Создать config.yaml из примера
cp config.example.yaml config.yaml

# 4. Настроить config.yaml
# ОБЯЗАТЕЛЬНЫЕ параметры:
# - tracker.org_id: получить из https://tracker.yandex.ru/admin/orgs
# - tracker.board_id: ID вашей доски в Tracker
# - time_rules.daily_tasks: ваши ежедневные задачи
# - time_rules.weekly_tasks: ваши еженедельные задачи

# 5. Тестовый запуск (dry-run, ничего не создаёт)
./time-tracker-bot sync --dry-run

# 6. Реальное списание времени
./time-tracker-bot sync

# 7. Запустить daemon (автоматическое списание в 20:00 MSK ежедневно)
./time-tracker-bot daemon
```

---

## ⚠️ Важно для SSO/Cloud Organizations

Для корпоративных аккаунтов с SSO и Cloud Organizations:

- ✅ **Используются IAM токены** (не OAuth)
- ✅ **API заголовок:** `X-Cloud-Org-Id` (автоматически настроено)
- ✅ **Авторизация:** `Authorization: Bearer <token>` (автоматически настроено)
- ⚠️ **НЕ ПУТАЙТЕ:** `federation-id` (для `yc init`) ≠ `org_id` (для `config.yaml`)

**Как получить правильный org_id:**
1. Открыть https://tracker.yandex.ru/admin/orgs
2. Скопировать Organization ID
3. Указать в `config.yaml` → `tracker.org_id`

---

## 📖 Использование

### CLI команды

```bash
# Dry-run (показать без записи)
./time-tracker-bot sync --dry-run

# Списать время за сегодня
./time-tracker-bot sync --date today

# Списать время за конкретную дату
./time-tracker-bot sync --date 2025-01-15

# Показать уже списанное время (агрегированное)
./time-tracker-bot status

# Показать детальный отчёт по задачам
./time-tracker-bot report --date 2025-11-01      # один день
./time-tracker-bot report --week                  # текущая неделя (пн-вс)
./time-tracker-bot report --month                 # текущий месяц (1-е до последнего числа)
./time-tracker-bot report --from 2025-11-01 --to 2025-11-07  # произвольный диапазон

# Показать расписание еженедельных задач
./time-tracker-bot weekly-schedule

# Статистика за месяц с дефицитом часов
./time-tracker-bot status --month                     # текущий месяц (month-to-date)
./time-tracker-bot status --from 2025-11-01 --to 2025-11-11  # произвольный период

# Заполнить пропущенные дни
./time-tracker-bot backfill                           # заполнить все пропущенные дни месяца
./time-tracker-bot backfill --from 2025-11-01 --to 2025-11-08  # заполнить конкретный период
./time-tracker-bot backfill --dry-run                 # показать что будет сделано (без записи)
```

### 📊 Month-to-Date Tracking & Backfill

**Status --month** показывает детальную статистику за период:

```bash
$ ./time-tracker-bot status --month

Month Status (2025-11-01 to 2025-11-11):
═══════════════════════════════════════════════════════
  Period:           2025-11-01 to 2025-11-11 (11 calendar days)
  Working days:     6 days
  Target hours:     47.0h (2820 minutes)
  Worked hours:     40.0h (2397 minutes)
  Deficit:          7.0h (423 minutes) ⚠️
  Progress:         85.0%

  Days breakdown:
    ✅ 2025-11-01 (Sat): 8h  0m / 7h  0m (114%)
    ✅ 2025-11-05 (Wed): 8h  0m / 8h  0m (100%)
    ❌ 2025-11-10 (Mon): 0h  0m / 8h  0m (0%)
    ⏳ 2025-11-11 (Tue): 7h 57m / 8h  0m (99%)

  Missing days: 1
  💡 Tip: Use 'time-tracker-bot backfill' to fill missing days
```

**Backfill** автоматически заполняет пропущенные дни:

```bash
$ ./time-tracker-bot backfill --dry-run

📋 Backfill Summary (2025-11-01 to 2025-11-10):
═══════════════════════════════════════════════════════
  Processed days:    1
  Total entries:     5
  Total time:        8.0h (480 minutes)

  Days processed:
    ✅ 2025-11-10: 5 entries, 8.0h
      • PROJ-101      0h 29m  Daily standup
      • PROJ-102      0h 10m  Team sync 1
      • PROJ-201      7h  0m  Development work

[DRY RUN] No worklogs were created
```

**Алгоритм поиска задач:**
- **Source 1:** Worklogs (задачи с уже залогированным временем)
- **Source 2:** Current Board (текущие задачи на доске)
- **Source 3:** Updated Filter (все обновлённые задачи с начала периода)
- **Changelog Analysis:** Для каждой задачи строится timeline статусов
- **Результат:** Для каждого пропущенного дня находятся все задачи которые были "in progress" на эту дату

**Safety features:**
- Default: backfill month-to-date (исключая текущий день)
- Dry-run preview перед реальным созданием worklogs
- Exact 8h normalization после рандомизации

### 🤖 Daemon Mode (Recommended)

**Автоматическое списание времени ежедневно в заданное время:**

**Запуск daemon:**

```bash
# Windows
.\time-tracker-bot.exe daemon

# Linux/macOS
./time-tracker-bot daemon
```

**Настройка в config.yaml:**
```yaml
daemon:
  daily_time: "20:00"     # Время списания (HH:MM, MSK UTC+3)
  system_tray: true       # Иконка в системном трее (Windows)
  log_file: "./logs/time-tracker-bot.log"
  log_level: "info"
```

**Daemon будет:**
- ✅ Списывать время каждый день в 20:00 MSK (настраивается)
- ✅ Если запустился ПОСЛЕ 20:00 → списывает время сразу
- ✅ Работать в фоне БЕЗ консольного окна (Windows)
- ✅ Показывать иконку в системном трее (Windows)
- ✅ Логировать в `./logs/time-tracker-bot.log`

**System Tray Menu (Windows):**
- 🔄 **Sync Now** - запустить синхронизацию вручную
- 📊 **Status** - показать текущий статус (worked/target minutes)
- 🚪 **Quit** - завершить daemon

**Windows: Автозапуск при загрузке системы**

**Через Task Scheduler (рекомендуется):**

1. Открыть Task Scheduler (`taskschd.msc`)
2. Create Task:
   - **General:** Name: "Time Tracker Bot", Run whether user is logged on or not
   - **Triggers:** At startup
   - **Actions:** Start program: `C:\path\to\time-tracker-bot.exe`, Arguments: `daemon`
   - **Settings:** Allow task to run on demand, Stop task if runs longer than 3 days

**Linux/macOS: systemd service**

```bash
sudo systemctl enable time-tracker-bot
sudo systemctl start time-tracker-bot
sudo systemctl status time-tracker-bot
```

### Cron Mode (альтернатива)

```bash
# Добавить в crontab:
0 */2 9-18 * * 1-5 /usr/local/bin/time-tracker-bot sync
```

---

## ⚠️ КРИТИЧНО: Duration Format

Yandex Tracker использует **BUSINESS time units**:
- **P1D** = 1 рабочий день = **8 часов** (не 24!)
- **P1W** = 1 рабочая неделя = **40 часов** (5 дней по 8 часов, не 168!)
- **P1W2D** = 1 неделя + 2 дня = 56 часов
- **PT8H** = 8 обычных часов

Парсер корректно обрабатывает все форматы, включая комбинированные (P1WT20M, P2DT3H30M).

---

## 📊 Архитектура

```
┌─────────────────────────────────────────┐
│   CLI / Daemon / Cron Modes             │
├─────────────────────────────────────────┤
│  Time Manager Core                      │
│   ├── Daily Tasks (fixed time)          │
│   ├── Weekly Tasks (scheduled days)     │
│   ├── Board Tasks (random selection)    │
│   └── Remaining Time → Open Issues      │
├─────────────────────────────────────────┤
│  Calendar Module                        │
│   ├── isdayoff.ru API (primary)         │
│   └── Fallback: xmlcalendar.ru          │
├─────────────────────────────────────────┤
│  Tracker API Client                     │
│   ├── Search Issues                     │
│   ├── Get All Board Issues              │
│   ├── Get Current User (SSO support)    │
│   ├── Get Worklogs (user filtering)     │
│   └── Create Worklog                    │
├─────────────────────────────────────────┤
│  IAM Token Manager                      │
│   └── Auto-refresh every hour           │
└─────────────────────────────────────────┘
```

---

## 🔧 Конфигурация

Основные параметры в `config.yaml`:

### 1. Tracker API

```yaml
tracker:
  # Organization ID (обязательно)
  # Получить: https://tracker.yandex.ru/admin/orgs
  org_id: "${TRACKER_ORG_ID}"  # ⚠️ Tracker org_id (НЕ federation-id!)

  # API endpoint (по умолчанию)
  api_endpoint: "https://api.tracker.yandex.net"

  # Board ID для получения задач
  board_id: 123

  # Запрос для поиска задач
  # Примеры:
  #   - Boards: 123 - задачи с доски 123
  #   - Assignee: me() - назначенные на текущего пользователя
  #   - Status: "inProgress" - в работе
  #   - Resolved: today() - завершённые сегодня
  #   - Type: story, task, bug - исключить родительские (feature, epic)
  issues_query: "Boards: 123 AND Assignee: me() AND (Status: \"inProgress\" OR Resolved: today()) AND Type: story, task, bug"
```

### 2. Production Calendar

```yaml
calendar:
  # Тип календаря: "isdayoff" (бесплатный) или "production-calendar" (устаревший, платный)
  type: "isdayoff"

  # Fallback URL для офлайн-данных (xmlcalendar.ru)
  # {year} будет заменён на текущий год (например, 2025)
  fallback_url: "https://xmlcalendar.ru/data/ru/{year}/calendar.json"

  # TTL кэша календаря
  cache_ttl: "24h"
```

### 3. Time Distribution Rules

```yaml
time_rules:
  # Целевые рабочие часы в день (обычно 8)
  target_hours_per_day: 8

  # Ежедневные задачи (фиксированное время каждый рабочий день)
  daily_tasks:
    - issue: "PROJ-101"
      minutes: 30
      description: "Daily standup"

    - issue: "PROJ-102"
      minutes: 10
      description: "Team sync"

  # Еженедельные задачи (распределяются на N случайных дней в неделю)
  weekly_tasks:
    - issue: "PROJ-201"
      hours_per_week: 8          # общее время в неделю
      days_per_week: 2           # на скольких днях распределить
      description: "Infrastructure tasks"

    - issue: "PROJ-202"
      hours_per_week: 2
      days_per_week: 2
      description: "Administrative tasks"

  # Опциональное случайное распределение времени на задачи с доски
  board_tasks:
    enabled: false                      # включить/выключить
    base_minutes_per_day: 30            # базовое время (например, 30 минут)
    randomization_percent: 40.0         # рандомизация времени ±40% (18-42 мин)
    tasks_percent: 20.0                 # процент задач от доски (20%)
    tasks_randomization_percent: 40.0   # рандомизация количества ±40%

  # Рандомизация времени для естественности (±1%)
  randomization_percent: 1.0
```

### 4. Daemon Mode

```yaml
daemon:
  # Время ежедневной синхронизации (HH:MM формат, MSK timezone UTC+3)
  # Запускает синхронизацию раз в день в указанное время
  # По умолчанию: 20:00 (8 PM по Москве)
  daily_time: "20:00"

  # Windows only: показывать иконку в системном трее
  # Включает меню по правому клику: "Sync Now", "Status", "Quit"
  system_tray: true

  # Путь к лог-файлу
  log_file: "./logs/time-tracker-bot.log"

  # Уровень логирования: debug, info, warn, error
  log_level: "info"
```

### 5. IAM Token

```yaml
iam:
  # Как часто ПРОВЕРЯТЬ нужно ли обновить токен
  # Токен обновляется ТОЛЬКО если истекает в течение 1 часа
  # IAM токены живут до 12 часов
  # Рекомендуется: проверять каждые 1-6 часов (по умолчанию: 6h)
  refresh_interval: "6h"

  # CLI команда для получения IAM токена
  cli_command: "yc iam create-token"  # или полный путь на Windows
```

**Примечание:** Теперь токен **НЕ запрашивается** каждый `refresh_interval`. Вместо этого система **проверяет** валидность токена и обновляет его **только если осталось меньше 1 часа** до истечения.

### 6. State Storage

```yaml
state:
  # Файл для хранения состояния еженедельных задач
  weekly_schedule_file: "./state/weekly_schedule.json"
```

**Полный пример со всеми параметрами:** [`config.example.yaml`](./config.example.yaml)

---

## 🧪 Тестирование

```bash
# Unit tests (26/26 passed ✅)
go test ./... -v

# Dry-run test
./time-tracker-bot sync --dry-run --date today

# Check status
./time-tracker-bot status

# Weekly schedule
./time-tracker-bot weekly-schedule
```

**Результаты (2025-11-11):**
- ✅ API authentication (SSO/Cloud Organizations)
- ✅ status: 1h worked, 8h target
- ✅ dry-run sync: 6 entries, 7h 2m distributed
- ✅ weekly-schedule: Week 46, 2025

---

## 🛠️ Troubleshooting

### Ошибка "yc: command not found"

```bash
# Linux/macOS:
curl -sSL https://storage.yandexcloud.net/yandexcloud-yc/install.sh | bash

# Windows:
iex (New-Object System.Net.WebClient).DownloadString('https://storage.yandexcloud.net/yandexcloud-yc/install.ps1')
```

### Ошибка "Authentication required"

```bash
yc init
# Для SSO:
yc init --federation-id=<your-federation-id>
```

### Ошибка 403/401 от Tracker API

Краткая проверка:
1. `yc config list` - проверить авторизацию
2. `yc iam create-token` - проверить создание токена
3. `yc organization-manager organization list` - проверить тип организации

### Полная диагностика

```bash
# 1. Проверить yc CLI
yc version && yc config list

# 2. Проверить создание токена
yc iam create-token

# 3. Проверить доступ к Tracker API
TOKEN=$(yc iam create-token)
ORG_ID=$(grep org_id config.yaml | cut -d'"' -f2)
curl -H "Authorization: Bearer $TOKEN" \
     -H "X-Cloud-Org-Id: $ORG_ID" \
     https://api.tracker.yandex.net/v2/myself

# 4. Dry-run тест
./time-tracker-bot sync --dry-run
```

---

## 📚 Документация

- **[config.example.yaml](./config.example.yaml)** - Пример конфигурации с комментариями

---

## 🔐 Безопасность

- ❌ **НЕ коммитить** `config.yaml` с реальными данными
- ✅ IAM токены хранятся только в памяти (автообновление каждый час)
- ✅ Логи не содержат токены и чувствительную информацию
- ✅ `config.yaml` и `state/*.json` в .gitignore

---

## 📊 Статус проекта

**Версия:** 1.0
**Дата:** 2025-11-11
**Статус:** ✅ PRODUCTION READY

**Проверено на:**
- ✅ Windows 10/11 (Task Scheduler)
- ✅ Linux (systemd, cron)
- ✅ macOS (cron)
- ✅ Docker

**Поддержка аккаунтов:**
- ✅ Cloud Organizations (SSO/федеративные)
- ✅ 360 Organizations (обычные)

---

## License

MIT
