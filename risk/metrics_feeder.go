package risk

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
  - 最大回撤(%) = 以首次观测到的账户权益为基准，
    调整权益 = 基准权益 + 基准后已实现净盈亏 + (当前未实现 - 基准时未实现)，相对高水位的回撤百分比。
    不直接用交易所余额做曲线，避免利润划转（合约→现货）被误判为回撤。高水位只在内存中，进程重启后重新起算。
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
)

// ErrEquityUnavailable 暂无可用的权益数据源（如尚无运行中的合约 Bot），属预期情况
var ErrEquityUnavailable = errors.New("账户权益暂不可用")

// TradeOutcome 一笔已平仓成交的净结果
type TradeOutcome struct {
	Key      string    // 唯一键（如 buyOrderID:sellOrderID），用于增量去重
	NetPnL   float64   // 净盈亏（扣手续费）
	ClosedAt time.Time // 成交时间
}

// TradeHistorySource 成交历史数据源（由 main 用 storage 实现）
type TradeHistorySource interface {
	TradesBetween(ctx context.Context, start, end time.Time, limit int) ([]TradeOutcome, error)
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
}

// MetricsFeederOptions 可选参数
type MetricsFeederOptions struct {
	Interval time.Duration
	Location *time.Location   // 日界时区，nil 使用 time.Local
	Now      func() time.Time // 测试注入
}

// MetricsFeeder 周期性计算并喂入熔断指标
type MetricsFeeder struct {
	sink   MetricsSink
	trades TradeHistorySource
	equity EquitySource
	bots   BotProvider
	opts   MetricsFeederOptions

	mu sync.Mutex
	// 回撤基准
	hasBase        bool
	baseAt         time.Time
	baseEquity     float64
	baseUnrealized float64
	highWater      float64
	baseAllMark    time.Time
	// 基准后已实现盈亏的增量统计
	realizedSinceBase float64
	realizedCursor    time.Time
	seenTrades        map[string]time.Time
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
	return &MetricsFeeder{
		sink:       sink,
		trades:     trades,
		equity:     equity,
		bots:       bots,
		opts:       opts,
		seenTrades: make(map[string]time.Time),
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
func (f *MetricsFeeder) Tick(ctx context.Context) (MetricsSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.opts.Now()
	marks := f.sink.MetricsResetMarks()
	var snap MetricsSnapshot

	snap.UnrealizedPnL = f.totalUnrealized()

	realizedToday, err := f.realizedToday(ctx, now, marks)
	if err != nil {
		return snap, err
	}
	snap.RealizedToday = realizedToday
	snap.DailyPnL = realizedToday + snap.UnrealizedPnL

	losses, err := f.consecutiveLosses(ctx, now, marks)
	if err != nil {
		return snap, err
	}
	snap.ConsecutiveLosses = losses

	dd, ok, err := f.drawdown(ctx, now, marks, snap.UnrealizedPnL)
	if err != nil {
		return snap, err
	}
	snap.MaxDrawdownPct = dd
	snap.DrawdownAvailable = ok

	f.sink.UpdateMetrics(snap.DailyPnL, snap.MaxDrawdownPct, snap.ConsecutiveLosses)
	return snap, nil
}

// totalUnrealized 汇总所有 Bot 未实现盈亏；未初始化/无价格的 Bot 跳过
func (f *MetricsFeeder) totalUnrealized() float64 {
	if f.bots == nil {
		return 0
	}
	total := 0.0
	for _, bot := range f.bots.GetAllBots() {
		if bot == nil {
			continue
		}
		pnl, _, err := bot.GetPositionSummary()
		if err != nil {
			// 未启动或暂无价格的 Bot 属正常情况，不计入也不告警
			logger.Debug("🔍 [熔断喂数] 跳过 Bot 未实现盈亏: %v", err)
			continue
		}
		total += pnl
	}
	return total
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// realizedToday 今日（配置时区）已实现净盈亏，手动恢复基线之后
func (f *MetricsFeeder) realizedToday(ctx context.Context, now time.Time, marks MetricsResetMarks) (float64, error) {
	if f.trades == nil {
		return 0, nil
	}
	local := now.In(f.opts.Location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, f.opts.Location)
	start := maxTime(dayStart, marks.All)

	trades, err := f.trades.TradesBetween(ctx, start, now, realizedPnLQueryLimit)
	if err != nil {
		return 0, fmt.Errorf("查询今日成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
	}
	if len(trades) >= realizedPnLQueryLimit {
		logger.Warn("⚠️ [熔断喂数] 今日成交超过 %d 笔，已实现盈亏可能被截断", realizedPnLQueryLimit)
	}
	total := 0.0
	for _, t := range trades {
		total += t.NetPnL
	}
	return total, nil
}

// consecutiveLosses 从最新成交起连续净亏损笔数（基线之后）
func (f *MetricsFeeder) consecutiveLosses(ctx context.Context, now time.Time, marks MetricsResetMarks) (int, error) {
	if f.trades == nil {
		return 0, nil
	}
	start := maxTime(now.Add(-consecutiveLossLookback), maxTime(marks.Streak, marks.All))
	trades, err := f.trades.TradesBetween(ctx, start, now, consecutiveLossQueryLimit)
	if err != nil {
		return 0, fmt.Errorf("查询近期成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
	}
	sort.SliceStable(trades, func(i, j int) bool { return trades[i].ClosedAt.After(trades[j].ClosedAt) })

	count := 0
	for _, t := range trades {
		if t.NetPnL > 0 {
			break
		}
		if t.NetPnL < 0 {
			count++
		}
	}
	return count, nil
}

// drawdown 计算相对高水位的回撤百分比；无权益数据时返回 ok=false
func (f *MetricsFeeder) drawdown(ctx context.Context, now time.Time, marks MetricsResetMarks, unrealized float64) (float64, bool, error) {
	if f.equity == nil {
		return 0, false, nil
	}

	if !f.hasBase || marks.All.After(f.baseAllMark) {
		equity, err := f.equity.TotalEquity(ctx)
		if err != nil {
			// 权益暂不可得时不阻断其他指标，下轮重试
			if errors.Is(err, ErrEquityUnavailable) {
				logger.Debug("🔍 [熔断喂数] 暂无权益数据，本轮回撤不可用: %v", err)
			} else {
				logger.Warn("⚠️ [熔断喂数] 获取账户权益失败，本轮回撤不可用: %v", err)
			}
			return 0, false, nil
		}
		if equity <= 0 {
			logger.Warn("⚠️ [熔断喂数] 账户权益 %.2f 无效，本轮回撤不可用", equity)
			return 0, false, nil
		}
		f.hasBase = true
		f.baseAt = now
		f.baseEquity = equity
		f.baseUnrealized = unrealized
		f.highWater = equity
		f.baseAllMark = marks.All
		f.realizedSinceBase = 0
		f.realizedCursor = now
		f.seenTrades = make(map[string]time.Time)
		logger.Info("📊 [熔断喂数] 回撤基准权益 %.2f USDT", equity)
		return 0, true, nil
	}

	if err := f.accumulateRealized(ctx, now); err != nil {
		return 0, false, err
	}

	adjusted := f.baseEquity + f.realizedSinceBase + (unrealized - f.baseUnrealized)
	if adjusted > f.highWater {
		f.highWater = adjusted
	}
	if f.highWater <= 0 {
		return 0, true, nil
	}
	dd := (f.highWater - adjusted) / f.highWater * percentMultiplier
	if dd < 0 {
		dd = 0
	}
	return dd, true, nil
}

// accumulateRealized 增量累加基准之后的已实现净盈亏（带重叠窗口去重）
func (f *MetricsFeeder) accumulateRealized(ctx context.Context, now time.Time) error {
	if f.trades == nil {
		return nil
	}
	start := maxTime(f.realizedCursor.Add(-realizedCursorOverlap), f.baseAt)
	trades, err := f.trades.TradesBetween(ctx, start, now, realizedPnLQueryLimit)
	if err != nil {
		return fmt.Errorf("增量查询成交失败 (start=%s): %w", start.Format(time.RFC3339), err)
	}
	prevCursor := f.realizedCursor
	for _, t := range trades {
		if t.ClosedAt.Before(f.baseAt) {
			continue
		}
		if t.Key == "" {
			// 无唯一键无法去重：只统计上次游标之后的成交
			if t.ClosedAt.After(prevCursor) {
				f.realizedSinceBase += t.NetPnL
			}
			continue
		}
		if _, seen := f.seenTrades[t.Key]; seen {
			continue
		}
		f.seenTrades[t.Key] = t.ClosedAt
		f.realizedSinceBase += t.NetPnL
	}
	f.realizedCursor = now

	// 下轮查询起点不早于本轮起点，本轮起点之前的去重记录不会再被查到
	for k, ts := range f.seenTrades {
		if ts.Before(start) {
			delete(f.seenTrades, k)
		}
	}
	return nil
}
