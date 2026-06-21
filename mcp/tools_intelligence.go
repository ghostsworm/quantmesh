package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sort"
	"strings"
	"time"

	"quantmesh/storage"
)

// RegisterIntelligenceTools 注册面向 agent 的导航、诊断和检索工具。
func RegisterIntelligenceTools(s *Server, p Providers) {
	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_capability_map",
			Description: "返回当前 MCP 工具能力地图，按领域归类，并标记只读/写入、必填参数和推荐排查路径。",
			InputSchema: emptyObjectSchema(),
		},
		Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
			return buildCapabilityMap(s, p), nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_tool_help",
			Description: "按工具名返回单个 MCP 工具的说明、输入 schema、必填参数、读写属性和使用建议。",
			InputSchema: schemaObject(map[string]any{
				"name": schemaString("工具名，例如 qm_list_bots"),
			}, "name"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			q.Name = strings.TrimSpace(q.Name)
			if q.Name == "" {
				return nil, errors.New("name 不能为空")
			}
			for _, entry := range sortedToolSnapshots(s.ToolSnapshots()) {
				if entry.Tool.Name == q.Name {
					return map[string]any{
						"name":         entry.Tool.Name,
						"description":  entry.Tool.Description,
						"write":        entry.Write,
						"category":     toolCategory(entry.Tool.Name),
						"required":     schemaRequired(entry.Tool.InputSchema),
						"input_schema": entry.Tool.InputSchema,
						"usage_tip":    toolUsageTip(entry.Tool.Name),
					}, nil
				}
			}
			return nil, errors.New("未找到工具: " + q.Name)
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_health_report",
			Description: "生成面向排障的综合健康报告：服务版本、注册工具、Bot 状态、持仓/订单规模、最近错误日志和系统指标。",
			InputSchema: schemaObject(map[string]any{
				"minutes": schemaInt("回看最近多少分钟的错误/警告日志（默认 60，最大 1440）", 1, 1440),
				"limit":   schemaInt("错误/警告日志最多返回条数（默认 20，最大 100）", 1, 100),
			}),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Minutes int `json:"minutes"`
				Limit   int `json:"limit"`
			}
			_ = json.Unmarshal(args, &q)
			if q.Minutes <= 0 || q.Minutes > 1440 {
				q.Minutes = 60
			}
			if q.Limit <= 0 || q.Limit > 100 {
				q.Limit = 20
			}
			return buildHealthReport(p, s.ToolCount(), q.Minutes, q.Limit), nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_find_entities",
			Description: "跨 Bot、持仓、订单、设置和日志检索关键字。敏感设置值会脱敏，适合 agent 快速定位对象和上下文。",
			InputSchema: schemaObject(map[string]any{
				"query": schemaString("检索关键字，支持 bot_id、symbol、exchange、设置 key、日志关键字等"),
				"limit": schemaInt("每类最多返回条数（默认 20，最大 100）", 1, 100),
			}, "query"),
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, err
			}
			q.Query = strings.TrimSpace(q.Query)
			if q.Query == "" {
				return nil, errors.New("query 不能为空")
			}
			if q.Limit <= 0 || q.Limit > 100 {
				q.Limit = 20
			}
			return findEntities(ctx, p, q.Query, q.Limit), nil
		},
	})
}

func buildCapabilityMap(s *Server, p Providers) map[string]any {
	categories := map[string][]map[string]any{}
	for _, entry := range sortedToolSnapshots(s.ToolSnapshots()) {
		name := entry.Tool.Name
		category := toolCategory(name)
		categories[category] = append(categories[category], map[string]any{
			"name":        name,
			"description": entry.Tool.Description,
			"write":       entry.Write,
			"required":    schemaRequired(entry.Tool.InputSchema),
		})
	}
	return map[string]any{
		"server": map[string]any{
			"name":     ServerName,
			"version":  p.Version,
			"protocol": ProtocolVersion,
		},
		"tool_count":   s.ToolCount(),
		"categories":   categories,
		"provider_map": providerMap(p),
		"workflows": []map[string]any{
			{"name": "connectivity", "tools": []string{"qm_server_info", "qm_capability_map", "qm_health_report"}},
			{"name": "bot_diagnosis", "tools": []string{"qm_list_bots", "qm_get_bot", "qm_risk_events", "qm_tail_logs"}},
			{"name": "trading_audit", "tools": []string{"qm_list_positions", "qm_list_orders", "qm_position_summary", "qm_order_pnl_audit"}},
			{"name": "pnl_review", "tools": []string{"qm_pnl_today", "qm_pnl_range", "qm_funding_payments_sum", "qm_daily_snapshots"}},
			{"name": "discovery", "tools": []string{"qm_find_entities", "qm_tool_help", "qm_list_settings"}},
		},
	}
}

func buildHealthReport(p Providers, toolCount, minutes, limit int) map[string]any {
	status := "ok"
	warnings := make([]string, 0)
	report := map[string]any{
		"generated_at": time.Now().Format(time.RFC3339),
		"version":      p.Version,
		"runtime": map[string]any{
			"go_version": runtime.Version(),
			"goroutines": runtime.NumGoroutine(),
		},
		"tool_count": toolCount,
	}

	if p.Storage == nil {
		status = "warn"
		warnings = append(warnings, "storage provider 未注入，交易/风控类工具不可用")
	} else {
		if metrics, err := p.Storage.GetLatestSystemMetrics(); err == nil && metrics != nil {
			report["latest_system_metrics"] = metrics
		} else if err != nil {
			status = "warn"
			warnings = append(warnings, "读取系统指标失败: "+err.Error())
		}
		report["bot_summary"] = summarizeBots(p.Storage)
		report["position_summary"] = summarizePositions(p.Storage)
		report["order_summary"] = summarizeOrders(p.Storage)
	}

	if p.LogStorage == nil {
		warnings = append(warnings, "log storage provider 未注入，无法读取最近错误日志")
		if status == "ok" {
			status = "warn"
		}
	} else {
		logSummary := recentProblemLogs(p.LogStorage, minutes, limit)
		report["recent_problem_logs"] = logSummary
		if count, ok := logSummary["count"].(int); ok && count > 0 {
			status = "warn"
		}
	}

	report["status"] = status
	report["warnings"] = warnings
	return report
}

func findEntities(ctx context.Context, p Providers, query string, limit int) map[string]any {
	q := strings.ToLower(query)
	results := map[string]any{}
	total := 0

	if p.Storage != nil {
		bots := make([]map[string]any, 0)
		if rows, err := p.Storage.ListBotStates(); err == nil {
			for _, row := range rows {
				if row == nil || len(bots) >= limit {
					continue
				}
				if containsAny(q, row.BotID, row.UpdatedBy, row.Reason) {
					bots = append(bots, map[string]any{
						"bot_id":     row.BotID,
						"enabled":    row.Enabled,
						"updated_at": row.UpdatedAt,
						"updated_by": row.UpdatedBy,
						"reason":     row.Reason,
					})
				}
			}
		}
		total += len(bots)
		results["bots"] = bots

		positions := make([]map[string]any, 0)
		if rows, err := p.Storage.QueryPositions(500, 0); err == nil {
			for _, row := range rows {
				if row == nil || len(positions) >= limit {
					continue
				}
				if containsAny(q, row.Symbol) {
					positions = append(positions, map[string]any{
						"symbol":        row.Symbol,
						"size":          row.Size,
						"entry_price":   row.EntryPrice,
						"current_price": row.CurrentPrice,
						"pnl":           row.PnL,
						"opened_at":     row.OpenedAt,
					})
				}
			}
		}
		total += len(positions)
		results["positions"] = positions

		orders := make([]map[string]any, 0)
		if rows, err := p.Storage.QueryOrdersWithFilter(500, 0, "", "", "", nil, nil); err == nil {
			for _, row := range rows {
				if row == nil || len(orders) >= limit {
					continue
				}
				if containsAny(q, row.BotID, row.Account, row.ClientOrderID, row.Symbol, row.Side, row.Exchange, row.Type, row.Status, row.StrategyName, row.StrategyType, row.OrderSource) {
					orders = append(orders, map[string]any{
						"order_id":        row.OrderID,
						"bot_id":          row.BotID,
						"account":         row.Account,
						"client_order_id": row.ClientOrderID,
						"symbol":          row.Symbol,
						"side":            row.Side,
						"exchange":        row.Exchange,
						"status":          row.Status,
						"strategy_name":   row.StrategyName,
						"updated_at":      row.UpdatedAt,
					})
				}
			}
		}
		total += len(orders)
		results["orders"] = orders
	}

	if p.SystemSettings != nil {
		settings := make([]map[string]any, 0)
		if rows, err := p.SystemSettings.GetSystemSettings(ctx, nil); err == nil {
			for _, row := range rows {
				if row == nil || len(settings) >= limit {
					continue
				}
				value := row.Value
				if isSensitiveKey(row.Key) {
					value = maskSettingValue(value)
				}
				if containsAny(q, row.Key, value, row.Type) {
					settings = append(settings, map[string]any{
						"key":        row.Key,
						"value":      value,
						"type":       row.Type,
						"updated_at": row.UpdatedAt,
					})
				}
			}
		}
		total += len(settings)
		results["settings"] = settings
	}

	if p.LogStorage != nil {
		logs := make([]map[string]any, 0)
		rows, _, err := p.LogStorage.GetLogs(storage.LogQueryParams{
			Keyword: query,
			Limit:   limit,
		})
		if err == nil {
			for _, row := range rows {
				if row == nil {
					continue
				}
				logs = append(logs, map[string]any{
					"id":        row.ID,
					"timestamp": row.Timestamp,
					"level":     row.Level,
					"message":   row.Message,
					"bot_id":    row.BotID,
				})
			}
		}
		total += len(logs)
		results["logs"] = logs
	}

	return map[string]any{
		"query":   query,
		"limit":   limit,
		"total":   total,
		"results": results,
	}
}

func providerMap(p Providers) map[string]bool {
	return map[string]bool{
		"storage":         p.Storage != nil,
		"log_storage":     p.LogStorage != nil,
		"backtest_tasks":  p.BacktestTasks != nil,
		"system_settings": p.SystemSettings != nil,
		"bot_control":     p.BotControl != nil,
	}
}

func sortedToolSnapshots(entries []ToolSnapshot) []ToolSnapshot {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Tool.Name < entries[j].Tool.Name
	})
	return entries
}

func toolCategory(name string) string {
	switch {
	case name == "qm_server_info" || name == "qm_capability_map" || name == "qm_tool_help" || name == "qm_health_report" || name == "qm_find_entities":
		return "meta_intelligence"
	case strings.Contains(name, "bot"):
		return "bots"
	case strings.Contains(name, "position"):
		return "positions"
	case strings.Contains(name, "order"):
		return "orders"
	case strings.Contains(name, "pnl") || strings.Contains(name, "funding") || strings.Contains(name, "snapshot"):
		return "pnl_funding"
	case strings.Contains(name, "risk") || strings.Contains(name, "metrics"):
		return "risk_health"
	case strings.Contains(name, "reconciliation") || strings.Contains(name, "audit"):
		return "reconciliation"
	case strings.Contains(name, "log"):
		return "logs"
	case strings.Contains(name, "backtest"):
		return "backtest"
	case strings.Contains(name, "setting"):
		return "settings"
	default:
		return "general"
	}
}

func toolUsageTip(name string) string {
	switch name {
	case "qm_capability_map":
		return "第一次连接 QuantMesh MCP 后优先调用，用于确认可用工具和推荐工作流。"
	case "qm_health_report":
		return "排查页面异常、交易异常或实例不稳定时先调用，再按报告中的对象继续下钻。"
	case "qm_find_entities":
		return "不知道 bot_id、symbol 或配置 key 时，先用关键字检索。"
	case "qm_tool_help":
		return "准备调用不熟悉的工具前使用，尤其要确认 required 参数。"
	default:
		return "可结合 qm_capability_map 查看它所在的推荐工作流。"
	}
}

func schemaRequired(schema map[string]any) []string {
	raw, ok := schema["required"].([]string)
	if ok {
		return raw
	}
	arr, ok := schema["required"].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func summarizeBots(s storage.Storage) map[string]any {
	rows, err := s.ListBotStates()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	enabled := 0
	for _, row := range rows {
		if row != nil && row.Enabled {
			enabled++
		}
	}
	return map[string]any{
		"total":    len(rows),
		"enabled":  enabled,
		"disabled": len(rows) - enabled,
	}
}

func summarizePositions(s storage.Storage) map[string]any {
	rows, err := s.QueryPositions(500, 0)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	totalPnL := 0.0
	for _, row := range rows {
		if row != nil {
			totalPnL += row.PnL
		}
	}
	return map[string]any{
		"sample_limit": 500,
		"count":        len(rows),
		"total_pnl":    totalPnL,
	}
}

func summarizeOrders(s storage.Storage) map[string]any {
	total, err := s.CountOrdersWithFilter("", "", "", nil, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"total": total}
}

func recentProblemLogs(ls *storage.LogStorage, minutes, limit int) map[string]any {
	start := time.Now().Add(-time.Duration(minutes) * time.Minute)
	errorRows, _, err := ls.GetLogs(storage.LogQueryParams{
		StartTime: start,
		Level:     "ERROR",
		Limit:     limit,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	warnRows, _, err := ls.GetLogs(storage.LogQueryParams{
		StartTime: start,
		Level:     "WARN",
		Limit:     limit,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	rows := append(errorRows, warnRows...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i] == nil || rows[j] == nil {
			return rows[j] == nil
		}
		return rows[i].Timestamp.After(rows[j].Timestamp)
	})

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, map[string]any{
			"id":        row.ID,
			"timestamp": row.Timestamp,
			"level":     row.Level,
			"message":   row.Message,
			"bot_id":    row.BotID,
		})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{
		"window_minutes": minutes,
		"count":          len(out),
		"logs":           out,
	}
}

func containsAny(query string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
