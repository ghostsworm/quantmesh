package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

func TestFundingCarryStoppedFlatCancellationPreservesProvenance(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "legacy"}[legacy], func(t *testing.T) {
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balance: 2, quantityDecimals: 8}
			futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 3}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wrapped := &cancelAfterPositionRead{IExchange: futures, cancel: cancel}
			store := &memoryRuntimeStateStore{}
			s := NewFundingCarryStrategy("stopped-cancel", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, wrapped, spot, nil, nil)
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			s.strategySpotKnown = true
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			verify := s.VerifyStoppedFlat
			if legacy {
				verify = s.VerifyFlat
			}
			if err := verify(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal("canceled read became flat evidence", err)
			}
			if wrapped.calls != 1 {
				t.Fatal("fixture did not cancel actual position query")
			}
			if legacy {
				if !s.unownedExposure {
					t.Fatal("ordinary sync lost strict financial unknown gate")
				}
				return
			}
			if s.unownedExposure || store.payload != before {
				t.Fatal("canceled readonly query rewrote provenance")
			}
			s.fut = futures
			if err := verify(t.Context()); err != nil || store.payload != before {
				t.Fatal("fresh proof cannot recover canceled read", err)
			}
		})
	}
}

func TestFundingCarryStoppedFlatReadFailureDoesNotAdoptOrClearExposure(t *testing.T) {
	for _, mode := range []string{"query_failure", "active", "existing_unknown", "wrong_symbol", "unexpected_exposure", "legacy_query_failure"} {
		t.Run(mode, func(t *testing.T) {
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balance: 2, quantityDecimals: 8}
			futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 3}
			store := &memoryRuntimeStateStore{}
			s := NewFundingCarryStrategy("stopped-flat", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			s.strategySpotKnown = true
			if mode == "active" {
				s.started = true
			}
			if mode == "existing_unknown" {
				s.unownedExposure = true
			}
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			switch mode {
			case "query_failure", "legacy_query_failure":
				futures.positionsErr = errors.New("fixture query unavailable")
			case "wrong_symbol":
				futures.positions = []*exchange.Position{{Symbol: "ETHUSDT"}}
			case "unexpected_exposure":
				futures.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}
			}
			verify := s.VerifyStoppedFlat
			if mode == "legacy_query_failure" {
				verify = s.VerifyFlat
			}
			if err := verify(t.Context()); err == nil {
				t.Fatal("invalid stopped proof accepted")
			}
			if mode == "query_failure" || mode == "wrong_symbol" || mode == "unexpected_exposure" {
				if s.unownedExposure || store.payload != before {
					t.Fatal("readonly final proof failure poisoned clean provenance")
				}
				futures.positionsErr = nil
				futures.positions = nil
				if err := verify(t.Context()); err != nil || store.payload != before {
					t.Fatal("fresh stopped proof cannot recover after new authoritative evidence", err)
				}
			} else if mode == "active" || mode == "existing_unknown" {
				if store.payload != before {
					t.Fatal("stopped proof rewrote active/unknown provenance")
				}
			} else if mode == "legacy_query_failure" && !s.unownedExposure {
				t.Fatal("ordinary runtime sync lost strict financial unknown gate")
			}
		})
	}
}
