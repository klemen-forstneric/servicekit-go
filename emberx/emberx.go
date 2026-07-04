// Package emberx holds cross-cutting ember subscription middleware.
package emberx

import (
	"context"

	"github.com/klemen-forstneric/ember"
	"github.com/klemen-forstneric/spark"
)

// IdempotencyKey stamps the consumed event's ID as the spark idempotency key, so
// commands a handler dispatches inherit it (qualified by command type via the
// spark middleware's QualifyByType).
func IdempotencyKey() ember.SubscriptionMiddleware {
	return func(next ember.HandleFunc) ember.HandleFunc {
		return func(ctx context.Context, e *ember.ReceivedEvent) error {
			return next(spark.WithIdempotencyKey(ctx, e.ID), e)
		}
	}
}
