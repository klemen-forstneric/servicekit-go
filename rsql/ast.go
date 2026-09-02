package rsql

// Operator is a comparison operator in canonical form. Aliases (`=eq=`, `<`,
// `=nin=`) collapse into these at parse time, so equivalent filters share one
// representation.
type Operator string

const (
	OpEq    Operator = "=="
	OpNeq   Operator = "!="
	OpGt    Operator = "=gt="
	OpGe    Operator = "=ge="
	OpLt    Operator = "=lt="
	OpLe    Operator = "=le="
	OpIn    Operator = "=in="
	OpNotIn Operator = "=out="
)

var operators = map[string]Operator{
	"==": OpEq, "=eq=": OpEq,
	"!=": OpNeq, "=ne=": OpNeq,
	">": OpGt, "=gt=": OpGt,
	">=": OpGe, "=ge=": OpGe,
	"<": OpLt, "=lt=": OpLt,
	"<=": OpLe, "=le=": OpLe,
	"=in=":  OpIn,
	"=out=": OpNotIn, "=nin=": OpNotIn,
}

// Node is an untyped syntax node. Parse produces these; nothing here knows
// about a schema or a datastore.
type Node interface {
	node()
}

type And struct{ Nodes []Node }

type Or struct{ Nodes []Node }

type Comparison struct {
	Selector string
	Op       Operator
	Args     []string
}

func (*And) node()        {}
func (*Or) node()         {}
func (*Comparison) node() {}
