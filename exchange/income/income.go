package income

import "time"

// Income 收入/支出記錄（資金費等）
type Income struct {
	Symbol        string  // 交易對
	IncomeType    string  // FUNDING_FEE 等
	Income        float64 // 正=收入，負=支出
	Asset         string
	Info          string
	TransactionID int64
	TradeTime     time.Time
}
