package httpx

import (
	"net/http"

	"github.com/klemen-forstneric/servicekit-go/errorsx"
)

// Translation maps internal errors to a status and the error a client may see.
type Translation struct {
	statuses []statusRules
	fallback *errorsx.Error
}

type statusRules struct {
	status int
	errors errorsx.Translation
}

// StatusRule adds a status group to a Translation.
type StatusRule func(*Translation)

// NewTranslation publishes 500 and fallback for every error no group matches.
func NewTranslation(fallback *errorsx.Error, statuses ...StatusRule) Translation {
	t := Translation{fallback: fallback}
	for _, s := range statuses {
		s(&t)
	}
	return t
}

// Status publishes the errors its rules match with status.
func Status(status int, rules ...errorsx.TranslationRule) StatusRule {
	return func(t *Translation) {
		t.statuses = append(t.statuses, statusRules{status: status, errors: errorsx.NewTranslation(nil, rules...)})
	}
}

// Public is the status and public error for err, matched in group order; ok is false for the fallback.
func (t Translation) Public(err error) (status int, public *errorsx.Error, ok bool) {
	for _, s := range t.statuses {
		if public, ok := s.errors.Public(err); ok {
			return s.status, public, true
		}
	}
	return http.StatusInternalServerError, t.fallback, false
}
