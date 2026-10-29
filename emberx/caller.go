package emberx

import (
	"context"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/ember/ext"

	"github.com/klemen-forstneric/servicekit-go/caller"
)

const (
	MetadataKeyCaller       ember.MetadataKey = "caller"
	MetadataKeyCallerClient ember.MetadataKey = "caller_client"
)

// MetadataGetter stamps every event with the correlation id, as ember's
// ext.MetadataGetter does, and with the caller when the request has one, so the
// event stream records who caused each change.
type MetadataGetter struct{}

var _ ember.MetadataGetter = MetadataGetter{}

func (MetadataGetter) Get(ctx context.Context) (ember.Metadata, error) {
	md, err := (&ext.MetadataGetter{}).Get(ctx)
	if err != nil {
		return nil, err
	}
	if c, err := caller.FromContext(ctx); err == nil {
		md[MetadataKeyCaller] = c.ID
		md[MetadataKeyCallerClient] = c.Client
	}
	return md, nil
}

// Caller puts the caller stamped on a consumed event back on the context, so a
// handler's logs, the events it publishes and the services it calls carry it on.
func Caller() ember.SubscriptionMiddleware {
	return func(next ember.HandleFunc) ember.HandleFunc {
		return func(ctx context.Context, e *ember.ReceivedEvent) error {
			if id, ok := e.Metadata[MetadataKeyCaller].(string); ok && id != "" {
				client, _ := e.Metadata[MetadataKeyCallerClient].(string)
				ctx = caller.NewContext(ctx, caller.Caller{ID: id, Client: client})
			}
			return next(ctx, e)
		}
	}
}
