package rsql

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Split partitions a filter by Field.Store, so each part can run against the
// datastore that owns its attributes and the results be combined in stages.
//
// It returns an error when a disjunction mixes stores. `a_dyn;b_mongo` splits,
// because both halves must hold and the order of evaluation is free.
// `a_dyn,b_mongo` does not: satisfying either is enough, so neither store can
// narrow the other and recomposing the halves would mean scanning one of them
// whole. Rejecting is the honest answer — the alternative is a query that looks
// like it worked and quietly reads the collection.
func Split(b Bound) (map[string]Bound, error) {
	out := map[string]Bound{}
	if err := split(b, out); err != nil {
		return nil, err
	}
	return out, nil
}

func split(b Bound, out map[string]Bound) error {
	if and, ok := b.(*BoundAnd); ok {
		for _, n := range and.Nodes {
			if err := split(n, out); err != nil {
				return err
			}
		}
		return nil
	}
	st := stores(b)
	if len(st) > 1 {
		slices.Sort(st)
		return fmt.Errorf("rsql: cannot split %s: a disjunction spans stores %s",
			Canonical(b), strings.Join(st, ", "))
	}
	if len(st) == 0 {
		return nil
	}
	out[st[0]] = All(out[st[0]], b)
	return nil
}

// stores returns the distinct stores a subtree touches.
func stores(b Bound) []string {
	seen := map[string]struct{}{}
	Walk(b, func(n Bound) bool {
		if c, ok := n.(*BoundComparison); ok {
			seen[c.Field.Store] = struct{}{}
		}
		return true
	})
	return slices.Collect(maps.Keys(seen))
}
