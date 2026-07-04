package emberx_test

import (
	"context"
	"testing"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/spark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/emberx"
)

func TestIdempotencyKey_StampsEventID(t *testing.T) {
	var got string
	h := emberx.IdempotencyKey()(func(ctx context.Context, e *ember.ReceivedEvent) error {
		got, _ = spark.IdempotencyKeyFromContext(ctx)
		return nil
	})
	require.NoError(t, h(context.Background(), &ember.ReceivedEvent{ID: "evt-42"}))
	assert.Equal(t, "evt-42", got)
}
