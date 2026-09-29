package risk

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
)

/*
MetricsFeeder 熔断器内部数据喂入（审查报告 C1）。

之前 GlobalCircuitBreaker.UpdateMetrics 只被 HTTP handler 调用，单日亏损/回撤/连亏熔断事实上不存在。
这里周期性地从真实数据计算三项指标并喂给熔断器：

  - 单日盈亏 = 今日（配置时区）已实现净盈亏（trades.pnl - fee）+ 所有 Bot 当前未实现盈亏。
    未实现盈亏按“当前值”计入而非“今日变化量”，偏保守：隔夜浮亏也会计入当日亏损。
  - 回撤(%) 每轮读取实际账户权益，扣除已核实的外部净入金后相对高水位计算。
    费用/资金费/借息留在实际权益中，不按本地 trades 重构，也不再次扣费。
    无完整现金流水的旧数据源仅报告未调整回撤；严格模式拒绝其作为风险依据。
    配置持久化时先保存高水位/流水凭据再发布指标，重启不重建基准。
  - 连续亏损 = 最近成交（按时间倒序）从最新一笔开始连续净亏损的笔数，净盈亏为 0 的成交跳过。

熔断恢复后通过 MetricsResetMarks 重置基线，避免同一批历史再次触发。
*/

const (
	// DefaultMetricsFeedInterval 默认喂数间隔
	DefaultMetricsFeedInterval = 60 * time.Second
	// consecutiveLossLookback 连续亏损回看窗口
	consecutiveLossLookback = 7 * 24 * time.Hour
	// consecutiveLossQueryLimit 连续亏损最多回看的成交笔数
	consecutiveLossQueryLimit = 500
	// realizedPnLQueryLimit 单次统计已实现盈亏的最大成交笔数（与存储层上限一致）
	realizedPnLQueryLimit = 10000
	// realizedCursorOverlap 增量统计时回看的重叠窗口，覆盖异步落库导致的 created_at 早于游标的成交
	realizedCursorOverlap = 5 * time.Minute
	// percentMultiplier 比例转百分比
	percentMultiplier = 100.0
	// circuitBreakerPnLAsset matches the configured monetary trigger unit in CircuitBreakerConfig.
	circuitBreakerPnLAsset = "USDT"
)

// ErrEquityUnavailable 暂无可用的权益数据源（如尚无运行中的合约 Bot），属预期情况
var ErrEquityUnavailable = errors.New("账户权益暂不可用")

// TradeOutcome 一笔已平仓成交的净结果
type TradeOutcome struct {
	Key      string    // 唯一键（如 buyOrderID:sellOrderID），用于增量去重
	NetPnL   float64   // 净盈亏（扣手续费）
	PnLAsset string    // 报价/結算資產；不同資產不得直接相加
	Fee      float64   // 未换算前记录的手续费量
	FeeAsset string    // 手续费计价资产
	ClosedAt time.Time // 成交时间
}

// TradeHistorySource 成交历史数据源（由 main 用 storage 实现）
type TradeHistorySource interface {
	TradesBetween(ctx context.Context, start, end time.Time, limit int) ([]TradeOutcome, error)
}

// TradeHistoryScanner optionally streams an ordered trade range without an in-memory result limit.
// Implementations must visit trades newest-first so consecutive-loss scans can stop early.
type TradeHistoryScanner interface {
	ScanTradesBetween(ctx context.Context, start, end time.Time, visit func(TradeOutcome) bool) error
}

// EquitySource 账户权益数据源（由 main 用交易所 GetAccount 实现）
type EquitySource interface {
	TotalEquity(ctx context.Context) (float64, error)
}

// MetricsSink 指标接收方（GlobalCircuitBreaker 实现）
type MetricsSink interface {
	UpdateMetrics(dailyPnL, maxDrawdown float64, consecutiveLosses int)
	MetricsResetMarks() MetricsResetMarks
}

// MetricsSnapshot 一次计算得到的指标
type MetricsSnapshot struct {
	DailyPnL          float64
	RealizedToday     float64
	UnrealizedPnL     float64
	MaxDrawdownPct    float64
	ConsecutiveLosses int
	DrawdownAvailable bool
	CashFlowAdjusted  bool
	EquityObservedAt  time.Time
}

// MetricsFeederOptions 可选参数
type MetricsFeederOptions struct {
	Interval                      time.Duration
	Location                      *time.Location   // 日界时区，nil 使用 time.Local
	Now                           func() time.Time // 测试注入
	MaxEquityAge                  time.Duration
	EquityStore                   EquityStateStore
	RequirePersistence            bool
	RequireCashFlowReconciliation bool
	RequireTradeHistory           bool
}

// MetricsFeeder 周期性计算并喂入熔断指标
type MetricsFeeder struct {
	sink   MetricsSink
	trades TradeHistorySource
	equity EquitySource
	bots   BotProvider
	opts   MetricsFeederOptions

	mu           sync.Mutex
	equityLoaded bool
	equityState  *EquityCheckpoint
}

// NewMetricsFeeder 创建喂数器；trades/equity/bots 均可为 nil（对应指标记为 0）
func NewMetricsFeeder(sink MetricsSink, trades TradeHistorySource, equity EquitySource, bots BotProvider, opts MetricsFeederOptions) *MetricsFeeder {
	if opts.Interval <= 0 {
		opts.Interval = DefaultMetricsFeedInterval
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxEquityAge <= 0 {
		opts.MaxEquityAge = 2 * opts.Interval
	}
	return &MetricsFeeder{
		sink:   sink,
		trades: trades,
		equity: equity,
		bots:   bots,
		opts:   opts,
	}
}

// Start 启动周期喂数，ctx 取消后退出
func (f *MetricsFeeder) Start(ctx context.Context) {
	go func() {
		f.tickAndLog(ctx)
		ticker := time.NewTicker(f.opts.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.tickAndLog(ctx)
			}
		}
	}()
	logger.Info("📊 [熔断喂数] 已启动，间隔 %v", f.opts.Interval)
}

func (f *MetricsFeeder) tickAndLog(ctx context.Context) {
	snap, err := f.Tick(ctx)
	if err != nil {
		logger.Warn("⚠️ [熔断喂数] 本轮指标计算失败，保留上次指标: %v", err)
		return
	}
	logger.Debug("📊 [熔断喂数] DailyPnL=%.2f (已实现 %.2f, 未实现 %.2f), 回撤=%.2f%% (可用=%v), 连亏=%d",
		snap.DailyPnL, snap.RealizedToday, snap.UnrealizedPnL, snap.MaxDrawdownPct, snap.DrawdownAvailable, snap.ConsecutiveLosses)
}

// Tick 计算一次指标并写入 sink；数据源出错时不写入（避免用残缺数据清零指标）
func (f *MetricsFeeder) Tick(ctx context.Context) (snap MetricsSnapshot, resultErr error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer func() { f.reportHealth(snap, resultErr) }()

	now := f.opts.Now()
	marks := f.sink.MetricsResetMarks()

	unrealizedPnL, unrealizedAsset, err := f.totalUnrealized()
	if err != nil {
		return snap, err
	}
	snap.UnrealizedPnL = unrealizedPnL
	if f.opts.RequireTradeHistory && f.trades == nil {
		return snap, fmt.Errorf("trade history source required by enabled circuit-breaker triggers")
	}

	realizedToday, realizedAsset, err := f.realizedToday(ctx, now, marks)
	if err != nil {
		return snap, err
	}
	if realizedAsset != "" && unrealizedAsset != "" && !strings.EqualFold(realizedAsset, unrealizedAsset) {
		return snap, fmt.Errorf("daily realized PnL asset %s differs from unrealized PnL asset %s", realizedAsset, unrealizedAsset)
	}
	snap.RealizedToday = realizedToday
	snap.DailyPnL = realizedToday + snap.UnrealizedPnL
	if !finiteEquity(snap.RealizedToday) || !finiteEquity(snap.UnrealizedPnL) || !finiteEquity(snap.DailyPnL) {
		return snap, fmt.Errorf("non-finite PnL source; risk metrics are unavailable")
	}

	losses, err := f.consecutiveLosses(ctx, now, marks)
	if err != nil {
		return snap, err
	}
	snap.ConsecutiveLosses = losses

	dd, ok, err := f.drawdown(ctx, now, marks)
	if err != nil {
		return snap, err
	}
	snap.MaxDrawdownPct = dd
	snap.DrawdownAvailable = ok
	if ok && f.equityState != nil {
		snap.CashFlowAdjusted = f.equityState.CashFlowAdjusted
		snap.EquityObservedAt = f.equityState.LastAt
	}

	if _, atomic := f.sink.(observedMetricsSink); !atomic {
		f.sink.UpdateMetrics(snap.DailyPnL, snap.MaxDrawdownPct, snap.ConsecutiveLosses)
	}
	return snap, nil
}

// totalUnrealized 汇总所有 Bot 未实现盈亏；未初始化/无价格的 Bot 跳过
func (f *MetricsFeeder) totalUnrealized() (float64, string, error) {
	if f.bots == nil {
		return 0, "", nil
	}
	total := 0.0
	asset := ""
	for index, bot := range f.bots.GetAllBots() {
		if bot == nil {
			continue
		}
		pnl, _, err := bot.GetPositionSummary()
		if err != nil {
			return 0, "", fmt.Errorf("read unrealized PnL for bot index %d: %w", index, err)
		}
		if !finiteEquity(pnl) {
			return 0, "", fmt.Errorf("unrealized PnL for bot index %d is non-finite", index)
		}
		if pnl != 0 {
			provider, ok := bot.(botPnLAssetProvider)
			if !ok {
				return 0, "", fmt.Errorf("unrealized PnL currency is unavailable for bot index %d", index)
			}
			botAsset := strings.ToUpper(strings.TrimSpace(provider.RiskPnLQuoteAsset()))
			if botAsset == "" {
				return 0, "", fmt.Errorf("unrealized PnL currency is unavailable for bot index %d", index)
			}
			if botAsset != circuitBreakerPnLAsset {
				return 0, "", fmt.Errorf("unrealized PnL currency %s cannot be compared with circuit-breaker threshold in %s", botAsset, circuitBreakerPnLAsset)
			}
			if asset != "" && asset != botAsset {
				return 0, "", fmt.Errorf("unrealized PnL currencies differ: %s and %s", asset, botAsset)
			}
			asset = botAsset
		}
		total += pnl
		if !finiteEquity(total) {
			return 0, "", fmt.Errorf("aggregate unrealized PnL is non-finite")
		}
	}
	return total, asset, nil
}

type botPnLAssetProvider interface {
	RiskPnLQuoteAsset() string
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// realizedToday 今日（配置时区）已实现净盈亏，手动恢复基线之后
func (f *MetricsFeeder) realizedToday(ctx context.Context, now time.Time, marks MetricsResetMarks) (float64, string, error) {
	if f.trades == nil {
		return 0, "", nil
	}
	local := now.In(f.opts.Location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, f.opts.Location)
	start := maxTime(dayStart, marks.All)
	if scanner, ok := f.trades.(TradeHistoryScanner); ok {
		total := 0.0
		asset := ""
		invalidTrade := false
		err := scanner.ScanTradesBetween(ctx, start, now, func(t TradeOutcome) bool {
			tradeAsset := strings.ToUpper(strings.TrimSpace(t.PnLAsset))
			feeAsset := strings.ToUpper(strings.TrimSpace(t.FeeAsset))
			if t.ClosedAt.Before(start) || t.ClosedAt.After(now) || !validTradePnL(t) || tradeAsset != circuitBreakerPnLAsset || (t.Fee != 0 && feeAsset != tradeAsset) || (asset != "" && asset != tradeAsset) {
				invalidTrade = true
				return false
			}
			asset = tradeAsset
			total += t.NetPnL
			if !finiteEquity(total) {
				invalidTrade = true
				return false
			}
			return true
		})
		if err != nil {
			return 0, "", fmt.Errorf("流式查询今日成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
		}
		if invalidTrade || !finiteEquity(total) {
			return 0, "", fmt.Errorf("今日成交包含未知/混合计价资产、未换算手续费或无效盈亏，无法确认已实现盈亏完整性")
		}
		return total, asset, nil
	}

	trades, err := f.trades.TradesBetween(ctx, start, now, realizedPnLQueryLimit)
	if err != nil {
		return 0, "", fmt.Errorf("查询今日成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
	}
	if len(trades) >= realizedPnLQueryLimit {
		return 0, "", fmt.Errorf("今日成交达到 %d 条查询上限，无法确认已实现盈亏完整性", realizedPnLQueryLimit)
	}
	total := 0.0
	asset := ""
	for _, t := range trades {
		tradeAsset := strings.ToUpper(strings.TrimSpace(t.PnLAsset))
		feeAsset := strings.ToUpper(strings.TrimSpace(t.FeeAsset))
		if t.ClosedAt.Before(start) || t.ClosedAt.After(now) || !validTradePnL(t) || tradeAsset != circuitBreakerPnLAsset || (t.Fee != 0 && feeAsset != tradeAsset) || (asset != "" && asset != tradeAsset) {
			return 0, "", fmt.Errorf("今日成交包含未知/混合计价资产、未换算手续费或无效盈亏，无法确认已实现盈亏完整性")
		}
		asset = tradeAsset
		total += t.NetPnL
		if !finiteEquity(total) {
			return 0, "", fmt.Errorf("今日成交累计盈亏溢出，无法确认已实现盈亏完整性")
		}
	}
	return total, asset, nil
}

func validTradePnL(trade TradeOutcome) bool {
	return finiteEquity(trade.NetPnL) && finiteEquity(trade.Fee)
}

// validTradeStreak does not require a denomination when no fee is present:
// the sign of PnL is currency-invariant. A non-zero fee must be proven to use
// the same unit as PnL before it can affect the sign.
func validTradeStreak(trade TradeOutcome) bool {
	if !validTradePnL(trade) {
		return false
	}
	if trade.Fee == 0 {
		return true
	}
	pnlAsset := strings.ToUpper(strings.TrimSpace(trade.PnLAsset))
	feeAsset := strings.ToUpper(strings.TrimSpace(trade.FeeAsset))
	return pnlAsset != "" && pnlAsset == feeAsset
}

// consecutiveLosses 从最新成交起连续净亏损笔数（基线之后）
func (f *MetricsFeeder) consecutiveLosses(ctx context.Context, now time.Time, marks MetricsResetMarks) (int, error) {
	if f.trades == nil {
		return 0, nil
	}
	start := maxTime(now.Add(-consecutiveLossLookback), maxTime(marks.Streak, marks.All))
	if scanner, ok := f.trades.(TradeHistoryScanner); ok {
		count := 0
		invalidTrade := false
		err := scanner.ScanTradesBetween(ctx, start, now, func(t TradeOutcome) bool {
			if t.ClosedAt.Before(start) || t.ClosedAt.After(now) || !validTradeStreak(t) {
				invalidTrade = true
				return false
			}
			if t.NetPnL > 0 {
				return false
			}
			if t.NetPnL < 0 {
				count++
			}
			return true
		})
		if err != nil {
			return 0, fmt.Errorf("流式查询近期成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
		}
		if invalidTrade {
			return 0, fmt.Errorf("近期成交包含未能核实计价关系的手续费、超出查询范围或无效盈亏，无法确认连续亏损完整性")
		}
		return count, nil
	}
	trades, err := f.trades.TradesBetween(ctx, start, now, consecutiveLossQueryLimit)
	if err != nil {
		return 0, fmt.Errorf("查询近期成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
	}
	if len(trades) >= consecutiveLossQueryLimit {
		return 0, fmt.Errorf("近期成交达到 %d 条查询上限，无法确认连续亏损完整性", consecutiveLossQueryLimit)
	}
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].ClosedAt.After(trades[j].ClosedAt) })

	count := 0
	for _, t := range trades {
		if t.ClosedAt.Before(start) || t.ClosedAt.After(now) || !validTradeStreak(t) {
			return 0, fmt.Errorf("近期成交包含未能核实计价关系的手续费、超出查询范围或无效盈亏，无法确认连续亏损完整性")
		}
		if t.NetPnL > 0 {
			break
		}
		if t.NetPnL < 0 {
			count++
		}
	}
	return count, nil
}
