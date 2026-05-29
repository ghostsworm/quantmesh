package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

// RegisterWriteTools 注册有副作用的工具。
// 仅当用户在全局设置里开启 mcp_allow_write=true 时才调用此函数。
//
// 防呆策略：每个写工具都要求 confirm=true，agent 一时手抖不会得逞。
func RegisterWriteTools(s *Server, p Providers) {
	if p.BotControl != nil {
		registerBotWriteTools(s, p)
	}
	if p.SystemSettings != nil {
		registerSettingWriteTools(s, p)
	}
}

func registerBotWriteTools(s *Server, p Providers) {
	s.Register(ToolEntry{
		Tool: Tool{
			Name: "qm_bot_set_enabled",
			Description: "启用或停用某个 Bot。⚠️ 写操作：必须显式传 confirm=true。" +
				"reason 会写入审计字段。",
			InputSchema: schemaObject(map[string]any{
				"bot_id":  schemaString("Bot ID（exchange:symbol:market_type）"),
				"enabled": schemaBool("true=启用 false=停用"),
				"reason":  schemaString("操作原因（可选，建议填写）"),
				"confirm": schemaBool("必须为 true，否则拒绝执行"),
			}, "bot_id", "enabled", "confirm"),
		},
		Write: true,
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				BotID   string `json:"bot_id"`
				Enabled bool   `json:"enabled"`
				Reason  string `json:"reason"`
				Confirm bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if !q.Confirm {
				return nil, errors.New("写操作需要 confirm=true")
			}
			if q.BotID == "" {
				return nil, errors.New("bot_id 不能为空")
			}
			if q.Reason == "" {
				q.Reason = "via MCP"
			}
			var err error
			if q.Enabled {
				err = p.BotControl.EnableBot(q.BotID, q.Reason)
			} else {
				err = p.BotControl.DisableBot(q.BotID, q.Reason)
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"bot_id":  q.BotID,
				"enabled": q.Enabled,
				"reason":  q.Reason,
				"ok":      true,
			}, nil
		},
	})
}

func registerSettingWriteTools(s *Server, p Providers) {
	s.Register(ToolEntry{
		Tool: Tool{
			Name: "qm_set_setting",
			Description: "修改 system_settings 中的某个 key。⚠️ 写操作：必须 confirm=true。" +
				"禁止修改 mcp_token/aipipe_api_key 等敏感字段。",
			InputSchema: schemaObject(map[string]any{
				"key":     schemaString("设置 key"),
				"value":   schemaString("新值（字符串）"),
				"confirm": schemaBool("必须为 true"),
			}, "key", "value", "confirm"),
		},
		Write: true,
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Key     string `json:"key"`
				Value   string `json:"value"`
				Confirm bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if !q.Confirm {
				return nil, errors.New("写操作需要 confirm=true")
			}
			if q.Key == "" {
				return nil, errors.New("key 不能为空")
			}
			if isSensitiveKey(q.Key) {
				return nil, errors.New("禁止通过 MCP 修改敏感字段: " + q.Key)
			}
			if err := p.SystemSettings.SetSystemSettingString(ctx, q.Key, q.Value); err != nil {
				return nil, err
			}
			return map[string]any{"key": q.Key, "ok": true}, nil
		},
	})
}
