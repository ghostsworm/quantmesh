package visionfetch

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func fullDayZip(day time.Time, minutes int) []byte {
	var csv bytes.Buffer
	csv.WriteString("open_time,open,high,low,close,volume\n")
	start := day.UnixMilli()
	for i := 0; i < minutes; i++ {
		fmt.Fprintf(&csv, "%d,100,101,99,100,10\n", start+int64(i)*60000)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("day.csv")
	if err != nil {
		panic(err)
	}
	if _, err := entry.Write(csv.Bytes()); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestFetchPublishesACompleteDayAndDropsAShortOne(t *testing.T) {
	root := t.TempDir()
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	transport := &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-02"), "TESTUSDT-1m-2026-01-02.zip", fullDayZip(day, 1440))
	req := testRequest(root, &http.Client{Transport: transport})
	req.Funding = false
	req.From = day
	req.To = day.AddDate(0, 0, 1)

	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.Outcomes[0].Status != "ok" {
		t.Fatalf("complete day not published: %+v", result.Outcomes)
	}

	short := t.TempDir()
	transport = &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-02"), "TESTUSDT-1m-2026-01-02.zip", fullDayZip(day, 1439))
	req.Root = short
	req.Client = &http.Client{Transport: transport}
	result, err = Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published || result.Outcomes[0].Reason != reasonCoverage {
		t.Fatalf("short day published: %+v", result.Outcomes)
	}
	if _, err := os.Stat(filepath.Join(short, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-02.zip")); !os.IsNotExist(err) {
		t.Fatal("a short day was left where replay can read it")
	}
}

func TestFetchRemovesACompleteDayWhenALaterDayIsShort(t *testing.T) {
	root := t.TempDir()
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	transport := &scriptedTransport{}
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-01"), "TESTUSDT-1m-2026-01-01.zip", fullDayZip(first, 1440))
	servePair(transport, officialKlineURL("TESTUSDT", "2026-01-02"), "TESTUSDT-1m-2026-01-02.zip", fullDayZip(second, 10))
	req := testRequest(root, &http.Client{Transport: transport})
	req.Funding = false

	result, err := Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Published {
		t.Fatal("a range with one short day was published")
	}
	for _, day := range []string{"2026-01-01", "2026-01-02"} {
		path := filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-"+day+".zip")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s stayed after the batch failed: %v", day, err)
		}
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
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	original := fullDayZip(day, 1440)
	path := filepath.Join(root, "TESTUSDT", "1m", "TESTUSDT-1m-2026-01-01.zip")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, original, 0o644)
	os.WriteFile(path+".CHECKSUM", checksumBody("TESTUSDT-1m-2026-01-01.zip", original), 0o644)

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
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("verified bytes changed (%d bytes, %v)", len(got), err)
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
