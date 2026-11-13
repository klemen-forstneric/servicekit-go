package errorsx

import "errors"

// Translation maps internal errors to the errors a boundary publishes.
type Translation struct {
	rules    []rule
	fallback *Error
}

type rule struct {
	internal error
	public   *Error
}

// TranslationRule adds rules to a Translation.
type TranslationRule func(*Translation)

// NewTranslation publishes fallback for every error no rule matches.
func NewTranslation(fallback *Error, rules ...TranslationRule) Translation {
	t := Translation{fallback: fallback}
	for _, r := range rules {
		r(&t)
	}
	return t
}

// Expose publishes each error as itself.
func Expose(errs ...*Error) TranslationRule {
	return func(t *Translation) {
		for _, e := range errs {
			t.rules = append(t.rules, rule{internal: e, public: e})
		}
	}
}

// ExposeAs publishes internal as public.
func ExposeAs(internal error, public *Error) TranslationRule {
	return func(t *Translation) {
		t.rules = append(t.rules, rule{internal: internal, public: public})
	}
}

// Public is the published error for err, matched with errors.Is in rule order; ok is false for the fallback.
func (t Translation) Public(err error) (public *Error, ok bool) {
	for _, r := range t.rules {
		if errors.Is(err, r.internal) {
			return r.public, true
		}
	}
	return t.fallback, false
}
