package emberx

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/klemen-forstneric/ember"
	"github.com/stretchr/testify/require"
)

// fakeEntity is a minimal ember.Entity with method-only identity.
type fakeEntity struct {
	ember.EntityRoot
	Name string
}

func (e *fakeEntity) Type() string { return "fake" }

// fakeMarshaler stores name+revision in the data blob.
type fakeMarshaler struct{}

func (fakeMarshaler) Marshal(_ context.Context, e *fakeEntity) (*ember.MarshaledEntity, error) {
	data, err := json.Marshal(map[string]any{"name": e.Name, "revision": 0})
	if err != nil {
		return nil, err
	}
	return &ember.MarshaledEntity{ID: e.ID(), Type: e.Type(), Version: e.Version(), Data: data}, nil
}

func (fakeMarshaler) Unmarshal(_ context.Context, me *ember.MarshaledEntity) (*fakeEntity, error) {
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(me.Data, &m); err != nil {
		return nil, err
	}
	e := &fakeEntity{EntityRoot: ember.NewEntityRoot(me.ID), Name: m.Name}
	e.SetVersion(me.Version)
	return e, nil
}

func TestAdaptRoundTripPreservesIdentityAndVersion(t *testing.T) {
	tc := Adapt[*fakeEntity](fakeMarshaler{})

	e := &fakeEntity{EntityRoot: ember.NewEntityRoot("e1"), Name: "alice"}
	e.SetVersion(ember.NewVersion(7))

	data, err := tc.Marshal(e)
	require.NoError(t, err)

	got, err := tc.Unmarshal(data)
	require.NoError(t, err)

	ge := got.(*fakeEntity)
	require.Equal(t, "e1", ge.ID())
	require.Equal(t, "alice", ge.Name)
	require.Equal(t, uint64(7), ge.Version().Value())
}

func TestAdaptWrongTypeErrors(t *testing.T) {
	tc := Adapt[*fakeEntity](fakeMarshaler{})
	_, err := tc.Marshal("not an entity")
	require.Error(t, err)
}
