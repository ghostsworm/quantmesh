package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// 本文件把 Bot 級的 auto_rebuild、slot_filter、close_on_stop_config 接到運行時：
// 啟動前校驗、啟動時應用、停止時清理/平倉。

const (
	slotFilterTypeExclude = "exclude"
	slotFilterTypeInclude = "include"

	autoRebuildModeSmart  = "smart"
	autoRebuildModeAlways = "always"

	// directionBoth 單向淨持倉雙向網格（config.NormalizeDirection 的返回值）
	directionBoth = "BOTH"

	// closeOnStopBaseTimeout 停止時平倉（撤單 + 查持倉 + 下單）的請求超時；
	// 限價單的超時重試由 ClosePositionManager 在後台按 timeout_sec 處理，不阻塞停止流程
	closeOnStopBaseTimeout = 30 * time.Second
)

// validateBotRuntimeExtras 校驗 auto_rebuild、slot_filter、close_on_stop_config；非法時拒絕本 Bot 啟動
func validateBotRuntimeExtras(symCfg config.SymbolConfig) error {
	var errs []error
	if err := validateAutoRebuildConfig(symCfg.AutoRebuild); err != nil {
		errs = append(errs, fmt.Errorf("auto_rebuild: %w", err))
	}
	if err := validateSlotFilterConfig(symCfg.SlotFilter); err != nil {
		errs = append(errs, fmt.Errorf("slot_filter: %w", err))
	}
	if err := validateCloseOnStopConfig(symCfg); err != nil {
		errs = append(errs, fmt.Errorf("close_on_stop_config: %w", err))
	}
	return errors.Join(errs...)
}

// validateAutoRebuildConfig 未啟用時不校驗；0 表示使用默認值，負數與越界值拒絕
func validateAutoRebuildConfig(c config.GridAutoRebuildConfig) error {
	if !c.Enabled {
		return nil
	}
	var errs []error
	nonNegative := map[string]int{
		"check_interval_minutes": c.CheckIntervalMinutes,
		"price_deviation_layers": c.PriceDeviationLayers,
		"order_expire_minutes":   c.OrderExpireMinutes,
		"max_rebuilds_per_hour":  c.MaxRebuildsPerHour,
		"min_rebuild_interval":   c.MinRebuildInterval,
	}
	for _, name := range []string{"check_interval_minutes", "price_deviation_layers", "order_expire_minutes", "max_rebuilds_per_hour", "min_rebuild_interval"} {
		if nonNegative[name] < 0 {
			errs = append(errs, fmt.Errorf("%s 不能為負數: %d", name, nonNegative[name]))
		}
	}
	if c.ExpiredOrderRatio < 0 || c.ExpiredOrderRatio > 1 {
		errs = append(errs, fmt.Errorf("expired_order_ratio 必須在 0~1 之間: %v", c.ExpiredOrderRatio))
	}
	switch strings.TrimSpace(c.RebuildMode) {
	case "", autoRebuildModeSmart, autoRebuildModeAlways:
	default:
		errs = append(errs, fmt.Errorf("rebuild_mode 只支持 smart/always: %q", c.RebuildMode))
	}
	if c.RequireTrendConfirm {
		errs = append(errs, errors.New("require_trend_confirm 尚未實現，請設為 false"))
	}
	return errors.Join(errs...)
}

// validateSlotFilterConfig 每條規則類型必須是 exclude/include，且至少有 prices 或完整的 [min_price, max_price]
func validateSlotFilterConfig(c config.SlotFilterConfig) error {
	var errs []error
	for i, r := range c.Rules {
		switch r.Type {
		case slotFilterTypeExclude, slotFilterTypeInclude:
		default:
			errs = append(errs, fmt.Errorf("rules[%d].type 只支持 exclude/include: %q", i, r.Type))
		}
		for _, p := range r.Prices {
			if p <= 0 {
				errs = append(errs, fmt.Errorf("rules[%d].prices 含非正價格: %v", i, p))
				break
			}
		}
		if r.MinPrice < 0 || r.MaxPrice < 0 {
			errs = append(errs, fmt.Errorf("rules[%d] 價格區間不能為負: [%v, %v]", i, r.MinPrice, r.MaxPrice))
		}
		hasMin, hasMax := r.MinPrice > 0, r.MaxPrice > 0
		if hasMin != hasMax {
			// 運行時只有兩端都 > 0 才按區間匹配，只填一端的規則永遠不會生效
			errs = append(errs, fmt.Errorf("rules[%d] min_price 和 max_price 必須同時設置: [%v, %v]", i, r.MinPrice, r.MaxPrice))
		}
		if hasMin && hasMax && r.MinPrice > r.MaxPrice {
			errs = append(errs, fmt.Errorf("rules[%d] min_price 大於 max_price: [%v, %v]", i, r.MinPrice, r.MaxPrice))
		}
		if len(r.Prices) == 0 && !hasMin && !hasMax {
			errs = append(errs, fmt.Errorf("rules[%d] 必須設置 prices 或 min_price/max_price", i))
		}
	}
	return errors.Join(errs...)
}

// closeOnStopConfigSet close_on_stop_config 是否配置（零值視為未配置，沿用舊的全平邏輯）
func closeOnStopConfigSet(c config.ClosePositionConfig) bool {
	return c != (config.ClosePositionConfig{})
}

// isFullCloseRatio quantity_ratio 為 0 或 1 表示全倉
func isFullCloseRatio(ratio float64) bool {
	return ratio == 0 || ratio == 1
}

func validateCloseOnStopConfig(symCfg config.SymbolConfig) error {
	c := symCfg.CloseOnStopConfig
	if !closeOnStopConfigSet(c) {
		return nil
	}
	var errs []error
	switch position.CloseMethod(c.Method) {
	case position.CloseMethodMarket, position.CloseMethodLimit:
	default:
		errs = append(errs, fmt.Errorf("method 只支持 market/limit: %q", c.Method))
	}
	if c.TimeoutSec < 0 {
		errs = append(errs, fmt.Errorf("timeout_sec 不能為負數: %d", c.TimeoutSec))
	}
	if c.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("max_retries 不能為負數: %d", c.MaxRetries))
	}
	if c.QuantityRatio < 0 || c.QuantityRatio > 1 {
		errs = append(errs, fmt.Errorf("quantity_ratio 必須在 0~1 之間: %v", c.QuantityRatio))
	}
	if symCfg.GetDirection() == directionBoth && !isFullCloseRatio(c.QuantityRatio) {
		errs = append(errs, fmt.Errorf("direction=BOTH 停止時按槽位腿別全平，不支持部分平倉 quantity_ratio=%v", c.QuantityRatio))
	}
	return errors.Join(errs...)
}

// applyConfiguredSlotFilter 啟動時應用配置的槽位過濾（與 /api/bots/:id/slot-filter 使用同一個 SetSlotFilter）
func applyConfiguredSlotFilter(ctx context.Context, symCfg config.SymbolConfig, spm *position.SuperPositionManager) {
	if spm == nil || len(symCfg.SlotFilter.Rules) == 0 {
		return
	}
	filter := symCfg.SlotFilter
	filter.Rules = append([]config.SlotFilterRule(nil), symCfg.SlotFilter.Rules...)
	spm.SetSlotFilter(&filter)
	logger.InfoCtx(ctx, "🧩 [%s] 已應用配置的槽位過濾規則: %d 條", symCfg.Symbol, len(filter.Rules))
}

// startConfiguredAutoRebuild 按配置啟動網格自動重建，返回停止函數（未啟動時為空操作）。
// gridActive=false（非網格多策略模式）時不啟動：沒有網格錨點和掛單，重建沒有意義。
func startConfiguredAutoRebuild(ctx context.Context, symCfg config.SymbolConfig, spm *position.SuperPositionManager, gridActive bool) func() {
	if spm == nil || !symCfg.AutoRebuild.Enabled {
		return func() {}
	}
	if !gridActive {
		logger.WarnCtx(ctx, "⚠️ [%s] 已配置 auto_rebuild，但當前為非網格多策略模式，不啟動網格自動重建", symCfg.Symbol)
		return func() {}
	}
	spm.StartAutoRebuild(symCfg.AutoRebuild)
	return spm.StopAutoRebuild
}

// closeOnStopActions 停止時平倉用到的操作，抽出以便測試
type closeOnStopActions struct {
	cancelAllOrders func()
	// liquidateAll 核實型全平倉（阻塞至平乾淨或 ctx 超時），殘留時返回錯誤
	liquidateAll   func(ctx context.Context) error
	closePositions func(ctx context.Context, cfg config.ClosePositionConfig) error
	// exchangePositionFlat 平倉前重查交易所持倉，返回 true 表示已無持倉（跳過平倉）。
	// nil 表示不支持重查（如現貨），直接按本地槽位處理。
	exchangePositionFlat func(ctx context.Context) (bool, error)
}

// shutdownCloseOwner 退出流程中由誰負責平倉（同一 Bot 只允許一條路徑提交平倉單）
type shutdownCloseOwner int

const (
	shutdownCloseNone    shutdownCloseOwner = iota // 不平倉
	shutdownCloseProcess                           // 進程級 system.close_positions_on_exit（按交易所持倉一次全平）
	shutdownCloseBot                               // Bot 級 close_on_stop（Stop 中執行）
)

// decideShutdownCloseOwner 決定退出時的平倉負責方：
//   - 只開一個開關：由該開關負責；
//   - 兩個都開：配置了 close_on_stop_config（method/ratio 等）或現貨（進程級按合約持倉查不到現貨餘額）時交給 Bot 級，
//     否則由進程級按交易所實際持倉做一次全平，Bot 級 close_on_stop 跳過。
func decideShutdownCloseOwner(processCloseOnExit bool, sc config.SymbolConfig) shutdownCloseOwner {
	switch {
	case !processCloseOnExit && !sc.CloseOnStop:
		return shutdownCloseNone
	case !processCloseOnExit:
		return shutdownCloseBot
	case !sc.CloseOnStop:
		return shutdownCloseProcess
	case closeOnStopConfigSet(sc.CloseOnStopConfig) || config.IsSpotMarketType(sc.MarketType):
		return shutdownCloseBot
	default:
		return shutdownCloseProcess
	}
}

// markShutdownCloseHandled 標記本 Bot 在退出流程中已被平倉，Stop 中的 close_on_stop 將跳過
func (rt *SymbolRuntime) markShutdownCloseHandled(reason string) {
	if rt == nil {
		return
	}
	rt.shutdownCloseHandled.Store(&reason)
}

// shutdownCloseHandledReason 返回已平倉原因；空串表示尚未由其他路徑平倉
func (rt *SymbolRuntime) shutdownCloseHandledReason() string {
	if rt == nil {
		return ""
	}
	if p := rt.shutdownCloseHandled.Load(); p != nil {
		return *p
	}
	return ""
}

// runCloseOnStop 停止時平倉：
//   - close_on_stop=false：不處理；
//   - 未配置 close_on_stop_config，或 direction=BOTH：LiquidateAll（按槽位腿別平倉，與改動前一致）；
//   - 否則撤銷本交易對掛單後按 close_on_stop_config 走 BotRuntime.ClosePositions；
//     下單失敗（此時沒有平倉單掛出）且是全倉平倉時回退 LiquidateAll。
func runCloseOnStop(ctx context.Context, symCfg config.SymbolConfig, act closeOnStopActions) {
	if err := runCloseOnStopWithError(ctx, symCfg, act); err != nil {
		logger.ErrorCtx(ctx, "❌ [%s] 停止平仓未核实完成: %v", symCfg.Symbol, err)
	}
}

func runCloseOnStopWithError(ctx context.Context, symCfg config.SymbolConfig, act closeOnStopActions) error {
	if !symCfg.CloseOnStop {
		return nil
	}
	// 平倉前重查交易所持倉：已被其他路徑平掉（或本就無倉）時不再提交一輪平倉單
	if act.exchangePositionFlat != nil {
		flat, err := act.exchangePositionFlat(ctx)
		if err != nil {
			return fmt.Errorf("停止时无法核实交易所持仓: %w", err)
		} else if flat {
			logger.InfoCtx(ctx, "ℹ️ [%s] 終止時交易所持倉已為 0，跳過 close_on_stop 平倉", symCfg.Symbol)
			return nil
		}
	}
	cfg := symCfg.CloseOnStopConfig
	if !closeOnStopConfigSet(cfg) {
		logger.InfoCtx(ctx, "🔄 [%s] 終止時全部平倉 (close_on_stop=true)...", symCfg.Symbol)
		return runVerifiedLiquidationWithError(ctx, symCfg, act)
	}
	if symCfg.GetDirection() == directionBoth {
		logger.InfoCtx(ctx, "🔄 [%s] 終止時全部平倉 (direction=BOTH 按槽位腿別平倉，close_on_stop_config.method 不適用)...", symCfg.Symbol)
		return runVerifiedLiquidationWithError(ctx, symCfg, act)
	}
	logger.InfoCtx(ctx, "🔄 [%s] 終止時平倉 (method=%s, ratio=%v, timeout=%ds)...",
		symCfg.Symbol, cfg.Method, cfg.QuantityRatio, cfg.TimeoutSec)
	// 先撤掉掛單：網格止盈單會佔用現貨餘額，也會和平倉單重複平倉
	act.cancelAllOrders()
	if err := act.closePositions(ctx, cfg); err != nil {
		return fmt.Errorf("停止时按配置平仓未核实（ratio=%v，不追加全平）: %w", cfg.QuantityRatio, err)
	}
	return nil
}

// runVerifiedLiquidation 保留給只需記錄日志的兼容調用；生命周期停止路径使用错误返回版本。
func runVerifiedLiquidation(ctx context.Context, symCfg config.SymbolConfig, act closeOnStopActions) {
	if err := runVerifiedLiquidationWithError(ctx, symCfg, act); err != nil {
		logger.ErrorCtx(ctx, "❌ [%s] 終止時全平倉未核實完成，請手動處理: %v", symCfg.Symbol, err)
	}
}

func runVerifiedLiquidationWithError(ctx context.Context, symCfg config.SymbolConfig, act closeOnStopActions) error {
	if err := act.liquidateAll(ctx); err != nil {
		return fmt.Errorf("停止时全平未核实完成: %w", err)
	}
	return nil
}

// closeOnStopActionsForRuntime 綁定到真實運行時：平倉走 R1 修正後的 BotRuntime.ClosePositions，
// 全平走核實型 LiquidateAllVerified（無交易所實例時退回只提交限價單的 LiquidateAll 並返回錯誤）
func closeOnStopActionsForRuntime(symCfg config.SymbolConfig, rt *SymbolRuntime) closeOnStopActions {
	spm := rt.SuperPositionManager
	return closeOnStopActions{
		cancelAllOrders: spm.CancelAllOrders,
		liquidateAll: func(ctx context.Context) error {
			venue := spm.NewLiquidationVenue(rt.Exchange)
			if venue == nil {
				spm.LiquidateAll()
				return fmt.Errorf("交易所實例不可用，已提交限價平倉單但無法核實成交")
			}
			// timeout=0：使用 ctx（closeOnStopBaseTimeout）剩餘時間
			return spm.LiquidateAllVerified(ctx, venue, 0)
		},
		closePositions: func(ctx context.Context, cfg config.ClosePositionConfig) error {
			botCfg := config.SymbolConfigToBotConfig(symCfg, false)
			br := &BotRuntime{Config: botCfg, BotID: config.BotIDOrGenerate(botCfg), Inner: rt}
			if spm.GetNetPositionQty() == 0 {
				logger.InfoCtx(ctx, "ℹ️ [%s] 終止時無持倉，無需平倉", symCfg.Symbol)
				return nil
			}
			_, err := br.ClosePositions(ctx, cfg)
			if err != nil {
				rt.markShutdownCloseUnverified(err.Error())
			}
			return err
		},
		exchangePositionFlat: exchangePositionFlatChecker(symCfg, rt),
	}
}

// exchangePositionFlatChecker 合約 Bot 返回按交易所持倉判斷是否已平的函數；現貨或無交易所實例返回 nil
func exchangePositionFlatChecker(symCfg config.SymbolConfig, rt *SymbolRuntime) func(ctx context.Context) (bool, error) {
	if rt == nil || rt.Exchange == nil || config.IsSpotMarketType(symCfg.MarketType) {
		return nil
	}
	ex := rt.Exchange
	return func(ctx context.Context) (bool, error) {
		positions, err := queryShutdownPositions(ctx, ex, symCfg.Symbol)
		if err != nil {
			return false, fmt.Errorf("查詢 %s 交易所持倉失敗: %w", symCfg.Symbol, err)
		}
		return len(positions) == 0, nil
	}
}

// shouldRunBotCloseOnStop Stop 中是否執行 close_on_stop：已由進程級平倉處理時跳過並記錄原因
func shouldRunBotCloseOnStop(ctx context.Context, symCfg config.SymbolConfig, rt *SymbolRuntime) bool {
	if !symCfg.CloseOnStop || rt == nil {
		return false
	}
	if reason := rt.shutdownCloseUnverifiedReason(); reason != "" {
		logger.ErrorCtx(ctx, "[%s] close_on_stop 已阻斷，不能重試未核實平倉：%s", symCfg.Symbol, reason)
		return false
	}
	if reason := rt.shutdownCloseHandledReason(); reason != "" {
		logger.InfoCtx(ctx, "ℹ️ [%s] 跳過 close_on_stop 平倉：%s", symCfg.Symbol, reason)
		return false
	}
	return rt.SuperPositionManager != nil
}

func (rt *SymbolRuntime) setShutdownContext(ctx context.Context) {
	if rt == nil {
		return
	}
	rt.shutdownContextMu.Lock()
	rt.shutdownContext = ctx
	rt.shutdownContextMu.Unlock()
}

func (rt *SymbolRuntime) stopContext(fallback context.Context) context.Context {
	if rt != nil {
		rt.shutdownContextMu.RLock()
		ctx := rt.shutdownContext
		rt.shutdownContextMu.RUnlock()
		if ctx != nil {
			return ctx
		}
	}
	if fallback == nil {
		return context.Background()
	}
	return context.WithoutCancel(fallback)
}

// closeOnStopForRuntime 停止流程調用入口（帶超時，不依賴可能已取消的啟動 ctx）
func closeOnStopForRuntime(logCtx context.Context, symCfg config.SymbolConfig, rt *SymbolRuntime) error {
	if !shouldRunBotCloseOnStop(logCtx, symCfg, rt) {
		if symCfg.CloseOnStop && rt != nil && rt.shutdownCloseUnverifiedReason() != "" {
			return fmt.Errorf("close_on_stop blocked: %s", rt.shutdownCloseUnverifiedReason())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(rt.stopContext(logCtx), closeOnStopBaseTimeout)
	defer cancel()
	if rt.ExchangeExecutor != nil {
		var err error
		ctx, err = rt.ExchangeExecutor.ShutdownCloseContext(ctx)
		if err != nil {
			rt.markShutdownCloseUnverified(err.Error())
			logger.ErrorCtx(logCtx, "[%s] 退出平仓未取得排空后的专用提交许可: %v", symCfg.Symbol, err)
			return fmt.Errorf("未取得排空后的专用平仓许可: %w", err)
		}
	}
	if err := runCloseOnStopWithError(ctx, symCfg, closeOnStopActionsForRuntime(symCfg, rt)); err != nil {
		rt.markShutdownCloseUnverified(err.Error())
		return err
	}
	return nil
}
