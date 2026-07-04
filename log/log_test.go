package log

import (
	"bytes"
	"context"
	"testing"

	"github.com/klemen-forstneric/ember/correlation"
	"github.com/stretchr/testify/assert"
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
