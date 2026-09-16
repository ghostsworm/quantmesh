package regime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/indicators"
	"quantmesh/logger"
)

const (
	// refreshBars 增量轮询时拉取的根数（与已有缓冲有重叠即可判定连续）
	refreshBars = 10
	// pollDivisor 自动轮询间隔 = K 線周期 / pollDivisor
	pollDivisor = 12
	// minAutoPoll / maxAutoPoll 自动轮询间隔的上下限
	minAutoPoll = 15 * time.Second
	maxAutoPoll = 5 * time.Minute
	// secondsTimestampLimit 小于该值的时间戳按秒解释（兼容少数以秒为单位的适配器）
	secondsTimestampLimit = int64(1e12)
	msPerSecond           = int64(1000)
)

var (
	// ErrAlreadyStarted 重复 Start
	ErrAlreadyStarted = errors.New("regime: detector already started")
	// ErrStopped Stop 之后不可再 Start
	ErrStopped = errors.New("regime: detector stopped")
)

// KlineSource 历史 K 線数据源。exchange.Exchange 天然满足该接口；测试可注入假实现。
type KlineSource interface {
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error)
}

// Option 检测器可选项
type Option func(*Detector)

// WithClock 注入时钟（测试用）
func WithClock(now func() time.Time) Option {
	return func(d *Detector) {
		if now != nil {
			d.now = now
		}
	}
}

// WithPollInterval 覆盖轮询间隔（优先级高于配置的 PollIntervalSeconds）
func WithPollInterval(interval time.Duration) Option {
	return func(d *Detector) {
		if interval > 0 {
			d.poll = interval
		}
	}
}

// WithOnChange 注册状态切换回调。回调在检测器锁外、同步调用，应尽快返回且不得调用会阻塞的交易逻辑。
func WithOnChange(fn func(Change)) Option {
	return func(d *Detector) { d.onChange = fn }
}

// Detector 基于已收盘 K 線的市場状态检测器（并发安全）。
//
// 生命周期：NewDetector → Start(ctx)（非阻塞，后台先 bootstrap 再按轮询间隔增量拉取）→ Stop()。
// 也可不 Start，由调用方手动 Refresh 或通过 OnCandle 推送已收盘 K 線。
type Detector struct {
	cfg      RegimeConfig
	symbol   string
	src      KlineSource
	interval time.Duration
	poll     time.Duration
	now      func() time.Time
	onChange func(Change)

	refreshMu sync.Mutex // 串行化 Refresh，避免并发重复拉取

	mu         sync.RWMutex
	bars       []indicators.Candle // Time 为开盘时间（ms），严格递增
	cls        *classifier
	snap       Snapshot
	needResync bool

	lifeMu  sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewDetector 创建检测器。cfg 会先 WithDefaults 再 Validate。
func NewDetector(symbol string, cfg RegimeConfig, src KlineSource, opts ...Option) (*Detector, error) {
	if symbol == "" {
		return nil, fmt.Errorf("%w: symbol is empty", ErrInvalidConfig)
	}
	if src == nil {
		return nil, fmt.Errorf("%w: kline source is nil (symbol=%s)", ErrInvalidConfig, symbol)
	}
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("new regime detector for %s: %w", symbol, err)
	}
	interval, err := ParseKlineInterval(cfg.KlineInterval)
	if err != nil {
		return nil, fmt.Errorf("new regime detector for %s: %w", symbol, err)
	}

	d := &Detector{
		cfg:      cfg,
		symbol:   symbol,
		src:      src,
		interval: interval,
		poll:     autoPollInterval(interval, cfg.PollIntervalSeconds),
		now:      time.Now,
		cls:      newClassifier(cfg),
		snap:     Snapshot{Symbol: symbol, Interval: cfg.KlineInterval},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

func autoPollInterval(interval time.Duration, configuredSeconds int) time.Duration {
	if configuredSeconds > 0 {
		return time.Duration(configuredSeconds) * time.Second
	}
	return min(max(interval/pollDivisor, minAutoPoll), maxAutoPoll)
}

// Config 返回补齐默认值后的配置
func (d *Detector) Config() RegimeConfig { return d.cfg }

// PollInterval 返回生效的轮询间隔
func (d *Detector) PollInterval() time.Duration { return d.poll }

// Start 启动后台轮询（非阻塞）。首次 bootstrap 失败不会返回错误，而是记录日志并在下个轮询周期重试；
// 期间 Snapshot().Effective() 为 Unknown。ctx 取消或 Stop 均会结束后台 goroutine。
func (d *Detector) Start(ctx context.Context) error {
	d.lifeMu.Lock()
	defer d.lifeMu.Unlock()
	if d.stopped {
		return fmt.Errorf("start regime detector %s: %w", d.symbol, ErrStopped)
	}
	if d.started {
		return fmt.Errorf("start regime detector %s: %w", d.symbol, ErrAlreadyStarted)
	}
	runCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.done = make(chan struct{})
	d.started = true

	go d.run(runCtx, d.done)
	return nil
}

func (d *Detector) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	d.refreshAndLog(ctx)

	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.refreshAndLog(ctx)
		}
	}
}

func (d *Detector) refreshAndLog(ctx context.Context) {
	if err := d.Refresh(ctx); err != nil && ctx.Err() == nil {
		logger.Warn("⚠️ [Regime] %s %s 刷新 K 線失败: %v", d.symbol, d.cfg.KlineInterval, err)
	}
}

// Stop 停止后台 goroutine 并等待其退出。幂等；未 Start 也可调用。
func (d *Detector) Stop() {
	d.lifeMu.Lock()
	d.stopped = true
	cancel, done := d.cancel, d.done
	d.lifeMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Refresh 执行一次拉取：缓冲为空或需要重同步时全量 bootstrap，否则增量拉取最近几根。
func (d *Detector) Refresh(ctx context.Context) error {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	d.mu.RLock()
	full := len(d.bars) == 0 || d.needResync
	var lastOpen int64
	if len(d.bars) > 0 {
		lastOpen = d.bars[len(d.bars)-1].Time
	}
	d.mu.RUnlock()

	if !full {
		bars, err := d.fetch(ctx, refreshBars)
		if err != nil {
			return err
		}
		// 拉到的窗口与已有缓冲不重叠 → 中间有缺口，改为全量重建
		if len(bars) == 0 || bars[0].Time <= lastOpen {
			d.apply(bars, false)
			return nil
		}
		logger.Warn("⚠️ [Regime] %s K 線出现缺口（last=%d, fetched_first=%d），全量重建", d.symbol, lastOpen, bars[0].Time)
	}

	bars, err := d.fetch(ctx, d.cfg.BootstrapBars)
	if err != nil {
		return err
	}
	if len(bars) == 0 {
		return fmt.Errorf("bootstrap regime %s %s: no closed klines returned", d.symbol, d.cfg.KlineInterval)
	}
	d.apply(bars, true)
	return nil
}

// OnCandle 推送一根 K 線（例如来自 StartKlineStream 回调）。未收盘的 K 線被忽略。
// 若与缓冲不连续，则不应用并标记重同步，由下一次 Refresh 全量重建。
func (d *Detector) OnCandle(c *exchange.Candle) {
	if c == nil || !c.IsClosed {
		return
	}
	bar, ok := d.normalize(c, d.now())
	if !ok {
		return
	}
	d.mu.Lock()
	if n := len(d.bars); n > 0 && bar.Time > d.bars[n-1].Time+d.interval.Milliseconds() {
		d.needResync = true
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.apply([]indicators.Candle{bar}, false)
}

func (d *Detector) fetch(ctx context.Context, limit int) ([]indicators.Candle, error) {
	raw, err := d.src.GetHistoricalKlines(ctx, d.symbol, d.cfg.KlineInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch klines symbol=%s interval=%s limit=%d: %w", d.symbol, d.cfg.KlineInterval, limit, err)
	}
	now := d.now()
	byTime := make(map[int64]indicators.Candle, len(raw))
	for _, c := range raw {
		// 拉取路径不信任 IsClosed（部分适配器对最新一根未收盘 K 線也标 true），只按时间判定
		if bar, ok := d.normalize(c, now); ok {
			byTime[bar.Time] = bar
		}
	}
	bars := make([]indicators.Candle, 0, len(byTime))
	for _, b := range byTime {
		bars = append(bars, b)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time < bars[j].Time })
	return bars, nil
}

// normalize 把交易所 K 線转成开盘时间对齐的 indicators.Candle，并过滤未收盘与脏数据。
func (d *Detector) normalize(c *exchange.Candle, now time.Time) (indicators.Candle, bool) {
	if c == nil || c.Timestamp <= 0 {
		return indicators.Candle{}, false
	}
	if anyNaN(c.Open, c.High, c.Low, c.Close) || c.Low <= 0 || c.High < c.Low || c.Close <= 0 {
		return indicators.Candle{}, false
	}
	ts := c.Timestamp
	if ts < secondsTimestampLimit {
		ts *= msPerSecond
	}
	intervalMs := d.interval.Milliseconds()
	// 开盘时间或"收盘时间(=下一根开盘-1ms)"取整后都得到开盘时间
	openMs := ts - ts%intervalMs
	if openMs+intervalMs > now.UnixMilli() {
		return indicators.Candle{}, false
	}
	return indicators.Candle{
		Time: openMs, Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Volume: c.Volume,
	}, true
}

// apply 把新 K 線并入缓冲并逐根推进状态机；full=true 时先清空缓冲与状态。
func (d *Detector) apply(incoming []indicators.Candle, full bool) {
	d.mu.Lock()
	prev := d.snap.Regime

	if full {
		d.bars = d.bars[:0]
		d.cls = newClassifier(d.cfg)
		d.snap = Snapshot{Symbol: d.symbol, Interval: d.cfg.KlineInterval}
		d.needResync = false
	}

	var lastOpen int64 = math.MinInt64
	if n := len(d.bars); n > 0 {
		lastOpen = d.bars[n-1].Time
	}
	added := 0
	for _, b := range incoming {
		if b.Time > lastOpen {
			d.bars = append(d.bars, b)
			lastOpen = b.Time
			added++
		}
	}
	if added == 0 {
		d.mu.Unlock()
		return
	}
	if over := len(d.bars) - d.cfg.BootstrapBars; over > 0 {
		d.bars = append(d.bars[:0], d.bars[over:]...)
	}

	s := computeSeries(d.bars, d.cfg)
	start := max(0, len(d.bars)-added)
	var last metrics
	for i := start; i < len(d.bars); i++ {
		last = metricsAt(s, i, d.cfg)
		if last.ok {
			d.cls.step(last, d.bars[i].Time)
		}
	}

	lastBar := d.bars[len(d.bars)-1]
	d.snap = Snapshot{
		Symbol:        d.symbol,
		Interval:      d.cfg.KlineInterval,
		Regime:        d.cls.regime,
		Ready:         d.cls.regime != Unknown,
		ADX:           last.adx,
		PlusDI:        last.plusDI,
		MinusDI:       last.minusDI,
		EMA:           last.ema,
		EMASlope:      last.slope,
		ATR:           last.atr,
		ATRPercentile: last.atrPercentile,
		Close:         lastBar.Close,
		BarOpenTime:   time.UnixMilli(lastBar.Time),
		BarsInRegime:  d.cls.barsIn,
		UpdatedAt:     d.now(),
	}
	if d.snap.Ready {
		d.snap.RegimeSince = time.UnixMilli(d.cls.since)
	}
	cur := d.snap
	d.mu.Unlock()

	if cur.Regime != prev {
		cur.Stale = d.isStale(cur, d.now())
		logger.Info("📈 [Regime] %s %s 状态切换 %s → %s (ADX=%.1f slope=%.3f ATR=%.4f pct=%.0f)",
			d.symbol, d.cfg.KlineInterval, prev, cur.Regime, cur.ADX, cur.EMASlope, cur.ATR, cur.ATRPercentile)
		if d.onChange != nil {
			d.onChange(Change{From: prev, To: cur.Regime, Snapshot: cur})
		}
	}
}

func (d *Detector) isStale(s Snapshot, now time.Time) bool {
	if s.BarOpenTime.IsZero() {
		return true
	}
	lastClose := s.BarOpenTime.Add(d.interval)
	limit := time.Duration(float64(d.interval) * d.cfg.StaleMultiplier)
	return now.Sub(lastClose) > limit
}

// Snapshot 返回当前快照（Stale 按读取时刻计算）。
func (d *Detector) Snapshot() Snapshot {
	d.mu.RLock()
	s := d.snap
	d.mu.RUnlock()
	s.Stale = d.isStale(s, d.now())
	return s
}

// Regime 返回可用于决策的状态，等价于 Snapshot().Effective()。
func (d *Detector) Regime() Regime {
	return d.Snapshot().Effective()
}
