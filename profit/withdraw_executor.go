package profit

import (
	"context"
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

const (
	immediateInterval = 5 * time.Minute
	dailyHour         = 2
	dailyMinute       = 0
	weeklyWeekday     = time.Monday
)

// ExchangeGetter 根据交易所 ID 获取交易所实例（用于内部转账）
type ExchangeGetter func(exchangeID string) exchange.IExchange

// WithdrawExecutor 利润提取定时执行器
type WithdrawExecutor struct {
	ctx             context.Context
	cancel          context.CancelFunc
	st              storage.Storage
	getExchange     ExchangeGetter
	immediateTicker *time.Ticker
}

// NewWithdrawExecutor 创建利润提取执行器
func NewWithdrawExecutor(ctx context.Context, st storage.Storage, getExchange ExchangeGetter) *WithdrawExecutor {
	ctx, cancel := context.WithCancel(ctx)
	return &WithdrawExecutor{
		ctx:         ctx,
		cancel:      cancel,
		st:          st,
		getExchange: getExchange,
	}
}

// Start 启动定时任务（immediate / daily / weekly）
func (e *WithdrawExecutor) Start() {
	go e.runImmediateTask()
	go e.runDailyTask()
	go e.runWeeklyTask()
	logger.Info("✅ 利润提取执行器已启动（immediate/daily/weekly）")
}

// Stop 停止执行器
func (e *WithdrawExecutor) Stop() {
	e.cancel()
	if e.immediateTicker != nil {
		e.immediateTicker.Stop()
	}
}

func (e *WithdrawExecutor) runImmediateTask() {
	e.immediateTicker = time.NewTicker(immediateInterval)
	defer e.immediateTicker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.immediateTicker.C:
			e.processRules("immediate")
		}
	}
}

func (e *WithdrawExecutor) runDailyTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Hour() == dailyHour && now.Minute() < 15 {
				e.processRules("daily")
			}
		}
	}
}

func (e *WithdrawExecutor) runWeeklyTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Weekday() == weeklyWeekday && now.Hour() == dailyHour && now.Minute() < 15 {
				e.processRules("weekly")
			}
		}
	}
}

func (e *WithdrawExecutor) processRules(frequency string) {
	accountIDs, err := e.st.ListAccountIDsWithProfitRules()
	if err != nil {
		logger.Warn("⚠️ [利润提取] 获取账户列表失败: %v", err)
		return
	}
	for _, accountID := range accountIDs {
		rules, err := e.st.ListProfitWithdrawRules(accountID)
		if err != nil {
			logger.Warn("⚠️ [利润提取] 获取规则失败 account=%s: %v", accountID, err)
			continue
		}
		for _, rule := range rules {
			if !rule.Enabled || rule.Frequency != frequency {
				continue
			}
			if !e.shouldExecute(rule, frequency) {
				continue
			}
			profit := e.calculateRealizedProfit(rule)
			if profit < rule.TriggerAmount {
				continue
			}
			withdrawAmount := profit * rule.WithdrawRatio
			if withdrawAmount < rule.MinWithdrawAmount {
				continue
			}
			if err := e.executeWithdraw(rule, withdrawAmount); err != nil {
				logger.Warn("⚠️ [利润提取] 执行失败 rule=%s: %v", rule.ID, err)
			}
		}
	}
}

func (e *WithdrawExecutor) shouldExecute(rule *storage.ProfitWithdrawRule, frequency string) bool {
	switch frequency {
	case "immediate":
		return true
	case "daily":
		if rule.LastTriggeredAt == nil {
			return true
		}
		now := time.Now()
		y, m, d := rule.LastTriggeredAt.Date()
		ny, nm, nd := now.Date()
		return y != ny || m != nm || d != nd
	case "weekly":
		if rule.LastTriggeredAt == nil {
			return true
		}
		now := time.Now()
		_, tw := rule.LastTriggeredAt.ISOWeek()
		_, nw := now.ISOWeek()
		return tw != nw || rule.LastTriggeredAt.Year() != now.Year()
	default:
		return false
	}
}

func (e *WithdrawExecutor) calculateRealizedProfit(rule *storage.ProfitWithdrawRule) float64 {
	startTime := time.Time{}
	endTime := time.Now()
	if rule.StrategyID != "" {
		summary, err := e.st.GetPnLBySymbol(rule.StrategyID, rule.AccountID, startTime, endTime)
		if err != nil {
			return 0
		}
		return summary.TotalPnL
	}
	list, err := e.st.GetPnLByTimeRange(rule.AccountID, startTime, endTime)
	if err != nil {
		return 0
	}
	var total float64
	for _, p := range list {
		total += p.TotalPnL
	}
	return total
}

func (e *WithdrawExecutor) executeWithdraw(rule *storage.ProfitWithdrawRule, amount float64) error {
	ex := e.getExchange(rule.ExchangeID)
	if ex == nil {
		return fmt.Errorf("未找到交易所: %s", rule.ExchangeID)
	}
	record := &storage.ProfitWithdrawRecord{
		ID:          fmt.Sprintf("wd_%d", time.Now().UnixNano()),
		RuleID:      rule.ID,
		AccountID:   rule.AccountID,
		ExchangeID:  rule.ExchangeID,
		StrategyID:  rule.StrategyID,
		Amount:      amount,
		Fee:         0,
		NetAmount:   amount,
		Currency:    "USDT",
		Type:        "auto",
		Status:      "pending",
		Destination: rule.Destination,
		CreatedAt:   time.Now(),
	}
	if err := e.st.SaveWithdrawRecord(record); err != nil {
		return fmt.Errorf("保存记录失败: %w", err)
	}
	transferID, err := ex.InternalTransfer(e.ctx, "UMFUTURE", "SPOT", "USDT", amount)
	if err != nil {
		_ = e.st.UpdateWithdrawRecordStatus(record.ID, "failed", "", err.Error())
		return err
	}
	if err := e.st.UpdateWithdrawRecordStatus(record.ID, "completed", transferID, ""); err != nil {
		logger.Warn("⚠️ [利润提取] 更新记录状态失败: %v", err)
	}
	if err := e.st.UpdateRuleLastTriggeredAt(rule.ID, time.Now()); err != nil {
		logger.Warn("⚠️ [利润提取] 更新规则执行时间失败: %v", err)
	}
	logger.Info("✅ [利润提取] 执行成功 rule=%s amount=%.2f USDT transferId=%s", rule.ID, amount, transferID)
	return nil
}
