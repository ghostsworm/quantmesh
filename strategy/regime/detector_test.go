package regime

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/exchange"
)

const (
	testSymbol   = "ETHUSDT"
	testInterval = time.Hour
	// testWaitTimeout 后台 goroutine 条件等待上限（轮询间隔为毫秒级，正常远小于此值）
	testWaitTimeout = 2 * time.Second
	testWaitStep    = time.Millisecond
)

var testT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fakeClock 可手动推进的时钟
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// fakeSource 按时钟返回"开盘时间 ≤ now"的 K 線（含未收盘的最新一根，模拟 Binance 行为）
type fakeSource struct {
	mu         sync.Mutex
	clock      *fakeClock
	bars       []exchange.Candle // Timestamp = 开盘时间 ms
	closeStamp bool              // true 时以"收盘时间-1ms"作为 Timestamp
	err        error
	calls      atomic.Int64
	limits     []int
}

func (f *fakeSource) GetHistoricalKlines(_ context.Context, symbol, interval string, limit int) ([]*exchange.Candle, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}
	nowMs := f.clock.Now().UnixMilli()
	var avail []*exchange.Candle
	for i := range f.bars {
		if f.bars[i].Timestamp <= nowMs {
			c := f.bars[i]
			if f.closeStamp {
				c.Timestamp += testInterval.Milliseconds() - 1
			}
			avail = append(avail, &c)
		}
	}
	if len(avail) > limit {
		avail = avail[len(avail)-limit:]
	}
	return avail, nil
}

func (f *fakeSource) lastLimit() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.limits) == 0 {
		return 0
	}
	return f.limits[len(f.limits)-1]
}

// barGen 生成确定性的价格路径：range 为锯齿震荡，up/down 为带锯齿的单边
type barGen struct {
	bars  []exchange.Candle
	close float64
}

func newBarGen() *barGen { return &barGen{close: 1000} }

func (g *barGen) add(n int, drift float64) *barGen {
	for i := 0; i < n; i++ {
		idx := len(g.bars)
		open := g.close
		wiggle := 1.0
		if idx%2 == 1 {
			wiggle = -1.0
		}
		closePx := open + drift + wiggle
		if drift == 0 {
			// 震荡：两个不同频率的正弦叠加，围绕 1000 来回
			closePx = 1000 + 6*math.Sin(float64(idx)*0.9) + 3*math.Sin(float64(idx)*2.3)
		}
		high := max(open, closePx) + 0.5
		low := min(open, closePx) - 0.5
		g.bars = append(g.bars, exchange.Candle{
			Symbol: testSymbol, Open: open, High: high, Low: low, Close: closePx, Volume: 1,
			Timestamp: testT0.Add(time.Duration(idx) * testInterval).UnixMilli(), IsClosed: true,
		})
		g.close = closePx
	}
	return g
}

func testDetectorConfig() RegimeConfig {
	return RegimeConfig{
		Enabled:               true,
		KlineInterval:         "1h",
		ADXPeriod:             14,
		ADXEnterThreshold:     25,
		ADXExitThreshold:      20,
		EMAPeriod:             20,
		EMASlopeLookback:      3,
		EMASlopeMinATR:        0.05,
		MinDwellBars:          2,
		ATRPeriod:             14,
		ATRPercentileLookback: 30,
		BootstrapBars:         150,
	}
}

// afterBar 返回第 n 根 K 線（下标 n-1）收盘后 1 分钟的时刻，此时第 n 根尚在形成
func afterBar(n int) time.Time {
	return testT0.Add(time.Duration(n)*testInterval + time.Minute)
}

func newTestDetector(t *testing.T, bars []exchange.Candle, now time.Time, opts ...Option) (*Detector, *fakeSource, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: now}
	src := &fakeSource{clock: clock, bars: bars}
	opts = append([]Option{WithClock(clock.Now)}, opts...)
	d, err := NewDetector(testSymbol, testDetectorConfig(), src, opts...)
	if err != nil {
		t.Fatalf("NewDetector: %v", err)
	}
	return d, src, clock
}

func TestNewDetectorValidation(t *testing.T) {
	src := &fakeSource{clock: &fakeClock{now: testT0}}
	if _, err := NewDetector("", testDetectorConfig(), src); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty symbol err=%v", err)
	}
	if _, err := NewDetector(testSymbol, testDetectorConfig(), nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil source err=%v", err)
	}
	bad := testDetectorConfig()
	bad.KlineInterval = "1w"
	if _, err := NewDetector(testSymbol, bad, src); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad interval err=%v", err)
	}
	d, err := NewDetector(testSymbol, RegimeConfig{}, src)
	if err != nil {
		t.Fatalf("zero config should get defaults: %v", err)
	}
	if d.PollInterval() != 5*time.Minute {
		t.Fatalf("auto poll for 1h = %v, want 5m", d.PollInterval())
	}
	if d.Regime() != Unknown || !d.Snapshot().Stale {
		t.Fatal("fresh detector must be Unknown and stale")
	}
}

func TestAutoPollInterval(t *testing.T) {
	tests := []struct {
		interval time.Duration
		seconds  int
		want     time.Duration
	}{
		{interval: time.Minute, want: minAutoPoll},
		{interval: 15 * time.Minute, want: 75 * time.Second},
		{interval: 4 * time.Hour, want: maxAutoPoll},
		{interval: time.Hour, seconds: 30, want: 30 * time.Second},
	}
	for _, tt := range tests {
		if got := autoPollInterval(tt.interval, tt.seconds); got != tt.want {
			t.Errorf("autoPollInterval(%v,%d)=%v want %v", tt.interval, tt.seconds, got, tt.want)
		}
	}
}

func TestDetectorBootstrapClassifies(t *testing.T) {
	tests := []struct {
		name       string
		gen        *barGen
		closeStamp bool
		want       Regime
	}{
		{name: "range", gen: newBarGen().add(150, 0), want: Range},
		{name: "uptrend", gen: newBarGen().add(150, 3), want: TrendUp},
		{name: "downtrend", gen: newBarGen().add(150, -3), want: TrendDown},
		{name: "uptrend close-time timestamps", gen: newBarGen().add(150, 3), closeStamp: true, want: TrendUp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := len(tt.gen.bars)
			// 时钟停在第 n 根"形成中"：源会返回它，检测器必须丢弃
			d, src, _ := newTestDetector(t, tt.gen.bars, afterBar(n-1))
			src.closeStamp = tt.closeStamp
			if err := d.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if src.lastLimit() != testDetectorConfig().BootstrapBars {
				t.Fatalf("bootstrap limit=%d", src.lastLimit())
			}
			s := d.Snapshot()
			if s.Regime != tt.want || s.Effective() != tt.want || !s.Ready || s.Stale {
				t.Fatalf("snapshot %+v, want regime %s", s, tt.want)
			}
			wantLastOpen := testT0.Add(time.Duration(n-2) * testInterval)
			if !s.BarOpenTime.Equal(wantLastOpen) {
				t.Fatalf("last bar open=%v want %v (forming bar must be dropped)", s.BarOpenTime, wantLastOpen)
			}
			if s.ATR <= 0 || s.ATRPercentile < 0 || s.ATRPercentile > 100 || s.Symbol != testSymbol || s.Interval != "1h" {
				t.Fatalf("bad metrics %+v", s)
			}
		})
	}
}

func TestDetectorIncrementalTransition(t *testing.T) {
	gen := newBarGen().add(150, 0).add(60, 3)
	var mu sync.Mutex
	var changes []Change
	d, src, clock := newTestDetector(t, gen.bars, afterBar(150), WithOnChange(func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	}))

	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if d.Regime() != Range {
		t.Fatalf("after bootstrap regime=%s want range", d.Regime())
	}

	switchedAt := -1
	for n := 151; n <= 210; n++ {
		clock.Set(afterBar(n))
		if err := d.Refresh(ctx); err != nil {
			t.Fatalf("refresh bar %d: %v", n, err)
		}
		if src.lastLimit() != refreshBars {
			t.Fatalf("incremental refresh should fetch %d bars, got %d", refreshBars, src.lastLimit())
		}
		if d.Regime() == TrendUp && switchedAt < 0 {
			switchedAt = n
		}
	}
	if switchedAt < 0 {
		t.Fatalf("never switched to trend_up; last snapshot %+v", d.Snapshot())
	}
	s := d.Snapshot()
	if s.Regime != TrendUp || s.BarsInRegime < 2 || s.RegimeSince.IsZero() {
		t.Fatalf("unexpected final snapshot %+v", s)
	}

	mu.Lock()
	defer mu.Unlock()
	// bootstrap: Unknown→Range；随后恰好一次 Range→TrendUp（滞回+驻留，不应来回抖动）
	if len(changes) != 2 {
		t.Fatalf("changes=%d want 2: %+v", len(changes), changes)
	}
	if changes[0].From != Unknown || changes[0].To != Range || changes[1].From != Range || changes[1].To != TrendUp {
		t.Fatalf("unexpected change sequence %+v", changes)
	}
	if changes[1].Snapshot.Regime != TrendUp {
		t.Fatalf("change snapshot mismatch %+v", changes[1].Snapshot)
	}
}

func TestDetectorStale(t *testing.T) {
	gen := newBarGen().add(150, 3)
	d, _, clock := newTestDetector(t, gen.bars, afterBar(150))
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	lastClose := testT0.Add(150 * testInterval)

	clock.Set(lastClose.Add(2 * testInterval))
	if s := d.Snapshot(); s.Stale || d.Regime() != TrendUp {
		t.Fatalf("exactly 2 intervals should not be stale: %+v", s)
	}
	clock.Set(lastClose.Add(2*testInterval + time.Second))
	s := d.Snapshot()
	if !s.Stale || s.Regime != TrendUp || d.Regime() != Unknown {
		t.Fatalf("should be stale with Effective Unknown: %+v", s)
	}
}

func TestDetectorGapTriggersResync(t *testing.T) {
	gen := newBarGen().add(300, 3)
	d, src, clock := newTestDetector(t, gen.bars, afterBar(150))
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// 跳过 50 根：增量窗口（10 根）与缓冲不重叠 → 全量重建
	clock.Set(afterBar(200))
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if src.lastLimit() != testDetectorConfig().BootstrapBars {
		t.Fatalf("expected full resync fetch, last limit=%d", src.lastLimit())
	}
	want := testT0.Add(199 * testInterval)
	if s := d.Snapshot(); !s.BarOpenTime.Equal(want) || s.Regime != TrendUp {
		t.Fatalf("after resync snapshot %+v, want last open %v", s, want)
	}
}

func TestDetectorOnCandle(t *testing.T) {
	gen := newBarGen().add(160, 3)
	d, src, clock := newTestDetector(t, gen.bars, afterBar(150))
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	next := gen.bars[150]

	// 未收盘 → 忽略
	unclosed := next
	unclosed.IsClosed = false
	clock.Set(afterBar(151))
	d.OnCandle(&unclosed)
	d.OnCandle(nil)
	if got := d.Snapshot().BarOpenTime; !got.Equal(testT0.Add(149 * testInterval)) {
		t.Fatalf("unclosed candle applied, last open=%v", got)
	}

	// 已收盘、收盘时间戳口径、连续 → 应用
	closed := next
	closed.Timestamp += testInterval.Milliseconds() - 1
	d.OnCandle(&closed)
	if got := d.Snapshot().BarOpenTime; !got.Equal(testT0.Add(150 * testInterval)) {
		t.Fatalf("closed candle not applied, last open=%v", got)
	}
	// 重复推送 → 幂等
	d.OnCandle(&closed)
	if got := d.Snapshot().BarOpenTime; !got.Equal(testT0.Add(150 * testInterval)) {
		t.Fatalf("duplicate candle changed state, last open=%v", got)
	}

	// 时钟未到收盘的"已收盘"标记 → 丢弃
	future := gen.bars[155]
	d.OnCandle(&future)

	// 不连续 → 不应用，下一次 Refresh 全量重建
	clock.Set(afterBar(158))
	gap := gen.bars[157]
	d.OnCandle(&gap)
	if got := d.Snapshot().BarOpenTime; !got.Equal(testT0.Add(150 * testInterval)) {
		t.Fatalf("gap candle applied, last open=%v", got)
	}
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if src.lastLimit() != testDetectorConfig().BootstrapBars {
		t.Fatalf("gap via OnCandle should force full resync, last limit=%d", src.lastLimit())
	}
	if got := d.Snapshot().BarOpenTime; !got.Equal(testT0.Add(157 * testInterval)) {
		t.Fatalf("after resync last open=%v", got)
	}
}

func TestDetectorRefreshErrors(t *testing.T) {
	d, src, _ := newTestDetector(t, nil, afterBar(10))
	if err := d.Refresh(context.Background()); err == nil {
		t.Fatal("empty source should error on bootstrap")
	}
	boom := errors.New("boom")
	src.err = boom
	err := d.Refresh(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("error should wrap source error: %v", err)
	}
}

func TestDetectorWarmupNotReady(t *testing.T) {
	gen := newBarGen().add(40, 3) // 少于 RequiredBars=44
	d, _, _ := newTestDetector(t, gen.bars, afterBar(40))
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	s := d.Snapshot()
	if s.Ready || s.Regime != Unknown || d.Regime() != Unknown {
		t.Fatalf("warmup snapshot should not be ready: %+v", s)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(testWaitStep)
	}
}

func TestDetectorStartStopLifecycle(t *testing.T) {
	gen := newBarGen().add(150, 3)
	d, src, _ := newTestDetector(t, gen.bars, afterBar(150), WithPollInterval(time.Millisecond))

	ctx := context.Background()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start err=%v", err)
	}
	waitFor(t, "bootstrap + polls", func() bool { return src.calls.Load() >= 3 && d.Regime() == TrendUp })

	d.Stop()
	callsAfterStop := src.calls.Load()
	d.Stop() // 幂等
	if err := d.Start(ctx); !errors.Is(err, ErrStopped) {
		t.Fatalf("Start after Stop err=%v", err)
	}
	// Stop 已等待 goroutine 退出，之后不应再有拉取
	time.Sleep(5 * time.Millisecond)
	if got := src.calls.Load(); got != callsAfterStop {
		t.Fatalf("source called after Stop: %d -> %d", callsAfterStop, got)
	}
}

func TestDetectorContextCancelEndsLoop(t *testing.T) {
	gen := newBarGen().add(150, 3)
	d, src, _ := newTestDetector(t, gen.bars, afterBar(150), WithPollInterval(time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "first fetch", func() bool { return src.calls.Load() >= 1 })
	cancel()

	d.lifeMu.Lock()
	done := d.done
	d.lifeMu.Unlock()
	select {
	case <-done:
	case <-time.After(testWaitTimeout):
		t.Fatal("goroutine did not exit after context cancel")
	}
	d.Stop()
}

func TestDetectorStopWithoutStart(t *testing.T) {
	d, _, _ := newTestDetector(t, nil, afterBar(1))
	d.Stop()
	if err := d.Start(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("Start after Stop err=%v", err)
	}
}

func TestDetectorConcurrentAccess(t *testing.T) {
	gen := newBarGen().add(150, 0).add(50, -3)
	d, _, clock := newTestDetector(t, gen.bars, afterBar(150))
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = d.Snapshot()
					_ = d.Regime()
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 151; n <= 200; n++ {
			bar := gen.bars[n-1]
			d.OnCandle(&bar)
		}
	}()
	for n := 151; n <= 200; n++ {
		clock.Set(afterBar(n))
		if err := d.Refresh(ctx); err != nil {
			t.Errorf("refresh %d: %v", n, err)
		}
	}
	close(stop)
	wg.Wait()
	if d.Regime() != TrendDown {
		t.Fatalf("final regime=%s want trend_down", d.Regime())
	}
}
