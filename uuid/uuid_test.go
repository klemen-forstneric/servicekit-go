package uuid_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klemen-forstneric/servicekit-go/uuid"
)

func TestIDerReturnsNonEmptyUniqueIDs(t *testing.T) {
	var d uuid.IDer
	a := d.ID()
	b := d.ID()
	assert.NotEmpty(t, a)
	assert.NotEmpty(t, b)
	assert.NotEqual(t, a, b)
}
