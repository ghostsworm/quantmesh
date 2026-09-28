package binance

import (
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestVerifiedUniversalTransferID(t *testing.T) {
	tests := []struct {
		name     string
		response *binancesdk.CreateUserUniversalTransferResponse
		want     string
		wantErr  bool
	}{
		{name: "valid ID", response: &binancesdk.CreateUserUniversalTransferResponse{ID: 12345}, want: "12345"},
		{name: "missing ID decodes as zero", response: &binancesdk.CreateUserUniversalTransferResponse{}, wantErr: true},
		{name: "negative ID", response: &binancesdk.CreateUserUniversalTransferResponse{ID: -1}, wantErr: true},
		{name: "nil response", response: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifiedUniversalTransferID(tt.response)
			if (err != nil) != tt.wantErr {
				t.Fatalf("verifiedUniversalTransferID() error=%v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("verifiedUniversalTransferID()=%q, want %q", got, tt.want)
			}
		})
	}
}
