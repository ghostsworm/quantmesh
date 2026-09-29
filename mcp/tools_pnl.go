package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/storage"
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
			Description: "按精确交易所、凭据作用域和盈亏币种返回今日已配对成交的净盈亏与笔数；不包含未实现盈亏。",
			InputSchema: schemaObject(map[string]any{
				"exchange":      schemaString("交易所代码（必填，例 binance）"),
				"account_scope": schemaString("凭据作用域标识（必填）"),
				"pnl_asset":     schemaString("盈亏计价币种（必填，例 USDT）"),
			}, "exchange", "account_scope", "pnl_asset"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange     string `json:"exchange"`
				AccountScope string `json:"account_scope"`
				PnLAsset     string `json:"pnl_asset"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, fmt.Errorf("解析今日盈亏查询参数失败: %w", err)
			}
			now := utils.NowConfiguredTimezone()
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
			rows, err := readScopedPnLRows(p.Storage, q.Exchange, q.AccountScope, q.PnLAsset, start, now.UTC())
			if err != nil {
				return nil, err
			}
			var trades int
			var netPnL float64
			for _, row := range rows {
				trades += row.TotalTrades
				netPnL += row.TotalPnL
			}
			if math.IsNaN(netPnL) || math.IsInf(netPnL, 0) {
				return nil, fmt.Errorf("今日盈亏总额非有限数，拒绝返回")
			}
			date := now.Format("2006-01-02")
			return map[string]any{
				"date":         date,
				"exchange":     strings.ToLower(strings.TrimSpace(q.Exchange)),
				"pnl_asset":    strings.ToUpper(strings.TrimSpace(q.PnLAsset)),
				"trades_count": trades,
				"net_pnl":      netPnL,
			}, nil
		},
	})

	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_pnl_range",
			Description: "按精确交易所、凭据作用域和盈亏币种查询已配对成交 PnL，days 默认 7，最大 90；不跨账户或币种合计。",
			InputSchema: schemaObject(map[string]any{
				"exchange":      schemaString("交易所代码（必填，例 binance）"),
				"account_scope": schemaString("凭据作用域标识（必填）"),
				"pnl_asset":     schemaString("盈亏计价币种（必填，例 USDT）"),
				"days":          schemaInt("回看天数（1-90，默认 7）", 1, 90),
			}, "exchange", "account_scope", "pnl_asset"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var q struct {
				Exchange     string `json:"exchange"`
				AccountScope string `json:"account_scope"`
				PnLAsset     string `json:"pnl_asset"`
				Days         int    `json:"days"`
			}
			if err := json.Unmarshal(args, &q); err != nil {
				return nil, fmt.Errorf("解析区间盈亏查询参数失败: %w", err)
			}
			if q.Days <= 0 || q.Days > 90 {
				q.Days = 7
			}
			localEnd := utils.NowConfiguredTimezone()
			start := localEnd.AddDate(0, 0, -q.Days).UTC()
			end := localEnd.UTC()
			rows, err := readScopedPnLRows(p.Storage, q.Exchange, q.AccountScope, q.PnLAsset, start, end)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"start":     start.Format(time.RFC3339),
				"end":       end.Format(time.RFC3339),
				"exchange":  strings.ToLower(strings.TrimSpace(q.Exchange)),
				"pnl_asset": strings.ToUpper(strings.TrimSpace(q.PnLAsset)),
				"count":     len(rows),
				"rows":      rows,
			}, nil
		},
	})
}

func readScopedPnLRows(st storage.Storage, exchange, accountScope, asset string, start, end time.Time) ([]*storage.PnLBySymbol, error) {
	reader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		return nil, fmt.Errorf("存储不支持凭据作用域与盈亏币种隔离查询")
	}
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(asset) == "" {
		return nil, fmt.Errorf("交易所、凭据作用域和盈亏币种均为必填")
	}
	return reader.GetPnLByAccountScopeAndAsset(strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(accountScope), strings.ToUpper(strings.TrimSpace(asset)), start, end)
}
