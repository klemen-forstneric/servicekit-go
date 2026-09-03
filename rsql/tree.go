package rsql

// All conjoins nodes, flattening nested conjunctions and dropping nils. A
// single operand is returned as-is, so no adapter ever receives a one-armed
// BoundAnd.
func All(nodes ...Bound) Bound { return combine(nodes, false) }

// Any disjoins nodes, on the same terms as All.
func Any(nodes ...Bound) Bound { return combine(nodes, true) }

func combine(nodes []Bound, or bool) Bound {
	flat := make([]Bound, 0, len(nodes))
	for _, n := range nodes {
		switch t := n.(type) {
		case nil:
		case *BoundAnd:
			if or {
				flat = append(flat, t)
				continue
			}
			flat = append(flat, t.Nodes...)
		case *BoundOr:
			if !or {
				flat = append(flat, t)
				continue
			}
			flat = append(flat, t.Nodes...)
		default:
			flat = append(flat, n)
		}
	}
	switch len(flat) {
	case 0:
		return nil
	case 1:
		return flat[0]
	}
	if or {
		return &BoundOr{Nodes: flat}
	}
	return &BoundAnd{Nodes: flat}
}

// Rewrite rebuilds the tree bottom-up, replacing each node with fn's result.
// Returning nil drops the node, and its parent collapses through All/Any, so a
// predicate can be removed without leaving an empty conjunction behind.
func Rewrite(b Bound, fn func(Bound) Bound) Bound {
	if b == nil {
		return nil
	}
	switch t := b.(type) {
	case *BoundAnd:
		return fn(All(rewriteAll(t.Nodes, fn)...))
	case *BoundOr:
		return fn(Any(rewriteAll(t.Nodes, fn)...))
	}
	return fn(b)
}

func rewriteAll(nodes []Bound, fn func(Bound) Bound) []Bound {
	out := make([]Bound, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, Rewrite(n, fn))
	}
	return out
}
