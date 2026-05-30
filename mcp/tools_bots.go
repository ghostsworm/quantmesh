package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

// RegisterBotTools Bot 状态查询。启停在 tools_write.go。
func RegisterBotTools(s *Server, p Providers) {
	if p.Storage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_list_bots",
			Description: "列出所有 Bot 的启停状态。返回 BotID、Enabled、UpdatedBy、UpdatedAt、Reason。",
			InputSchema: emptyObjectSchema(),
		},
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			rows, err := p.Storage.ListBotStates()
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return "暂无 Bot 状态记录。", nil
			}
			return map[string]any{"count": len(rows), "bots": rows}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_get_bot",
			Description: "获取指定 Bot 状态。bot_id 格式: exchange:symbol:market_type，例 binance:BTCUSDT:perp。",
			InputSchema: schemaObject(map[string]any{
				"bot_id": schemaString("Bot ID，例 binance:BTCUSDT:perp"),
			}, "bot_id"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				BotID string `json:"bot_id"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if q.BotID == "" {
				return nil, errors.New("bot_id 不能为空")
			}
			st, err := p.Storage.GetBotState(q.BotID)
			if err != nil {
				return nil, err
			}
			if st == nil {
				return "未找到该 Bot 状态记录。", nil
			}
			return st, nil
		},
	})
}
