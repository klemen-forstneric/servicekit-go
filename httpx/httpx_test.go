package httpx_test

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

func TestJSONEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return httpx.JSON(c, fiber.StatusOK, fiber.Map{"x": 1}) })
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"data":{"x":1},"error":null}`, string(body))
}

func TestErrorEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error { return httpx.Error(c, fiber.StatusBadRequest, errors.New("boom")) })
	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"data":null,"error":"boom"}`, string(body))
}

func TestHealthRoutes(t *testing.T) {
	app := fiber.New()
	httpx.SetupHealthRoutes(app)
	for _, path := range []string{"/", "/health"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		assert.JSONEq(t, `{"status":"ok"}`, string(body))
	}
}
