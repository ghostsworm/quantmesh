package mcp

import (
	"context"
	"encoding/json"
	"time"
)

type exchangePnLOrderStatsReader interface {
	GetExchangePnLOrderStats(exchange, symbol string) (withPnLCount, missingPnLCount int, totalPnL float64, err error)
}

// RegisterReconciliationTools 注册对账/盈亏完整性诊断工具，给外部 agent 做数据核对用。
func RegisterReconciliationTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_reconciliation_latest",
			Description: "获取指定交易所/交易对/账户的最新对账记录，用于核对本地持仓与交易所持仓差异。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码（可选）"),
				"symbol":   schemaString("交易对（必填，例 BTCUSDT）"),
				"account":  schemaString("账户 ID（可选）"),
			}, "symbol"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				Account  string `json:"account"`
			}
			_ = json.Unmarshal(args, &q)
			row, err := p.Storage.GetLatestReconciliationHistory(q.Exchange, q.Symbol, q.Account)
			if err != nil {
				return nil, err
			}
			if row == nil {
				return "暂无匹配的对账记录。", nil
			}
			return row, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_reconciliation_history",
			Description: "查询最近对账历史。days 默认 7，limit 默认 50，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码（可选）"),
				"symbol":   schemaString("交易对（必填，例 BTCUSDT）"),
				"account":  schemaString("账户 ID（可选）"),
				"days":     schemaInt("回看天数（1-90，默认 7）", 1, 90),
				"limit":    schemaInt("返回条数（默认 50，最大 500）", 1, 500),
				"offset":   schemaInt("偏移量", 0, 100000),
			}, "symbol"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				Account  string `json:"account"`
				Days     int    `json:"days"`
				Limit    int    `json:"limit"`
				Offset   int    `json:"offset"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 50
			}
			end := time.Now()
			start := end.AddDate(0, 0, -q.Days)
			rows, err := p.Storage.QueryReconciliationHistory(q.Exchange, q.Symbol, q.Account, start, end, q.Limit, q.Offset)
			if err != nil {
				return nil, err
			}
			total, err := p.Storage.GetReconciliationCount(q.Exchange, q.Symbol, q.Account)
			if err != nil {
				total = int64(len(rows))
			}
			return map[string]any{
				"start":    start.Format(time.RFC3339),
				"end":      end.Format(time.RFC3339),
				"total":    total,
				"returned": len(rows),
				"rows":     rows,
			}, nil
		},
	})

	if stats, ok := p.Storage.(exchangePnLOrderStatsReader); ok {
		s.Register(ToolEntry{
			Tool: Tool{
				Name:        "qm_order_pnl_audit",
				Description: "统计 FILLED 订单 realized_pnl 完整性，帮助定位交易所已实现盈亏漏记或核对差异。",
				InputSchema: schemaObject(map[string]any{
					"exchange": schemaString("交易所代码（可选）"),
					"symbol":   schemaString("交易对（可选）"),
				}),
			},
			Handler: func(_ context.Context, args json.RawMessage) (any, error) {
				var q struct {
					Exchange string `json:"exchange"`
					Symbol   string `json:"symbol"`
				}
				_ = json.Unmarshal(args, &q)
				withPnL, missingPnL, totalPnL, err := stats.GetExchangePnLOrderStats(q.Exchange, q.Symbol)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"exchange":           q.Exchange,
					"symbol":             q.Symbol,
					"with_pnl_count":     withPnL,
					"missing_pnl_count":  missingPnL,
					"total_realized_pnl": totalPnL,
					"ok":                 missingPnL == 0,
				}, nil
			},
		})
	}
}
