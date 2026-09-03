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

// node is an untyped syntax node, the parser's output and the binder's input.
// Nothing here knows about a schema or a datastore.
type node interface {
	node()
}

type and struct{ Nodes []node }

type or struct{ Nodes []node }

type comparison struct {
	Selector string
	Op       Operator
	Args     []string
}

func (*and) node()        {}
func (*or) node()         {}
func (*comparison) node() {}
