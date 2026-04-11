package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/ai/service"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
)

// defaultTestSymbol 用於建立適配器；GetAccount 不依賴交易對，但部分交易所建適配器時需要合法 symbol。
func defaultTestSymbolAndMarket(exchangeName, requestedMarket string) (symbol string, market string) {
	exchangeName = strings.ToLower(strings.TrimSpace(exchangeName))
	market = strings.ToLower(strings.TrimSpace(requestedMarket))
	if market == "" {
		market = "futures"
	}
	switch exchangeName {
	case "bitkub", "coinsph":
		market = "spot"
	}
	switch exchangeName {
	case "bitkub":
		return "BTC_THB", market
	case "coinsph":
		return "BTC_PHP", market
	default:
		return "BTCUSDT", market
	}
}

type testGeminiRequest struct {
	GeminiAPIKey string `json:"gemini_api_key"`
}

type testGeminiResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	ReplyPreview string `json:"reply_preview,omitempty"`
	Model        string `json:"model,omitempty"`
}

// postConfigTestGemini POST /api/config/test-gemini
func postConfigTestGemini(c *gin.Context) {
	var req testGeminiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, testGeminiResponse{Success: false, Message: "請求格式錯誤"})
		return
	}
	key := strings.TrimSpace(req.GeminiAPIKey)
	if key == "" {
		c.JSON(http.StatusOK, testGeminiResponse{Success: false, Message: "請填寫 Gemini API Key"})
		return
	}

	sv := service.NewAIService()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	model := "gemini-2.0-flash"
	resp, err := sv.GenerateContent(ctx, service.AIRequest{
		Prompt:              "Reply with exactly one word: OK",
		SystemInstruction:   "You are an API connectivity check. Respond with only the letters OK, nothing else.",
		GeminiAPIKey:        key,
		Model:               model,
		ResponseMimeType:    "text/plain",
	})
	if err != nil {
		logger.Warn("test-gemini: %v", err)
		c.JSON(http.StatusOK, testGeminiResponse{Success: false, Message: fmt.Sprintf("請求失敗: %v", err)})
		return
	}
	if resp == nil || !resp.Success {
		msg := "Gemini API 返回失敗"
		if resp != nil && resp.Error != "" {
			msg = resp.Error
		}
		c.JSON(http.StatusOK, testGeminiResponse{Success: false, Message: msg, Model: model})
		return
	}

	preview := strings.TrimSpace(resp.Content)
	if len(preview) > 120 {
		preview = preview[:120] + "…"
	}
	c.JSON(http.StatusOK, testGeminiResponse{
		Success:      true,
		Message:      "連線成功，金鑰可用",
		ReplyPreview: preview,
		Model:        model,
	})
}

type testExchangeRequest struct {
	Exchange   string `json:"exchange"`
	APIKey     string `json:"api_key"`
	SecretKey  string `json:"secret_key"`
	Passphrase string `json:"passphrase,omitempty"`
	Testnet    bool   `json:"testnet"`
	MarketType string `json:"market_type"` // futures | spot，默認 futures；Bitkub/Coins.ph 強制現貨
}

type testExchangeResponse struct {
	Success              bool    `json:"success"`
	Message              string  `json:"message"`
	Exchange             string  `json:"exchange,omitempty"`
	MarketType           string  `json:"market_type,omitempty"`
	TotalWalletBalance   float64 `json:"total_wallet_balance,omitempty"`
	AvailableBalance     float64 `json:"available_balance,omitempty"`
	TotalMarginBalance   float64 `json:"total_margin_balance,omitempty"`
}

// postConfigTestExchange POST /api/config/test-exchange
func postConfigTestExchange(c *gin.Context) {
	var req testExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, testExchangeResponse{Success: false, Message: "請求格式錯誤"})
		return
	}
	exName := strings.ToLower(strings.TrimSpace(req.Exchange))
	if exName == "" {
		c.JSON(http.StatusOK, testExchangeResponse{Success: false, Message: "請指定交易所"})
		return
	}
	if strings.TrimSpace(req.APIKey) == "" || strings.TrimSpace(req.SecretKey) == "" {
		c.JSON(http.StatusOK, testExchangeResponse{Success: false, Message: "請填寫 API Key 與 Secret"})
		return
	}

	symbol, market := defaultTestSymbolAndMarket(exName, req.MarketType)

	ec := config.ExchangeConfig{
		APIKey:     strings.TrimSpace(req.APIKey),
		SecretKey:  strings.TrimSpace(req.SecretKey),
		Passphrase: strings.TrimSpace(req.Passphrase),
		Testnet:    req.Testnet,
	}

	cfg := config.CreateMinimalConfig()
	cfg.Exchanges = map[string]config.ExchangeConfig{exName: ec}
	cfg.App.CurrentExchange = exName
	cfg.Trading.Symbol = symbol

	ex, err := exchange.NewExchange(cfg, exName, symbol, market)
	if err != nil {
		logger.Warn("test-exchange create %s: %v", exName, err)
		c.JSON(http.StatusOK, testExchangeResponse{Success: false, Message: fmt.Sprintf("無法建立連線: %v", err), Exchange: exName, MarketType: market})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	acc, err := ex.GetAccount(ctx)
	if err != nil {
		logger.Warn("test-exchange GetAccount %s: %v", exName, err)
		c.JSON(http.StatusOK, testExchangeResponse{
			Success:    false,
			Message:    fmt.Sprintf("已連上適配器，但讀取帳戶失敗（請檢查權限與密鑰）: %v", err),
			Exchange:   exName,
			MarketType: market,
		})
		return
	}
	if acc == nil {
		c.JSON(http.StatusOK, testExchangeResponse{Success: false, Message: "帳戶資料為空", Exchange: exName, MarketType: market})
		return
	}

	c.JSON(http.StatusOK, testExchangeResponse{
		Success:            true,
		Message:            "連線成功，已讀取帳戶餘額",
		Exchange:           exName,
		MarketType:         market,
		TotalWalletBalance: acc.TotalWalletBalance,
		TotalMarginBalance: acc.TotalMarginBalance,
		AvailableBalance:   acc.AvailableBalance,
	})
}
