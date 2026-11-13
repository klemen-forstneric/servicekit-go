package httpx_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klemen-forstneric/servicekit-go/errorsx"
	"github.com/klemen-forstneric/servicekit-go/httpx"
)

var (
	errInternal    = errorsx.New("internal", "internal error")
	errNotFound    = errorsx.New("not_found", "pkg: not found")
	errEmpty       = errorsx.New("empty", "pkg: empty")
	errConflict    = errorsx.New("version_conflict", "version conflict")
	errUnavailable = errorsx.New("unavailable", "service unavailable")
	errDisabled    = errorsx.New("disabled", "pkg: generation is disabled")
	errStoreClash  = errors.New("store: entity version conflict")
)

func translation() httpx.Translation {
	return httpx.NewTranslation(errInternal,
		httpx.Status(http.StatusNotFound, errorsx.Expose(errNotFound)),
		httpx.Status(http.StatusBadRequest, errorsx.Expose(errEmpty)),
		httpx.Status(http.StatusConflict, errorsx.ExposeAs(errStoreClash, errConflict)),
		httpx.Status(http.StatusServiceUnavailable, errorsx.ExposeAs(errDisabled, errUnavailable)),
	)
}

func TestTranslation_PublishesTheStatusOfTheMatchingGroup(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		public *errorsx.Error
	}{
		"exposed as itself":      {err: errEmpty, status: http.StatusBadRequest, public: errEmpty},
		"exposed as another":     {err: errStoreClash, status: http.StatusConflict, public: errConflict},
		"5xx behind a fixed one": {err: errDisabled, status: http.StatusServiceUnavailable, public: errUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			status, public, ok := translation().Public(err(tc.err))

			assert.True(t, ok)
			assert.Equal(t, tc.status, status)
			assert.Same(t, tc.public, public)
		})
	}
}

func TestTranslation_MatchesThroughWrappingAndPublishesNeverTheWrappedText(t *testing.T) {
	status, public, ok := translation().Public(fmt.Errorf("%w: provider said sk-123 is rate limited", errNotFound))

	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "pkg: not found", public.Error())
}

func TestTranslation_FirstGroupWins(t *testing.T) {
	tr := httpx.NewTranslation(errInternal,
		httpx.Status(http.StatusNotFound, errorsx.Expose(errNotFound)),
		httpx.Status(http.StatusBadRequest, errorsx.Expose(errEmpty)),
	)

	status, public, _ := tr.Public(fmt.Errorf("%w and %w", errEmpty, errNotFound))

	assert.Equal(t, http.StatusNotFound, status)
	assert.Same(t, errNotFound, public)
}

func TestTranslation_FallsBackTo500(t *testing.T) {
	status, public, ok := translation().Public(errors.New("pq: connection to 10.0.0.2 lost"))

	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Same(t, errInternal, public)
}

// err passes e through a wrap, so every case also proves matching goes through the chain.
func err(e error) error { return fmt.Errorf("handler: %w", e) }
