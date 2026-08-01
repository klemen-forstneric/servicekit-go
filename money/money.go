package money

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// ErrInvalidCurrency is returned when a currency is not a 3–5 character
// alphanumeric code.
var ErrInvalidCurrency = errors.New("money: invalid currency")

// Money is a general-purpose monetary amount. It carries no domain rules about
// where it is used — in particular it does not reject negative amounts, so it
// can represent a signed ledger movement as well as a balance. The currency is
// any 3–5 character alphanumeric code (fiat or synthetic token); no ISO 4217
// membership is enforced.
type Money struct {
	Amount   decimal.Decimal `json:"amount"`
	Currency string          `json:"currency"`
}

// New upper-cases and trims the currency, requires a 3–5 char alphanumeric code,
// and allows any sign for the amount.
func New(amount decimal.Decimal, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !validCurrency(currency) {
		return Money{}, fmt.Errorf("%w: %q is not a 3-5 character alphanumeric code", ErrInvalidCurrency, currency)
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func validCurrency(c string) bool {
	if len(c) < 3 || len(c) > 5 {
		return false
	}
	for _, r := range c {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func (m Money) IsZero() bool                  { return m.Amount.IsZero() }
func (m Money) IsNegative() bool              { return m.Amount.IsNegative() }
func (m Money) SameCurrency(other Money) bool { return m.Currency == other.Currency }

// Add returns the sum of the two amounts, keeping the receiver's currency. The
// caller is responsible for ensuring the currencies match (see SameCurrency).
func (m Money) Add(other Money) Money {
	return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}
}

// Zero returns a zero amount in the receiver's currency. It cannot fail: the
// currency is carried over as-is rather than re-parsed, so unlike New this adds
// no validation — the receiver is assumed already valid.
func (m Money) Zero() Money {
	return Money{Amount: decimal.Zero, Currency: m.Currency}
}

func (m Money) String() string {
	return fmt.Sprintf("%s %s", m.Amount.String(), m.Currency)
}
