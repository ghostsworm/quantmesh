package exchange

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/config"
)

// FundingCarryPermissionResult 資金費套利預檢結果
type FundingCarryPermissionResult struct {
	OK              bool     `json:"ok"`
	Exchange        string   `json:"exchange"`
	Missing         []string `json:"missing,omitempty"`
	FuturesOK       bool     `json:"futures_ok"`
	SpotOK          bool     `json:"spot_ok"`
	FuturesMessage  string   `json:"futures_message,omitempty"`
	SpotMessage     string   `json:"spot_message,omitempty"`
}

// CheckFundingCarrySetup 檢查是否具備現貨+UM 合約交易能力（第一版：Binance）
func CheckFundingCarrySetup(ctx context.Context, cfg *config.Config, exchangeName, symbol string) (*FundingCarryPermissionResult, error) {
	res := &FundingCarryPermissionResult{Exchange: exchangeName}
	if cfg == nil {
		return nil, fmt.Errorf("config 為空")
	}
	exName := strings.ToLower(strings.TrimSpace(exchangeName))
	if exName != "binance" {
		return nil, fmt.Errorf("%w: 資金費套利當前僅支援 binance", ErrNotImplemented)
	}

	futEx, err := NewExchange(cfg, exchangeName, symbol, "futures")
	if err != nil {
		return nil, fmt.Errorf("創建合約實例失敗: %w", err)
	}
	spotEx, err := NewExchange(cfg, exchangeName, symbol, "spot")
	if err != nil {
		return nil, fmt.Errorf("創建現貨實例失敗: %w", err)
	}

	if _, err := futEx.GetAccount(ctx); err != nil {
		res.FuturesMessage = fmt.Sprintf("合約帳戶不可用: %v", err)
	} else {
		res.FuturesOK = true
	}

	if _, err := spotEx.GetAccount(ctx); err != nil {
		res.SpotMessage = fmt.Sprintf("現貨帳戶不可用: %v", err)
	} else {
		res.SpotOK = true
	}

	if !res.FuturesOK {
		res.Missing = append(res.Missing, "futures")
	}
	if !res.SpotOK {
		res.Missing = append(res.Missing, "spot")
	}
	res.OK = res.FuturesOK && res.SpotOK
	return res, nil
}
