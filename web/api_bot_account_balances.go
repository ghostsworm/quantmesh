package web

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
)

// findBotConfigByID 與 Bot 列表 / 啟動邏輯一致：空 ID 時用 BotIDOrGenerate（含 funding_perp_spread）
func findBotConfigByID(cfg *config.Config, botID string) *config.BotConfig {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.BotIDOrGenerate(cfg.Bots[i])
		}
		if id == botID {
			return &cfg.Bots[i]
		}
	}
	return nil
}

func futuresAccountPayload(acc *exchange.Account, err error, quoteAsset string) gin.H {
	h := gin.H{"quote_asset": quoteAsset}
	if err != nil {
		h["error"] = err.Error()
		return h
	}
	if acc == nil {
		return h
	}
	h["total_wallet_balance"] = acc.TotalWalletBalance
	h["available_balance"] = acc.AvailableBalance
	h["total_margin_balance"] = acc.TotalMarginBalance
	h["account_leverage"] = acc.AccountLeverage
	return h
}

// getBotAccountBalances GET /api/bots/:id/account-balances
// 按 Bot 的交易所、交易對、市場類型查詢交易所帳戶：現貨返回標的兩幣可用餘額；合約返回帳戶匯總。
func getBotAccountBalances(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}
	botCfg := findBotConfigByID(cfg, botID)
	if botCfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "bot config not found"})
		return
	}
	mt := botCfg.GetMarketType()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	switch mt {
	case config.MarketTypeFundingCarry:
		spotEx, err1 := getExchangeForCancel(botCfg.Exchange, botCfg.Symbol, "spot")
		futEx, err2 := getExchangeForCancel(botCfg.Exchange, botCfg.Symbol, "futures")
		if err1 != nil {
			logger.Warn("account-balances funding_carry spot: %v", err1)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err1.Error()})
			return
		}
		if err2 != nil {
			logger.Warn("account-balances funding_carry futures: %v", err2)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err2.Error()})
			return
		}
		base, quote := spotEx.GetBaseAsset(), spotEx.GetQuoteAsset()
		baseBal, bErr := spotEx.GetBalance(ctx, base)
		quoteBal, qErr := spotEx.GetBalance(ctx, quote)
		futAcc, faErr := futEx.GetAccount(ctx)
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"exchange":    botCfg.Exchange,
			"symbol":      botCfg.Symbol,
			"market_type": mt,
			"funding_carry": gin.H{
				"spot": gin.H{
					"base_asset":      base,
					"quote_asset":     quote,
					"base_available":  baseBal,
					"quote_available": quoteBal,
					"base_error":      errString(bErr),
					"quote_error":     errString(qErr),
				},
				"futures": futuresAccountPayload(futAcc, faErr, futEx.GetQuoteAsset()),
			},
		})
	case config.MarketTypeFundingPerpSpread:
		fp := botCfg.FundingPerpSpread
		if fp == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "funding_perp_spread config missing"})
			return
		}
		legs := []gin.H{}
		for _, item := range []struct {
			name string
			leg  config.FundingPerpLeg
		}{
			{name: "leg_a", leg: fp.LegA},
			{name: "leg_b", leg: fp.LegB},
		} {
			ex, exErr := getExchangeForCancel(item.leg.Exchange, item.leg.Symbol, "futures")
			if exErr != nil {
				legs = append(legs, gin.H{
					"name": item.name, "exchange": item.leg.Exchange, "symbol": item.leg.Symbol,
					"error": exErr.Error(),
				})
				continue
			}
			acc, accErr := ex.GetAccount(ctx)
			legs = append(legs, gin.H{
				"name":     item.name,
				"exchange": item.leg.Exchange,
				"symbol":   item.leg.Symbol,
				"futures":  futuresAccountPayload(acc, accErr, ex.GetQuoteAsset()),
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"market_type": mt,
			"funding_perp_spread": gin.H{
				"legs": legs,
			},
		})
	default:
		ex, exErr := getExchangeForCancel(botCfg.Exchange, botCfg.Symbol, mt)
		if exErr != nil {
			logger.Warn("account-balances NewExchange: %v", exErr)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": exErr.Error()})
			return
		}
		if mt == "spot" || mt == "spot_margin" {
			base, quote := ex.GetBaseAsset(), ex.GetQuoteAsset()
			baseBal, bErr := ex.GetBalance(ctx, base)
			quoteBal, qErr := ex.GetBalance(ctx, quote)
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"exchange":    botCfg.Exchange,
				"symbol":      botCfg.Symbol,
				"market_type": mt,
				"spot": gin.H{
					"base_asset":      base,
					"quote_asset":     quote,
					"base_available":  baseBal,
					"quote_available": quoteBal,
					"base_error":      errString(bErr),
					"quote_error":     errString(qErr),
				},
			})
			return
		}
		acc, accErr := ex.GetAccount(ctx)
		c.JSON(http.StatusOK, gin.H{
			"success":     accErr == nil,
			"exchange":    botCfg.Exchange,
			"symbol":      botCfg.Symbol,
			"market_type": mt,
			"futures":     futuresAccountPayload(acc, accErr, ex.GetQuoteAsset()),
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
