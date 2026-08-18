package httpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
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
	// RedactedValue replaces every redacted header value and query parameter.
	RedactedValue = "[redacted]"

	defaultMaxBodyBytes = 64 << 10
)

// defaultRedactHeaders is always redacted; RedactHeaders adds to it.
var defaultRedactHeaders = []string{
	"Authorization",
	"Cookie",
	"Set-Cookie",
	"Proxy-Authorization",
	"X-Api-Key",
}

// DefaultRedactHeaders lists the credential headers Record always redacts. It
// is exported so fiberx.Record redacts exactly the same set.
func DefaultRedactHeaders() []string { return slices.Clone(defaultRedactHeaders) }

// RecordConfig tunes what Record logs. SkipPaths matches exactly; SkipBodyPaths
// matches by prefix. RedactHeaders extends the credential headers Record always
// redacts; it cannot un-redact them.
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
	redactHeader := make(map[string]struct{}, len(defaultRedactHeaders)+len(cfg.RedactHeaders))
	for _, hs := range [][]string{defaultRedactHeaders, cfg.RedactHeaders} {
		for _, h := range hs {
			redactHeader[http.CanonicalHeaderKey(h)] = struct{}{}
		}
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
// WriteHeader is never called, so this is the only signal we get. It delegates
// to the response controller so a wrapper that only implements Unwrap is still
// walked through.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	r.hijacked = true
	r.capture = false
	return conn, rw, nil
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
	head, body := readAndRestoreBody(r.Body, limit)
	r.Body = body
	return head
}

// readAndRestoreBody reads at most limit bytes for logging and stitches the
// rest back unread, so a large payload is never fully buffered here and the
// caller still receives every byte.
//
// A read error still yields the bytes read so far; dropping them would leave
// the body drained and silently truncate the payload.
func readAndRestoreBody(body io.ReadCloser, limit int) ([]byte, io.ReadCloser) {
	if body == nil {
		return nil, nil
	}
	head, _ := io.ReadAll(io.LimitReader(body, int64(limit)))
	return head, struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), body), body}
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
			out[k] = RedactedValue
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
			q.Set(key, RedactedValue)
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
