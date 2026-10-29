package httpx

import (
	"net/http"

	"github.com/klemen-forstneric/servicekit-go/caller"
)

// CallerTransport sends the context's caller as the X-Caller and X-Client
// headers, so a service called on behalf of a staff request knows who made it.
// Headers the caller set are kept. Use it only for internal services: a
// third-party API has no business knowing staff emails.
type CallerTransport struct {
	next http.RoundTripper
}

// NewCallerTransport wraps next, defaulting to http.DefaultTransport.
func NewCallerTransport(next http.RoundTripper) *CallerTransport {
	if next == nil {
		next = http.DefaultTransport
	}
	return &CallerTransport{next: next}
}

var _ http.RoundTripper = (*CallerTransport)(nil)

func (t *CallerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c, err := caller.FromContext(req.Context())
	if err != nil || req.Header.Get(CallerHeader) != "" {
		return t.next.RoundTrip(req)
	}

	// A RoundTripper must not change the request it was given.
	req = req.Clone(req.Context())
	req.Header.Set(CallerHeader, c.ID)
	if c.Client != "" {
		req.Header.Set(CallerClientHeader, c.Client)
	}
	return t.next.RoundTrip(req)
}

// NewInternalTransport is the transport for calls to the platform's own
// services: it forwards the request's correlation id and caller, so the
// callee's logs and events carry the same ones. Not for third-party APIs.
func NewInternalTransport(next http.RoundTripper) http.RoundTripper {
	return NewCorrelationTransport(NewCallerTransport(next), nil)
}
