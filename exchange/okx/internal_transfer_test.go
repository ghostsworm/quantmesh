package okx

import "testing"

func TestMapOKXTransferEndpoints(t *testing.T) {
	from, to, err := mapOKXTransferEndpoints("FUNDING", "TRADING")
	if err != nil || from != "18" || to != "6" {
		t.Fatalf("FUNDING->TRADING: from=%s to=%s err=%v", from, to, err)
	}
	from, to, err = mapOKXTransferEndpoints("TRADING", "FUND")
	if err != nil || from != "6" || to != "18" {
		t.Fatalf("TRADING->FUND: from=%s to=%s err=%v", from, to, err)
	}
	_, _, err = mapOKXTransferEndpoints("X", "Y")
	if err == nil {
		t.Fatal("expected error for unknown pair")
	}
}
