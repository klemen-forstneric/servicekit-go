package httpx_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

type line struct {
	msg string
	kvs []any
}

type recLogger struct{ lines []line }

func (l *recLogger) Debug(_ context.Context, msg string, kvs ...any) {}
func (l *recLogger) Info(_ context.Context, msg string, kvs ...any) {
	l.lines = append(l.lines, line{msg: msg, kvs: kvs})
}
func (l *recLogger) Warn(_ context.Context, msg string, kvs ...any)       {}
func (l *recLogger) Error(_ context.Context, _ string, _ error, _ ...any) {}

func (l *recLogger) value(t *testing.T, idx int, key string) any {
	t.Helper()
	require.Less(t, idx, len(l.lines))
	kvs := l.lines[idx].kvs
	for i := 0; i+1 < len(kvs); i += 2 {
		if kvs[i] == key {
			return kvs[i+1]
		}
	}
	return nil
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, "hi")
	})
}

func TestRecordLogsRequestAndResponse(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	require.Len(t, l.lines, 2)
	assert.Equal(t, "Incoming HTTP request", l.lines[0].msg)
	assert.Equal(t, "Returned HTTP response", l.lines[1].msg)
	assert.Equal(t, 200, l.value(t, 1, "status_code"))
}

func TestRecordSkipsConfiguredPathsEntirely(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipPaths: []string{"/health"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))

	assert.Empty(t, l.lines)
}

// SkipPaths is an exact match, so a longer path with the same prefix is logged.
func TestRecordSkipPathsIsExact(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipPaths: []string{"/health"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))

	assert.Len(t, l.lines, 2)
}

func TestRecordRedactsHeaders(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{RedactHeaders: []string{"Authorization"}})(okHandler())

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	h.ServeHTTP(httptest.NewRecorder(), req)

	headers, ok := l.value(t, 0, "headers").(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "[redacted]", headers["Authorization"])
	assert.NotContains(t, headers["Authorization"], "super-secret")
}

func TestRecordRedactsQueryParams(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{RedactQuery: []string{"token"}})(okHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ws?token=super-secret&x=1", nil))

	path, _ := l.value(t, 0, "path").(string)
	assert.Contains(t, path, "token=%5Bredacted%5D")
	assert.Contains(t, path, "x=1")
	assert.NotContains(t, path, "super-secret")
}

// SkipBodyPaths is a prefix match: the request is still logged, the body is not.
func TestRecordSkipsBodiesByPrefix(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipBodyPaths: []string{"/auth/"}})(okHandler())

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"password":"hunter2"}`))
	h.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, l.lines, 2)
	assert.Nil(t, l.value(t, 0, "body"))
	assert.Nil(t, l.value(t, 1, "body"))
}

func TestRecordLogsBodyOnOtherPaths(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{SkipBodyPaths: []string{"/auth/"}})(okHandler())

	req := httptest.NewRequest("POST", "/chats", strings.NewReader(`{"title":"hi"}`))
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.NotNil(t, l.value(t, 0, "body"))
}

// The handler must still see the body Record consumed for logging.
func TestRecordRestoresRequestBody(t *testing.T) {
	l := &recLogger{}
	var seen string
	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 32)
		n, _ := r.Body.Read(b)
		seen = string(b[:n])
		httpx.EmptyJSON(w, http.StatusOK)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1}`)))

	assert.Equal(t, `{"a":1}`, seen)
}

func TestRecordTruncatesLargeBodies(t *testing.T) {
	l := &recLogger{}
	h := httpx.Record(l, httpx.RecordConfig{MaxBodyBytes: 8})(okHandler())

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 100))))

	body, _ := l.value(t, 0, "body").(string)
	assert.Len(t, body, 8)
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// Without Unwrap, http.NewResponseController cannot reach the real writer and
// every WebSocket upgrade behind Record fails.
func TestRecordResponseWriterSupportsHijack(t *testing.T) {
	l := &recLogger{}
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}

	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ws", nil))

	assert.True(t, rec.hijacked)
}

// A real WebSocket upgrade (as ReverseProxy performs it) never calls
// WriteHeader — it hijacks and writes the 101 status line straight to the raw
// connection. Record must learn about the upgrade from the hijack itself.
func TestRecordDoesNotCaptureUpgradeBody(t *testing.T) {
	l := &recLogger{}
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}

	h := httpx.Record(l, httpx.RecordConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ws", nil))

	require.Len(t, l.lines, 2)
	assert.Equal(t, 101, l.value(t, 1, "status_code"))
	assert.Nil(t, l.value(t, 1, "body"))
}

// The bytes beyond MaxBodyBytes are stitched back onto the body unread, not
// buffered, so a large upload still arrives at the handler in full.
func TestRecordDoesNotTruncateBodyForHandler(t *testing.T) {
	l := &recLogger{}
	var seen string
	h := httpx.Record(l, httpx.RecordConfig{MaxBodyBytes: 8})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		httpx.EmptyJSON(w, http.StatusOK)
	}))

	full := strings.Repeat("a", 100)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(full)))

	assert.Equal(t, full, seen)
}
