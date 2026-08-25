package main

import (
	"flag"
	"os"
	"testing"
)

func TestMainGeneratesLicense(t *testing.T) {
	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})

	os.Args = []string{
		"license_generator",
		"-plugin", "premium-grid",
		"-customer", "customer-1",
		"-days", "30",
		"-instances", "2",
		"-features", "grid,ai",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	main()
}
