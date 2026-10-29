package log

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/klemen-forstneric/ember/correlation"
	"github.com/stretchr/testify/assert"

	"github.com/klemen-forstneric/servicekit-go/caller"
)

func TestLoggerStampsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	lg := newWithWriter(&buf)
	ctx := correlation.NewContext(context.Background(), "corr-1")
	lg.Info(ctx, "hello")
	assert.Contains(t, buf.String(), `"correlation_id":"corr-1"`)
	assert.Contains(t, buf.String(), `"msg":"hello"`)
}

func TestLoggerOmitsCorrelationIDWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	lg := newWithWriter(&buf)
	lg.Info(context.Background(), "hello")
	assert.NotContains(t, buf.String(), "correlation_id")
}

func TestLoggerStampsCaller(t *testing.T) {
	var buf bytes.Buffer
	lg := newWithWriter(&buf)
	ctx := caller.NewContext(context.Background(), caller.Caller{ID: "ops@stxt.ai", Client: "bo-cli"})
	lg.Info(ctx, "hello")
	assert.Contains(t, buf.String(), `"caller":"ops@stxt.ai"`)
	assert.Contains(t, buf.String(), `"caller_client":"bo-cli"`)
}

func TestLoggerOmitsCallerWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	lg := newWithWriter(&buf)
	lg.Info(context.Background(), "hello")
	assert.NotContains(t, buf.String(), "caller")
}

func TestLoggerWritesDurationsAsText(t *testing.T) {
	var buf bytes.Buffer
	lg := newWithWriter(&buf)
	lg.Info(context.Background(), "done", "duration", 75312*time.Microsecond)
	assert.Contains(t, buf.String(), `"duration":"75.312ms"`)
}
