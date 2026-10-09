package strategy

const (
	fundingCarryRecoveryReasonExecution   = "execution_reconciliation_required"
	fundingCarryRecoveryReasonStartup     = "startup_recovery_failed"
	fundingCarryRecoveryReasonPersistence = "runtime_state_write_failed"
	fundingCarryRecoveryReasonFinalClose  = "final_close_verification_pending"
	fundingCarryRecoveryReasonRepayment   = "repayment_confirmation_pending"
	fundingCarryRecoveryReasonCover       = "cover_order_reconciliation_pending"
	fundingCarryRecoveryReasonExposure    = "exposure_unknown"
	fundingCarryRecoveryReasonIntent      = "financial_intent_in_flight"
)

// Caller holds s.mu. These stable codes are operator hints only; they never
// include underlying exchange errors or assert that external exposure is flat.
func (s *FundingCarryStrategy) reconciliationReasonsLocked() []string {
	reasons := make([]string, 0, 8)
	if s.executionRecoveryRequired {
		reasons = append(reasons, fundingCarryRecoveryReasonExecution)
	}
	if s.startupRecoveryErr != nil {
		reasons = append(reasons, fundingCarryRecoveryReasonStartup)
	}
	if s.runtimeStateErr != nil {
		reasons = append(reasons, fundingCarryRecoveryReasonPersistence)
	}
	if s.marginCloseVerificationPending {
		reasons = append(reasons, fundingCarryRecoveryReasonFinalClose)
	}
	if s.marginRepayIntent != nil {
		reasons = append(reasons, fundingCarryRecoveryReasonRepayment)
	}
	if s.marginCoverIntent != nil || hasUnverifiedFundingCarryCover(s.marginCoverOrders) {
		reasons = append(reasons, fundingCarryRecoveryReasonCover)
	}
	if s.unownedExposure {
		reasons = append(reasons, fundingCarryRecoveryReasonExposure)
	}
	if s.intentInFlight {
		reasons = append(reasons, fundingCarryRecoveryReasonIntent)
	}
	return reasons
}
