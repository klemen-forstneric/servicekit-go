package emberx_test

import (
	"context"
	"testing"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/correlation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/caller"
	"github.com/klemen-forstneric/servicekit-go/emberx"
)

func TestMetadataGetter_StampsCorrelationAndCaller(t *testing.T) {
	ctx := correlation.NewContext(context.Background(), "cid-1")
	ctx = caller.NewContext(ctx, caller.Caller{ID: "ops@stxt.ai", Client: "bo-cli"})

	md, err := emberx.MetadataGetter{}.Get(ctx)

	require.NoError(t, err)
	assert.Equal(t, ember.Metadata{
		"correlation_id":               "cid-1",
		emberx.MetadataKeyCaller:       "ops@stxt.ai",
		emberx.MetadataKeyCallerClient: "bo-cli",
	}, md)
}

func TestMetadataGetter_NoCallerOnFanRequests(t *testing.T) {
	md, err := emberx.MetadataGetter{}.Get(correlation.NewContext(context.Background(), "cid-1"))

	require.NoError(t, err)
	assert.Equal(t, ember.Metadata{"correlation_id": "cid-1"}, md)
}

// Same contract as ember's ext.MetadataGetter, which every publisher relies on:
// an event without a correlation id is refused, not published.
func TestMetadataGetter_RequiresCorrelationID(t *testing.T) {
	_, err := emberx.MetadataGetter{}.Get(caller.NewContext(context.Background(), caller.Caller{ID: "ops@stxt.ai"}))

	assert.Error(t, err)
}

func TestCaller_RestoresTheEventsCaller(t *testing.T) {
	var got caller.Caller
	var gotErr error
	h := emberx.Caller()(func(ctx context.Context, e *ember.ReceivedEvent) error {
		got, gotErr = caller.FromContext(ctx)
		return nil
	})

	require.NoError(t, h(context.Background(), &ember.ReceivedEvent{Metadata: ember.Metadata{
		emberx.MetadataKeyCaller:       "ops@stxt.ai",
		emberx.MetadataKeyCallerClient: "bo-cli",
	}}))

	require.NoError(t, gotErr)
	assert.Equal(t, caller.Caller{ID: "ops@stxt.ai", Client: "bo-cli"}, got)
}

func TestCaller_LeavesEventsWithoutACallerAlone(t *testing.T) {
	var gotErr error
	h := emberx.Caller()(func(ctx context.Context, e *ember.ReceivedEvent) error {
		_, gotErr = caller.FromContext(ctx)
		return nil
	})

	require.NoError(t, h(context.Background(), &ember.ReceivedEvent{Metadata: ember.Metadata{"correlation_id": "cid-1"}}))

	assert.ErrorIs(t, gotErr, caller.ErrNotFound)
}
