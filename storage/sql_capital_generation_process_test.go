package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	capitalGenerationPathEnv = "QUANTMESH_CAPITAL_GENERATION_PROBE_PATH"
	capitalGenerationLegEnv  = "QUANTMESH_CAPITAL_GENERATION_PROBE_LEG"
	capitalGenerationExit    = 23
	capitalGenerationBot     = "isolated-generation-owner"
)

func capitalGenerationClaims() []AccountWalletCapitalClaim {
	claims := make([]AccountWalletCapitalClaim, 3)
	for i, market := range []string{"futures", "spot", "spot_margin"} {
		claims[i] = AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", i+1), ReservationToken: fmt.Sprintf("%064x", i+101), Amount: 20, Available: 100,
			Exchange: "binance", Market: market, QuoteAsset: "USDT", Symbol: "BTCUSDT"}
	}
	return claims
}

func TestCapitalGenerationAbruptExitHelper(t *testing.T) {
	path := os.Getenv(capitalGenerationPathEnv)
	if path == "" {
		return
	}
	leg, err := strconv.Atoi(os.Getenv(capitalGenerationLegEnv))
	if err != nil || (leg != 0 && leg != 2) {
		t.Fatal("invalid isolated generation probe leg")
	}
	store, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	claim := capitalGenerationClaims()[leg]
	claim.ReservationToken = fmt.Sprintf("%064x", leg+201)
	claim.Amount = 30
	claim.ObservationSequence, err = store.BeginAccountWalletBalanceObservation(t.Context(), claim.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(t.Context(), capitalGenerationBot, []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	// Deliberately skip Close/defer: the replacement transaction has committed,
	// but no process-local memory or cleanup survives this abrupt exit.
	os.Exit(capitalGenerationExit)
}

func TestCapitalGenerationAbruptExitPreservesAtomicStaleRelease(t *testing.T) {
	for _, leg := range []int{0, 2} {
		t.Run(fmt.Sprintf("replacement_leg_%d", leg), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "capital.db")
			old := capitalGenerationClaims()
			store, err := NewSQLStorage(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := range old {
				old[i].ObservationSequence, err = store.BeginAccountWalletBalanceObservation(t.Context(), old[i].WalletKey)
				if err != nil {
					_ = store.Close()
					t.Fatal(err)
				}
			}
			if err := store.ReserveAccountWalletCapital(t.Context(), capitalGenerationBot, old); err != nil {
				_ = store.Close()
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, executable, "-test.run=^TestCapitalGenerationAbruptExitHelper$", "-test.count=1")
			child.Env = append(os.Environ(), capitalGenerationPathEnv+"="+path, capitalGenerationLegEnv+"="+strconv.Itoa(leg))
			err = child.Run()
			var exited *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exited) || exited.ExitCode() != capitalGenerationExit {
				t.Fatal("replacement child did not reach committed abrupt exit")
			}
			fresh, err := NewSQLStorage(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := fresh.Close(); err != nil {
					t.Error(err)
				}
			})
			current := capitalGenerationClaims()
			current[leg].ReservationToken = fmt.Sprintf("%064x", leg+201)
			current[leg].Amount = 30
			for attempt := 0; attempt < 2; attempt++ {
				if err := fresh.ReleaseAccountWalletCapital(t.Context(), capitalGenerationBot, old); err == nil {
					t.Fatal("stale generation release accepted after replacement process exit")
				}
				rows, err := fresh.ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
				if err != nil || len(rows) != len(current) {
					t.Fatal("failed stale release partially deleted reservations", err)
				}
				for i, claim := range current {
					var token string
					var amount float64
					if err := fresh.db.QueryRowContext(t.Context(), `SELECT reservation_token, amount FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, fundingSpreadBotKey(capitalGenerationBot)).Scan(&token, &amount); err != nil {
						t.Fatal(err)
					}
					if token != claim.ReservationToken || amount != claim.Amount || rows[i].WalletKey != claim.WalletKey || !rows[i].Mapped || rows[i].Market != claim.Market || rows[i].Symbol != claim.Symbol {
						t.Fatal("stale release changed generation, amount or ownership metadata")
					}
				}
			}
			if err := fresh.ReleaseAccountWalletCapital(t.Context(), capitalGenerationBot, current); err != nil {
				t.Fatal("current generation could not release its claims", err)
			}
			rows, err := fresh.ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
			if err != nil || len(rows) != 0 {
				t.Fatal("current generation release retained mapped rows", err)
			}
		})
	}
}
