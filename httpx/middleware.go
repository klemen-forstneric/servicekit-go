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
