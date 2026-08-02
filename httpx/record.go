package httpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
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

			status := rec.status
			if rec.hijacked {
				status = http.StatusSwitchingProtocols
			}

			var respBody any
			if logBody && status != http.StatusSwitchingProtocols {
				respBody = decodeBody(rec.body.Bytes())
			}

			l.Info(r.Context(), "Returned HTTP response",
				logFieldStatusCode, status,
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
	status   int
	body     bytes.Buffer
	capture  bool
	limit    int
	hijacked bool
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Hijack is required because a reverse proxy performs a WebSocket upgrade by
// hijacking and writing the 101 status line straight to the raw connection —
// WriteHeader is never called, so this is the only signal we get.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	r.hijacked = true
	r.capture = false
	return h.Hijack()
}

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

// readAndRestore reads at most limit bytes for logging and stitches the rest
// of the body back unread, so a large upload is never fully buffered here.
func readAndRestore(r *http.Request, limit int) []byte {
	if r.Body == nil {
		return nil
	}
	head, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)))
	if err != nil {
		return nil
	}
	body := r.Body
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), body), body}
	return head
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
