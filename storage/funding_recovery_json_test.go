package storage

import (
	"encoding/json"
	"testing"
)

func TestCanonicalRecoveryJSONEquivalentObjectRepresentations(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "reordered keys", input: `{"z":3,"a":{"y":2,"x":1}}`},
		{name: "whitespace", input: " { \n \"a\" : { \"x\" : 1 , \"y\" : 2 } , \"z\" : 3 } \t\n"},
	}
	want := `{"a":{"x":1e0,"y":2e0},"z":3e0}`
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonicalRecoveryJSON([]byte(test.input))
			if err != nil {
				t.Fatalf("canonicalRecoveryJSON(%s): %v", test.input, err)
			}
			if string(got) != want {
				t.Fatalf("canonical JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestCanonicalRecoveryJSONNumberEquivalentSpellingsRemainExact(t *testing.T) {
	tests := []struct {
		name      string
		spellings []string
		want      string
	}{
		{name: "one", spellings: []string{"1", "1.0", "1e0", "10e-1"}, want: "1e0"},
		{name: "greater than exact float integer range", spellings: []string{"9007199254740993", "9007199254740993.0", "9.007199254740993e15"}, want: "9007199254740993e0"},
		{name: "uint64 maximum", spellings: []string{"18446744073709551615", "18446744073709551615.0", "1.8446744073709551615e19"}, want: "18446744073709551615e0"},
		{name: "uint64 maximum plus trailing decimal zero", spellings: []string{"184467440737095516150e-1"}, want: "18446744073709551615e0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, spelling := range test.spellings {
				got, err := canonicalRecoveryJSONNumber(spelling)
				if err != nil {
					t.Fatalf("canonicalRecoveryJSONNumber(%q): %v", spelling, err)
				}
				if got != test.want {
					t.Errorf("canonicalRecoveryJSONNumber(%q) = %q, want %q", spelling, got, test.want)
				}
			}
		})
	}
}

func TestCanonicalRecoveryJSONAdjacentLargeIntegersRemainDistinct(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		wantL string
		wantR string
	}{
		{
			name:  "above 2^53",
			left:  `9007199254740992`,
			right: `9007199254740993`,
			wantL: `9007199254740992e0`,
			wantR: `9007199254740993e0`,
		},
		{
			name:  "uint64 maximum and next integer",
			left:  `18446744073709551615`,
			right: `18446744073709551616`,
			wantL: `18446744073709551615e0`,
			wantR: `18446744073709551616e0`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left, err := canonicalRecoveryJSON([]byte(test.left))
			if err != nil {
				t.Fatalf("canonicalize lower integer: %v", err)
			}
			right, err := canonicalRecoveryJSON([]byte(test.right))
			if err != nil {
				t.Fatalf("canonicalize adjacent integer: %v", err)
			}
			if string(left) == string(right) {
				t.Fatalf("adjacent integers collapsed to the same canonical value: %s", left)
			}
			if string(left) != test.wantL || string(right) != test.wantR {
				t.Fatalf("large integer canonicalization lost precision: lower=%s adjacent=%s", left, right)
			}
		})
	}
}

func TestCanonicalRecoveryJSONArrayOrderIsSignificant(t *testing.T) {
	forward, err := canonicalRecoveryJSON([]byte(`[1,2,3]`))
	if err != nil {
		t.Fatal("canonicalize forward array:", err)
	}
	reversed, err := canonicalRecoveryJSON([]byte(`[3,2,1]`))
	if err != nil {
		t.Fatal("canonicalize reversed array:", err)
	}
	if string(forward) == string(reversed) {
		t.Fatalf("array order was discarded: both canonicalized to %s", forward)
	}
	if string(forward) != `[1e0,2e0,3e0]` || string(reversed) != `[3e0,2e0,1e0]` {
		t.Fatalf("canonical array order mismatch: forward=%s reversed=%s", forward, reversed)
	}
}

func TestCanonicalRecoveryJSONRejectsDuplicateKeysAndTrailingValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "duplicate top-level key", input: `{"revision":1,"revision":2}`},
		{name: "duplicate nested key", input: `{"episode":{"revision":1,"revision":2}}`},
		{name: "trailing object", input: `{"revision":1}{"revision":2}`},
		{name: "trailing scalar", input: `{"revision":1} true`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, err := canonicalRecoveryJSON([]byte(test.input)); err == nil {
				t.Fatalf("canonicalRecoveryJSON accepted invalid input and returned %s", got)
			}
		})
	}
	if got, err := canonicalRecoveryJSON([]byte("{\"revision\":1} \n\t")); err != nil || string(got) != `{"revision":1e0}` {
		t.Fatalf("legal trailing whitespace rejected or changed: got=%s err=%v", got, err)
	}
}

func TestCanonicalRecoveryJSONPreservesAmountAndTimestampStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "amount string scale is preserved",
			input: `{"amount":"1.0"}`,
			want:  `{"amount":"1.0"}`,
		},
		{
			name:  "different amount string scale remains different",
			input: `{"amount":"1.00"}`,
			want:  `{"amount":"1.00"}`,
		},
		{
			name:  "timestamp string spelling is preserved",
			input: `{"observed_at":"2026-10-13T12:34:56.123456789Z"}`,
			want:  `{"observed_at":"2026-10-13T12:34:56.123456789Z"}`,
		},
		{
			name:  "long amount timestamp and uint64 payload",
			input: `{"amount":"9007199254740993.000000000000000001","observed_at":"2026-10-13T12:34:56.123456789Z","revision":18446744073709551615}`,
			want:  `{"amount":"9007199254740993.000000000000000001","observed_at":"2026-10-13T12:34:56.123456789Z","revision":18446744073709551615e0}`,
		},
	}
	canonicalByInput := make(map[string]string, len(tests))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonicalRecoveryJSON([]byte(test.input))
			if err != nil {
				t.Fatalf("canonicalRecoveryJSON: %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("canonical JSON altered string semantics or integer precision: got=%s want=%s", got, test.want)
			}
			canonicalByInput[test.input] = string(got)
		})
	}
	if canonicalByInput[`{"amount":"1.0"}`] == canonicalByInput[`{"amount":"1.00"}`] {
		t.Fatal("business amount strings with different scale were treated as equivalent")
	}
}

func TestVerifyFundingRecoveryProjectionHashRejectsUnknownFieldEvenWithMatchingSemanticHash(t *testing.T) {
	known, err := json.Marshal(fundingRecoveryProjection{})
	if err != nil {
		t.Fatal("marshal known projection shape:", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(known, &object); err != nil {
		t.Fatal("decode known projection shape:", err)
	}
	object["unknown_future_field"] = json.RawMessage(`{"value":"must-not-be-dropped"}`)
	payload, err := json.Marshal(object)
	if err != nil {
		t.Fatal("marshal projection with unknown field:", err)
	}
	matchingHash, err := fundingRecoveryProjectionHash(payload)
	if err != nil {
		t.Fatal("hash projection including unknown field:", err)
	}
	if err := VerifyFundingRecoveryProjectionHash(payload, matchingHash); err == nil {
		t.Fatal("projection verifier accepted an unknown field hidden behind a matching semantic hash")
	}
}
