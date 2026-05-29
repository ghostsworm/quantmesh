package mcp

import (
	"context"
	"encoding/json"
)

// RegisterOrderTools 订单查询（最近 N 条）。
func RegisterOrderTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_list_orders",
			Description: "查询最近订单。支持按状态/交易所/币种过滤。limit 默认 50，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"status":   schemaString("订单状态过滤（可选，如 FILLED/NEW/CANCELED）"),
				"exchange": schemaString("交易所代码（可选）"),
				"symbol":   schemaString("交易对（可选）"),
				"limit":    schemaInt("条数（默认 50，最大 500）", 1, 500),
				"offset":   schemaInt("偏移", 0, 100000),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Status   string `json:"status"`
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				Limit    int    `json:"limit"`
				Offset   int    `json:"offset"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 50
			}
			rows, err := p.Storage.QueryOrdersWithFilter(q.Limit, q.Offset, q.Status, q.Exchange, q.Symbol, nil, nil)
			if err != nil {
				return nil, err
			}
			cnt, err := p.Storage.CountOrdersWithFilter(q.Status, q.Exchange, q.Symbol, nil, nil)
			if err != nil {
				cnt = int64(len(rows))
			}
			return map[string]any{
				"total":    cnt,
				"returned": len(rows),
				"orders":   rows,
			}, nil
		},
	})
}
