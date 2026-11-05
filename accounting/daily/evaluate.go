package daily

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	reasonMethodMismatch          = "method_mismatch"
	reasonTimezoneMismatch        = "timezone_mismatch"
	reasonScopeIdentity           = "scope_identity"
	reasonScopeWallets            = "scope_wallet_set"
	reasonScopeHistory            = "scope_history_start"
	reasonScopeEffective          = "scope_effective_window"
	reasonSnapshotOpening         = "snapshot_opening_incomplete"
	reasonSnapshotClosing         = "snapshot_closing_incomplete"
	reasonSnapshotTime            = "snapshot_time"
	reasonSnapshotSource          = "snapshot_source"
	reasonSnapshotObserved        = "snapshot_observed_at"
	reasonSnapshotRevision        = "snapshot_scope_revision"
	reasonCoverageMissing         = "coverage_missing"
	reasonCoverageIncomplete      = "coverage_incomplete"
	reasonCoverageDigest          = "coverage_digest_invalid"
	reasonCoverageWindow          = "coverage_window"
	reasonCoveragePages           = "coverage_pages"
	reasonCoverageCount           = "coverage_count"
	reasonCoverageObserved        = "coverage_observed_at"
	reasonCoverageSource          = "coverage_source_unsupported"
	reasonCoverageWallet          = "coverage_wallet_out_of_scope"
	reasonCoverageNow             = "coverage_now_too_early"
	reasonCoverageInterestHistory = "coverage_interest_history"
	reasonCoverageInterestLag     = "coverage_interest_lag"
	reasonFuturesOutOfScope       = "futures_symbol_out_of_scope"
	reasonFuturesMissing          = "futures_symbol_missing"
	reasonExecutionCoverage       = "execution_coverage_missing"
	reasonFundingCoverage         = "funding_coverage_missing"
	reasonInterestCoverage        = "interest_coverage_missing"
	reasonBalanceDuplicate        = "balance_duplicate"
	reasonBalanceWallet           = "balance_wallet_out_of_scope"
	reasonBalanceAsset            = "balance_asset_missing"
	reasonBalanceMissing          = "balance_missing"
	reasonBalanceAssetChanged     = "balance_asset_changed"
	reasonBalancePriceMissing     = "balance_price_missing"
	reasonBalancePriceInvalid     = "balance_price_invalid"
	reasonBalanceUSDTPrice        = "balance_usdt_price"
	reasonBalancePriceEvidence    = "balance_price_evidence"
	reasonBalanceLiability        = "balance_liability_negative"
	reasonBalanceQuantity         = "balance_quantity_negative"
	reasonBalanceNonUSDTLiability = "balance_non_usdt_liability"
	reasonBalanceNonUSDTNotFixed  = "balance_non_usdt_not_constant"
	reasonBalanceNonUSDTFlow      = "balance_non_usdt_flow"
	reasonEventOutside            = "event_time_outside"
	reasonEventWallet             = "event_wallet_out_of_scope"
	reasonEventAsset              = "event_asset_unsupported"
	reasonEventUnknown            = "event_unknown"
	reasonEventEconomicConflict   = "economic_id_conflict"
	reasonEventLedgerMissing      = "event_ledger_missing"
	reasonEventLedgerMismatch     = "event_ledger_mismatch"
	reasonEventPnlAmount          = "event_pnl_amount"
	reasonEventInterestAccrual    = "event_interest_accrual"
	reasonEventSettlement         = "event_settlement"
	reasonEventPrincipalDelta     = "event_principal_delta"
	reasonEventDelta              = "event_delta_unexpected"
	reasonEventSign               = "event_sign"
	reasonEventTransfer           = "event_transfer_conflict"
	reasonEventTransferLeg        = "event_transfer_missing_leg"
	reasonEventTransferCounter    = "event_transfer_counterparty"
	reasonEventOtherFee           = "event_other_fee"
	reasonWalletQuantity          = "wallet_quantity_mismatch"
	reasonWalletPrincipal         = "wallet_principal_mismatch"
	reasonWalletInterest          = "wallet_interest_mismatch"
	reasonLedgerUnknown           = "ledger_unknown"
	reasonLedgerAmountMissing     = "ledger_amount_missing"
	reasonLedgerAsset             = "ledger_asset"
	reasonLedgerMarket            = "ledger_market_unsupported"
	reasonLedgerKind              = "ledger_kind_unsupported"
	reasonLedgerTime              = "ledger_time_outside"
	reasonLedgerFeeSign           = "ledger_fee_sign"
	reasonLedgerInterestSign      = "ledger_interest_sign"
	reasonLedgerUnmatched         = "ledger_unmatched"
	reasonPositionSymbol          = "position_symbol_missing"
	reasonPositionDuplicate       = "position_duplicate"
	reasonPositionPriceMissing    = "position_price_missing"
	reasonPositionPriceEvidence   = "position_price_evidence"
	reasonPositionEntry           = "position_entry_price"
	reasonPositionMark            = "position_mark_price"
	reasonPositionUnrealized      = "position_unrealized_mismatch"
	reasonPositionFill            = "position_fill_evidence"
	reasonPositionVolume          = "position_volume_mismatch"
	reasonResidual                = "residual_exceeds_tolerance"
)

type ledgerFacts struct {
	tradingGross *big.Rat
	tradingFees  *big.Rat
	fundingNet   *big.Rat
	interestCost *big.Rat
	otherCosts   *big.Rat
	volume       map[string]*big.Rat
}

type parsedBalance struct {
	raw       Balance
	quantity  *big.Rat
	principal *big.Rat
	interest  *big.Rat
	price     *big.Rat
}

type parsedPosition struct {
	raw        Position
	quantity   *big.Rat
	entryPrice *big.Rat
	markPrice  *big.Rat
	unrealized *big.Rat
}

type validator struct {
	now   time.Time
	start time.Time
	end   time.Time
	scope Scope

	reasons []string
	seen    map[string]struct{}

	walletSet      map[string]struct{}
	openingBalance map[string]map[string]parsedBalance
	closingBalance map[string]map[string]parsedBalance
	openingPos     map[string]parsedPosition
	closingPos     map[string]parsedPosition

	entries    []LedgerEntry
	ledgerByID map[string]LedgerEntry

	coverage      []Coverage
	validCoverage []Coverage
	ledgerCov     []Coverage

	futuresSymbols map[string]struct{}

	externalCapital   *big.Rat
	valuationChange   *big.Rat
	openingEquity     *big.Rat
	closingEquity     *big.Rat
	openingUnrealized *big.Rat
	closingUnrealized *big.Rat
}

// Evaluate verifies one closed account day from a full input snapshot. It
// never calls an exchange. Structural damage returns ErrInvalid/ErrConflict;
// incomplete or unsupported business facts return a Result with
// EquityReconciliationComplete=false and stable reason codes.
func Evaluate(inputs Inputs, now time.Time) (Result, error) {
	if now.IsZero() {
		return Result{}, fmt.Errorf("%w: evaluation time is required", ErrInvalid)
	}
	evidence, err := PrepareEvidence(inputs.Evidence)
	if err != nil {
		return Result{}, err
	}
	entries, err := normalizeEntries(inputs.Entries)
	if err != nil {
		return Result{}, err
	}
	ledgerCoverage, err := normalizeCoverageList(inputs.LedgerCoverage)
	if err != nil {
		return Result{}, err
	}
	start, end, err := DayBounds(evidence.Date)
	if err != nil {
		return Result{}, err
	}
	digest, err := InputDigest(inputs)
	if err != nil {
		return Result{}, err
	}
	coverage, err := mergeCoverage(evidence.Coverage, ledgerCoverage)
	if err != nil {
		return Result{}, err
	}

	value := &validator{
		now:               now.UTC(),
		start:             start,
		end:               end,
		scope:             evidence.Scope,
		seen:              make(map[string]struct{}),
		walletSet:         make(map[string]struct{}),
		ledgerByID:        make(map[string]LedgerEntry, len(entries)),
		coverage:          coverage,
		ledgerCov:         ledgerCoverage,
		entries:           entries,
		futuresSymbols:    make(map[string]struct{}),
		externalCapital:   new(big.Rat),
		valuationChange:   new(big.Rat),
		openingEquity:     new(big.Rat),
		closingEquity:     new(big.Rat),
		openingUnrealized: new(big.Rat),
		closingUnrealized: new(big.Rat),
	}
	for _, entry := range entries {
		value.ledgerByID[entry.ID] = entry
	}

	value.validateIdentity(evidence)
	value.validateScope(evidence.Scope)
	value.validateSnapshot("opening", evidence.Opening, start)
	value.validateSnapshot("closing", evidence.Closing, end)
	value.validateCoverage(evidence)
	value.validateFuturesSymbols(evidence)

	value.openingBalance = value.collectBalances(evidence.Opening)
	value.closingBalance = value.collectBalances(evidence.Closing)
	value.openingPos = value.collectPositions(evidence.Opening)
	value.closingPos = value.collectPositions(evidence.Closing)
	value.validateBalances(eventAssets(evidence.Events))

	facts := value.validateLedger(evidence)
	value.validateEvents(evidence.Events, facts)
	value.validatePositionVolume(facts)
	value.validateEquity()

	return value.buildResult(inputs, evidence, digest, facts), nil
}

func (v *validator) fail(reason string) {
	if _, exists := v.seen[reason]; exists {
		return
	}
	v.seen[reason] = struct{}{}
	v.reasons = append(v.reasons, reason)
}

func (v *validator) validateIdentity(evidence Evidence) {
	if evidence.Method != MethodVersion {
		v.fail(reasonMethodMismatch)
	}
	if evidence.Timezone != Timezone {
		v.fail(reasonTimezoneMismatch)
	}
	if evidence.Scope.Exchange == "" || evidence.Scope.AccountScope == "" {
		v.fail(reasonScopeIdentity)
	}
}

func (v *validator) validateScope(scope Scope) {
	required := []string{WalletSpot, WalletMargin, WalletFutures}
	allowed := make(map[string]struct{}, len(required))
	for _, wallet := range required {
		allowed[wallet] = struct{}{}
	}
	if len(scope.Wallets) != len(required) {
		v.fail(reasonScopeWallets)
	}
	seenWallets := make(map[string]struct{}, len(scope.Wallets))
	for _, wallet := range scope.Wallets {
		if wallet == "" {
			v.fail(reasonScopeWallets)
			continue
		}
		if _, exists := seenWallets[wallet]; exists {
			v.fail(reasonScopeWallets)
			continue
		}
		seenWallets[wallet] = struct{}{}
		if _, ok := allowed[wallet]; !ok {
			v.fail(reasonScopeWallets)
			continue
		}
		v.walletSet[wallet] = struct{}{}
	}
	for _, wallet := range required {
		if _, exists := seenWallets[wallet]; !exists {
			v.fail(reasonScopeWallets)
		}
	}
	if scope.HistoryStart.IsZero() || scope.HistoryStart.After(v.start) {
		v.fail(reasonScopeHistory)
	}
	if scope.EffectiveFrom.IsZero() || scope.EffectiveFrom.After(v.start) || scope.EffectiveThrough.IsZero() || scope.EffectiveThrough.Before(v.end) {
		v.fail(reasonScopeEffective)
	}
}

func (v *validator) validateSnapshot(label string, snapshot Snapshot, at time.Time) {
	if !snapshot.Complete {
		if label == "opening" {
			v.fail(reasonSnapshotOpening)
		} else {
			v.fail(reasonSnapshotClosing)
		}
	}
	if snapshot.At.IsZero() || !snapshot.At.Equal(at) {
		v.fail(reasonSnapshotTime)
	}
	if snapshot.SourceID == "" {
		v.fail(reasonSnapshotSource)
	}
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Before(snapshot.At) || snapshot.ObservedAt.After(v.now) {
		v.fail(reasonSnapshotObserved)
	}
	if snapshot.ScopeRevision != v.scope.Revision {
		v.fail(reasonSnapshotRevision)
	}
}

func (v *validator) collectBalances(snapshot Snapshot) map[string]map[string]parsedBalance {
	out := make(map[string]map[string]parsedBalance, len(v.walletSet))
	for wallet := range v.walletSet {
		out[wallet] = make(map[string]parsedBalance)
	}
	for _, balance := range snapshot.Balances {
		if _, ok := v.walletSet[balance.Wallet]; !ok {
			v.fail(reasonBalanceWallet)
			continue
		}
		if balance.Asset == "" {
			v.fail(reasonBalanceAsset)
			continue
		}
		if _, exists := out[balance.Wallet][balance.Asset]; exists {
			v.fail(reasonBalanceDuplicate)
			continue
		}
		parsed := parsedBalance{raw: balance}
		if parsed.quantity, _ = parseOptionalDecimalInput(balance.Quantity); parsed.quantity == nil {
			v.fail(reasonBalanceMissing)
		}
		if parsed.price, _ = parseOptionalDecimalInput(balance.Price); parsed.price == nil {
			v.fail(reasonBalancePriceMissing)
		}
		if parsed.principal, _ = parseZeroIfEmpty(balance.Principal); parsed.principal == nil {
			v.fail(reasonBalanceMissing)
		}
		if parsed.interest, _ = parseZeroIfEmpty(balance.Interest); parsed.interest == nil {
			v.fail(reasonBalanceMissing)
		}
		out[balance.Wallet][balance.Asset] = parsed
	}
	return out
}

func parseOptionalDecimalInput(value string) (*big.Rat, error) {
	if value == "" {
		return nil, fmt.Errorf("%w: empty decimal", ErrInvalid)
	}
	return parseDecimal(value)
}

func parseZeroIfEmpty(value string) (*big.Rat, error) {
	if value == "" {
		return new(big.Rat), nil
	}
	return parseDecimal(value)
}

func (v *validator) collectPositions(snapshot Snapshot) map[string]parsedPosition {
	out := make(map[string]parsedPosition, len(snapshot.Positions))
	for _, position := range snapshot.Positions {
		if position.Symbol == "" {
			v.fail(reasonPositionSymbol)
			continue
		}
		if _, exists := out[position.Symbol]; exists {
			v.fail(reasonPositionDuplicate)
			continue
		}
		parsed := parsedPosition{raw: position}
		if parsed.quantity, _ = parseOptionalDecimalInput(position.Quantity); parsed.quantity == nil {
			v.fail(reasonPositionPriceMissing)
		}
		if parsed.entryPrice, _ = parseOptionalDecimalInput(position.EntryPrice); parsed.entryPrice == nil {
			v.fail(reasonPositionEntry)
		}
		if parsed.markPrice, _ = parseOptionalDecimalInput(position.MarkPrice); parsed.markPrice == nil {
			v.fail(reasonPositionMark)
		}
		if parsed.unrealized, _ = parseOptionalDecimalInput(position.Unrealized); parsed.unrealized == nil {
			v.fail(reasonPositionUnrealized)
		}
		if parsed.quantity != nil {
			if parsed.quantity.Sign() != 0 {
				if parsed.entryPrice == nil || parsed.entryPrice.Sign() <= 0 {
					v.fail(reasonPositionEntry)
				}
				if parsed.markPrice == nil || parsed.markPrice.Sign() <= 0 {
					v.fail(reasonPositionMark)
				}
			}
			if parsed.entryPrice != nil && parsed.markPrice != nil && parsed.unrealized != nil {
				expected := new(big.Rat).Mul(parsed.quantity, new(big.Rat).Sub(parsed.markPrice, parsed.entryPrice))
				if expected.Cmp(parsed.unrealized) != 0 {
					v.fail(reasonPositionUnrealized)
				}
			}
		}
		if position.PriceSource == "" || position.PriceAt.IsZero() || position.PriceAt.After(snapshot.At) || position.PriceAt.After(v.now) {
			v.fail(reasonPositionPriceEvidence)
		}
		out[position.Symbol] = parsed
	}
	return out
}

func (v *validator) validateBalances(eventAssetSet map[string]struct{}) {
	for _, wallet := range sortedSet(v.walletSet) {
		opening := v.openingBalance[wallet]
		closing := v.closingBalance[wallet]
		assets := make(map[string]struct{}, len(opening)+len(closing))
		for asset := range opening {
			assets[asset] = struct{}{}
		}
		for asset := range closing {
			assets[asset] = struct{}{}
		}
		if _, ok := opening[Asset]; !ok {
			v.fail(reasonBalanceMissing)
		}
		if _, ok := closing[Asset]; !ok {
			v.fail(reasonBalanceMissing)
		}
		for _, asset := range sortedSet(assets) {
			openBalance, hasOpen := opening[asset]
			closeBalance, hasClose := closing[asset]
			if !hasOpen || !hasClose {
				v.fail(reasonBalanceAssetChanged)
				continue
			}
			if openBalance.quantity == nil || closeBalance.quantity == nil ||
				openBalance.price == nil || closeBalance.price == nil ||
				openBalance.principal == nil || closeBalance.principal == nil ||
				openBalance.interest == nil || closeBalance.interest == nil {
				v.fail(reasonBalanceMissing)
				continue
			}
			if openBalance.price.Sign() <= 0 || closeBalance.price.Sign() <= 0 {
				v.fail(reasonBalancePriceInvalid)
				continue
			}
			if openBalance.principal.Sign() < 0 || closeBalance.principal.Sign() < 0 ||
				openBalance.interest.Sign() < 0 || closeBalance.interest.Sign() < 0 {
				v.fail(reasonBalanceLiability)
			}
			if openBalance.quantity.Sign() < 0 || closeBalance.quantity.Sign() < 0 {
				// Balance.Quantity is gross assets; debt belongs in Principal.
				v.fail(reasonBalanceQuantity)
			}
			if asset == Asset {
				if openBalance.price.Cmp(big.NewRat(1, 1)) != 0 || closeBalance.price.Cmp(big.NewRat(1, 1)) != 0 {
					v.fail(reasonBalanceUSDTPrice)
				}
			} else {
				if openBalance.principal.Sign() != 0 || closeBalance.principal.Sign() != 0 ||
					openBalance.interest.Sign() != 0 || closeBalance.interest.Sign() != 0 {
					v.fail(reasonBalanceNonUSDTLiability)
				}
				if openBalance.quantity.Cmp(closeBalance.quantity) != 0 {
					v.fail(reasonBalanceNonUSDTNotFixed)
				}
				if _, flows := eventAssetSet[asset]; flows {
					v.fail(reasonBalanceNonUSDTFlow)
				}
				if openBalance.quantity.Sign() != 0 {
					if openBalance.raw.PriceSource == "" || openBalance.raw.PriceAt.IsZero() ||
						openBalance.raw.PriceAt.After(v.openingTime()) || openBalance.raw.PriceAt.After(v.now) {
						v.fail(reasonBalancePriceEvidence)
					}
					if closeBalance.raw.PriceSource == "" || closeBalance.raw.PriceAt.IsZero() ||
						closeBalance.raw.PriceAt.After(v.closingTime()) || closeBalance.raw.PriceAt.After(v.now) {
						v.fail(reasonBalancePriceEvidence)
					}
				}
				deltaPrice := new(big.Rat).Sub(closeBalance.price, openBalance.price)
				valuation := new(big.Rat).Mul(openBalance.quantity, deltaPrice)
				v.valuationChange.Add(v.valuationChange, valuation)
			}
			openValue := new(big.Rat).Mul(openBalance.quantity, openBalance.price)
			openValue.Sub(openValue, openBalance.principal)
			openValue.Sub(openValue, openBalance.interest)
			v.openingEquity.Add(v.openingEquity, openValue)

			closeValue := new(big.Rat).Mul(closeBalance.quantity, closeBalance.price)
			closeValue.Sub(closeValue, closeBalance.principal)
			closeValue.Sub(closeValue, closeBalance.interest)
			v.closingEquity.Add(v.closingEquity, closeValue)
		}
	}
}

func (v *validator) openingTime() time.Time { return v.start }
func (v *validator) closingTime() time.Time { return v.end }

func eventAssets(events []Event) map[string]struct{} {
	out := make(map[string]struct{})
	for _, event := range events {
		if event.Asset != "" {
			out[event.Asset] = struct{}{}
		}
	}
	return out
}

func (v *validator) validateEquity() {
	for _, position := range v.openingPos {
		if position.unrealized != nil {
			v.openingUnrealized.Add(v.openingUnrealized, position.unrealized)
		}
	}
	for _, position := range v.closingPos {
		if position.unrealized != nil {
			v.closingUnrealized.Add(v.closingUnrealized, position.unrealized)
		}
	}
	v.openingEquity.Add(v.openingEquity, v.openingUnrealized)
	v.closingEquity.Add(v.closingEquity, v.closingUnrealized)
}

func (v *validator) validateCoverage(evidence Evidence) {
	if v.now.Before(v.end.Add(SafetyLag)) {
		v.fail(reasonCoverageNow)
	}
	for _, receipt := range v.coverage {
		if v.validateReceipt(receipt) {
			v.validCoverage = append(v.validCoverage, receipt)
		}
	}

	for _, wallet := range []string{WalletSpot, WalletMargin, WalletFutures} {
		if !v.hasWalletCoverage(SourceCashflow, wallet) {
			v.fail(reasonCoverageMissing)
		}
	}
	if !v.hasWalletCoverage(SourcePositions, WalletFutures) {
		v.fail(reasonCoverageMissing)
	}

	if !v.hasWalletCoverage(SourceExecution, WalletSpot) || !v.hasWalletCoverage(SourceExecution, WalletMargin) {
		v.fail(reasonExecutionCoverage)
	}
	symbols := sortedSet(v.futuresSymbols)
	if !v.hasWalletCoverage(SourceExecution, WalletFutures) && !v.hasAllSymbolCoverage(SourceExecution, WalletFutures, symbols, v.validCoverage) {
		v.fail(reasonExecutionCoverage)
	}
	if !v.hasWalletCoverage(SourceFunding, WalletFutures) && !v.hasAllSymbolCoverage(SourceFunding, WalletFutures, symbols, v.validCoverage) {
		v.fail(reasonFundingCoverage)
	}
	if !v.hasWalletCoverage(SourceInterest, WalletMargin) {
		v.fail(reasonInterestCoverage)
	}

	// The real ledger coverage must prove each discovered futures symbol and
	// the account-wide margin_interest query. One symbol cannot prove the rest.
	for _, symbol := range symbols {
		if !v.hasLedgerSymbolCoverage(SourceExecution, WalletFutures, symbol) {
			v.fail(reasonExecutionCoverage)
		}
		if !v.hasLedgerSymbolCoverage(SourceFunding, WalletFutures, symbol) {
			v.fail(reasonFundingCoverage)
		}
	}
	if !v.hasLedgerWildcardCoverage(SourceInterest, WalletMargin) {
		v.fail(reasonInterestCoverage)
	}
}

func (v *validator) validateReceipt(receipt Coverage) bool {
	valid := true
	switch receipt.Source {
	case SourceCashflow, SourceExecution, SourceFunding, SourceInterest, SourcePositions:
	default:
		v.fail(reasonCoverageSource)
		valid = false
	}
	if _, ok := v.walletSet[receipt.Wallet]; !ok {
		v.fail(reasonCoverageWallet)
		valid = false
	}
	if !validDigest(receipt.Digest) {
		v.fail(reasonCoverageDigest)
		valid = false
	}
	if !receipt.Complete {
		v.fail(reasonCoverageIncomplete)
		valid = false
	}
	if receipt.Pages <= 0 {
		v.fail(reasonCoveragePages)
		valid = false
	}
	if receipt.ExpectedCount < 0 || receipt.ActualCount < 0 || receipt.ExpectedCount != receipt.ActualCount {
		v.fail(reasonCoverageCount)
		valid = false
	}
	if receipt.From.IsZero() || receipt.Through.IsZero() || receipt.From.After(v.start) || receipt.Through.Before(v.end) {
		v.fail(reasonCoverageWindow)
		valid = false
	}
	if receipt.ObservedAt.IsZero() || receipt.ObservedAt.Before(receipt.Through) || receipt.ObservedAt.After(v.now) {
		v.fail(reasonCoverageObserved)
		valid = false
	}
	if receipt.Source == SourceInterest {
		if !receipt.ObservedAt.IsZero() && receipt.Through.After(receipt.ObservedAt.Add(-SafetyLag)) {
			v.fail(reasonCoverageInterestLag)
			valid = false
		}
		if !receipt.ObservedAt.IsZero() && v.start.Before(receipt.ObservedAt.Add(-InterestHistoryLimit)) {
			v.fail(reasonCoverageInterestHistory)
			valid = false
		}
		if !receipt.ObservedAt.IsZero() && receipt.From.Before(receipt.ObservedAt.Add(-InterestHistoryLimit)) {
			v.fail(reasonCoverageInterestHistory)
			valid = false
		}
	}
	return valid
}

func validDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32
}

func (v *validator) hasWalletCoverage(source, wallet string) bool {
	for _, receipt := range v.validCoverage {
		if receipt.Source == source && receipt.Wallet == wallet && (receipt.Symbol == "" || receipt.Symbol == "*") {
			return true
		}
	}
	return false
}

func (v *validator) hasAllSymbolCoverage(source, wallet string, symbols []string, pool []Coverage) bool {
	if len(symbols) == 0 {
		return false
	}
	for _, symbol := range symbols {
		found := false
		for _, receipt := range pool {
			if receipt.Source == source && receipt.Wallet == wallet &&
				(receipt.Symbol == symbol || receipt.Symbol == "" || receipt.Symbol == "*") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (v *validator) hasLedgerSymbolCoverage(source, wallet, symbol string) bool {
	for _, receipt := range v.ledgerCov {
		if receipt.Source == source && receipt.Wallet == wallet &&
			(receipt.Symbol == symbol || receipt.Symbol == "" || receipt.Symbol == "*") {
			return true
		}
	}
	return false
}

func (v *validator) hasLedgerWildcardCoverage(source, wallet string) bool {
	for _, receipt := range v.ledgerCov {
		if receipt.Source == source && receipt.Wallet == wallet && (receipt.Symbol == "" || receipt.Symbol == "*") {
			return true
		}
	}
	return false
}

func (v *validator) validateFuturesSymbols(evidence Evidence) {
	discovered := make(map[string]struct{}, len(evidence.FuturesSymbols))
	for _, symbol := range evidence.FuturesSymbols {
		discovered[symbol] = struct{}{}
		v.futuresSymbols[symbol] = struct{}{}
	}
	observed := make(map[string]struct{})
	for _, position := range evidence.Opening.Positions {
		if position.Symbol != "" {
			observed[position.Symbol] = struct{}{}
		}
	}
	for _, position := range evidence.Closing.Positions {
		if position.Symbol != "" {
			observed[position.Symbol] = struct{}{}
		}
	}
	for _, entry := range v.entries {
		switch entry.Kind {
		case RealizedPnL, TradingFee, Funding:
			if entry.Symbol != "" && isFuturesMarket(entry.Market) {
				observed[entry.Symbol] = struct{}{}
			}
		}
	}
	for _, symbol := range sortedSet(discovered) {
		if _, ok := observed[symbol]; !ok {
			v.fail(reasonFuturesOutOfScope)
		}
	}
	for _, symbol := range sortedSet(observed) {
		if _, ok := discovered[symbol]; !ok {
			v.fail(reasonFuturesMissing)
		}
	}
}

func (v *validator) validateLedger(evidence Evidence) *ledgerFacts {
	facts := &ledgerFacts{
		tradingGross: new(big.Rat),
		tradingFees:  new(big.Rat),
		fundingNet:   new(big.Rat),
		interestCost: new(big.Rat),
		otherCosts:   new(big.Rat),
		volume:       make(map[string]*big.Rat),
	}
	matched := make(map[string]struct{}, len(v.entries))
	eventMatches := make(map[string]string)
	economicEntries := make(map[string]map[string]string)

	for _, event := range evidence.Events {
		switch event.Kind {
		case RealizedPnL, TradingFee, Funding, InterestAccrual:
		default:
			continue
		}
		if event.LedgerRef == "" {
			v.fail(reasonEventLedgerMissing)
			continue
		}
		entry, ok := v.ledgerByID[event.LedgerRef]
		if !ok {
			v.fail(reasonEventLedgerMissing)
			continue
		}
		if entry.Kind != event.Kind || !entry.At.Equal(event.At) || entry.Asset != Asset || event.Asset != Asset {
			v.fail(reasonEventLedgerMismatch)
			continue
		}
		switch event.Kind {
		case RealizedPnL, TradingFee, Funding:
			if event.Wallet != WalletFutures || !isFuturesMarket(entry.Market) {
				v.fail(reasonEventLedgerMismatch)
				continue
			}
			if event.Amount != entry.Amount || event.PrincipalDelta != "0" || event.InterestDelta != "0" {
				v.fail(reasonEventPnlAmount)
				continue
			}
		case InterestAccrual:
			if event.Wallet != WalletMargin {
				v.fail(reasonEventLedgerMismatch)
				continue
			}
			if event.Amount != "0" || event.PrincipalDelta != "0" {
				v.fail(reasonEventInterestAccrual)
				continue
			}
			expectedDelta, err := negatedDecimal(entry.Amount)
			if err != nil || event.InterestDelta != expectedDelta {
				v.fail(reasonEventInterestAccrual)
				continue
			}
		}
		signature := ledgerMatchSignature(event)
		if previous, exists := eventMatches[entry.ID]; exists && previous != signature {
			v.fail(reasonEventLedgerMismatch)
			continue
		}
		eventMatches[entry.ID] = signature
		matched[entry.ID] = struct{}{}
		if key := eventEconomicKey(event); key != "" {
			if economicEntries[key] == nil {
				economicEntries[key] = make(map[string]string)
			}
			economicEntries[key][entry.ID] = ledgerEntrySignature(entry)
		}
	}

	// The same economic identity may be evidenced by more than one source. It
	// must not be counted twice even when each source projects its own ledger
	// row. Conflicting projections are rejected.
	duplicateEntries := make(map[string]struct{})
	for _, entries := range economicEntries {
		if len(entries) < 2 {
			continue
		}
		ids := make([]string, 0, len(entries))
		for id := range entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		signature := entries[ids[0]]
		for _, id := range ids[1:] {
			if entries[id] != signature {
				v.fail(reasonEventEconomicConflict)
			}
			duplicateEntries[id] = struct{}{}
		}
	}

	for _, entry := range v.entries {
		if !entry.Known {
			v.fail(reasonLedgerUnknown)
			continue
		}
		if entry.Asset != Asset {
			v.fail(reasonLedgerAsset)
			continue
		}
		if entry.At.Before(v.start) || !entry.At.Before(v.end) {
			v.fail(reasonLedgerTime)
			continue
		}
		if entry.Amount == "" {
			v.fail(reasonLedgerAmountMissing)
			continue
		}
		amount, err := parseDecimal(entry.Amount)
		if err != nil {
			v.fail(reasonLedgerAmountMissing)
			continue
		}
		switch entry.Kind {
		case RealizedPnL:
			if !isFuturesMarket(entry.Market) {
				v.fail(reasonLedgerMarket)
				continue
			}
			if entry.Symbol == "" {
				v.fail(reasonPositionFill)
			}
			quantity, sideSign, ok := fillQuantity(entry)
			if !ok {
				v.fail(reasonPositionFill)
				continue
			}
			if _, duplicate := duplicateEntries[entry.ID]; duplicate {
				continue
			}
			facts.tradingGross.Add(facts.tradingGross, amount)
			if entry.Symbol != "" {
				volume := facts.volume[entry.Symbol]
				if volume == nil {
					volume = new(big.Rat)
					facts.volume[entry.Symbol] = volume
				}
				volume.Add(volume, new(big.Rat).Mul(quantity, sideSign))
			}
		case TradingFee:
			if !isFuturesMarket(entry.Market) {
				v.fail(reasonLedgerMarket)
				continue
			}
			if amount.Sign() > 0 {
				v.fail(reasonLedgerFeeSign)
				continue
			}
			if _, duplicate := duplicateEntries[entry.ID]; duplicate {
				continue
			}
			facts.tradingFees.Add(facts.tradingFees, new(big.Rat).Neg(amount))
		case Funding:
			if !isFuturesMarket(entry.Market) {
				v.fail(reasonLedgerMarket)
				continue
			}
			if entry.Symbol == "" {
				v.fail(reasonFuturesMissing)
			}
			if _, duplicate := duplicateEntries[entry.ID]; duplicate {
				continue
			}
			facts.fundingNet.Add(facts.fundingNet, amount)
		case InterestAccrual:
			if amount.Sign() >= 0 {
				v.fail(reasonLedgerInterestSign)
				continue
			}
			if _, duplicate := duplicateEntries[entry.ID]; duplicate {
				continue
			}
			facts.interestCost.Add(facts.interestCost, new(big.Rat).Neg(amount))
		default:
			v.fail(reasonLedgerKind)
			continue
		}
		if _, ok := matched[entry.ID]; !ok {
			v.fail(reasonLedgerUnmatched)
		}
	}
	return facts
}

func ledgerMatchSignature(event Event) string {
	return string(event.Kind) + "|" + event.Wallet + "|" + event.Asset + "|" +
		event.At.UTC().Format(time.RFC3339Nano) + "|" + event.Amount + "|" + event.InterestDelta
}

func eventEconomicKey(event Event) string {
	if event.EconomicID == "" {
		return ""
	}
	// A single business event may legitimately carry several accounting
	// components (for example principal repayment and its accrued interest, or
	// a fill's PnL and fee). The kind separates components while still
	// deduplicating the same component reported by several sources.
	return event.EconomicID + "|" + event.Wallet + "|" + string(event.Kind)
}

func ledgerEntrySignature(entry LedgerEntry) string {
	return string(entry.Kind) + "|" + entry.Market + "|" + entry.Symbol + "|" + entry.Asset + "|" +
		entry.At.UTC().Format(time.RFC3339Nano) + "|" + entry.Amount + "|" + entry.Quantity + "|" + entry.Side
}

func fillQuantity(entry LedgerEntry) (*big.Rat, *big.Rat, bool) {
	if entry.Quantity == "" {
		return nil, nil, false
	}
	quantity, err := parseDecimal(entry.Quantity)
	if err != nil || quantity.Sign() <= 0 {
		return nil, nil, false
	}
	switch strings.ToLower(strings.TrimSpace(entry.Side)) {
	case "buy":
		return quantity, big.NewRat(1, 1), true
	case "sell":
		return quantity, big.NewRat(-1, 1), true
	default:
		return nil, nil, false
	}
}

func isFuturesMarket(market string) bool {
	switch strings.ToLower(strings.TrimSpace(market)) {
	case "", "futures", "usdt_futures", "usdt-m", "usdm", "um_futures", "binance_futures":
		return true
	default:
		return false
	}
}

func negatedDecimal(value string) (string, error) {
	rat, err := parseDecimal(value)
	if err != nil {
		return "", err
	}
	return formatDecimal(new(big.Rat).Neg(rat)), nil
}

func (v *validator) validateEvents(events []Event, facts *ledgerFacts) {
	deduped := v.dedupeEconomic(events)
	quantity := make(map[string]*big.Rat)
	principal := make(map[string]*big.Rat)
	interest := make(map[string]*big.Rat)
	for wallet := range v.walletSet {
		quantity[wallet] = new(big.Rat)
		principal[wallet] = new(big.Rat)
		interest[wallet] = new(big.Rat)
	}
	internalTransfer := make(map[int]struct{})

	transferGroup := make(map[string][]int)
	for index, event := range deduped {
		if event.Kind == Transfer {
			transferGroup[event.TransferID] = append(transferGroup[event.TransferID], index)
		}
	}

	for index, event := range deduped {
		if event.At.Before(v.start) || !event.At.Before(v.end) {
			v.fail(reasonEventOutside)
			continue
		}
		if _, ok := v.walletSet[event.Wallet]; !ok {
			v.fail(reasonEventWallet)
			continue
		}
		if event.Asset != Asset {
			v.fail(reasonEventAsset)
			continue
		}
		amount, err := parseDecimal(event.Amount)
		if err != nil {
			v.fail(reasonEventAsset)
			continue
		}
		principalDelta, _ := parseDecimal(event.PrincipalDelta)
		interestDelta, _ := parseDecimal(event.InterestDelta)

		switch event.Kind {
		case Deposit:
			if amount.Sign() <= 0 {
				v.fail(reasonEventSign)
				continue
			}
			if principalDelta.Sign() != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventDelta)
				continue
			}
			v.externalCapital.Add(v.externalCapital, amount)
		case Withdrawal:
			if amount.Sign() >= 0 {
				v.fail(reasonEventSign)
				continue
			}
			if principalDelta.Sign() != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventDelta)
				continue
			}
			v.externalCapital.Add(v.externalCapital, amount)
		case Transfer:
			if amount.Sign() == 0 || event.TransferID == "" || event.CounterpartyWallet == "" {
				v.fail(reasonEventTransferCounter)
				continue
			}
			if principalDelta.Sign() != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventDelta)
				continue
			}
			pair, paired := v.transferPair(deduped, index)
			if paired {
				internalTransfer[index] = struct{}{}
				internalTransfer[pair] = struct{}{}
			} else if _, scoped := v.walletSet[event.CounterpartyWallet]; scoped {
				v.fail(reasonEventTransferLeg)
			} else {
				v.externalCapital.Add(v.externalCapital, amount)
			}
		case BorrowPrincipal:
			if amount.Sign() <= 0 || principalDelta.Cmp(amount) != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventPrincipalDelta)
				continue
			}
		case RepayPrincipal:
			if amount.Sign() >= 0 || principalDelta.Cmp(amount) != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventPrincipalDelta)
				continue
			}
		case InterestSettlement:
			if amount.Sign() >= 0 || interestDelta.Cmp(amount) != 0 || principalDelta.Sign() != 0 {
				v.fail(reasonEventSettlement)
				continue
			}
		case InterestAccrual:
			if amount.Sign() != 0 || interestDelta.Sign() <= 0 || principalDelta.Sign() != 0 {
				v.fail(reasonEventInterestAccrual)
				continue
			}
		case OtherFee:
			if amount.Sign() >= 0 || principalDelta.Sign() != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventOtherFee)
				continue
			}
			facts.otherCosts.Add(facts.otherCosts, new(big.Rat).Neg(amount))
		case RealizedPnL, TradingFee, Funding:
			if principalDelta.Sign() != 0 || interestDelta.Sign() != 0 {
				v.fail(reasonEventDelta)
				continue
			}
		default:
			v.fail(reasonEventUnknown)
			continue
		}

		quantity[event.Wallet].Add(quantity[event.Wallet], amount)
		principal[event.Wallet].Add(principal[event.Wallet], principalDelta)
		interest[event.Wallet].Add(interest[event.Wallet], interestDelta)
	}

	for _, indices := range transferGroup {
		pending := 0
		for _, index := range indices {
			if _, ok := internalTransfer[index]; !ok {
				pending++
			}
		}
		if pending > 1 {
			v.fail(reasonEventTransfer)
		}
	}

	v.compareWallets(quantity, principal, interest)
}

func (v *validator) transferPair(events []Event, index int) (int, bool) {
	event := events[index]
	amount, err := parseDecimal(event.Amount)
	if err != nil {
		return 0, false
	}
	for candidateIndex, candidate := range events {
		if candidateIndex == index || candidate.Kind != Transfer {
			continue
		}
		if candidate.TransferID != event.TransferID || candidate.Asset != event.Asset {
			continue
		}
		if candidate.Wallet != event.CounterpartyWallet || candidate.CounterpartyWallet != event.Wallet {
			continue
		}
		candidateAmount, err := parseDecimal(candidate.Amount)
		if err != nil {
			return 0, false
		}
		if new(big.Rat).Neg(amount).Cmp(candidateAmount) == 0 {
			return candidateIndex, true
		}
	}
	return 0, false
}

func (v *validator) dedupeEconomic(events []Event) []Event {
	out := make([]Event, 0, len(events))
	seen := make(map[string]Event)
	seenLedger := make(map[string]Event)
	for _, event := range events {
		key := eventEconomicKey(event)
		if key == "" && event.LedgerRef != "" {
			key = "ledger|" + event.LedgerRef
		}
		if key != "" {
			if previous, exists := seen[key]; exists {
				if !equivalentEconomic(previous, event) {
					v.fail(reasonEventEconomicConflict)
				}
				continue
			}
			seen[key] = event
		}
		if event.LedgerRef != "" {
			if previous, exists := seenLedger[event.LedgerRef]; exists {
				if !equivalentEconomic(previous, event) {
					v.fail(reasonEventEconomicConflict)
				}
				continue
			}
			seenLedger[event.LedgerRef] = event
		}
		out = append(out, event)
	}
	return out
}

func equivalentEconomic(left, right Event) bool {
	return left.Kind == right.Kind &&
		left.Wallet == right.Wallet &&
		left.Asset == right.Asset &&
		left.At.Equal(right.At) &&
		left.Amount == right.Amount &&
		left.PrincipalDelta == right.PrincipalDelta &&
		left.InterestDelta == right.InterestDelta &&
		left.TransferID == right.TransferID &&
		left.CounterpartyWallet == right.CounterpartyWallet
}

func (v *validator) compareWallets(quantity, principal, interest map[string]*big.Rat) {
	for _, wallet := range sortedSet(v.walletSet) {
		openUSDT, hasOpen := v.openingBalance[wallet][Asset]
		closeUSDT, hasClose := v.closingBalance[wallet][Asset]
		if !hasOpen || !hasClose {
			v.fail(reasonBalanceMissing)
			continue
		}
		if openUSDT.quantity == nil || closeUSDT.quantity == nil {
			v.fail(reasonBalanceMissing)
			continue
		}
		expected := new(big.Rat).Sub(closeUSDT.quantity, openUSDT.quantity)
		actual := ratOrZero(quantity[wallet])
		if expected.Cmp(actual) != 0 {
			v.fail(reasonWalletQuantity)
		}
		openPrincipal := ratOrZero(openUSDT.principal)
		closePrincipal := ratOrZero(closeUSDT.principal)
		expectedPrincipal := new(big.Rat).Sub(closePrincipal, openPrincipal)
		if expectedPrincipal.Cmp(ratOrZero(principal[wallet])) != 0 {
			v.fail(reasonWalletPrincipal)
		}
		openInterest := ratOrZero(openUSDT.interest)
		closeInterest := ratOrZero(closeUSDT.interest)
		expectedInterest := new(big.Rat).Sub(closeInterest, openInterest)
		if expectedInterest.Cmp(ratOrZero(interest[wallet])) != 0 {
			v.fail(reasonWalletInterest)
		}
	}
}

func (v *validator) validatePositionVolume(facts *ledgerFacts) {
	symbols := make(map[string]struct{})
	for symbol := range v.openingPos {
		symbols[symbol] = struct{}{}
	}
	for symbol := range v.closingPos {
		symbols[symbol] = struct{}{}
	}
	for symbol := range facts.volume {
		symbols[symbol] = struct{}{}
	}
	for _, symbol := range sortedSet(symbols) {
		openingQty := new(big.Rat)
		if position, ok := v.openingPos[symbol]; ok && position.quantity != nil {
			openingQty = position.quantity
		}
		closingQty := new(big.Rat)
		if position, ok := v.closingPos[symbol]; ok && position.quantity != nil {
			closingQty = position.quantity
		}
		expected := new(big.Rat).Sub(closingQty, openingQty)
		actual := ratOrZero(facts.volume[symbol])
		if expected.Cmp(actual) != 0 {
			v.fail(reasonPositionVolume)
		}
	}
}

func (v *validator) buildResult(inputs Inputs, evidence Evidence, digest string, facts *ledgerFacts) Result {
	unrealizedChange := new(big.Rat).Sub(v.closingUnrealized, v.openingUnrealized)
	tradingNet := new(big.Rat).Sub(facts.tradingGross, facts.tradingFees)

	explained := new(big.Rat)
	explained.Add(explained, tradingNet)
	explained.Add(explained, facts.fundingNet)
	explained.Sub(explained, facts.interestCost)
	explained.Add(explained, unrealizedChange)
	explained.Add(explained, v.valuationChange)
	explained.Sub(explained, facts.otherCosts)

	equityProfit := new(big.Rat).Sub(v.closingEquity, v.openingEquity)
	equityProfit.Sub(equityProfit, v.externalCapital)
	residual := new(big.Rat).Sub(equityProfit, explained)

	reasons := make([]string, 0, len(v.reasons)+len(inputs.Blockers))
	reasons = append(reasons, v.reasons...)
	for _, blocker := range inputs.Blockers {
		blocker = strings.TrimSpace(blocker)
		if blocker == "" {
			continue
		}
		if _, exists := v.seen[blocker]; exists {
			continue
		}
		v.seen[blocker] = struct{}{}
		reasons = append(reasons, blocker)
	}
	if new(big.Rat).Abs(residual).Cmp(toleranceRat) > 0 {
		if _, exists := v.seen[reasonResidual]; !exists {
			v.seen[reasonResidual] = struct{}{}
			reasons = append(reasons, reasonResidual)
		}
	}
	complete := len(reasons) == 0

	return Result{
		Query: Query{
			Exchange:     evidence.Scope.Exchange,
			AccountScope: evidence.Scope.AccountScope,
			Date:         evidence.Date,
		},
		Scope:            evidence.Scope,
		Timezone:         evidence.Timezone,
		Method:           evidence.Method,
		Asset:            Asset,
		PeriodStart:      v.start,
		PeriodEnd:        v.end,
		Revision:         evidence.Scope.Revision,
		EvidenceRevision: inputs.EvidenceRevision,
		InputDigest:      digest,
		ComputedAt:       v.now,
		Amounts: Amounts{
			OpeningEquity:      formatDecimal(v.openingEquity),
			ClosingEquity:      formatDecimal(v.closingEquity),
			NetExternalCapital: formatDecimal(v.externalCapital),
			TradingGross:       formatDecimal(facts.tradingGross),
			TradingFees:        formatDecimal(facts.tradingFees),
			TradingNet:         formatDecimal(tradingNet),
			FundingNet:         formatDecimal(facts.fundingNet),
			InterestCost:       formatDecimal(facts.interestCost),
			UnrealizedChange:   formatDecimal(unrealizedChange),
			ValuationChange:    formatDecimal(v.valuationChange),
			OtherCosts:         formatDecimal(facts.otherCosts),
			EquityProfit:       formatDecimal(equityProfit),
			ExplainedProfit:    formatDecimal(explained),
			Residual:           formatDecimal(residual),
		},
		Tolerance:                    Tolerance,
		EquityReconciliationComplete: complete,
		Reasons:                      reasons,
	}
}

func ratOrZero(value *big.Rat) *big.Rat {
	if value == nil {
		return new(big.Rat)
	}
	return value
}

func mergeCoverage(left, right []Coverage) ([]Coverage, error) {
	combined := make([]Coverage, 0, len(left)+len(right))
	combined = append(combined, left...)
	combined = append(combined, right...)
	return normalizeCoverageList(combined)
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
