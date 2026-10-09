package exchange

import (
	"testing"

	"quantmesh/config"
)

func TestNewAccountEvidenceSourceSupportsOnlyReadOnlyAdapters(t *testing.T) {
	credentials := config.ExchangeConfig{APIKey: "test-key", SecretKey: "test-secret", Passphrase: "test-passphrase", Testnet: true}
	for _, test := range []struct {
		name, market string
	}{
		{name: "binance", market: "futures"},
		{name: "bitget", market: "futures"},
		{name: "bitget", market: "spot"},
	} {
		t.Run(test.name+"/"+test.market, func(t *testing.T) {
			source, err := NewAccountEvidenceSource(test.name, test.market, credentials)
			if err != nil || source == nil {
				t.Fatalf("read-only evidence source unavailable: source=%T err=%v", source, err)
			}
		})
	}
}

func TestNewAccountEvidenceSourceRejectsUnsupportedMarketAndMissingCredentials(t *testing.T) {
	if source, err := NewAccountEvidenceSource("kraken", "futures", config.ExchangeConfig{APIKey: "key"}); err == nil || source != nil {
		t.Fatal("unsupported exchange unexpectedly received an account evidence source")
	}
	if source, err := NewAccountEvidenceSource("binance", "futures", config.ExchangeConfig{APIKey: "key"}); err == nil || source != nil {
		t.Fatal("incomplete account credentials unexpectedly received an account evidence source")
	}
}

func TestNewAccountFuturesFlatnessVerifierUsesOnlySupportedRestAdapters(t *testing.T) {
	credentials := config.ExchangeConfig{APIKey: "test-key", SecretKey: "test-secret", Passphrase: "test-passphrase", Testnet: true}
	for _, name := range []string{"binance", "bitget"} {
		verifier, err := NewAccountFuturesFlatnessVerifier(name, credentials)
		if err != nil || verifier == nil {
			t.Fatalf("REST-only verifier for %s unavailable: verifier=%T err=%v", name, verifier, err)
		}
		observer, err := NewAccountFuturesFlatnessObserver(name, credentials)
		if err != nil || observer == nil {
			t.Fatalf("REST-only observer for %s unavailable: observer=%T err=%v", name, observer, err)
		}
	}
	if verifier, err := NewAccountFuturesFlatnessVerifier("kraken", credentials); err == nil || verifier != nil {
		t.Fatal("unsupported account received a Futures flatness verifier")
	}
	if observer, err := NewAccountFuturesFlatnessObserver("kraken", credentials); err == nil || observer != nil {
		t.Fatal("unsupported account received a Futures flatness observer")
	}
	if verifier, err := NewAccountFuturesFlatnessVerifier("bitget", config.ExchangeConfig{APIKey: "key", SecretKey: "secret"}); err == nil || verifier != nil {
		t.Fatal("Bitget verifier was created without its passphrase")
	}
}

func TestNewAccountSpotMarginFlatnessVerifierUsesReadOnlyBinanceAdapter(t *testing.T) {
	credentials := config.ExchangeConfig{APIKey: "test-key", SecretKey: "test-secret", Testnet: true}
	verifier, err := NewAccountSpotMarginFlatnessVerifier(" Binance ", credentials)
	if err != nil || verifier == nil {
		t.Fatalf("Binance read-only Spot Margin verifier unavailable: verifier=%T err=%v", verifier, err)
	}
	if verifier, err := NewAccountSpotMarginFlatnessVerifier("bitget", credentials); err == nil || verifier != nil {
		t.Fatal("unsupported account unexpectedly received a Spot Margin verifier")
	}
	if verifier, err := NewAccountSpotMarginFlatnessVerifier("binance", config.ExchangeConfig{APIKey: "test-key"}); err == nil || verifier != nil {
		t.Fatal("Spot Margin verifier was created with incomplete credentials")
	}
}
