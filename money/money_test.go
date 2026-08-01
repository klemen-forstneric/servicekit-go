package money_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/money"
)

func TestNew_NormalizesCurrencyAndAllowsNegative(t *testing.T) {
	m, err := money.New(decimal.NewFromInt(-5), " usd ")
	require.NoError(t, err)
	assert.Equal(t, "USD", m.Currency)
	assert.True(t, m.IsNegative())
}

func TestNew_AcceptsTokensOfLength3To5(t *testing.T) {
	for _, cur := range []string{"USD", "SXT", "usdc", "WBTC0"} {
		m, err := money.New(decimal.NewFromInt(1), cur)
		require.NoError(t, err, cur)
		assert.Len(t, m.Currency, len(cur))
	}
}

func TestNew_RejectsInvalidCurrency(t *testing.T) {
	for _, cur := range []string{"US", "TOOLONG", "dollars", "U$D", ""} {
		_, err := money.New(decimal.NewFromInt(1), cur)
		require.ErrorIs(t, err, money.ErrInvalidCurrency, cur)
	}
}

func TestAdd_KeepsCurrencyAndSums(t *testing.T) {
	a, _ := money.New(decimal.NewFromInt(10), "USD")
	b, _ := money.New(decimal.NewFromInt(-3), "USD")
	sum := a.Add(b)
	assert.Equal(t, "USD", sum.Currency)
	assert.True(t, sum.Amount.Equal(decimal.NewFromInt(7)))
	assert.False(t, sum.IsNegative())
}

func TestSameCurrency(t *testing.T) {
	a, _ := money.New(decimal.NewFromInt(1), "USD")
	b, _ := money.New(decimal.NewFromInt(1), "EUR")
	assert.False(t, a.SameCurrency(b))
	assert.True(t, a.SameCurrency(a))
}

func TestZero_KeepsCurrencyAndClearsAmount(t *testing.T) {
	m, err := money.New(decimal.NewFromInt(10), "USD")
	require.NoError(t, err)

	z := m.Zero()

	assert.True(t, z.IsZero())
	assert.Equal(t, "USD", z.Currency)
	// Decimal holds a pointer, so confirm the receiver is untouched.
	assert.True(t, m.Amount.Equal(decimal.NewFromInt(10)))
}
