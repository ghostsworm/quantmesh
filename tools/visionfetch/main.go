// visionfetch downloads checksum-verified data.binance.vision USD-M files.
// It publishes a file only after the official sidecar matches its bytes, and a
// failed object leaves every other object untouched.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"quantmesh/backtest/visionfetch"
)

func main() {
	symbol := flag.String("symbol", "", "one USD-M symbol, for example BTCUSDT")
	from := flag.String("from", "", "first UTC date, inclusive (YYYY-MM-DD)")
	to := flag.String("to", "", "last UTC date, exclusive (YYYY-MM-DD)")
	root := flag.String("root", visionfetch.DefaultRoot, "local directory replay reads")
	funding := flag.Bool("funding", true, "also fetch the overlapping monthly funding files")
	flag.Parse()

	req, err := request(*symbol, *from, *to, *root, *funding)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result, err := visionfetch.Fetch(context.Background(), req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	failed := 0
	for _, outcome := range result.Outcomes {
		line := "visionfetch " + outcome.Status + " " + outcome.Name
		if outcome.Reason != "" {
			line += " " + outcome.Reason
			failed++
		}
		fmt.Println(line)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "visionfetch: %d failed\n", failed)
		os.Exit(3)
	}
}

func calendarDate(flagName, value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("visionfetch: bad %s %q", flagName, value)
	}
	return parsed, nil
}

func request(symbol, from, to, root string, funding bool) (visionfetch.Request, error) {
	start, err := calendarDate("-from", from)
	if err != nil {
		return visionfetch.Request{}, err
	}
	end, err := calendarDate("-to", to)
	if err != nil {
		return visionfetch.Request{}, err
	}
	if !end.After(start) {
		return visionfetch.Request{}, fmt.Errorf("visionfetch: -to %s is not after -from %s", to, from)
	}
	return visionfetch.Request{
		Root: root, Symbol: symbol, Funding: funding,
		From: start.UTC(), To: end.UTC(),
		Client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}
