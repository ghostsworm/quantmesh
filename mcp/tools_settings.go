package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// 一些不应该通过 MCP 读到的敏感 key（API Key、密钥前缀等）。
// 命中 → 工具返回脱敏值。
var sensitiveSettingPrefixes = []string{
	// aipipe 集成已移除，但老部署的 system_settings 表里仍残留 aipipe_api_key 行。
	// 这条必须保留：移出本列表会让 MCP 明文返回那个残留的 API Key。
	"aipipe_api_key",
	"mcp_token",
	"secret",
	"password",
	"webauthn",
	"webhook",
}

func isSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, p := range sensitiveSettingPrefixes {
		if strings.Contains(lk, p) {
			return true
		}
	}
	return false
}

func maskSettingValue(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + "..." + v[len(v)-4:]
}

// RegisterSettingsTools 全局设置只读访问。
func RegisterSettingsTools(s *Server, p Providers) {
	if p.SystemSettings == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name: "qm_list_settings",
			Description: "列出 system_settings 表中的全部配置项。" +
				"已知敏感字段（含 api_key/secret/password/token 等）的 value 会脱敏。",
			InputSchema: emptyObjectSchema(),
		},
		Handler: func(ctx context.Context, _ json.RawMessage) (any, error) {
			rows, err := p.SystemSettings.GetSystemSettings(ctx, nil)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(rows))
			for _, r := range rows {
				val := r.Value
				if isSensitiveKey(r.Key) {
					val = maskSettingValue(val)
				}
				out = append(out, map[string]any{
					"key":        r.Key,
					"value":      val,
					"type":       r.Type,
					"updated_at": r.UpdatedAt,
				})
			}
			return map[string]any{"count": len(out), "settings": out}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_get_setting",
			Description: "按 key 获取单个设置。命中敏感字段时 value 会脱敏。",
			InputSchema: schemaObject(map[string]any{
				"key": schemaString("设置 key"),
			}, "key"),
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			if q.Key == "" {
				return nil, errors.New("key 不能为空")
			}
			r, err := p.SystemSettings.GetSystemSetting(ctx, q.Key)
			if err != nil {
				return nil, err
			}
			if r == nil {
				return "未找到该设置。", nil
			}
			val := r.Value
			if isSensitiveKey(r.Key) {
				val = maskSettingValue(val)
			}
			return map[string]any{
				"key":        r.Key,
				"value":      val,
				"type":       r.Type,
				"updated_at": r.UpdatedAt,
			}, nil
		},
	})
}
