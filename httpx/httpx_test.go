package httpx_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

// The envelope bytes must match fiberx exactly — the eight-service migration
// rests on it — so these assert raw strings, not JSONEq.
func TestJSONEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.JSON(w, http.StatusOK, map[string]int{"x": 1})

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, `{"data":{"x":1},"error":null}`, w.Body.String())
}

func TestEmptyJSONEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.EmptyJSON(w, http.StatusAccepted)

	assert.Equal(t, 202, w.Code)
	assert.Equal(t, `{"data":null,"error":null}`, w.Body.String())
}

func TestErrorEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.Error(w, http.StatusBadRequest, errors.New("boom"))

	assert.Equal(t, 400, w.Code)
	assert.Equal(t, `{"data":null,"error":"boom"}`, w.Body.String())
}

func TestNotFoundEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.NotFound(w, httptest.NewRequest("GET", "/nope", nil))

	assert.Equal(t, 404, w.Code)
	assert.Equal(t, `{"data":null,"error":"not found"}`, w.Body.String())
}

func TestMethodNotAllowedEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.MethodNotAllowed(w, httptest.NewRequest("POST", "/x", nil))

	assert.Equal(t, 405, w.Code)
	assert.Equal(t, `{"data":null,"error":"method not allowed"}`, w.Body.String())
}

// Encoding after WriteHeader would commit a success status and then emit a
// truncated body.
func TestJSONUnmarshalableValueYieldsErrorEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.JSON(w, http.StatusOK, struct {
		Ch chan int `json:"ch"`
	}{make(chan int)})

	assert.Equal(t, 500, w.Code)
	assert.Equal(t, `{"data":null,"error":"internal server error"}`, w.Body.String())
}

func TestHealthRoutes(t *testing.T) {
	mux := http.NewServeMux()
	httpx.SetupHealthRoutes(mux)

	for _, path := range []string{"/", "/health"} {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest("GET", path, nil))

		require.Equal(t, 200, resp.Code, path)
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, `{"status":"ok"}`, string(body))
	}
}

// The health handler is registered for the exact root, not as a catch-all, so
// an unregistered path still falls through to the mux's own 404.
func TestHealthRootIsExactNotCatchAll(t *testing.T) {
	mux := http.NewServeMux()
	httpx.SetupHealthRoutes(mux)

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest("GET", "/something-else", nil))

	assert.Equal(t, 404, resp.Code)
}
