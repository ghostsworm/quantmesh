package safety

import (
	"context"
	"quantmesh/exchange"
	"testing"
)

// MockExchange 模拟交易所实现
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
func (m *MockExchange) GetQuoteAsset() string { return m.QuoteAsset }
func (m *MockExchange) GetPriceDecimals() int { return m.PriceDecimals }

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
			name: "余额不足",
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
			name: "杠杆过高",
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
			name: "已有持仓跳过检查",
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
			name: "利润无法覆盖手续费",
			mockEx: &MockExchange{
				Name: "Binance",
				Account: &exchange.Account{
					AvailableBalance: 3000.0,
					AccountLeverage:  10,
				},
				Positions:  []*exchange.Position{},
				QuoteAsset: "USDT",
			},
			// 修改参数使得利润过低
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pInterval := priceInterval
			fRate := feeRate
			if tt.name == "利润无法覆盖手续费" {
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

