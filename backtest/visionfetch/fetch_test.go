package visionfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/backtest/visionfeed"
)

type scriptedTransport struct {
	got  []string
	body map[string]scriptedResponse
}

type scriptedResponse struct {
	status int
	body   []byte
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.got = append(s.got, req.URL.String())
	resp, ok := s.body[req.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody, Request: req, Header: make(http.Header)}, nil
	}
	return &http.Response{StatusCode: resp.status, Body: ioNopCloser(resp.body), Request: req, Header: make(http.Header)}, nil
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }

func ioNopCloser(body []byte) nopCloser { return nopCloser{strings.NewReader(string(body))} }

func checksumBody(name string, data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
}

func servePair(s *scriptedTransport, rawURL, name string, data []byte) {
	if s.body == nil {
		s.body = map[string]scriptedResponse{}
	}
	s.body[rawURL+".CHECKSUM"] = scriptedResponse{status: http.StatusOK, body: checksumBody(name, data)}
	s.body[rawURL] = scriptedResponse{status: http.StatusOK, body: data}
}

func testRequest(root string, client *http.Client) Request {
	return Request{
		Root: root, Symbol: "TESTUSDT", Funding: true, Client: client,
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}
}

func TestFetchSkipsVerifiedAndPublishesTheRest(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-01.zip")
	os.MkdirAll(filepath.Dir(first), 0o755)
	os.WriteFile(first, []byte("day-one"), 0o644)
	os.WriteFile(first+".CHECKSUM", checksumBody("TESTUSDT-1m-2026-01-01.zip", []byte("day-one")), 0o644)

	transport := &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-02"), "TESTUSDT-1m-2026-01-02.zip", []byte("day-two"))
	servePair(transport, officialFundingURL("TESTUSDT", "2026-01"), "TESTUSDT-fundingRate-2026-01.zip", []byte("funding"))
	client := &http.Client{Transport: transport}

	result, err := Fetch(context.Background(), testRequest(root, client))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published {
		t.Fatalf("not published: %+v", result.Outcomes)
	}
	for _, url := range transport.got {
		if strings.Contains(url, "2026-01-01") {
			t.Fatalf("verified day was requested: %s", url)
		}
	}
	loaded, err := visionfeed.Load(context.Background(), visionfeed.LoadRequest{
		Root: root, Symbol: "TESTUSDT", FirstMs: 1767225600000, EndMs: 1767225600000 + 2*86400000,
	})
	if err == nil {
		t.Fatal("a two-byte stand-in must not pass as a full day")
	}
	if !strings.Contains(err.Error(), "visionfeed:") {
		t.Fatal(err)
	}
	_ = loaded
	second, err := os.ReadFile(filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-02.zip"))
	if err != nil || string(second) != "day-two" {
		t.Fatalf("published bytes = %q (%v)", second, err)
	}
	if _, err := os.Stat(filepath.Join(root, "TESTUSDT", "1m", ".partial")); err != nil {
		t.Fatal("stage directory missing")
	}
}

func TestFetchRemovesAPartialFundingMonth(t *testing.T) {
	root := t.TempDir()
	transport := &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-01"), "TESTUSDT-1m-2026-01-01.zip", []byte("day"))
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-02"), "TESTUSDT-1m-2026-01-02.zip", []byte("day"))
	client := &http.Client{Transport: transport}
	req := testRequest(root, client)
	req.To = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	req.From = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published {
		t.Fatal("a missing funding month was treated as complete")
	}
	if _, err := os.Stat(filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-01.zip")); !os.IsNotExist(err) {
		t.Fatal("a kline file stayed visible after the batch failed")
	}
	if _, err := os.Stat(filepath.Join(root, "TESTUSDT", "fundingRate", "TESTUSDT-fundingRate-2026-01.zip")); !os.IsNotExist(err) {
		t.Fatal("a funding file was published without its month")
	}
}

func TestFetchLeavesAVerifiedFileUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-01.zip")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("original"), 0o644)
	os.WriteFile(path+".CHECKSUM", checksumBody("TESTUSDT-1m-2026-01-01.zip", []byte("original")), 0o644)

	transport := &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-01"), "TESTUSDT-1m-2026-01-01.zip", []byte("replacement"))
	req := testRequest(root, &http.Client{Transport: transport})
	req.Funding = false
	req.To = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.Outcomes[0].Status != "skip" {
		t.Fatalf("verified file was not skipped: %+v", result.Outcomes)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatalf("verified bytes = %q (%v)", got, err)
	}
	if len(transport.got) != 0 {
		t.Fatalf("verified file was requested: %v", transport.got)
	}
}

func TestFetchKeepsExistingBytesOnChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-02.csv")
	os.MkdirAll(filepath.Dir(existing), 0o755)
	os.WriteFile(existing, []byte("keep-me"), 0o644)

	transport := &scriptedTransport{}
	transport.body = map[string]scriptedResponse{
		officialKlineURL("TESTUSDT", "2026-01-02") + ".CHECKSUM": {status: http.StatusOK, body: []byte(strings.Repeat("ab", 32) + "  other.zip\n")},
	}
	client := &http.Client{Transport: transport}
	req := testRequest(root, client)
	req.Funding = false

	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published {
		t.Fatal("a bad checksum was published")
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "keep-me" {
		t.Fatalf("existing bytes = %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-02.zip")); !os.IsNotExist(err) {
		t.Fatal("a mismatched checksum published a zip next to the existing csv")
	}
}

func TestFetchRefusesARedirect(t *testing.T) {
	root := t.TempDir()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound, Request: req, Header: http.Header{"Location": {"https://evil.example/fake.zip"}},
			Body: http.NoBody,
		}, nil
	})}
	req := testRequest(root, client)
	req.Funding = false
	req.To = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published || result.Outcomes[0].Reason != reasonRedirect {
		t.Fatalf("redirect published: %+v", result.Outcomes)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "TESTUSDT", "1m"))
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("redirect left %s", entry.Name())
		}
	}
}

func TestFetchRejectsBadInputBeforeAnyRequest(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}
	req := testRequest(t.TempDir(), client)
	req.Symbol = "btcusdt"
	if _, err := Fetch(context.Background(), req); err == nil {
		t.Fatal("lowercase symbol accepted")
	}
	req.Symbol = "TESTUSDT"
	req.To = req.From
	if _, err := Fetch(context.Background(), req); err == nil {
		t.Fatal("empty range accepted")
	}
	if called {
		t.Fatal("a rejected request used the network")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
