package storage

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

const (
	AuditEventOrder = "order"
	AuditEventTrade = "trade"
)

// AuditTradeRecord 合规审计記錄（订單或成交）
type AuditTradeRecord struct {
	EventType     string    `json:"event_type"`      // order | trade
	Timestamp     time.Time `json:"timestamp"`
	Exchange      string    `json:"exchange"`
	Symbol        string    `json:"symbol"`
	OrderID       int64     `json:"order_id,omitempty"`
	ClientOrderID string   `json:"client_order_id,omitempty"`
	Side          string   `json:"side,omitempty"`   // BUY, SELL, TRADE
	Type          string   `json:"type,omitempty"`   // LIMIT, MARKET
	Price         float64  `json:"price,omitempty"`
	Quantity      float64  `json:"quantity"`
	FilledQty     float64  `json:"filled_qty,omitempty"`
	Status        string   `json:"status,omitempty"`
	Fee           float64  `json:"fee,omitempty"`
	FeeCoin       string   `json:"fee_coin,omitempty"`
	BuyOrderID    int64    `json:"buy_order_id,omitempty"`
	SellOrderID   int64    `json:"sell_order_id,omitempty"`
	BuyPrice      float64  `json:"buy_price,omitempty"`
	SellPrice     float64  `json:"sell_price,omitempty"`
	PnL           float64  `json:"pnl,omitempty"`
	Account       string   `json:"account,omitempty"`
}

// AuditTradeLogger 交易审计日志記錄器，按天分文件写入 CSV/JSONL
type AuditTradeLogger struct {
	dir       string
	format   string // csv, jsonl, both
	mu       sync.Mutex
	csvFiles map[string]*os.File
	jsonFiles map[string]*os.File
	csvWriters map[string]*csv.Writer
	closed   bool
}

// NewAuditTradeLogger 創建审计日志記錄器
// format: csv, jsonl, both
func NewAuditTradeLogger(dir, format string) (*AuditTradeLogger, error) {
	if dir == "" {
		dir = "./data/audit"
	}
	if format == "" {
		format = "both"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("創建审计日志目錄失败: %w", err)
	}
	return &AuditTradeLogger{
		dir:        dir,
		format:     format,
		csvFiles:   make(map[string]*os.File),
		jsonFiles:  make(map[string]*os.File),
		csvWriters: make(map[string]*csv.Writer),
	}, nil
}

func (a *AuditTradeLogger) dateKey(t time.Time) string {
	return utils.ToUTC(t).Format("2006-01-02")
}

func (a *AuditTradeLogger) getOrCreateCSV(dateKey string) (*csv.Writer, error) {
	if w, ok := a.csvWriters[dateKey]; ok {
		return w, nil
	}
	fpath := filepath.Join(a.dir, fmt.Sprintf("audit_trades_%s.csv", dateKey))
	exists := false
	if _, err := os.Stat(fpath); err == nil {
		exists = true
	}
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	a.csvFiles[dateKey] = f
	w := csv.NewWriter(f)
	if !exists {
		header := []string{
			"event_type", "timestamp", "exchange", "symbol", "order_id", "client_order_id",
			"side", "type", "price", "quantity", "filled_qty", "status", "fee", "fee_coin",
			"buy_order_id", "sell_order_id", "buy_price", "sell_price", "pnl", "account",
		}
		if err := w.Write(header); err != nil {
			f.Close()
			delete(a.csvFiles, dateKey)
			return nil, err
		}
		w.Flush()
	}
	a.csvWriters[dateKey] = w
	return w, nil
}

func (a *AuditTradeLogger) getOrCreateJSONL(dateKey string) (*os.File, error) {
	if f, ok := a.jsonFiles[dateKey]; ok {
		return f, nil
	}
	fpath := filepath.Join(a.dir, fmt.Sprintf("audit_trades_%s.jsonl", dateKey))
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	a.jsonFiles[dateKey] = f
	return f, nil
}

func (a *AuditTradeLogger) writeCSV(w *csv.Writer, r *AuditTradeRecord) error {
	ts := utils.ToUTC(r.Timestamp).Format(time.RFC3339)
	row := []string{
		r.EventType, ts, r.Exchange, r.Symbol,
		fmt.Sprintf("%d", r.OrderID), r.ClientOrderID, r.Side, r.Type,
		fmt.Sprintf("%.8f", r.Price), fmt.Sprintf("%.8f", r.Quantity), fmt.Sprintf("%.8f", r.FilledQty),
		r.Status, fmt.Sprintf("%.8f", r.Fee), r.FeeCoin,
		fmt.Sprintf("%d", r.BuyOrderID), fmt.Sprintf("%d", r.SellOrderID),
		fmt.Sprintf("%.8f", r.BuyPrice), fmt.Sprintf("%.8f", r.SellPrice),
		fmt.Sprintf("%.8f", r.PnL), r.Account,
	}
	return w.Write(row)
}

func (a *AuditTradeLogger) writeJSONL(f *os.File, r *AuditTradeRecord) error {
	enc := json.NewEncoder(f)
	return enc.Encode(r)
}

// Log 写入一条审计記錄
func (a *AuditTradeLogger) Log(r *AuditTradeRecord) error {
	if a == nil || a.closed {
		return nil
	}
	if r.Timestamp.IsZero() {
		r.Timestamp = utils.NowUTC()
	}
	dateKey := a.dateKey(r.Timestamp)

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.format == "csv" || a.format == "both" {
		w, err := a.getOrCreateCSV(dateKey)
		if err != nil {
			logger.Warn("审计日志 CSV 写入失败: %v", err)
		} else {
			if err := a.writeCSV(w, r); err != nil {
				logger.Warn("审计日志 CSV 写入失败: %v", err)
			} else {
				w.Flush()
			}
		}
	}
	if a.format == "jsonl" || a.format == "both" {
		f, err := a.getOrCreateJSONL(dateKey)
		if err != nil {
			logger.Warn("审计日志 JSONL 写入失败: %v", err)
		} else {
			if err := a.writeJSONL(f, r); err != nil {
				logger.Warn("审计日志 JSONL 写入失败: %v", err)
			}
		}
	}
	return nil
}

// LogOrder 記錄订單事件
func (a *AuditTradeLogger) LogOrder(exchange, symbol, account string, order *Order) {
	if a == nil || a.closed || order == nil {
		return
	}
	filledQty := 0.0
	if order.Status == "FILLED" || order.Status == "PARTIALLY_FILLED" {
		filledQty = order.Quantity
	}
	r := &AuditTradeRecord{
		EventType:     AuditEventOrder,
		Timestamp:     order.CreatedAt,
		Exchange:      exchange,
		Symbol:        symbol,
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Side:          order.Side,
		Type:          "LIMIT",
		Price:         order.Price,
		Quantity:      order.Quantity,
		FilledQty:     filledQty,
		Status:        order.Status,
		Account:       account,
	}
	_ = a.Log(r)
}

// LogTrade 記錄成交事件
func (a *AuditTradeLogger) LogTrade(trade *Trade) {
	if a == nil || a.closed || trade == nil {
		return
	}
	r := &AuditTradeRecord{
		EventType:   AuditEventTrade,
		Timestamp:   trade.CreatedAt,
		Exchange:    trade.Exchange,
		Symbol:      trade.Symbol,
		BuyOrderID:  trade.BuyOrderID,
		SellOrderID: trade.SellOrderID,
		BuyPrice:    trade.BuyPrice,
		SellPrice:   trade.SellPrice,
		Quantity:    trade.Quantity,
		PnL:         trade.PnL,
		Account:     trade.Account,
	}
	_ = a.Log(r)
}

// globalAuditLogger 全局审计日志記錄器（由 main 在啟用合规時設置）
var globalAuditLogger *AuditTradeLogger

// SetGlobalAuditLogger 設置全局审计日志記錄器
func SetGlobalAuditLogger(l *AuditTradeLogger) {
	globalAuditLogger = l
}

// GetGlobalAuditLogger 獲取全局审计日志記錄器
func GetGlobalAuditLogger() *AuditTradeLogger {
	return globalAuditLogger
}

// Close 关闭所有按天打开的文件句柄
func (a *AuditTradeLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	for _, w := range a.csvWriters {
		w.Flush()
	}
	for _, f := range a.csvFiles {
		_ = f.Close()
	}
	for _, f := range a.jsonFiles {
		_ = f.Close()
	}
	a.csvFiles = make(map[string]*os.File)
	a.jsonFiles = make(map[string]*os.File)
	a.csvWriters = make(map[string]*csv.Writer)
	return nil
}
