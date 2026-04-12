package kucoin

import "testing"

func TestMapKuCoinTransferDirection(t *testing.T) {
	out, err := mapKuCoinTransferDirection("UMFUTURE", "MAIN")
	if err != nil || !out {
		t.Fatalf("UMFUTURE->MAIN want futures-out, got out=%v err=%v", out, err)
	}
	out, err = mapKuCoinTransferDirection("SPOT", "UMFUTURE")
	if err != nil || out {
		t.Fatalf("SPOT->UMFUTURE want transfer-in, got out=%v err=%v", out, err)
	}
	_, err = mapKuCoinTransferDirection("SPOT", "SPOT")
	if err == nil {
		t.Fatal("expected error for SPOT->SPOT")
	}
}
