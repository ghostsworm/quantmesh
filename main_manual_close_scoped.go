package main

import (
	"context"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/web"
)

func manualCloseConfigMatches(cfg config.BotConfig, exchange, symbol, marketType string) bool {
	return strings.EqualFold(cfg.Exchange, exchange) && strings.EqualFold(cfg.Symbol, symbol) &&
		(marketType == "" || strings.EqualFold(cfg.GetMarketType(), marketType))
}

func (a *symbolManagerWebAdapter) ClosePositions(exchange, symbol string) (*web.ClosePositionsResponse, error) {
	if a == nil {
		return nil, web.ErrManualCloseScopeUnavailable
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.ClosePositionsScoped(ctx, exchange, symbol, "", "")
}

func (a *symbolManagerWebAdapter) ClosePositionsScoped(ctx context.Context, exchange, symbol, marketType, botID string) (*web.ClosePositionsResponse, error) {
	if a == nil || a.manager == nil || a.manager.botManager == nil || ctx == nil {
		return nil, web.ErrManualCloseScopeUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.ctx != nil && a.ctx.Err() != nil {
		return nil, a.ctx.Err()
	}
	closeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if a.ctx != nil {
		stop := context.AfterFunc(a.ctx, cancel)
		defer stop()
	}
	bm := a.manager.botManager
	var target *BotRuntime
	for _, bot := range bm.List() {
		if botID != "" && bot.BotID != botID {
			continue
		}
		bot.configMu.Lock()
		matches := manualCloseConfigMatches(bot.Config, exchange, symbol, marketType)
		bot.configMu.Unlock()
		if !matches {
			continue
		}
		if target != nil {
			return nil, web.ErrManualCloseScopeAmbiguous
		}
		target = bot
	}
	if target == nil {
		return nil, web.ErrManualCloseScopeUnavailable
	}
	var result *web.ClosePositionsResponse
	err := bm.WithBotStrategyConfigurationContext(closeCtx, target.BotID, func(managed bool) error {
		if err := closeCtx.Err(); err != nil {
			return err
		}
		if a.ctx != nil && a.ctx.Err() != nil {
			return a.ctx.Err()
		}
		current, ok := bm.Get(target.BotID)
		if !managed || !ok || current != target || current.Inner == nil {
			return web.ErrManualCloseScopeUnavailable
		}
		current.configMu.Lock()
		matches := manualCloseConfigMatches(current.Config, exchange, symbol, marketType)
		current.configMu.Unlock()
		if !matches {
			return web.ErrManualCloseScopeUnavailable
		}
		success, failed, err := a.manager.closeLegacyPositions(closeCtx, current.Inner)
		if err != nil {
			return err
		}
		result = &web.ClosePositionsResponse{SuccessCount: success, FailCount: failed, Message: "manual_close_verified"}
		return nil
	})
	return result, err
}
