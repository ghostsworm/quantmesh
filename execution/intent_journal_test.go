package execution

import "testing"

func TestIntentJournalScopeSeparatesEveryOwnerDimension(t *testing.T) {
	base := IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot"}
	key, err := base.Key()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*IntentScope){
		func(s *IntentScope) { s.Account = "other" }, func(s *IntentScope) { s.Exchange = "other" },
		func(s *IntentScope) { s.Market = "spot" }, func(s *IntentScope) { s.Symbol = "ETHUSDT" }, func(s *IntentScope) { s.Bot = "other" },
	} {
		scope := base
		change(&scope)
		other, err := scope.Key()
		if err != nil || other == key {
			t.Fatalf("owner collision: %v", err)
		}
	}
	base.Account = ""
	if _, err := base.Key(); err == nil {
		t.Fatal("missing account accepted")
	}
}
