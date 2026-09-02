package rsql

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Canonical renders a bound filter to a stable string. AND and OR are
// commutative, so their operands are sorted: `tags==a;tags==b` and
// `tags==b;tags==a` canonicalise identically and therefore share one cache key.
// The output is also what an error response should echo, since it is fully
// parenthesised and shows the parse the server actually chose.
func Canonical(b Bound) string {
	var sb strings.Builder
	writeCanonical(&sb, b)
	return sb.String()
}

func writeCanonical(sb *strings.Builder, b Bound) {
	switch t := b.(type) {
	case *BoundAnd:
		writeGroup(sb, ";", t.Nodes)
	case *BoundOr:
		writeGroup(sb, ",", t.Nodes)
	case *BoundComparison:
		// The selector, not the column: this is echoed to callers, so it has to
		// be re-parseable and must not leak storage names.
		sb.WriteString(t.Selector)
		sb.WriteString(string(t.Op))
		if len(t.Values) == 1 {
			sb.WriteString(literal(t.Values[0]))
			return
		}
		vals := make([]string, 0, len(t.Values))
		for _, v := range t.Values {
			vals = append(vals, literal(v))
		}
		sort.Strings(vals)
		sb.WriteString("(" + strings.Join(vals, ",") + ")")
	}
}

func writeGroup(sb *strings.Builder, sep string, nodes []Bound) {
	parts := make([]string, 0, len(nodes))
	for _, n := range nodes {
		parts = append(parts, Canonical(n))
	}
	sort.Strings(parts)
	sb.WriteString("(" + strings.Join(parts, sep) + ")")
}

// literal quotes anything that could not be lexed bare, so the canonical form
// stays valid RSQL and is safe to echo back as the filter the server parsed.
func literal(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format(time.RFC3339Nano)
	}
	s := fmt.Sprintf("%v", v)
	if !strings.ContainsAny(s, reserved) && !strings.ContainsAny(s, " \t\n") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
