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
	ErrBodyTooLarge     = errors.New("request body too large")
)

type envelope struct {
	Data  any     `json:"data"`
	Error *string `json:"error"`
}

// Marshal before committing the status: an Encoder would write a newline fiberx
// does not, and would pair a success status with a truncated body when the value
// fails to marshal.
func write(w http.ResponseWriter, status int, data any, errMsg *string) {
	body, err := json.Marshal(envelope{Data: data, Error: errMsg})
	if err != nil {
		Error(w, http.StatusInternalServerError, ErrInternal)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
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
