// Package mongox translates a bound RSQL filter into a MongoDB query document.
//
// The result is a plain map rather than bson.M so the package carries no driver
// dependency and works with v1 and v2 alike — bson.M is a map[string]any and
// the driver marshals either identically.
package mongox

import (
	"fmt"

	"github.com/klemen-forstneric/servicekit-go/rsql"
)

// Filter is a MongoDB query document.
type Filter = map[string]any

// ToFilter builds a query document. Values arrive already typed, so nothing
// here concatenates strings and there is no operator injection to defend
// against.
func ToFilter(b rsql.Bound) (Filter, error) {
	switch t := b.(type) {
	case *rsql.BoundAnd:
		parts, err := each(t.Nodes)
		if err != nil {
			return nil, err
		}
		return Filter{"$and": parts}, nil
	case *rsql.BoundOr:
		parts, err := each(t.Nodes)
		if err != nil {
			return nil, err
		}
		return Filter{"$or": parts}, nil
	case *rsql.BoundComparison:
		return comparison(t)
	}
	return nil, fmt.Errorf("mongox: unsupported node %T", b)
}

func each(nodes []rsql.Bound) ([]Filter, error) {
	out := make([]Filter, 0, len(nodes))
	for _, n := range nodes {
		m, err := ToFilter(n)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// comparison relies on Mongo already treating equality against an array field
// as "contains", so a repeated field needs no special case: tags=="a";tags=="b"
// is the intersection and tags=in=(a,b) the union.
func comparison(c *rsql.BoundComparison) (Filter, error) {
	col := c.Field.Column
	switch c.Op {
	case rsql.OpEq:
		return Filter{col: c.Values[0]}, nil
	case rsql.OpNeq:
		return Filter{col: Filter{"$ne": c.Values[0]}}, nil
	case rsql.OpGt:
		return Filter{col: Filter{"$gt": c.Values[0]}}, nil
	case rsql.OpGe:
		return Filter{col: Filter{"$gte": c.Values[0]}}, nil
	case rsql.OpLt:
		return Filter{col: Filter{"$lt": c.Values[0]}}, nil
	case rsql.OpLe:
		return Filter{col: Filter{"$lte": c.Values[0]}}, nil
	case rsql.OpIn:
		return Filter{col: Filter{"$in": c.Values}}, nil
	case rsql.OpNotIn:
		return Filter{col: Filter{"$nin": c.Values}}, nil
	}
	return nil, fmt.Errorf("mongox: unsupported operator %q", c.Op)
}
