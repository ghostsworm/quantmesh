package profit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
	"quantmesh/utils"
)

var ErrWithdrawOutcomeUnknown = errors.New("profit withdrawal outcome is unknown")

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
		wait := nextScheduleTime(frequency, now).Sub(now)
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
		for _, rule := range rules {
			if !rule.Enabled || rule.Frequency != frequency {
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
	}
	windowEnd := e.now()

	profit, err := e.calculateRealizedProfit(rule, since, windowEnd)
	if err != nil {
		return err
	}
	if profit <= 0 || profit < rule.TriggerAmount {
		return nil
	}
	withdrawn, err := e.withdrawnSince(rule, since)
	if err != nil {
		return err
	}
	withdrawAmount := profit*rule.WithdrawRatio - withdrawn
	if rule.MaxWithdrawAmount != nil && *rule.MaxWithdrawAmount > 0 && withdrawAmount > *rule.MaxWithdrawAmount {
		withdrawAmount = *rule.MaxWithdrawAmount
	}
	if withdrawAmount <= 0 || withdrawAmount < rule.MinWithdrawAmount {
		return nil
	}
	return e.executeWithdraw(rule, claimID, withdrawAmount, windowEnd)
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
		SumReservedWithdrawAmount(accountID, ruleID string, since time.Time) (float64, error)
	})
	if !ok {
		return 0, fmt.Errorf("storage lacks unbounded withdrawal reservation aggregation; automatic withdrawal is disabled")
	}
	total, err := aggregator.SumReservedWithdrawAmount(rule.AccountID, rule.ID, since)
	if err != nil {
		return 0, fmt.Errorf("汇总提取预留金额 account=%s rule=%s: %w", rule.AccountID, rule.ID, err)
	}
	return total, nil
}

// executeWithdraw 執行劃轉；windowEnd 為本次利潤統計截止時刻，成功後寫入 LastTriggeredAt 作為下次統計起點
func (e *WithdrawExecutor) executeWithdraw(rule *storage.ProfitWithdrawRule, claimID string, amount float64, windowEnd time.Time) error {
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
	if err := e.st.SaveWithdrawRecord(record); err != nil {
		return fmt.Errorf("保存記錄失败: %w", err)
	}
	transferID, err := ex.InternalTransfer(e.ctx, "UMFUTURE", "SPOT", "USDT", amount)
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
