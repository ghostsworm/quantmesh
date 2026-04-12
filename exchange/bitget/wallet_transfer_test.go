package bitget

import "testing"

func TestMapBitgetTransferWalletTypes(t *testing.T) {
	f, to, err := mapBitgetTransferWalletTypes("UMFUTURE", "SPOT", "USDT")
	if err != nil || f != "mix_usdt" || to != "spot" {
		t.Fatalf("got %s %s %v", f, to, err)
	}
	f, to, err = mapBitgetTransferWalletTypes("MAIN", "CONTRACT", "USDT")
	if err != nil || f != "spot" || to != "mix_usdt" {
		t.Fatalf("got %s %s %v", f, to, err)
	}
	_, _, err = mapBitgetTransferWalletTypes("SPOT", "SPOT", "USDT")
	if err == nil {
		t.Fatal("expected error")
	}
}
