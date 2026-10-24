package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klemen-forstneric/ember/correlation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

// correlationEcho answers with the Correlation-ID header it received.
func correlationEcho(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get(httpx.CorrelationIDHeader)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sentCorrelationID(t *testing.T, rt http.RoundTripper, req *http.Request) string {
	t.Helper()
	resp, err := (&http.Client{Transport: rt}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b := make([]byte, 64)
	n, _ := resp.Body.Read(b)
	return string(b[:n])
}

func TestCorrelationTransportSendsTheContextID(t *testing.T) {
	srv := correlationEcho(t)
	ctx := correlation.NewContext(context.Background(), "cid-1")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	assert.Equal(t, "cid-1", sentCorrelationID(t, httpx.NewCorrelationTransport(nil, nil), req))
	assert.Empty(t, req.Header.Get(httpx.CorrelationIDHeader), "the caller's request is not changed")
}

func TestCorrelationTransportKeepsAHeaderTheCallerSet(t *testing.T) {
	srv := correlationEcho(t)
	ctx := correlation.NewContext(context.Background(), "from-ctx")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	req.Header.Set(httpx.CorrelationIDHeader, "from-caller")

	assert.Equal(t, "from-caller", sentCorrelationID(t, httpx.NewCorrelationTransport(nil, nil), req))
}

func TestCorrelationTransportUsesTheGivenReader(t *testing.T) {
	srv := correlationEcho(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	from := func(context.Context) (string, error) { return "own-key", nil }

	assert.Equal(t, "own-key", sentCorrelationID(t, httpx.NewCorrelationTransport(nil, from), req))
}

func TestCorrelationTransportSendsNothingWithoutAnID(t *testing.T) {
	srv := correlationEcho(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	missing := func(context.Context) (string, error) { return "", errors.New("no id") }

	assert.Empty(t, sentCorrelationID(t, httpx.NewCorrelationTransport(nil, nil), req))
	assert.Empty(t, sentCorrelationID(t, httpx.NewCorrelationTransport(nil, missing), req.Clone(context.Background())))
}
