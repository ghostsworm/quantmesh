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
	realizedBTC, realizedETH, openFillPnL := 100.0, 50.0, 0.0
	fills := []OrderFill{
		{Exchange: "BINANCE", MarketType: "FUTURES", AccountScope: "scope-a", Account: "acct", Symbol: "btcusdt", TradeID: "btc-execution", OrderID: 501, Side: "SELL", Price: 100, Quantity: 1, Commission: 2, CommissionAsset: "USDT", RealizedPnL: &realizedBTC, RealizedPnLAsset: "USDT", TradeTime: now},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "BTCUSDT", TradeID: "btc-open-execution", OrderID: 503, Side: "BUY", Price: 100, Quantity: 1, Commission: 0.5, CommissionAsset: "USDT", RealizedPnL: &openFillPnL, RealizedPnLAsset: "USDT", TradeTime: now},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Symbol: "ETHUSDT", TradeID: "eth-execution", OrderID: 502, Side: "SELL", Price: 100, Quantity: 1, CommissionAsset: "USDT", RealizedPnL: &realizedETH, RealizedPnLAsset: "USDT", TradeTime: now},
	}
	for i := range fills {
		if err := st.SaveOrderFill(&fills[i]); err != nil {
			t.Fatal(err)
		}
	}
	funding := []FundingPayment{
		{Exchange: "BINANCE", Symbol: "btcusdt", Account: "acct", MarketType: "FUTURES", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 101, Income: -5, Asset: "USDT", TradeTime: now},
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
	if math.Abs(got-92.5) > 1e-9 {
		t.Fatalf("case-normalized execution PnL and scoped funding=%v, want 92.5", got)
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

func TestGetRealizedPnLForWithdrawalRejectsUnidentifiedLegacyFunding(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-unidentified-funding.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO funding_payments
		(exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time, identity_key)
		VALUES ('binance', 'BTCUSDT', 'legacy-account', 'futures', 'scope-a', 'FUNDING_FEE', -75, 'USDT', 77, ?, NULL)`, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("withdrawal must reject a legacy negative funding payment with no stable identity")
	}
}

func TestGetRealizedPnLForWithdrawalRejectsUnownedLegacyExecution(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-unowned-fill.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO order_fills
		(exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side, price, quantity,
		 quote_quantity, commission, commission_asset, commission_quote, commission_quote_rate, commission_quote_known,
		 realized_pnl, realized_pnl_asset, trade_time)
		VALUES ('binance', 'futures', '', 'legacy-account', '', 'BTCUSDT', 'legacy-unowned-fill', 77, 'SELL', 100, 1,
		 100, 0, 'USDT', 0, 0, 1, -75, 'USDT', ?)`, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("withdrawal must reject negative execution PnL without verified account ownership")
	}
}

func TestGetRealizedPnLForWithdrawalRejectsUnknownMarketTypeRows(t *testing.T) {
	t.Run("funding", func(t *testing.T) {
		st, err := NewSQLStorage(t.TempDir() + "/withdrawal-unknown-funding-market.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		now := time.Now().UTC()
		start, end := now.Add(-time.Hour), now
		if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
			t.Fatal(err)
		}
		if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT INTO funding_payments
			(exchange, symbol, account, market_type, account_scope, income_type, income, asset, transaction_id, trade_time, identity_key)
			VALUES ('binance', 'BTCUSDT', 'acct', 'perpetual', 'scope-a', 'FUNDING_FEE', -75, 'USDT', 88, ?, 'legacy-market-type-88')`, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
			t.Fatal("withdrawal must reject funding rows with an unrecognized market type")
		}
	})

	t.Run("execution", func(t *testing.T) {
		st, err := NewSQLStorage(t.TempDir() + "/withdrawal-unknown-execution-market.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		now := time.Now().UTC()
		start, end := now.Add(-time.Hour), now
		if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", start, end); err != nil {
			t.Fatal(err)
		}
		if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", "scope-a", start, end); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT INTO order_fills
			(exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side, price, quantity,
			 quote_quantity, commission, commission_asset, commission_quote, commission_quote_rate, commission_quote_known,
			 realized_pnl, realized_pnl_asset, trade_time)
			VALUES ('binance', 'perpetual', 'scope-a', 'acct', '', 'BTCUSDT', 'unknown-market-fill', 89, 'SELL', 100, 1,
			 100, 0, 'USDT', 0, 0, 1, -75, 'USDT', ?)`, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
			t.Fatal("withdrawal must reject executions with an unrecognized market type")
		}
	})
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
	fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "non-usdt-fee", OrderID: 503, Side: "SELL", Price: 10, Quantity: 1, Commission: 0.01, CommissionAsset: "BNB", RealizedPnL: &realized, RealizedPnLAsset: "USDT", TradeTime: now}
	if err := st.SaveOrderFill(&fill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("non-USDT execution fee must block automatic withdrawal")
	}
}

func TestGetRealizedPnLForWithdrawalRejectsMissingFeeCurrencyEvenWhenFeeIsZero(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-missing-fee-currency.db")
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
	realized := 10.0
	fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "missing-fee-currency", OrderID: 506,
		Side: "SELL", Price: 100, Quantity: 1, Commission: 0, RealizedPnL: &realized, RealizedPnLAsset: "USDT", TradeTime: now}
	if err := st.SaveOrderFill(&fill); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
		t.Fatal("missing fee currency must block withdrawal even when a malformed adapter reports zero commission")
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

func TestGetRealizedPnLForWithdrawalRejectsUnknownOrNonUSDTExecutionPnLAsset(t *testing.T) {
	for _, asset := range []string{"", "BTC"} {
		t.Run("asset="+asset, func(t *testing.T) {
			st, err := NewSQLStorage(t.TempDir() + "/withdrawal-pnl-asset.db")
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
			realized := 10.0
			fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "pnl-asset", OrderID: 505, Side: "SELL", Price: 100, Quantity: 1, CommissionAsset: "USDT", RealizedPnL: &realized, RealizedPnLAsset: asset, TradeTime: now}
			if err := st.SaveOrderFill(&fill); err != nil {
				t.Fatal(err)
			}
			if _, err := st.GetRealizedPnLForWithdrawal("binance", "BTCUSDT", "scope-a", start, end); err == nil {
				t.Fatalf("realized PnL asset %q must block USDT withdrawal computation", asset)
			}
		})
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
			fill := OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", TradeID: "covered-execution", OrderID: 601, Side: "SELL", Price: 100, Quantity: 1, CommissionAsset: "USDT", RealizedPnL: &realized, RealizedPnLAsset: "USDT", TradeTime: now}
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

func TestFundingIncomeCoverageMergesOverlapsAndRejectsOutOfOrderRegression(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/funding-coverage-order.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	mark := func(from, through time.Time) {
		t.Helper()
		if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a", from, through); err != nil {
			t.Fatal(err)
		}
	}
	mark(base.Add(-2*time.Hour), base)
	mark(base.Add(-time.Hour), base.Add(time.Hour))
	mark(base.Add(-3*time.Hour), base.Add(-time.Minute)) // Older request finishes last.
	gotFrom, gotThrough, err := st.GetFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a")
	if err != nil || !gotFrom.Equal(base.Add(-2*time.Hour)) || !gotThrough.Equal(base.Add(time.Hour)) {
		t.Fatalf("coverage should merge overlap but ignore an older completion: interval=(%v,%v), err=%v", gotFrom, gotThrough, err)
	}
	// A newer disjoint interval replaces the snapshot instead of bridging the gap.
	mark(base.Add(3*time.Hour), base.Add(4*time.Hour))
	gotFrom, gotThrough, err = st.GetFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-a")
	if err != nil || !gotFrom.Equal(base.Add(3*time.Hour)) || !gotThrough.Equal(base.Add(4*time.Hour)) {
		t.Fatalf("new disjoint interval must replace without bridging: interval=(%v,%v), err=%v", gotFrom, gotThrough, err)
	}
}
