package errorsx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klemen-forstneric/servicekit-go/errorsx"
)

var errThing = errorsx.New("thing_missing", "pkg: thing missing")

func TestCode_ReadsThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("%w: detail", errThing)
	assert.Equal(t, "thing_missing", errorsx.Code(wrapped))
	assert.ErrorIs(t, wrapped, errThing)
	assert.Equal(t, "pkg: thing missing: detail", wrapped.Error())
}

func TestCode_EmptyForUncodedAndNil(t *testing.T) {
	assert.Equal(t, "", errorsx.Code(errors.New("plain")))
	assert.Equal(t, "", errorsx.Code(nil))
}
