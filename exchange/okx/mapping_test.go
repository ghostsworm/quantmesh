package okx

import "testing"

func TestToNativeMapping(t *testing.T) {
	tests := []struct {
		side, typ, tif string
		postOnly       bool
		wantSide       Side
		wantType       OrderType
		wantErr        bool
	}{
		{"BUY", "LIMIT", "GTC", false, SideBuy, OrderTypeLimit, false},
		{"SELL", "LIMIT", "", true, SideSell, OrderTypePostOnly, false},
		{"BUY", "LIMIT", "GTX", false, SideBuy, OrderTypePostOnly, false},
		{"SELL", "MARKET", "", false, SideSell, OrderTypeMarket, false},
		{"BUY", "MARKET", "", true, SideBuy, "", true},
		{"BUY", "STOP", "", false, SideBuy, "", true},
	}
	for _, tt := range tests {
		side, err := ToNativeSide(tt.side)
		if err != nil || side != tt.wantSide {
			t.Fatalf("ToNativeSide(%s)=%v,%v", tt.side, side, err)
		}
		typ, err := ToNativeOrderType(tt.typ, tt.postOnly, tt.tif)
		if (err != nil) != tt.wantErr || typ != tt.wantType {
			t.Fatalf("ToNativeOrderType(%s,%v,%s)=%v,%v", tt.typ, tt.postOnly, tt.tif, typ, err)
		}
	}
	if _, err := ToNativeSide("HOLD"); err == nil {
		t.Fatal("未知方向應報錯")
	}
}

func TestToInternalMapping(t *testing.T) {
	statuses := map[OrderStatus]string{
		"live":             "NEW",
		"partially_filled": "PARTIALLY_FILLED",
		"filled":           "FILLED",
		"canceled":         "CANCELED",
		"mmp_canceled":     "CANCELED",
	}
	for native, want := range statuses {
		got, err := ToInternalStatus(native)
		if err != nil || got != want {
			t.Fatalf("ToInternalStatus(%s)=%v,%v want %s", native, got, err, want)
		}
	}
	if _, err := ToInternalStatus("FILLED"); err == nil {
		t.Fatal("非原生狀態不應透傳")
	}
	if s, err := ToInternalSide("sell"); err != nil || s != "SELL" {
		t.Fatalf("ToInternalSide(sell)=%v,%v", s, err)
	}
	if _, err := ToInternalSide("SELL"); err == nil {
		t.Fatal("非原生方向不應透傳")
	}
	if typ, err := ToInternalOrderType("post_only"); err != nil || typ != "LIMIT" {
		t.Fatalf("post_only → %v,%v", typ, err)
	}
	if _, err := ToInternalOrderType("conditional"); err == nil {
		t.Fatal("未知類型應報錯")
	}
}
