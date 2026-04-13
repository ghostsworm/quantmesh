package monitor

import (
	"testing"

	"quantmesh/config"
)

func TestNewsMonitorApplyRuntimeConfigDisablesWithoutRestart(t *testing.T) {
	cfg := config.CreateMinimalConfig()
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = "" // NewsCollector 不啟動定時（無 key）
	cfg.NewsMonitor.EnableAnalysis = func() *bool { b := false; return &b }()

	nm := NewNewsMonitor(cfg, nil)
	if err := nm.Start(); err != nil {
		t.Fatal(err)
	}
	if !nm.isRunning {
		t.Fatal("expected running")
	}

	cfg2 := config.CreateMinimalConfig()
	cfg2.NewsMonitor.Enabled = false
	nm.ApplyRuntimeConfig(cfg2)

	if nm.isRunning {
		t.Fatal("expected stopped after disable")
	}
	if nm.cfg.NewsMonitor.Enabled {
		t.Fatal("cfg should have enabled=false")
	}
}

func TestNewsMonitorApplyRuntimeConfigUpdatesCfgPointer(t *testing.T) {
	cfg := config.CreateMinimalConfig()
	cfg.NewsMonitor.Enabled = false
	nm := NewNewsMonitor(cfg, nil)
	cfg2 := config.CreateMinimalConfig()
	cfg2.NewsMonitor.Enabled = false
	cfg2.NewsMonitor.CheckInterval = "10m"
	nm.ApplyRuntimeConfig(cfg2)
	if nm.cfg.NewsMonitor.CheckInterval != "10m" {
		t.Fatalf("got %q", nm.cfg.NewsMonitor.CheckInterval)
	}
}
