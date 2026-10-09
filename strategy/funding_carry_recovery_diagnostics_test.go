package strategy

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFundingCarryVisualizationExposesSafeRecoveryEvidence(t *testing.T) {
	borrowedAt := time.Date(2026, time.October, 9, 0, 0, 0, 123000000, time.UTC)
	strategy := &FundingCarryStrategy{
		executionRecoveryRequired:      true,
		startupRecoveryErr:             errors.New("sensitive exchange response must not be exposed"),
		runtimeStateErr:                errors.New("sensitive storage diagnostic must not be exposed"),
		marginCloseVerificationPending: true,
		marginRepayIntent:              &fundingCarryRepayIntent{},
		marginCoverIntent:              &fundingCarryCoverIntent{},
		unownedExposure:                true,
		intentInFlight:                 true,
		direction:                      DirectionReverse,
		spotQty:                        0.25,
		futQty:                         0.24,
		marginDebt:                     0.26,
		marginBorrowTransferID:         123456789,
		marginBorrowedAt:               borrowedAt,
	}

	got := strategy.GetVisualizationData()
	wantReasons := []string{
		fundingCarryRecoveryReasonExecution,
		fundingCarryRecoveryReasonStartup,
		fundingCarryRecoveryReasonPersistence,
		fundingCarryRecoveryReasonFinalClose,
		fundingCarryRecoveryReasonRepayment,
		fundingCarryRecoveryReasonCover,
		fundingCarryRecoveryReasonExposure,
		fundingCarryRecoveryReasonIntent,
	}
	if required, ok := got["reconciliation_required"].(bool); !ok || !required {
		t.Fatalf("reconciliation_required=%v, want true", got["reconciliation_required"])
	}
	if reasons, ok := got["reconciliation_reasons"].([]string); !ok || !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("reconciliation_reasons=%v, want %v", got["reconciliation_reasons"], wantReasons)
	}
	if got["margin_debt"] != 0.26 || got["margin_debt_basis"] != "durable_strategy_ledger_not_live_exchange_liability" ||
		got["margin_borrow_transfer_id"] != int64(123456789) || got["margin_borrowed_at"] != borrowedAt.Format(time.RFC3339Nano) ||
		got["spot_qty"] != 0.25 || got["futures_qty"] != 0.24 {
		t.Fatalf("recovery evidence missing or mislabeled: %#v", got)
	}
	encoded := fmt.Sprint(got)
	if strings.Contains(encoded, "sensitive exchange response") || strings.Contains(encoded, "sensitive storage diagnostic") {
		t.Fatalf("raw error escaped in recovery diagnostics: %s", encoded)
	}
}

func TestFundingCarryVisualizationDoesNotInventReconciliationReasons(t *testing.T) {
	strategy := &FundingCarryStrategy{}
	got := strategy.GetVisualizationData()
	if required, ok := got["reconciliation_required"].(bool); !ok || required {
		t.Fatalf("reconciliation_required=%v, want false", got["reconciliation_required"])
	}
	if reasons, ok := got["reconciliation_reasons"].([]string); !ok || len(reasons) != 0 {
		t.Fatalf("reconciliation_reasons=%v, want empty", got["reconciliation_reasons"])
	}
}
