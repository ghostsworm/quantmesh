package gate

import "testing"

func TestMapGateWalletEndpoints(t *testing.T) {
	from, to, err := mapGateWalletEndpoints("UMFUTURE", "SPOT")
	if err != nil || from != "futures" || to != "spot" {
		t.Fatalf("got %s %s %v", from, to, err)
	}
	from, to, err = mapGateWalletEndpoints("MAIN", "CONTRACT")
	if err != nil || from != "spot" || to != "futures" {
		t.Fatalf("got %s %s %v", from, to, err)
	}
	_, _, err = mapGateWalletEndpoints("SPOT", "SPOT")
	if err == nil {
		t.Fatal("expected error")
	}
}
