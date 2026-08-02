// Package fiberx holds cross-cutting Fiber middleware.
package fiberx

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/correlation"
	"github.com/klemen-forstneric/spark"
)

const (
	CorrelationIDHeader  = "Correlation-ID"
	IdempotencyKeyHeader = "Idempotency-Key"
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

// Record logs each request and its response. Paths in skipPaths are not logged
// (e.g. health checks).
func Record(l ember.LoggerCtx, skipPaths ...string) fiber.Handler {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *fiber.Ctx) error {
		if _, ok := skip[c.Path()]; ok {
			return c.Next()
		}

		ctx := c.Context()

		requestBody := c.Body()
		var reqBody interface{}
		if len(requestBody) != 0 {
			if json.Valid(requestBody) {
				reqBody = json.RawMessage(requestBody)
			} else {
				reqBody = string(requestBody)
			}
		}

		headers := make(map[string]string)
		c.Request().Header.VisitAll(func(key, value []byte) {
			headers[string(key)] = string(value)
		})

		remoteAddress := c.IP()
		if forwardedFor := c.Get(headerForwardedFor); forwardedFor != "" {
			remoteAddress = forwardedFor
		}

		l.Info(ctx, "Incoming HTTP request",
			logFieldRemoteAddress, remoteAddress,
			logFieldHTTPMethod, c.Method(),
			logFieldPath, c.OriginalURL(),
			logFieldHeaders, headers,
			logFieldBody, reqBody,
		)

		if err := c.Next(); err != nil {
			return err
		}

		responseBody := c.Response().Body()
		var respBody json.RawMessage
		if len(responseBody) != 0 {
			respBody = json.RawMessage(responseBody)
		}

		l.Info(ctx, "Returned HTTP response",
			logFieldStatusCode, c.Response().StatusCode(),
			logFieldHTTPMethod, c.Method(),
			logFieldPath, c.OriginalURL(),
			logFieldBody, respBody,
		)
		return nil
	}
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
