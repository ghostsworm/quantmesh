package profit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
	"quantmesh/logger"
	"quantmesh/storage"
	"quantmesh/utils"
)

var ErrWithdrawOutcomeUnknown = errors.New("profit withdrawal outcome is unknown")

const abandonedWithdrawClaimAge = 15 * time.Minute

const (
	immediateInterval = 5 * time.Minute
	dailyHour         = 2
	dailyMinute       = 0
	weeklyWeekday     = time.Monday

	frequencyImmediate = "immediate"
	frequencyDaily     = "daily"
	frequencyWeekly    = "weekly"
)

// ExchangeGetter 根據交易所 ID 獲取交易所實例（用於內部轉帳）
type ExchangeGetter func(exchangeID string) exchange.IExchange

// WithdrawExecutor 利润提取定時執行器
type WithdrawExecutor struct {
	ctx         context.Context
	cancel      context.CancelFunc
	st          storage.Storage
	getExchange ExchangeGetter
	now         func() time.Time // 可注入時鐘（測試用），預設 time.Now
}

// NewWithdrawExecutor 創建利润提取執行器
func NewWithdrawExecutor(ctx context.Context, st storage.Storage, getExchange ExchangeGetter) *WithdrawExecutor {
	ctx, cancel := context.WithCancel(ctx)
	return &WithdrawExecutor{
		ctx:         ctx,
		cancel:      cancel,
		st:          st,
		getExchange: getExchange,
		now:         time.Now,
	}
}

// Start 啟动定時任務（immediate / daily / weekly）
func (e *WithdrawExecutor) Start() {
	go e.runImmediateTask()
	go e.runScheduledTask(frequencyDaily)
	go e.runScheduledTask(frequencyWeekly)
	logger.Info("✅ 利润提取執行器已啟动（immediate/daily/weekly）")
}

// Stop 停止執行器
func (e *WithdrawExecutor) Stop() {
	e.cancel()
}

func (e *WithdrawExecutor) nowInConfiguredTimezone() time.Time {
	return e.now().In(utils.GlobalLocation)
}

func (e *WithdrawExecutor) runImmediateTask() {
	ticker := time.NewTicker(immediateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.processRules(frequencyImmediate)
		}
	}
}

// runScheduledTask daily/weekly 任務：計時器直接對準下一個目標時刻，不依賴 ticker 相位；
// 啟動時先補跑一次，由 shouldExecute 按週期去重（本週期已成功執行的規則不會重複觸發）
func (e *WithdrawExecutor) runScheduledTask(frequency string) {
	e.processRules(frequency)
	for {
		now := e.nowInConfiguredTimezone()
		wait := nextScheduledRetryWait(frequency, now)
		timer := time.NewTimer(wait)
		select {
		case <-e.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			e.processRules(frequency)
		}
	}
}

// nextScheduledRetryWait retries overdue rules periodically after transient
// accounting/coverage failures instead of deferring them until the next day or week.
func nextScheduledRetryWait(frequency string, now time.Time) time.Duration {
	wait := nextScheduleTime(frequency, now).Sub(now)
	if wait > immediateInterval {
		return immediateInterval
	}
	return wait
}

// currentPeriodStart 返回 now 所在週期的起點（最近一個 <= now 的目標時刻）
func currentPeriodStart(frequency string, now time.Time) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), dailyHour, dailyMinute, 0, 0, now.Location())
	switch frequency {
	case frequencyWeekly:
		offset := (int(now.Weekday()) - int(weeklyWeekday) + 7) % 7
		target = target.AddDate(0, 0, -offset)
		if now.Before(target) {
			target = target.AddDate(0, 0, -7)
		}
	default:
		if now.Before(target) {
			target = target.AddDate(0, 0, -1)
		}
	}
	return target
}

// nextScheduleTime 返回 now 之後的下一個目標時刻
func nextScheduleTime(frequency string, now time.Time) time.Time {
	start := currentPeriodStart(frequency, now)
	if frequency == frequencyWeekly {
		return start.AddDate(0, 0, 7)
	}
	return start.AddDate(0, 0, 1)
}

func (e *WithdrawExecutor) processRules(frequency string) {
	recoverer, ok := e.st.(interface {
		RecoverAbandonedProfitWithdrawRuleClaims(time.Time) (int64, error)
	})
	if !ok {
		logger.Error("❌ [利润提取] 存储不支持安全回收崩溃遗留的规则 claim，自动提取已禁用")
		return
	}
	recovered, err := recoverer.RecoverAbandonedProfitWithdrawRuleClaims(e.now().UTC().Add(-abandonedWithdrawClaimAge))
	if err != nil {
		logger.Error("❌ [利润提取] 回收遗留规则 claim 失败，自动提取本轮停止: %v", err)
		return
	}
	if recovered > 0 {
		logger.Warn("⚠️ [利润提取] 已回收 %d 个无未决转账预留且超时的崩溃遗留规则 claim", recovered)
	}
	accountIDs, err := e.st.ListAccountIDsWithProfitRules()
	if err != nil {
		logger.Warn("⚠️ [利润提取] 獲取帳戶列表失败: %v", err)
		return
	}
	for _, accountID := range accountIDs {
		rules, err := e.st.ListProfitWithdrawRules(accountID)
		if err != nil {
			logger.Warn("⚠️ [利润提取] 獲取规则失败 account=%s: %v", accountID, err)
			continue
		}
		streamCounts := countEnabledWithdrawStreams(rules, accountID)
		for _, rule := range rules {
			if !rule.Enabled || rule.Frequency != frequency {
				continue
			}
			if streamCounts[withdrawStreamForRule(rule, accountID)] > 1 {
				logger.Error("❌ [利润提取] 同一账户/交易所/交易对存在多条启用规则，拒绝重复核算和划转 account=%s exchange=%s symbol=%s rule=%s",
					rule.AccountID, rule.ExchangeID, rule.StrategyID, rule.ID)
				continue
			}
			if err := ValidateWithdrawRule(rule); err != nil {
				logger.Error("❌ [利润提取] 规则配置未通过安全校验 rule=%s: %v", rule.ID, err)
				continue
			}
			if !e.shouldExecute(rule, frequency) {
				continue
			}
			if err := e.processRule(rule); err != nil {
				logger.Warn("⚠️ [利润提取] 執行失败 rule=%s: %v", rule.ID, err)
			}
		}
	}
}

type withdrawStreamKey struct {
	accountID    string
	accountScope string
	exchange     string
	symbol       string
}

func withdrawStreamForRule(rule *storage.ProfitWithdrawRule, accountID string) withdrawStreamKey {
	if rule == nil {
		return withdrawStreamKey{}
	}
	if strings.TrimSpace(rule.AccountID) != "" {
		accountID = rule.AccountID
	}
	return withdrawStreamKey{accountID: strings.TrimSpace(accountID), accountScope: strings.TrimSpace(rule.AccountScope),
		exchange: strings.ToLower(strings.TrimSpace(rule.ExchangeID)), symbol: strings.ToUpper(strings.TrimSpace(rule.StrategyID))}
}

func countEnabledWithdrawStreams(rules []*storage.ProfitWithdrawRule, accountID string) map[withdrawStreamKey]int {
	counts := make(map[withdrawStreamKey]int)
	for _, rule := range rules {
		if rule == nil || !rule.Enabled {
			continue
		}
		key := withdrawStreamForRule(rule, accountID)
		if key.accountID == "" || key.accountScope == "" || key.exchange == "" || key.symbol == "" {
			continue
		}
		counts[key]++
	}
	return counts
}

// processRule 只提取「上次成功提取之後」新實現的利潤，並扣除該區間內已提取/處理中的金額，
// 保證重複 tick（含 LastTriggeredAt 更新失敗的情況）不會重複劃轉
func (e *WithdrawExecutor) processRule(rule *storage.ProfitWithdrawRule) (retErr error) {
	if err := ValidateWithdrawRule(rule); err != nil {
		return err
	}
	// A pending reservation is written only after calculating the amount. Serialize
	// the complete calculation/reserve/transfer path per rule inside this process.
	unlock := withdrawRuleLocks.lock(rule.AccountID + "\x00" + rule.ID)
	defer unlock()
	claimer, ok := e.st.(interface {
		ClaimProfitWithdrawRule(ruleID, claimID string) (bool, error)
		ReleaseProfitWithdrawRuleClaim(ruleID, claimID string) error
	})
	if !ok {
		return fmt.Errorf("storage lacks durable rule claim; automatic withdrawal is disabled")
	}
	claimID := "claim_" + utils.NewCompactOrderID()
	claimed, err := claimer.ClaimProfitWithdrawRule(rule.ID, claimID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	keepClaim := false
	defer func() {
		if errors.Is(retErr, ErrWithdrawOutcomeUnknown) {
			keepClaim = true
		}
		if keepClaim {
			return
		}
		if releaseErr := claimer.ReleaseProfitWithdrawRuleClaim(rule.ID, claimID); releaseErr != nil {
			logger.Error("❌ [利润提取] 规则 claim 无法释放 rule=%s: %v", rule.ID, releaseErr)
			if retErr == nil {
				retErr = releaseErr
			}
		}
	}()
	var since time.Time
	if rule.LastTriggeredAt != nil {
		since = *rule.LastTriggeredAt
	} else {
		since = rule.CreatedAt
	}
	if since.IsZero() {
		return fmt.Errorf("withdrawal rule lacks a trusted accounting start time; automatic withdrawal is disabled")
	}
	legacyCheckpoint, err := legacyWithdrawalCheckpoint(e.st, rule.AccountID, rule.ExchangeID)
	if err != nil {
		return err
	}
	if legacyCheckpoint.After(since) {
		since = legacyCheckpoint
	}
	coverageReader, ok := e.st.(interface {
		GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope string) (time.Time, time.Time, error)
	})
	if !ok {
		return fmt.Errorf("storage lacks funding income coverage state; automatic withdrawal is disabled")
	}
	coveredFrom, coveredThrough, err := coverageReader.GetFundingIncomeCoverage(rule.ExchangeID, rule.StrategyID, "futures", rule.AccountScope)
	if err != nil {
		return fmt.Errorf("read funding income coverage: %w", err)
	}
	if coveredFrom.IsZero() || coveredThrough.IsZero() {
		return fmt.Errorf("funding income coverage is unavailable; automatic withdrawal is disabled")
	}
	if coveredFrom.After(since) {
		return fmt.Errorf("funding income history starts after the withdrawal accounting window; automatic withdrawal is disabled")
	}
	windowEnd := coveredThrough
	if now := e.now(); windowEnd.After(now) {
		windowEnd = now
	}
	if !since.Before(windowEnd) {
		return nil
	}

	profit, err := e.calculateRealizedProfit(rule, since, windowEnd)
	if err != nil {
		return err
	}
	if math.IsNaN(profit) || math.IsInf(profit, 0) {
		return fmt.Errorf("realized profit is not finite; automatic withdrawal is disabled")
	}
	if profit <= 0 || profit < rule.TriggerAmount {
		return nil
	}
	verifiedBudget := profit * rule.WithdrawRatio
	if math.IsNaN(verifiedBudget) || math.IsInf(verifiedBudget, 0) || verifiedBudget <= 0 {
		return fmt.Errorf("verified withdrawal budget is invalid; automatic withdrawal is disabled")
	}
	withdrawn, err := e.withdrawnSince(rule, since)
	if err != nil {
		return err
	}
	withdrawAmount := verifiedBudget - withdrawn
	if rule.MaxWithdrawAmount != nil && *rule.MaxWithdrawAmount > 0 && withdrawAmount > *rule.MaxWithdrawAmount {
		withdrawAmount = *rule.MaxWithdrawAmount
	}
	if withdrawAmount <= 0 || withdrawAmount < rule.MinWithdrawAmount {
		return nil
	}
	return e.executeWithdraw(rule, claimID, withdrawAmount, since, windowEnd, verifiedBudget)
}

func legacyWithdrawalCheckpoint(st storage.Storage, accountID, exchangeID string) (time.Time, error) {
	records, err := st.GetWithdrawRecords(accountID, 1000)
	if err != nil {
		return time.Time{}, fmt.Errorf("read legacy withdrawal records: %w", err)
	}
	if len(records) >= 1000 {
		return time.Time{}, fmt.Errorf("withdrawal history reached its verification limit; automatic withdrawal is disabled")
	}
	return LegacyWithdrawalCheckpoint(records, exchangeID)
}

// LegacyWithdrawalCheckpoint returns the latest confirmed completion time for
// unscoped withdrawals on an exchange. Both automatic and manual paths use it
// so an earlier reservation timestamp cannot reopen already withdrawn profit.
func LegacyWithdrawalCheckpoint(records []*storage.ProfitWithdrawRecord, exchangeID string) (time.Time, error) {
	var checkpoint time.Time
	for _, record := range records {
		if record == nil || !strings.EqualFold(record.ExchangeID, exchangeID) || record.AccountScope != "" {
			continue
		}
		if IsReconciledWithdrawalFailure(record) {
			continue
		}
		if record.Status == "completed" {
			if record.CompletedAt == nil || record.CompletedAt.IsZero() {
				return time.Time{}, fmt.Errorf("legacy completed withdrawal lacks a trusted completion time; reconcile it before automatic transfer")
			}
			if record.CompletedAt.After(checkpoint) {
				checkpoint = *record.CompletedAt
			}
			continue
		}
		return time.Time{}, fmt.Errorf("legacy withdrawal outcome is unresolved; reconcile it before automatic transfer")
	}
	return checkpoint, nil
}

// IsReconciledWithdrawalFailure only exempts an unscoped legacy record when a
// human reconciliation explicitly confirmed the transfer did not occur.
func IsReconciledWithdrawalFailure(record *storage.ProfitWithdrawRecord) bool {
	const verifiedFailurePrefix = "人工核账确认交易所流水未发生划转："
	return record != nil && record.Status == "failed" && strings.HasPrefix(record.FailedReason, verifiedFailurePrefix) && strings.TrimSpace(record.TransferID) != ""
}

func (e *WithdrawExecutor) shouldExecute(rule *storage.ProfitWithdrawRule, frequency string) bool {
	return shouldExecuteAt(rule, frequency, e.nowInConfiguredTimezone())
}

// shouldExecuteAt immediate 總是執行（金額由 processRule 按區間去重）；
// daily/weekly 在本週期起點之後尚未成功執行時才執行
func shouldExecuteAt(rule *storage.ProfitWithdrawRule, frequency string, now time.Time) bool {
	switch frequency {
	case frequencyImmediate:
		return true
	case frequencyDaily, frequencyWeekly:
		if rule.LastTriggeredAt == nil {
			return true
		}
		return rule.LastTriggeredAt.Before(currentPeriodStart(frequency, now))
	default:
		return false
	}
}

// calculateRealizedProfit 計算 (since, end] 區間內已實現利潤
func (e *WithdrawExecutor) calculateRealizedProfit(rule *storage.ProfitWithdrawRule, since, end time.Time) (float64, error) {
	reader, ok := e.st.(interface {
		GetRealizedPnLForWithdrawal(exchange, symbol, accountScope string, startTime, endTime time.Time) (float64, error)
	})
	if !ok {
		return 0, fmt.Errorf("storage lacks exact exchange/account-scope/futures PnL query; automatic withdrawal is disabled")
	}
	// StrategyID is currently populated with the trading symbol by the UI/API.
	// The paired-trades table has no strategy identity, so do not pretend it does.
	if strings.TrimSpace(rule.AccountScope) == "" {
		return 0, fmt.Errorf("withdrawal rule lacks immutable account scope; automatic withdrawal is disabled")
	}
	return reader.GetRealizedPnLForWithdrawal(rule.ExchangeID, rule.StrategyID, rule.AccountScope, since, end)
}

// withdrawnSince 統計該規則在 since 之後創建、已完成或仍在處理中的提取金額
// （pending/processing 結果未知，保守計入，寧可少提也不重複劃轉）
func (e *WithdrawExecutor) withdrawnSince(rule *storage.ProfitWithdrawRule, since time.Time) (float64, error) {
	aggregator, ok := e.st.(interface {
		SumReservedWithdrawAmountForStream(accountID, accountScope, exchange, symbol string, since time.Time) (float64, error)
	})
	if !ok {
		return 0, fmt.Errorf("storage lacks exact-stream withdrawal reservation aggregation; automatic withdrawal is disabled")
	}
	total, err := aggregator.SumReservedWithdrawAmountForStream(rule.AccountID, rule.AccountScope, rule.ExchangeID, rule.StrategyID, since)
	if err != nil {
		return 0, fmt.Errorf("汇总提取预留金额 account=%s rule=%s: %w", rule.AccountID, rule.ID, err)
	}
	return total, nil
}

// ValidateTransferSafety is shared by manual and automatic transfers. It only
// proves the requested futures-to-spot transfer is inside the exchange's
// fresh USDT transfer limits; callers must separately prove profit coverage.
func ValidateTransferSafety(ctx context.Context, ex exchange.IExchange, symbol, accountScope string, amount float64, windowStart, windowEnd time.Time) error {
	if ctx == nil || ex == nil || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" || windowStart.IsZero() || !windowStart.Before(windowEnd) {
		return fmt.Errorf("withdrawal transfer requires an exchange and complete accounting interval")
	}
	scopedExchange, ok := ex.(interface{ WithdrawalAccountScope() string })
	if !ok || strings.TrimSpace(scopedExchange.WithdrawalAccountScope()) == "" || scopedExchange.WithdrawalAccountScope() != accountScope {
		return fmt.Errorf("exchange credential scope does not match the withdrawal accounting scope; withdrawal is disabled")
	}
	ledgerSource, ok := ex.(interface {
		ReadVerifiedWithdrawalEvidence(context.Context, time.Time) (accounting.Snapshot, error)
	})
	if !ok {
		return fmt.Errorf("exchange does not provide durably reconciled account income evidence; withdrawal is disabled")
	}
	ledger, err := ledgerSource.ReadVerifiedWithdrawalEvidence(ctx, windowStart)
	if err != nil {
		return fmt.Errorf("read complete account income evidence before transfer: %w", err)
	}
	if ledger.Currency != "USDT" || math.IsNaN(ledger.Equity) || math.IsInf(ledger.Equity, 0) ||
		ledger.ObservedAt.IsZero() || time.Since(ledger.ObservedAt) > 30*time.Second || ledger.ObservedAt.After(time.Now().Add(2*time.Second)) ||
		ledger.Wallet.ObservedAt.IsZero() || !ledger.Wallet.ObservedAt.Equal(ledger.ObservedAt) ||
		ledger.Wallet.From.IsZero() || ledger.Wallet.Through.IsZero() || ledger.Wallet.From.After(ledger.Wallet.Through) ||
		ledger.Wallet.From.After(windowStart) || ledger.Wallet.Through.Before(windowEnd) {
		return fmt.Errorf("account income evidence does not cover the withdrawal window; withdrawal is disabled")
	}
	if _, err := accounting.CanonicalDecimal(ledger.Wallet.Balance); err != nil {
		return fmt.Errorf("account income evidence wallet balance is invalid: %w", err)
	}
	freshProfit := new(big.Rat)
	seenProfitEntries := make(map[string]struct{})
	for _, entry := range ledger.Entries {
		if entry.At.IsZero() {
			return fmt.Errorf("account income evidence contains an entry without a timestamp; withdrawal is disabled")
		}
		if !entry.At.After(windowStart) || entry.At.After(windowEnd) {
			continue
		}
		switch entry.Kind {
		case "insurance_clear", "unallocated_fee", "interest":
			return fmt.Errorf("withdrawal interval contains unallocated account cash flow %q; withdrawal is disabled", entry.Kind)
		case "funding", "realized_pnl", "fee":
			if strings.TrimSpace(entry.Symbol) == "" || !strings.EqualFold(strings.TrimSpace(entry.Symbol), strings.TrimSpace(symbol)) {
				return fmt.Errorf("withdrawal interval contains %q not attributable to requested symbol %q; withdrawal is disabled", entry.Kind, symbol)
			}
			if !strings.EqualFold(strings.TrimSpace(entry.Currency), "USDT") {
				return fmt.Errorf("withdrawal interval contains %q without verified USDT denomination; withdrawal is disabled", entry.Kind)
			}
			entryID := strings.TrimSpace(entry.ID)
			if entryID == "" {
				return fmt.Errorf("withdrawal interval contains %q without a stable ledger identity; withdrawal is disabled", entry.Kind)
			}
			if _, duplicate := seenProfitEntries[entryID]; duplicate {
				return fmt.Errorf("withdrawal interval contains duplicate ledger identity; withdrawal is disabled")
			}
			seenProfitEntries[entryID] = struct{}{}
			amountRat, err := accounting.Decimal(entry.Amount)
			if err != nil {
				return fmt.Errorf("withdrawal interval contains %q with an invalid amount: %w", entry.Kind, err)
			}
			if entry.Kind == "fee" && amountRat.Sign() > 0 {
				return fmt.Errorf("withdrawal interval contains a positive fee without verified rebate classification; withdrawal is disabled")
			}
			freshProfit.Add(freshProfit, amountRat)
		case "transfer_in", "transfer_out", "rebate":
			if !strings.EqualFold(strings.TrimSpace(entry.Currency), "USDT") {
				return fmt.Errorf("withdrawal interval contains %q without verified USDT denomination; withdrawal is disabled", entry.Kind)
			}
			entryID := strings.TrimSpace(entry.ID)
			if entryID == "" {
				return fmt.Errorf("withdrawal interval contains %q without a stable ledger identity; withdrawal is disabled", entry.Kind)
			}
			if _, duplicate := seenProfitEntries[entryID]; duplicate {
				return fmt.Errorf("withdrawal interval contains duplicate ledger identity; withdrawal is disabled")
			}
			seenProfitEntries[entryID] = struct{}{}
			amountRat, err := accounting.Decimal(entry.Amount)
			if err != nil {
				return fmt.Errorf("withdrawal interval contains %q with an invalid amount: %w", entry.Kind, err)
			}
			switch entry.Kind {
			case "transfer_in":
				if amountRat.Sign() < 0 {
					return fmt.Errorf("withdrawal interval contains a negative incoming transfer; withdrawal is disabled")
				}
				// Incoming capital is not realized strategy profit.
			case "transfer_out":
				if amountRat.Sign() > 0 {
					return fmt.Errorf("withdrawal interval contains a positive outgoing transfer; withdrawal is disabled")
				}
				// Existing unreserved debits reduce the remaining withdrawal budget.
				freshProfit.Add(freshProfit, amountRat)
			case "rebate":
				if amountRat.Sign() < 0 {
					return fmt.Errorf("withdrawal interval contains an invalid or negative rebate; withdrawal is disabled")
				}
				// Rebates remain excluded unless explicitly attributed to a strategy.
			}
		default:
			return fmt.Errorf("withdrawal interval contains unsupported account cash flow %q; withdrawal is disabled", entry.Kind)
		}
	}
	amountRat, err := accounting.Decimal(strconv.FormatFloat(amount, 'f', -1, 64))
	if err != nil || freshProfit.Sign() <= 0 || amountRat.Cmp(freshProfit) > 0 {
		return fmt.Errorf("withdrawal amount exceeds fresh exchange-ledger realized USDT profit; withdrawal is disabled")
	}
	freshAccount, ok := ex.(interface {
		GetAccountFresh(context.Context) (*exchange.Account, error)
	})
	if !ok {
		return fmt.Errorf("exchange does not provide uncached account balances; withdrawal is disabled")
	}
	account, err := freshAccount.GetAccountFresh(ctx)
	if err != nil {
		return fmt.Errorf("read fresh futures balance before transfer: %w", err)
	}
	if account == nil || account.BalanceAsset != "USDT" || math.IsNaN(account.AvailableBalance) || math.IsInf(account.AvailableBalance, 0) || account.AvailableBalance < 0 ||
		math.IsNaN(account.MaxWithdrawAmount) || math.IsInf(account.MaxWithdrawAmount, 0) || account.MaxWithdrawAmount < 0 || account.MaxWithdrawAmount > account.AvailableBalance {
		return fmt.Errorf("fresh futures balance is not a verified USDT amount; withdrawal is disabled")
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || amount > account.AvailableBalance || amount > account.MaxWithdrawAmount {
		return fmt.Errorf("withdrawal amount %.8f exceeds fresh transferable futures balance %.8f USDT", amount, math.Min(account.AvailableBalance, account.MaxWithdrawAmount))
	}
	return nil
}

// executeWithdraw 執行劃轉；windowEnd 為本次利潤統計截止時刻，成功後寫入 LastTriggeredAt 作為下次統計起點
func (e *WithdrawExecutor) executeWithdraw(rule *storage.ProfitWithdrawRule, claimID string, amount float64, windowStart, windowEnd time.Time, verifiedBudget float64) error {
	if e.getExchange == nil {
		return fmt.Errorf("exchange lookup is unavailable; automatic withdrawal is disabled")
	}
	ex := e.getExchange(rule.ExchangeID)
	if ex == nil {
		return fmt.Errorf("未找到交易所: %s", rule.ExchangeID)
	}
	record := &storage.ProfitWithdrawRecord{
		ID:           "wd_" + utils.NewCompactOrderID(),
		RuleID:       rule.ID,
		AccountID:    rule.AccountID,
		AccountScope: rule.AccountScope,
		ClaimID:      claimID,
		ExchangeID:   rule.ExchangeID,
		StrategyID:   rule.StrategyID,
		Amount:       amount,
		Fee:          0,
		NetAmount:    amount,
		Currency:     "USDT",
		Type:         "auto",
		Status:       "processing",
		Destination:  rule.Destination,
		// CreatedAt 與 windowEnd 對齊：下次以 LastTriggeredAt=windowEnd 起算時，本記錄不會被重複扣減；
		// 若 LastTriggeredAt 更新失敗，本記錄仍落在舊區間內，會被扣減從而避免重複劃轉
		CreatedAt: windowEnd,
	}
	recorder, ok := e.st.(interface {
		SaveWithdrawRecordForClaim(*storage.ProfitWithdrawRecord, time.Time, float64) error
	})
	if !ok {
		return fmt.Errorf("storage lacks claim-fenced withdrawal reservation; automatic transfer is disabled")
	}
	if err := recorder.SaveWithdrawRecordForClaim(record, windowStart, verifiedBudget); err != nil {
		return fmt.Errorf("保存記錄失败: %w", err)
	}
	if err := ValidateTransferSafety(e.ctx, ex, rule.StrategyID, rule.AccountScope, amount, windowStart, windowEnd); err != nil {
		if updateErr := e.st.UpdateWithdrawRecordStatus(record.ID, "failed", "", "转账前安全校验失败，未发起划转: "+err.Error()); updateErr != nil {
			return fmt.Errorf("pre-transfer safety check failed (%v) and reservation could not be released: %w", err, updateErr)
		}
		return fmt.Errorf("pre-transfer safety check failed; no transfer was submitted: %w", err)
	}
	transferID, err := ex.InternalTransfer(e.ctx, "UMFUTURE", "SPOT", "USDT", amount)
	if err == nil && strings.TrimSpace(transferID) == "" {
		err = errors.New("exchange confirmed transfer without a verifiable transfer ID")
	}
	if err != nil {
		// A transfer timeout/error can arrive after the exchange completed it.
		// Keep the record pending so the profit is reserved and never retried
		// automatically until an operator reconciles the exchange ledger.
		const pendingReason = "转账结果未核实；请先核对交易所资金流水，禁止自动重试"
		if updErr := e.st.UpdateWithdrawRecordStatus(record.ID, "pending", "", pendingReason+": "+err.Error()); updErr != nil {
			logger.Error("⚠️ [利润提取] 转账结果未知且无法更新记录，记录保持 pending record=%s: %v", record.ID, updErr)
		}
		return fmt.Errorf("%w: transfer result unverified rule=%s amount=%.2f; reconcile exchange ledger before action: %v", ErrWithdrawOutcomeUnknown, rule.ID, amount, err)
	}
	if err := e.st.UpdateWithdrawRecordStatus(record.ID, "completed", transferID, ""); err != nil {
		logger.Error("⚠️ [利润提取] 转账已确认，但完成状态未写入；pending 预留继续阻止重复划转 rule=%s: %v", rule.ID, err)
	}
	if err := e.st.UpdateRuleLastTriggeredAt(rule.ID, windowEnd); err != nil {
		return fmt.Errorf("转账已确认但更新规则执行时间失败 rule=%s: %w", rule.ID, err)
	}
	logger.Info("✅ [利润提取] 執行成功 rule=%s amount=%.2f USDT transferId=%s", rule.ID, amount, transferID)
	return nil
}
