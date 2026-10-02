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
