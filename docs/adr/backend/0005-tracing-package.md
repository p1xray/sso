# ADR: `backend/pkg/tracing`

## Статус

Принят и реализован (сентябрь 2026). Пакет: `github.com/p1xray/sso/backend/pkg/tracing`, Go 1.27, зависимости: `go.opentelemetry.io/{otel,sdk,trace}` v1.44.0, `.../exporters/otlp/otlptrace/otlptracegrpc` v1.44.0, `go.opentelemetry.io/contrib/instrumentation/{google.golang.org/grpc/otelgrpc,net/http/otelhttp}` v0.69.0, `go.opentelemetry.io/proto/otlp` v1.10.0, `google.golang.org/grpc` v1.83.2.

## Контекст:

Единая обёртка над OpenTelemetry SDK для сбора распределённых трейсов всех сервисов монорепо с экспортом OTLP gRPC в LGTM-стек (Grafana + Tempo, локально `localhost:4317`). Первичные потребители - `backend/cmd/gateway`, где рождается решение о сэмплировании, и gRPC-сервисы `auth`/`oidc`/`users`/`clients`, которые обязаны это решение уважать.

### Требования:

- Сэмплер `ParentBased(TraceIDRatioBased(ratio))`: решение принимается один раз на краю, дочерние сервисы не рвут трейс - если шлюз сэмплировал запрос, downstream записывает спаны;
- Ресурс ровно из `service.name`/`service.version`/`deployment.environment` - единственных обязательных атрибутов по семантическим конвенциям, по которым бэкенд фильтрует данные;
- Перенос решения между хопами - W3C TraceContext через хелперы для существующих `grpcserver`/`grpcclient`/`httpserver` (см. [0003](0003-grpcserver-package.md), [0004](0004-grpcclient-package.md));
- Best-effort старт: недоступный коллектор не роняет и не блокирует сервис - осознанное отступление от fail-fast `postgresql`/`grpcclient` (см. [0002](0002-postgresql-package.md)): трейсинг - вспомогательная функция, падение Tempo не должно останавливать аутентификацию;
- DI без env и собственного глобального состояния, конвенции [0001](0001-logger-package.md)-[0004](0004-grpcclient-package.md).

### Альтернативы:

- Голый otel SDK в каждом сервисе - сэмплер, ресурс и экспортёр дублируются в пяти местах, drift настроек неизбежен;
- OTLP HTTP (порт 4318) вместо gRPC - стек принимает оба, но gRPC уже в зависимостях и ретраит неудачные экспорты из коробки;
- Интерцепторы otelgrpc - deprecated в актуальном contrib («Use stats handlers instead»);
- Глобальный `otel.SetTracerProvider` внутри пакета - process-wide состояние ломает изоляцию тестов и повторяет анти-паттерн, от которого отказались в [0001](0001-logger-package.md) (`slog.SetDefault`).

### Non-goals:

Метрики и логи (отдельные пакеты по образцу при появлении), доменные спаны прикладного кода, tail-based sampling, ручки конфигурации `BatchSpanProcessor`, менеджмент TLS-сертификатов, ресурс-детекторы (host/container/AWS).

## Ключевые архитектурные решения

### 1. Запечатанный интерфейс `Provider` + `Unwrap() *sdktrace.TracerProvider`

`*sdktrace.TracerProvider` приватен, наружу - интерфейс `Provider`: `Tracer` (точка входа для доменных спанов), `ServerStatsHandler`/`ClientStatsHandler` (`stats.Handler` для gRPC-сервера и клиента), `HTTPHandler` (обёртка `http.Handler`), `Shutdown` и `Unwrap` - escape-hatch, симметрично [0001](0001-logger-package.md)-[0004](0004-grpcclient-package.md). Контракт зафиксирован compile-time проверкой `var _ Provider = (*provider)(nil)`. Подключение в сервисах - через существующие точки перехвата: `grpcserver.WithGRPCOptions(grpc.StatsHandler(...))`, `grpcclient.WithGRPCOptions(grpc.WithStatsHandler(...))`, `httpserver.WithHandler(...)`.

### 2. `ParentBased(TraceIDRatioBased(ratio))` и дефолт 1.0

Дока `TraceIDRatioBased` прямо предписывает композицию: «To respect the parent trace's `SampledFlag`, the `TraceIDRatioBased` sampler should be used as a delegate of a `Parent` sampler». Дефолты `ParentBased` делегируют решение родителю (remote sampled → AlwaysSample, remote not sampled → Drop), поэтому ratio управляет только корневыми спанами, а решение шлюза, доставленное заголовком `traceparent`, действует до конца цепочки. Дефолтный ratio 1.0 - всё пишется, под dev-LGTM это бесплатно; прод снижает опцией. Перенос между хопами - `propagation.TraceContext{}`, переданный в stats-хендлеры otelgrpc и в `otelhttp.NewHandler`.

### 3. Ресурс: `Merge(Default(), NewSchemaless(...))` и semconv v1.26.0

`resource.Merge(a, b)` даёт приоритет `b` - явные `service.name`/`service.version`/`deployment.environment` затирают `unknown_service:go` из дефолтного ресурса. Наш ресурс строится `NewSchemaless` (пустой schema URL): `resource.Default()` собран на схеме 1.41.0, и пара непустых разных URL дала бы `ErrSchemaURLConflict`; schemaless-аргумент наследует URL дефолта без ошибки. Константы берутся из `go.opentelemetry.io/otel/semconv/v1.26.0` - последней версии с литералом `deployment.environment` (в 1.41.0 атрибут переименован в `deployment.environment.name`).

### 4. Best-effort старт без readiness-проверки

`otlptracegrpc.New` не подключается к коллектору (внутри `grpc.NewClient` в idle) и сам ретраит неудачные экспорты (5s → 30s, потолок 1m). Пакет не повторяет ping-retry цикл [0002](0002-postgresql-package.md)/[0004](0004-grpcclient-package.md): `New` возвращается мгновенно, недоступность LGTM - проблема наблюдаемости, не старта. `Shutdown` флашит буфер `BatchSpanProcessor` синхронно, ограничен `context.WithTimeout(ctx, shutdownTimeout)` (дефолт 10s).

### 5. Пропагатор и провайдер - явно, без otel-глобалов

Пропагатор и провайдер передаются в хендлеры опциями `WithPropagators`/`WithTracerProvider`. Зеркало решения [0001](0001-logger-package.md) «пакет никогда не зовёт `slog.SetDefault`»: сервису, которому нужны глобалы для сторонних библиотек, достаточно `otel.SetTracerProvider(p.Unwrap())` в main. Побочный выигрыш - изоляция тестов (otel-глобалы process-wide).

### 6. Единственная мутация глобала - `otel.SetErrorHandler` при заданном `WithLogger`

`BatchSpanProcessor` репортит ошибки экспорта только через `otel.Handle(err)`; без хука «ошибки экспорта логируются» невыполнимо. При заданном логгере `New` ставит `otel.SetErrorHandler(otel.ErrorHandlerFunc(...))` с Warn через `sl.Err`. Process-wide, но в процессе один провайдер; без логгера хук не ставится и жизненный цикл молчит.

### 7. Опции с валидацией, обязательные - без дефолтов

`Option func(*provider) error`, ошибка опции проваливает `New` (обёртка `tracing:` на границе). Обязательны: `WithEndpoint` (host:port, `net.SplitHostPort`), ровно один из `WithInsecure`/`WithTLS` (взаимоисключающие, прецедент [0004](0004-grpcclient-package.md)), `WithServiceName`/`WithServiceVersion`/`WithEnvironment` - identity сервиса. Ручки: `WithSamplingRatio` ∈ [0, 1], `WithShutdownTimeout` > 0, `WithHeaders` (metadata на каждый экспорт, напр. `X-Scope-OrgID` мульти-тенантного Tempo; защитная копия `maps.Clone`), `WithLogger` (отклоняет nil).

## Интересные моменты реализации

- Тип хендлера - `stats.Handler` из `google.golang.org/grpc/stats`: типа `grpc.StatsHandler` в grpc v1.83.2 нет, `grpc.StatsHandler(...)` - имя опции-функции.
- У `sdktrace.TracerProvider` нет аксессора `Resource()`, поэтому ресурс проверяется в тестах «по проводам»: самодельный OTLP-коллектор на `go.opentelemetry.io/proto/otlp` (клиент шлёт настоящий proto, строковый кодек как в тестах [0004](0004-grpcclient-package.md) не подошёл бы).
- Регрессии на оба требования пакета: сэмплированный remote-parent записывается и доезжает до коллектора при `WithSamplingRatio(0)`; ресурс несёт ровно три атрибута identity и сохраняет дефолтные (`telemetry.sdk.name`).
- Тест логирования ошибок экспорта возвращает `codes.InvalidArgument` - код нон-ретрайбельный, иначе минутный backoff ретраев экспортёра растянул бы тест.
- Дефолтный глобальный error handler otel печатает через `log.Print` - тесты, записывающие спаны, используют живой фейк-коллектор, а не мёртвый порт, чтобы не ждать экспортного backoff при `Shutdown`.
- Атрибут схемы 1.26.0 едет в ресурсе со schema URL 1.41.0 от `Default()` - ключ для Tempo корректен, формальная чистота схемы страдает; при миграции дашбордов на `deployment.environment.name` меняется константа и импорт semconv.
- `resource.Default()` читает `OTEL_SERVICE_NAME`/`OTEL_RESOURCE_ATTRIBUTES` - явные опции их перебивают (приоритет `b` в `Merge`), но это надо учитывать в окружениях с предустановленными переменными.
- pkg→pkg зависимость на `logger` резолвится через `go.work` без `require` в `go.mod`, `go mod tidy` в пакете не запускается - прецедент [0004](0004-grpcclient-package.md).
- Drop спанов при переполнении очереди BSP (2048 по умолчанию) при долгой недоступности коллектора - принятая цена best-effort.

## Возможные улучшения

1. **`WithPropagators`** - добавить Baggage к TraceContext для переноса бизнес-контекста.
2. **Ручки `WithBatcher`** - размер очереди и batch timeout против drop при переполнении.
3. **OTLP HTTP-экспортёр** - порт 4318 для окружений без gRPC до коллектора.
4. **Метрики** - симметричный пакет на `otel/sdk/metric` с OTLP-экспортом в тот же стек.
5. **`otelgrpc.WithPublicEndpoint`** - для шлюза: внешние клиенты дают link вместо child-спана.
6. **Опция `WithGlobal`** - регистрация otel-глобалов для сторонних библиотек: pgx-трейсер из [0002](0002-postgresql-package.md), `trace_id` в логах из [0001](0001-logger-package.md).
