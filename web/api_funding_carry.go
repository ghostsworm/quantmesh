package web

import (
	"context"
	"errors"
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
const fundingCarryIncomeBasis = "gross_account_scoped_futures_funding"

func logFundingCarryBotStartFailure(botID string, _ error) {
	logger.Warn("⚠️ 批量啟動 Bot %s 失敗；底層診斷未輸出至通用日誌", botID)
}

type fundingCarryScopedReader interface {
	GetFundingPaymentsSumByScope(exchange, marketType, symbol, asset, accountScope string, startTime, endTime time.Time) (float64, error)
	GetDailyFundingPaymentsByAccountScopeAndAsset(exchange, marketType, asset, accountScope string, startTime, endTime time.Time) (map[string]float64, error)
}

type fundingCarryDashboardSymbolInfo struct {
	Symbol  string  `json:"symbol"`
	BotID   string  `json:"bot_id"`
	Status  string  `json:"status"`
	Capital float64 `json:"capital"`
}

func readFundingCarrySum(reader interface {
	GetFundingPaymentsSumByScope(exchange, marketType, symbol, asset, accountScope string, startTime, endTime time.Time) (float64, error)
}, exchange, symbol, scope string, start, end time.Time) (float64, error) {
	if reader == nil || strings.TrimSpace(exchange) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(scope) == "" || end.IsZero() || (!start.IsZero() && !start.Before(end)) {
		return 0, errors.New("funding carry query requires complete scope and valid time range")
	}
	total, err := reader.GetFundingPaymentsSumByScope(exchange, "futures", symbol, fundingCarryReportingAsset, scope, start, end)
	if err != nil {
		return 0, err
	}
	if value, ok := addFiniteProfitValues(total); !ok {
		return 0, errors.New("funding carry query returned a non-finite total")
	} else {
		return value, nil
	}
}

func mergeFundingCarryDailyTotals(target map[string]float64, source map[string]float64) error {
	if target == nil {
		return errors.New("funding carry daily totals map is missing")
	}
	for date, income := range source {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return errors.New("funding carry daily totals contain an invalid date")
		}
		if err := addFiniteProfitToMap(target, date, income); err != nil {
			return err
		}
	}
	return nil
}

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
						logFundingCarryBotStartFailure(b.ID, err)
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
	var scopedReader fundingCarryScopedReader
	for _, bc := range cfg.Bots {
		if bc.MarketType == config.MarketTypeFundingCarry {
			if storageProv == nil || storageProv.GetStorage() == nil {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
			var ok bool
			scopedReader, ok = storageProv.GetStorage().(fundingCarryScopedReader)
			if !ok {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
			break
		}
	}

	var totalIncome24h, totalIncome7d, totalIncome30d, totalIncomeAll float64
	var activeBots int
	var totalCapital float64
	var sumOK bool

	var symbols []fundingCarryDashboardSymbolInfo
	seenSymbolIncome := make(map[string]struct{})

	for _, bc := range cfg.Bots {
		if bc.MarketType != config.MarketTypeFundingCarry {
			continue
		}

		if bc.TotalAllocatedCapital < 0 {
			respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
			return
		}
		totalCapital, sumOK = addFiniteProfitValues(totalCapital, bc.TotalAllocatedCapital)
		if !sumOK {
			respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
			return
		}

		status := "stopped"
		if botManagerProvider() != nil {
			for _, br := range botManagerProvider().ListBots() {
				if br.BotID == bc.ID {
					status = fundingCarryDashboardStatus(br)
					break
				}
			}
		}
		if status == "running" {
			activeBots++
		}

		var inc24h, inc7d float64
		if scopedReader != nil {
			accountScope := accountIDForExchange(cfg, bc.Exchange)
			var queryErr error
			inc24h, queryErr = readFundingCarrySum(scopedReader, bc.Exchange, bc.Symbol, accountScope, now.Add(-24*time.Hour), now)
			if queryErr == nil {
				inc7d, queryErr = readFundingCarrySum(scopedReader, bc.Exchange, bc.Symbol, accountScope, now.Add(-7*24*time.Hour), now)
			}
			if queryErr != nil {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
		}

		incomeKey := strings.ToLower(bc.Exchange) + "\x00" + strings.ToUpper(bc.Symbol) + "\x00" + accountIDForExchange(cfg, bc.Exchange)
		if _, alreadyCounted := seenSymbolIncome[incomeKey]; !alreadyCounted {
			seenSymbolIncome[incomeKey] = struct{}{}
			totalIncome24h, sumOK = addFiniteProfitValues(totalIncome24h, inc24h)
			if sumOK {
				totalIncome7d, sumOK = addFiniteProfitValues(totalIncome7d, inc7d)
			}
			if !sumOK {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
		}

		symbols = append(symbols, fundingCarryDashboardSymbolInfo{
			Symbol:  bc.Symbol,
			BotID:   bc.ID,
			Status:  status,
			Capital: bc.TotalAllocatedCapital,
		})
	}

	if scopedReader != nil {
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
			income30d, err30d := readFundingCarrySum(scopedReader, bc.Exchange, bc.Symbol, scope, now.Add(-30*24*time.Hour), now)
			incomeAll, errAll := readFundingCarrySum(scopedReader, bc.Exchange, bc.Symbol, scope, time.Time{}, now)
			if err30d != nil || errAll != nil {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
			totalIncome30d, sumOK = addFiniteProfitValues(totalIncome30d, income30d)
			if sumOK {
				totalIncomeAll, sumOK = addFiniteProfitValues(totalIncomeAll, incomeAll)
			}
			if !sumOK {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
		}
	}

	annualized := 0.0
	if totalCapital > 0 && totalIncome7d != 0 {
		annualized = (totalIncome7d / 7) * 365 / totalCapital
		if _, ok := addFiniteProfitValues(annualized); !ok {
			respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
			return
		}
	}

	var dailyIncome []map[string]interface{}
	if scopedReader != nil {
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
			daily, err := scopedReader.GetDailyFundingPaymentsByAccountScopeAndAsset(bc.Exchange, "futures", fundingCarryReportingAsset, scope, now.Add(-30*24*time.Hour), now)
			if err != nil || mergeFundingCarryDailyTotals(dailyTotals, daily) != nil {
				respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
				return
			}
		}
		for date, income := range dailyTotals {
			dailyIncome = append(dailyIncome, map[string]interface{}{"date": date, "income": income})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"overview": gin.H{
			"income_basis":           fundingCarryIncomeBasis,
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

	status := fundingCarryDashboardStatus(detail.BotResponse)
	c.JSON(http.StatusOK, gin.H{
		"bot_id":                botID,
		"symbol":                detail.Symbol,
		"exchange":              detail.Exchange,
		"managed":               detail.Running,
		"running":               status == "running",
		"status":                status,
		"funding_carry_runtime": detail.FundingCarryRuntime,
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
		verified, verifyErr := fundingPaymentItemFromRecord(r, accountScope, exchangeID)
		if verifyErr != nil {
			respondError(c, http.StatusServiceUnavailable, "error.storage_unavailable")
			return
		}
		if symbol != "" && !strings.EqualFold(verified.Symbol, symbol) {
			continue
		}
		items = append(items, paymentItem{
			Symbol:    verified.Symbol,
			Income:    verified.Income,
			Asset:     verified.Asset,
			TradeTime: verified.TradeTime,
		})
	}

	c.JSON(http.StatusOK, gin.H{"income_basis": fundingCarryIncomeBasis, "records": items, "total": len(items)})
}
