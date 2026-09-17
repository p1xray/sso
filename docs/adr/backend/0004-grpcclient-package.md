# ADR: `backend/pkg/grpcclient`

## Статус

Принят и реализован (сентябрь 2026). Пакет: `github.com/p1xray/sso/backend/pkg/grpcclient`, Go 1.27, внешняя зависимость: `google.golang.org/grpc`.

## Контекст:

Единая обёртка над `google.golang.org/grpc` для gRPC-клиентов монорепо. Первичный потребитель - `backend/cmd/gateway`: HTTP-шлюз вызывает `auth`/`oidc`/`profile` через генерируемые клиенты, конструкторы которых принимают `grpc.ClientConnInterface`, так что обёртка обязана отдавать совместимое значение.

### Требования:

- Сервис не стартует, пока зависимость недоступна: readiness-проверка коннекта с retry/backoff/jitter, симметрично пулу `postgresql` (см. [0002](0002-postgresql-package.md));
- Явный выбор транспорта безопасности (insecure/TLS) - никаких неявных дефолтов;
- Точки перехвата grpc-опций (TLS, keepalive, лимиты, интерцепторы) без открытия всего `*grpc.ClientConn` в прикладной код;
- DI без глобального состояния, конвенции `grpcserver` (см. [0003](0003-grpcserver-package.md)).

### Альтернативы:

- Голый `grpc.NewClient` в каждом сервисе - readiness-retry и явная безопасность дублируются в каждом месте диала;
- Deprecated `grpc.Dial`/`grpc.DialContext` с `WithBlock` - заброшенный API и другой дефолтный резолвер (passthrough вместо dns);
- Готовая обёртка - лишняя зависимость, API не под конвенции репо.

### Non-goals:

Интерцепторы (логирование, recovery, auth), retry-политики RPC, менеджмент TLS-сертификатов, health-проба - вне пакета: интерцепторы и retry-политики подключаются через `WithGRPCOptions`, остальное принимается отдельными решениями.

## Ключевые архитектурные решения

### 1. Запечатанный интерфейс `Client`, встраивающий `grpc.ClientConnInterface`

`*grpc.ClientConn` приватен, наружу - интерфейс `Client`: встроенный `grpc.ClientConnInterface` (`Invoke`/`NewStream` - генерируемые конструкторы `NewXServiceClient` принимают значение напрямую, без `Unwrap`) плюс `Close` и `Unwrap` - escape-hatch, симметрично `logger`/`postgresql`/`grpcserver` (см. [0001](0001-logger-package.md), [0002](0002-postgresql-package.md), [0003](0003-grpcserver-package.md)). Контракт зафиксирован compile-time проверкой `var _ Client = (*client)(nil)`.

### 2. `grpc.NewClient` вместо `Dial` и явный выбор безопасности

`NewClient` не делает I/O и возвращает канал в IDLE; дефолтный резолвер - dns, target без схемы (`host:port`) уходит в fallback `dns:///`, глубокая валидация синтаксиса остаётся за grpc (невалидный target - синхронная ошибка `NewClient`). Сам grpc не подключается вовсе без credentials (`no transport security set`), но пакет проверяет выбор раньше и со своим сообщением: `WithInsecure`/`WithTLS` взаимоисключающие, дефолта нет - `New` без выбора не проходит. Креденшел пакета идёт первой диал-опцией, так что пользовательский через `WithGRPCOptions` перебивает (прецедент `ConnectionTimeout` в `grpcserver`).

### 3. Connect-with-retry - зеркало `pingWithRetry` из `postgresql`

В grpc v1.83 `Connect()` без контекста и неблокирующий, готового «подождать READY» нет, поэтому пакет повторяет цикл блокирующего dial: `GetState` → `Connect()` на Idle → `WaitForStateChange` до Ready или истечения попытки. Внешний цикл - структурная копия `pingWithRetry`: N попыток (10), потолок каждой попытки и задержки `maxBackoff` (10s), задержка от `connectAttemptTimeout` (1s) с удвоением и jitter `[0.5, 1)`, отменяемый sleep через select, warn о неудачной попытке, провал → `Close` соединения + `grpcclient: connect after N attempts: ...`. `ResetConnectBackoff` в начале каждой попытки - каденс ретраев определяет пакет, а не внутренний backoff канала (база 1s, множитель 1.6).

### 4. Опции с валидацией, With-префикс

`Option func(*client) error`, ошибка опции проваливает `New`. `WithTarget` - обязателен, проверяется на непустоту: пустой target grpc молча превратил бы в `dns:///` и упал только при разрешении. `WithInsecure`/`WithTLS` - выбор безопасности; конфликт отвергается, прежнее значение сохраняется. `WithConnectAttempts`/`WithConnectAttemptTimeout`/`WithMaxBackoff` - ручки ретраев, положительные. `WithGRPCOptions` - проход `grpc.DialOption`. `WithLogger` - отклоняет nil. Ошибки оборачиваются префиксом `grpcclient:`. Отступление от безпрефиксных имён `postgresql` - якорь на конвенции `grpcserver`.

### 5. Логирование жизненного цикла

Опционально через `logger.Logger`: подключение - info с атрибутом `target`, неудачная попытка - warn с `attempt`/`total_attempts` и ошибкой через `sl.Err`. Логгер не задан (по умолчанию) - жизненный цикл молчит; зависимость - только на интерфейс, не на реализацию (см. [0001](0001-logger-package.md)).

## Интересные моменты реализации

- `Connect`/`ResetConnectBackoff` - experimental API grpc: если будущий апгрейд даст блокирующий `Connect(ctx)`, внутренний цикл ожидания свернётся в один вызов.
- Повторный `Close` канала возвращает deprecated `ErrClientConnClosing` - наш `Close` просто делегирует; контракт - «первый вызов nil».
- Тесты используют literal `127.0.0.1`, а не `localhost`: dns-резолвер по умолчанию, `localhost` может резолвиться в `::1`, а тестовый сервер слушает на IPv4.
- Тесты: e2e Invoke через самодельный `grpc.ServiceDesc` и строковый кодек (без proto-зависимости), значение передаётся в функцию с параметром `grpc.ClientConnInterface` - контракт генерируемых клиентов; задержанный старт сервера (порт занят сразу, Serve - через 300ms) прогоняет ретраи с логами; исчерпание попыток, отмена ctx, невалидный target, семантика Close, логирование. Всё stdlib-only под `-race`.
- pkg→pkg зависимость на `logger` резолвится через `go.work` без `require` в `go.mod` - модуль ещё не опубликован, прецедент `postgresql`/`grpcserver`. `go mod tidy` в пакете не запускается: он попытался бы разрешить непубликованный модуль и разрушил бы требуемую форму go.mod.

## Возможные улучшения

1. **Интерцепторы** - logging/recovery-цепочки отдельными pkg, подключение через `WithGRPCOptions`.
2. **Retry-политики RPC** - service config с retryPolicy для унарных вызовов.
3. **Метрики** - состояние канала и счётчики через `stats.Handler`/channelz.
4. **Health-проба** - `grpc_health_v1` как критерий readiness после коннекта.
5. **TLS-обвязка** - загрузка CA, mTLS, ротация сертификатов.
