package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"quantmesh/accounting"
)

// productionRecoverySourceWindowVerifier is installed once by StorageService.
// It validates the trusted collector's canonical, raw-event-bound completion
// digest and never makes exchange/network calls.
type productionRecoverySourceWindowVerifier struct{}

func (productionRecoverySourceWindowVerifier) VerifyRecoverySourceWindow(ctx context.Context, window accounting.RecoverySourceWindow, events []accounting.RecoveryEvent) error {
	if ctx == nil {
		return errors.New("recovery source verification requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := window.Validate(); err != nil {
		return err
	}
	switch window.Source {
	case accounting.RecoverySourceBinanceFuturesIncomeV1:
		if window.Kind != accounting.RecoveryFundingEvent || !strings.EqualFold(window.Exchange, "binance") || !strings.EqualFold(window.Market, "futures") || window.Symbol == "" ||
			!validRecoveryDigest(window.CollectorEvidenceHash) || window.InitialFrom.IsZero() || window.InitialFrom.After(window.From) || !validRecoveryDigest(window.HistoricalEvidenceHash) {
			return errors.New("Binance futures income source has incompatible dimensions")
		}
	case accounting.RecoverySourceBinanceMarginDebtV1:
		if (window.Kind != accounting.RecoveryBorrowEvent && window.Kind != accounting.RecoveryRepayEvent && window.Kind != accounting.RecoveryInterestEvent) ||
			!strings.EqualFold(window.Exchange, "binance") || !strings.EqualFold(window.Market, "spot_margin") || window.Symbol != "" {
			return errors.New("Binance margin debt source has incompatible dimensions")
		}
	default:
		return fmt.Errorf("unregistered trusted recovery source %q", window.Source)
	}
	computed, err := accounting.RecoverySourceWindowEvidenceHash(window, events)
	if err != nil {
		return err
	}
	if computed != window.EvidenceHash {
		return errors.New("trusted recovery source window digest does not match the exact rows")
	}
	return nil
}

func validRecoveryDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
