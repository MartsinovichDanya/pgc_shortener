# PGC Shortener

Сервис сокращения URL на Go: HTTP-сервер принимает длинные ссылки и возвращает короткие. Поддерживает файловое хранилище и PostgreSQL, авторизацию по cookie, структурированное логирование и события аудита.

## Структура проекта

- **`cmd/shortener`** — точка входа сервера сокращения ссылок
- **`cmd/client`**, **`cmd/reset`** — вспомогательные утилиты
- **`internal/`** — конфигурация, обработчики, middleware, репозитории, сервисы, логгер, аудит
- **`migrations/`** — миграции базы данных
- **`swagger/`**, **`api/`** — описание API

## Конфигурация

Параметры задаются переменными окружения (имеют приоритет) или флагами командной строки:

| Переменная окружения | Флаг | Описание |
|---|---|---|
| `SERVER_ADDRESS` | `-a` | адрес HTTP-сервера (по умолчанию `localhost:8080`) |
| `BASE_URL` | `-b` | базовый адрес коротких ссылок |
| `FILE_STORAGE_PATH` | `-f` | путь к файлу-хранилищу |
| `DATABASE_DSN` | `-d` | строка подключения к PostgreSQL (без неё используется файловое хранилище) |
| `LOG_LEVEL` | — | уровень логирования (по умолчанию `DEBUG`) |
| `AUDIT_FILE` / `AUDIT_URL` | `-audit-file` / `-audit-url` | приёмник событий аудита |

## Сборка

Обычная сборка бинарника:

```bash
go build -o shortener ./cmd/shortener
```

### Вшивка версии, даты и коммита сборки через `-ldflags`

В `cmd/shortener/main.go` объявлены переменные:

```go
var buildVersion = "N/A"
var buildDate    = "N/A"
var buildCommit  = "N/A"
```

При обычной сборке они остаются равными `"N/A"`, и при старте сервер выведет:

```
{"level":"info","ts":1793175609.1788976,"caller":"shortener/main.go:45","msg":"Build info","Build version":"N/A","Build date":"N/A","Build commit":"N/A"}
```

Чтобы получить реальные значения, передайте их линковщику через флаг `-ldflags` с опцией `-X`. Для пакета `main`, лежащего в `cmd/shortener`, правильный формат — `main.<имя_переменной>=<значение>`:

```bash
go build -ldflags "\
  -X 'main.buildVersion=$(git describe --tags --always)' \
  -X 'main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)' \
  -X 'main.buildCommit=$(git rev-parse --short HEAD)' \
  " -o shortener ./cmd/shortener
```

Результат при запуске `./shortener`:

```
{"level":"info","ts":1793175609.1788976,"caller":"shortener/main.go:45","msg":"Build info","Build version":"iter23","Build date":"2026-10-27T09:00:00Z","Build commit":"a1b2c3d"}
```

## Запуск

```bash
./shortener
```

Swagger-описание API находится в директории `swagger/` (генерируется командой `swag init`).
