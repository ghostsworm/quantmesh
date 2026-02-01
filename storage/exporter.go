package storage

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// ExportFormat 导出格式
type ExportFormat string

const (
	ExportFormatCSV  ExportFormat = "csv"
	ExportFormatJSON ExportFormat = "json"
)

// ExportParams 导出参数
type ExportParams struct {
	Format     ExportFormat
	StartTime  time.Time
	EndTime    time.Time
	Exchange   string
	Symbol     string
	Account    string
	Limit      int
	Offset     int
	AuditDir   string // 审计日志目录
	ConfigPath string
}

// Exporter 数据导出器
type Exporter struct {
	storage Storage
}

// NewExporter 创建导出器
func NewExporter(s Storage) *Exporter {
	return &Exporter{storage: s}
}

// ExportTrades 导出交易历史
func (e *Exporter) ExportTrades(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	startTime, endTime := params.StartTime, params.EndTime
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -90) // 默认90天
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}
	limit := 10000
	if params.Limit > 0 && params.Limit < limit {
		limit = params.Limit
	}

	trades, err := e.storage.QueryTrades(startTime, endTime, limit, params.Offset)
	if err != nil {
		return nil, "", err
	}

	// 内存过滤
	trades = filterTrades(trades, params.Exchange, params.Symbol, params.Account)

	return e.encodeData(trades, params.Format, "trades")
}

// ExportOrders 导出订单历史
func (e *Exporter) ExportOrders(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	limit := 10000
	if params.Limit > 0 && params.Limit < limit {
		limit = params.Limit
	}

	orders, err := e.storage.QueryOrders(limit, params.Offset, "")
	if err != nil {
		return nil, "", err
	}

	return e.encodeData(orders, params.Format, "orders")
}

// ExportPositions 导出持仓历史
func (e *Exporter) ExportPositions(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	limit := 10000
	if params.Limit > 0 && params.Limit < limit {
		limit = params.Limit
	}

	positions, err := e.storage.QueryPositions(limit, params.Offset)
	if err != nil {
		return nil, "", err
	}

	// 按 symbol 过滤
	if params.Symbol != "" {
		var filtered []*Position
		for _, p := range positions {
			if p.Symbol == params.Symbol {
				filtered = append(filtered, p)
			}
		}
		positions = filtered
	}

	return e.encodeData(positions, params.Format, "positions")
}

// ExportStatistics 导出统计数据（每日统计）
func (e *Exporter) ExportStatistics(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	startTime, endTime := params.StartTime, params.EndTime
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -365)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	stats, err := e.storage.QueryDailyStatisticsByExchange(params.Exchange, params.Account, startTime, endTime)
	if err != nil {
		return nil, "", err
	}

	return e.encodeData(stats, params.Format, "statistics")
}

// ExportReconciliation 导出对账历史
func (e *Exporter) ExportReconciliation(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	startTime, endTime := params.StartTime, params.EndTime
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -90)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}
	limit := 10000
	if params.Limit > 0 && params.Limit < limit {
		limit = params.Limit
	}

	histories, err := e.storage.QueryReconciliationHistory(params.Exchange, params.Symbol, params.Account, startTime, endTime, limit, params.Offset)
	if err != nil {
		return nil, "", err
	}

	return e.encodeData(histories, params.Format, "reconciliation")
}

// ExportRiskChecks 导出风控检查历史（展平为行）
func (e *Exporter) ExportRiskChecks(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	startTime, endTime := params.StartTime, params.EndTime
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	histories, err := e.storage.QueryRiskCheckHistory(startTime, endTime, 500)
	if err != nil {
		return nil, "", err
	}

	// 展平：每个 CheckTime + Symbol 为一行
	var exportRows []RiskCheckExportRow
	for _, h := range histories {
		for _, s := range h.Symbols {
			exportRows = append(exportRows, RiskCheckExportRow{
				CheckTime:      h.CheckTime,
				Symbol:         s.Symbol,
				IsHealthy:      s.IsHealthy,
				PriceDeviation: s.PriceDeviation,
				VolumeRatio:    s.VolumeRatio,
				Reason:         s.Reason,
			})
		}
	}

	return e.encodeData(exportRows, params.Format, "risk_checks")
}

// RiskCheckExportRow 风控检查导出行
type RiskCheckExportRow struct {
	CheckTime      time.Time `json:"check_time"`
	Symbol         string    `json:"symbol"`
	IsHealthy      bool      `json:"is_healthy"`
	PriceDeviation float64   `json:"price_deviation"`
	VolumeRatio    float64   `json:"volume_ratio"`
	Reason         string    `json:"reason"`
}

// ExportSystemMetrics 导出系统监控数据
func (e *Exporter) ExportSystemMetrics(params ExportParams) ([]byte, string, error) {
	if e.storage == nil {
		return nil, "", fmt.Errorf("存储未初始化")
	}
	startTime, endTime := params.StartTime, params.EndTime
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -7)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	metrics, err := e.storage.QuerySystemMetrics(startTime, endTime)
	if err != nil {
		return nil, "", err
	}

	return e.encodeData(metrics, params.Format, "system_metrics")
}

// encodeData 将数据编码为指定格式
func (e *Exporter) encodeData(data interface{}, format ExportFormat, name string) ([]byte, string, error) {
	switch format {
	case ExportFormatCSV:
		return e.toCSV(data, name)
	case ExportFormatJSON:
		return e.toJSON(data, name)
	default:
		return e.toJSON(data, name)
	}
}

func (e *Exporter) toJSON(data interface{}, name string) ([]byte, string, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return b, "application/json", nil
}

func (e *Exporter) toCSV(data interface{}, name string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// 根据数据类型生成 CSV
	switch v := data.(type) {
	case []*Trade:
		w.Write([]string{"buy_order_id", "sell_order_id", "exchange", "account", "symbol", "buy_price", "sell_price", "quantity", "pnl", "created_at"})
		for _, t := range v {
			w.Write([]string{
				fmt.Sprintf("%d", t.BuyOrderID),
				fmt.Sprintf("%d", t.SellOrderID),
				t.Exchange,
				t.Account,
				t.Symbol,
				fmt.Sprintf("%.8f", t.BuyPrice),
				fmt.Sprintf("%.8f", t.SellPrice),
				fmt.Sprintf("%.8f", t.Quantity),
				fmt.Sprintf("%.8f", t.PnL),
				utils.ToUTC(t.CreatedAt).Format(time.RFC3339),
			})
		}
	case []*Order:
		w.Write([]string{"order_id", "client_order_id", "symbol", "side", "price", "quantity", "status", "created_at", "updated_at"})
		for _, o := range v {
			w.Write([]string{
				fmt.Sprintf("%d", o.OrderID),
				o.ClientOrderID,
				o.Symbol,
				o.Side,
				fmt.Sprintf("%.8f", o.Price),
				fmt.Sprintf("%.8f", o.Quantity),
				o.Status,
				utils.ToUTC(o.CreatedAt).Format(time.RFC3339),
				utils.ToUTC(o.UpdatedAt).Format(time.RFC3339),
			})
		}
	case []*Position:
		w.Write([]string{"slot_price", "symbol", "size", "entry_price", "current_price", "pnl", "opened_at", "closed_at"})
		for _, p := range v {
			closedAt := ""
			if p.ClosedAt != nil {
				closedAt = utils.ToUTC(*p.ClosedAt).Format(time.RFC3339)
			}
			w.Write([]string{
				fmt.Sprintf("%.8f", p.SlotPrice),
				p.Symbol,
				fmt.Sprintf("%.8f", p.Size),
				fmt.Sprintf("%.8f", p.EntryPrice),
				fmt.Sprintf("%.8f", p.CurrentPrice),
				fmt.Sprintf("%.8f", p.PnL),
				utils.ToUTC(p.OpenedAt).Format(time.RFC3339),
				closedAt,
			})
		}
	case []*DailyStatisticsWithTradeCount:
		w.Write([]string{"date", "total_trades", "total_volume", "total_pnl", "win_rate", "winning_trades", "losing_trades"})
		for _, s := range v {
			w.Write([]string{
				s.Date.Format("2006-01-02"),
				fmt.Sprintf("%d", s.TotalTrades),
				fmt.Sprintf("%.8f", s.TotalVolume),
				fmt.Sprintf("%.8f", s.TotalPnL),
				fmt.Sprintf("%.4f", s.WinRate),
				fmt.Sprintf("%d", s.WinningTrades),
				fmt.Sprintf("%d", s.LosingTrades),
			})
		}
	case []*ReconciliationHistory:
		w.Write([]string{"exchange", "symbol", "account", "reconcile_time", "local_position", "exchange_position", "position_diff", "active_buy_orders", "active_sell_orders", "estimated_profit", "actual_profit"})
		for _, h := range v {
			w.Write([]string{
				h.Exchange,
				h.Symbol,
				h.Account,
				utils.ToUTC(h.ReconcileTime).Format(time.RFC3339),
				fmt.Sprintf("%.8f", h.LocalPosition),
				fmt.Sprintf("%.8f", h.ExchangePosition),
				fmt.Sprintf("%.8f", h.PositionDiff),
				fmt.Sprintf("%d", h.ActiveBuyOrders),
				fmt.Sprintf("%d", h.ActiveSellOrders),
				fmt.Sprintf("%.8f", h.EstimatedProfit),
				fmt.Sprintf("%.8f", h.ActualProfit),
			})
		}
	case []*SystemMetrics:
		w.Write([]string{"timestamp", "cpu_percent", "memory_mb", "memory_percent", "process_id"})
		for _, m := range v {
			w.Write([]string{
				utils.ToUTC(m.Timestamp).Format(time.RFC3339),
				fmt.Sprintf("%.2f", m.CPUPercent),
				fmt.Sprintf("%.2f", m.MemoryMB),
				fmt.Sprintf("%.2f", m.MemoryPercent),
				fmt.Sprintf("%d", m.ProcessID),
			})
		}
	case []RiskCheckExportRow:
		w.Write([]string{"check_time", "symbol", "is_healthy", "price_deviation", "volume_ratio", "reason"})
		for _, r := range v {
			healthy := "false"
			if r.IsHealthy {
				healthy = "true"
			}
			w.Write([]string{
				utils.ToUTC(r.CheckTime).Format(time.RFC3339),
				r.Symbol,
				healthy,
				fmt.Sprintf("%.4f", r.PriceDeviation),
				fmt.Sprintf("%.4f", r.VolumeRatio),
				r.Reason,
			})
		}
	default:
		return e.toJSON(data, name)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "text/csv", nil
}

func filterTrades(trades []*Trade, exchange, symbol, account string) []*Trade {
	if exchange == "" && symbol == "" && account == "" {
		return trades
	}
	var out []*Trade
	for _, t := range trades {
		if exchange != "" && t.Exchange != exchange {
			continue
		}
		if symbol != "" && t.Symbol != symbol {
			continue
		}
		if account != "" && t.Account != account {
			continue
		}
		out = append(out, t)
	}
	return out
}

// CreateExportZip 创建全量导出 ZIP
// logRecords 可为 []*LogRecord 或 []*web.LogRecordResponse 等可 JSON 序列化的类型
func (e *Exporter) CreateExportZip(params ExportParams, logRecords interface{}, configContent []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	ts := time.Now().Format("20060102_150405")

	// 1. 配置
	if len(configContent) > 0 {
		f, _ := zw.Create(fmt.Sprintf("export_%s/config.yaml", ts))
		f.Write(configContent)
	}

	// 2. 交易
	if e.storage != nil {
		if data, _, err := e.ExportTrades(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/trades.json", ts))
			f.Write(data)
		} else if err != nil {
			logger.Warn("导出交易失败: %v", err)
		}
	}

	// 3. 订单
	if e.storage != nil {
		if data, _, err := e.ExportOrders(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/orders.json", ts))
			f.Write(data)
		}
	}

	// 4. 持仓
	if e.storage != nil {
		if data, _, err := e.ExportPositions(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/positions.json", ts))
			f.Write(data)
		}
	}

	// 5. 统计
	if e.storage != nil {
		if data, _, err := e.ExportStatistics(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/statistics.json", ts))
			f.Write(data)
		}
	}

	// 6. 对账历史
	if e.storage != nil {
		if data, _, err := e.ExportReconciliation(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/reconciliation.json", ts))
			f.Write(data)
		}
	}

	// 7. 风控检查
	if e.storage != nil {
		if data, _, err := e.ExportRiskChecks(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/risk_checks.json", ts))
			f.Write(data)
		}
	}

	// 8. 系统监控
	if e.storage != nil {
		if data, _, err := e.ExportSystemMetrics(params); err == nil && len(data) > 0 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/system_metrics.json", ts))
			f.Write(data)
		}
	}

	// 9. 应用日志
	if logRecords != nil {
		if logData, err := json.MarshalIndent(logRecords, "", "  "); err == nil && len(logData) > 2 {
			f, _ := zw.Create(fmt.Sprintf("export_%s/logs.json", ts))
			f.Write(logData)
		}
	}

	// 10. 审计日志（从目录读取）
	if params.AuditDir != "" {
		e.addAuditLogsToZip(zw, params.AuditDir, params.StartTime, params.EndTime, ts)
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (e *Exporter) addAuditLogsToZip(zw *zip.Writer, auditDir string, startTime, endTime time.Time, ts string) {
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		logger.Warn("读取审计日志目录失败: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if (len(name) > 4 && name[len(name)-4:] != ".csv" && name[len(name)-6:] != ".jsonl") || len(name) < 15 {
			continue
		}
		// audit_trades_2025-01-15.csv
		if len(name) >= 14 && name[:13] == "audit_trades_" {
			dateStr := name[13:]
			if len(dateStr) >= 10 {
				dateStr = dateStr[:10]
				if t, err := time.Parse("2006-01-02", dateStr); err == nil {
					if !startTime.IsZero() && t.Before(startTime) {
						continue
					}
					if !endTime.IsZero() && t.After(endTime) {
						continue
					}
				}
			}
		}

		fpath := filepath.Join(auditDir, name)
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		f, err := zw.Create(fmt.Sprintf("export_%s/audit/%s", ts, name))
		if err != nil {
			continue
		}
		f.Write(data)
	}
}
