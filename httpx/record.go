package httpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/klemen-forstneric/ember"
)

const (
	logFieldRemoteAddress = "remote_address"
	logFieldHTTPMethod    = "method"
	logFieldPath          = "path"
	logFieldHeaders       = "headers"
	logFieldBody          = "body"
	logFieldStatusCode    = "status_code"
	logFieldDuration      = "duration"
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

// defaultRedactKeys is always redacted from bodies and queries, in normalized form.
var defaultRedactKeys = []string{
	"password", "oldpassword", "newpassword",
	"token", "accesstoken", "refreshtoken", "idtoken",
	"code", "secret", "clientsecret", "apikey",
}

// RedactKeys holds the body and query keys to redact. Keys match ignoring case,
// '_' and '-', so access_token, accessToken and Access-Token are one key.
type RedactKeys map[string]struct{}

// NewRedactKeys returns the credential keys Record always redacts plus extra.
func NewRedactKeys(extra ...string) RedactKeys {
	keys := make(RedactKeys, len(defaultRedactKeys)+len(extra))
	for _, ks := range [][]string{defaultRedactKeys, extra} {
		for _, k := range ks {
			keys[normalizeKey(k)] = struct{}{}
		}
	}
	return keys
}

// IsRedacted reports whether the value under key must not be logged.
func (k RedactKeys) IsRedacted(key string) bool {
	_, ok := k[normalizeKey(key)]
	return ok
}

// RedactQuery returns rawQuery with every redacted key's values replaced. A
// well-formed query with nothing to redact is returned unchanged; a malformed
// one loses the pairs that do not parse.
func (k RedactKeys) RedactQuery(rawQuery string) string {
	q, err := url.ParseQuery(rawQuery)
	redacted := err != nil
	for key := range q {
		if k.IsRedacted(key) {
			q.Set(key, RedactedValue)
			redacted = true
		}
	}
	if !redacted {
		return rawQuery
	}
	return q.Encode()
}

var keySeparators = strings.NewReplacer("_", "", "-", "")

func normalizeKey(key string) string {
	return keySeparators.Replace(strings.ToLower(key))
}

// RecordConfig tunes what Record logs. SkipPaths matches exactly; SkipBodyPaths
// matches by prefix; SkipBodyRoutes matches a method and an exact path. Each
// matches the path as sent or cleaned, so //auth/login counts as /auth/login.
// RedactHeaders and RedactQuery extend the credentials Record always redacts;
// they cannot un-redact them.
type RecordConfig struct {
	SkipPaths      []string
	SkipBodyPaths  []string
	SkipBodyRoutes []Route
	RedactQuery    []string
	RedactHeaders  []string
	MaxBodyBytes   int
}

// Route is one endpoint: a method and an exact path. An empty Method is GET.
type Route struct {
	Method string
	Path   string
}

func (r Route) matches(method, path string) bool {
	m := r.Method
	if m == "" {
		m = http.MethodGet
	}
	return path == r.Path && strings.EqualFold(method, m)
}

// LogsBody reports whether Record logs the bodies of a request to method and
// path.
func (c RecordConfig) LogsBody(method, urlPath string) bool {
	for _, p := range spellings(urlPath) {
		if hasPrefix(p, c.SkipBodyPaths) {
			return false
		}
		for _, r := range c.SkipBodyRoutes {
			if r.matches(method, p) {
				return false
			}
		}
	}
	return true
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
	redactQuery := NewRedactKeys(cfg.RedactQuery...)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSkipped(skip, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			logBody := cfg.LogsBody(r.Method, r.URL.Path)

			var reqBody any
			if logBody {
				raw := readAndRestore(r, maxBody)
				reqBody = decodeBody(raw)
			}

			l.Info(r.Context(), "Incoming HTTP request",
				logFieldRemoteAddress, r.RemoteAddr,
				logFieldHTTPMethod, r.Method,
				logFieldPath, redactedURL(r, redactQuery),
				logFieldHeaders, headers(r, redactHeader),
				logFieldBody, reqBody,
			)

			rec := &recorder{ResponseWriter: w, status: http.StatusOK, capture: logBody, limit: maxBody}
			start := time.Now()
			next.ServeHTTP(rec, r)
			duration := time.Since(start)

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
				logFieldPath, redactedURL(r, redactQuery),
				logFieldDuration, duration,
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

func redactedURL(r *http.Request, keys RedactKeys) string {
	if r.URL.RawQuery == "" {
		return r.URL.RequestURI()
	}
	u := *r.URL
	u.RawQuery = keys.RedactQuery(u.RawQuery)
	return u.RequestURI()
}

// spellings are a path as sent and cleaned: ServeMux routes //e/v1/i as
// /e/v1/i, and cleaning drops the trailing slash a prefix like /auth/ needs.
func spellings(p string) [2]string {
	return [2]string{p, path.Clean(p)}
}

func isSkipped(skip map[string]struct{}, urlPath string) bool {
	for _, p := range spellings(urlPath) {
		if _, ok := skip[p]; ok {
			return true
		}
	}
	return false
}

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
