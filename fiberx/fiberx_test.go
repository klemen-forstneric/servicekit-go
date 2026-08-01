package fiberx_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/klemen-forstneric/ember/correlation"
	"github.com/klemen-forstneric/spark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/fiberx"
)

func TestCorrelationID_GeneratesAndEchoes(t *testing.T) {
	app := fiber.New()
	var fromUserCtx string
	app.Use(fiberx.CorrelationID())
	app.Get("/", func(c *fiber.Ctx) error {
		cid, _ := correlation.FromContext(c.UserContext())
		fromUserCtx = cid
		return c.SendStatus(fiber.StatusOK)
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	echoed := resp.Header.Get(fiberx.CorrelationIDHeader)
	assert.NotEmpty(t, echoed)
	assert.Equal(t, echoed, fromUserCtx) // same id on the response header and the user context
}

func TestCorrelationID_PreservesIncoming(t *testing.T) {
	app := fiber.New()
	app.Use(fiberx.CorrelationID())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiberx.CorrelationIDHeader, "corr-9")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, "corr-9", resp.Header.Get(fiberx.CorrelationIDHeader))
}

func TestIdempotencyKey_PresentAndAbsent(t *testing.T) {
	newApp := func() (*fiber.App, *bool, *string) {
		app := fiber.New()
		var ok bool
		var got string
		app.Use(fiberx.IdempotencyKey())
		app.Get("/", func(c *fiber.Ctx) error {
			got, ok = spark.IdempotencyKeyFromContext(c.UserContext())
			return c.SendStatus(fiber.StatusOK)
		})
		return app, &ok, &got
	}

	app, ok, got := newApp()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(fiberx.IdempotencyKeyHeader, "key-1")
	_, err := app.Test(req)
	require.NoError(t, err)
	assert.True(t, *ok)
	assert.Equal(t, "key-1", *got)

	app2, ok2, _ := newApp()
	_, err = app2.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.False(t, *ok2)
}

type capLogger struct{ infos int }

func (c *capLogger) Debug(context.Context, string, ...any)        {}
func (c *capLogger) Info(context.Context, string, ...any)         { c.infos++ }
func (c *capLogger) Warn(context.Context, string, ...any)         {}
func (c *capLogger) Error(context.Context, string, error, ...any) {}

func TestRecord_LogsAndSkips(t *testing.T) {
	// logs request + response (2 Info calls) for a normal path
	cap := &capLogger{}
	app := fiber.New()
	app.Use(fiberx.Record(cap, "/health"))
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })

	_, err := app.Test(httptest.NewRequest("GET", "/x", nil))
	require.NoError(t, err)
	assert.Equal(t, 2, cap.infos)

	// skipPaths → no logging
	cap.infos = 0
	_, err = app.Test(httptest.NewRequest("GET", "/health", nil))
	require.NoError(t, err)
	assert.Equal(t, 0, cap.infos)
}

// ipApp echoes c.IP() so the trusted-proxy behaviour is observable. app.Test
// dials with peer 0.0.0.0, so that is the address to trust or withhold trust
// from.
func ipApp(trusted []string) *fiber.App {
	app := fiber.New(fiberx.TrustedProxyConfig(trusted))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString(c.IP()) })
	return app
}

func ipOf(t *testing.T, app *fiber.App, forwardedFor string) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	body := make([]byte, resp.ContentLength)
	_, _ = resp.Body.Read(body)
	return string(body)
}

func TestTrustedProxyConfig_ReportsForwardedClient(t *testing.T) {
	got := ipOf(t, ipApp([]string{"0.0.0.0"}), "203.0.113.7")
	assert.Equal(t, "203.0.113.7", got)
}

// The client is the first hop; the rest of the chain is proxies. Reporting the
// whole header verbatim would send "client, proxy" downstream as the IP.
func TestTrustedProxyConfig_TakesTheClientFromAChain(t *testing.T) {
	got := ipOf(t, ipApp([]string{"0.0.0.0"}), "203.0.113.7, 10.0.0.1, 10.0.0.2")
	assert.Equal(t, "203.0.113.7", got)
}

func TestTrustedProxyConfig_AcceptsCIDRs(t *testing.T) {
	got := ipOf(t, ipApp([]string{"0.0.0.0/8"}), "203.0.113.7")
	assert.Equal(t, "203.0.113.7", got)
}

// X-Forwarded-For is caller-supplied: honoured from an untrusted peer, any
// client could name its own IP.
func TestTrustedProxyConfig_IgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	got := ipOf(t, ipApp([]string{"192.0.2.1"}), "203.0.113.7")
	assert.Equal(t, "0.0.0.0", got)
}

func TestTrustedProxyConfig_EmptyListTrustsNothing(t *testing.T) {
	got := ipOf(t, ipApp(nil), "203.0.113.7")
	assert.Equal(t, "0.0.0.0", got)
}
