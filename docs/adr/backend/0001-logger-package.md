# ADR: `backend/pkg/logger`

## Статус
Принят и реализован (сентябрь 2026). Пакет: `github.com/p1xray/sso/backend/pkg/logger`, Go 1.27, **ноль внешних зависимостей**.

## Контекст:
Библиотека структурированного логирования для Go-сервисов поверх `log/slog`.

### Требования:
- Контекстуальность (Tracing & Request ID);
- Глобальные атрибуты (Enrichment);
- Умное форматирование (Prod/Dev);
- Маскирование чувствительных данных (Sanitization);
- Стек-трейсы;
- Динамический уровень;
- DI без глобального состояния.

## Ключевые архитектурные решения

### 1. Цепочка декораторов над `slog.Handler`

Собирается в единственной функции `buildHandlersChain` (`logger.go`). Каждый декоратор живёт в своём подпакете `handlers/<name>`. Порядок важен:

```
slog.Logger → sanitize → ctx → stack → sink (JSON | Pretty)
```

- **sanitize** - единственный, кто видит атрибуты прикладного кода: call-site и `With`-атрибуты.
    - **Ключи**: точное совпадение без учёта регистра по leaf-ключу, ищет рекурсивно в группы на любой глубине. Дефолтный deny-list переопределяется через `SanitizeConfig`: `ExtraKeys` расширяет, `ExceptKeys` исключает.
    - **Значения**: regex-скан строк; отключается с помощью `DisableValueScan`. Замаскированное значение - `[REDACTED]` независимо от типа.
    - `With`-атрибуты валидируются один раз при присоединении, а не на каждой записи.
- **ctx** - добавляет атрибуты из контекста в каждую запись. Атрибуты для добавления определяются с помощью переданных методов в конфигурацию `Config.ContextAttrs`. По-дефолту добавляет только `request_id` с помощью метода  `ExtractRequestID`, не-nil → **ровно** список вызывающего (замена, не слияние). Хелперы `WithRequestID`/`RequestIDFromContext` - задел под middleware/интерсепторы.
- **stack** - работает только с уровнем записи с `rec.Level` ≥ порога (дефолт ERROR, настраивается `Config.StackTraceLevel`, выкл - `StackTraceDisabled`). Клонирует запись с атрибутом `stack_trace` (`debug.Stack()`). Хендлер синхронный - стек захватывается в месте логирования, а не в месте создания ошибки.
- **sink** - форматирование и вывод данных. `slog.JSONHandler` (dev/prod) либо pretty-хендлер (консоль, см. `Format`). Атрибуты сериализует внутренний `slog.JSONHandler` (полная верность типов, групп и `AddSource`) с последующим `json.Indent` и раскраской через стратегию `color.Colorizer`. Структура иммутабельна - `With*` возвращают клоны с общими mutex и буфером, одна запись - один `Write`.
- **Будущий семплер** встаёт между ctx и stack: контекст ещё доступен, дорогой захват стека ещё не случился - просто новое поле в `Config`, без изменения API.

### 2. Запечатанный API: интерфейс `Logger` + неэкспортируемая реализация

```go
type Logger interface {
    Debug(ctx context.Context, msg string, args ...any)
    Info(ctx context.Context, msg string, args ...any)
    Warn(ctx context.Context, msg string, args ...any)
    Error(ctx context.Context, msg string, args ...any)
    Enabled(ctx context.Context, level slog.Level) bool
    With(args ...any) Logger
    WithGroup(name string) Logger
    SetLevel(slog.Level)
}

type logger struct { inner *slog.Logger; level *slog.LevelVar }
func New(cfg Config) (*logger, error)
```

- **Только ctx-методы**: суффикс `Context` в stdlib различает парные методы - у нас пары нет, имя `Info(ctx, ...)` честно.
- **`With`/`WithGroup` возвращают `Logger`**: дети делят родительский `*slog.LevelVar`.
- **`LevelVar()` и `Unwrap()` - вне интерфейса**, методы конкретного типа: escape-hatch для `slog.SetDefault(lg.Unwrap())` и библиотек, принимающих `*slog.Logger`, - дело `main`-обвязки, не прикладного кода.
- Сопутствующий подпакет `sl` - сахар для атрибутов: `sl.Err(err)`, `sl.Strings(key, values)`.
- **Граница дисциплины** (зафиксирована честно): контракт действует там, где `Logger` объявлен типом параметра; `slog.Default()` и сторонние библиотеки - вне его.

### 3. Создание и внешняя конфигурация

`New(Config)` - единственная точка создания. Пакет не читает env, не держит глобального состояния и сам не вызывает `slog.SetDefault`: логгер конфигурируется целиком извне, из конфигурационного слоя приложения.

### 4. Конфигурация через `Config`

Одна структура со всеми полями; `resolve()` валидирует и применяет дефолты:

- **Identity**: `Service` (обязательно), `Env` (обязательно), опциональные `Version`/`Instance` → базовые атрибуты каждой записи через `With` (рендерятся до атрибутов записи).
- **Вывод**: `Writer` (дефолт `os.Stdout`), `Format`, `NoColor`.
- **Уровень**: `Level` строкой, `AddSource *bool`.
- **Диагностика**: `StackTraceLevel`, `Sanitize SanitizeConfig`, `ContextAttrs`.

Примитивы - типизированные строки с конструкторами-валидаторами: `Env` (local/dev/prod), `Format` (auto/json/console), `Level` (понимает `warn+2`, `-4` и числа по шкале slog). Дефолты зависят только от `Env` - других профилей нет.

### 5. Динамический уровень через `SetLevel`

Уровень живёт в одном `*slog.LevelVar` в синке; `Enabled` всех слоёв цепочки делегируется к нему, поэтому `SetLevel` меняет порог мгновенно - для самого логгера и всех производных из `With`/`WithGroup`.

## Интересные моменты реализации

- Pretty-рендер строится поверх внутреннего `slog.JSONHandler` + `json.Indent` - никакой ручной сериализации значений; блок атрибутов опускается, когда атрибутов нет.
- `rec.PC` сохраняется при пересборке записи (sanitize, ctx), иначе `AddSource` указал бы в пакет логгера.
- ctx-атрибуты инжектятся до call-site атрибутов (пересборкой записи) - `request_id` стоит рядом с identity-атрибутами `service`/`env`.
- Шкала slog: `Debug=-4, Info=0, Warn=4, Error=8`. Свой мягкий парсер уровней (`warn+2`), т.к. `slog.Level.UnmarshalText` требует строгих имён.
- Нулевой `StackTraceLevel` трактуется как «не задано» (дефолт ERROR), выключение через `StackTraceDisabled = math.MaxInt32`.

## Возможные улучшения

1. **Сэмплирование** - декоратор (порог N записей/сек на уровень, WARN+ не семплируется), слот между ctx и stack.
2. **OTel trace_id/span_id** - экстрактор через `Config.ContextAttrs`, без правок пакета.
3. **Прокидывание request_id между сервисами** - HTTP-middleware / gRPC-интерсепторы: генерация и передача через заголовки/metadata.
4. **Захват стека в месте создания ошибки** (`pkg/errs`) - богаче диагностика, ценой миграции с `fmt.Errorf`.
5. **Бенчмарки** - первый кандидат `BenchmarkSanitize` (regex на каждой записи).
