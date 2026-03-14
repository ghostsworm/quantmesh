package option

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const binanceOptionBase = "https://eapi.binance.com"
const binanceOptionTestnet = "https://testnet.binancefuture.com" // 期权测试网可能不同

// BinanceAdapter Binance 期权适配器
type BinanceAdapter struct {
	apiKey    string
	secretKey string
	baseURL   string
	client    *http.Client
}

// NewBinanceAdapter 创建 Binance 期权适配器
func NewBinanceAdapter(apiKey, secretKey string, isTestnet bool) *BinanceAdapter {
	base := binanceOptionBase
	if isTestnet {
		base = "https://testnet.eapi.binance.com" // 若 Binance 提供期权测试网
	}
	return &BinanceAdapter{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   base,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// binanceOptionPosition 原始持仓
type binanceOptionPosition struct {
	Symbol       string  `json:"symbol"`
	PositionSide string  `json:"positionSide"`
	Side         string  `json:"side"`
	Quantity     float64 `json:"quantity"`
	EntryPrice   float64 `json:"entryPrice"`
	MarkPrice    float64 `json:"markPrice"`
	StrikePrice  float64 `json:"strikePrice"`
	ExpiryDate   int64   `json:"expiryDate"`
	OptionSide   string  `json:"optionSide"` // PUT / CALL
	UnrealizedPNL float64 `json:"unrealizedProfit"`
}

// Exchange 实现 Adapter
func (b *BinanceAdapter) Exchange() string { return "binance" }

// FetchPositions 拉取期权仓位，按 direction 决定拉 Put 还是 Call
func (b *BinanceAdapter) FetchPositions(ctx context.Context, params FetchParams) ([]OptionHedgePosition, error) {
	if b.apiKey == "" || b.secretKey == "" {
		return nil, fmt.Errorf("binance options: api credentials required")
	}

	positions, err := b.getPositions(ctx)
	if err != nil {
		return nil, err
	}

	wantRight := "PUT"
	if strings.ToUpper(params.Direction) == "SHORT" {
		wantRight = "CALL"
	}

	var out []OptionHedgePosition
	now := time.Now()
	for _, p := range positions {
		if strings.ToUpper(p.OptionSide) != wantRight {
			continue
		}
		if p.Quantity == 0 {
			continue
		}
		pos := OptionHedgePosition{
			Exchange:   "binance",
			Symbol:     params.Symbol,
			Instrument: p.Symbol,
			Right:      wantRight,
			Strike:     p.StrikePrice,
			Expiry:     time.UnixMilli(p.ExpiryDate),
			Qty:        p.Quantity,
			MarkPrice:  p.MarkPrice,
			Delta:      0, // Binance 需单独查 mark 接口获取 Greeks
			Premium:    p.EntryPrice * p.Quantity,
			Source:     "api",
			UpdatedAt:  now,
		}
		out = append(out, pos)
	}
	logger.Info("Binance options: fetched %d %s positions", len(out), wantRight)
	return out, nil
}

func (b *BinanceAdapter) getPositions(ctx context.Context) ([]binanceOptionPosition, error) {
	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	sig := b.sign(params.Encode())
	params.Set("signature", sig)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/eapi/v1/position?"+params.Encode(), nil)
	req.Header.Set("X-MBX-APIKEY", b.apiKey)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("binance options api: %d %s", resp.StatusCode, errBody.Msg)
	}

	var raw []binanceOptionPosition
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (b *BinanceAdapter) sign(query string) string {
	h := hmac.New(sha256.New, []byte(b.secretKey))
	h.Write([]byte(query))
	return hex.EncodeToString(h.Sum(nil))
}
