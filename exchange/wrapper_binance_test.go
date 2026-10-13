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

func TestBinanceFuturesHistoryWindowMatchesVenueLimit(t *testing.T) {
	wrapper := &binanceWrapper{}
	if got, want := wrapper.MaxOrderHistoryRange(), 7*24*time.Hour-time.Second; got != want {
		t.Fatalf("MaxOrderHistoryRange() = %s, want %s", got, want)
	}
}

func TestBinanceOrderFillToExchangePreservesRawEvidenceAndKnownZeroValues(t *testing.T) {
	got := binanceOrderFillToExchange(&binance.OrderFill{
		OrderID: 17, TradeID: "trade-23", Symbol: "BTCUSDT", Side: binance.SideBuy,
		Price: 123.45, PriceRaw: "123.4500",
		Quantity: 0.0067, QuantityRaw: "0.006700",
		QuoteQuantity: 0.827115, QuoteQuantityRaw: "0.82711500",
		Commission: 0, CommissionRaw: "0.00000000", CommissionKnown: true, CommissionAsset: "BNB",
		CommissionQuote: 0, CommissionQuoteRate: 0, CommissionQuoteKnown: true,
		RealizedPnL: 0, RealizedPnLRaw: "0.00000000", RealizedPnLKnown: true,
	})
	if got == nil {
		t.Fatal("binanceOrderFillToExchange() = nil, want mapped fill")
	}

	if got.PriceRaw != "123.4500" || got.QuantityRaw != "0.006700" || got.QuoteQuantityRaw != "0.82711500" ||
		got.CommissionRaw != "0.00000000" || got.RealizedPnLRaw != "0.00000000" {
		t.Fatalf("raw decimal evidence was not preserved: %+v", got)
	}
	if got.Commission != 0 || got.RealizedPnL != 0 {
		t.Fatalf("explicit zero values changed: commission=%v realizedPnL=%v", got.Commission, got.RealizedPnL)
	}
	if !got.CommissionKnown || !got.RealizedPnLKnown || !got.CommissionQuoteKnown {
		t.Fatalf("known flags were not preserved: commission=%t realizedPnL=%t commissionQuote=%t",
			got.CommissionKnown, got.RealizedPnLKnown, got.CommissionQuoteKnown)
	}
	if got.CommissionAsset != "BNB" {
		t.Fatalf("CommissionAsset=%q, want BNB", got.CommissionAsset)
	}
}

func TestBinanceBatchToExchangePreservesCompletenessRevisionAndRawOrderEvidence(t *testing.T) {
	var _ OrderFillBatchSource = (*binanceWrapper)(nil)
	got := binanceBatchToExchange(&binance.OrderFillBatch{
		OrderID: 17, ClientOrderID: "cid-17", Symbol: "BTCUSDT", Side: binance.SideBuy,
		TerminalStatus: binance.OrderStatusFilled, OrderUpdateTime: 1790503200000, FillHistoryComplete: true,
		ExecutedQuantityRaw: "0.006700000000000001", SourceRevision: "sha256:abc123", Complete: true,
		FeesComplete: false,
		Fills: []*binance.OrderFill{{OrderID: 17, TradeID: "23", Symbol: "BTCUSDT", Side: binance.SideBuy,
			PriceRaw: "123.4500", QuantityRaw: "0.006700000000000001", QuoteQuantityRaw: "0.82711500",
			CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}},
	})
	if got == nil || !got.Complete || got.FeesComplete || !got.FillHistoryComplete || got.SourceRevision != "sha256:abc123" ||
		got.ExecutedQuantityRaw != "0.006700000000000001" || got.ClientOrderID != "cid-17" || got.OrderUpdateTime != 1790503200000 {
		t.Fatalf("batch metadata was not preserved: %+v", got)
	}
	if got.TerminalStatus != OrderStatusFilled || len(got.Fills) != 1 || got.Fills[0].QuantityRaw != "0.006700000000000001" ||
		got.Fills[0].CommissionRaw != "0" || !got.Fills[0].CommissionKnown {
		t.Fatalf("batch fill evidence was not preserved: %+v", got)
	}
}

func TestSetOrderFillBatchPnLAssetUsesAuthoritativeSettlementAsset(t *testing.T) {
	batch := &OrderFillBatch{Fills: []*OrderFill{
		{RealizedPnLKnown: true, RealizedPnLRaw: "-0.0003"},
		{RealizedPnLKnown: true, RealizedPnLRaw: "0", RealizedPnLAsset: "BUSD"},
		{RealizedPnLKnown: false},
	}}
	got := setOrderFillBatchPnLAsset(batch, "USDT")
	if got.Fills[0].RealizedPnLAsset != "USDT" || got.Fills[1].RealizedPnLAsset != "BUSD" || got.Fills[2].RealizedPnLAsset != "" {
		t.Fatalf("settlement asset mapping mismatch: %+v", got.Fills)
	}
}
