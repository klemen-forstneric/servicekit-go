# servicekit-go httpx for net/http — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `servicekit-go/httpx` into the `net/http` package — envelope, middleware, request logging with redaction, panic recovery, trusted-proxy client IP — and move the existing fiber helpers into `fiberx`.

**Architecture:** `fiberx` becomes the single package holding everything fiber, so it can be deleted wholesale when the fleet migrates. `httpx` is rewritten from scratch against `net/http` with no fiber dependency. `fiberx` imports `httpx` for the two shared header constants; nothing imports in the other direction.

**Tech Stack:** Go 1.26, `net/http`, `github.com/google/uuid`, `github.com/klemen-forstneric/ember`, `github.com/klemen-forstneric/spark`, testify.

## Global Constraints

- Module is `github.com/klemen-forstneric/servicekit-go`, Go 1.26.3.
- No new module dependencies. `uuid`, `ember`, `spark`, and `testify` are already required; `httpx` must not import `github.com/gofiber/fiber/v2`.
- Tests are `package <name>_test`, plain `testify/assert` and `testify/require`, matching every existing test in this repo. Do not introduce `suite`.
- The JSON envelope is exactly `{"data":…,"error":…}` with `error` null on success — byte-identical to what `fiberx` emits, because both wire formats must stay interchangeable during the migration.
- Header constant values: `Correlation-ID` and `Idempotency-Key`.
- Correlation ids are UUIDs (`uuid.NewString()`), matching `fiberx.CorrelationID`.
- Comments are terse one-liners only where the code is genuinely non-obvious. No paragraph rationale blocks.
- After every task: `go build ./... && go test ./... && go vet ./...` must pass.

---

### Task 1: Move the fiber envelope into fiberx

`httpx` currently holds four fiber functions. They move to `fiberx` so `httpx` is free to be rewritten. The old `httpx` package is deleted in this task and recreated in Task 2 — between the two tasks the module has no `httpx` package, which is fine because nothing inside this repo imports it.

Consumers pin a module version, so deleting the package does not break the eight fiber services until they bump — that is a separate plan.

**Files:**
- Modify: `fiberx/fiberx.go` (append the four moved functions)
- Modify: `fiberx/fiberx_test.go` (append the moved tests)
- Delete: `httpx/httpx.go`
- Delete: `httpx/httpx_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `fiberx.JSON(c *fiber.Ctx, status int, data interface{}) error`, `fiberx.EmptyJSON(c *fiber.Ctx, status int) error`, `fiberx.Error(c *fiber.Ctx, status int, err error) error`, `fiberx.SetupHealthRoutes(a *fiber.App)`.

- [ ] **Step 1: Append the four moved functions to `fiberx/fiberx.go`**

Add at the end of the file. `fiber` is already imported.

```go
func JSON(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(fiber.Map{
		"data":  data,
		"error": nil,
	})
}

func EmptyJSON(c *fiber.Ctx, status int) error {
	return JSON(c, status, nil)
}

func Error(c *fiber.Ctx, status int, err error) error {
	return c.Status(status).JSON(fiber.Map{
		"data":  nil,
		"error": err.Error(),
	})
}

func SetupHealthRoutes(a *fiber.App) {
	health := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	}

	a.Get("/", health)
	a.Get("/health", health)
}
```

- [ ] **Step 2: Append the moved tests to `fiberx/fiberx_test.go`**

These are the three tests from `httpx/httpx_test.go` repointed at `fiberx`, plus one for `EmptyJSON`, which the old file never covered. Add them at the end. `errors` and `io` are new imports; `fiber`, `assert`, `require`, `httptest` and `testing` are already there.

```go
func TestJSONEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return fiberx.JSON(c, fiber.StatusOK, fiber.Map{"x": 1}) })
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"data":{"x":1},"error":null}`, string(body))
}

func TestErrorEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return fiberx.Error(c, fiber.StatusBadRequest, errors.New("boom")) })
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"data":null,"error":"boom"}`, string(body))
}

func TestEmptyJSONEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return fiberx.EmptyJSON(c, fiber.StatusAccepted) })
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.Equal(t, 202, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"data":null,"error":null}`, string(body))
}

func TestHealthRoutes(t *testing.T) {
	app := fiber.New()
	fiberx.SetupHealthRoutes(app)
	for _, path := range []string{"/", "/health"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `{"status":"ok"}`, string(body))
	}
}
```

- [ ] **Step 3: Delete the old httpx package**

```bash
rm httpx/httpx.go httpx/httpx_test.go
rmdir httpx
```

- [ ] **Step 4: Verify**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS. The four `fiberx` envelope tests run alongside the existing `fiberx` middleware tests.

- [ ] **Step 5: Commit**

```bash
git add fiberx/fiberx.go fiberx/fiberx_test.go
git add -A httpx
git commit -m "refactor(fiberx): absorb the JSON envelope and health routes from httpx

httpx is about to become the net/http package. Everything fiber now lives
in fiberx, which is the package that gets deleted when the fleet finishes
migrating off fiber."
```

---

### Task 2: httpx envelope and health for net/http

**Files:**
- Create: `httpx/httpx.go`
- Create: `httpx/httpx_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `httpx.CorrelationIDHeader` = `"Correlation-ID"`, `httpx.IdempotencyKeyHeader` = `"Idempotency-Key"`
  - `httpx.JSON(w http.ResponseWriter, status int, data any)`
  - `httpx.EmptyJSON(w http.ResponseWriter, status int)`
  - `httpx.Error(w http.ResponseWriter, status int, err error)`
  - `httpx.NotFound(w http.ResponseWriter, r *http.Request)`
  - `httpx.MethodNotAllowed(w http.ResponseWriter, r *http.Request)`
  - `httpx.SetupHealthRoutes(mux *http.ServeMux)`
  - `httpx.ErrNotFound`, `httpx.ErrMethodNotAllowed`, `httpx.ErrInternal`

- [ ] **Step 1: Write the failing test**

Create `httpx/httpx_test.go`:

```go
package httpx_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

func TestJSONEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.JSON(w, http.StatusOK, map[string]int{"x": 1})

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"data":{"x":1},"error":null}`, w.Body.String())
}

func TestEmptyJSONEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.EmptyJSON(w, http.StatusAccepted)

	assert.Equal(t, 202, w.Code)
	assert.JSONEq(t, `{"data":null,"error":null}`, w.Body.String())
}

func TestErrorEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.Error(w, http.StatusBadRequest, errors.New("boom"))

	assert.Equal(t, 400, w.Code)
	assert.JSONEq(t, `{"data":null,"error":"boom"}`, w.Body.String())
}

func TestNotFoundEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.NotFound(w, httptest.NewRequest("GET", "/nope", nil))

	assert.Equal(t, 404, w.Code)
	assert.JSONEq(t, `{"data":null,"error":"not found"}`, w.Body.String())
}

func TestMethodNotAllowedEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.MethodNotAllowed(w, httptest.NewRequest("POST", "/x", nil))

	assert.Equal(t, 405, w.Code)
	assert.JSONEq(t, `{"data":null,"error":"method not allowed"}`, w.Body.String())
}

func TestHealthRoutes(t *testing.T) {
	mux := http.NewServeMux()
	httpx.SetupHealthRoutes(mux)

	for _, path := range []string{"/", "/health"} {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", path, nil))

		require.Equal(t, 200, resp.Code, path)
		body, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `{"status":"ok"}`, string(body))
	}
}

// The health handler is registered for the exact root, not as a catch-all, so
// an unregistered path still falls through to the mux's own 404.
func TestHealthRootIsExactNotCatchAll(t *testing.T) {
	mux := http.NewServeMux()
	httpx.SetupHealthRoutes(mux)

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest("GET", "/something-else", nil))

	assert.Equal(t, 404, resp.Code)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./httpx/...`
Expected: FAIL — the `httpx` package does not exist.

- [ ] **Step 3: Write the implementation**

Create `httpx/httpx.go`:

```go
// Package httpx holds cross-cutting net/http helpers: the response envelope,
// middleware, and trusted-proxy client IP resolution.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

const (
	CorrelationIDHeader  = "Correlation-ID"
	IdempotencyKeyHeader = "Idempotency-Key"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrMethodNotAllowed = errors.New("method not allowed")
	ErrInternal         = errors.New("internal server error")
)

type envelope struct {
	Data  any     `json:"data"`
	Error *string `json:"error"`
}

func write(w http.ResponseWriter, status int, data any, errMsg *string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data, Error: errMsg})
}

func JSON(w http.ResponseWriter, status int, data any) { write(w, status, data, nil) }

func EmptyJSON(w http.ResponseWriter, status int) { write(w, status, nil, nil) }

func Error(w http.ResponseWriter, status int, err error) {
	msg := err.Error()
	write(w, status, nil, &msg)
}

func NotFound(w http.ResponseWriter, _ *http.Request) {
	Error(w, http.StatusNotFound, ErrNotFound)
}

func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	Error(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed)
}

func SetupHealthRoutes(mux *http.ServeMux) {
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}

	// "/{$}" matches the root exactly; a bare "/" would swallow every path.
	mux.HandleFunc("GET /{$}", health)
	mux.HandleFunc("GET /health", health)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./httpx/... -v`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add httpx/httpx.go httpx/httpx_test.go
git commit -m "feat(httpx): net/http response envelope and health routes

Same {data,error} wire format fiberx emits, so both are interchangeable
while the fleet migrates."
```

---

### Task 3: httpx middleware — Chain, CorrelationID, IdempotencyKey, Recover

`fiberx` also re-points its two header constants at `httpx` in this task, so the values are declared once. The `fiberx` constants keep their names and values, so no `fiberx` caller changes.

**Files:**
- Create: `httpx/middleware.go`
- Create: `httpx/middleware_test.go`
- Modify: `fiberx/fiberx.go` (the `const` block only)

**Interfaces:**
- Consumes: `httpx.Error`, `httpx.ErrInternal`, `httpx.CorrelationIDHeader`, `httpx.IdempotencyKeyHeader` from Task 2.
- Produces:
  - `type httpx.Middleware func(http.Handler) http.Handler`
  - `httpx.Chain(h http.Handler, ms ...Middleware) http.Handler`
  - `httpx.CorrelationID() Middleware`
  - `httpx.IdempotencyKey() Middleware`
  - `httpx.Recover(l ember.LoggerCtx) Middleware`

- [ ] **Step 1: Write the failing test**

Create `httpx/middleware_test.go`:

```go
package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klemen-forstneric/ember/correlation"
	"github.com/klemen-forstneric/spark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

func TestChainRunsFirstListedOutermost(t *testing.T) {
	var order []string
	mw := func(name string) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := httpx.Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		mw("first"), mw("second"),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, []string{"first", "second", "handler"}, order)
}

func TestCorrelationIDGeneratesAndEchoes(t *testing.T) {
	var fromCtx string
	h := httpx.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromCtx, _ = correlation.FromContext(r.Context())
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	echoed := w.Header().Get(httpx.CorrelationIDHeader)
	assert.NotEmpty(t, echoed)
	assert.Equal(t, echoed, fromCtx)
	assert.Len(t, echoed, 36) // uuid, matching fiberx
}

func TestCorrelationIDPreservesIncoming(t *testing.T) {
	var fromCtx string
	h := httpx.CorrelationID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromCtx, _ = correlation.FromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(httpx.CorrelationIDHeader, "corr-9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, "corr-9", w.Header().Get(httpx.CorrelationIDHeader))
	assert.Equal(t, "corr-9", fromCtx)
}

// The id is set on the request headers too, so a reverse proxy forwards the
// normalized value rather than the client's absence of one.
func TestCorrelationIDSetsRequestHeader(t *testing.T) {
	var forwarded string
	h := httpx.CorrelationID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get(httpx.CorrelationIDHeader)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	assert.NotEmpty(t, forwarded)
}

func TestIdempotencyKeyPresentAndAbsent(t *testing.T) {
	var got string
	var ok bool
	h := httpx.IdempotencyKey()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = spark.IdempotencyKeyFromContext(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(httpx.IdempotencyKeyHeader, "key-1")
	h.ServeHTTP(httptest.NewRecorder(), req)
	assert.True(t, ok)
	assert.Equal(t, "key-1", got)

	got, ok = "", false
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	assert.False(t, ok)
	assert.Empty(t, got)
}

type errLogger struct{ errs int }

func (l *errLogger) Debug(context.Context, string, ...any)        {}
func (l *errLogger) Info(context.Context, string, ...any)         {}
func (l *errLogger) Warn(context.Context, string, ...any)         {}
func (l *errLogger) Error(context.Context, string, error, ...any) { l.errs++ }

func TestRecoverTurnsPanicIntoEnvelope(t *testing.T) {
	l := &errLogger{}
	h := httpx.Recover(l)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	})

	assert.Equal(t, 500, w.Code)
	assert.JSONEq(t, `{"data":null,"error":"internal server error"}`, w.Body.String())
	assert.Equal(t, 1, l.errs)
}

func TestRecoverPassesThroughWhenNoPanic(t *testing.T) {
	l := &errLogger{}
	h := httpx.Recover(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, "fine")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 0, l.errs)
}

func TestRecoverAcceptsNilLogger(t *testing.T) {
	h := httpx.Recover(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	})
	assert.Equal(t, 500, w.Code)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./httpx/...`
Expected: FAIL — `undefined: httpx.Middleware`, `undefined: httpx.Chain`, and the rest.

- [ ] **Step 3: Write the implementation**

Create `httpx/middleware.go`:

```go
package httpx

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/google/uuid"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/correlation"
	"github.com/klemen-forstneric/spark"
)

type Middleware func(http.Handler) http.Handler

// Chain applies ms so the first listed runs outermost.
func Chain(h http.Handler, ms ...Middleware) http.Handler {
	for i := len(ms) - 1; i >= 0; i-- {
		h = ms[i](h)
	}
	return h
}

// CorrelationID ensures every request carries a correlation id — taken from the
// Correlation-ID header or generated — echoes it on the response, sets it on the
// request so proxied calls forward the normalized value, and stores it where
// correlation.FromContext can read it.
func CorrelationID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(CorrelationIDHeader)
			if id == "" {
				id = uuid.NewString()
			}

			w.Header().Set(CorrelationIDHeader, id)
			r.Header.Set(CorrelationIDHeader, id)

			next.ServeHTTP(w, r.WithContext(correlation.NewContext(r.Context(), id)))
		})
	}
}

// IdempotencyKey lifts a client Idempotency-Key header onto the request context
// so dispatched commands inherit it as the spark idempotency key. Absent header
// means no key, and the command bypasses idempotency.
func IdempotencyKey() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get(IdempotencyKeyHeader); v != "" {
				r = r.WithContext(spark.WithIdempotencyKey(r.Context(), v))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recover converts a panic into a 500 envelope. If the handler already wrote a
// status, the envelope write is a no-op and net/http logs a superfluous
// WriteHeader warning — the connection is already committed at that point.
func Recover(l ember.LoggerCtx) Middleware {
	if l == nil {
		l = ember.NopLogger
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				l.Error(r.Context(), "Panic serving HTTP request", fmt.Errorf("%v", v),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				Error(w, http.StatusInternalServerError, ErrInternal)
			}()

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./httpx/... -v`
Expected: PASS.

- [ ] **Step 5: Re-point the fiberx header constants**

In `fiberx/fiberx.go`, replace the `const` block:

```go
const (
	CorrelationIDHeader  = "Correlation-ID"
	IdempotencyKeyHeader = "Idempotency-Key"
)
```

with:

```go
const (
	CorrelationIDHeader  = httpx.CorrelationIDHeader
	IdempotencyKeyHeader = httpx.IdempotencyKeyHeader
)
```

and add `"github.com/klemen-forstneric/servicekit-go/httpx"` to the import block. `httpx` has no fiber dependency, so there is no cycle.

- [ ] **Step 6: Verify the whole module**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS. The existing `fiberx` correlation and idempotency tests still pass unchanged, proving the constant values did not shift.

- [ ] **Step 7: Commit**

```bash
git add httpx/middleware.go httpx/middleware_test.go fiberx/fiberx.go
git commit -m "feat(httpx): Chain, CorrelationID, IdempotencyKey and Recover for net/http

Header constants are declared once in httpx; fiberx aliases them so its
callers are unaffected."
```

---

### Task 4: httpx Record with redaction

The trap in this task is the `ResponseWriter` wrapper. `ReverseProxy` performs WebSocket upgrades through `http.NewResponseController`, which reaches the underlying writer only if the wrapper exposes `Unwrap() http.ResponseWriter`. Without that method every WebSocket handshake behind this middleware fails.

**Files:**
- Create: `httpx/record.go`
- Create: `httpx/record_test.go`

**Interfaces:**
- Consumes: `httpx.Middleware` from Task 3.
- Produces:
  - `type httpx.RecordConfig struct { SkipPaths, SkipBodyPaths, RedactQuery, RedactHeaders []string; MaxBodyBytes int }`
  - `httpx.Record(l ember.LoggerCtx, cfg RecordConfig) Middleware`

- [ ] **Step 1: Write the failing test**

Create `httpx/record_test.go`:

```go
package httpx_test

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

type line struct {
	msg string
	kvs []any
}

type recLogger struct{ lines []line }

func (l *recLogger) Debug(_ context.Context, msg string, kvs ...any) {}
func (l *recLogger) Info(_ context.Context, msg string, kvs ...any) {
	l.lines = append(l.lines, line{msg: msg, kvs: kvs})
}
func (l *recLogger) Warn(_ context.Context, msg string, kvs ...any)          {}
func (l *recLogger) Error(_ context.Context, _ string, _ error, _ ...any)    {}

func (l *recLogger) value(t *testing.T, idx int, key string) any {
	t.Helper()
	require.Less(t, idx, len(l.lines))
	kvs := l.lines[idx].kvs
	for i := 0; i+1 < len(kvs); i += 2 {
		if kvs[i] == key {
			return kvs[i+1]
		}
	}
	return nil
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, "hi")
	})
}

func TestRecordLogsRequestAndResponse(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	require.Len(t, l.lines, 2)
	assert.Equal(t, "Incoming HTTP request", l.lines[0].msg)
	assert.Equal(t, "Returned HTTP response", l.lines[1].msg)
	assert.Equal(t, 200, l.value(t, 1, "status_code"))
}

func TestRecordSkipsConfiguredPathsEntirely(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipPaths: []string{"/health"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))

	assert.Empty(t, l.lines)
}

// SkipPaths is an exact match, so a longer path with the same prefix is logged.
func TestRecordSkipPathsIsExact(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipPaths: []string{"/health"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))

	assert.Len(t, l.lines, 2)
}

func TestRecordRedactsHeaders(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{RedactHeaders: []string{"Authorization"}})(okHandler())

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	h.ServeHTTP(httptest.NewRecorder(), req)

	headers, ok := l.value(t, 0, "headers").(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "[redacted]", headers["Authorization"])
	assert.NotContains(t, headers["Authorization"], "super-secret")
}

func TestRecordRedactsQueryParams(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{RedactQuery: []string{"token"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ws?token=super-secret&x=1", nil))

	path, _ := l.value(t, 0, "path").(string)
	assert.Contains(t, path, "token=%5Bredacted%5D")
	assert.Contains(t, path, "x=1")
	assert.NotContains(t, path, "super-secret")
}

// SkipBodyPaths is a prefix match: the request is still logged, the body is not.
func TestRecordSkipsBodiesByPrefix(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipBodyPaths: []string{"/auth/"}})(okHandler())

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"password":"hunter2"}`))
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, l.lines, 2)
	assert.Nil(t, l.value(t, 0, "body"))
	assert.Nil(t, l.value(t, 1, "body"))
}

func TestRecordLogsBodyOnOtherPaths(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipBodyPaths: []string{"/auth/"}})(okHandler())

	req := httptest.NewRequest("POST", "/chats", strings.NewReader(`{"title":"hi"}`))
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.NotNil(t, l.value(t, 0, "body"))
}

// The handler must still see the body Record consumed for logging.
func TestRecordRestoresRequestBody(t *testing.T) {
	l := &recLogger{}
	var seen string
	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 32)
		n, _ := r.Body.Read(b)
		seen = string(b[:n])
		httpx.EmptyJSON(w, http.StatusOK)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1}`)))

	assert.Equal(t, `{"a":1}`, seen)
}

func TestRecordTruncatesLargeBodies(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{MaxBodyBytes: 8})(okHandler())

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 100))))

	body, _ := l.value(t, 0, "body").(string)
	assert.Len(t, body, 8)
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// Without Unwrap, http.NewResponseController cannot reach the real writer and
// every WebSocket upgrade behind Record fails.
func TestRecordResponseWriterSupportsHijack(t *testing.T) {
	l := &recLogger{}
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}

	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ws", nil))

	assert.True(t, rec.hijacked)
}

// A 101 response has no body to capture and the connection belongs to the
// upgraded protocol afterwards.
func TestRecordDoesNotCaptureUpgradeBody(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
		_, _ = w.Write([]byte("binary-frame-data"))
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ws", nil))

	require.Len(t, l.lines, 2)
	assert.Equal(t, 101, l.value(t, 1, "status_code"))
	assert.Nil(t, l.value(t, 1, "body"))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./httpx/...`
Expected: FAIL — `undefined: httpx.Record`, `undefined: httpx.RecordConfig`.

- [ ] **Step 3: Write the implementation**

Create `httpx/record.go`:

```go
package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/klemen-forstneric/ember"
)

const (
	logFieldRemoteAddress = "remote_address"
	logFieldHTTPMethod    = "method"
	logFieldPath          = "path"
	logFieldHeaders       = "headers"
	logFieldBody          = "body"
	logFieldStatusCode    = "status_code"
)

const (
	redactedValue       = "[redacted]"
	defaultMaxBodyBytes = 64 << 10
)

// RecordConfig tunes what Record logs. SkipPaths matches exactly; SkipBodyPaths
// matches by prefix.
type RecordConfig struct {
	SkipPaths     []string
	SkipBodyPaths []string
	RedactQuery   []string
	RedactHeaders []string
	MaxBodyBytes  int
}

// Record logs each request and its response.
func Record(l ember.LoggerCtx, cfg RecordConfig) Middleware {
	if l == nil {
		l = ember.NopLogger
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	skip := make(map[string]struct{}, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = struct{}{}
	}
	redactHeader := make(map[string]struct{}, len(cfg.RedactHeaders))
	for _, h := range cfg.RedactHeaders {
		redactHeader[http.CanonicalHeaderKey(h)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			logBody := !hasPrefix(r.URL.Path, cfg.SkipBodyPaths)

			var reqBody any
			if logBody {
				raw := readAndRestore(r, maxBody)
				reqBody = decodeBody(raw)
			}

			l.Info(r.Context(), "Incoming HTTP request",
				logFieldRemoteAddress, r.RemoteAddr,
				logFieldHTTPMethod, r.Method,
				logFieldPath, redactedURL(r, cfg.RedactQuery),
				logFieldHeaders, headers(r, redactHeader),
				logFieldBody, reqBody,
			)

			rec := &recorder{ResponseWriter: w, status: http.StatusOK, capture: logBody, limit: maxBody}
			next.ServeHTTP(rec, r)

			var respBody any
			if logBody && rec.status != http.StatusSwitchingProtocols {
				respBody = decodeBody(rec.body.Bytes())
			}

			l.Info(r.Context(), "Returned HTTP response",
				logFieldStatusCode, rec.status,
				logFieldHTTPMethod, r.Method,
				logFieldPath, redactedURL(r, cfg.RedactQuery),
				logFieldBody, respBody,
			)
		})
	}
}

// recorder captures the status and a bounded prefix of the response body.
// Unwrap is what lets http.NewResponseController reach the real writer, which
// WebSocket upgrades depend on.
type recorder struct {
	http.ResponseWriter
	status  int
	body    bytes.Buffer
	capture bool
	limit   int
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) WriteHeader(code int) {
	r.status = code
	if code == http.StatusSwitchingProtocols {
		r.capture = false
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.capture && r.body.Len() < r.limit {
		r.body.Write(b[:min(len(b), r.limit-r.body.Len())])
	}
	return r.ResponseWriter.Write(b)
}

func readAndRestore(r *http.Request, limit int) []byte {
	if r.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) > limit {
		return raw[:limit]
	}
	return raw
}

func decodeBody(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	return string(raw)
}

func headers(r *http.Request, redact map[string]struct{}) map[string]string {
	out := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if _, ok := redact[k]; ok {
			out[k] = redactedValue
			continue
		}
		out[k] = strings.Join(v, ",")
	}
	return out
}

func redactedURL(r *http.Request, redact []string) string {
	if len(redact) == 0 || r.URL.RawQuery == "" {
		return r.URL.RequestURI()
	}
	u := *r.URL
	q := u.Query()
	for _, key := range redact {
		if q.Has(key) {
			q.Set(key, redactedValue)
		}
	}
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./httpx/... -v -run Record`
Expected: PASS, eleven tests.

- [ ] **Step 5: Commit**

```bash
git add httpx/record.go httpx/record_test.go
git commit -m "feat(httpx): request logging with header, query and body redaction

The fiber version logs full bodies, which pointed at a login route would
write plaintext passwords to stdout. SkipBodyPaths covers that.

The response wrapper implements Unwrap so http.NewResponseController can
reach the real writer — without it every WebSocket upgrade behind this
middleware fails."
```

---

### Task 5: httpx IPResolver

**Files:**
- Create: `httpx/ip.go`
- Create: `httpx/ip_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `httpx.NewIPResolver(cidrs []string) (*IPResolver, error)`
  - `(*httpx.IPResolver).ClientIP(r *http.Request) string`

- [ ] **Step 1: Write the failing test**

Create `httpx/ip_test.go`:

```go
package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

func request(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIPHonoursForwardedForFromTrustedPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestClientIPTakesFirstHopOfChain(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7, 10.1.2.3")))
}

// The whole point: an untrusted caller cannot name its own IP.
func TestClientIPIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "198.51.100.9", res.ClientIP(request("198.51.100.9:5555", "203.0.113.7")))
}

func TestClientIPEmptyConfigTrustsNothing(t *testing.T) {
	res, err := httpx.NewIPResolver(nil)
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestClientIPMalformedForwardedForFallsBack(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "not-an-ip")))
}

func TestClientIPAcceptsBareAddressAsTrustedProxy(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"172.18.0.10"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("172.18.0.10:5555", "203.0.113.7")))
	assert.Equal(t, "172.18.0.11", res.ClientIP(request("172.18.0.11:5555", "203.0.113.7")))
}

func TestClientIPHandlesRemoteAddrWithoutPort(t *testing.T) {
	res, err := httpx.NewIPResolver(nil)
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3", "")))
}

func TestClientIPSkipsBlankEntries(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"", " 10.0.0.0/8 ", ""})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestNewIPResolverRejectsGarbage(t *testing.T) {
	_, err := httpx.NewIPResolver([]string{"not-a-cidr"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-cidr")
}

func TestClientIPNilResolverTrustsNothing(t *testing.T) {
	var res *httpx.IPResolver
	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./httpx/...`
Expected: FAIL — `undefined: httpx.NewIPResolver`.

- [ ] **Step 3: Write the implementation**

Create `httpx/ip.go`:

```go
package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const headerForwardedFor = "X-Forwarded-For"

// IPResolver reports the originating client address. X-Forwarded-For is
// honoured only from peers inside the trusted set, because the header is
// caller-supplied: honouring it from anyone would let a client name its own IP.
// The zero set trusts nothing and always reports the peer address.
type IPResolver struct {
	trusted []netip.Prefix
}

// NewIPResolver accepts CIDRs and bare addresses; a bare address becomes a
// single-host prefix. Blank entries are ignored so a trailing comma in an env
// var is harmless.
func NewIPResolver(cidrs []string) (*IPResolver, error) {
	trusted := make([]netip.Prefix, 0, len(cidrs))

	for _, raw := range cidrs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		if p, err := netip.ParsePrefix(entry); err == nil {
			trusted = append(trusted, p)
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("httpx: invalid trusted proxy %q: %w", entry, err)
		}
		trusted = append(trusted, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return &IPResolver{trusted: trusted}, nil
}

func (r *IPResolver) ClientIP(req *http.Request) string {
	peer := peerAddr(req)
	if !r.trusts(peer) {
		return peer
	}

	forwarded := req.Header.Get(headerForwardedFor)
	if forwarded == "" {
		return peer
	}

	first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
	addr, err := netip.ParseAddr(first)
	if err != nil {
		return peer
	}
	return addr.String()
}

func (r *IPResolver) trusts(peer string) bool {
	if r == nil || len(r.trusted) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil {
		return false
	}
	for _, p := range r.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddr(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./httpx/... -v -run ClientIP`
Expected: PASS, ten tests.

- [ ] **Step 5: Commit**

```bash
git add httpx/ip.go httpx/ip_test.go
git commit -m "feat(httpx): trusted-proxy client IP resolution

net/http equivalent of fiberx.TrustedProxyConfig. Empty config trusts
nothing and falls back to the peer address, which is the safe default."
```

---

### Task 6: Final verification

**Files:**
- Modify: `go.mod`, `go.sum` (only if `go mod tidy` changes them)

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: nothing.

- [ ] **Step 1: Confirm httpx has no fiber dependency**

Run: `go list -deps ./httpx | grep -c gofiber`
Expected: `0`. If this prints anything else, an import leaked in and must be removed.

- [ ] **Step 2: Confirm the two envelope formats are byte-identical**

Run: `go test ./... -run Envelope -v`
Expected: PASS. `fiberx` and `httpx` each assert `{"data":…,"error":…}` against the same literals.

- [ ] **Step 3: Tidy and run everything**

```bash
go mod tidy
go build ./...
go test ./...
go vet ./...
```
Expected: all PASS, and `go mod tidy` adds no new requirements — every import was already a dependency.

- [ ] **Step 4: Commit if tidy changed anything**

```bash
git add go.mod go.sum
git commit -m "chore: go mod tidy after the httpx rewrite"
```

If `git status` is clean, skip this step.

---

## Notes for the Implementer

**Why `fiberx` stays one file but `httpx` becomes four.** Every package in this repo is a single file named after itself, and `fiberx` keeps that — the four envelope functions go into `fiberx.go`, the tests into `fiberx_test.go`. `httpx` is the exception because it is genuinely a bigger package than anything else here: envelope, middleware, request logging and IP resolution are four unrelated concerns totalling roughly 425 lines, where the largest existing file in the repo is 144. `record.go` alone exceeds it. If you would rather hold the convention absolutely, collapsing all four into `httpx/httpx.go` is a mechanical change and nothing else in the plan depends on the split.

**Why `httpx` was deleted and recreated rather than edited.** The package changed transport entirely; there is no shared code between the fiber version and the net/http version. Deleting in Task 1 and recreating in Task 2 keeps each commit coherent.

**Consumers are unaffected until they bump.** The eight fiber services import `servicekit-go/httpx` at a pinned module version. They keep compiling against the old pin. Moving them onto `fiberx` is a separate plan and must happen before any of them bump servicekit.

**`min` is a builtin.** Go 1.21+ provides it; `record.go` uses it without an import.
