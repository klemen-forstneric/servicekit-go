package rsql

import (
	"fmt"
	"slices"
	"strconv"
	"time"
)

type Kind int

const (
	KindString Kind = iota
	KindInt
	KindFloat
	KindBool
	KindTime
)

// Field declares one filterable attribute. A selector absent from the Schema is
// a parse error, so the Schema is the allowlist — that is what keeps internal
// attributes unreachable from a caller-supplied filter.
type Field struct {
	// Column is the name the datastore knows. Defaults to the selector, so it
	// only needs setting when the wire name and the stored name differ.
	Column string
	Kind   Kind
	// Ops restricts operators further than Kind allows. Empty means every
	// operator valid for the Kind.
	Ops []Operator
	// Enum, when set, is the closed set of accepted values.
	Enum []string
}

type Schema map[string]Field

// Limits bound the shape of an accepted filter. Zero means unbounded.
type Limits struct {
	MaxDepth        int
	MaxDisjunctions int
	MaxComparisons  int
}

// Bound mirrors Node with selectors resolved to Fields and values coerced to
// their declared types. Datastore adapters consume this and never see a string.
type Bound interface {
	bound()
}

type BoundAnd struct{ Nodes []Bound }

type BoundOr struct{ Nodes []Bound }

type BoundComparison struct {
	// Selector is the wire-facing name, kept so errors and rewrites can talk
	// in the caller's vocabulary rather than the column's.
	Selector string
	Field    Field
	Op       Operator
	Values   []any
}

func (*BoundAnd) bound()        {}
func (*BoundOr) bound()         {}
func (*BoundComparison) bound() {}

func (k Kind) ops() []Operator {
	switch k {
	case KindBool:
		return []Operator{OpEq, OpNeq}
	case KindString:
		return []Operator{OpEq, OpNeq, OpIn, OpNotIn}
	default:
		return []Operator{OpEq, OpNeq, OpGt, OpGe, OpLt, OpLe, OpIn, OpNotIn}
	}
}

// Bind parses an RSQL expression and validates it against a Schema and Limits,
// coercing every value to its declared type. Syntax errors and schema errors
// both surface here; there is no half-bound intermediate to hold wrong.
func Bind(expr string, s Schema, lim Limits) (Bound, error) {
	n, err := parse(expr)
	if err != nil {
		return nil, err
	}
	b := binder{schema: s, limits: lim}
	out, err := b.walk(n, 1)
	if err != nil {
		return nil, err
	}
	if lim.MaxComparisons > 0 && b.comparisons > lim.MaxComparisons {
		return nil, fmt.Errorf("rsql: filter has %d comparisons, limit is %d", b.comparisons, lim.MaxComparisons)
	}
	if lim.MaxDisjunctions > 0 && b.disjunctions > lim.MaxDisjunctions {
		return nil, fmt.Errorf("rsql: filter has %d disjunctions, limit is %d", b.disjunctions, lim.MaxDisjunctions)
	}
	return out, nil
}

type binder struct {
	schema       Schema
	limits       Limits
	comparisons  int
	disjunctions int
}

func (b *binder) walk(n node, depth int) (Bound, error) {
	if b.limits.MaxDepth > 0 && depth > b.limits.MaxDepth {
		return nil, fmt.Errorf("rsql: filter nests deeper than the limit of %d", b.limits.MaxDepth)
	}
	switch t := n.(type) {
	case *and:
		nodes, err := b.walkAll(t.Nodes, depth)
		if err != nil {
			return nil, err
		}
		return &BoundAnd{Nodes: nodes}, nil
	case *or:
		b.disjunctions += len(t.Nodes) - 1
		nodes, err := b.walkAll(t.Nodes, depth)
		if err != nil {
			return nil, err
		}
		return &BoundOr{Nodes: nodes}, nil
	case *comparison:
		b.comparisons++
		return b.comparison(t)
	}
	return nil, fmt.Errorf("rsql: unsupported node %T", n)
}

func (b *binder) walkAll(ns []node, depth int) ([]Bound, error) {
	out := make([]Bound, 0, len(ns))
	for _, n := range ns {
		bn, err := b.walk(n, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, bn)
	}
	return out, nil
}

func (b *binder) comparison(c *comparison) (Bound, error) {
	f, ok := b.schema[c.Selector]
	if !ok {
		return nil, fmt.Errorf("rsql: %q is not a filterable field", c.Selector)
	}
	if f.Column == "" {
		f.Column = c.Selector
	}
	allowed := f.Ops
	if len(allowed) == 0 {
		allowed = f.Kind.ops()
	}
	if !slices.Contains(allowed, c.Op) {
		return nil, fmt.Errorf("rsql: operator %q is not allowed on %q", c.Op, c.Selector)
	}
	multi := c.Op == OpIn || c.Op == OpNotIn
	if !multi && len(c.Args) != 1 {
		return nil, fmt.Errorf("rsql: operator %q on %q takes one value, got %d", c.Op, c.Selector, len(c.Args))
	}
	values := make([]any, 0, len(c.Args))
	for _, raw := range c.Args {
		v, err := coerce(raw, f)
		if err != nil {
			return nil, fmt.Errorf("rsql: %q: %w", c.Selector, err)
		}
		values = append(values, v)
	}
	return &BoundComparison{Selector: c.Selector, Field: f, Op: c.Op, Values: values}, nil
}

func coerce(raw string, f Field) (any, error) {
	if len(f.Enum) > 0 && !slices.Contains(f.Enum, raw) {
		return nil, fmt.Errorf("%q is not one of %v", raw, f.Enum)
	}
	switch f.Kind {
	case KindString:
		return raw, nil
	case KindInt:
		return strconv.ParseInt(raw, 10, 64)
	case KindFloat:
		return strconv.ParseFloat(raw, 64)
	case KindBool:
		return strconv.ParseBool(raw)
	case KindTime:
		return time.Parse(time.RFC3339, raw)
	}
	return nil, fmt.Errorf("unsupported kind for %q", raw)
}
