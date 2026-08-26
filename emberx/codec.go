package emberx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/klemen-forstneric/ember"
	sparkmw "github.com/klemen-forstneric/spark/middleware"
)

// entityBlob
type entityBlob struct {
	ID      string          `json:"id"`
	Version uint64          `json:"version"`
	Data    json.RawMessage `json:"data"`
}

// Adapt bridges an ember entity marshaler into a spark TypeCodec, so command
// results that are entities round-trip through the same marshaler as the store
// (migrations included) without spark importing ember.
func Adapt[E ember.Entity](m ember.EntityMarshaler[E]) sparkmw.TypeCodec {
	return codec[E]{m: m}
}

// codec
type codec[E ember.Entity] struct {
	m ember.EntityMarshaler[E]
}

func (c codec[E]) Marshal(v any) ([]byte, error) {
	e, ok := v.(E)
	if !ok {
		return nil, fmt.Errorf("emberx: expected %T, got %T", *new(E), v)
	}

	me, err := c.m.Marshal(context.Background(), e)
	if err != nil {
		return nil, err
	}

	return json.Marshal(entityBlob{
		ID:      me.ID,
		Version: me.Version.Value(),
		Data:    me.Data,
	})
}

func (c codec[E]) Unmarshal(b []byte) (any, error) {
	return c.unmarshal(b)
}

func (c codec[E]) unmarshal(b []byte) (E, error) {
	var empty E

	var eb entityBlob
	if err := json.Unmarshal(b, &eb); err != nil {
		return empty, err
	}

	return c.m.Unmarshal(context.Background(), &ember.MarshaledEntity{
		ID:      eb.ID,
		Version: ember.NewVersion(eb.Version),
		Data:    eb.Data,
	})
}

// AdaptSlice is Adapt for a command result that is a slice of entities, so a
// batch result round-trips with every id and version intact. S is the exact
// slice type the handler returns — named (conversation.Messages) or not
// ([]*conversation.Message) — and must match what is registered, since the
// registry keys a slice on its full type name.
func AdaptSlice[S ~[]E, E ember.Entity](m ember.EntityMarshaler[E]) sparkmw.TypeCodec {
	return sliceCodec[S, E]{elem: codec[E]{m: m}}
}

// sliceCodec
type sliceCodec[S ~[]E, E ember.Entity] struct {
	elem codec[E]
}

func (c sliceCodec[S, E]) Marshal(v any) ([]byte, error) {
	s, ok := v.(S)
	if !ok {
		return nil, fmt.Errorf("emberx: expected %T, got %T", *new(S), v)
	}

	blobs := make([]json.RawMessage, 0, len(s))
	for _, e := range s {
		b, err := c.elem.Marshal(e)
		if err != nil {
			return nil, err
		}

		blobs = append(blobs, b)
	}

	return json.Marshal(blobs)
}

func (c sliceCodec[S, E]) Unmarshal(b []byte) (any, error) {
	var blobs []json.RawMessage
	if err := json.Unmarshal(b, &blobs); err != nil {
		return nil, err
	}

	s := make(S, 0, len(blobs))
	for _, blob := range blobs {
		e, err := c.elem.unmarshal(blob)
		if err != nil {
			return nil, err
		}

		s = append(s, e)
	}

	return s, nil
}
