package web

import (
	"encoding/json"
	"fmt"

	"quantmesh/config"
)

// Patch both risk sections in one durable main snapshot. Never mutate the
// manager's currently published configuration before persistence succeeds.
func persistRiskControlBundle(botID string, rc *config.BotRiskControl, grid *config.GridRiskControl) error {
	if fileConfigManager == nil {
		return fmt.Errorf("configuration persistence is unavailable")
	}
	fcm := fileConfigManager
	if _, err := fcm.GetConfig(); err != nil {
		return err
	}
	fcm.mu.Lock()
	defer fcm.mu.Unlock()
	data, err := json.Marshal(fcm.currentConfig)
	if err != nil {
		return fmt.Errorf("copy risk configuration: %w", err)
	}
	var next config.Config
	if err := json.Unmarshal(data, &next); err != nil {
		return fmt.Errorf("copy risk configuration: %w", err)
	}
	for i := range next.Bots {
		id := next.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(next.Bots[i].Exchange, next.Bots[i].Symbol, next.Bots[i].GetMarketType())
		}
		if id != botID {
			continue
		}
		if rc != nil {
			copy := *rc
			next.Bots[i].OpenPositionControl.BotRiskControl = &copy
		}
		if grid != nil {
			next.Bots[i].GridRiskControl = *grid
		}
		if err := next.Validate(); err != nil {
			return err
		}
		if err := persistAppConfigToDB(&next, "web", "bot_risk_update", "put_bot_risk_control"); err != nil {
			return err
		}
		fcm.currentConfig = &next
		return nil
	}
	return fmt.Errorf("bot missing from authoritative configuration")
}
