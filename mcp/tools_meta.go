package mcp

import (
	"context"
	"encoding/json"
	"runtime"
	"time"
)

// RegisterMetaTools 注册"关于服务自身"的工具。不依赖任何 provider，
// 用来在 agent 端做最基本的连通性 + 自我描述。
func RegisterMetaTools(s *Server, p Providers) {
	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_server_info",
			Description: "返回 QuantMesh 服务自身信息（版本、Go runtime、时间），用于连通性确认。",
			InputSchema: emptyObjectSchema(),
		},
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"name":       ServerName,
				"version":    p.Version,
				"go_version": runtime.Version(),
				"goroutines": runtime.NumGoroutine(),
				"server_time": time.Now().Format(time.RFC3339),
				"timezone":   time.Now().Location().String(),
			}, nil
		},
	})
}
