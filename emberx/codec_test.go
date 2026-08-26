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

// fakeEntities is the named-slice form a service would return from a batch
// command; the unnamed []*fakeEntity form is covered alongside it.
type fakeEntities []*fakeEntity

func seedEntities() []*fakeEntity {
	a := &fakeEntity{EntityRoot: ember.NewEntityRoot("e1"), Name: "alice"}
	b := &fakeEntity{EntityRoot: ember.NewEntityRoot("e2"), Name: "bob"}
	b.SetVersion(ember.NewVersion(3))
	return []*fakeEntity{a, b}
}

func TestAdaptSliceRoundTripsNamedSlice(t *testing.T) {
	tc := AdaptSlice[fakeEntities](fakeMarshaler{})

	data, err := tc.Marshal(fakeEntities(seedEntities()))
	require.NoError(t, err)

	got, err := tc.Unmarshal(data)
	require.NoError(t, err)

	out, ok := got.(fakeEntities)
	require.True(t, ok, "must decode back to the registered type, got %T", got)
	require.Len(t, out, 2)
	require.Equal(t, "e1", out[0].ID())
	require.Equal(t, "alice", out[0].Name)
	require.Equal(t, "e2", out[1].ID())
	require.EqualValues(t, 3, out[1].Version().Value())
}

func TestAdaptSliceRoundTripsUnnamedSlice(t *testing.T) {
	tc := AdaptSlice[[]*fakeEntity](fakeMarshaler{})

	data, err := tc.Marshal(seedEntities())
	require.NoError(t, err)

	got, err := tc.Unmarshal(data)
	require.NoError(t, err)

	out, ok := got.([]*fakeEntity)
	require.True(t, ok, "got %T", got)
	require.Len(t, out, 2)
	require.Equal(t, "e1", out[0].ID())
	require.EqualValues(t, 3, out[1].Version().Value())
}

func TestAdaptSliceEmptySliceRoundTrips(t *testing.T) {
	tc := AdaptSlice[fakeEntities](fakeMarshaler{})

	data, err := tc.Marshal(fakeEntities{})
	require.NoError(t, err)

	got, err := tc.Unmarshal(data)
	require.NoError(t, err)
	require.Equal(t, fakeEntities{}, got)
}

func TestAdaptSliceWrongTypeErrors(t *testing.T) {
	tc := AdaptSlice[fakeEntities](fakeMarshaler{})

	_, err := tc.Marshal("not a slice of entities")
	require.Error(t, err)

	// The element type is right but the slice type is not the registered one.
	_, err = tc.Marshal(seedEntities())
	require.Error(t, err)
}
