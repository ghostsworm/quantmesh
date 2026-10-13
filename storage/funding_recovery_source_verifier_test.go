package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
)

func TestStorageServiceInstallsImmutableTrustedSourceVerifier(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "sqlite", filepath.Join(t.TempDir(), "source-verifier.db")
	service, err := NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	store := service.GetStorage().(*SQLStorage)
	verifier := store.recoverySourceVerifier()
	if _, ok := verifier.(productionRecoverySourceWindowVerifier); !ok {
		t.Fatalf("production verifier = %T, want installed trusted verifier", verifier)
	}
	if err := store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err == nil {
		t.Fatal("runtime verifier replacement was accepted")
	}

	from := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	event := accounting.RecoveryEvent{
		ID: "funding-event-1", Source: accounting.RecoverySourceBinanceFuturesIncomeV1,
		Exchange: "binance", Market: "futures", AccountScope: fmt.Sprintf("%064x", 11), Symbol: "BTCUSDT",
		Kind: accounting.RecoveryFundingEvent, ExternalID: "income-123", EpisodeID: "episode-source-verifier",
		Asset: "USDT", Amount: "-0.012345678901234567", SourceRevision: "123", SourceHash: strings.Repeat("a", 64),
		ObservedAt: from.Add(time.Minute),
	}
	window := accounting.RecoverySourceWindow{
		Source: event.Source, Exchange: event.Exchange, Market: event.Market, AccountScope: event.AccountScope, Symbol: event.Symbol,
		Kind: event.Kind, Asset: event.Asset, From: from, Through: from.Add(time.Hour), Rows: 1, TotalRows: 1,
		CompletionMarker: accounting.RecoverySourceWindowCompleteV1, CollectorEvidenceHash: strings.Repeat("b", 64), InitialFrom: from,
		HistoricalEvidenceHash: strings.Repeat("c", 64), EvidenceHash: strings.Repeat("0", 64),
	}
	window.EvidenceHash, err = accounting.RecoverySourceWindowEvidenceHash(window, []accounting.RecoveryEvent{event})
	if err != nil {
		t.Fatal("compute source window digest:", err)
	}
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), window, []accounting.RecoveryEvent{event}); err != nil {
		t.Fatalf("valid fully-bound source evidence rejected: %v", err)
	}
	missingCollectorProof := window
	missingCollectorProof.CollectorEvidenceHash = ""
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), missingCollectorProof, []accounting.RecoveryEvent{event}); err == nil {
		t.Fatal("Binance source window without the stable scan digest passed verification")
	}
	missingHistoryProof := window
	missingHistoryProof.HistoricalEvidenceHash = ""
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), missingHistoryProof, []accounting.RecoveryEvent{event}); err == nil {
		t.Fatal("Binance source window without cumulative history proof passed verification")
	}
	mutated := event
	mutated.Amount = "-99"
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), window, []accounting.RecoveryEvent{mutated}); err == nil {
		t.Fatal("tampered source row passed the trusted verifier")
	}
	wrongSymbol := window
	wrongSymbol.Symbol = "ETHUSDT"
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), wrongSymbol, []accounting.RecoveryEvent{event}); err == nil {
		t.Fatal("source window with a different symbol passed verification")
	}
	unknownSource := window
	unknownSource.Source = "unregistered_source"
	if err := verifier.VerifyRecoverySourceWindow(t.Context(), unknownSource, []accounting.RecoveryEvent{event}); err == nil {
		t.Fatal("unregistered source passed production verification")
	}
}
