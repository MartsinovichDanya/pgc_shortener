# go-musthave-shortener-tpl

Шаблон репозитория для трека «Сервис сокращения URL».

## Начало работы

1. Склонируйте репозиторий в любую подходящую директорию на вашем компьютере.
2. В корне репозитория выполните команду `go mod init <name>` (где `<name>` — адрес вашего репозитория на GitHub без префикса `https://`) для создания модуля.

## Обновление шаблона

Чтобы иметь возможность получать обновления автотестов и других частей шаблона, выполните команду:

```
git remote add -m v2 template https://github.com/Yandex-Practicum/go-musthave-shortener-tpl.git
```

Для обновления кода автотестов выполните команду:

```
git fetch template && git checkout template/v2 .github
```

Затем добавьте полученные изменения в свой репозиторий.

## Запуск автотестов

Для успешного запуска автотестов называйте ветки `iter<number>`, где `<number>` — порядковый номер инкремента. Например, в ветке с названием `iter4` запустятся автотесты для инкрементов с первого по четвёртый.

При мёрже ветки с инкрементом в основную ветку `main` будут запускаться все автотесты.

Подробнее про локальный и автоматический запуск читайте в [README автотестов](https://github.com/Yandex-Practicum/go-autotests).

## Структура проекта

Приведённая в этом репозитории структура проекта является рекомендуемой, но не обязательной.

Это лишь пример организации кода, который поможет вам в реализации сервиса.

При необходимости можно вносить изменения в структуру проекта, использовать любые библиотеки и предпочитаемые структурные паттерны организации кода приложения, например:
- **DDD** (Domain-Driven Design)
- **Clean Architecture**
- **Hexagonal Architecture**
- **Layered Architecture**




File: ___go_build_shortener_.exe
Build ID: C:\Users\Danya\AppData\Local\JetBrains\GoLand2026.2\tmp\GoLand\___go_build_shortener_.exe2026-10-08 22:55:59.601582 +0300 MSK
Type: inuse_space
Time: 2026-10-08 23:07:09 MSK
Showing nodes accounting for 6196.39kB, 100% of 6196.39kB total
flat  flat%   sum%        cum   cum%
1536.51kB 24.80% 24.80%  1536.51kB 24.80%  go.uber.org/zap/zapcore.newCounters (inline)
1465.54kB 23.65% 48.45%  1465.54kB 23.65%  compress/flate.newFastEnc (inline)
1026kB 16.56% 65.01%     1026kB 16.56%  runtime.mallocgc
632.14kB 10.20% 75.21%   632.14kB 10.20%  github.com/MartsinovichDanya/pgc_shortener/internal/audit.NewSubject
512.11kB  8.26% 83.47%   512.11kB  8.26%  net.newFD (inline)
512.05kB  8.26% 91.74%   512.05kB  8.26%  github.com/MartsinovichDanya/pgc_shortener/internal/audit.(*Subject).worker
512.04kB  8.26%   100%   512.04kB  8.26%  context.withCancel (inline)
0     0%   100%  1465.54kB 23.65%  compress/flate.(*compressor).init
0     0%   100%  1465.54kB 23.65%  compress/flate.NewWriter (inline)
0     0%   100%  1465.54kB 23.65%  compress/gzip.(*Writer).Write
0     0%   100%   512.04kB  8.26%  context.WithCancel
0     0%   100%  1465.54kB 23.65%  github.com/MartsinovichDanya/pgc_shortener/internal/auth.AuthMiddleware.func1.1
0     0%   100%  1465.54kB 23.65%  github.com/MartsinovichDanya/pgc_shortener/internal/handler.(*ShortenerHandler).ShortenAPI
0     0%   100%  1536.51kB 24.80%  github.com/MartsinovichDanya/pgc_shortener/internal/logger.Initialize
0     0%   100%  1465.54kB 23.65%  github.com/MartsinovichDanya/pgc_shortener/internal/middleware.(*gzipResponseWriter).Write
0     0%   100%  1465.54kB 23.65%  github.com/MartsinovichDanya/pgc_shortener/internal/middleware.GzipMiddleware.func1
0     0%   100%  2680.76kB 43.26%  github.com/MartsinovichDanya/pgc_shortener/internal/server.Run
0     0%   100%  1465.54kB 23.65%  github.com/go-chi/chi/v5.(*Mux).ServeHTTP
0     0%   100%  1465.54kB 23.65%  github.com/go-chi/chi/v5.(*Mux).routeHTTP
0     0%   100%  1465.54kB 23.65%  github.com/go-chi/chi/v5/middleware.Recoverer.func1
0     0%   100%  1465.54kB 23.65%  github.com/go-chi/chi/v5/middleware.RequestID.func1
0     0%   100%  1465.54kB 23.65%  github.com/go-chi/chi/v5/middleware.RequestLogger.func1.1
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.(*Logger).WithOptions
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.Config.Build
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.Config.buildOptions.func1
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.New
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.WrapCore.func1
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap.optionFunc.apply
0     0%   100%  1536.51kB 24.80%  go.uber.org/zap/zapcore.NewSamplerWithOptions
0     0%   100%  2680.76kB 43.26%  main.main
0     0%   100%   512.11kB  8.26%  net.(*TCPListener).Accept
0     0%   100%   512.11kB  8.26%  net.(*TCPListener).accept
0     0%   100%   512.11kB  8.26%  net.(*netFD).accept
0     0%   100%   512.11kB  8.26%  net/http.(*Server).ListenAndServe
0     0%   100%   512.11kB  8.26%  net/http.(*Server).Serve
0     0%   100%  1977.58kB 31.91%  net/http.(*conn).serve
0     0%   100%  1465.54kB 23.65%  net/http.HandlerFunc.ServeHTTP
0     0%   100%   512.11kB  8.26%  net/http.ListenAndServe (inline)
0     0%   100%  1465.54kB 23.65%  net/http.serverHandler.ServeHTTP
0     0%   100%     1026kB 16.56%  runtime.allocm
0     0%   100%  2680.76kB 43.26%  runtime.main
0     0%   100%     1026kB 16.56%  runtime.mstart
0     0%   100%     1026kB 16.56%  runtime.mstart0
0     0%   100%     1026kB 16.56%  runtime.mstart1
0     0%   100%     1026kB 16.56%  runtime.newm
0     0%   100%     1026kB 16.56%  runtime.newobject
0     0%   100%     1026kB 16.56%  runtime.resetspinning
0     0%   100%     1026kB 16.56%  runtime.schedule
0     0%   100%     1026kB 16.56%  runtime.startm
0     0%   100%     1026kB 16.56%  runtime.wakep
