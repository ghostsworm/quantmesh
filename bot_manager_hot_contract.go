package main

import (
	"errors"
	"reflect"

	"quantmesh/config"
)

var errRuntimeConfigurationRequiresRestart = errors.New("runtime configuration requires restart")

// Only fields with application code in applyRuntimeTradingParams are hot.
// Clearing that allowlist makes newly introduced config fields cold by default.
func coldRuntimeConfiguration(cfg config.BotConfig) config.BotConfig {
	return config.ColdBotConfiguration(cfg)
}

func runtimeHotContractMatches(previous, next config.BotConfig) bool {
	return reflect.DeepEqual(coldRuntimeConfiguration(previous), coldRuntimeConfiguration(next))
}

// Equality of cold values does not transfer ownership of the caller's maps,
// slices or pointers. Retain runtime-owned cold references and publish only
// applied hot fields, cloning the reference-bearing opening controls.
func runtimeHotCandidate(previous, requested config.BotConfig) config.BotConfig {
	next := previous
	next.PriceInterval = requested.PriceInterval
	next.ProfitSpread = requested.ProfitSpread
	next.OrderQuantity = requested.OrderQuantity
	next.BuyWindowSize = requested.BuyWindowSize
	next.SellWindowSize = requested.SellWindowSize
	next.OpenPositionControl = config.CloneOpenPositionControl(requested.OpenPositionControl)
	next.GridRiskControl = requested.GridRiskControl
	next.Name, next.CreatedAt = requested.Name, requested.CreatedAt
	return next
}
