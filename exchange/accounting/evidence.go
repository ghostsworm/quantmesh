// Package accounting carries read-only account evidence without depending on
// exchange wrappers or risk policy. Optional providers must fail, not fabricate
// an empty ledger, when completeness/valuation cannot be established.
package accounting

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"time"
)

type Entry struct {
	ID, Kind, Currency, Amount, Symbol string
	At                                 time.Time
}

const (
	MaxClockSkew       = 2 * time.Second
	MaxCaptureDuration = 5 * time.Second
)

// Wallet's cursor is in the exchange's clock domain; it is NOT the local
// observation timestamp. Balances remain decimal strings for exact checking.
type Wallet struct {
	Balance    string    `json:"balance"`
	From       time.Time `json:"from"`
	Through    time.Time `json:"through"`
	ObservedAt time.Time `json:"observed_at"`
}

// Snapshot is candidate evidence. Exhausting endpoint pagination does not prove
// wallet/ledger consistency or absence of delayed exchange records. Consumers
// must reconcile it with persisted receipts before declaring data verified.
type Snapshot struct {
	Currency   string
	Equity     float64
	ObservedAt time.Time
	Wallet     Wallet
	Entries    []Entry
}

type Source interface {
	ReadAccountEvidence(context.Context, time.Time) (Snapshot, error)
}

// Bound the integer part as well as the input length so canonical 18-place
// values remain valid input. Otherwise a long integer can expand beyond the
// parser limit when fractional zeroes are appended.
var decimalPattern = regexp.MustCompile(`^[+-]?[0-9]{1,80}(?:\.[0-9]{1,18})?$`)

func Decimal(value string) (*big.Rat, error) {
	if len(value) > 100 || !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("invalid account decimal")
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid account decimal")
	}
	return r, nil
}

func CanonicalDecimal(value string) (string, error) {
	r, err := Decimal(value)
	if err != nil {
		return "", err
	}
	return r.FloatString(18), nil
}
