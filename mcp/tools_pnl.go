package mcp

import (
	"context"
	"encoding/json"
	"time"

	"quantmesh/utils"
)

// RegisterPnLTools PNL / 统计查询。
func RegisterPnLTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_pnl_today",
			Description: "返回指定交易所/币种的当日成交统计（交易笔数、毛利、手续费）。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码（必填，例 binance）"),
				"symbol":   schemaString("交易对（必填，例 BTCUSDT）"),
				"account":  schemaString("账户 ID（可选）"),
				"bot_id":   schemaString("Bot ID（可选，精确匹配）"),
			}, "exchange", "symbol"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				Account  string `json:"account"`
				BotID    string `json:"bot_id"`
			}
			_ = json.Unmarshal(args, &q)
			// GetDailyTradesSummary 按「配置時區」對 created_at 分桶，
			// 這裡若用本機 time.Now()，在伺服器本機時區與配置時區不一致時
			// （例如伺服器跑 UTC、配置寫 Asia/Shanghai）會查錯日期，
			// 每天有數小時的窗口回傳到前一天/後一天的數據。
			date := utils.NowConfiguredTimezone().Format("2006-01-02")
			count, gross, fee, err := p.Storage.GetDailyTradesSummary(q.Exchange, q.Account, date, q.BotID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"date":         date,
				"exchange":     q.Exchange,
				"symbol":       q.Symbol,
				"trades_count": count,
				"gross_pnl":    gross,
				"total_fee":    fee,
				"net_pnl":      gross - fee,
			}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_pnl_range",
			Description: "按时间段查询 PnL，按币种聚合。days 默认 7，最大 90。",
			InputSchema: schemaObject(map[string]any{
				"account": schemaString("账户 ID（可选；空则全部账户）"),
				"days":    schemaInt("回看天数（1-90，默认 7）", 1, 90),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Account string `json:"account"`
				Days    int    `json:"days"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			end := time.Now()
			start := end.AddDate(0, 0, -q.Days)
			rows, err := p.Storage.GetPnLByTimeRange(q.Account, start, end)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"start": start.Format(time.RFC3339),
				"end":   end.Format(time.RFC3339),
				"count": len(rows),
				"rows":  rows,
			}, nil
		},
	})
}
