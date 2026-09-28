package main

import (
	"fmt"
	"math"
	"strings"

	"quantmesh/config"
)

type walletCapitalBot struct {
	id       string
	exchange string
	market   string
	symbol   string
	capital  float64
	orderQty float64
	spread   *config.FundingPerpSpreadConfig
}

// configuredAccountWalletCapitalTotal converts gross Bot allocations into
// commitments against one credential, market account, and quote asset. Pure
// callers infer the candidate asset; runtime callers should use the exchange's
// market metadata through configuredAccountWalletCapitalForQuote.
func configuredAccountWalletCapitalTotal(cfg *config.Config, candidate config.SymbolConfig, walletExchange, walletMarket string) (float64, error) {
	quoteAsset, err := configuredCandidateQuoteAsset(candidate)
	if err != nil {
		return 0, fmt.Errorf("candidate Bot quote asset: %w", err)
	}
	return configuredAccountWalletCapitalForQuote(cfg, candidate, walletExchange, walletMarket, quoteAsset)
}

func configuredCandidateQuoteAsset(candidate config.SymbolConfig) (string, error) {
	if candidate.GetMarketType() == config.MarketTypeFundingPerpSpread {
		if candidate.FundingPerpSpread == nil {
			return "", fmt.Errorf("funding_perp_spread leg configuration is unavailable")
		}
		for _, leg := range []config.FundingPerpLeg{candidate.FundingPerpSpread.LegA, candidate.FundingPerpSpread.LegB} {
			quoteAsset, err := quoteAssetFromConfiguredSymbol(leg.Symbol)
			if err != nil {
				return "", err
			}
			if quoteAsset != "USDT" {
				return "", fmt.Errorf("funding_perp_spread requires USDT quote assets, got %s", quoteAsset)
			}
		}
		return "USDT", nil
	}
	return quoteAssetFromConfiguredSymbol(candidate.Symbol)
}

// Paired strategies use half their gross allocation per leg.
func configuredAccountWalletCapitalForQuote(cfg *config.Config, candidate config.SymbolConfig, walletExchange, walletMarket, walletQuoteAsset string) (float64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("account capital configuration is unavailable")
	}
	walletExchange = strings.TrimSpace(walletExchange)
	walletMarket = strings.ToLower(strings.TrimSpace(walletMarket))
	walletQuoteAsset = strings.ToUpper(strings.TrimSpace(walletQuoteAsset))
	if walletExchange == "" || walletMarket == "" || walletQuoteAsset == "" {
		return 0, fmt.Errorf("wallet exchange, market, and quote asset are required")
	}
	walletCfg, ok := cfg.Exchanges[walletExchange]
	if !ok || strings.TrimSpace(walletCfg.APIKey) == "" {
		return 0, fmt.Errorf("wallet account identity is unavailable")
	}
	walletScope := equityAccountScopeID(walletExchange, walletCfg)
	currentExchange := strings.TrimSpace(cfg.App.CurrentExchange)
	candidateExchange := strings.TrimSpace(candidate.Exchange)
	if candidateExchange == "" {
		candidateExchange = currentExchange
	}
	candidateMarket := candidate.GetMarketType()
	candidateID := strings.TrimSpace(candidate.ID)
	if candidateID == "" {
		if candidateMarket == config.MarketTypeFundingPerpSpread {
			candidateID = config.GenerateBotIDFundingPerpSpread(candidate.FundingPerpSpread)
		} else {
			candidateID = config.GenerateBotID(candidateExchange, candidate.Symbol, candidateMarket)
		}
	}

	bots := make([]walletCapitalBot, 0, len(cfg.Bots)+len(cfg.Trading.Symbols)+1)
	if len(cfg.Bots) > 0 {
		for _, bot := range cfg.Bots {
			exchangeName := strings.TrimSpace(bot.Exchange)
			if exchangeName == "" {
				exchangeName = currentExchange
			}
			bots = append(bots, walletCapitalBot{id: configuredWalletBotID(bot, exchangeName), exchange: exchangeName, symbol: bot.Symbol,
				market: bot.GetMarketType(), capital: bot.TotalAllocatedCapital, orderQty: bot.OrderQuantity,
				spread: bot.FundingPerpSpread})
		}
	} else {
		for _, symbol := range cfg.Trading.Symbols {
			exchangeName := strings.TrimSpace(symbol.Exchange)
			if exchangeName == "" {
				exchangeName = currentExchange
			}
			id := strings.TrimSpace(symbol.ID)
			if id == "" {
				if symbol.GetMarketType() == config.MarketTypeFundingPerpSpread {
					id = config.GenerateBotIDFundingPerpSpread(symbol.FundingPerpSpread)
				} else {
					id = config.GenerateBotID(exchangeName, symbol.Symbol, symbol.GetMarketType())
				}
			}
			bots = append(bots, walletCapitalBot{id: id, exchange: exchangeName, symbol: symbol.Symbol,
				market: symbol.GetMarketType(), capital: symbol.TotalAllocatedCapital, orderQty: symbol.OrderQuantity,
				spread: symbol.FundingPerpSpread})
		}
	}
	if candidateID == "" {
		return 0, fmt.Errorf("candidate Bot identity is unavailable")
	}
	candidateInserted := false
	for i := range bots {
		if bots[i].id == candidateID {
			bots[i] = walletCapitalBot{id: candidateID, exchange: candidateExchange, market: candidateMarket, symbol: candidate.Symbol,
				capital: candidate.TotalAllocatedCapital, orderQty: candidate.OrderQuantity, spread: candidate.FundingPerpSpread}
			candidateInserted = true
			break
		}
	}
	if !candidateInserted {
		bots = append(bots, walletCapitalBot{id: candidateID, exchange: candidateExchange, market: candidateMarket, symbol: candidate.Symbol,
			capital: candidate.TotalAllocatedCapital, orderQty: candidate.OrderQuantity, spread: candidate.FundingPerpSpread})
	}
	seen := make(map[string]struct{}, len(bots))
	total := 0.0
	for _, bot := range bots {
		if bot.id == "" {
			return 0, fmt.Errorf("configured Bot identity is unavailable")
		}
		if _, exists := seen[bot.id]; exists {
			return 0, fmt.Errorf("duplicate configured Bot identity %q prevents wallet commitment verification", bot.id)
		}
		seen[bot.id] = struct{}{}
		fraction, relevant, err := walletCapitalFraction(cfg, bot, walletExchange, walletScope, walletMarket, walletQuoteAsset)
		if err != nil {
			return 0, fmt.Errorf("Bot %s wallet commitment: %w", bot.id, err)
		}
		if !relevant {
			continue
		}
		capital := bot.capital
		if capital == 0 {
			capital = bot.orderQty
		}
		if capital == 0 {
			capital = cfg.Strategies.CapitalAllocation.TotalCapital
		}
		if math.IsNaN(capital) || math.IsInf(capital, 0) || capital <= 0 {
			return 0, fmt.Errorf("Bot %s has no valid capital allocation", bot.id)
		}
		total += capital * fraction
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return 0, fmt.Errorf("configured wallet capital total is invalid")
		}
	}
	return total, nil
}

func configuredWalletBotID(bot config.BotConfig, exchangeName string) string {
	if id := strings.TrimSpace(bot.ID); id != "" {
		return id
	}
	if bot.GetMarketType() == config.MarketTypeFundingPerpSpread {
		return config.GenerateBotIDFundingPerpSpread(bot.FundingPerpSpread)
	}
	return config.GenerateBotID(exchangeName, bot.Symbol, bot.GetMarketType())
}

func walletCapitalFraction(cfg *config.Config, bot walletCapitalBot, walletExchange, walletScope, walletMarket, walletQuoteAsset string) (float64, bool, error) {
	if bot.market == config.MarketTypeFundingCarry {
		botExchange := strings.TrimSpace(bot.exchange)
		botCfg, exists := cfg.Exchanges[botExchange]
		if botExchange == walletExchange && (!exists || strings.TrimSpace(botCfg.APIKey) == "") {
			return 0, false, fmt.Errorf("funding_carry account identity is unavailable")
		}
		if !exists || equityAccountScopeID(botExchange, botCfg) != walletScope || botExchange != walletExchange {
			return 0, false, nil
		}
		if walletQuoteAsset != "USDT" {
			return 0, false, nil
		}
		if walletMarket == "spot" || walletMarket == "futures" || walletMarket == "spot_margin" {
			return 0.5, true, nil
		}
		return 0, false, nil
	}
	if bot.market == config.MarketTypeFundingPerpSpread {
		if bot.spread == nil {
			return 0, false, fmt.Errorf("funding_perp_spread leg configuration is unavailable")
		}
		if walletMarket != "futures" {
			return 0, false, nil
		}
		var fraction float64
		for _, leg := range []config.FundingPerpLeg{bot.spread.LegA, bot.spread.LegB} {
			legExchange := strings.TrimSpace(leg.Exchange)
			if legExchange == "" {
				return 0, false, fmt.Errorf("funding_perp_spread leg exchange is unavailable")
			}
			legCfg, exists := cfg.Exchanges[legExchange]
			if !exists || strings.TrimSpace(legCfg.APIKey) == "" {
				if strings.EqualFold(legExchange, walletExchange) {
					return 0, false, fmt.Errorf("spread leg account identity is unavailable")
				}
				continue
			}
			if legExchange == walletExchange && equityAccountScopeID(legExchange, legCfg) == walletScope {
				quoteAsset, err := quoteAssetFromConfiguredSymbol(leg.Symbol)
				if err != nil {
					return 0, false, fmt.Errorf("funding_perp_spread quote asset is unavailable: %w", err)
				}
				if quoteAsset != walletQuoteAsset {
					return 0, false, fmt.Errorf("funding_perp_spread quote asset %s does not match wallet %s", quoteAsset, walletQuoteAsset)
				}
				fraction += 0.5
			}
		}
		return fraction, fraction > 0, nil
	}
	botExchange := strings.TrimSpace(bot.exchange)
	botCfg, exists := cfg.Exchanges[botExchange]
	if botExchange == walletExchange && (!exists || strings.TrimSpace(botCfg.APIKey) == "") {
		return 0, false, fmt.Errorf("account identity is unavailable")
	}
	if !exists || equityAccountScopeID(botExchange, botCfg) != walletScope || botExchange != walletExchange {
		return 0, false, nil
	}
	if strings.ToLower(strings.TrimSpace(bot.market)) != walletMarket {
		return 0, false, nil
	}
	quoteAsset, err := quoteAssetFromConfiguredSymbol(bot.symbol)
	if err != nil {
		return 0, false, fmt.Errorf("configured Bot %s quote asset is unavailable: %w", bot.id, err)
	}
	if quoteAsset != walletQuoteAsset {
		return 0, false, nil
	}
	return 1, true, nil
}

var configuredQuoteAssetSuffixes = []string{
	"USDT", "USDC", "FDUSD", "BUSD", "TUSD", "USDP", "DAI", "PAXG",
	"EUR", "GBP", "AUD", "BRL", "CAD", "CHF", "JPY", "KRW", "MXN", "RUB", "TRY",
	"USD", "BTC", "ETH", "BNB",
}

func quoteAssetFromConfiguredSymbol(symbol string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	for _, separator := range []string{"/", "-", "_", ":", " "} {
		normalized = strings.ReplaceAll(normalized, separator, "")
	}
	for _, contractSuffix := range []string{"SWAP", "PERP"} {
		normalized = strings.TrimSuffix(normalized, contractSuffix)
	}
	for _, suffix := range configuredQuoteAssetSuffixes {
		if strings.HasSuffix(normalized, suffix) && len(normalized) > len(suffix) {
			return suffix, nil
		}
	}
	return "", fmt.Errorf("cannot identify quote asset from symbol %q", symbol)
}
