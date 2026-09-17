package config

// botMarketTypeFromSymbol 將 SymbolConfig.GetMarketType() 的結果映射為 BotConfig.MarketType 原始值。
// spot_margin 是由 market_type=spot + use_spot_margin=true 推導出的有效類型，並非合法的原始值：
// 若直接寫入 BotConfig.MarketType，BotConfig.GetMarketType() 會回退為 futures。
// 因此存 "spot"，並依賴同步複製的 UseSpotMargin 還原為 spot_margin。
func botMarketTypeFromSymbol(effective string) string {
	if effective == "spot_margin" {
		return "spot"
	}
	return effective
}

// MergeBotConfigFileInto 用 BotConfigFile 更新已有的 BotConfig（主配置 cfg.Bots[i]），
// 保留 BotConfigFile 無法承載或請求未填的字段，避免整條覆蓋時清空：
//   - Enabled：BotConfigFile 不含啟停狀態，始終沿用 existing
//   - CreatedAt、ID：請求未填（空字符串）時沿用 existing
func MergeBotConfigFileInto(existing BotConfig, bcf *BotConfigFile) BotConfig {
	merged := ConvertToBotConfig(bcf)
	merged.Enabled = existing.Enabled
	if merged.CreatedAt == "" {
		merged.CreatedAt = existing.CreatedAt
	}
	if merged.ID == "" {
		merged.ID = existing.ID
	}
	return merged
}
