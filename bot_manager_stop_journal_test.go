package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/storage"
)

func journalTestOwner(bm *BotManager, stop func() error) *BotRuntime {
	owner := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	br := &BotRuntime{BotID: owner.ID, Config: owner, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(owner), StopWithError: stop}}
	bm.AddRuntime(br)
	return br
}

func TestValidateDurableStopCapitalClaims(t *testing.T) {
	valid := durableStopCapitalClaim{
		WalletKey:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReservationToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Amount:           1,
	}
	tests := []struct {
		name    string
		claims  []durableStopCapitalClaim
		wantErr bool
	}{
		{name: "legacy journal without claims"},
		{name: "valid claim", claims: []durableStopCapitalClaim{valid}},
		{name: "invalid wallet digest", claims: []durableStopCapitalClaim{{WalletKey: "wallet", ReservationToken: valid.ReservationToken, Amount: 1}}, wantErr: true},
		{name: "invalid generation token", claims: []durableStopCapitalClaim{{WalletKey: valid.WalletKey, ReservationToken: "unknown", Amount: 1}}, wantErr: true},
		{name: "non-positive amount", claims: []durableStopCapitalClaim{{WalletKey: valid.WalletKey, ReservationToken: valid.ReservationToken, Amount: 0}}, wantErr: true},
		{name: "duplicate wallet", claims: []durableStopCapitalClaim{valid, valid}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDurableStopClaims(test.claims)
			if (err != nil) != test.wantErr {
				t.Fatalf("validate claims error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDurableStopIntentRetriesDirectorySyncForSameOperation(t *testing.T) {
	bm := newEnableStateStorage(t)
	var syncCalls int
	bm.stopJournalDirectorySync = func(path string) error {
		syncCalls++
		if syncCalls == 1 {
			return errors.New("fixture directory fsync failure after journal publication")
		}
		return syncStopJournalDirectory(path)
	}
	br := journalTestOwner(bm, nil)
	state := &storage.BotState{BotID: "owner", Enabled: false}
	if err := bm.beginDurableStop(br, state); err == nil {
		t.Fatal("directory fsync failure was reported as a durable stop intent")
	}
	firstOperation := br.stopJournalOperation
	if firstOperation == "" {
		t.Fatal("failed sync lost the in-memory operation identity")
	}
	journal, err := bm.readStopJournal("owner")
	if err != nil || journal == nil || journal.Operation != firstOperation {
		t.Fatal("published journal did not retain the original operation")
	}
	if err := bm.beginDurableStop(br, state); err != nil {
		t.Fatal("same operation could not retry directory sync", err)
	}
	journal, err = bm.readStopJournal("owner")
	if err != nil || journal == nil || journal.Operation != firstOperation || syncCalls != 2 {
		t.Fatal("retry replaced the operation or failed to confirm directory durability")
	}
}

func TestDurableStopReconstructionCanFinishPersistenceWithoutFinancialReplay(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	br := journalTestOwner(bm, func() error { calls++; return nil })
	if err := bm.storageService.GetStorage().Close(); err != nil {
		t.Fatal(err)
	}
	if err := bm.StopBot(br.BotID); err == nil {
		t.Fatal("primary fixture should fail")
	}
	journal, err := bm.readStopJournal(br.BotID)
	if err != nil || journal == nil || !journal.Complete {
		t.Fatal("completed stop was not durable")
	}
	if !br.Inner.controllerStopCompleted.Load() {
		t.Fatal("completed controller shutdown not marked")
	}
	if _, err := bm.sealProcessRuntimes(context.Background()); err == nil {
		t.Fatal("pending stop sealed as verified")
	}
	ss, err := storage.NewStorageService(bm.cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ss.Stop)
	fresh := NewBotManager(bm.cfg, event.NewEventBus(8), ss, nil, "")
	fresh.botStatesFileOverride = bm.botStatesFileOverride
	if enabled, _ := fresh.IsBotEnabledInDB(br.BotID); enabled {
		t.Fatal("restart ignored stop intent")
	}
	if err := fresh.EnableBot(br.BotID); err == nil {
		t.Fatal("enable silently cleared unresolved stop")
	}
	if err := fresh.StopBot(br.BotID); err != nil {
		t.Fatal(err)
	}
	state, err := ss.GetStorage().GetBotState(br.BotID)
	if err != nil || state == nil || state.Enabled {
		t.Fatal("recovery did not disable primary")
	}
	if journal, err := fresh.readStopJournal(br.BotID); err != nil || journal != nil {
		t.Fatal("reconciled intent not retired")
	}
	if calls != 1 {
		t.Fatal("reconstruction repeated financial stop")
	}
	if err := fresh.EnableBot(br.BotID); err != nil {
		t.Fatal("explicit enable after durable reconciliation failed", err)
	}
	if enabled, _ := fresh.IsBotEnabledInDB(br.BotID); !enabled {
		t.Fatal("explicit enable after reconciliation not usable")
	}
}

func TestInterruptedStopCannotBeGuessedCompleteOnReconstruction(t *testing.T) {
	bm := newEnableStateStorage(t)
	bm.eventBus = event.NewEventBus(8)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	journalTestOwner(bm, func() error { return errors.New("fixture unknown stop terminal") })
	if err := bm.StopBot("owner"); err == nil {
		t.Fatal("unknown stop reported success")
	}
	journal, err := bm.readStopJournal("owner")
	if err != nil || journal == nil || journal.Complete {
		t.Fatal("unknown stop marked complete")
	}
	fresh := NewBotManager(bm.cfg, event.NewEventBus(8), bm.storageService, nil, "")
	fresh.botStatesFileOverride = bm.botStatesFileOverride
	if enabled, _ := fresh.IsBotEnabledInDB("owner"); enabled {
		t.Fatal("unknown stop permitted automatic start")
	}
	if err := fresh.StopBot("owner"); err == nil {
		t.Fatal("interrupted stop recovered without financial evidence")
	}
	if err := fresh.EnableBot("owner"); err == nil {
		t.Fatal("enable bypassed unresolved intent")
	}
}

func TestDurableStopRecoveryCallbackMustSucceedBeforeJournalRetirement(t *testing.T) {
	t.Run("callback failure preserves enabled state and journal", func(t *testing.T) {
		bm := newEnableStateStorage(t)
		bm.eventBus = event.NewEventBus(8)
		t.Cleanup(bm.eventBus.Close)
		if err := bm.EnableBot("owner"); err != nil {
			t.Fatal(err)
		}
		journalTestOwner(bm, func() error { return errors.New("fixture interrupted stop") })
		if err := bm.StopBot("owner"); err == nil {
			t.Fatal("interrupted stop reported success")
		}
		fresh := NewBotManager(bm.cfg, event.NewEventBus(8), bm.storageService, nil, "")
		fresh.botStatesFileOverride = bm.botStatesFileOverride
		var callbackCalls int
		fresh.SetDurableStopRecovery(func(ctx context.Context, journal *botStopJournal) error {
			callbackCalls++
			if err := ctx.Err(); err != nil {
				return err
			}
			if journal == nil || journal.Complete || journal.State.BotID != "owner" {
				return errors.New("unexpected recovery journal")
			}
			return errors.New("fixture account evidence unavailable")
		})
		if err := fresh.StopBot("owner"); err == nil {
			t.Fatal("failed financial reconciliation reported success")
		}
		if callbackCalls != 1 {
			t.Fatalf("recovery callback calls = %d, want 1", callbackCalls)
		}
		state, err := fresh.storageService.GetStorage().GetBotState("owner")
		if err != nil || state == nil || !state.Enabled {
			t.Fatalf("failed reconciliation changed durable enabled state: state=%+v err=%v", state, err)
		}
		journal, err := fresh.readStopJournal("owner")
		if err != nil || journal == nil || journal.Complete {
			t.Fatalf("failed reconciliation retired or completed journal: journal=%+v err=%v", journal, err)
		}
	})

	t.Run("successful callback permits durable disable and retirement", func(t *testing.T) {
		bm := newEnableStateStorage(t)
		bm.eventBus = event.NewEventBus(8)
		t.Cleanup(bm.eventBus.Close)
		if err := bm.EnableBot("owner"); err != nil {
			t.Fatal(err)
		}
		journalTestOwner(bm, func() error { return errors.New("fixture interrupted stop") })
		if err := bm.StopBot("owner"); err == nil {
			t.Fatal("interrupted stop reported success")
		}
		fresh := NewBotManager(bm.cfg, event.NewEventBus(8), bm.storageService, nil, "")
		fresh.botStatesFileOverride = bm.botStatesFileOverride
		var callbackCalls int
		fresh.SetDurableStopRecovery(func(ctx context.Context, journal *botStopJournal) error {
			callbackCalls++
			if journal == nil || journal.Complete || journal.State.BotID != "owner" {
				return errors.New("unexpected recovery journal")
			}
			return nil
		})
		if err := fresh.StopBot("owner"); err != nil {
			t.Fatal("successful reconciliation did not finish stop persistence:", err)
		}
		if callbackCalls != 1 {
			t.Fatalf("recovery callback calls = %d, want 1", callbackCalls)
		}
		state, err := fresh.storageService.GetStorage().GetBotState("owner")
		if err != nil || state == nil || state.Enabled {
			t.Fatalf("successful reconciliation did not disable Bot durably: state=%+v err=%v", state, err)
		}
		if journal, err := fresh.readStopJournal("owner"); err != nil || journal != nil {
			t.Fatalf("successful reconciliation did not retire journal: journal=%+v err=%v", journal, err)
		}
	})
}

func TestStopAllDurablyGuardsInterruptedStopWithoutDisablingCleanShutdown(t *testing.T) {
	t.Run("interrupted", func(t *testing.T) {
		bm := newEnableStateStorage(t)
		bm.eventBus = event.NewEventBus(8)
		t.Cleanup(bm.eventBus.Close)
		if err := bm.EnableBot("owner"); err != nil {
			t.Fatal(err)
		}
		journalTestOwner(bm, func() error { return errors.New("fixture stop interrupted") })
		if err := bm.StopAll(); err == nil {
			t.Fatal("unverified StopAll reported success")
		}
		journal, err := bm.readStopJournal("owner")
		if err != nil || journal == nil || journal.Complete || journal.Mode != botStopJournalModeShutdown || !journal.State.Enabled {
			t.Fatal("interrupted StopAll did not retain an enabled shutdown intent")
		}
		fresh := NewBotManager(bm.cfg, event.NewEventBus(8), bm.storageService, nil, "")
		fresh.botStatesFileOverride = bm.botStatesFileOverride
		if enabled, _ := fresh.IsBotEnabledInDB("owner"); enabled {
			t.Fatal("restart permitted after interrupted StopAll")
		}
		if err := fresh.EnableBot("owner"); err == nil {
			t.Fatal("enable cleared interrupted StopAll intent")
		}
	})

	t.Run("clean_shutdown_preserves_enablement", func(t *testing.T) {
		bm := newEnableStateStorage(t)
		bm.eventBus = event.NewEventBus(8)
		t.Cleanup(bm.eventBus.Close)
		if err := bm.EnableBot("owner"); err != nil {
			t.Fatal(err)
		}
		var calls int
		journalTestOwner(bm, func() error {
			calls++
			journal, err := bm.readStopJournal("owner")
			if err != nil || journal == nil || journal.Mode != botStopJournalModeShutdown || journal.Complete || !journal.State.Enabled {
				return errors.New("shutdown intent was not durable before financial stop")
			}
			return nil
		})
		if err := bm.StopAll(); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatal("verified shutdown callback count changed")
		}
		if enabled, _ := bm.IsBotEnabledInDB("owner"); !enabled {
			t.Fatal("clean StopAll disabled the Bot")
		}
		if journal, err := bm.readStopJournal("owner"); err != nil || journal != nil {
			t.Fatal("clean StopAll left a durable shutdown intent")
		}
	})
}

func TestCompletedShutdownJournalIsRetiredBeforeEnabledBotStart(t *testing.T) {
	bm := newEnableStateStorage(t)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	journal := &botStopJournal{
		State:     &storage.BotState{BotID: "owner", Enabled: true},
		Operation: "verified-shutdown-operation", Mode: botStopJournalModeShutdown, Complete: true,
	}
	if err := bm.writeStopJournal(journal, true); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := bm.IsBotEnabledInDB("owner"); !enabled {
		t.Fatal("verified shutdown journal hid the authoritative enabled state")
	}
	called := false
	bm.SetStartConfigValidator(func(config.BotConfig) error {
		called = true
		if journal, err := bm.readStopJournal("owner"); err != nil || journal != nil {
			return errors.New("completed shutdown intent was not retired before start validation")
		}
		return errors.New("fixture stops before runtime startup")
	})
	enabledConfig := true
	_, err := bm.StartBot(context.Background(), config.BotConfig{ID: "owner", Enabled: &enabledConfig})
	if err == nil || !called {
		t.Fatal("start did not retire completed shutdown intent before validating config")
	}
	if journal, err := bm.readStopJournal("owner"); err != nil || journal != nil {
		t.Fatal("completed shutdown intent remained after guarded restart")
	}
	if enabled, _ := bm.IsBotEnabledInDB("owner"); !enabled {
		t.Fatal("retiring completed shutdown intent changed durable enablement")
	}
}
