# Сервис электронного документооборота (СЭД)

Самостоятельный серверный сервис жизненного цикла документов (API `/v1`, PostgreSQL, потоковое файловое хранилище `sha256`, фоновый воркер SLA и Outbox) с тремя клиентскими режимами и изолированным временным сервисом аутентификации `temp-auth` для локальной разработки и тестирования.

## Структура репозитория

```text
.
├── .cursor/skills/sed-tz-compliance/ # Скилл и скрипт проверки соответствия ТЗ
├── docs/ROADMAP.md                   # Поэтапная схема создания сервиса
├── deploy/reverse-proxy/             # Обратный прокси /v1 (streaming до 50 МБ)
├── services/
│   ├── api/                          # Сервер приложений REST API /v1 (M01-M09)
│   └── worker/                       # Фоновый воркер SLA, календаря и Outbox 1С
├── web/                              # Веб-клиент + адаптивный Тонкий режим
├── clients/thick-adapter/            # SDK-адаптер для настольного Толстого клиента
├── temp-auth/                        # ОТДЕЛЬНЫЙ временный сервис аутентификации (JWT + сессии)
│   └── tests/                        # Автономные тесты temp-auth
├── devtools/mock-external/           # Отдельный стаб внешних систем (задачи, уведомления, 1С)
│   └── tests/                        # Тесты мок-сервера
├── tests/                            # Директории тестов всех не внутренних контуров СЭД
│   ├── api-v1/
│   ├── worker/
│   ├── orchestrator-gateway/
│   ├── onec-outbox/
│   ├── reverse-proxy/
│   ├── web-client/
│   ├── thin-client/
│   ├── thick-client/
│   └── acceptance-ac/                # Приёмочные сценарии AC-01..AC-12
├── scripts/secret_guard.go           # Защита от утечки секретов и внутренних терминов
├── docker-compose.yml                # Основной контур (без temp-auth)
├── docker-compose.dev-auth.yml       # Подключение изолированного temp-auth
└── docker-compose.dev-mocks.yml      # Подключение моков внешних систем
```

## Быстрый старт (Docker)

Скопируйте шаблон переменных окружения:

```bash
cp .env.example .env
```

Запуск основного контура с отдельным временным сервисом аутентификации и внешними моками:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev-auth.yml -f docker-compose.dev-mocks.yml up --build
```

Для перехода на реальный сервис аутентификации достаточно запустить только `docker-compose.yml` и задать `AUTH_BASE_URL` на целевой endpoint. Папку `temp-auth/` и `docker-compose.dev-auth.yml` можно удалить без правок кода СЭД.

## Проверки и аудит

```bash
# Проверка соответствия архитектуре и инвариантам ТЗ
go run ./.cursor/skills/sed-tz-compliance/scripts/verify_compliance.go

# Сканер секретов и запрещённых файлов
go run ./scripts/secret_guard.go --all

# Тесты изолированного сервиса аутентификации
cd temp-auth && go test -v -race ./...
```
