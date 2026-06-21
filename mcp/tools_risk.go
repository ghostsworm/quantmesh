package mcp

import (
	"context"
	"encoding/json"
	"time"
)

// RegisterRiskTools 注册系统健康、风控事件和权益快照等只读工具。
func RegisterRiskTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_system_metrics_latest",
			Description: "获取最近一条系统指标记录，用于核对服务 CPU/内存/磁盘等运行状态。",
			InputSchema: emptyObjectSchema(),
		},
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			row, err := p.Storage.GetLatestSystemMetrics()
			if err != nil {
				return nil, err
			}
			if row == nil {
				return "暂无系统指标记录。", nil
			}
			return row, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_risk_events",
			Description: "查询 Bot 风控事件历史。limit 默认 50，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"bot_id": schemaString("Bot ID（可选，精确匹配）"),
				"limit":  schemaInt("返回条数（默认 50，最大 500）", 1, 500),
				"offset": schemaInt("偏移量", 0, 100000),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				BotID  string `json:"bot_id"`
				Limit  int    `json:"limit"`
				Offset int    `json:"offset"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 50
			}
			rows, err := p.Storage.QueryBotRiskControlEvents(q.BotID, q.Limit, q.Offset)
			if err != nil {
				return nil, err
			}
			total, err := p.Storage.CountBotRiskControlEvents(q.BotID)
			if err != nil {
				total = int64(len(rows))
			}
			return map[string]any{
				"total":    total,
				"returned": len(rows),
				"events":   rows,
			}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_risk_checks",
			Description: "查询风险检查历史。days 默认 7，limit 默认 100，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"bot_id": schemaString("Bot ID（可选，精确匹配）"),
				"days":   schemaInt("回看天数（1-90，默认 7）", 1, 90),
				"limit":  schemaInt("返回条数（默认 100，最大 500）", 1, 500),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				BotID string `json:"bot_id"`
				Days  int    `json:"days"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 100
			}
			end := time.Now()
			start := end.AddDate(0, 0, -q.Days)
			rows, err := p.Storage.QueryRiskCheckHistory(start, end, q.Limit, q.BotID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"start":    start.Format(time.RFC3339),
				"end":      end.Format(time.RFC3339),
				"returned": len(rows),
				"checks":   rows,
			}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_daily_snapshots",
			Description: "查询每日权益快照，用于核对账户权益、未实现盈亏与日内回撤。days 默认 7，最大 90。",
			InputSchema: schemaObject(map[string]any{
				"exchange": schemaString("交易所代码（可选）"),
				"symbol":   schemaString("交易对（可选）"),
				"account":  schemaString("账户 ID（可选）"),
				"days":     schemaInt("回看天数（1-90，默认 7）", 1, 90),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange string `json:"exchange"`
				Symbol   string `json:"symbol"`
				Account  string `json:"account"`
				Days     int    `json:"days"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			end := time.Now()
			start := end.AddDate(0, 0, -q.Days)
			rows, err := p.Storage.QueryDailySnapshots(q.Exchange, q.Symbol, q.Account, start, end)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"start":     start.Format("2006-01-02"),
				"end":       end.Format("2006-01-02"),
				"returned":  len(rows),
				"snapshots": rows,
			}, nil
		},
	})
}
