package ai

import "testing"

func TestParsePolymarketJSON(t *testing.T) {
	raw := `{"signal":"bullish","strength":0.7,"confidence":0.8,"reasoning":"test","signals":[{"question":"q","signal":"neutral","probability":0.5,"strength":0.5,"confidence":0.6,"reasoning":"r","relevance":"macro"}]}`
	a, err := parsePolymarketJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signal != "bullish" || len(a.Signals) != 1 {
		t.Fatalf("%+v", a)
	}
}

func TestParsePolymarketJSON_MarkdownFence(t *testing.T) {
	raw := "Here is JSON:\n```\n{\"signal\":\"neutral\",\"strength\":0.1,\"confidence\":0.2,\"reasoning\":\"x\"}\n```"
	a, err := parsePolymarketJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signal != "neutral" {
		t.Fatal(a.Signal)
	}
}
