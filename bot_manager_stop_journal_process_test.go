package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/storage"
)

const (
	stopJournalProbeDirEnv   = "QUANTMESH_STOP_JOURNAL_PROBE_DIR"
	stopJournalProbePhaseEnv = "QUANTMESH_STOP_JOURNAL_PROBE_PHASE"
	stopJournalProbeExitCode = 23
)

func stopJournalProcessManager(t *testing.T, dir string) *BotManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(dir, "state.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	ss, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ss.Stop)
	bus := event.NewEventBus(8)
	t.Cleanup(bus.Close)
	bm := NewBotManager(cfg, bus, ss, nil, "")
	bm.botStatesFileOverride = filepath.Join(dir, "states.json")
	return bm
}

func TestStopJournalAbruptProcessExitHelper(t *testing.T) {
	dir, phase := os.Getenv(stopJournalProbeDirEnv), os.Getenv(stopJournalProbePhaseEnv)
	if dir == "" || phase == "" {
		return
	}
	if phase != "incomplete" && phase != "complete" && phase != "stopall-incomplete" {
		t.Fatal("invalid isolated probe phase")
	}
	bm := stopJournalProcessManager(t, dir)
	if err := bm.EnableBot("owner"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	owner := journalTestOwner(bm, func() error {
		calls++
		if phase != "complete" {
			os.Exit(stopJournalProbeExitCode)
		}
		return nil
	})
	owner.Inner.capitalReservationClaims = []storage.AccountWalletCapitalClaim{{
		WalletKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReservationToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Amount: 125.5, Exchange: "binance", Market: "futures", QuoteAsset: "USDT", Symbol: "BTCUSDT",
	}}
	if err := bm.storageService.GetStorage().Close(); err != nil {
		t.Fatal(err)
	}
	journalDir := filepath.Dir(bm.stopJournalPath("owner"))
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(journalDir, 0755); err != nil {
		t.Fatal(err)
	}
	var stopErr error
	if phase == "stopall-incomplete" {
		stopErr = bm.StopAll()
	} else {
		stopErr = bm.StopBot("owner")
	}
	if stopErr == nil {
		t.Fatal("closed primary should reject stop persistence")
	}
	journal, err := bm.readStopJournal("owner")
	if err != nil || journal == nil || !journal.Complete || calls != 1 {
		t.Fatal("completed stop fixture not durable")
	}
	// Deliberately bypass all test cleanup/defer, as an abrupt process exit.
	os.Exit(stopJournalProbeExitCode)
}

func TestStopJournalSurvivesAbruptProcessExit(t *testing.T) {
	for _, phase := range []string{"incomplete", "complete", "stopall-incomplete"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestStopJournalAbruptProcessExitHelper$", "-test.count=1")
			child.Env = append(os.Environ(), stopJournalProbeDirEnv+"="+dir, stopJournalProbePhaseEnv+"="+phase)
			err = child.Run()
			var exited *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exited) || exited.ExitCode() != stopJournalProbeExitCode {
				t.Fatal("isolated child did not reach expected abrupt exit")
			}
			fresh := stopJournalProcessManager(t, dir)
			journal, err := fresh.readStopJournal("owner")
			if err != nil || journal == nil || journal.Complete != (phase == "complete") {
				t.Fatal("journal phase not preserved across process exit")
			}
			if phase == "stopall-incomplete" && (journal.Mode != botStopJournalModeShutdown || !journal.State.Enabled) {
				t.Fatal("interrupted StopAll did not preserve shutdown intent")
			}
			if len(journal.CapitalClaims) != 1 || journal.CapitalClaims[0].WalletKey != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
				journal.CapitalClaims[0].ReservationToken != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" ||
				journal.CapitalClaims[0].Amount != 125.5 || journal.CapitalClaims[0].Exchange != "binance" ||
				journal.CapitalClaims[0].Market != "futures" || journal.CapitalClaims[0].QuoteAsset != "USDT" ||
				journal.CapitalClaims[0].Symbol != "BTCUSDT" {
				t.Fatal("capital reservation generation was not preserved across process exit")
			}
			info, err := os.Stat(fresh.stopJournalPath("owner"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("journal lacks private file permissions")
			}
			dirInfo, err := os.Stat(filepath.Dir(fresh.stopJournalPath("owner")))
			if err != nil || dirInfo.Mode().Perm() != 0700 {
				t.Fatal("journal directory lacks private permissions")
			}
			if enabled, _ := fresh.IsBotEnabledInDB("owner"); enabled {
				t.Fatal("new process state read ignored unresolved stop")
			}
			if err := fresh.EnableBot("owner"); err == nil {
				t.Fatal("enable cleared unresolved stop")
			}
			if phase != "complete" {
				if err := fresh.StopBot("owner"); err == nil {
					t.Fatal("interrupted callback guessed complete")
				}
				return
			}
			if err := fresh.StopBot("owner"); err != nil {
				t.Fatal(err)
			}
			state, err := fresh.storageService.GetStorage().GetBotState("owner")
			if err != nil || state == nil || state.Enabled {
				t.Fatal("complete stop did not reconcile primary")
			}
			if journal, err := fresh.readStopJournal("owner"); err != nil || journal != nil {
				t.Fatal("completed stop intent not retired")
			}
		})
	}
}
