package httpx

import (
	"net/http"
	"time"

	"github.com/klemen-forstneric/ember"
)

const (
	logFieldURL       = "url"
	logFieldElapsedMs = "elapsed_ms"
)

// LoggerTransportConfig tunes what LoggerTransport logs. RedactHeaders extends
// the credential headers it always redacts; it cannot un-redact them.
type LoggerTransportConfig struct {
	RedactQuery   []string
	RedactHeaders []string
	MaxBodyBytes  int
}

// LoggerTransport logs each outbound request and its response. It is the
// counterpart to Record: an upstream's own words — a decline code, a parse
// failure, an error payload returned under HTTP 200 — are only ever in the
// response body, so a bare status code cannot explain what happened.
type LoggerTransport struct {
	next        http.RoundTripper
	l           ember.LoggerCtx
	redact      map[string]struct{}
	redactQuery []string
	maxBody     int
}

// NewLoggerTransport wraps next, defaulting to http.DefaultTransport.
func NewLoggerTransport(l ember.LoggerCtx, next http.RoundTripper, cfg LoggerTransportConfig) *LoggerTransport {
	if l == nil {
		l = ember.NopLogger
	}
	if next == nil {
		next = http.DefaultTransport
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	redact := make(map[string]struct{}, len(defaultRedactHeaders)+len(cfg.RedactHeaders))
	for _, hs := range [][]string{defaultRedactHeaders, cfg.RedactHeaders} {
		for _, h := range hs {
			redact[http.CanonicalHeaderKey(h)] = struct{}{}
		}
	}

	return &LoggerTransport{next: next, l: l, redact: redact, redactQuery: cfg.RedactQuery, maxBody: maxBody}
}

var _ http.RoundTripper = (*LoggerTransport)(nil)

func (t *LoggerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	url := outboundURL(req, t.redactQuery)

	reqBody, body := readAndRestoreBody(req.Body, t.maxBody)
	req.Body = body

	t.l.Info(ctx, "Outgoing HTTP request",
		logFieldHTTPMethod, req.Method,
		logFieldURL, url,
		logFieldHeaders, headers(req, t.redact),
		logFieldBody, decodeBody(reqBody),
	)

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		t.l.Error(ctx, "Outgoing HTTP request failed", err,
			logFieldHTTPMethod, req.Method,
			logFieldURL, url,
			logFieldElapsedMs, elapsed.Milliseconds(),
		)
		return nil, err
	}

	respBody, body := readAndRestoreBody(resp.Body, t.maxBody)
	resp.Body = body

	t.l.Info(ctx, "Received HTTP response",
		logFieldStatusCode, resp.StatusCode,
		logFieldHTTPMethod, req.Method,
		logFieldURL, url,
		logFieldElapsedMs, elapsed.Milliseconds(),
		logFieldBody, decodeBody(respBody),
	)

	return resp, nil
}

// outboundURL is the full URL with credentials in userinfo masked, unlike the
// inbound path-only form.
func outboundURL(r *http.Request, redactQuery []string) string {
	u := *r.URL
	if len(redactQuery) > 0 && u.RawQuery != "" {
		q := u.Query()
		for _, key := range redactQuery {
			if q.Has(key) {
				q.Set(key, RedactedValue)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.Redacted()
}
