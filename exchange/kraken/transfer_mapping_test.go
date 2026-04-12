package kraken

import "testing"

func TestMapKrakenTransferToCalls(t *testing.T) {
	spotToFut, err := mapKrakenTransferToCalls("MAIN", "FUTURES")
	if err != nil || !spotToFut {
		t.Fatalf("MAIN->FUTURES want spot API, got spotToFut=%v err=%v", spotToFut, err)
	}
	spotToFut, err = mapKrakenTransferToCalls("UMFUTURE", "SPOT")
	if err != nil || spotToFut {
		t.Fatalf("UMFUTURE->SPOT want futures withdrawal, got spotToFut=%v err=%v", spotToFut, err)
	}
	_, err = mapKrakenTransferToCalls("SPOT", "SPOT")
	if err == nil {
		t.Fatal("expected error for SPOT->SPOT")
	}
}

func TestKrakenSpotAssetCode(t *testing.T) {
	if krakenSpotAssetCode("BTC") != "XBT" {
		t.Fatal("BTC -> XBT")
	}
	if krakenSpotAssetCode("USDT") != "USDT" {
		t.Fatal("USDT")
	}
}
