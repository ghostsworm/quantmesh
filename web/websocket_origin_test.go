package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginWebSocketRequest(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		origin     string
		forwarded  string
		wantAccept bool
	}{
		{name: "same http origin with default port", target: "http://app.example/ws", origin: "http://app.example", wantAccept: true},
		{name: "same https origin behind proxy", target: "http://app.example/ws", origin: "https://app.example", forwarded: "https", wantAccept: true},
		{name: "foreign host rejected", target: "http://app.example/ws", origin: "http://attacker.example"},
		{name: "scheme mismatch rejected", target: "https://app.example/ws", origin: "http://app.example"},
		{name: "non-origin URL rejected", target: "http://app.example/ws", origin: "http://app.example/path"},
		{name: "opaque origin rejected", target: "http://app.example/ws", origin: "null"},
		{name: "non-browser client without origin", target: "http://app.example/ws", wantAccept: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", test.forwarded)
			}
			if got := sameOriginWebSocketRequest(req); got != test.wantAccept {
				t.Fatalf("sameOriginWebSocketRequest()=%v, want %v", got, test.wantAccept)
			}
		})
	}
}
