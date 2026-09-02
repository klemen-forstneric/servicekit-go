package rsql

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokValue
	tokOp
	tokLParen
	tokRParen
	tokAnd
	tokOr
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

const reserved = `"'();,=!~<>`

type lexer struct {
	src []rune
	pos int
}

func (l *lexer) peek() (rune, bool) {
	if l.pos >= len(l.src) {
		return 0, false
	}
	return l.src[l.pos], true
}

func (l *lexer) next() (token, error) {
	for {
		r, ok := l.peek()
		if !ok {
			return token{kind: tokEOF, pos: l.pos}, nil
		}
		if unicode.IsSpace(r) {
			l.pos++
			continue
		}
		break
	}
	start := l.pos
	r := l.src[l.pos]
	switch r {
	case '(':
		l.pos++
		return token{tokLParen, "(", start}, nil
	case ')':
		l.pos++
		return token{tokRParen, ")", start}, nil
	case ';':
		l.pos++
		return token{tokAnd, ";", start}, nil
	case ',':
		l.pos++
		return token{tokOr, ",", start}, nil
	case '=', '!', '<', '>':
		return l.readOp()
	case '"', '\'':
		return l.readQuoted()
	}
	return l.readBare()
}

func (l *lexer) readOp() (token, error) {
	start := l.pos
	switch l.src[l.pos] {
	case '!':
		l.pos++
		if r, ok := l.peek(); !ok || r != '=' {
			return token{}, fmt.Errorf("rsql: %d: expected %q after %q", start, "=", "!")
		}
		l.pos++
		return token{tokOp, "!=", start}, nil
	case '<', '>':
		c := string(l.src[l.pos])
		l.pos++
		if r, ok := l.peek(); ok && r == '=' {
			l.pos++
			return token{tokOp, c + "=", start}, nil
		}
		return token{tokOp, c, start}, nil
	}
	// '=' : either "==" or "=word="
	l.pos++
	if r, ok := l.peek(); ok && r == '=' {
		l.pos++
		return token{tokOp, "==", start}, nil
	}
	var name strings.Builder
	for {
		r, ok := l.peek()
		if !ok {
			return token{}, fmt.Errorf("rsql: %d: unterminated operator", start)
		}
		if r == '=' {
			l.pos++
			break
		}
		if !unicode.IsLetter(r) {
			return token{}, fmt.Errorf("rsql: %d: invalid character %q in operator", l.pos, r)
		}
		name.WriteRune(r)
		l.pos++
	}
	return token{tokOp, "=" + name.String() + "=", start}, nil
}

func (l *lexer) readQuoted() (token, error) {
	start := l.pos
	q := l.src[l.pos]
	l.pos++
	var b strings.Builder
	for {
		r, ok := l.peek()
		if !ok {
			return token{}, fmt.Errorf("rsql: %d: unterminated quoted value", start)
		}
		l.pos++
		if r == '\\' {
			esc, ok := l.peek()
			if !ok {
				return token{}, fmt.Errorf("rsql: %d: trailing escape", l.pos)
			}
			l.pos++
			b.WriteRune(esc)
			continue
		}
		if r == q {
			return token{tokValue, b.String(), start}, nil
		}
		b.WriteRune(r)
	}
}

func (l *lexer) readBare() (token, error) {
	start := l.pos
	var b strings.Builder
	for {
		r, ok := l.peek()
		if !ok {
			break
		}
		if r == '\\' {
			l.pos++
			esc, ok := l.peek()
			if !ok {
				return token{}, fmt.Errorf("rsql: %d: trailing escape", l.pos)
			}
			l.pos++
			b.WriteRune(esc)
			continue
		}
		if unicode.IsSpace(r) || strings.ContainsRune(reserved, r) {
			break
		}
		b.WriteRune(r)
		l.pos++
	}
	if b.Len() == 0 {
		return token{}, fmt.Errorf("rsql: %d: unexpected character %q", start, l.src[start])
	}
	return token{tokValue, b.String(), start}, nil
}
