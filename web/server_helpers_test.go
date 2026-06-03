package web

import "testing"

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		min     int
		max     int
		want    int
		wantErr bool
	}{
		{name: "valid", input: "42", min: 1, max: 100, want: 42},
		{name: "clamps low", input: "0", min: 1, max: 100, want: 1},
		{name: "clamps high", input: "200", min: 1, max: 100, want: 100},
		{name: "invalid", input: "abc", min: 1, max: 100, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIntParam(tt.input, tt.min, tt.max)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseIntParam(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
