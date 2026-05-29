package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// RegisterFundingTools 资金费率/资金费查询。
func RegisterFundingTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_funding_rate_latest",
			Description: "获取指定交易所 + 交易对最新一条资金费率。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码（例 binance）"),
				"symbol":   schemaString("交易对（例 BTCUSDT）"),
			}, "exchange", "symbol"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if q.Exchange == "" || q.Symbol == "" {
				return nil, errors.New("exchange 和 symbol 都不能为空")
			}
			rate, err := p.Storage.GetLatestFundingRate(q.Symbol, q.Exchange)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"exchange": q.Exchange,
				"symbol":   q.Symbol,
				"rate":     rate,
				"as_of":    time.Now().Format(time.RFC3339),
			}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_funding_payments_sum",
			Description: "汇总指定账户/交易所最近 N 天的资金费收益（正为收入，负为支出）。",
			InputSchema: schemaObject(map[string]any{
				"account":  schemaString("账户 ID"),
				"exchange": schemaString("交易所代码"),
				"days":     schemaInt("回看天数（1-90，默认 7）", 1, 90),
			}, "account", "exchange"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Account  string `json:"account"`
				Exchange string `json:"exchange"`
				Days     int    `json:"days"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			end := time.Now()
			start := end.AddDate(0, 0, -q.Days)
			sum, err := p.Storage.GetFundingPaymentsSum(q.Account, q.Exchange, start, end)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"account":  q.Account,
				"exchange": q.Exchange,
				"days":     q.Days,
				"sum":      sum,
			}, nil
		},
	})
}
