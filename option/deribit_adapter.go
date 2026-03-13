package option

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const deribitMainnet = "https://www.deribit.com"
const deribitTestnet = "https://test.deribit.com"

// DeribitAdapter Deribit 期权适配器
type DeribitAdapter struct {
	apiKey    string
	secretKey string
	baseURL   string
	client    *http.Client
	token     string
}

// NewDeribitAdapter 创建 Deribit 适配器
func NewDeribitAdapter(apiKey, secretKey string, isTestnet bool) *DeribitAdapter {
	base := deribitMainnet
	if isTestnet {
		base = deribitTestnet
	}
	return &DeribitAdapter{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   base,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// deribitPosition 原始持仓结构
type deribitPosition struct {
	InstrumentName     string  `json:"instrument_name"`
	Size               float64 `json:"size"`
	Direction          string  `json:"direction"`
	AveragePrice       float64 `json:"average_price"`
	MarkPrice          float64 `json:"mark_price"`
	TotalProfitLoss    float64 `json:"total_profit_loss"`
	Delta              float64 `json:"delta"`
	Gamma              float64 `json:"gamma"`
	Theta              float64 `json:"theta"`
	Vega               float64 `json:"vega"`
	Rho                float64 `json:"rho"`
}

// deribitTicker 行情（含 Greeks）
type deribitTicker struct {
	InstrumentName string  `json:"instrument_name"`
	MarkPrice      float64 `json:"mark_price"`
	Delta          float64 `json:"delta"`
	Gamma          float64 `json:"gamma"`
	Theta          float64 `json:"theta"`
	Vega           float64 `json:"vega"`
}

// Exchange 实现 Adapter
func (d *DeribitAdapter) Exchange() string { return "deribit" }

// FetchPositions 拉取期权 Put 仓位
func (d *DeribitAdapter) FetchPositions(ctx context.Context, symbol string) ([]OptionHedgePosition, error) {
	if d.apiKey == "" || d.secretKey == "" {
		return nil, fmt.Errorf("deribit: api credentials required")
	}
	if err := d.ensureAuth(ctx); err != nil {
		return nil, fmt.Errorf("deribit auth: %w", err)
	}

	currency := "BTC"
	if strings.HasPrefix(strings.ToUpper(symbol), "ETH") {
		currency = "ETH"
	}

	positions, err := d.getPositions(ctx, currency)
	if err != nil {
		return nil, err
	}

	var out []OptionHedgePosition
	now := time.Now()
	for _, p := range positions {
		if !isPutInstrument(p.InstrumentName) {
			continue
		}
		delta := p.Delta
		if p.Delta == 0 {
			ticker, _ := d.getTicker(ctx, p.InstrumentName)
			if ticker != nil {
				delta = ticker.Delta
			}
		}
		strike, expiry := parseDeribitInstrument(p.InstrumentName)
		pos := OptionHedgePosition{
			Exchange:   "deribit",
			Symbol:     symbol,
			Instrument: p.InstrumentName,
			Right:      "PUT",
			Strike:     strike,
			Expiry:     expiry,
			Qty:        p.Size,
			MarkPrice:  p.MarkPrice,
			Delta:      delta,
			Vega:       p.Vega,
			Theta:      p.Theta,
			Premium:    p.AveragePrice * p.Size,
			Source:     "api",
			UpdatedAt:  now,
		}
		if pos.Qty != 0 {
			out = append(out, pos)
		}
	}
	return out, nil
}

func (d *DeribitAdapter) ensureAuth(ctx context.Context) error {
	if d.token != "" {
		return nil
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := timestamp
	data := ""
	signStr := timestamp + "\n" + nonce + "\n" + data
	h := hmac.New(sha256.New, []byte(d.secretKey))
	h.Write([]byte(signStr))
	signature := hex.EncodeToString(h.Sum(nil))

	params := map[string]interface{}{
		"grant_type": "client_signature",
		"client_id":  d.apiKey,
		"timestamp":  timestamp,
		"signature":  signature,
		"nonce":      nonce,
		"data":       data,
	}
	paramBytes, _ := json.Marshal(params)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"public/auth","params":%s}`, string(paramBytes))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/v2/public/auth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Result struct {
			AccessToken string `json:"access_token"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("auth error: %s", r.Error.Message)
	}
	d.token = r.Result.AccessToken
	logger.Info("Deribit option adapter authenticated")
	return nil
}

func (d *DeribitAdapter) getPositions(ctx context.Context, currency string) ([]deribitPosition, error) {
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"private/get_positions","params":{"currency":"%s","kind":"option"}}`, currency)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/v2/private/get_positions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Result []deribitPosition `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, fmt.Errorf("get_positions: %s", r.Error.Message)
	}
	return r.Result, nil
}

func (d *DeribitAdapter) getTicker(ctx context.Context, instrument string) (*deribitTicker, error) {
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"public/ticker","params":{"instrument_name":"%s"}}`, instrument)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/v2/public/ticker", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Result deribitTicker `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, fmt.Errorf("ticker: %s", r.Error.Message)
	}
	return &r.Result, nil
}

func isPutInstrument(name string) bool {
	return strings.HasSuffix(strings.ToUpper(name), "-P")
}

func parseDeribitInstrument(name string) (strike float64, expiry time.Time) {
	// BTC-28MAR25-90000-P -> strike 90000, expiry 2025-03-28
	parts := strings.Split(name, "-")
	if len(parts) < 4 {
		return 0, time.Time{}
	}
	strike, _ = strconv.ParseFloat(parts[2], 64)
	expStr := parts[1]
	if len(expStr) >= 7 {
		// 28MAR25 -> 28Mar25 for Parse
		month := expStr[2:5]
		if len(month) == 3 {
			month = strings.ToUpper(month[:1]) + strings.ToLower(month[1:])
		}
		expStrNorm := expStr[:2] + month + expStr[5:]
		t, _ := time.Parse("02Jan06", expStrNorm)
		if !t.IsZero() {
			expiry = t
		}
	}
	return strike, expiry
}
