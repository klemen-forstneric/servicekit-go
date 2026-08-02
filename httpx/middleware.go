package httpx

import (
	"bufio"
	"errors"
	"fmt"
	"net"
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

// Recover converts a panic into a 500 envelope, but only while the response is
// still uncommitted — once the handler has written, hijacked, or flushed,
// appending the envelope would corrupt the body, so the partial response is left
// as it is. http.ErrAbortHandler is re-panicked untouched: net/http defines it
// to mean "abort silently", and ReverseProxy raises it on every client
// disconnect mid-response.
func Recover(l ember.LoggerCtx) Middleware {
	if l == nil {
		l = ember.NopLogger
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := &commitWriter{ResponseWriter: w}

			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				l.Error(r.Context(), "Panic serving HTTP request", fmt.Errorf("%v", v),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				if !cw.committed {
					Error(cw, http.StatusInternalServerError, ErrInternal)
				}
			}()

			next.ServeHTTP(cw, r)
		})
	}
}

// commitWriter reports whether anything has reached the client yet. Unwrap keeps
// http.NewResponseController able to reach the real writer.
type commitWriter struct {
	http.ResponseWriter
	committed bool
}

func (w *commitWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *commitWriter) WriteHeader(code int) {
	w.committed = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *commitWriter) Write(b []byte) (int, error) {
	w.committed = true
	return w.ResponseWriter.Write(b)
}

// A successful flush commits the response: net/http writes the header first if
// the handler has not. A rejected one commits nothing.
func (w *commitWriter) Flush() {
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err == nil {
		w.committed = true
	}
}

func (w *commitWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	w.committed = true
	return h.Hijack()
}
