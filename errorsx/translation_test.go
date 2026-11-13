package errorsx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klemen-forstneric/servicekit-go/errorsx"
)

var (
	errInternal   = errorsx.New("internal", "pkg: something went wrong")
	errNotFound   = errorsx.New("not_found", "pkg: not found")
	errEmpty      = errorsx.New("empty", "pkg: empty")
	errPrivate    = errorsx.New("private_missing", "pkg: private thing missing")
	errPublicGone = errorsx.New("gone", "api: gone")
)

func TestTranslation_ExposesAnErrorAsItself(t *testing.T) {
	tr := errorsx.NewTranslation(errInternal, errorsx.Expose(errNotFound, errEmpty))

	pub, ok := tr.Public(errEmpty)

	assert.True(t, ok)
	assert.Same(t, errEmpty, pub)
}

func TestTranslation_ExposesAnErrorAsAnother(t *testing.T) {
	tr := errorsx.NewTranslation(errInternal, errorsx.ExposeAs(errPrivate, errPublicGone))

	pub, ok := tr.Public(errPrivate)

	assert.True(t, ok)
	assert.Same(t, errPublicGone, pub)
}

func TestTranslation_ExposesAnUncodedError(t *testing.T) {
	conflict := errors.New("store: version conflict")
	tr := errorsx.NewTranslation(errInternal, errorsx.ExposeAs(conflict, errPublicGone))

	pub, ok := tr.Public(fmt.Errorf("save: %w", conflict))

	assert.True(t, ok)
	assert.Same(t, errPublicGone, pub)
}

func TestTranslation_PublishesNeverTheWrappedText(t *testing.T) {
	tr := errorsx.NewTranslation(errInternal, errorsx.Expose(errNotFound))

	pub, ok := tr.Public(fmt.Errorf("%w: provider said sk-123 is rate limited", errNotFound))

	assert.True(t, ok)
	assert.Equal(t, "pkg: not found", pub.Error())
	assert.Equal(t, "not_found", pub.Code())
}

func TestTranslation_FirstMatchWins(t *testing.T) {
	both := fmt.Errorf("%w and %w", errPrivate, errNotFound)
	tr := errorsx.NewTranslation(errInternal,
		errorsx.ExposeAs(errPrivate, errPublicGone),
		errorsx.Expose(errNotFound),
	)

	pub, _ := tr.Public(both)

	assert.Same(t, errPublicGone, pub)
}

func TestTranslation_FallsBackForAnythingElse(t *testing.T) {
	tr := errorsx.NewTranslation(errInternal, errorsx.Expose(errNotFound))

	for name, err := range map[string]error{
		"uncoded":         errors.New("x-ai: rate limit exceeded"),
		"coded, unlisted": errPrivate,
	} {
		t.Run(name, func(t *testing.T) {
			pub, ok := tr.Public(err)

			assert.False(t, ok)
			assert.Same(t, errInternal, pub)
		})
	}
}
