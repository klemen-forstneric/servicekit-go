// Package dynamox translates a bound RSQL filter into a DynamoDB condition.
//
// A DynamoDB FilterExpression is applied after the read, so it reduces the
// payload but not the consumed capacity, and it cannot stand in for a key
// condition. Use this for the coarse creator filtering that already runs as a
// Scan — not as a substitute for an index.
package dynamox

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

	"github.com/klemen-forstneric/servicekit-go/rsql"
)

// ToCondition builds a condition suitable for a FilterExpression.
func ToCondition(b rsql.Bound) (expression.ConditionBuilder, error) {
	var zero expression.ConditionBuilder
	switch t := b.(type) {
	case *rsql.BoundAnd:
		parts, err := each(t.Nodes)
		if err != nil {
			return zero, err
		}
		return expression.And(parts[0], parts[1], parts[2:]...), nil
	case *rsql.BoundOr:
		parts, err := each(t.Nodes)
		if err != nil {
			return zero, err
		}
		return expression.Or(parts[0], parts[1], parts[2:]...), nil
	case *rsql.BoundComparison:
		return comparison(t)
	}
	return zero, fmt.Errorf("dynamox: unsupported node %T", b)
}

func each(nodes []rsql.Bound) ([]expression.ConditionBuilder, error) {
	if len(nodes) < 2 {
		return nil, fmt.Errorf("dynamox: a conjunction needs at least two operands, got %d", len(nodes))
	}
	out := make([]expression.ConditionBuilder, 0, len(nodes))
	for _, n := range nodes {
		c, err := ToCondition(n)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func comparison(c *rsql.BoundComparison) (expression.ConditionBuilder, error) {
	var zero expression.ConditionBuilder
	name := expression.Name(c.Field.Column)

	// A repeated attribute has no equality in Dynamo — membership is Contains,
	// which takes a string operand.
	if c.Field.Repeated {
		switch c.Op {
		case rsql.OpEq:
			return name.Contains(fmt.Sprint(c.Values[0])), nil
		case rsql.OpNeq:
			return name.Contains(fmt.Sprint(c.Values[0])).Not(), nil
		case rsql.OpIn:
			return anyContains(name, c.Values), nil
		case rsql.OpNotIn:
			return anyContains(name, c.Values).Not(), nil
		}
		return zero, fmt.Errorf("dynamox: operator %q is not supported on the repeated field %q", c.Op, c.Field.Column)
	}

	switch c.Op {
	case rsql.OpEq:
		return name.Equal(value(c.Values[0])), nil
	case rsql.OpNeq:
		return name.NotEqual(value(c.Values[0])), nil
	case rsql.OpGt:
		return name.GreaterThan(value(c.Values[0])), nil
	case rsql.OpGe:
		return name.GreaterThanEqual(value(c.Values[0])), nil
	case rsql.OpLt:
		return name.LessThan(value(c.Values[0])), nil
	case rsql.OpLe:
		return name.LessThanEqual(value(c.Values[0])), nil
	case rsql.OpIn, rsql.OpNotIn:
		operands := make([]expression.OperandBuilder, 0, len(c.Values))
		for _, v := range c.Values {
			operands = append(operands, value(v))
		}
		in := name.In(operands[0], operands[1:]...)
		if c.Op == rsql.OpNotIn {
			return in.Not(), nil
		}
		return in, nil
	}
	return zero, fmt.Errorf("dynamox: unsupported operator %q", c.Op)
}

func anyContains(name expression.NameBuilder, values []any) expression.ConditionBuilder {
	if len(values) == 1 {
		return name.Contains(fmt.Sprint(values[0]))
	}
	parts := make([]expression.ConditionBuilder, 0, len(values))
	for _, v := range values {
		parts = append(parts, name.Contains(fmt.Sprint(v)))
	}
	return expression.Or(parts[0], parts[1], parts[2:]...)
}

// value renders timestamps as RFC 3339 strings, which is how the tables in this
// repo store them; the default marshaller would emit a Unix number instead.
func value(v any) expression.ValueBuilder {
	if t, ok := v.(time.Time); ok {
		return expression.Value(t.UTC().Format(time.RFC3339))
	}
	return expression.Value(v)
}
