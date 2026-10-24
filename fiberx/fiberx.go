// Package fiberx holds cross-cutting Fiber middleware.
package fiberx

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/correlation"
	"github.com/klemen-forstneric/spark"

	"github.com/klemen-forstneric/servicekit-go/errorsx"
	"github.com/klemen-forstneric/servicekit-go/httpx"
)

const (
	CorrelationIDHeader  = httpx.CorrelationIDHeader
	IdempotencyKeyHeader = httpx.IdempotencyKeyHeader
)

// TrustedProxyConfig returns the Fiber config a service needs for c.IP() to
// report the real caller when it sits behind a reverse proxy (fe-gateway) that
// appends X-Forwarded-For. Without it c.IP() is the proxy's own address, which
// then travels downstream as the cardholder's IP and fails PSP fraud checks.
//
// trustedProxies holds the addresses or CIDRs the forwarded header is honoured
// from. Empty trusts nothing and c.IP() falls back to the peer address — the
// safe default, because X-Forwarded-For is caller-supplied: honouring it from
// anyone would let a client name its own IP, which is worse than reading the
// socket. Only ever list proxies you control.
func TrustedProxyConfig(trustedProxies []string) fiber.Config {
	return fiber.Config{
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          trustedProxies,
		// Without validation Fiber hands back the raw header, so a chained
		// "client, proxy" chain would be reported verbatim as the IP.
		EnableIPValidation: true,
	}
}

// CorrelationID ensures every request carries a correlation id — taken from the
// Correlation-ID header or generated — echoes it on the response, and stores it
// where correlation.FromContext can read it — both on Locals (for readers of
// c.Context()) and on the user context, which is what dispatched commands carry.
func CorrelationID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		correlationID := c.Get(CorrelationIDHeader)
		if correlationID == "" {
			correlationID = uuid.NewString()
		}

		c.Set(CorrelationIDHeader, correlationID)
		c.Locals(correlation.ContextKey, correlationID)
		c.SetUserContext(correlation.NewContext(c.UserContext(), correlationID))

		return c.Next()
	}
}

// IdempotencyKey lifts a client Idempotency-Key header onto the request's user
// context so dispatched commands inherit it as the spark idempotency key. Absent
// header → no key (the command bypasses idempotency).
func IdempotencyKey() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if v := c.Get(IdempotencyKeyHeader); v != "" {
			c.SetUserContext(spark.WithIdempotencyKey(c.UserContext(), v))
		}
		return c.Next()
	}
}

const (
	logFieldRemoteAddress = "remote_address"
	logFieldHTTPMethod    = "method"
	logFieldPath          = "path"
	logFieldHeaders       = "headers"
	logFieldBody          = "body"
	logFieldStatusCode    = "status_code"
)

const headerForwardedFor = "X-Forwarded-For"

// DefaultMaxBodyBytes is how much of a body Record logs when MaxBodyBytes is
// not set.
const DefaultMaxBodyBytes = 64 << 10

// RecordConfig tunes what Record logs. SkipPaths matches exactly; SkipBodyPaths
// matches by prefix; SkipBodyRoutes matches a method and an exact path.
// RedactHeaders extends the credential headers Record always redacts; it cannot
// un-redact them.
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
func (c RecordConfig) LogsBody(method, path string) bool {
	if hasPrefix(path, c.SkipBodyPaths) {
		return false
	}
	for _, r := range c.SkipBodyRoutes {
		if r.matches(method, path) {
			return false
		}
	}
	return true
}

// Record logs each request and its response. Credential headers are always
// redacted, and a body is logged up to MaxBodyBytes.
func Record(l ember.LoggerCtx, cfg RecordConfig) fiber.Handler {
	if l == nil {
		l = ember.NopLogger
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}
	skip := make(map[string]struct{}, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = struct{}{}
	}
	redactHeader := make(map[string]struct{})
	for _, hs := range [][]string{httpx.DefaultRedactHeaders(), cfg.RedactHeaders} {
		for _, h := range hs {
			redactHeader[http.CanonicalHeaderKey(h)] = struct{}{}
		}
	}

	return func(c *fiber.Ctx) error {
		if _, ok := skip[c.Path()]; ok {
			return c.Next()
		}

		ctx := c.Context()
		logBody := cfg.LogsBody(c.Method(), c.Path())
		path := redactedURL(c.OriginalURL(), cfg.RedactQuery)

		var reqBody any
		if logBody {
			reqBody = bodyForLog(c.Body(), maxBody)
		}

		headers := make(map[string]string)
		c.Request().Header.VisitAll(func(key, value []byte) {
			k := http.CanonicalHeaderKey(string(key))
			if _, ok := redactHeader[k]; ok {
				headers[k] = httpx.RedactedValue
				return
			}
			// VisitAll yields one call per value; httpx joins repeats the same way.
			if prev, ok := headers[k]; ok {
				headers[k] = prev + "," + string(value)
				return
			}
			headers[k] = string(value)
		})

		remoteAddress := c.IP()
		if forwardedFor := c.Get(headerForwardedFor); forwardedFor != "" {
			remoteAddress = forwardedFor
		}

		l.Info(ctx, "Incoming HTTP request",
			logFieldRemoteAddress, remoteAddress,
			logFieldHTTPMethod, c.Method(),
			logFieldPath, path,
			logFieldHeaders, headers,
			logFieldBody, reqBody,
		)

		if err := c.Next(); err != nil {
			return err
		}

		var respBody any
		if logBody {
			respBody = bodyForLog(c.Response().Body(), maxBody)
		}

		l.Info(ctx, "Returned HTTP response",
			logFieldStatusCode, c.Response().StatusCode(),
			logFieldHTTPMethod, c.Method(),
			logFieldPath, path,
			logFieldBody, respBody,
		)
		return nil
	}
}

// bodyForLog keeps at most limit bytes. A cut JSON body is no longer valid JSON,
// so it is logged as a string.
func bodyForLog(raw []byte, limit int) any {
	raw = raw[:min(len(raw), limit)]
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	return string(raw)
}

func redactedURL(uri string, redact []string) string {
	if len(redact) == 0 {
		return uri
	}
	u, err := url.ParseRequestURI(uri)
	if err != nil || u.RawQuery == "" {
		return uri
	}
	q := u.Query()
	for _, key := range redact {
		if q.Has(key) {
			q.Set(key, httpx.RedactedValue)
		}
	}
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

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
	body := fiber.Map{
		"data":  nil,
		"error": err.Error(),
	}
	if code := errorsx.Code(err); code != "" {
		body["code"] = code
	}
	return c.Status(status).JSON(body)
}

func SetupHealthRoutes(a *fiber.App) {
	health := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	}

	a.Get("/", health)
	a.Get("/health", health)
}

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
