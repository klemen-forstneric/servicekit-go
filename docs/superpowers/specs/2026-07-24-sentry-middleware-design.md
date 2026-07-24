# Sentry middleware for spark and ember — design

Date: 2026-07-24

## Goal

Report handler errors and panics from the spark command bus and the ember
subscription pipeline to Sentry, enriched with correlation and dispatch/event
metadata. Errors and panics only — no performance tracing in this pass (see
Non-goals).

## Context

servicekit wraps two in-house buses, both with the same middleware shape (an
inner call that returns `(…, error)`):

- **spark** — `Middleware = func(spark.Next) spark.Next`,
  `Next(ctx, cmd) (any, error)`, `cmd.Type() string`. Already ships
  `spark/middleware.Recover()` (panic → error).
- **ember** — `SubscriptionMiddleware = func(ember.HandleFunc) ember.HandleFunc`,
  `HandleFunc(ctx, *ReceivedEvent) error`. Event carries `ID`, `Type()`,
  `EntityID()`, `Metadata`. ember has **no** panic guard today — a handler
  panic kills the worker goroutine.

Cross-cutting middleware lives in per-framework `*x` packages that import their
framework's concrete deps directly (`fiberx` imports gofiber, `emberx` imports
ember/spark). Correlation id lives on the context via
`correlation.FromContext(ctx)` and is stamped upstream (fiberx / ember
`CorrelationID` middleware).

## Decisions

- **Scope**: errors + panics only. Designed to leave room for tracing later.
- **SDK**: depend on `github.com/getsentry/sentry-go` directly. The service owns
  `sentry.Init` and `Flush` lifecycle; middleware only reports.
- **Placement**: per-framework. `sparkx.Sentry()` in a new `sparkx` package;
  `emberx.Sentry()` and `emberx.Recover()` in the existing `emberx` package.
- **Shared glue**: duplicated per package (a `correlationTag` lookup and a
  panic-message helper — ~5 lines each). No shared/internal package, to keep the
  packages self-contained.
- **Panic path**: repanic pattern (idiomatic sentry-go). Recovery *control flow*
  is owned by a Recover middleware; the Sentry middleware only transiently
  recovers to capture a live stacktrace, then re-panics. It never swallows.

## Components

### 1. `sparkx.Sentry() spark.Middleware` (new package `sparkx`)

```go
func Sentry() spark.Middleware {
    return func(next spark.Next) spark.Next {
        return func(ctx context.Context, cmd spark.Command) (result any, err error) {
            hub := hubFrom(ctx) // sentry.GetHubFromContext(ctx) or sentry.CurrentHub()
            defer func() {
                if r := recover(); r != nil {
                    hub.WithScope(func(s *sentry.Scope) {
                        enrich(s, ctx, cmd.Type())
                        hub.RecoverWithContext(ctx, r) // capture with live stacktrace
                    })
                    panic(r) // re-panic; outer Recover converts to error
                }
            }()
            result, err = next(ctx, cmd)
            if err != nil {
                hub.WithScope(func(s *sentry.Scope) {
                    enrich(s, ctx, cmd.Type())
                    hub.CaptureException(err)
                })
            }
            return
        }
    }
}
```

- Uses the ctx hub via `hub.WithScope` (temporary scope, no clone, no leak).
  spark dispatch runs inline inside an already-scoped goroutine, so no new
  concurrency is introduced here.
- Reuses upstream `spark/middleware.Recover()` — placed **outer** of Sentry — to
  turn the re-panic into an error for the caller.
- Recommended chain: `spark.WithMiddleware(Log, Recover, Sentry, …handler)`
  (first = outermost). Correlation id is already on ctx by dispatch time.
- Tags: `spark.command` = `cmd.Type()`, `correlation_id` (when present).

### 2. `emberx.Recover() ember.SubscriptionMiddleware` (new, decoupled)

```go
func Recover() ember.SubscriptionMiddleware {
    return func(next ember.HandleFunc) ember.HandleFunc {
        return func(ctx context.Context, e *ember.ReceivedEvent) (err error) {
            defer func() {
                if r := recover(); r != nil {
                    err = fmt.Errorf("emberx: handler panic for %s (%s): %v\n%s",
                        e.Type(), e.ID, r, debug.Stack())
                }
            }()
            return next(ctx, e)
        }
    }
}
```

- Owns panic control flow: converts a panic into an error, which the subscriber
  turns into a nack (redelivery). Mirrors spark's `Recover` shape.
- Independent of Sentry — usable on any ember chain.

### 3. `emberx.Sentry() ember.SubscriptionMiddleware`

```go
func Sentry() ember.SubscriptionMiddleware {
    return func(next ember.HandleFunc) ember.HandleFunc {
        return func(ctx context.Context, e *ember.ReceivedEvent) error {
            hub := sentry.CurrentHub().Clone() // per-event, concurrency-safe
            enrich(hub.Scope(), ctx, e.Type(), e.ID, e.EntityID())
            ctx = sentry.SetHubOnContext(ctx, hub)
            defer func() {
                if r := recover(); r != nil {
                    hub.RecoverWithContext(ctx, r) // capture with live stacktrace
                    panic(r)                       // re-panic; outer Recover handles it
                }
            }()
            if err := next(ctx, e); err != nil {
                hub.CaptureException(err)
                return err
            }
            return nil
        }
    }
}
```

- **Clones a hub per event** and sets it on ctx. `StickyEntityConsumer` fans out
  N concurrent workers sharing one ctx; a shared hub's scope stack is not safe
  under concurrent use, so each event gets its own hub. A spark dispatch inside
  the handler then finds *this* event's hub via `GetHubFromContext` and enriches
  it — isolated per event.
- Tags: `ember.event_type`, `ember.event_id`, `ember.entity_id`,
  `correlation_id` (when present).
- Recommended chain (first = outermost):
  `[CorrelationID, Recover, Sentry, IdempotencyKey, …handler]`.

## Data / control flow

Chain outer→inner: `… , Recover, Sentry, handler`.

- **Handler returns error** → Sentry `CaptureException` (once) → returns error →
  Recover passes it through → spark caller gets error / ember subscriber nacks.
- **Handler panics** → Sentry transiently recovers, `RecoverWithContext`
  captures with live stack, re-panics → Recover catches, converts to error → same
  downstream as above. No double-capture: Recover (outer) creates the synthesized
  error *above* Sentry, so Sentry's error branch never sees it.

## Error handling

- Capture happens on error/panic only; successful dispatch/consume is silent.
- Default Sentry level (Error) for `CaptureException`; `RecoverWithContext`
  captures the panic as a Sentry exception with parsed frames.
- Missing correlation id → tag omitted, no error.
- No hub on ctx and no global hub configured → sentry-go's `CurrentHub()` is
  always non-nil (no-op client until `sentry.Init`), so calls are safe no-ops.

## Non-goals (this pass)

- Performance tracing (transactions / spans).
- Distributed trace-context propagation on publish / HTTP-client edges.
- A `sentryx.Fiber()` HTTP middleware — the official `sentryfiber` contrib
  already covers inbound HTTP.

Tracing is deliberately deferred: at the leaves it yields only in-process spans,
and true distributed traces require publish-side + HTTP-client propagation — a
larger coordinated effort. The middleware shape above leaves room to add a
tracing layer later (transaction root at ember consume, child span at spark).

## Testing

Per package, using `testify/suite` and a fake `sentry.Transport` bound to a
client/hub — asserts our logic, not the SDK:

- Handler returns error → exactly one event captured, carrying the expected tags.
- Handler succeeds → zero events captured.
- Handler panics → event captured with a stacktrace, and the panic propagates
  (spark: re-panic reaches the test's outer Recover / recover; ember: same, plus
  `emberx.Recover()` converts it to an error).
- `emberx.Recover()` alone → panic becomes an error, no Sentry dependency.

Fake transport captures `[]*sentry.Event` for assertion. Hub/client bound via
`sentry.NewClient` + `sentry.NewHub`, set on ctx (spark) or as current hub
(ember, which clones it).

## go.mod

Adds `github.com/getsentry/sentry-go`.
