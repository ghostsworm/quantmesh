package bitget

import "testing"

func TestBitgetOrderFeeAuthorityRequiresCompleteFeeDetail(t *testing.T) {
	tests := []struct {
		name string
		fee  interface{}
		want bool
	}{
		{name: "explicit zero fee", fee: []interface{}{map[string]interface{}{"fee": "0", "feeCoin": "USDT"}}, want: true},
		{name: "valid fee", fee: []interface{}{map[string]interface{}{"fee": "0.01", "feeCoin": "USDT"}}, want: true},
		{name: "missing fee detail", fee: nil},
		{name: "missing asset for positive fee", fee: []interface{}{map[string]interface{}{"fee": "0.01"}}},
		{name: "invalid amount", fee: []interface{}{map[string]interface{}{"fee": "NaN", "feeCoin": "USDT"}}},
		{name: "multiple fee assets", fee: []interface{}{map[string]interface{}{"fee": "0.01", "feeCoin": "USDT"}, map[string]interface{}{"fee": "0.001", "feeCoin": "BTC"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := (&WebSocketManager{}).parseOrderUpdate(map[string]interface{}{
				"orderId": "1", "clientOid": "cid", "instId": "BTCUSDT", "side": "buy", "status": "partially_filled",
				"price": "100", "size": "1", "accBaseVolume": "0.5", "priceAvg": "100", "feeDetail": tt.fee,
			})
			if update == nil || update.CommissionKnown != tt.want {
				t.Fatalf("CommissionKnown=%v, want %v; update=%+v", update != nil && update.CommissionKnown, tt.want, update)
			}
		})
	}
}
