package emberx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/klemen-forstneric/ember"
	sparkmw "github.com/klemen-forstneric/spark/middleware"
)

// Adapt bridges an ember entity marshaler into a spark TypeCodec, so command
// results that are entities round-trip through the same marshaler as the store
// (migrations included) without spark importing ember.
func Adapt[E ember.Entity](m ember.EntityMarshaler[E]) sparkmw.TypeCodec {
	return codec[E]{m: m}
}

type codec[E ember.Entity] struct {
	m ember.EntityMarshaler[E]
}

type entityBlob struct {
	ID      string          `json:"id"`
	Version uint64          `json:"version"`
	Data    json.RawMessage `json:"data"`
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
	return json.Marshal(entityBlob{ID: me.ID, Version: me.Version.Value(), Data: me.Data})
}

func (c codec[E]) Unmarshal(b []byte) (any, error) {
	var eb entityBlob
	if err := json.Unmarshal(b, &eb); err != nil {
		return nil, err
	}
	return c.m.Unmarshal(context.Background(), &ember.MarshaledEntity{
		ID:      eb.ID,
		Version: ember.NewVersion(eb.Version),
		Data:    eb.Data,
	})
}
