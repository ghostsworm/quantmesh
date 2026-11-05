// replaywf runs isolated synthetic acceptance samples without network or keys.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quantmesh/backtest/optimizer"
	"quantmesh/backtest/optimizer/replayfixture"
)

func main() {
	output := flag.String("output", "", "new directory for frozen data and experiment JSON")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(2)
	}
	if err := run(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeJSON(path string, value interface{}) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(b, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func run(output string) error {
	if err := os.Mkdir(output, 0700); err != nil {
		return fmt.Errorf("create new experiment directory: %w", err)
	}
	for _, regime := range replayfixture.Regimes {
		candles := replayfixture.Candles(regime)
		space := optimizer.UniversalSearchSpace{Strategy: "grid", ReplayWalkForward: replayfixture.Config(), Ranges: map[string]optimizer.ParamRange{"grid_spacing": {Min: 15, Max: 30, Step: 15}}}
		result, err := (&optimizer.UniversalOptimizer{}).Run(context.Background(), "fixture_"+regime, "BTCUSDT", replayfixture.Interval, candles, space, replayfixture.Capital, nil)
		if err != nil {
			return fmt.Errorf("%s sample: %w", regime, err)
		}
		if err := writeJSON(filepath.Join(output, regime+"-candles.json"), candles); err != nil {
			return err
		}
		request := struct {
			Strategy     string                         `json:"strategy"`
			Symbol       string                         `json:"symbol"`
			Interval     string                         `json:"interval"`
			StartTime    time.Time                      `json:"start_time"`
			EndTime      time.Time                      `json:"end_time"`
			TotalCapital float64                        `json:"total_capital"`
			SearchSpace  optimizer.UniversalSearchSpace `json:"search_space"`
		}{"grid", "BTCUSDT", replayfixture.Interval, time.UnixMilli(candles[0].Timestamp).UTC(), time.UnixMilli(candles[len(candles)-1].Timestamp + 3600000).UTC(), replayfixture.Capital, space}
		if err := writeJSON(filepath.Join(output, regime+"-request.json"), request); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(output, regime+"-result.json"), result); err != nil {
			return err
		}
	}
	return nil
}
