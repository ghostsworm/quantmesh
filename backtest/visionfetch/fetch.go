// Package visionfetch downloads official data.binance.vision USD-M files
// into a local directory. A file becomes visible to visionfeed.Load only
// after its official checksum sidecar verifies. A failed object publishes
// nothing and leaves every other object untouched.
package visionfetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"quantmesh/backtest/visionfeed"
)

const (
	// Host is the only host this fetcher accepts.
	Host = "data.binance.vision"
	// DefaultRoot is the gitignored directory replay reads.
	DefaultRoot = visionfeed.DefaultRoot

	kindKline   = "kline"
	kindFunding = "funding"

	reason404      = "404"
	reasonChecksum = "checksum"
	reasonHTTP     = "http"
	reasonRedirect = "redirect"
	reasonLocal    = "local"
	reasonCoverage = "coverage"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,32}$`)

// Object is one official daily kline or monthly funding file.
type Object struct {
	Kind      string
	Label     string
	Directory string
	Name      string
	URL       string
}

// Outcome is one planned object's result. Status is "ok", "skip", or "fail".
type Outcome struct {
	Name   string
	Status string
	Reason string
}

// Request is one explicit fetch. To is exclusive.
type Request struct {
	Root    string
	Symbol  string
	From    time.Time
	To      time.Time
	Funding bool
	Client  *http.Client
}

// Result reports every planned object. Published is false when any failed,
// and then every file this call created has been removed.
type Result struct {
	Outcomes  []Outcome
	Published bool
}

// Fetch downloads the planned objects. It never follows a redirect and never
// replaces a file that is already present.
func Fetch(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !symbolPattern.MatchString(req.Symbol) {
		return Result{}, fmt.Errorf("visionfetch: symbol %q must match %s", req.Symbol, symbolPattern)
	}
	if !req.From.Equal(req.From.UTC()) || !req.To.Equal(req.To.UTC()) || req.From.IsZero() || !req.To.After(req.From) {
		return Result{}, fmt.Errorf("visionfetch: range [%s,%s) is not a positive UTC span", req.From.Format(time.RFC3339), req.To.Format(time.RFC3339))
	}
	if req.Client == nil {
		return Result{}, fmt.Errorf("visionfetch: http client is required")
	}
	objects, err := plan(req)
	if err != nil {
		return Result{}, err
	}
	result := Result{Published: true}
	var created []string
	for _, object := range objects {
		outcome, paths, err := fetchOne(ctx, req.Client, req.Root, object)
		if err != nil {
			removeAll(created)
			return Result{}, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
		if outcome.Status == "fail" {
			result.Published = false
		}
		created = append(created, paths...)
	}
	if !result.Published {
		removeAll(created)
	}
	return result, nil
}

func plan(req Request) ([]Object, error) {
	var objects []Object
	for day := req.From; day.Before(req.To); day = day.AddDate(0, 0, 1) {
		label := day.UTC().Format("2006-01-02")
		if _, err := time.Parse("2006-01-02", label); err != nil {
			return nil, fmt.Errorf("visionfetch: bad date %s", label)
		}
		name := fmt.Sprintf("%s-1m-%s.zip", req.Symbol, label)
		objects = append(objects, Object{
			Kind: kindKline, Label: label, Name: name,
			Directory: filepath.Join(req.Root, req.Symbol, "1m"),
			URL:       officialKlineURL(req.Symbol, label),
		})
	}
	if req.Funding {
		seen := map[string]bool{}
		for day := req.From; day.Before(req.To); day = day.AddDate(0, 0, 1) {
			month := day.UTC().Format("2006-01")
			if seen[month] {
				continue
			}
			seen[month] = true
			name := fmt.Sprintf("%s-fundingRate-%s.zip", req.Symbol, month)
			objects = append(objects, Object{
				Kind: kindFunding, Label: month, Name: name,
				Directory: filepath.Join(req.Root, req.Symbol, "fundingRate"),
				URL:       officialFundingURL(req.Symbol, month),
			})
		}
	}
	return objects, nil
}

func symbolOf(object Object) string {
	return strings.SplitN(object.Name, "-", 2)[0]
}

func officialKlineURL(symbol, day string) string {
	return "https://" + Host + "/data/futures/um/daily/klines/" + symbol + "/1m/" + symbol + "-1m-" + day + ".zip"
}

func officialFundingURL(symbol, month string) string {
	return "https://" + Host + "/data/futures/um/monthly/fundingRate/" + symbol + "/" + symbol + "-fundingRate-" + month + ".zip"
}

func fetchOne(ctx context.Context, client *http.Client, root string, object Object) (Outcome, []string, error) {
	rel := relPath(object)
	existing, state := localState(object)
	switch state {
	case "verified":
		return Outcome{Name: relName(root, existing), Status: "skip"}, nil, nil
	case "present":
		return Outcome{Name: rel, Status: "fail", Reason: reasonLocal}, nil, nil
	}

	stage := filepath.Join(object.Directory, ".partial")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return Outcome{}, nil, fmt.Errorf("visionfetch: create %s: %w", stage, err)
	}
	sidecar := filepath.Join(stage, object.Name+".CHECKSUM")
	data := filepath.Join(stage, object.Name)
	defer os.Remove(sidecar)
	defer os.Remove(data)

	body, status, err := getExact(ctx, client, object.URL+".CHECKSUM")
	if err != nil {
		reason := reasonHTTP
		if strings.Contains(err.Error(), "redirect refused") {
			reason = reasonRedirect
		}
		return Outcome{Name: rel, Status: "fail", Reason: reason}, nil, nil
	}
	if status == http.StatusNotFound {
		return Outcome{Name: rel, Status: "fail", Reason: reason404}, nil, nil
	}
	if status != http.StatusOK {
		return Outcome{Name: rel, Status: "fail", Reason: reasonHTTP}, nil, nil
	}
	if err := os.WriteFile(sidecar, body, 0o644); err != nil {
		return Outcome{}, nil, err
	}
	body, status, err = getExact(ctx, client, object.URL)
	if err != nil || status != http.StatusOK {
		reason := reasonHTTP
		if status == http.StatusNotFound {
			reason = reason404
		}
		return Outcome{Name: rel, Status: "fail", Reason: reason}, nil, nil
	}
	if err := os.WriteFile(data, body, 0o644); err != nil {
		return Outcome{}, nil, err
	}
	if _, err := verifiedSidecar(data, body); err != nil {
		return Outcome{Name: rel, Status: "fail", Reason: reasonChecksum}, nil, nil
	}
	if object.Kind == kindKline {
		if err := visionfeed.CompleteUTCDay(data, symbolOf(object)); err != nil {
			return Outcome{Name: rel, Status: "fail", Reason: reasonCoverage}, nil, nil
		}
	}
	if err := visionfeed.PublishVerifiedFile(object.Directory, data, sidecar); err != nil {
		return Outcome{Name: rel, Status: "fail", Reason: reasonLocal}, nil, nil
	}
	return Outcome{Name: rel, Status: "ok"}, []string{filepath.Join(object.Directory, object.Name)}, nil
}

// localState reports "absent", "verified", or "present".
func localState(object Object) (string, string) {
	var present []string
	for _, ext := range []string{".zip", ".csv"} {
		name := strings.TrimSuffix(object.Name, ".zip") + ext
		if _, err := os.Stat(filepath.Join(object.Directory, name)); err == nil {
			present = append(present, name)
		}
	}
	switch len(present) {
	case 0:
		return "", "absent"
	case 1:
		path := filepath.Join(object.Directory, present[0])
		if _, err := verifiedLocalFile(path); err != nil {
			return path, "present"
		}
		if object.Kind == kindKline {
			if err := visionfeed.CompleteUTCDay(path, symbolOf(object)); err != nil {
				return path, "present"
			}
		}
		return path, "verified"
	default:
		return "", "present"
	}
}

func verifiedLocalFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return verifiedSidecar(path, body)
}

func verifiedSidecar(path string, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	sidecar, err := os.ReadFile(path + ".CHECKSUM")
	if err != nil {
		return "", err
	}
	base := filepath.Base(path)
	matched := false
	for _, line := range strings.Split(string(sidecar), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "  ", 2)
		if len(fields) != 2 || len(fields[0]) != 64 || strings.Contains(fields[1], "/") {
			return "", fmt.Errorf("visionfetch: checksum: malformed line")
		}
		for _, r := range fields[0] {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return "", fmt.Errorf("visionfetch: checksum: malformed line")
			}
		}
		if fields[1] != base {
			continue
		}
		matched = true
		if fields[0] != want {
			return "", fmt.Errorf("visionfetch: checksum: hash mismatch")
		}
	}
	if !matched {
		return "", fmt.Errorf("visionfetch: checksum: basename not listed")
	}
	return want, nil
}

func getExact(ctx context.Context, client *http.Client, rawURL string) ([]byte, int, error) {
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("visionfetch: redirect refused")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := noRedirect.Do(req)
	if err != nil {
		var target *url.Error
		if errors.As(err, &target) && strings.Contains(target.Err.Error(), "redirect refused") {
			return nil, 0, fmt.Errorf("visionfetch: redirect refused for %s", rawURL)
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.String() != rawURL {
		return nil, 0, fmt.Errorf("visionfetch: response URL %v is not %s", resp.Request.URL, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

func relPath(object Object) string {
	kind := "1m"
	if object.Kind == kindFunding {
		kind = "fundingRate"
	}
	return filepath.Base(filepath.Dir(object.Directory)) + "/" + kind + "/" + object.Name
}

func relName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}

func removeAll(paths []string) {
	for _, path := range paths {
		os.Remove(path)
		os.Remove(path + ".CHECKSUM")
	}
}
