package safety

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"quantmesh/exchange"
)

// MockExchange 模拟交易所實現
type MockExchange struct {
	exchange.IExchange
	Name             string
	Account          *exchange.Account
	Positions        []*exchange.Position
	QuoteAsset       string
	PriceDecimals    int
	QuantityDecimals int
}

func (m *MockExchange) GetName() string { return m.Name }
func (m *MockExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	return m.Account, nil
}
func (m *MockExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return m.Positions, nil
}
func (m *MockExchange) GetQuoteAsset() string   { return m.QuoteAsset }
func (m *MockExchange) GetPriceDecimals() int { return m.PriceDecimals }
func (m *MockExchange) GetMarketType() string { return "futures" }

func TestCheckAccountSafety(t *testing.T) {
	symbol := "BTCUSDT"
	currentPrice := 50000.0
	orderAmount := 30.0
	priceInterval := 100.0
	feeRate := 0.0002
	requiredPositions := 100
	priceDecimals := 2
	maxLeverage := 10

	tests := []struct {
		name      string
		mockEx    *MockExchange
		expectErr bool
	}{
		{
			name: "正常场景",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 3000.0,
					AccountLeverage:  10,
				},
				Positions:  []*exchange.Position{},
				QuoteAsset: "USDT",
			},
			expectErr: false,
		},
		{
			name: "餘額不足",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 100.0, // 3000 -> 100
					AccountLeverage:  10,
				},
				Positions:  []*exchange.Position{},
				QuoteAsset: "USDT",
			},
			expectErr: true,
		},
		{
			name: "杠杆過高",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 3000.0,
					AccountLeverage:  20, // 10 -> 20
				},
				Positions:  []*exchange.Position{},
				QuoteAsset: "USDT",
			},
			expectErr: true,
		},
		{
			name: "已有持倉跳過检查",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 3000.0,
					AccountLeverage:  10,
				},
				Positions: []*exchange.Position{
					{Symbol: symbol, Size: 0.1, Leverage: 10},
				},
				QuoteAsset: "USDT",
			},
			expectErr: false,
		},
		{
			name: "利润無法覆盖手续费",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 3000.0,
					AccountLeverage:  10,
				},
				Positions:  []*exchange.Position{},
				QuoteAsset: "USDT",
			},
			// 修改参數使得利润過低
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pInterval := priceInterval
			fRate := feeRate
			if tt.name == "利润無法覆盖手续费" {
				pInterval = 0.01 // 极小的利润
				fRate = 0.1      // 极高的手续费
			}

			err := CheckAccountSafety(tt.mockEx, symbol, currentPrice, orderAmount, pInterval, fRate, requiredPositions, priceDecimals, maxLeverage)
			if (err != nil) != tt.expectErr {
				t.Errorf("CheckAccountSafety() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestCheckAccountSafetyInsufficientBalanceMessageIncludesAmounts(t *testing.T) {
	symbol := "BTCUSDT"
	ex := &MockExchange{
		Name: "Binance",
		Account: &exchange.Account{
			AvailableBalance: 1084.0,
			AccountLeverage:  1,
		},
		Positions:  []*exchange.Position{},
		QuoteAsset: "USDT",
	}
	// 250 USDT/倉 × 5 倉 / 1x = 1250 > 1084
	err := CheckAccountSafety(ex, symbol, 50000.0, 250.0, 100.0, 0.0002, 5, 2, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	if !strings.Contains(s, "當前可用餘額") || !strings.Contains(s, "1084.00") {
		t.Fatalf("expected 當前可用餘額 with balance, got: %s", s)
	}
	if !strings.Contains(s, "滿足 5 倉") || !strings.Contains(s, "1250.00") {
		t.Fatalf("expected 滿足 5 倉 and required ~1250 USDT, got: %s", s)
	}
}

func TestAccountAPIErrorHint(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		hasHint bool
	}{
		{"空響應", fmt.Errorf("<APIError> rsp= "), true},
		{"空響應無空格", fmt.Errorf("<APIError> rsp="), true},
		{"有 code 無 hint", fmt.Errorf("<APIError> code=-2015, msg=Invalid API-key"), false},
		{"普通錯誤無 hint", fmt.Errorf("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint := accountAPIErrorHint(tt.err)
			if (len(hint) > 0) != tt.hasHint {
				t.Errorf("accountAPIErrorHint() hint=%q, hasHint=%v", hint, tt.hasHint)
			}
		})
	}
}
