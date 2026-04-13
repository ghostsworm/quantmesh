package utils

import "os"

// SendTelemetry 保留 API，已不再向任何第三方發送數據。
func SendTelemetry(event string, version string) error {
	return SendTelemetryWithProperties(event, version, nil)
}

// SendTelemetryWithProperties 保留 API；後端遙測（原 PostHog）已移除，此處立即返回。
func SendTelemetryWithProperties(event string, version string, properties map[string]interface{}) error {
	_ = event
	_ = version
	_ = properties
	return nil
}

// SendInstallTelemetry 保留 API，無操作。
func SendInstallTelemetry(version string) {
	_ = version
}

// SendStartupTelemetry 保留 API，無操作。
func SendStartupTelemetry(version string) {
	_ = version
}

func getVersion(version string) string {
	if version != "" {
		return version
	}
	if v := os.Getenv("QUANTMESH_VERSION"); v != "" {
		return v
	}
	return "unknown"
}

// TrackExchangeUsage 保留 API，無操作。
func TrackExchangeUsage(version string, exchangeName string, symbol string) {
	_ = getVersion(version)
	_ = exchangeName
	_ = symbol
}

// TrackAPILatency 保留 API，無操作。
func TrackAPILatency(version string, exchangeName string, apiMethod string, latencyMs int64, success bool) {
	_ = getVersion(version)
	_ = exchangeName
	_ = apiMethod
	_ = latencyMs
	_ = success
}

// TrackWebSocketLatency 保留 API，無操作。
func TrackWebSocketLatency(version string, exchangeName string, latencyMs int64, messageType string) {
	_ = getVersion(version)
	_ = exchangeName
	_ = latencyMs
	_ = messageType
}

// TrackTradingActivity 保留 API，無操作。
func TrackTradingActivity(version string, exchangeName string, symbol string, side string) {
	_ = getVersion(version)
	_ = exchangeName
	_ = symbol
	_ = side
}
