// Package caller carries who made a request on the context, the way
// ember/correlation carries the request id. Today that is the staff member
// behind a backoffice gateway call; fan requests have no caller.
package caller

import (
	"context"
	"errors"
)

type contextKey string

// ContextKey is the key the caller is stored under, also usable with fiber's
// Locals so readers of the raw request context find it.
const ContextKey contextKey = "caller"

var ErrNotFound = errors.New("caller not found in context")

// Caller is who made the request.
type Caller struct {
	// ID is the person: the staff email the gateway sends as X-Caller.
	ID string
	// Client is the app client the token was issued to, sent as X-Client.
	Client string
}

func NewContext(parent context.Context, c Caller) context.Context {
	return context.WithValue(parent, ContextKey, c)
}

func FromContext(ctx context.Context) (Caller, error) {
	c, ok := ctx.Value(ContextKey).(Caller)
	if !ok {
		return Caller{}, ErrNotFound
	}
	return c, nil
}
