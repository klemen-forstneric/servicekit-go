package httpx_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

// echoServer returns the body it was sent, so a test can assert the client
// still receives every byte the recorder read for logging.
func echoServer(t *testing.T, status int, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLoggerTransportLogsRequestAndResponse(t *testing.T) {
	l := &recLogger{}
	srv := echoServer(t, http.StatusOK, `{"predictions":[{"summary":"ok"}]}`)
	c := &http.Client{Transport: httpx.NewLoggerTransport(l, nil, httpx.LoggerTransportConfig{})}

	resp, err := c.Post(srv.URL+"/predict", "application/json", strings.NewReader(`{"mode":"x"}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Len(t, l.lines, 2)
	assert.Equal(t, "Outgoing HTTP request", l.lines[0].msg)
	assert.Equal(t, "Received HTTP response", l.lines[1].msg)
	assert.Equal(t, "POST", l.value(t, 0, "method"))
	assert.Equal(t, srv.URL+"/predict", l.value(t, 0, "url"))
	assert.JSONEq(t, `{"mode":"x"}`, string(l.value(t, 0, "body").(json.RawMessage)))
	assert.Equal(t, 200, l.value(t, 1, "status_code"))
	assert.JSONEq(t, `{"predictions":[{"summary":"ok"}]}`, string(l.value(t, 1, "body").(json.RawMessage)))
	assert.NotNil(t, l.value(t, 1, "elapsed_ms"))
}

// The point of the recorder: a body carrying an in-band error under HTTP 200 is
// the only place the reason exists.
func TestLoggerTransportLogsErrorBodyOnHTTP200(t *testing.T) {
	l := &recLogger{}
	srv := echoServer(t, http.StatusOK, `{"predictions":[{"error":"not a single JSON object","error_reason":"ValueError"}]}`)
	c := &http.Client{Transport: httpx.NewLoggerTransport(l, nil, httpx.LoggerTransportConfig{})}

	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Contains(t, string(l.value(t, 1, "body").(json.RawMessage)), "ValueError")
}

// The recorder must not consume what it logs.
func TestLoggerTransportPreservesBodies(t *testing.T) {
	l := &recLogger{}
	reply := strings.Repeat("a", 200)
	srv := echoServer(t, http.StatusOK, reply)
	c := &http.Client{Transport: httpx.NewLoggerTransport(l, nil, httpx.LoggerTransportConfig{MaxBodyBytes: 10})}

	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, reply, string(got), "caller must receive the full body, not the logged prefix")
	assert.Len(t, l.value(t, 1, "body"), 10, "only the logged copy is capped")
}

func TestLoggerTransportRedactsCredentials(t *testing.T) {
	l := &recLogger{}
	srv := echoServer(t, http.StatusOK, `{}`)
	c := &http.Client{Transport: httpx.NewLoggerTransport(l, nil, httpx.LoggerTransportConfig{RedactQuery: []string{"token"}})}

	req, err := http.NewRequest("GET", srv.URL+"/p?token=secret&keep=1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer rp-secret")
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	hdrs, ok := l.value(t, 0, "headers").(map[string]string)
	require.True(t, ok)
	assert.Equal(t, httpx.RedactedValue, hdrs["Authorization"])
	url, _ := l.value(t, 0, "url").(string)
	assert.NotContains(t, url, "secret")
	assert.Contains(t, url, "keep=1")
}

func TestLoggerTransportLogsTransportFailure(t *testing.T) {
	l := &recLogger{}
	want := errors.New("dial refused")
	c := &http.Client{Transport: httpx.NewLoggerTransport(l, roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, want
	}), httpx.LoggerTransportConfig{})}

	_, err := c.Get("https://example.invalid/predict")

	require.Error(t, err)
	require.Len(t, l.lines, 2)
	assert.Equal(t, "Outgoing HTTP request failed", l.lines[1].msg)
	assert.ErrorIs(t, l.value(t, 1, "error").(error), want)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
