package huobi

import "testing"

func TestMapHuobiTransferDirection(t *testing.T) {
	out, err := mapHuobiTransferDirection("SPOT", "UMFUTURE")
	if err != nil || !out {
		t.Fatalf("SPOT->UMFUTURE want spot->futures, got out=%v err=%v", out, err)
	}
	out, err = mapHuobiTransferDirection("UMFUTURE", "MAIN")
	if err != nil || out {
		t.Fatalf("UMFUTURE->MAIN want futures->spot, got out=%v err=%v", out, err)
	}
	_, err = mapHuobiTransferDirection("SPOT", "SPOT")
	if err == nil {
		t.Fatal("expected error for SPOT->SPOT")
	}
}
