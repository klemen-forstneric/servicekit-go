package httpx_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// net/http defines ErrAbortHandler as "abort silently". ReverseProxy raises it
// whenever copying an upstream response fails, which is the routine path for a
// client disconnecting mid-response.
func TestRecoverRepanicsErrAbortHandler(t *testing.T) {
	l := &errLogger{}
	h := httpx.Recover(l)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	w := httptest.NewRecorder()
	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	})

	assert.Equal(t, 0, l.errs)
	assert.Empty(t, w.Body.String())
}

func TestRecoverRepanicsWrappedErrAbortHandler(t *testing.T) {
	l := &errLogger{}
	h := httpx.Recover(l)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(fmt.Errorf("copying response: %w", http.ErrAbortHandler))
	}))

	assert.Panics(t, func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	})
	assert.Equal(t, 0, l.errs)
}

// The envelope is only safe to emit while nothing has been committed; appending
// it to a partial body would corrupt the response.
func TestRecoverLeavesAlreadyWrittenResponseAlone(t *testing.T) {
	l := &errLogger{}
	h := httpx.Recover(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"partial":`))
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	})

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, `{"data":{"partial":`, w.Body.String())
	assert.Equal(t, 1, l.errs)
}

// A bare Write commits the response just as much as WriteHeader does.
func TestRecoverLeavesResponseAloneAfterBareWrite(t *testing.T) {
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, "partial", w.Body.String())
}

// Flushing commits the response even with nothing written yet — net/http emits
// the header on the way out — which is how a streamed response commits.
func TestRecoverWritesNoEnvelopeAfterFlush(t *testing.T) {
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, http.NewResponseController(w).Flush())
		panic("kaboom")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	assert.True(t, w.Flushed)
	assert.Empty(t, w.Body.String())
}

// plainWriter is a bare ResponseWriter: it supports neither Flush nor Hijack.
type plainWriter struct{ rec *httptest.ResponseRecorder }

func (w plainWriter) Header() http.Header         { return w.rec.Header() }
func (w plainWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w plainWriter) WriteHeader(code int)        { w.rec.WriteHeader(code) }

// A flush the underlying writer refuses must report that refusal, and commits
// nothing, so the envelope is still owed.
func TestRecoverStillWritesEnvelopeAfterRejectedFlush(t *testing.T) {
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assert.ErrorIs(t, http.NewResponseController(w).Flush(), http.ErrNotSupported)
		panic("kaboom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(plainWriter{rec: rec}, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, 500, rec.Code)
	assert.Equal(t, `{"data":null,"error":"internal server error"}`, rec.Body.String())
}

// A wrapper that only implements Unwrap (the Go 1.20+ convention, as otelhttp
// and httpsnoop do) must not cost the chain its hijack support.
func TestRecoverHijacksThroughUnwrapOnlyWrapper(t *testing.T) {
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
		panic("kaboom")
	}))

	h.ServeHTTP(unwrapOnlyWriter{inner: rec}, httptest.NewRequest("GET", "/ws", nil))

	assert.True(t, rec.hijacked)
	assert.Empty(t, rec.Body.String())
}

func TestRecoverReportsHijackUnsupported(t *testing.T) {
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		assert.ErrorIs(t, err, http.ErrNotSupported)
		panic("kaboom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(plainWriter{rec: rec}, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, 500, rec.Code)
}

// A hijacked connection is committed too: the envelope would be written to a
// writer whose response has already left through the raw conn.
func TestRecoverWritesNoEnvelopeAfterHijack(t *testing.T) {
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	h := httpx.Recover(&errLogger{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
		panic("kaboom")
	}))

	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ws", nil))

	assert.True(t, rec.hijacked)
	assert.Empty(t, rec.Body.String())
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

func TestMaxBodyRejectsADeclaredOversizedBody(t *testing.T) {
	var reached bool
	h := httpx.MaxBody(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	req := httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 64)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.JSONEq(t, `{"data":null,"error":"request body too large"}`, w.Body.String())
	assert.False(t, reached, "the handler must not run")
}

// A client that lies about or omits Content-Length still gets capped, but only
// once something reads the body — which is the handler's error to report.
func TestMaxBodyCapsAnUnderdeclaredBody(t *testing.T) {
	var readErr error
	h := httpx.MaxBody(16)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	req := httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 64)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)

	var tooLarge *http.MaxBytesError
	assert.ErrorAs(t, readErr, &tooLarge)
}

func TestMaxBodyPassesAnAcceptableBodyThrough(t *testing.T) {
	var got string
	h := httpx.MaxBody(1 << 20)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	}))

	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, `{"a":1}`, got)
}

// A bodyless GET declares ContentLength 0 and must not be rejected.
func TestMaxBodyIgnoresABodylessRequest(t *testing.T) {
	var reached bool
	h := httpx.MaxBody(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	assert.True(t, reached)
}
