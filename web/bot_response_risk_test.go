package web

import (
	"encoding/json"
	"testing"
)

func TestBotResponse_JSON_risk_trigger_message(t *testing.T) {
	r := BotResponse{
		BotID:              "binance:btcusdt:futures",
		RiskTriggered:      true,
		RiskTriggerMessage: "價格1.23%低於均線/量×2.0; depth msg",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out["risk_trigger_message"] != r.RiskTriggerMessage {
		t.Fatalf("risk_trigger_message: got %v", out["risk_trigger_message"])
	}
}
