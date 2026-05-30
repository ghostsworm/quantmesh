package mcp

import (
	"context"
	"encoding/json"
)

// RegisterBacktestTools 回测任务列表/详情查询。
func RegisterBacktestTools(s *Server, p Providers) {
	if p.BacktestTasks == nil {
		return
	}

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_list_backtests",
			Description: "列出最近的回测任务。limit 默认 20，最大 200。",
			InputSchema: schemaObject(map[string]any{
				"limit":  schemaInt("条数（默认 20，最大 200）", 1, 200),
				"offset": schemaInt("偏移", 0, 100000),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Limit <= 0 || q.Limit > 200 {
				q.Limit = 20
			}
			rows, err := p.BacktestTasks.ListBacktestTasks(q.Limit, q.Offset)
			if err != nil {
				return nil, err
			}
			return map[string]any{"count": len(rows), "tasks": rows}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_get_backtest",
			Description: "按 id 获取单个回测任务的详情（参数、状态、结果摘要）。",
			InputSchema: schemaObject(map[string]any{
				"id": schemaString("回测任务 ID"),
			}, "id"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			task, err := p.BacktestTasks.GetBacktestTask(q.ID)
			if err != nil {
				return nil, err
			}
			if task == nil {
				return "未找到该回测任务。", nil
			}
			return task, nil
		},
	})
}
