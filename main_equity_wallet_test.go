package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
	"quantmesh/storage"
)

type equityLedgerExchange struct {
	equityAccountExchange
	snapshot      accounting.Snapshot
	evidenceErr   error
	evidenceCalls int
	since         time.Time
	onEvidence    func()
}

func (e *equityLedgerExchange) ReadAccountEvidence(_ context.Context, since time.Time) (accounting.Snapshot, error) {
	e.evidenceCalls++
	e.since = since
	if e.onEvidence != nil {
		e.onEvidence()
	}
	return e.snapshot, e.evidenceErr
}

func runtimeWalletFixture(local time.Time, balance string, equity float64) accounting.Snapshot {
	return accounting.Snapshot{Currency: "USDT", Equity: equity, ObservedAt: local,
		Wallet: accounting.Wallet{Balance: balance, From: local.Add(-5 * time.Minute), Through: local.Add(-time.Millisecond), ObservedAt: local}}
}

func walletRuntimeFixture(account string, ex *equityLedgerExchange) *SymbolRuntime {
	return &SymbolRuntime{Exchange: ex, AccountScope: account, AccountMarketType: "futures"}
}

func TestRuntimeEquityWalletAggregatesAccountsAndPreservesCursors(t *testing.T) {
	now := time.Now().Add(-time.Second)
	first := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	second := &equityLedgerExchange{snapshot: runtimeWalletFixture(now.Add(time.Millisecond), "200", 200)}
	entry := accounting.Entry{ID: "shared-id", Kind: "fee", Currency: "USDT", Amount: "-1", At: now.Add(-time.Minute)}
	first.snapshot.Entries = []accounting.Entry{entry}
	second.snapshot.Entries = []accounting.Entry{entry}
	a, b := walletRuntimeFixture("a", first), walletRuntimeFixture("b", second)
	o, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{b, a, a})
	if err != nil || !o.CashFlowComplete || o.Equity != 1200 || len(o.Wallets) != 2 || len(o.Flows) != 2 || !o.ObservedAt.Equal(now) {
		t.Fatalf("aggregate=%+v err=%v", o, err)
	}
	if first.calls != 0 || second.calls != 0 || first.evidenceCalls != 1 || second.evidenceCalls != 1 {
		t.Fatal("cached/raw or duplicate account read")
	}
	if o.Flows[0].ID == o.Flows[1].ID || o.Flows[0].Account == o.Flows[1].Account || o.Flows[0].ExactAmount != "-1" {
		t.Fatal("account receipt identities collided")
	}
	cursors := make(map[string]time.Time)
	for key, wallet := range o.Wallets {
		cursors[key] = wallet.From
	}
	reordered, err := observeRuntimeEquityCursors(t.Context(), []*SymbolRuntime{a, b}, cursors)
	if err != nil || reordered.Scope != o.Scope || !first.since.Equal(first.snapshot.Wallet.From) || !second.since.Equal(second.snapshot.Wallet.From) {
		t.Fatalf("durable cursor mapping failed: %v", err)
	}
	if _, err := observeRuntimeEquityCursors(t.Context(), []*SymbolRuntime{a}, cursors); err == nil {
		t.Fatal("disappeared account silently removed")
	}
}

func TestRuntimeEquityRejectsMembershipChangeDuringAccountRead(t *testing.T) {
	now := time.Now().Add(-time.Second)
	first := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	firstRuntime := walletRuntimeFixture("account-a", first)
	secondRuntime := walletRuntimeFixture("account-b", &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "200", 200)})
	manager := &SymbolManager{botManager: &BotManager{runtimes: make(map[string]*BotRuntime)}}
	manager.botManager.AddRuntime(&BotRuntime{BotID: "bot-a", Inner: firstRuntime})
	added := false
	first.onEvidence = func() {
		if !added {
			added = true
			manager.botManager.AddRuntime(&BotRuntime{BotID: "bot-b", Inner: secondRuntime})
		}
	}

	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err == nil || observation.CashFlowComplete || observation.Equity != 0 {
		t.Fatalf("membership changed mid-sample but equity was published: observation=%+v err=%v", observation, err)
	}
}

func TestRuntimeEquityRequiresAllEnabledConfiguredAccounts(t *testing.T) {
	now := time.Now().Add(-time.Second)
	firstConfig := config.ExchangeConfig{APIKey: "account-a-key", Testnet: true}
	secondConfig := config.ExchangeConfig{APIKey: "account-b-key", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": firstConfig, "bitget": secondConfig},
		Bots: []config.BotConfig{
			{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
			{Exchange: "bitget", Symbol: "ETHUSDT", MarketType: "futures"},
		},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	runtime := walletRuntimeFixture(equityAccountScopeID("binance", firstConfig), &equityLedgerExchange{
		snapshot: runtimeWalletFixture(now, "1000", 1000),
	})
	manager.botManager.AddRuntime(&BotRuntime{BotID: "binance-btc", Inner: runtime})

	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err == nil || observation.Equity != 0 || observation.CashFlowComplete {
		t.Fatalf("missing configured account was silently omitted: observation=%+v err=%v", observation, err)
	}
}

func TestRuntimeEquityConfiguredScopeDeduplicatesBotsOnSameAccount(t *testing.T) {
	now := time.Now().Add(-time.Second)
	exchangeConfig := config.ExchangeConfig{APIKey: "shared-account-key", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig},
		Bots: []config.BotConfig{
			{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
			{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"},
		},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	runtime := walletRuntimeFixture(equityAccountScopeID("binance", exchangeConfig), &equityLedgerExchange{
		snapshot: runtimeWalletFixture(now, "1000", 1000),
	})
	manager.botManager.AddRuntime(&BotRuntime{BotID: "binance-btc", Inner: runtime})

	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err != nil || observation.Equity != 1000 || !observation.CashFlowComplete {
		t.Fatalf("Bot-level duplicates incorrectly expanded the account scope: observation=%+v err=%v", observation, err)
	}
}

func TestRuntimeEquityRejectsEnabledSpotScopeUntilValuationIsSupported(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "spot-account-key"}},
		Bots:      []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err == nil || observation.Equity != 0 || observation.CashFlowComplete {
		t.Fatalf("enabled Spot account scope was silently treated as Futures-only: observation=%+v err=%v", observation, err)
	}
}

func TestBuildEquityScopeSnapshotSupportsLegacyEnabledSymbols(t *testing.T) {
	exchangeConfig := config.ExchangeConfig{APIKey: "legacy-account-key", Testnet: false}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbols = []config.SymbolConfig{{Symbol: "BTCUSDT", MarketType: "futures"}}
	snapshot := buildEquityScopeSnapshot(cfg)
	_, _, want, err := runtimeEquityAccounts([]*SymbolRuntime{walletRuntimeFixture(equityAccountScopeID("binance", exchangeConfig), &equityLedgerExchange{})})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.configured || snapshot.err != "" || snapshot.scope != want {
		t.Fatalf("legacy Trading.Symbols scope=%+v want digest=%q", snapshot, want)
	}
}

func TestRuntimeEquityRejectsConfiguredScopeChangeDuringSampling(t *testing.T) {
	now := time.Now().Add(-time.Second)
	exchangeConfig := config.ExchangeConfig{APIKey: "account-a-key", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig},
		Bots:      []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	provider := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	runtime := walletRuntimeFixture(equityAccountScopeID("binance", exchangeConfig), provider)
	manager.botManager.AddRuntime(&BotRuntime{BotID: "binance-btc", Inner: runtime})
	updatedConfig := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig, "bitget": {APIKey: "account-b-key", Testnet: true}},
		Bots: []config.BotConfig{
			{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
			{Exchange: "bitget", Symbol: "ETHUSDT", MarketType: "futures"},
		},
	}
	provider.onEvidence = func() { manager.botManager.UpdateRuntimeTradingParams(updatedConfig) }

	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err == nil || observation.Equity != 0 || observation.CashFlowComplete {
		t.Fatalf("configuration changed during sampling but observation was published: observation=%+v err=%v", observation, err)
	}
}

func TestRuntimeEquityAllowsNonScopeConfigChangeDuringSampling(t *testing.T) {
	now := time.Now().Add(-time.Second)
	exchangeConfig := config.ExchangeConfig{APIKey: "stable-account-key", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig},
		Bots:      []config.BotConfig{{ID: "bot-a", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", Name: "before"}},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	provider := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	runtime := walletRuntimeFixture(equityAccountScopeID("binance", exchangeConfig), provider)
	manager.botManager.AddRuntime(&BotRuntime{BotID: "bot-a", Inner: runtime})
	updatedConfig := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": exchangeConfig},
		Bots:      []config.BotConfig{{ID: "bot-a", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", Name: "renamed"}},
	}
	provider.onEvidence = func() { manager.botManager.UpdateRuntimeTradingParams(updatedConfig) }

	observation, err := (&runtimeEquitySource{manager: manager}).ObserveAccountEquity(t.Context(), nil)
	if err != nil || observation.Equity != 1000 || !observation.CashFlowComplete {
		t.Fatalf("unrelated config update invalidated unchanged account scope: observation=%+v err=%v", observation, err)
	}
}

func TestRuntimeEquityWalletAggregatesCurrenciesAndValuesLedgerReceipts(t *testing.T) {
	now := time.Now().Add(-time.Second)
	snapshot := accounting.Snapshot{Currency: "USDT", Equity: 76000, ObservedAt: now,
		Wallets: map[string]accounting.Wallet{
			"USDT": {Balance: "10000", From: now.Add(-5 * time.Minute), Through: now.Add(-time.Millisecond), ObservedAt: now},
			"BTC":  {Balance: "1.1", From: now.Add(-5 * time.Minute), Through: now.Add(-time.Millisecond), ObservedAt: now},
		},
		Entries: []accounting.Entry{{ID: "deposit-btc", Kind: "deposit", Currency: "BTC", Amount: "0.1", ValuationRate: "60000", ValuationSource: "binance:BTCUSDT:historical", ValuationAt: now.Add(-time.Second), At: now.Add(-time.Second)}}}
	ex := &equityLedgerExchange{snapshot: snapshot}
	observation, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{walletRuntimeFixture("acct", ex)})
	if err != nil {
		t.Fatal(err)
	}
	if !observation.CashFlowComplete || len(observation.Wallets) != 2 || len(observation.Flows) != 1 || observation.Flows[0].Currency != "USDT" || observation.Flows[0].WalletCurrency != "BTC" || observation.Flows[0].Amount != 6000 {
		t.Fatalf("multi-currency observation=%+v", observation)
	}
	cursors := make(map[string]time.Time, len(observation.Wallets))
	for identity, wallet := range observation.Wallets {
		cursors[identity] = wallet.From
	}
	if _, err := observeRuntimeEquityCursors(t.Context(), []*SymbolRuntime{walletRuntimeFixture("acct", ex)}, cursors); err != nil {
		t.Fatalf("multi-currency durable cursors were not mapped to account: %v", err)
	}
}

func TestRuntimeEquityWalletFailuresDoNotReturnAdjustedPartialTotals(t *testing.T) {
	for _, name := range []string{"read_error", "currency", "nan", "capture_mismatch", "missing_id", "invalid_amount", "future", "missing_capture"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().Add(-time.Second)
			first := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
			second := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "200", 200)}
			switch name {
			case "read_error":
				second.evidenceErr = errors.New("fixture unavailable")
			case "currency":
				second.snapshot.Currency = "BNB"
			case "nan":
				second.snapshot.Equity = math.NaN()
			case "capture_mismatch":
				second.snapshot.Wallet.ObservedAt = now.Add(-time.Second)
			case "missing_id":
				second.snapshot.Entries = []accounting.Entry{{Amount: "-1"}}
			case "invalid_amount":
				second.snapshot.Entries = []accounting.Entry{{ID: "fee", Amount: "NaN"}}
			case "future":
				second.snapshot.ObservedAt = now.Add(time.Minute)
				second.snapshot.Wallet.ObservedAt = second.snapshot.ObservedAt
			case "missing_capture":
				second.snapshot.ObservedAt = time.Time{}
			}
			o, err := observeRuntimeEquity(t.Context(), []*SymbolRuntime{walletRuntimeFixture("a", first), walletRuntimeFixture("b", second)})
			if err == nil || o.CashFlowComplete {
				t.Fatal("partial or invalid evidence certified")
			}
		})
	}
}

func TestRuntimeEquityWalletMixedCapabilitiesRemainUnadjusted(t *testing.T) {
	now := time.Now().Add(-time.Second)
	first := &equityLedgerExchange{equityAccountExchange: equityAccountExchange{balance: 1000, asset: "USDT"}, snapshot: runtimeWalletFixture(now, "1000", 1000)}
	second := &equityAccountExchange{balance: 200, asset: "USDT"}
	runtimes := []*SymbolRuntime{walletRuntimeFixture("a", first), {Exchange: second, AccountScope: "b", AccountMarketType: "futures"}}
	o, err := observeRuntimeEquity(t.Context(), runtimes)
	if err != nil || o.CashFlowComplete || len(o.Wallets) != 0 || o.Equity != 1200 || first.evidenceCalls != 0 {
		t.Fatalf("mixed capability=%+v err=%v", o, err)
	}
}

func TestRuntimeEquityWalletHonorsCanceledRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Now().Add(-time.Second)
	first := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	o, err := observeRuntimeEquity(ctx, []*SymbolRuntime{walletRuntimeFixture("a", first)})
	if !errors.Is(err, context.Canceled) || o.CashFlowComplete {
		t.Fatal("canceled read certified equity")
	}
}

type runtimeWalletTestSource struct{ runtimes []*SymbolRuntime }

func (s *runtimeWalletTestSource) TotalEquity(ctx context.Context) (float64, error) {
	o, err := observeRuntimeEquity(ctx, s.runtimes)
	return o.Equity, err
}
func (s *runtimeWalletTestSource) ObserveAccountEquity(ctx context.Context, cursors map[string]time.Time) (risk.EquityObservation, error) {
	return observeRuntimeEquityCursors(ctx, s.runtimes, cursors)
}

type runtimeWalletTestSink struct {
	health risk.MetricsHealth
	snap   risk.MetricsSnapshot
}

func (s *runtimeWalletTestSink) UpdateMetrics(float64, float64, int) {}
func (s *runtimeWalletTestSink) MetricsResetMarks() risk.MetricsResetMarks {
	return risk.MetricsResetMarks{}
}
func (s *runtimeWalletTestSink) UpdateMetricsObservation(snap risk.MetricsSnapshot, health risk.MetricsHealth) {
	s.health = health
	if health.Available {
		s.snap = snap
	}
}

func TestRuntimeEquityWalletSQLiteRestartAndMissingLedgerHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "equity.db")
	db, err := storage.NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-10 * time.Second).Truncate(time.Millisecond)
	first := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	source := &runtimeWalletTestSource{runtimes: []*SymbolRuntime{walletRuntimeFixture("a", first)}}
	sink := &runtimeWalletTestSink{}
	options := risk.MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: &persistedEquityState{backend: db}, RequirePersistence: true, RequireCashFlowReconciliation: true}
	feeder := risk.NewMetricsFeeder(sink, nil, source, nil, options)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	from := first.snapshot.Wallet.From
	now = now.Add(time.Second)
	first.snapshot = runtimeWalletFixture(now, "1400", 1400)
	first.snapshot.Wallet.From = from
	entries := []accounting.Entry{
		{ID: "deposit", Kind: "transfer_in", Currency: "USDT", Amount: "500", At: now.Add(-2 * time.Millisecond)},
		{ID: "funding", Kind: "funding", Currency: "USDT", Amount: "-100", At: now.Add(-time.Millisecond)},
	}
	first.snapshot.Entries = entries
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sink.snap.MaxDrawdownPct != 10 || !sink.health.CashFlowAdjusted || !sink.health.Persisted {
		t.Fatalf("unverified metrics: %+v %+v", sink.snap, sink.health)
	}
	expectedSince := first.snapshot.Wallet.Through.Add(-5 * time.Minute)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	options.EquityStore = &persistedEquityState{backend: db}
	feeder = risk.NewMetricsFeeder(sink, nil, source, nil, options)
	now = now.Add(time.Second)
	first.snapshot = runtimeWalletFixture(now, "1399", 1399)
	first.snapshot.Wallet.From = from
	first.snapshot.Entries = entries // deliberately missing fee
	if _, err := feeder.Tick(t.Context()); err == nil || sink.health.Available || sink.snap.MaxDrawdownPct != 10 {
		t.Fatal("missing fee silently advanced risk state")
	}
	first.snapshot.Entries = append(entries, accounting.Entry{ID: "fee", Kind: "fee", Currency: "USDT", Amount: "-1", At: now.Add(-time.Millisecond)})
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if math.Abs(sink.snap.MaxDrawdownPct-10.1) > 1e-9 || !first.since.Equal(expectedSince) || !sink.health.Persisted {
		t.Fatalf("restart lost account cursor/high water: %+v", sink.snap)
	}
}
