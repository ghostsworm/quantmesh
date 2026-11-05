package storage

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// daily equity ledger amounts are persisted decimals, not display values. They
// are read row-by-row as text and canonicalized here without rounding. The
// limits match accounting/daily's decimal parser so the values accepted by the
// core digest are exactly the values accepted from SQL.
const (
	dailyLedgerMaxDecimalLength = 48
	dailyLedgerMaxDecimalDigits = 36
	dailyLedgerMaxDecimalScale  = 18
	dailyLedgerMaxExponent      = 40
)

// dailyLedgerPlainDecimalSyntax accepts only a decimal number with an optional
// exponent. big.Rat.SetString also accepts rationals such as "1/2"; those are
// not persisted DECIMAL values and must not be reinterpreted as money.
func dailyLedgerPlainDecimalSyntax(value string) bool {
	index := 0
	if index < len(value) && (value[index] == '+' || value[index] == '-') {
		index++
	}
	digits := 0
	dot := false
	for index < len(value) && value[index] != 'e' && value[index] != 'E' {
		switch ch := value[index]; {
		case ch == '.':
			if dot {
				return false
			}
			dot = true
		case ch >= '0' && ch <= '9':
			digits++
		default:
			return false
		}
		index++
	}
	if digits == 0 {
		return false
	}
	if index == len(value) {
		return true
	}
	index++ // exponent marker
	if index < len(value) && (value[index] == '+' || value[index] == '-') {
		index++
	}
	exponentDigits := 0
	for ; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
		exponentDigits++
	}
	return exponentDigits > 0
}

// dailyLedgerCanonicalDecimal converts a stored DECIMAL/REAL text form to a
// plain, canonical decimal string. Exponent notation produced by SQLite is
// expanded exactly; no missing precision is invented and no value is rounded.
func dailyLedgerCanonicalDecimal(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("empty decimal")
	}
	if len(value) > 64 {
		return "", fmt.Errorf("decimal exceeds input length limit")
	}
	if !dailyLedgerPlainDecimalSyntax(value) {
		return "", fmt.Errorf("value is not a plain decimal")
	}
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent := strings.TrimPrefix(strings.TrimPrefix(value[index+1:], "+"), "-")
		parsed, err := strconv.Atoi(exponent)
		if err != nil || parsed > dailyLedgerMaxExponent {
			return "", fmt.Errorf("decimal exponent is out of the supported range")
		}
	}
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return "", fmt.Errorf("value is not a decimal")
	}
	denominator := new(big.Int).Set(rat.Denom())
	two := big.NewInt(2)
	five := big.NewInt(5)
	remainder := new(big.Int)
	twos := 0
	for {
		remainder.Mod(denominator, two)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Quo(denominator, two)
		twos++
	}
	fives := 0
	for {
		remainder.Mod(denominator, five)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Quo(denominator, five)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("value is not a finite decimal")
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	if scale > dailyLedgerMaxDecimalScale {
		return "", fmt.Errorf("decimal exceeds %d fractional digits", dailyLedgerMaxDecimalScale)
	}
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	factor := new(big.Int).Quo(pow10, rat.Denom())
	scaled := new(big.Int).Mul(rat.Num(), factor)
	sign := ""
	if scaled.Sign() < 0 {
		sign = "-"
		scaled.Abs(scaled)
	}
	digits := scaled.String()
	var canonical string
	if scale == 0 {
		canonical = digits
	} else {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		integerPart := digits[:len(digits)-scale]
		fractionPart := strings.TrimRight(digits[len(digits)-scale:], "0")
		if fractionPart == "" {
			canonical = integerPart
		} else {
			canonical = integerPart + "." + fractionPart
		}
	}
	if canonical == "0" || canonical == "" {
		return "0", nil
	}
	digitCount := len(strings.ReplaceAll(canonical, ".", ""))
	if digitCount > dailyLedgerMaxDecimalDigits || len(canonical) > dailyLedgerMaxDecimalLength {
		return "", fmt.Errorf("decimal exceeds persisted precision limits")
	}
	return sign + canonical, nil
}

// dailyLedgerNegateDecimal returns the exact additive inverse of a canonical
// ledger decimal. It never rounds and collapses negative zero to zero.
func dailyLedgerNegateDecimal(canonical string) (string, error) {
	if canonical == "" {
		return "", fmt.Errorf("empty decimal")
	}
	if canonical == "0" {
		return "0", nil
	}
	if strings.HasPrefix(canonical, "-") {
		return canonical[1:], nil
	}
	return "-" + canonical, nil
}
