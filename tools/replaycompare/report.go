package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	reportFileMode = 0o644
	reportDirMode  = 0o755
	baselineName   = "baseline"
)

// ReportParams 運行參數
type ReportParams struct {
	Start             string       `json:"start"`
	End               string       `json:"end_exclusive"`
	Symbols           []SymbolSpec `json:"symbols"`
	Bases             []BaseSpec   `json:"bases"`
	Variants          []Variant    `json:"variants"`
	Segments          []Segment    `json:"segments"`
	IntrabarPath      string       `json:"intrabar_path"`
	StepsPerLeg       int          `json:"intrabar_steps_per_leg"`
	PointsPerMinute   int          `json:"points_per_minute"`
	MakerFee          float64      `json:"maker_fee_rate"`
	TakerFee          float64      `json:"taker_fee_rate"`
	InitialCapital    float64      `json:"initial_capital"`
	Profiles          []Profile    `json:"profiles"`
	WindowSize        int          `json:"window_size"`
	MaxPositionLayers int          `json:"max_position_layers"`
	Workers           int          `json:"workers"`
}

// Report 完整報告
type Report struct {
	GeneratedAt  string       `json:"generated_at"`
	Params       ReportParams `json:"params"`
	Coverage     []Coverage   `json:"coverage"`
	RegimeDiag   []RegimeDiag `json:"regime_diagnostics"`
	Runs         []RunSummary `json:"runs"`
	WallClockSec float64      `json:"wall_clock_sec"`
	Errors       []string     `json:"errors,omitempty"`
}

// WriteReports 寫出 <prefix>.json 與 <prefix>.md
func WriteReports(prefix string, rep Report, notes string) error {
	if err := os.MkdirAll(filepath.Dir(prefix), reportDirMode); err != nil {
		return fmt.Errorf("create report dir for %s: %w", prefix, err)
	}
	js, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report json: %w", err)
	}
	if err := os.WriteFile(prefix+".json", append(js, '\n'), reportFileMode); err != nil {
		return fmt.Errorf("write %s.json: %w", prefix, err)
	}
	if err := os.WriteFile(prefix+".md", []byte(RenderMarkdown(rep, notes)), reportFileMode); err != nil {
		return fmt.Errorf("write %s.md: %w", prefix, err)
	}
	return nil
}

// RenderMarkdown 生成 Markdown：人工結論（notes）+ 自動生成的數據表
func RenderMarkdown(rep Report, notes string) string {
	var b strings.Builder
	if strings.TrimSpace(notes) != "" {
		b.WriteString(strings.TrimRight(notes, "\n"))
		b.WriteString("\n\n")
	} else {
		b.WriteString("# 回放校準報告\n\n")
	}
	p := rep.Params
	b.WriteString("## 附錄 A：運行參數與數據覆蓋（自動生成）\n\n")
	fmt.Fprintf(&b, "- 生成時間：%s；總耗時 %.0f 秒（%d 並行）\n", rep.GeneratedAt, rep.WallClockSec, p.Workers)
	fmt.Fprintf(&b, "- 回放區間：%s ～ %s（UTC，不含終點）\n", p.Start, p.End)
	fmt.Fprintf(&b, "- K 線內路徑：%s，每段 %d 步（每分鐘 %d 個價格點，每點調用一次 AdjustOrders）\n", p.IntrabarPath, p.StepsPerLeg, p.PointsPerMinute)
	fmt.Fprintf(&b, "- 費率 maker %.2f%% / taker %.2f%%；初始資金 %.0f USDT，模擬保證金；LONG；買/賣窗口 %d；max_position_layers=%d；撮合：穿價成交、queue_factor=0、參與率 1.0\n",
		p.MakerFee*100, p.TakerFee*100, p.InitialCapital, p.WindowSize, p.MaxPositionLayers)
	for _, prof := range p.Profiles {
		fmt.Fprintf(&b, "- 口徑 `%s`：%s\n", prof.Name, prof.Description)
	}
	for _, s := range p.Symbols {
		fmt.Fprintf(&b, "- %s：每格 %.0f USDT，價格精度 %d，數量精度 %d\n", s.Symbol, s.OrderQuantity, s.PriceDecimals, s.QuantityDecimals)
	}
	b.WriteString("\n| 交易對 | 首根 | 末根 | 1m 根數 | 期望根數 | 缺失天 | 不完整天 | 資金費點數 | 最後資金費 |\n|---|---|---|---:|---:|---|---|---:|---|\n")
	for _, c := range rep.Coverage {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %s | %s | %d | %s |\n", c.Symbol, c.FirstBar, c.LastBar, c.Bars, c.ExpectedBars,
			listOrDash(c.MissingDays), listOrDash(c.IncompleteDays), c.FundingPoints, dashIfEmpty(c.FundingLast))
	}
	b.WriteString("\n時間段：")
	for i, s := range p.Segments {
		if i > 0 {
			b.WriteString("；")
		}
		fmt.Fprintf(&b, "%s = %s ～ %s", s.Name, s.Start, s.End)
	}
	b.WriteString("\n\n")

	b.WriteString("## 附錄 B：回放結果（自動生成）\n\n")
	b.WriteString("列說明：淨盈虧 = 已實現 + 未實現 − 手續費 − 資金費；資金費正數為支付；回撤為權益最大回撤；格數 = 平倉成交次數；")
	b.WriteString("淨利/費 = (已實現 − 手續費)/手續費；在場% = 有持倉時間占比；最大名義 = 最大持倉名義價值；Sharpe* = 日收益 mean/std×√365（僅作相對比較）。")
	b.WriteString("Δ 為相對同組 baseline 的淨盈虧差；最長停擺 = 最長無成交間隔（小時）。被阻塞的變體不列出（見正文）。\n\n")
	renderDeltaSummary(&b, rep)
	renderRegimeRuns(&b, rep)
	for _, prof := range p.Profiles {
		fmt.Fprintf(&b, "### 口徑 %s：%s\n\n", prof.Name, prof.Description)
		for _, sym := range p.Symbols {
			for _, base := range p.Bases {
				for _, seg := range p.Segments {
					renderRunTable(&b, sym, base, seg, filterRuns(rep.Runs, prof.Name, sym.Symbol, base.Name, seg.Name))
				}
			}
		}
	}

	b.WriteString("## 附錄 C：離線 regime 診斷（自動生成，不經過網格）\n\n")
	b.WriteString("逐小時在模擬時間上驅動 `regime.Detector`（默認參數，1h，K 線源只返回評估時刻前已收盤的 K 線）。")
	b.WriteString("未來 24h 收益/振幅只用於事後評估。自適應倍數 = `adaptive_interval`（k=0.5）選擇的間隔 / base 的平均值；")
	b.WriteString("含 regime = 再乘 LONG 的 `IntervalScale` 並量化。上沿越界% = 收盤價 > EMA+3×ATR 的小時占比（`upper_bound_freeze` 會暫停開倉的時間）。\n\n")
	b.WriteString("| 交易對 | 段 | 小時 | 切換次數 | 拉取失敗 | 狀態 | 占比 | 未來 24h 平均收益% | 未來 24h 平均振幅% |\n|---|---|---:|---:|---:|---|---:|---:|---:|\n")
	for _, d := range rep.RegimeDiag {
		for i, name := range sortedRegimeNames(d.ByRegime) {
			st := d.ByRegime[name]
			if i == 0 {
				fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s | %.1f%% | %+.3f | %.2f |\n", d.Symbol, d.Segment, d.Hours, d.Switches, d.RefreshErrors, name, st.SharePct, st.MeanFwd24hRetPct, st.MeanFwd24hRangePct)
			} else {
				fmt.Fprintf(&b, "| | | | | | %s | %.1f%% | %+.3f | %.2f |\n", name, st.SharePct, st.MeanFwd24hRetPct, st.MeanFwd24hRangePct)
			}
		}
	}
	b.WriteString("\n| 交易對 | 段 | 上沿越界% |")
	for _, base := range p.Bases {
		fmt.Fprintf(&b, " 自適應倍數 %s | 含 regime %s |", base.Name, base.Name)
	}
	b.WriteString("\n|---|---|---:|")
	for range p.Bases {
		b.WriteString("---:|---:|")
	}
	b.WriteString("\n")
	for _, d := range rep.RegimeDiag {
		fmt.Fprintf(&b, "| %s | %s | %.1f%% |", d.Symbol, d.Segment, d.AutoBoundHits)
		for _, base := range p.Bases {
			fmt.Fprintf(&b, " %.2f | %.2f |", d.AdaptiveMultiple[base.Name], d.PolicyMultiple[base.Name])
		}
		b.WriteString("\n")
	}
	if len(rep.Errors) > 0 {
		b.WriteString("\n## 運行錯誤\n\n")
		for _, e := range rep.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return b.String()
}

// renderDeltaSummary 匯總表：每組 baseline 淨盈虧、最長停擺，以及各可運行變體相對 baseline 的 Δ
func renderDeltaSummary(b *strings.Builder, rep Report) {
	p := rep.Params
	var names []string
	for _, v := range p.Variants {
		if v.Blocked == "" && v.Name != baselineName {
			names = append(names, v.Name)
		}
	}
	b.WriteString("### 匯總：相對 baseline 的淨盈虧差\n\n| 口徑 | 交易對 | 間隔 | 段 | baseline 淨盈虧 | baseline 成交 | baseline 最長停擺 h |")
	for _, n := range names {
		fmt.Fprintf(b, " Δ %s |", n)
	}
	b.WriteString("\n|---|---|---|---|---:|---:|---:|")
	for range names {
		b.WriteString("---:|")
	}
	b.WriteString("\n")
	for _, prof := range p.Profiles {
		for _, sym := range p.Symbols {
			for _, base := range p.Bases {
				for _, seg := range p.Segments {
					rows := filterRuns(rep.Runs, prof.Name, sym.Symbol, base.Name, seg.Name)
					bl := findRun(rows, baselineName)
					if bl == nil {
						continue
					}
					fmt.Fprintf(b, "| %s | %s | %s | %s | %.2f | %d | %.0f |", prof.Name, sym.Symbol, base.Name, seg.Name, bl.NetPnL, bl.Fills, bl.MaxNoFillGapHours)
					for _, n := range names {
						if r := findRun(rows, n); r != nil {
							fmt.Fprintf(b, " %+.2f |", r.NetPnL-bl.NetPnL)
						} else {
							b.WriteString(" — |")
						}
					}
					b.WriteString("\n")
				}
			}
		}
	}
	b.WriteString("\n")
}

// renderRegimeRuns 注入 regime 檢測器的運行：狀態占比、間隔修改次數與平均倍數（每 30s 模擬時間採樣）
func renderRegimeRuns(b *strings.Builder, rep Report) {
	var rows []RunSummary
	for _, r := range rep.Runs {
		if r.Error == "" && r.Blocked == "" && r.RegimeSharePct != nil {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("### 回放中的 regime 狀態與間隔（自動生成）\n\n")
	b.WriteString("每 30 秒模擬時間採樣一次（與實盤間隔控制循環周期相同）。平均間隔倍數 = 當前 price_interval / 配置間隔；段首預熱期檢測器拉取失敗計入「拉取失敗」，期間狀態為 unknown、行為與基線一致。\n\n")
	b.WriteString("| 口徑 | 交易對 | 間隔 | 段 | 變體 | range% | trend_up% | trend_down% | unknown% | 間隔修改次數 | 平均間隔倍數 | 拉取失敗 |\n|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %.1f | %.1f | %.1f | %.1f | %d | %.2f | %d |\n", r.Profile, r.Symbol, r.Base, r.Segment, r.Variant,
			r.RegimeSharePct["range"], r.RegimeSharePct["trend_up"], r.RegimeSharePct["trend_down"], r.RegimeSharePct["unknown"],
			r.IntervalChanges, r.MeanIntervalMultiple, r.RegimeRefreshErrors)
	}
	b.WriteString("\n")
}

// findRun 返回組內指定變體的成功運行
func findRun(rows []RunSummary, variant string) *RunSummary {
	for i := range rows {
		if rows[i].Variant == variant && rows[i].Error == "" && rows[i].Blocked == "" {
			return &rows[i]
		}
	}
	return nil
}

// renderRunTable 單組（口徑 × 交易對 × 間隔 × 段）明細表
func renderRunTable(b *strings.Builder, sym SymbolSpec, base BaseSpec, seg Segment, rows []RunSummary) {
	if len(rows) == 0 {
		return
	}
	bl := findRun(rows, baselineName)
	first := rows[0]
	fmt.Fprintf(b, "#### %s · 間隔 %s（%.*f）· %s（%s ～ %s，%.*f → %.*f）\n\n", sym.Symbol, base.Name, sym.PriceDecimals, first.Interval,
		seg.Name, seg.Start, seg.End, sym.PriceDecimals, first.StartPrice, sym.PriceDecimals, first.EndPrice)
	b.WriteString("| 變體 | 淨盈虧 | Δ | 已實現 | 未實現 | maker 費 | taker 費 | 資金費 | 回撤 | 回撤% | 成交 | maker 占比 | 格數 | 每格淨利 | 淨利/費 | 在場% | 最大名義 | 平均名義 | Sharpe* | 撤單 | 最長停擺 h | 耗時s |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		if r.Blocked != "" {
			continue
		}
		if r.Error != "" {
			fmt.Fprintf(b, "| %s | 錯誤：%s |||||||||||||||||||||\n", r.Variant, r.Error)
			continue
		}
		delta := "—"
		if bl != nil && r.Variant != baselineName {
			delta = fmt.Sprintf("%+.2f", r.NetPnL-bl.NetPnL)
		}
		fmt.Fprintf(b, "| %s | %.2f | %s | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %d | %.1f%% | %d | %.3f | %.2f | %.1f | %.0f | %.0f | %.2f | %d | %.0f | %.1f |\n",
			r.Variant, r.NetPnL, delta, r.RealizedPnL, r.UnrealizedPnL, r.FeesMaker, r.FeesTaker, r.FundingPaid,
			r.MaxDrawdownAbs, r.MaxDrawdownPct, r.Fills, r.MakerRatio*100, r.ClosedGrids, r.NetProfitPerGrid, r.ProfitToFee,
			r.TimeInMarketPct, r.MaxAbsNotional, r.AvgAbsNotional, r.DailySharpeLike, r.OrdersCanceled, r.MaxNoFillGapHours, r.RuntimeSec)
	}
	b.WriteString("\n")
}

func filterRuns(runs []RunSummary, profile, symbol, base, seg string) []RunSummary {
	var out []RunSummary
	for _, r := range runs {
		if r.Profile == profile && r.Symbol == symbol && r.Base == base && r.Segment == seg {
			out = append(out, r)
		}
	}
	return out
}

func listOrDash(v []string) string {
	if len(v) == 0 {
		return "—"
	}
	return strings.Join(v, ", ")
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
