package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fakePermissionExchange struct {
	exchange.IExchange
	permissions *exchange.APIPermissions
	err         error
}

func (f fakePermissionExchange) CheckAPIPermissions(ctx context.Context) (*exchange.APIPermissions, error) {
	return f.permissions, f.err
}

func TestNewbieRiskCheckItemsCoverRiskBranches(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {Testnet: true},
		},
	}
	cfg.RiskControl.MaxLeverage = 3
	cfg.Trading.PositionSafetyCheck = 60
	cfg.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.1}
	cfg.Trading.Symbols = []config.SymbolConfig{
		{
			Symbol:          "BTCUSDT",
			GridRiskControl: config.GridRiskControl{Enabled: true, StopLossRatio: 0.1},
			WithdrawalPolicy: config.WithdrawalPolicy{
				Enabled: true,
			},
		},
	}

	if got := checkLeverage(cfg); got.Score != 100 || got.Level != "safe" {
		t.Fatalf("safe leverage = %#v", got)
	}
	cfg.RiskControl.MaxLeverage = 7
	if got := checkLeverage(cfg); got.Score != 30 || got.Level != "warning" {
		t.Fatalf("warning leverage = %#v", got)
	}
	cfg.RiskControl.MaxLeverage = 20
	if got := checkLeverage(cfg); got.Score != 0 || got.Level != "danger" {
		t.Fatalf("danger leverage = %#v", got)
	}

	if got := checkStopLoss(cfg); got.Score != 100 {
		t.Fatalf("stop loss all symbols = %#v", got)
	}
	cfg.Trading.Symbols[0].GridRiskControl.Enabled = false
	if got := checkStopLoss(cfg); got.Score != 60 {
		t.Fatalf("stop loss fallback global = %#v", got)
	}
	cfg.Trading.GridRiskControl.Enabled = false
	if got := checkStopLoss(cfg); got.Score != 0 || got.Level != "danger" {
		t.Fatalf("stop loss missing = %#v", got)
	}

	if got := checkMarginBuffer(cfg); got.Score != 100 {
		t.Fatalf("margin safe = %#v", got)
	}
	cfg.Trading.PositionSafetyCheck = 25
	if got := checkMarginBuffer(cfg); got.Score != 60 {
		t.Fatalf("margin warning = %#v", got)
	}
	cfg.Trading.PositionSafetyCheck = 5
	if got := checkMarginBuffer(cfg); got.Score != 0 {
		t.Fatalf("margin danger = %#v", got)
	}

	if got := checkProfitProtection(cfg); got.Score != 100 {
		t.Fatalf("profit protection enabled = %#v", got)
	}
	cfg.Trading.Symbols[0].WithdrawalPolicy.Enabled = false
	if got := checkProfitProtection(cfg); got.Score != 0 || got.Level != "warning" {
		t.Fatalf("profit protection disabled = %#v", got)
	}

	if got := checkEnvironment(cfg); got.Score != 100 || got.Level != "safe" {
		t.Fatalf("testnet environment = %#v", got)
	}
	cfg.Exchanges["binance"] = config.ExchangeConfig{Testnet: false}
	if got := checkEnvironment(cfg); got.Score != 50 || got.Level != "warning" {
		t.Fatalf("live environment = %#v", got)
	}
}

func TestParamAdvisorFeeAndRoundingHelpers(t *testing.T) {
	oldConfig := globalConfig
	t.Cleanup(func() { globalConfig = oldConfig })

	globalConfig = &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {FeeRate: 0.001},
		},
	}
	maker, taker, source := getFeeRateFromConfig("binance")
	if maker != 0.0004 || taker != 0.001 || source != "config" {
		t.Fatalf("config fees = %f %f %s", maker, taker, source)
	}
	maker, taker, source = getFeeRateFromConfig("missing")
	if maker != 0.0002 || taker != 0.0005 || source != "default" {
		t.Fatalf("default fees = %f %f %s", maker, taker, source)
	}

	if got := getExchangeMinOrder("binance"); got != 100 {
		t.Fatalf("binance min order = %f", got)
	}
	if got := getExchangeMinOrder("unknown"); got != 10 {
		t.Fatalf("unknown min order = %f", got)
	}
	if got := roundToSignificant(123.4, 50000); got != 120 {
		t.Fatalf("high price rounding = %f", got)
	}
	if got := roundToSignificant(1.234, 200); got != 1.2 {
		t.Fatalf("mid price rounding = %f", got)
	}
	if got := roundToSignificant(0.012345, 0.5); got != 0.0123 {
		t.Fatalf("low price rounding = %f", got)
	}
	if got := roundOrderQty(11); got != 15 {
		t.Fatalf("roundOrderQty(11) = %f", got)
	}
	if got := roundOrderQty(501); got != 550 {
		t.Fatalf("roundOrderQty(501) = %f", got)
	}

	empty := calculateSuggestions(0, 0, 0, "binance")
	if empty.BreakevenFeeRate != 0 {
		t.Fatalf("zero price suggestion = %#v", empty)
	}
	suggestion := calculateSuggestions(50000, 0, 0, "binance")
	if suggestion.PriceInterval.Min <= 0 || suggestion.OrderQuantity.Min < 100 {
		t.Fatalf("unexpected suggestion = %#v", suggestion)
	}
}

func TestPermissionChecksAndReports(t *testing.T) {
	unsupported := CheckExchangePermissions(context.Background(), struct{ exchange.IExchange }{}, "demo", "BTCUSDT")
	if unsupported.ErrorMessage == "" || !unsupported.IsSecure {
		t.Fatalf("unsupported permission check = %#v", unsupported)
	}

	failing := CheckExchangePermissions(context.Background(), fakePermissionExchange{err: errors.New("api down")}, "demo", "BTCUSDT")
	if !strings.Contains(failing.ErrorMessage, "api down") || !failing.IsSecure {
		t.Fatalf("failing permission check = %#v", failing)
	}

	permissions := &exchange.APIPermissions{
		CanTrade:     true,
		CanWithdraw:  true,
		CanTransfer:  true,
		IPRestricted: false,
	}
	permissions.CalculateSecurityScore()
	checked := CheckExchangePermissions(context.Background(), fakePermissionExchange{permissions: permissions}, "demo", "BTCUSDT")
	if checked.IsSecure || len(checked.Warnings) == 0 {
		t.Fatalf("risky permission check = %#v", checked)
	}

	if report := FormatPermissionReport(nil); report != "没有需要检测的交易所" {
		t.Fatalf("empty report = %q", report)
	}
	report := FormatPermissionReport([]*PermissionCheckResult{
		{
			Exchange:     "demo",
			Symbol:       "BTCUSDT",
			Permissions:  permissions,
			Warnings:     checked.Warnings,
			IsSecure:     false,
			CheckTime:    time.Unix(0, 0),
			ErrorMessage: "",
		},
		{
			Exchange:     "err",
			Symbol:       "ETHUSDT",
			CheckTime:    time.Unix(1, 0),
			ErrorMessage: "not supported",
		},
	})
	for _, expected := range []string{"API 权限安全检测报告", "高风險", "not supported"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report missing %q:\n%s", expected, report)
		}
	}
}
