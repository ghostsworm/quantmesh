package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

// RegisterPositionTools 持仓查询。
func RegisterPositionTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_list_positions",
			Description: "列出当前活动持仓（含 symbol/size/entry/current/PnL）。limit 默认 50，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"limit":  schemaInt("返回条数（默认 50，最大 500）", 1, 500),
				"offset": schemaInt("偏移量，分页用", 0, 100000),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 50
			}
			rows, err := p.Storage.QueryPositions(q.Limit, q.Offset)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return "暂无持仓记录。", nil
			}
			return map[string]any{"count": len(rows), "positions": rows}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_position_summary",
			Description: "按交易所/币种汇总持仓 PnL（基于最近 24 小时已成交订单的盈亏）。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码，可选；空则不过滤"),
				"symbol":   schemaString("交易对（例 BTCUSDT），可选"),
				"bot_id":   schemaString("Bot ID 精确匹配，可选"),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				BotID    string `json:"bot_id"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Exchange == "" && q.Symbol == "" {
				return nil, errors.New("请至少提供 exchange 或 symbol 之一")
			}
			pnl, err := p.Storage.GetExchangePnLTotal(q.Exchange, q.Symbol, q.BotID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"exchange":     q.Exchange,
				"symbol":       q.Symbol,
				"bot_id":       q.BotID,
				"realized_pnl": pnl,
			}, nil
		},
	})
}
