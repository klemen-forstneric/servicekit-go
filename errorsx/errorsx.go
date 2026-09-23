// Package errorsx gives errors a stable machine-readable code.
package errorsx

import "errors"

// Error
type Error struct {
	code string
	msg  string
}

func New(code, msg string) *Error {
	return &Error{code: code, msg: msg}
}

func (e *Error) Error() string { return e.msg }

func (e *Error) Code() string { return e.code }

// Code returns the code of the first coded error in err's chain, or "".
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.code
	}
	return ""
}
