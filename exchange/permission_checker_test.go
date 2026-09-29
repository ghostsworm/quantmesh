package exchange

import "testing"

func TestAPIPermissionsValidateForTrading(t *testing.T) {
	tests := []struct {
		name        string
		permissions *APIPermissions
		wantErr     bool
	}{
		{name: "nil evidence", wantErr: true},
		{name: "trade disabled", permissions: &APIPermissions{}, wantErr: true},
		{name: "withdraw enabled", permissions: &APIPermissions{CanTrade: true, CanWithdraw: true}, wantErr: true},
		{name: "transfer enabled", permissions: &APIPermissions{CanTrade: true, CanTransfer: true}, wantErr: true},
		{name: "trade only", permissions: &APIPermissions{CanTrade: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.permissions.ValidateForTrading()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateForTrading() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := tt.permissions.IsSecure(); got == tt.wantErr {
				t.Fatalf("IsSecure() = %v, want %v", got, !tt.wantErr)
			}
		})
	}
}
