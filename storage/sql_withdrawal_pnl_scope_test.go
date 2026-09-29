package storage

import (
	"math"
	"testing"
	"time"
)

func TestGetRealizedPnLForWithdrawalIsolatesFundingAccountMarketAndSymbol(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-pnl.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	trades := []Trade{
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 900, Fee: 2, FeeAsset: "USDT", CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", Symbol: "ETHUSDT", PnL: 150, CreatedAt: now},
		{Exchange: "bybit", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 900, CreatedAt: now},
		{Exchange: "binance", MarketType: "spot", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 800, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-b", Symbol: "BTCUSDT", PnL: 700, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 600, CreatedAt: now},
		{Exchange: "binance", MarketType: "", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 500, CreatedAt: now},
	}
	for i := range trades {
		if err := st.SaveTrade(&trades[i]); err != nil {
			t.Fatalf("save trade %d: %v", i, err)
		}
	}
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFundingIncomeCoverage("binance", "ETHUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT"} {
		if err := st.AdvanceOrderFillCoverage("binance", "futures", symbol, "scope-a", start, end); err != nil {
			t.Fatal(err)
		}
	}
	realizedBTC, realizedETH := 100.0, 50.0
	fills := []OrderFill{
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "btc-execution", OrderID: 501, Side: "SELL", Price: 100, Quantity: 1, Commission: 2, CommissionAsset: "USDT", RealizedPnL: &realizedBTC, TradeTime: now},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "ETHUSDT", TradeID: "eth-execution", OrderID: 502, Side: "SELL", Price: 100, Quantity: 1, RealizedPnL: &realizedETH, TradeTime: now},
	}
	for i := range fills {
		if err := st.SaveOrderFill(&fills[i]); err != nil {
			t.Fatal(err)
		}
	}
	funding := []FundingPayment{
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 101, Income: -5, Asset: "USDT", TradeTime: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-b", IncomeType: "FUNDING_FEE", TransactionID: 102, Income: -500, Asset: "USDT", TradeTime: now},
		{Exchange: "bybit", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 103, Income: -500, Asset: "USDT", TradeTime: now},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "spot", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 104, Income: -500, Asset: "USDT", TradeTime: now},
		{Exchange: "binance", Symbol: "ETHUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 105, Income: -500, Asset: "USDT", TradeTime: now},
	}
	for i := range funding {
		if err := st.SaveFundingPayment(&funding[i]); err != nil {
			t.Fatalf("save funding payment %d: %v", i, err)
		}
	}

	got, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-93) > 1e-9 {
		t.Fatalf("exchange execution PnL with scoped funding=%v, want 93", got)
	}
	ethPnL, err := st.GetRealizedPnLForWithdrawal("binance", "ETHUSDT", "scope-a", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if ethPnL != -450 {
		t.Fatalf("ETH PnL=%v, want -450 including only ETH funding", ethPnL)
	}
	// Profit windows are (since, end]. The exact previous boundary must not be
	// counted again by the next automatic-withdrawal run.
	boundaryPnL, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", now, end)
	if err != nil {
		t.Fatal(err)
	}
	if boundaryPnL != 0 {
		t.Fatalf("PnL at exclusive lower boundary was counted again: %v", boundaryPnL)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("missing exchange identity must fail closed")
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "", start, end); err == nil {
		t.Fatal("missing account scope must fail closed")
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "", "scope-a", start, end); err == nil {
		t.Fatal("account-wide rule without exact symbol must fail closed")
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", end, end); err == nil {
		t.Fatal("empty withdrawal interval must fail closed")
	}

	unknownAsset := FundingPayment{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 106, Income: 0.1, Asset: "BTC", TradeTime: now}
	if err := st.SaveFundingPayment(&unknownAsset); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("unvalued funding asset must block automatic withdrawal")
	}
}

func TestGetRealizedPnLForWithdrawalRequiresFreshFundingCoverage(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-coverage.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", now.Add(-30*time.Minute), end); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("coverage beginning after withdrawal window must fail closed")
	}
}

func TestGetRealizedPnLForWithdrawalRequiresExecutionCoverage(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-execution-coverage.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("missing execution-history coverage must block automatic withdrawal")
	}
}

func TestGetRealizedPnLForWithdrawalRejectsNonUSDTTradeFees(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-fee-denomination.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	trade := Trade{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 10, Fee: 0.01, FeeAsset: "BNB", CreatedAt: now}
	if err := st.SaveTrade(&trade); err != nil {
		t.Fatal(err)
	}
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	realized := 10.0
	fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "non-usdt-fee", OrderID: 503, Side: "SELL", Price: 10, Quantity: 1, Commission: 0.01, CommissionAsset: "BNB", RealizedPnL: &realized, TradeTime: now}
	if err := st.SaveOrderFill(&fill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("non-USDT execution fee must block automatic withdrawal")
	}
}

func TestGetRealizedPnLForWithdrawalRejectsUnknownExecutionPnL(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-unknown-execution-pnl.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "unknown-pnl", OrderID: 504, Side: "SELL", Price: 100, Quantity: 1, CommissionAsset: "USDT", TradeTime: now}
	if err := st.SaveOrderFill(&fill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("missing authoritative execution PnL must block automatic withdrawal")
	}
}

func TestGetRealizedPnLForWithdrawalBlocksPendingFeeCorrections(t *testing.T) {
	tests := []struct {
		name    string
		account string
		legacy  bool
		wantErr bool
	}{
		{name: "same account across bots", account: "scope-a", wantErr: true},
		{name: "different account", account: "scope-b", wantErr: false},
		{name: "legacy event without account scope", legacy: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := NewSQLStorage(t.TempDir() + "/withdrawal-fee-correction.db")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			now := time.Now().UTC()
			start, end := now.Add(-time.Minute), now.Add(time.Minute)
			if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
				t.Fatal(err)
			}
			if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
				t.Fatal(err)
			}
			realized := 25.0
			fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "covered-execution", OrderID: 601, Side: "SELL", Price: 100, Quantity: 1, CommissionAsset: "USDT", RealizedPnL: &realized, TradeTime: now}
			if err := st.SaveOrderFill(&fill); err != nil {
				t.Fatal(err)
			}
			if tt.legacy {
				if err := st.SaveEvent("trade_fee_correction", map[string]interface{}{"bot_id": "other-bot", "exchange": "binance", "symbol": "BTCUSDT", "order_id": 602}); err != nil {
					t.Fatal(err)
				}
			} else {
				correction := TradeFeeCorrection{CorrectionID: "correction-" + tt.account, BotID: "another-bot", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", AccountScope: tt.account, OrderID: 602, Leg: "open", Side: "BUY", Fee: 0.1, FeeAsset: "USDT", ExecutedQty: 1, Reason: "late exchange fee evidence"}
				if err := st.SaveTradeFeeCorrection(&correction); err != nil {
					t.Fatal(err)
				}
			}
			_, err = st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end)
			if tt.wantErr && err == nil {
				t.Fatal("pending fee correction must block withdrawal")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("correction in another account scope must not block withdrawal: %v", err)
			}
		})
	}
}

func TestFundingIncomeCoverageReplacesSnapshotInsteadOfBridgingOutage(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/funding-coverage-gap.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", now.Add(-72*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", now.Add(-24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	covered, err := st.HasFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", now.Add(-60*time.Hour), now.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if covered {
		t.Fatal("successful snapshots must not be merged across an unverified outage")
	}
	covered, err = st.HasFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", now.Add(-23*time.Hour), now)
	if err != nil || !covered {
		t.Fatalf("latest interval should be covered: covered=%v err=%v", covered, err)
	}
}
