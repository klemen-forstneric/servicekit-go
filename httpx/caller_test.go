package httpx_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klemen-forstneric/ember/correlation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/caller"
	"github.com/klemen-forstneric/servicekit-go/httpx"
)

// headerEcho answers with the propagation headers it received.
func headerEcho(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"correlation": r.Header.Get(httpx.CorrelationIDHeader),
			"caller":      r.Header.Get(httpx.CallerHeader),
			"client":      r.Header.Get(httpx.CallerClientHeader),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sentHeaders(t *testing.T, rt http.RoundTripper, req *http.Request) map[string]string {
	t.Helper()
	resp, err := (&http.Client{Transport: rt}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var got map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return got
}

func TestCallerTransportSendsTheContextCaller(t *testing.T) {
	srv := headerEcho(t)
	ctx := caller.NewContext(context.Background(), caller.Caller{ID: "ops@stxt.ai", Client: "bo-cli"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	got := sentHeaders(t, httpx.NewCallerTransport(nil), req)

	assert.Equal(t, "ops@stxt.ai", got["caller"])
	assert.Equal(t, "bo-cli", got["client"])
	assert.Empty(t, req.Header.Get(httpx.CallerHeader), "the caller's request is not changed")
}

func TestCallerTransportKeepsAHeaderTheCallerSet(t *testing.T) {
	srv := headerEcho(t)
	ctx := caller.NewContext(context.Background(), caller.Caller{ID: "from-ctx"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	req.Header.Set(httpx.CallerHeader, "from-caller")

	assert.Equal(t, "from-caller", sentHeaders(t, httpx.NewCallerTransport(nil), req)["caller"])
}

func TestCallerTransportSendsNothingWithoutACaller(t *testing.T) {
	srv := headerEcho(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	got := sentHeaders(t, httpx.NewCallerTransport(nil), req)

	assert.Empty(t, got["caller"])
	assert.Empty(t, got["client"])
}

func TestInternalTransportForwardsCorrelationAndCaller(t *testing.T) {
	srv := headerEcho(t)
	ctx := correlation.NewContext(context.Background(), "cid-1")
	ctx = caller.NewContext(ctx, caller.Caller{ID: "ops@stxt.ai", Client: "bo-cli"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	got := sentHeaders(t, httpx.NewInternalTransport(nil), req)

	assert.Equal(t, map[string]string{"correlation": "cid-1", "caller": "ops@stxt.ai", "client": "bo-cli"}, got)
}
