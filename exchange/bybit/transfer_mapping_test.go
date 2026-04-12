package bybit

import "testing"

func TestMapBybitTransferAccounts(t *testing.T) {
	f, to, err := mapBybitTransferAccounts("UMFUTURE", "MAIN")
	if err != nil || f != "CONTRACT" || to != "SPOT" {
		t.Fatalf("got %s %s %v", f, to, err)
	}
	f, to, err = mapBybitTransferAccounts("SPOT", "CONTRACT")
	if err != nil || f != "SPOT" || to != "CONTRACT" {
		t.Fatalf("got %s %s %v", f, to, err)
	}
	_, _, err = mapBybitTransferAccounts("SPOT", "SPOT")
	if err == nil {
		t.Fatal("expected error")
	}
}
