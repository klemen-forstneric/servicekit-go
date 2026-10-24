package httpx

import (
	"context"
	"net/http"

	"github.com/klemen-forstneric/ember/correlation"
)

// CorrelationTransport sends the caller's correlation id as the Correlation-ID
// header, so the callee logs the same id. A header the caller set is kept.
type CorrelationTransport struct {
	next http.RoundTripper
	from func(context.Context) (string, error)
}

// NewCorrelationTransport wraps next, defaulting to http.DefaultTransport. from
// reads the id from the request context, defaulting to correlation.FromContext;
// a service that keeps the id under its own context key passes its reader.
func NewCorrelationTransport(next http.RoundTripper, from func(context.Context) (string, error)) *CorrelationTransport {
	if next == nil {
		next = http.DefaultTransport
	}
	if from == nil {
		from = correlation.FromContext
	}
	return &CorrelationTransport{next: next, from: from}
}

var _ http.RoundTripper = (*CorrelationTransport)(nil)

func (t *CorrelationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(CorrelationIDHeader) == "" {
		if id, err := t.from(req.Context()); err == nil && id != "" {
			// A RoundTripper must not change the request it was given.
			req = req.Clone(req.Context())
			req.Header.Set(CorrelationIDHeader, id)
		}
	}
	return t.next.RoundTrip(req)
}
