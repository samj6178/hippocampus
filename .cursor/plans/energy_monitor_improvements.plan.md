---
name: ""
overview: ""
todos: []
isProject: false
---

# Energy Monitor - План улучшений и документация

## Текущее состояние проекта (Февраль 2026)

### Архитектура системы

- **Backend**: Go + PostgreSQL/TimescaleDB + MQTT
- **Frontend**: Vanilla JS + Chart.js + CSS3 Animations
- **Telegram Bot**: Отчёты и уведомления
- **ML**: CatBoost модель для предиктивного анализа (только "Блок выдува" UUID 21)

### Ключевые компоненты

1. **Топология сети** (`Топология сети` tab) - визуализация энергосети завода
2. **Оперативная панель** - мониторинг в реальном времени
3. **Телеметрия** - исторические графики с выбором периода (NEW!)
4. **Исторический анализ** - графики и экспорт данных
5. **Экономика** - расчёт потребления и затрат

### Таблицы БД (TimescaleDB hypertables)


| Таблица                       | Описание                         | Источник данных     |
| ----------------------------- | -------------------------------- | ------------------- |
| `me337_timeseries`            | ME337 power meters (UUIDs 8-48)  | MQTT `ME337-UUID:*` |
| `rogovski_timeseries`         | Rogovski coil sensor (UUID 1)    | MQTT `RogID-1`      |
| `equipment_operational_modes` | Режимы работы оборудования       | ML классификатор    |
| `commercial_meter_readings`   | Показания коммерческих счётчиков | Ручной ввод         |
| `energy_economics_summary`    | Экономические сводки             | Агрегация           |


---

## Исправления Февраль 2026

### 🔧 Критические баги (исправлено)

#### 1. Rogovski данные не сохранялись в БД

**Проблема**: Колонка в SQL запросе называлась `raw_message`, а в таблице `raw_msg`
**Файл**: `internal/storage/postgres_storage.go`
**Исправление**: Переименовано `raw_message` → `raw_msg` в INSERT запросе
**Результат**: Данные UUID 1 (Ввод Т1) теперь сохраняются в `rogovski_timeseries`

#### 2. KPI "Расход за сутки" показывал ~300 кВА·ч вместо тысяч

**Проблема**: Запрос для T1 искал данные в `me337_timeseries` вместо `rogovski_timeseries`
**Файл**: `internal/service/economics_api.go` функция `calculateEnergyKVA()`
**Исправление**: T1 теперь берётся из `rogovski_timeseries` с колонками `phase1_amp, phase2_amp, phase3_amp`

#### 3. Группы показывали завышенную мощность (double-counting)

**Проблема**: "Голова" группы (head sensor) считалась дважды - как сумма и как отдельное устройство
**Файл**: `frontend/app.js` функция `updateGroupStatusCards()`
**Исправление**: Если есть `headUuid`, берём только его ток; иначе суммируем children

### ✨ Новые возможности

#### 1. Вкладка "Телеметрия"

- Третья вкладка в навигации (между Оперативной панелью и Историческим анализом)
- Выбор произвольного периода через календарь (datetime-local inputs)
- Быстрые кнопки: 1ч, 8ч, 24ч, 7д, 30д
- Multi-select устройств по группам (аккордеон)
- График средней мощности (кВА) по времени
- Статистика: кол-во устройств, точек данных, макс/средняя мощность
- Данные загружаются из PostgreSQL (не из realtime buffer)

#### 2. Конвертация единиц A → кВА

- Все отображения тока конвертированы в мощность (кВА)
- Формула: `P = I_avg × 380 × √3 / 1000`
- Затронуты: Статус оборудования, Top-5 потребителей, Баланс фаз, Схема

#### 3. "Неизвестный расход" в Экономике

- Переименован "Дисбаланс" → "Неизвестный расход"
- Показывает разницу между головным датчиком и суммой дочерних
- Визуальная индикация (зелёный/жёлтый/красный по %)

### 🎨 UI/UX улучшения

- GPU-ускоренные анимации (`will-change`, `translateZ(0)`)
- Ускорены все анимации (0.6s → 0.15s)
- Унифицированный дизайн T1 и T2 блоков (`.main-transformer`)
- Улучшенная читаемость карточек устройств
- Легенда для графиков телеметрии

---

## Выполненные задачи (до Февраля 2026)

### Интеграция устройств

- ПЭТ-2 (UUIDs 8, 9, 10-19, 45, 46)
- Распределитель (UUIDs 38-43)
- Стекло (UUIDs 30-35)
- Северный компрессор №2 (UUID 48)
- Южный компрессор (UUID 27)
- Блок выдува (UUID 21) с ML
- **Ввод Т1 / Rogovski (UUID 1)** - данные теперь сохраняются!

### Frontend улучшения

- Dual-transformer schema (T1 + T2)
- "ЗАВОД ВСЕГО" block (T1 + T2 sum)
- Sticky navigation
- Real-time animations
- Mini-charts in device modals (API-based)
- 3-phase display (I1, I2, I3)
- Light/Dark theme support
- ML chip for UUID 21
- KPI simplification (факт вместо прогноза)
- **Телеметрия tab с календарём**
- **Единицы измерения кВА везде**

### Backend

- Historical API endpoint с поддержкой `from`/`to` параметров
- Economics API
- Device hierarchy support
- systemd auto-restart
- **Rogovski persistence fix**

---

## 🚨 Известные проблемы / TODO

### Критические


| #   | Проблема                                                                      | Статус           | Файл                                     |
| --- | ----------------------------------------------------------------------------- | ---------------- | ---------------------------------------- |
| 1   | Лог-файл 68 ГБ на сервере                                                     | ⚠️ Нужна ротация | `/srv/energy-monitor/energy-monitor.log` |
| 2   | KPI "Расход за сутки" будет корректен только после накопления данных Rogovski | ⏳ Ждём           | `economics_api.go`                       |


### Средние


| #   | Проблема                                       | Статус       | Примечание                             |
| --- | ---------------------------------------------- | ------------ | -------------------------------------- |
| 3   | Нет исторических данных Rogovski до 02.02.2026 | ℹ️ Норма     | Данные начали записываться после фикса |
| 4   | Распределитель и Стекло показывают ОФЛАЙН      | 🔍 Проверить | Возможно нет MQTT данных               |


### Рекомендации

- **Настроить logrotate** для `/srv/energy-monitor/energy-monitor.log`
- **Добавить мониторинг** размера лог-файла
- **Backup** таблицы `rogovski_timeseries` и `me337_timeseries`

---

## Рекомендуемые улучшения

### 🔴 Высокий приоритет

#### 1. Алерты и уведомления

```
Файлы: internal/service/alerts.go (создать), internal/telegram/bot.go
```

- Пороговые значения для тока/мощности
- Push-уведомления в Telegram при превышении
- Email alerts (опционально)
- Визуальные алерты на схеме (мигание, цвет)

#### 2. Улучшение ML модели

```
Файлы: internal/ml/predictor.go, frontend/app.js
```

- Расширить ML на другие устройства (компрессоры)
- Добавить anomaly detection
- Улучшить confidence scoring
- Real-time prediction display

#### 3. Экспорт данных

```
Файлы: internal/service/export.go (создать), frontend/app.js
```

- Excel export (xlsx)
- PDF reports с графиками
- Scheduled reports (еженедельные, ежемесячные)
- Custom date range selection

### 🟡 Средний приоритет

#### 4. Балансировка фаз

```
Файлы: internal/service/phase_balance.go (создать), frontend/app.js
```

- Расчёт дисбаланса фаз (%)
- Визуализация на схеме
- Рекомендации по перераспределению нагрузки
- Historical phase balance trends

#### 5. Иерархия устройств и баланс сети

```
Файлы: internal/storage/migrations/005_device_hierarchy.sql, frontend/app.js
```

- Parent-child relationships в БД
- "Head" sensor per group
- Delta calculation (head vs sum of children)
- Unbalance highlighting (>5%)

#### 6. AI Chat Integration

```
Файлы: См. ai_chat_integration_66c119a0.plan.md
```

- Чат-бот для запросов по данным
- Natural language queries
- Integration с Telegram

#### 7. Dashboard customization

```
Файлы: frontend/app.js, frontend/style.css
```

- Drag-and-drop widgets
- Custom layouts per user
- Widget resize
- Save/load configurations

### 🟢 Низкий приоритет (nice-to-have)

#### 8. Mobile-first responsive design

```
Файлы: frontend/style.css
```

- Optimized mobile layout
- Touch gestures
- PWA support

#### 9. Multi-language support

```
Файлы: frontend/i18n/ (создать)
```

- Russian (default)
- English
- Language switcher

#### 10. Advanced charting

```
Файлы: frontend/app.js
```

- Heatmaps
- Sankey diagrams для потоков энергии
- Comparison charts (period vs period)
- Forecast overlays

#### 11. User management improvements

```
Файлы: internal/auth/, frontend/
```

- Role-based permissions
- Audit log
- Session management
- Password reset

#### 12. Integration с внешними системами

```
Файлы: internal/integrations/ (создать)
```

- 1C integration
- SCADA systems
- Energy billing systems

---

## Технические улучшения

### Performance

- WebSocket для real-time updates (вместо polling)
- Query optimization для больших периодов
- Frontend bundle optimization
- Image/asset compression
- Lazy loading для графиков

### Security

- Rate limiting
- Input validation
- HTTPS enforcement
- CORS configuration
- SQL injection prevention audit

### DevOps

- CI/CD pipeline
- Automated testing
- Docker containerization
- Monitoring (Prometheus/Grafana)
- Backup automation

### Code Quality

- Unit tests coverage >80%
- Integration tests
- API documentation (Swagger/OpenAPI)
- Frontend component library
- Error handling standardization

---

## Структура файлов проекта

```
energo/
├── cmd/
│   └── energy-monitor/
│       └── main.go           # Entry point, routes, handlers
├── internal/
│   ├── auth/                 # Authentication
│   ├── config/               # Configuration
│   ├── ml/                   # Machine learning
│   ├── mqtt/                 # MQTT client
│   ├── service/
│   │   ├── energy_service.go # Core business logic
│   │   └── economics_api.go  # KPI calculations
│   ├── storage/
│   │   └── migrations/       # DB migrations
│   └── telegram/
│       └── bot.go            # Telegram bot
├── frontend/
│   ├── index.html            # Main HTML
│   ├── app.js                # Frontend logic (4300+ lines)
│   └── style.css             # Styles (6300+ lines)
└── .cursor/
    └── plans/                # Documentation
```

---

## Ключевые UUIDs устройств


| UUID        | Название              | Группа     | Таблица БД            | Примечание         |
| ----------- | --------------------- | ---------- | --------------------- | ------------------ |
| 1           | Ввод Т1 (Rogovski)    | Подстанция | `rogovski_timeseries` | Head T1, ~350-500A |
| 25          | Общий ввод ПЭТ-1      | ПЭТ-1      | `me337_timeseries`    | Head ПЭТ-1         |
| 26          | Общий ввод Сев. комп. | Сев. комп. | `me337_timeseries`    | Head Сев. комп.    |
| 27          | Общий ввод Юж. комп.  | Юж. комп.  | `me337_timeseries`    | Head Юж. комп.     |
| 30          | Общий ввод Стекло     | Стекло     | `me337_timeseries`    | Head Стекло        |
| 21          | Блок выдува           | ПЭТ-1      | `me337_timeseries`    | ML enabled         |
| 8-19, 45-46 | ПЭТ-2                 | ПЭТ-2      | `me337_timeseries`    | T2 branch          |
| 38-43       | Распределитель        | Распред.   | `me337_timeseries`    | T1 branch          |
| 48          | Сев. компрессор №2    | Сев. комп. | `me337_timeseries`    | T1 branch          |


### Группы и головные датчики


| Группа             | Head UUID | Child UUIDs        |
| ------------------ | --------- | ------------------ |
| ПЭТ-1              | 25        | 20, 21, 22, 23, 24 |
| Сев. компрессорная | 26        | 28, 29, 48         |
| Юж. компрессорная  | 27        | -                  |
| Стекло             | 30        | 31, 32, 33, 34, 35 |
| ПЭТ-2              | -         | 8-19, 45-46        |
| Распределитель     | -         | 38-43              |


---

## API Endpoints


| Endpoint                        | Method | Params                         | Description                   |
| ------------------------------- | ------ | ------------------------------ | ----------------------------- |
| `/api/v1/timeseries/realtime`   | GET    | `uuid`, `timeframe`            | Real-time data from buffer    |
| `/api/v1/timeseries/historical` | GET    | `uuid`, `from`, `to`, `period` | Historical data from DB       |
| `/api/v1/kpi/daily`             | GET    | -                              | Daily KPIs (мощность, расход) |
| `/api/v1/economics/summary`     | GET    | -                              | Economics data                |
| `/api/v1/ml/predict/{uuid}`     | GET    | -                              | ML predictions                |
| `/api/v1/devices`               | GET    | -                              | Device list                   |


### Historical API параметры

```
GET /api/v1/timeseries/historical?uuid=21&from=2026-02-01T00:00:00Z&to=2026-02-02T00:00:00Z
```


| Param    | Type    | Description                         |
| -------- | ------- | ----------------------------------- |
| `uuid`   | int     | Device UUID (required)              |
| `from`   | ISO8601 | Start datetime (optional)           |
| `to`     | ISO8601 | End datetime (optional)             |
| `period` | string  | Alternative: `7d`, `30d` (optional) |


Bucket size автоматически выбирается по длительности периода:

- < 2h: 1 minute
- < 24h: 5 minutes  
- < 7d: 15 minutes
- < 30d: 1 hour
- ≥ 30d: 1 day

---

## Контакты и ресурсы

- **Server**: 87.192.224.234:50000 (SSH)
- **Frontend**: /srv/energy-monitor/frontend/
- **Backend**: /srv/energy-monitor/
- **Logs**: /srv/energy-monitor/energy-monitor.log (⚠️ 68GB!)
- **Database**: PostgreSQL/TimescaleDB (энергия_monitor)

---

## MQTT Topics и форматы сообщений

### ME337 Power Meters

**Topic**: `ME337-UUID:{uuid}` (например `ME337-UUID:21`)
**Format**: JSON с полями PT, I1, I2, I3, U1, U2, U3, PFT и др.

### Rogovski Coil Sensors

**Topic**: `RogID-1`
**Format**: `[HH::MM::SS::microseconds][RogID-1] -> P1: XXX.XX; P2: XXX.XX; P3: XXX.XX`
**Пример**: `[15::11::10::269269][RogID-1] -> P1: 331.198; P2: 331.974; P3: 356.692`

**Парсер**: `internal/models/energy.go` функция `ParseRogovskiMessage()`

---

## Полезные команды

```bash
# Проверить статус сервиса
ssh -t -p 50000 energy@87.192.224.234 "sudo systemctl status energy-monitor"

# Посмотреть логи (последние 100 строк)
ssh -t -p 50000 energy@87.192.224.234 "tail -100 /srv/energy-monitor/energy-monitor.log"

# Проверить данные Rogovski в БД
ssh -t -p 50000 energy@87.192.224.234 "sudo -u postgres psql -d energy_monitor -c \"SELECT COUNT(*) FROM rogovski_timeseries WHERE time > NOW() - INTERVAL '1 hour';\""

# Послушать MQTT топик Rogovski
ssh -t -p 50000 energy@87.192.224.234 "timeout 10 mosquitto_sub -h localhost -p 51883 -u EnergyMonitor -P Energy -t 'RogID-1' -v"

# Деплой (после сборки)
scp -P 50000 energy-monitor energy@87.192.224.234:/tmp/
ssh -t -p 50000 energy@87.192.224.234 "sudo systemctl stop energy-monitor && sudo cp /tmp/energy-monitor /srv/energy-monitor/ && sudo systemctl start energy-monitor"
```

---

*Документ обновлён: 02 Февраля 2026*