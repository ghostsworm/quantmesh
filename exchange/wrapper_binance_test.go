package exchange

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange/binance"
)

type binanceHistoryFixture struct {
	rows  []*binance.UserTrade
	asset string
}

func (f *binanceHistoryFixture) GetUserTradesFromID(context.Context, string, int64, int64, int64, int) ([]*binance.UserTrade, error) {
	return f.rows, nil
}

func (f *binanceHistoryFixture) GetQuoteAsset() string { return f.asset }

func TestBinanceOrderHistoryUsesSettlementAssetNotCommissionAsset(t *testing.T) {
	tradeTime := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fixture := &binanceHistoryFixture{
		asset: "USDT",
		rows: []*binance.UserTrade{{
			ID: 1, OrderID: 2, Symbol: "BTCUSDT", Side: binance.SideSell,
			Price: 100, Quantity: 1, QuoteQuantity: 100, Commission: 0.01,
			CommissionAsset: "BNB", RealizedPnL: 5, Time: tradeTime,
		}},
	}

	page, err := binanceOrderHistoryPage(context.Background(), fixture, "BTCUSDT", tradeTime.Add(-time.Minute).UnixMilli(), tradeTime.Add(time.Minute).UnixMilli(), "", 10)
	if err != nil {
		t.Fatalf("binanceOrderHistoryPage: %v", err)
	}
	if len(page.Fills) != 1 {
		t.Fatalf("fills=%d, want 1", len(page.Fills))
	}
	fill := page.Fills[0]
	if fill.CommissionAsset != "BNB" || fill.RealizedPnLAsset != "USDT" || fill.RealizedPnL != 5 {
		t.Fatalf("fee/PnL assets were conflated: %+v", fill)
	}
}

func TestBinanceOrderHistoryLeavesPnLAssetUnknownWithoutSettlementMetadata(t *testing.T) {
	tradeTime := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fixture := &binanceHistoryFixture{
		rows: []*binance.UserTrade{{
			ID: 1, OrderID: 2, Symbol: "BTCUSDT", Side: binance.SideSell,
			Price: 100, Quantity: 1, QuoteQuantity: 100, RealizedPnL: 5, Time: tradeTime,
		}},
	}

	page, err := binanceOrderHistoryPage(context.Background(), fixture, "BTCUSDT", tradeTime.Add(-time.Minute).UnixMilli(), tradeTime.Add(time.Minute).UnixMilli(), "", 10)
	if err != nil {
		t.Fatalf("binanceOrderHistoryPage: %v", err)
	}
	if got := page.Fills[0].RealizedPnLAsset; got != "" {
		t.Fatalf("PnL asset=%q, want unknown without settlement metadata", got)
	}
}
