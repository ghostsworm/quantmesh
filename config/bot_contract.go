package config

// ColdBotConfiguration excludes only display metadata and fields supported by
// the runtime hot-application path. New fields remain cold by default.
func ColdBotConfiguration(cfg BotConfig) BotConfig {
	cfg.ID = BotIDOrGenerate(cfg)
	cfg.Direction = cfg.GetDirection()
	cfg.MarketType = cfg.GetMarketType()
	cfg.UseSpotMargin = false
	cfg.SpotInventoryPolicy = NormalizeSpotInventoryPolicy(cfg.SpotInventoryPolicy)
	cfg.PriceInterval, cfg.ProfitSpread, cfg.OrderQuantity = 0, 0, 0
	cfg.BuyWindowSize, cfg.SellWindowSize = 0, 0
	cfg.OpenPositionControl = OpenPositionControl{}
	cfg.GridRiskControl = GridRiskControl{}
	cfg.Name, cfg.CreatedAt = "", ""
	return cfg
}
