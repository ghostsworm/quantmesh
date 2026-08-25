package main

import (
	"flag"
	"os"
	"testing"
	"time"

	"quantmesh/plugin"
)

func TestMainValidatesLicense(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	licenseKey, err := plugin.GenerateLicense(
		"premium-grid",
		"customer-1",
		time.Now().Add(24*time.Hour),
		2,
		[]string{"grid", "ai"},
		"",
		"quantmesh-secret-key-2025",
	)
	if err != nil {
		t.Fatalf("GenerateLicense() error = %v", err)
	}

	oldArgs := os.Args
	oldFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldFlags
	})

	os.Args = []string{"license_validator", "-key", licenseKey}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	main()
}
