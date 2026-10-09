package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
	"quantmesh/storage"
)

type failRetiredResetAuditOnceBackend struct {
	backend riskCheckpointBackend
	armed   atomic.Bool
}

func (b *failRetiredResetAuditOnceBackend) LoadRiskCheckpoint(ctx context.Context, key string) ([]byte, int64, error) {
	return b.backend.LoadRiskCheckpoint(ctx, key)
}

func (b *failRetiredResetAuditOnceBackend) SaveRiskCheckpoint(ctx context.Context, key string, expected int64, payload []byte) error {
	var state retiredEquityAccountsState
	if b.armed.Load() && json.Unmarshal(payload, &state) == nil && state.PendingReset == nil && len(state.ResetHistory) > 0 {
		if b.armed.CompareAndSwap(true, false) {
			return errors.New("injected reset-audit persistence failure")
		}
	}
	return b.backend.SaveRiskCheckpoint(ctx, key, expected, payload)
}

func TestMySQLRetiredResetRecoversAfterBaselineBeforeAuditAcrossRestart(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to disposable loopback MySQL")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid disposable MySQL fixture DSN")
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || host != "127.0.0.1" || cfg.User != "root" || cfg.Passwd != "" ||
		(cfg.DBName != "quantmesh_test" && !strings.HasPrefix(cfg.DBName, "quantmesh_audit_")) {
		t.Fatal("retired-account reset test requires credential-free loopback disposable MySQL")
	}

	cleanupDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanupDB.Close() })
	if err := cleanupDB.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	backend, err := storage.NewMySQLStorage(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := backend.MigrateOpeningPauseHolders(t.Context()); err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("retired-reset-mysql-test-%d-", time.Now().UnixNano())
	archivePrefix, equityPrefix := prefix+"archive-", prefix+"equity-"
	instanceIdentity := prefix + "pause-owner"
	ownerDigest := sha256.Sum256([]byte(instanceIdentity))
	ownerID := hex.EncodeToString(ownerDigest[:16])
	t.Cleanup(func() {
		_, _ = cleanupDB.Exec(`DELETE FROM risk_checkpoints WHERE checkpoint_key LIKE ?`, prefix+"%")
		_, _ = cleanupDB.Exec(`DELETE FROM opening_pause_owners WHERE owner_id = ? AND source = ?`, ownerID, retiredEquityAccountPauseSource)
	})

	archiveBackend := &failRetiredResetAuditOnceBackend{backend: namespacedRiskCheckpointBackend{backend: backend, prefix: archivePrefix}}
	archiveBackend.armed.Store(true)
	archive := &retiredEquityAccountsStore{backend: archiveBackend, key: []byte("0123456789abcdef0123456789abcdef")}
	oldCredentials := config.ExchangeConfig{APIKey: "reset-mysql-old-api-key", SecretKey: "reset-mysql-old-secret"}
	newCredentials := config.ExchangeConfig{APIKey: "reset-mysql-new-api-key", SecretKey: "reset-mysql-new-secret"}
	oldConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	newConfig := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": newCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	oldScope, newScope := buildEquityScopeSnapshot(oldConfig), buildEquityScopeSnapshot(newConfig)
	now := time.Now().UTC()
	if err := archive.archiveRemoved(t.Context(), oldScope, newScope, now.Add(-5*time.Minute)); err != nil {
		t.Fatal("persist retired-account archive:", err)
	}
	accounts, _, err := archive.load(t.Context())
	if err != nil || len(accounts) != 1 {
		t.Fatalf("load retired-account fixture: count=%d err=%v", len(accounts), err)
	}
	firstFlat := now.Add(-3 * time.Minute)
	if err := archive.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat); err != nil {
		t.Fatal("persist first flat observation:", err)
	}
	if err := archive.recordEvidence(t.Context(), accounts[0].ID, true, true, firstFlat.Add(retiredEquityFlatEvidenceInterval)); err != nil {
		t.Fatal("persist second flat observation:", err)
	}

	equityBackend := namespacedRiskCheckpointBackend{backend: backend, prefix: equityPrefix}
	equityStore := &persistedEquityState{backend: equityBackend}
	oldCheckpoint := risk.EquityCheckpoint{Version: 1, Revision: 1, Scope: oldScope.scope, Currency: "USDT", CashFlowAdjusted: true,
		BaseAt: now.Add(-time.Hour), LastAt: now.Add(-time.Minute), LastEquity: 1000, AdjustedEquity: 1000, HighWater: 1000,
		Receipts: map[string]risk.EquityCashFlow{}}
	if err := equityStore.SaveEquityState(t.Context(), 0, oldCheckpoint); err != nil {
		t.Fatal("persist prior-scope equity checkpoint:", err)
	}
	coordinator, err := risk.NewOpeningPauseCoordinatorWithStore(t.Context(), backend, instanceIdentity)
	if err != nil {
		t.Fatal("restore durable opening-pause coordinator:", err)
	}
	manager := newRetiredResetMySQLManager(t, newConfig, archive, coordinator, equityStore, newScope.scope)
	observer := retiredResetMySQLObserver(t, oldCredentials)
	firstOperation, err := manager.ResetRetiredEquityAccounts(t.Context(), "mysql-admin", observer)
	if err == nil || firstOperation.ID == "" {
		t.Fatalf("injected audit failure was not returned with its durable operation identity: has_id=%v", firstOperation.ID != "")
	}
	if !coordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("opening hold released although reset audit persistence failed")
	}
	pendingPayload, _, err := archiveBackend.LoadRiskCheckpoint(t.Context(), retiredEquityAccountsCheckpointKey)
	if err != nil {
		t.Fatal("load pending reset after injected failure:", err)
	}
	var pendingState retiredEquityAccountsState
	if err := json.Unmarshal(pendingPayload, &pendingState); err != nil || pendingState.PendingReset == nil || pendingState.PendingReset.ID != firstOperation.ID || len(pendingState.Accounts) != 1 {
		t.Fatalf("failed audit did not preserve reset intent and archived account: pending=%v accounts=%d err=%v", pendingState.PendingReset != nil, len(pendingState.Accounts), err)
	}
	checkpoint, err := equityStore.LoadEquityState(t.Context())
	if err != nil || checkpoint == nil || checkpoint.Scope != newScope.scope || checkpoint.ResetOperationID != firstOperation.ID {
		t.Fatalf("baseline CAS should be durable before the injected audit failure: has_checkpoint=%v err=%v", checkpoint != nil, err)
	}

	if err := backend.Close(); err != nil {
		t.Fatal("close storage to simulate process restart:", err)
	}
	backend, err = storage.NewMySQLStorage(dsn)
	if err != nil {
		t.Fatal("reopen MySQL storage after restart:", err)
	}
	archive = &retiredEquityAccountsStore{backend: namespacedRiskCheckpointBackend{backend: backend, prefix: archivePrefix}, key: []byte("0123456789abcdef0123456789abcdef")}
	equityStore = &persistedEquityState{backend: namespacedRiskCheckpointBackend{backend: backend, prefix: equityPrefix}}
	coordinator, err = risk.NewOpeningPauseCoordinatorWithStore(t.Context(), backend, instanceIdentity)
	if err != nil {
		t.Fatal("restore opening-pause coordinator after restart:", err)
	}
	if !coordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("durable reset hold was not restored after restart")
	}
	manager = newRetiredResetMySQLManager(t, newConfig, archive, coordinator, equityStore, newScope.scope)
	secondOperation, err := manager.ResetRetiredEquityAccounts(t.Context(), "mysql-admin", observer)
	if err != nil {
		t.Fatal("retry reset after restart:", err)
	}
	if secondOperation.ID != firstOperation.ID || secondOperation.Actor != "mysql-admin" || secondOperation.TargetScope != newScope.scope ||
		secondOperation.BaselineRevision != checkpoint.ResetOperationRevision || secondOperation.CompletedAt.IsZero() {
		t.Fatalf("recovered reset audit mismatch: same_operation=%v revision=%d completion_set=%v", secondOperation.ID == firstOperation.ID,
			secondOperation.BaselineRevision, !secondOperation.CompletedAt.IsZero())
	}
	if archiveBackend.armed.Load() || coordinator.IsHeldBy(retiredEquityAccountPauseSource) {
		t.Fatal("reset completion or pause release did not converge after retry")
	}
	remaining, _, err := archive.load(t.Context())
	if err != nil || len(remaining) != 0 {
		t.Fatalf("retired credentials remain after audited reset: count=%d err=%v", len(remaining), err)
	}
	history, err := manager.RetiredEquityAccountResetHistory(t.Context())
	if err != nil || len(history) != 1 || history[0].ID != firstOperation.ID || history[0].Actor != "mysql-admin" || history[0].BaselineRevision != checkpoint.ResetOperationRevision {
		t.Fatalf("durable reset history mismatch: count=%d err=%v", len(history), err)
	}
	rows, err := backend.LoadOpeningPauseHolders(t.Context())
	if err != nil {
		t.Fatal("verify durable pause owners after reset:", err)
	}
	for _, row := range rows {
		if row.OwnerID == ownerID && row.Source == retiredEquityAccountPauseSource {
			t.Fatal("retired-account opening hold remains durable after completed reset")
		}
	}
}

func newRetiredResetMySQLManager(t *testing.T, cfg *config.Config, archive *retiredEquityAccountsStore,
	coordinator *risk.OpeningPauseCoordinator, equityStore risk.EquityStateStore, scope string) *BotManager {
	t.Helper()
	manager := NewBotManager(cfg, nil, nil, nil, "")
	manager.retiredEquityAccounts = archive
	manager.SetOpeningPauseCoordinator(coordinator)
	source := retiredResetEquitySource{observation: risk.EquityObservation{Scope: scope, Currency: "USDT", Equity: 1200,
		CashFlowComplete: true, Wallets: map[string]accounting.Wallet{"new-account": {
			Balance: "1200", Currency: "USDT", From: time.Now().UTC().Add(-time.Hour),
		}}}}
	feeder := risk.NewMetricsFeeder(passiveWithdrawalMetricsSink{}, nil, source, nil, risk.MetricsFeederOptions{
		MaxEquityAge: time.Hour, EquityStore: equityStore, RequirePersistence: true, RequireCashFlowReconciliation: true,
	})
	manager.SetEquityBaselineResetter(feeder)
	return manager
}

func retiredResetMySQLObserver(t *testing.T, oldCredentials config.ExchangeConfig) retiredEquityFuturesObserverFactory {
	t.Helper()
	return func(exchangeName string, credentials config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error) {
		if exchangeName != "binance" || credentials.APIKey != oldCredentials.APIKey || credentials.SecretKey != oldCredentials.SecretKey {
			return nil, fmt.Errorf("read-only verifier received unexpected archived account identity")
		}
		return retiredAccountObserverFixture{complete: true, flat: true, observed: time.Now().UTC()}, nil
	}
}
