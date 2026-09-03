package rsql

import "fmt"

// parse turns an RSQL string into a syntax tree. Syntax only - selectors,
// operators and values are checked against a Schema by Bind.
func parse(s string) (node, error) {
	p := &parser{lex: &lexer{src: []rune(s)}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokEOF {
		return nil, fmt.Errorf("rsql: %d: unexpected %q", p.tok.pos, p.tok.text)
	}
	return n, nil
}

type parser struct {
	lex *lexer
	tok token
}

func (p *parser) advance() error {
	t, err := p.lex.next()
	if err != nil {
		return err
	}
	p.tok = t
	return nil
}

func (p *parser) parseOr() (node, error) {
	return p.parseBinary(tokOr, p.parseAnd, func(ns []node) node { return &or{Nodes: ns} })
}

func (p *parser) parseAnd() (node, error) {
	return p.parseBinary(tokAnd, p.parseConstraint, func(ns []node) node { return &and{Nodes: ns} })
}

func (p *parser) parseBinary(sep tokenKind, operand func() (node, error), join func([]node) node) (node, error) {
	first, err := operand()
	if err != nil {
		return nil, err
	}
	nodes := []node{first}
	for p.tok.kind == sep {
		if err := p.advance(); err != nil {
			return nil, err
		}
		n, err := operand()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 1 {
		return nodes[0], nil
	}
	return join(nodes), nil
}

// parseConstraint resolves the only place the grammar looks ambiguous: a "("
// here is always a group, because a selector is unreserved-str and "(" is
// reserved. Argument lists are reachable only after a comparison operator.
func (p *parser) parseConstraint() (node, error) {
	if p.tok.kind == tokLParen {
		if err := p.advance(); err != nil {
			return nil, err
		}
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tokRParen {
			return nil, fmt.Errorf("rsql: %d: expected %q", p.tok.pos, ")")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return n, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (node, error) {
	if p.tok.kind != tokValue {
		return nil, fmt.Errorf("rsql: %d: expected a selector, got %q", p.tok.pos, p.tok.text)
	}
	selector := p.tok.text
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind != tokOp {
		return nil, fmt.Errorf("rsql: %d: expected a comparison operator after %q", p.tok.pos, selector)
	}
	op, ok := operators[p.tok.text]
	if !ok {
		return nil, fmt.Errorf("rsql: %d: unknown operator %q", p.tok.pos, p.tok.text)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	args, err := p.parseArguments()
	if err != nil {
		return nil, err
	}
	return &comparison{Selector: selector, Op: op, Args: args}, nil
}

func (p *parser) parseArguments() ([]string, error) {
	if p.tok.kind != tokLParen {
		if p.tok.kind != tokValue {
			return nil, fmt.Errorf("rsql: %d: expected a value, got %q", p.tok.pos, p.tok.text)
		}
		v := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}
		return []string{v}, nil
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	var args []string
	for {
		if p.tok.kind != tokValue {
			return nil, fmt.Errorf("rsql: %d: expected a value, got %q", p.tok.pos, p.tok.text)
		}
		args = append(args, p.tok.text)
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.tok.kind == tokOr {
			if err := p.advance(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}
	if p.tok.kind != tokRParen {
		return nil, fmt.Errorf("rsql: %d: expected %q", p.tok.pos, ")")
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return args, nil
}
