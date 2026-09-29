package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

// BatchCreateFundingRequest 批量建立 funding_carry Bot
type BatchCreateFundingRequest struct {
	Exchange       string                 `json:"exchange"`
	Symbols        []string               `json:"symbols"`
	TotalCapital   float64                `json:"total_capital"`
	Allocation     string                 `json:"allocation"`
	StrategyConfig map[string]interface{} `json:"strategy_config"`
}

type BatchCreateFundingResponse struct {
	Created []string `json:"created"`
	Errors  []string `json:"errors,omitempty"`
}

const fundingCarryReportingAsset = "USDT"

// postBatchCreateFunding POST /api/funding-carry/batch-create
func postBatchCreateFunding(c *gin.Context) {
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	var req BatchCreateFundingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	if req.Exchange == "" || len(req.Symbols) == 0 {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_config")
		return
	}
	if req.TotalCapital < float64(len(req.Symbols))*200 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "insufficient_capital",
			"error_key": "error.funding_carry_insufficient_capital",
			"message":   "總資金不足，每幣種至少需要 200 USDT",
		})
		return
	}

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	perSymbolCap := req.TotalCapital / float64(len(req.Symbols))

	var created []string
	var errors []string

	for _, sym := range req.Symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym == "" {
			continue
		}

		botID := config.GenerateBotID(req.Exchange, sym, config.MarketTypeFundingCarry)

		conflict := false
		for _, existing := range cfg.Bots {
			if existing.Exchange == req.Exchange && existing.Symbol == sym {
				errors = append(errors, sym+": 已存在同交易對 Bot")
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}

		strategies := []config.StrategyInstance{{
			Type:   "funding_carry",
			Weight: 1.0,
			Config: req.StrategyConfig,
		}}

		enabled := true
		bc := config.BotConfig{
			ID:                    uuid.New().String(),
			Name:                  "FC-" + sym,
			Exchange:              req.Exchange,
			Symbol:                sym,
			MarketType:            config.MarketTypeFundingCarry,
			TotalAllocatedCapital: perSymbolCap,
			Strategies:            strategies,
			Enabled:               &enabled,
		}
		_ = botID

		cfg.Bots = append(cfg.Bots, bc)
		created = append(created, sym)
	}

	if len(created) > 0 {
		if err := fileConfigManager.UpdateConfigWithBotHistorySource(cfg, "post_funding_carry_batch"); err != nil {
			respondError(c, http.StatusInternalServerError, "error.config_save_failed")
			return
		}

		if botManagerProvider() != nil {
			for _, bc := range cfg.Bots {
				if bc.MarketType != config.MarketTypeFundingCarry {
					continue
				}
				found := false
				for _, sym := range created {
					if bc.Symbol == sym && bc.Exchange == req.Exchange {
						found = true
						break
					}
				}
				if !found {
					continue
				}
				go func(b config.BotConfig) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := botManagerProvider().StartBot(ctx, b); err != nil {
						logger.Warn("⚠️ 批量啟動 Bot %s 失敗: %v", b.ID, err)
					}
				}(bc)
			}
		}
	}

	c.JSON(http.StatusOK, BatchCreateFundingResponse{
		Created: created,
		Errors:  errors,
	})
}

// getFundingCarryDashboard GET /api/funding-carry/dashboard
func getFundingCarryDashboard(c *gin.Context) {
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	storageProv := PickStorageProvider(c)
	now := time.Now()

	var totalIncome24h, totalIncome7d, totalIncome30d, totalIncomeAll float64
	var activeBots int
	var totalCapital float64

	type symbolInfo struct {
		Symbol    string  `json:"symbol"`
		BotID     string  `json:"bot_id"`
		Status    string  `json:"status"`
		Capital   float64 `json:"capital"`
		Income24h float64 `json:"income_24h"`
		Income7d  float64 `json:"income_7d"`
	}
	var symbols []symbolInfo
	seenSymbolIncome := make(map[string]struct{})

	for _, bc := range cfg.Bots {
		if bc.MarketType != config.MarketTypeFundingCarry {
			continue
		}

		activeBots++
		totalCapital += bc.TotalAllocatedCapital

		status := "stopped"
		if botManagerProvider() != nil {
			for _, br := range botManagerProvider().ListBots() {
				if br.BotID == bc.ID && br.Running {
					status = "running"
					break
				}
			}
		}

		var inc24h, inc7d float64
		if storageProv != nil {
			st := storageProv.GetStorage()
			if scoped, ok := st.(interface {
				GetFundingPaymentsSumByScope(exchange, marketType, symbol, asset, accountScope string, startTime, endTime time.Time) (float64, error)
			}); ok && st != nil {
				accountScope := accountIDForExchange(cfg, bc.Exchange)
				marketType := bc.GetMarketType()
				if marketType == config.MarketTypeFundingCarry {
					marketType = "futures"
				}
				inc24h, _ = scoped.GetFundingPaymentsSumByScope(bc.Exchange, marketType, bc.Symbol, fundingCarryReportingAsset, accountScope, now.Add(-24*time.Hour), now)
				inc7d, _ = scoped.GetFundingPaymentsSumByScope(bc.Exchange, marketType, bc.Symbol, fundingCarryReportingAsset, accountScope, now.Add(-7*24*time.Hour), now)
			}
		}

		incomeKey := strings.ToLower(bc.Exchange) + "\x00" + strings.ToUpper(bc.Symbol) + "\x00" + accountIDForExchange(cfg, bc.Exchange)
		if _, alreadyCounted := seenSymbolIncome[incomeKey]; !alreadyCounted {
			seenSymbolIncome[incomeKey] = struct{}{}
			totalIncome24h += inc24h
			totalIncome7d += inc7d
		}

		symbols = append(symbols, symbolInfo{
			Symbol:    bc.Symbol,
			BotID:     bc.ID,
			Status:    status,
			Capital:   bc.TotalAllocatedCapital,
			Income24h: inc24h,
			Income7d:  inc7d,
		})
	}

	if storageProv != nil {
		if scoped, ok := storageProv.GetStorage().(interface {
			GetFundingPaymentsSumByScope(exchange, marketType, symbol, asset, accountScope string, startTime, endTime time.Time) (float64, error)
		}); ok {
			seen := make(map[string]struct{})
			for _, bc := range cfg.Bots {
				if bc.MarketType != config.MarketTypeFundingCarry {
					continue
				}
				scope := accountIDForExchange(cfg, bc.Exchange)
				key := strings.ToLower(bc.Exchange) + "\x00" + strings.ToUpper(bc.Symbol) + "\x00" + scope
				if scope == "" {
					continue
				}
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				income30d, err30d := scoped.GetFundingPaymentsSumByScope(bc.Exchange, "futures", bc.Symbol, fundingCarryReportingAsset, scope, now.Add(-30*24*time.Hour), now)
				incomeAll, errAll := scoped.GetFundingPaymentsSumByScope(bc.Exchange, "futures", bc.Symbol, fundingCarryReportingAsset, scope, time.Time{}, now)
				if err30d == nil && errAll == nil {
					totalIncome30d += income30d
					totalIncomeAll += incomeAll
				}
			}
		}
	}

	annualized := 0.0
	if totalCapital > 0 && totalIncome7d != 0 {
		annualized = (totalIncome7d / 7) * 365 / totalCapital
	}

	var dailyIncome []map[string]interface{}
	if storageProv != nil {
		st := storageProv.GetStorage()
		if st != nil {
			if scoped, ok := st.(interface {
				GetDailyFundingPaymentsByAccountScopeAndAsset(exchange, marketType, asset, accountScope string, startTime, endTime time.Time) (map[string]float64, error)
			}); ok {
				dailyTotals := make(map[string]float64)
				seenScopes := make(map[string]struct{})
				for _, bc := range cfg.Bots {
					if bc.MarketType != config.MarketTypeFundingCarry {
						continue
					}
					scope := accountIDForExchange(cfg, bc.Exchange)
					key := strings.ToLower(bc.Exchange) + "\x00futures\x00" + scope
					if scope == "" {
						continue
					}
					if _, exists := seenScopes[key]; exists {
						continue
					}
					seenScopes[key] = struct{}{}
					daily, err := scoped.GetDailyFundingPaymentsByAccountScopeAndAsset(bc.Exchange, "futures", fundingCarryReportingAsset, scope, now.Add(-30*24*time.Hour), now)
					if err == nil {
						for date, income := range daily {
							dailyTotals[date] += income
						}
					}
				}
				for date, income := range dailyTotals {
					dailyIncome = append(dailyIncome, map[string]interface{}{"date": date, "income": income})
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"overview": gin.H{
			"total_income_24h":       totalIncome24h,
			"total_income_7d":        totalIncome7d,
			"total_income_30d":       totalIncome30d,
			"total_income_all":       totalIncomeAll,
			"annualized_yield":       annualized,
			"active_bots":            activeBots,
			"total_capital_deployed": totalCapital,
		},
		"symbols":      symbols,
		"daily_income": dailyIncome,
	})
}

// getFundingCarryStatus GET /api/funding-carry/status/:botId
func getFundingCarryStatus(c *gin.Context) {
	botID := c.Param("botId")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return
	}

	if botManagerProvider() == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_not_ready")
		return
	}

	detail, found := botManagerProvider().GetBot(botID)
	if !found || detail == nil {
		respondError(c, http.StatusNotFound, "error.bot_not_found")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bot_id":   botID,
		"symbol":   detail.Symbol,
		"exchange": detail.Exchange,
		"running":  detail.Running,
	})
}

// getFundingIncomeHistory GET /api/funding-carry/income-history
func getFundingIncomeHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
		return
	}

	symbol := c.Query("symbol")
	period := c.DefaultQuery("period", "30d")

	var start time.Time
	now := time.Now()
	switch period {
	case "7d":
		start = now.Add(-7 * 24 * time.Hour)
	case "90d":
		start = now.Add(-90 * 24 * time.Hour)
	case "1y":
		start = now.Add(-365 * 24 * time.Hour)
	default:
		start = now.Add(-30 * 24 * time.Hour)
	}

	cfg, _ := GetLatestConfig()
	accountScope := ""
	exchangeID := ""
	if cfg != nil {
		for _, bc := range cfg.Bots {
			if bc.MarketType == config.MarketTypeFundingCarry {
				exchangeID = bc.Exchange
				accountScope = accountIDForExchange(cfg, bc.Exchange)
				break
			}
		}
	}
	fundingReader, ok := st.(interface {
		GetFundingPaymentsByAccountScope(accountScope, exchange string, startTime, endTime time.Time) ([]*storage.FundingPayment, error)
	})
	if !ok || accountScope == "" || exchangeID == "" {
		respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
		return
	}
	records, err := fundingReader.GetFundingPaymentsByAccountScope(accountScope, exchangeID, start, now)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_failed", err)
		return
	}

	type paymentItem struct {
		Symbol    string  `json:"symbol"`
		Income    float64 `json:"income"`
		Asset     string  `json:"asset"`
		TradeTime string  `json:"trade_time"`
	}
	var items []paymentItem
	for _, r := range records {
		if symbol != "" && !strings.EqualFold(r.Symbol, symbol) {
			continue
		}
		items = append(items, paymentItem{
			Symbol:    r.Symbol,
			Income:    r.Income,
			Asset:     r.Asset,
			TradeTime: r.TradeTime.Format(time.RFC3339),
		})
	}

	c.JSON(http.StatusOK, gin.H{"records": items, "total": len(items)})
}
