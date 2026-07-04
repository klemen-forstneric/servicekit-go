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
