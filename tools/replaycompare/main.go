// Command replaycompare 用回放引擎（backtest/replay）校準 R5 網格功能的默認值。
//
// 讀取 data.binance.vision 的 USD-M 1m K 線（zip 或 csv）與資金費率，按「交易對 × 基礎間隔 × 時間段 × 功能變體」
// 矩陣回放，輸出 JSON 與 Markdown 報告。K 線內路徑為假設（見 ADR 2026-09-17-replay-backtest 已知差距 7）。
//
// 用法：
//
//	go run ./tools/replaycompare -data data/kline/binance_vision -start 2026-06-19 -end 2026-09-16 -requested-end 2026-09-17 \
//	    -bases 0.0015,0.003,0.0005 -out docs/reports/2026-09-17-replay-calibration
//
// 報告正文（人工結論）默認讀取已提交的 tools/replaycompare/notes/2026-09-17.md（-notes 可替換，-notes "" 不插入）。
// 需要 K 線 regime 或資金費監控的變體通過 replay.Config.Setup/OnTick 在模擬時間上注入（見 hooks.go）。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/logger"
)

const (
	dateLayout         = "2006-01-02"
	defaultSegmentDays = 30
	defaultSegments    = 3
	// defaultNotesPath 已提交的報告正文（相對倉庫根目錄），保證報告可重現
	defaultNotesPath = "tools/replaycompare/notes/2026-09-17.md"
)

// symbolSpecs Binance USD-M 精度；BTC 最小名義價值 100 USDT，因此每格金額不能用 20 USDT
var symbolSpecs = map[string]SymbolSpec{
	"BTCUSDT": {Symbol: "BTCUSDT", PriceDecimals: 1, QuantityDecimals: 3, OrderQuantity: 150},
	"ETHUSDT": {Symbol: "ETHUSDT", PriceDecimals: 2, QuantityDecimals: 3, OrderQuantity: 25},
}

type options struct {
	dataDir     string
	symbols     []string
	start, end  time.Time // end 不含
	reqEnd      time.Time // 請求的覆蓋終點（不含），用於統計缺失天
	bases       []BaseSpec
	variants    map[string]bool
	segments    []string
	profiles    []string
	steps       int
	workers     int
	out         string
	notes       string
	logLevel    string
	segmentDays int
}

func parseFlags(args []string) (options, error) {
	fs := flag.NewFlagSet("replaycompare", flag.ContinueOnError)
	var o options
	var symbols, start, end, reqEnd, bases, variants, segments, profiles string
	fs.StringVar(&o.dataDir, "data", "data/kline/binance_vision", "數據根目錄（<root>/<SYMBOL>/1m, <root>/<SYMBOL>/fundingRate）")
	fs.StringVar(&symbols, "symbols", "BTCUSDT,ETHUSDT", "交易對，逗號分隔")
	fs.StringVar(&start, "start", "2026-06-19", "開始日期（UTC，含）")
	fs.StringVar(&end, "end", "2026-09-16", "結束日期（UTC，不含）")
	fs.StringVar(&reqEnd, "requested-end", "", "請求的覆蓋終點（UTC，不含；缺省=end），用於報告缺失天")
	fs.StringVar(&bases, "bases", "0.0015,0.003", "基礎間隔（起始價比例），逗號分隔")
	fs.StringVar(&variants, "variants", "", "只運行這些變體（逗號分隔，缺省全部）")
	fs.StringVar(&segments, "segments", "", "只運行這些段（full,seg1,seg2,seg3；缺省全部）")
	fs.StringVar(&profiles, "profiles", strings.Join(DefaultProfileNames(), ","), "運行的口徑（live_default,no_cleaner,no_order_cap）")
	fs.IntVar(&o.steps, "steps", replay.DefaultIntrabarStepsPerLeg, "K 線內路徑每段插值步數（1 = 每分鐘 4 個點）")
	fs.IntVar(&o.workers, "workers", max(1, runtime.NumCPU()-2), "並行數")
	fs.StringVar(&o.out, "out", "", "報告輸出路徑前綴（生成 .json 與 .md）")
	fs.StringVar(&o.notes, "notes", defaultNotesPath, "插入 Markdown 報告開頭的人工結論文件（空字符串表示不插入）")
	fs.StringVar(&o.logLevel, "log-level", "ERROR", "倉位管理器日誌級別")
	fs.IntVar(&o.segmentDays, "segment-days", defaultSegmentDays, "分段天數")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.out == "" {
		return o, fmt.Errorf("-out is required")
	}
	var err error
	if o.start, err = time.Parse(dateLayout, start); err != nil {
		return o, fmt.Errorf("parse -start %q: %w", start, err)
	}
	if o.end, err = time.Parse(dateLayout, end); err != nil {
		return o, fmt.Errorf("parse -end %q: %w", end, err)
	}
	if !o.end.After(o.start) {
		return o, fmt.Errorf("-end %s must be after -start %s", end, start)
	}
	o.reqEnd = o.end
	if reqEnd != "" {
		if o.reqEnd, err = time.Parse(dateLayout, reqEnd); err != nil {
			return o, fmt.Errorf("parse -requested-end %q: %w", reqEnd, err)
		}
	}
	for _, s := range splitList(symbols) {
		s = strings.ToUpper(s)
		if _, ok := symbolSpecs[s]; !ok {
			return o, fmt.Errorf("unsupported symbol %s (known: BTCUSDT, ETHUSDT)", s)
		}
		o.symbols = append(o.symbols, s)
	}
	for _, b := range splitList(bases) {
		var r float64
		if _, err := fmt.Sscanf(b, "%g", &r); err != nil || r <= 0 {
			return o, fmt.Errorf("invalid base ratio %q", b)
		}
		o.bases = append(o.bases, BaseSpec{Name: fmt.Sprintf("%.2f%%", r*100), IntervalRatio: r})
	}
	if variants != "" {
		o.variants = map[string]bool{}
		for _, v := range splitList(variants) {
			o.variants[v] = true
		}
	}
	o.segments = splitList(segments)
	o.profiles = splitList(profiles)
	return o, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	o, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "replaycompare: %v\n", err)
		os.Exit(2)
	}
	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "replaycompare: %v\n", err)
		os.Exit(1)
	}
}

func run(o options) error {
	logger.SetLevel(logger.ParseLogLevel(o.logLevel))
	wallStart := time.Now()
	startMs, endMs := o.start.UnixMilli(), o.end.UnixMilli()
	segs := SplitSegments(startMs, endMs, o.segmentDays, defaultSegments)
	if len(o.segments) > 0 {
		keep := map[string]bool{}
		for _, s := range o.segments {
			keep[s] = true
		}
		filtered := segs[:0]
		for _, s := range segs {
			if keep[s.Name] {
				filtered = append(filtered, s)
			}
		}
		segs = filtered
	}
	profiles := AllProfiles()
	if len(o.profiles) > 0 {
		keep := map[string]bool{}
		for _, p := range o.profiles {
			keep[p] = true
		}
		filtered := profiles[:0]
		for _, p := range profiles {
			if keep[p.Name] {
				filtered = append(filtered, p)
			}
		}
		profiles = filtered
	}
	variants := AllVariants()
	if o.variants != nil {
		filtered := variants[:0]
		for _, v := range variants {
			if o.variants[v.Name] {
				filtered = append(filtered, v)
			}
		}
		variants = filtered
	}

	rep := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Params: ReportParams{
			Start: o.start.Format(dateLayout), End: o.end.Format(dateLayout), Bases: o.bases, Variants: variants,
			Segments: segs, IntrabarPath: string(replay.IntrabarPathAuto), StepsPerLeg: o.steps,
			PointsPerMinute: 1 + 3*o.steps, MakerFee: defaultMakerFee, TakerFee: defaultTakerFee,
			InitialCapital: defaultInitialCapital, Profiles: profiles, WindowSize: defaultWindowSize,
			MaxPositionLayers: defaultMaxPositionLayers, Workers: o.workers,
		},
	}

	var jobs []Job
	for _, sym := range o.symbols {
		spec := symbolSpecs[sym]
		rep.Params.Symbols = append(rep.Params.Symbols, spec)
		candles, err := LoadKlines(filepath.Join(o.dataDir, sym, "1m"), sym, startMs, endMs)
		if err != nil {
			return fmt.Errorf("load klines %s: %w", sym, err)
		}
		funding, err := LoadFunding(filepath.Join(o.dataDir, sym, "fundingRate"))
		if err != nil {
			return fmt.Errorf("load funding %s: %w", sym, err)
		}
		cov := ComputeCoverage(sym, candles, startMs, o.reqEnd.UnixMilli())
		cov.FundingPoints = len(funding)
		if len(funding) > 0 {
			cov.FundingLast = fmtTime(funding[len(funding)-1].Timestamp)
		}
		rep.Coverage = append(rep.Coverage, cov)
		fmt.Printf("[%s] 1m bars=%d funding points=%d missing days=%v\n", sym, len(candles), len(funding), cov.MissingDays)

		hourly, err := AggregateCandles(candles, hourMs)
		if err != nil {
			return fmt.Errorf("aggregate %s: %w", sym, err)
		}
		startPriceOf := func(seg Segment) float64 {
			if c := sliceCandles(candles, seg.StartMs, seg.EndMs); len(c) > 0 {
				return c[0].Open
			}
			return 0
		}
		for _, seg := range segs {
			d, err := RunRegimeDiag(sym, hourly, seg, o.bases, startPriceOf, spec.PriceDecimals)
			if err != nil {
				return fmt.Errorf("regime diag %s %s: %w", sym, seg.Name, err)
			}
			rep.RegimeDiag = append(rep.RegimeDiag, d)
		}

		for _, prof := range profiles {
			for _, base := range o.bases {
				for _, seg := range segs {
					segCandles := sliceCandles(candles, seg.StartMs, seg.EndMs)
					for _, v := range variants {
						jobs = append(jobs, Job{Profile: prof, Spec: spec, Base: base, Segment: seg, Variant: v, Candles: segCandles, Hourly: hourly, Funding: funding, Steps: o.steps})
					}
				}
			}
		}
	}

	rep.Runs = RunAll(jobs, o.workers, func(done, total int, s RunSummary) {
		status := "ok"
		switch {
		case s.Blocked != "":
			status = "blocked"
		case s.Error != "":
			status = "error: " + s.Error
		}
		fmt.Printf("[%d/%d] %s %s %s %s %-18s net=%.2f fills=%d gap=%.0fh %.1fs %s\n", done, total, s.Profile, s.Symbol, s.Base, s.Segment, s.Variant, s.NetPnL, s.Fills, s.MaxNoFillGapHours, s.RuntimeSec, status)
	})
	rep.WallClockSec = time.Since(wallStart).Seconds()
	for _, r := range rep.Runs {
		if r.Error != "" {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s %s %s %s %s: %s", r.Profile, r.Symbol, r.Base, r.Segment, r.Variant, r.Error))
		}
	}
	var notes string
	if o.notes != "" {
		b, err := os.ReadFile(o.notes)
		if err != nil {
			return fmt.Errorf("read notes %s: %w", o.notes, err)
		}
		notes = string(b)
	}
	if err := WriteReports(o.out, rep, notes); err != nil {
		return err
	}
	fmt.Printf("done in %.1fs; reports: %s.json / %s.md\n", rep.WallClockSec, o.out, o.out)
	return nil
}
