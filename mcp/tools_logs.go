package mcp

import (
	"context"
	"encoding/json"
	"time"

	"quantmesh/storage"
)

// RegisterLogTools 日志查询。
func RegisterLogTools(s *Server, p Providers) {
	if p.LogStorage == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name: "qm_tail_logs",
			Description: "查询最近的运行日志，可按 level/keyword/bot_id 过滤。" +
				"返回时间倒序的日志条目。limit 默认 50，最大 500。",
			InputSchema: schemaObject(map[string]any{
				"level":     schemaEnum("日志级别过滤（可空）", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", ""),
				"keyword":   schemaString("正文模糊匹配（可空）"),
				"bot_id":    schemaString("Bot ID 精确匹配（可空）"),
				"minutes":   schemaInt("最近 N 分钟（默认 60，最大 1440）", 1, 1440),
				"limit":     schemaInt("返回条数（默认 50，最大 500）", 1, 500),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Level   string `json:"level"`
				Keyword string `json:"keyword"`
				BotID   string `json:"bot_id"`
				Minutes int    `json:"minutes"`
				Limit   int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Minutes <= 0 || q.Minutes > 1440 {
				q.Minutes = 60
			}
			if q.Limit <= 0 || q.Limit > 500 {
				q.Limit = 50
			}
			end := time.Now()
			start := end.Add(-time.Duration(q.Minutes) * time.Minute)
			rows, total, err := p.LogStorage.GetLogs(storage.LogQueryParams{
				StartTime: start,
				EndTime:   end,
				Level:     q.Level,
				Keyword:   q.Keyword,
				BotID:     q.BotID,
				Limit:     q.Limit,
				Offset:    0,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"range_start":  start.Format(time.RFC3339),
				"range_end":    end.Format(time.RFC3339),
				"total":        total,
				"returned":     len(rows),
				"logs":         rows,
			}, nil
		},
	})
}
