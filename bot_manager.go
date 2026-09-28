package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/execution"
	"quantmesh/feerate"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
)

// BotRuntime 代表單個 Bot 的運行時，封裝 SymbolRuntime 實現 Bot 級別的邏輯隔離
type BotRuntime struct {
	Config   config.BotConfig
	BotID    string
	Inner    *SymbolRuntime
	EventBus *event.EventBus
	configMu sync.RWMutex // 保護 Config 的並發訪問
}

// botStartFailure 記錄異步啟動失敗原因（供 Web API 與前端輪詢展示）
type botStartFailure struct {
	Message string
	At      time.Time
}

// BotManager 管理多個 BotRuntime，按 BotID 進行生命週期管理
type BotManager struct {
	runtimeAdmissions            execution.OpeningGate // drains admitted start/stop transitions during process shutdown
	shutdownTransitionUnverified atomic.Bool
	cfg                          *config.Config
	runtimes                     map[string]*BotRuntime
	runtimesMu                   sync.RWMutex
	groupLegAlerted              map[string]bool
	groupLegTimers               map[string]*time.Timer
	singleLegGraceSec            int
	eventBus                     *event.EventBus
	storageService               *storage.StorageService
	distributedLock              lock.DistributedLock
	botStatesFileOverride        string // 測試用，空時用默認 ./data/bot_states.json
	startFailMu                  sync.RWMutex
	startFail                    map[string]botStartFailure
	primaryYAMLPath              string // 命令行主配置路徑（非空時啟動前與主庫一併刷新內存配置）
}

// NewBotManager 創建 Bot 管理器。primaryYAMLPath 為啟動時傳入的主 YAML 路徑（無則傳空），用於與 app_config 一致的刷新順序。
func NewBotManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock, primaryYAMLPath string) *BotManager {
	return &BotManager{
		cfg:               cfg,
		runtimes:          make(map[string]*BotRuntime),
		groupLegAlerted:   make(map[string]bool),
		groupLegTimers:    make(map[string]*time.Timer),
		singleLegGraceSec: 30,
		eventBus:          eventBus,
		storageService:    storageService,
		distributedLock:   distributedLock,
		startFail:         make(map[string]botStartFailure),
		primaryYAMLPath:   strings.TrimSpace(primaryYAMLPath),
	}
}

func (bm *BotManager) refreshConfigBeforeBotStart() error {
	if bm == nil || bm.cfg == nil {
		return nil
	}
	var st storage.Storage
	if bm.storageService != nil {
		st = bm.storageService.GetStorage()
	}
	return storage.RefreshTradingConfigFromPrimarySource(bm.primaryYAMLPath, st, &bm.cfg)
}

// resolveLatestStartConfig 在真正啟動前重新對齊 Bot 配置：
// 1. 優先使用剛刷新的 bm.cfg.Bots；
// 2. 若主庫存在 bot_configs 快照，則再用該 Bot 專屬快照覆蓋。
// 避免 StartBot 調用入口傳入的是保存前/刷新前的舊副本。
func (bm *BotManager) resolveLatestStartConfig(botCfg config.BotConfig) config.BotConfig {
	if bm == nil {
		return botCfg
	}
	botID := config.BotIDOrGenerate(botCfg)
	latest := botCfg

	if bm.cfg != nil {
		for i := range bm.cfg.Bots {
			candidate := bm.cfg.Bots[i]
			if config.BotIDOrGenerate(candidate) == botID {
				latest = candidate
				break
			}
		}
	}

	if bm.storageService == nil {
		return latest
	}
	ss, ok := bm.storageService.GetStorage().(*storage.SQLStorage)
	if !ok || ss == nil {
		return latest
	}
	doc, err := ss.GetBotConfigDocument(context.Background(), botID)
	if err != nil {
		logger.Warn("⚠️ [%s] 啟動前讀取 bot_configs 失敗，回退主配置快照: %v", botID, err)
		return latest
	}
	if doc == nil || strings.TrimSpace(doc.Content) == "" {
		return latest
	}
	var bf config.BotConfigFile
	if err := json.Unmarshal([]byte(doc.Content), &bf); err != nil {
		logger.Warn("⚠️ [%s] 啟動前解析 bot_configs 失敗，回退主配置快照: %v", botID, err)
		return latest
	}
	// BotConfigFile 不帶 Enabled：沿用主配置（或調用方）中的值，CreatedAt/ID 為空時也沿用
	latest = config.MergeBotConfigFileInto(latest, &bf)
	if latest.ID == "" {
		latest.ID = botID
	}
	return latest
}

// applyExchangeFeeFromAPIForBot 啟動前把交易所 taker 費率寫入進程內 cfg.Exchanges[ex].FeeRate。
//
// 與 symbol_manager.applyGridFeeRates 看似重複（兩處都拉一次費率），但並非冗餘，不能刪除：
//   - 本函數寫的是全局配置的 taker 費率，被 startSymbolRuntime 讀作 feeRate，用於
//     safety.CheckAccountSafety（持倉安全檢查）、selectProfile 的費率切換規則，以及 web 參數建議（api_param_advisor）；
//   - applyGridFeeRates 只把 maker/taker 注入本 Bot 的 SuperPositionManager（費率感知最小利差），不回寫全局配置，
//     且在交易所接口失敗時回退到本函數寫入的 FeeRate。
//
// 代價是 Bot 啟動時多一次費率 REST 請求（受 timing.skip_exchange_fee_on_bot_start 控制）。
func (bm *BotManager) applyExchangeFeeFromAPIForBot(botCfg config.BotConfig) {
	if bm == nil || bm.cfg == nil || botCfg.Exchange == "" || botCfg.Symbol == "" {
		return
	}
	if bm.cfg.Timing.SkipExchangeFeeOnBotStart {
		return
	}
	maker, taker, err := feerate.FetchFromExchangeAPI(bm.cfg, botCfg.Exchange, botCfg.Symbol)
	if err != nil {
		logger.Info("ℹ️ 啟動前從交易所拉取手續費跳過: %v", err)
		return
	}
	if taker <= 0 {
		return
	}
	if exCfg, ok := bm.cfg.Exchanges[botCfg.Exchange]; ok {
		exCfg.FeeRate = taker
		bm.cfg.Exchanges[botCfg.Exchange] = exCfg
		logger.Info("💳 啟動前已依交易所接口更新 %s Taker 手續費: %.4f%%（maker %.4f%%，用於持倉安全檢查）",
			botCfg.Exchange, taker*100, maker*100)
	}
}

// runPeriodicFeeRefresh 先從主庫/YAML 刷新內存配置，再按各交易所拉取 Taker 費率寫入內存（不強制寫回數據庫）。
func (bm *BotManager) runPeriodicFeeRefresh() {
	if bm == nil || bm.cfg == nil {
		return
	}
	var st storage.Storage
	if bm.storageService != nil {
		st = bm.storageService.GetStorage()
	}
	if err := storage.RefreshTradingConfigFromPrimarySource(bm.primaryYAMLPath, st, &bm.cfg); err != nil {
		logger.Warn("⚠️ 定期刷新主配置失敗（仍嘗試拉取交易所費率）: %v", err)
	}

	seen := make(map[string]bool)
	type pair struct{ ex, sym string }
	var pairs []pair
	for _, b := range bm.cfg.Bots {
		if b.Exchange == "" || b.Symbol == "" {
			continue
		}
		k := strings.ToLower(b.Exchange)
		if seen[k] {
			continue
		}
		seen[k] = true
		pairs = append(pairs, pair{b.Exchange, b.Symbol})
	}
	if len(pairs) == 0 {
		seen = make(map[string]bool)
		for _, s := range bm.cfg.Trading.Symbols {
			if s.Exchange == "" || s.Symbol == "" {
				continue
			}
			k := strings.ToLower(s.Exchange)
			if seen[k] {
				continue
			}
			seen[k] = true
			pairs = append(pairs, pair{s.Exchange, s.Symbol})
		}
	}
	for _, p := range pairs {
		maker, taker, err := feerate.FetchFromExchangeAPI(bm.cfg, p.ex, p.sym)
		if err != nil {
			logger.Info("ℹ️ 定期拉取 %s 手續費失敗: %v", p.ex, err)
			continue
		}
		if taker <= 0 {
			continue
		}
		if exCfg, ok := bm.cfg.Exchanges[p.ex]; ok {
			exCfg.FeeRate = taker
			bm.cfg.Exchanges[p.ex] = exCfg
			logger.Info("💳 定期同步 %s Taker 手續費: %.4f%%（maker %.4f%%）", p.ex, taker*100, maker*100)
		}
	}
}

// StartFeeRateRefreshLoop 按 timing.fee_rate_refresh_minutes 週期刷新主配置並拉取交易所費率；分鐘數 <= 0 時不啟動。
func (bm *BotManager) StartFeeRateRefreshLoop(ctx context.Context) {
	if bm == nil || bm.cfg == nil {
		return
	}
	min := bm.cfg.Timing.FeeRateRefreshMinutes
	if min <= 0 {
		return
	}
	d := time.Duration(min) * time.Minute
	logger.Info("⏱️ 交易所手續費定期同步已啟用（間隔 %v）", d)
	go func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bm.runPeriodicFeeRefresh()
			}
		}
	}()
}

// symbolKey 返回交易所+交易對+市場類型的標準化 key，用於衝突檢測
func symbolKey(exchange, symbol, marketType string) string {
	return strings.ToLower(fmt.Sprintf("%s:%s:%s", exchange, symbol, marketType))
}

// findConflictingRuntime 檢查是否已有與待啟動 Bot 衝突的運行中實例（期現套利規則 + 期貨腿重疊）
func (bm *BotManager) findConflictingRuntime(newBot *config.BotConfig) *BotRuntime {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	return bm.findConflictingRuntimeUnlocked(newBot)
}

func (bm *BotManager) findConflictingRuntimeUnlocked(newBot *config.BotConfig) *BotRuntime {
	for _, br := range bm.runtimes {
		if br == nil {
			continue
		}
		if config.BotsConflict(&br.Config, newBot) {
			return br
		}
	}
	return nil
}

func (bm *BotManager) recordStartFailure(botID string, err error) {
	if bm == nil || err == nil || botID == "" {
		return
	}
	bm.startFailMu.Lock()
	defer bm.startFailMu.Unlock()
	if bm.startFail == nil {
		bm.startFail = make(map[string]botStartFailure)
	}
	bm.startFail[botID] = botStartFailure{Message: err.Error(), At: time.Now()}
}

func (bm *BotManager) clearStartFailure(botID string) {
	if bm == nil || botID == "" {
		return
	}
	bm.startFailMu.Lock()
	defer bm.startFailMu.Unlock()
	if bm.startFail != nil {
		delete(bm.startFail, botID)
	}
}

// GetLastStartFailure 返回最近一次啟動失敗信息（無則 ok=false）
func (bm *BotManager) GetLastStartFailure(botID string) (message string, failedAt time.Time, ok bool) {
	if bm == nil || botID == "" {
		return "", time.Time{}, false
	}
	bm.startFailMu.RLock()
	defer bm.startFailMu.RUnlock()
	if bm.startFail == nil {
		return "", time.Time{}, false
	}
	rec, ok := bm.startFail[botID]
	if !ok {
		return "", time.Time{}, false
	}
	return rec.Message, rec.At, true
}

// StartBot 啟動指定 Bot
func (bm *BotManager) StartBot(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	finishTransition, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return nil, fmt.Errorf("process shutdown rejects Bot start: %w", err)
	}
	defer finishTransition()
	botID := config.BotIDOrGenerate(botCfg)
	botCfg.ID = botID
	bm.clearStartFailure(botID)
	bm.runtimesMu.RLock()
	_, exists := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	if exists {
		return nil, nil // 已在運行
	}

	// 🔒 衝突檢測：阻止同一交易所+交易對+市場類型啟動多個 Bot
	// 原因：交易所倉位按 Symbol 隔離，多個 Bot 無法區分誰擁有哪部分倉位，
	// 會導致倉位重複認領、訂單互撞、對賬覆蓋等問題。
	if conflict := bm.findConflictingRuntime(&botCfg); conflict != nil {
		logger.Warn("🚫 [%s] 跳過啟動：與運行中 Bot [%s] 衝突（期貨腿或資金費套利互斥規則）。"+
			"如需多策略，請在同一 Bot 內配置多個 strategies。",
			botID, conflict.BotID)
		err := fmt.Errorf("symbol_conflict: 與 Bot [%s] 衝突", conflict.BotID)
		bm.recordStartFailure(botID, err)
		return nil, err
	}

	// 🔒 檢查數據庫中的啟停狀態（優先級高於配置文件）
	// 如果數據庫中標記為已禁用，則跳過啟動
	dbEnabled, reason := bm.isBotEnabledInDB(botID)
	if !dbEnabled {
		logger.Warn("🚫 [%s] 跳過啟動：數據庫中已禁用此 Bot（原因: %s）", botID, reason)
		// 发布启动失败事件（因被禁用）
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    fmt.Sprintf("Bot 在數據庫中被禁用: %s", reason),
			},
		})
		err := fmt.Errorf("bot_disabled_in_database: %s", reason)
		bm.recordStartFailure(botID, err)
		return nil, err
	}

	if err := bm.refreshConfigBeforeBotStart(); err != nil {
		logger.Warn("⚠️ 啟動前重新加載主配置失敗（繼續使用進程內存中的配置）: %v", err)
	}
	botCfg = bm.resolveLatestStartConfig(botCfg)
	bm.applyExchangeFeeFromAPIForBot(botCfg)

	symCfg := config.BotConfigToSymbolConfig(botCfg)
	onRequestStop := func(botID string) {
		_ = bm.StopBotWithReason(botID, "close_condition", "關閉條件觸發")
	}
	rt, err := startSymbolRuntime(ctx, bm.cfg, symCfg, bm.eventBus, bm.storageService, bm.distributedLock, onRequestStop)
	if err != nil {
		// 发布启动失败事件
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    err.Error(),
			},
		})
		bm.recordStartFailure(botID, err)
		return nil, err
	}
	if bm.runtimeAdmissions.Blocked() {
		bm.shutdownTransitionUnverified.Store(true)
		sealRuntimeShutdown(rt)
		rt.markShutdownCloseUnverified("Bot 启动与进程退出重叠，需核账后再平仓")
		if rt.Stop != nil {
			rt.Stop()
		}
		return nil, fmt.Errorf("process shutdown interrupted Bot start")
	}
	br := &BotRuntime{
		Config:   botCfg,
		BotID:    botID,
		Inner:    rt,
		EventBus: bm.eventBus,
	}
	bm.runtimesMu.Lock()
	if _, ok := bm.runtimes[botID]; ok {
		bm.runtimesMu.Unlock()
		if rt != nil && rt.Stop != nil {
			rt.Stop()
		}
		return nil, nil
	}
	if conflict := bm.findConflictingRuntimeUnlocked(&botCfg); conflict != nil {
		bm.runtimesMu.Unlock()
		if rt != nil && rt.Stop != nil {
			rt.Stop()
		}
		err := fmt.Errorf("symbol_conflict: 與 Bot [%s] 衝突", conflict.BotID)
		bm.recordStartFailure(botID, err)
		return nil, err
	}
	bm.runtimes[botID] = br
	bm.runtimesMu.Unlock()

	bm.clearStartFailure(botID)

	if bm.storageService != nil {
		registerWebSymbolProvidersForRuntime(rt, &botCfg, bm.storageService)
	}

	// 发布启动成功事件
	bm.eventBus.Publish(&event.Event{
		Type: event.EventTypeTradingStarted,
		Data: map[string]interface{}{
			"bot_id":   botID,
			"exchange": botCfg.Exchange,
			"symbol":   botCfg.Symbol,
			"strategy": botCfg.Strategies,
		},
	})
	bm.checkGroupLegConsistencyForBot(botID)

	return br, nil
}

// StopBot 停止指定 Bot
func (bm *BotManager) StopBot(botID string) error {
	return bm.StopBotWithReason(botID, "web_ui", "用戶通過 Web UI 停止")
}

// StopBotWithReason 停止指定 Bot 並記錄原因（供關閉條件等自動停止場景使用）
func (bm *BotManager) StopBotWithReason(botID, updatedBy, reason string) error {
	finishTransition, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return fmt.Errorf("process shutdown owns Bot stop: %w", err)
	}
	defer finishTransition()
	bm.runtimesMu.Lock()
	br, ok := bm.runtimes[botID]
	if !ok {
		bm.runtimesMu.Unlock()
		return nil
	}
	bm.runtimesMu.Unlock()

	unregisterWebSymbolProvidersForRuntime(&br.Config)

	if br.Inner != nil && br.Inner.Stop != nil {
		br.Inner.Stop()
		if bm.runtimeAdmissions.Blocked() && br.Inner.shutdownCloseUnverifiedReason() != "" {
			bm.shutdownTransitionUnverified.Store(true)
		}
	}
	// Keep the owner registered until its stop/close has finished. Otherwise a
	// concurrent StartBot can claim the same symbol while the old Bot is closing.
	bm.runtimesMu.Lock()
	if bm.runtimes[botID] == br {
		delete(bm.runtimes, botID)
	}
	bm.runtimesMu.Unlock()

	// 🔥 保存停止狀態到數據庫（持久化，重啟後仍然有效）
	bm.saveBotStateToDB(botID, false, updatedBy, reason)

	// 发布停止事件
	bm.eventBus.Publish(&event.Event{
		Type: event.EventTypeTradingStopped,
		Data: map[string]interface{}{
			"bot_id":   botID,
			"exchange": br.Config.Exchange,
			"symbol":   br.Config.Symbol,
			"strategy": br.Config.Strategies,
			"reason":   reason,
		},
	})
	bm.checkGroupLegConsistencyForBot(botID)

	return nil
}

// EnableBot 啟用 Bot（從數據庫移除禁用標記）
// 注意：這只是從數據庫移除禁用標記，不會立即啟動 Bot
// Bot 需要通過 StartBot 方法或在配置文件中 enabled=true 才會啟動
func (bm *BotManager) EnableBot(botID string) error {
	// 🔥 從數據庫中刪除禁用記錄（或設置為 enabled=true）
	bm.saveBotStateToDB(botID, true, "web_ui", "用戶通過 Web UI 啟用")

	logger.Info("✅ [%s] Bot 已在數據庫中標記為啟用，可以通過 StartBot 方法啟動", botID)
	return nil
}

// Get 按 BotID 獲取運行時
func (bm *BotManager) Get(botID string) (*BotRuntime, bool) {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	br, ok := bm.runtimes[botID]
	return br, ok
}

// GetByExchangeSymbol 按交易所和交易對獲取運行時（兼容舊接口）
func (bm *BotManager) GetByExchangeSymbol(exchangeName, symbol string, marketType ...string) (*BotRuntime, bool) {
	mt := "futures"
	if len(marketType) > 0 && marketType[0] != "" {
		mt = marketType[0]
	}
	botID := config.GenerateBotID(exchangeName, symbol, mt)
	return bm.Get(botID)
}

// List 列出所有 Bot 運行時
func (bm *BotManager) List() []*BotRuntime {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	list := make([]*BotRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		list = append(list, br)
	}
	return list
}

// Remove 從管理器中移除 Bot 運行時（會先停止 Bot）
func (bm *BotManager) Remove(botID string) {
	_ = bm.StopBot(botID)
}

// AddRuntime 註冊已創建的 BotRuntime（用於啟動時已有 SymbolRuntime 的向後兼容場景）
func (bm *BotManager) AddRuntime(br *BotRuntime) {
	if br == nil || br.BotID == "" {
		return
	}
	bm.runtimesMu.Lock()
	defer bm.runtimesMu.Unlock()
	if bm.groupLegAlerted == nil {
		bm.groupLegAlerted = make(map[string]bool)
	}
	bm.runtimes[br.BotID] = br
}

// StopAll 停止所有 Bot
func (bm *BotManager) StopAll() {
	bm.runtimesMu.Lock()
	runtimes := make([]*BotRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		runtimes = append(runtimes, br)
	}
	bm.runtimes = make(map[string]*BotRuntime)
	bm.groupLegAlerted = make(map[string]bool)
	for _, timer := range bm.groupLegTimers {
		if timer != nil {
			timer.Stop()
		}
	}
	bm.groupLegTimers = make(map[string]*time.Timer)
	bm.runtimesMu.Unlock()
	for _, br := range runtimes {
		if br != nil && br.Inner != nil && br.Inner.Stop != nil {
			br.Inner.Stop()
		}
	}
}

func (bm *BotManager) checkGroupLegConsistencyForBot(botID string) {
	if bm == nil || bm.cfg == nil || botID == "" {
		return
	}
	for _, group := range bm.cfg.BotGroups {
		if len(group.BotIDs) < 2 || !containsBotID(group.BotIDs, botID) {
			continue
		}

		var runningIDs []string
		total := len(group.BotIDs)
		publishAlert := false
		publishRecovered := false

		bm.runtimesMu.Lock()
		if bm.groupLegAlerted == nil {
			bm.groupLegAlerted = make(map[string]bool)
		}
		for _, id := range group.BotIDs {
			if _, ok := bm.runtimes[id]; ok {
				runningIDs = append(runningIDs, id)
			}
		}
		running := len(runningIDs)
		alreadyAlerted := bm.groupLegAlerted[group.ID]
		if running > 0 && running < total {
			if !alreadyAlerted {
				bm.groupLegAlerted[group.ID] = true
				publishAlert = true
			}
			bm.scheduleSingleLegEnforcementLocked(group.ID)
		} else {
			if running == total && alreadyAlerted {
				publishRecovered = true
			}
			bm.groupLegAlerted[group.ID] = false
			bm.cancelSingleLegEnforcementLocked(group.ID)
		}
		bm.runtimesMu.Unlock()

		if publishAlert {
			logger.Warn("⚠️ [BotGroup:%s] 对冲组出现单腿运行，running=%v total=%d", group.ID, runningIDs, total)
			if bm.eventBus != nil {
				bm.eventBus.Publish(&event.Event{
					Type: event.EventTypeError,
					Data: map[string]interface{}{
						"group_id":         group.ID,
						"group_name":       group.Name,
						"issue":            "single_leg_running",
						"running_bot_ids":  runningIDs,
						"expected_bot_ids": group.BotIDs,
					},
				})
			}
		}
		if publishRecovered {
			logger.Info("✅ [BotGroup:%s] 对冲组双腿已恢复一致运行", group.ID)
			if bm.eventBus != nil {
				bm.eventBus.Publish(&event.Event{
					Type: event.EventTypeRiskRecovered,
					Data: map[string]interface{}{
						"group_id":        group.ID,
						"group_name":      group.Name,
						"issue":           "single_leg_running",
						"running_bot_ids": runningIDs,
					},
				})
			}
		}
	}
}

func (bm *BotManager) scheduleSingleLegEnforcementLocked(groupID string) {
	if bm.singleLegGraceSec <= 0 {
		return
	}
	if bm.groupLegTimers == nil {
		bm.groupLegTimers = make(map[string]*time.Timer)
	}
	if _, exists := bm.groupLegTimers[groupID]; exists {
		return
	}
	delay := time.Duration(bm.singleLegGraceSec) * time.Second
	bm.groupLegTimers[groupID] = time.AfterFunc(delay, func() {
		bm.enforceSingleLegPause(groupID)
	})
}

func (bm *BotManager) cancelSingleLegEnforcementLocked(groupID string) {
	if bm.groupLegTimers == nil {
		return
	}
	if timer, exists := bm.groupLegTimers[groupID]; exists {
		timer.Stop()
		delete(bm.groupLegTimers, groupID)
	}
}

func (bm *BotManager) enforceSingleLegPause(groupID string) {
	if bm == nil || bm.cfg == nil || groupID == "" {
		return
	}

	var targetGroup *config.BotGroup
	for i := range bm.cfg.BotGroups {
		if bm.cfg.BotGroups[i].ID == groupID {
			targetGroup = &bm.cfg.BotGroups[i]
			break
		}
	}
	if targetGroup == nil {
		bm.runtimesMu.Lock()
		bm.cancelSingleLegEnforcementLocked(groupID)
		bm.runtimesMu.Unlock()
		return
	}

	runningIDs := make([]string, 0, len(targetGroup.BotIDs))
	bm.runtimesMu.Lock()
	for _, id := range targetGroup.BotIDs {
		if _, ok := bm.runtimes[id]; ok {
			runningIDs = append(runningIDs, id)
		}
	}
	// 当前已经恢复（全运行）或全部停止，不执行自动暂停
	if len(runningIDs) == 0 || len(runningIDs) == len(targetGroup.BotIDs) {
		bm.cancelSingleLegEnforcementLocked(groupID)
		bm.runtimesMu.Unlock()
		return
	}
	bm.cancelSingleLegEnforcementLocked(groupID)
	bm.runtimesMu.Unlock()

	for _, id := range runningIDs {
		if br, ok := bm.Get(id); ok && br != nil {
			br.PauseOpening("single_leg_running")
		}
	}

	logger.Warn("🛑 [BotGroup:%s] 单腿运行超过 %d 秒，自动暂停开仓。running=%v", groupID, bm.singleLegGraceSec, runningIDs)
	if bm.eventBus != nil {
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeRiskTriggered,
			Data: map[string]interface{}{
				"group_id":        groupID,
				"issue":           "single_leg_running",
				"action":          "pause_opening",
				"running_bot_ids": runningIDs,
			},
		})
	}
}

func containsBotID(botIDs []string, target string) bool {
	for _, id := range botIDs {
		if id == target {
			return true
		}
	}
	return false
}

// ListSymbolRuntimes 返回底層 SymbolRuntime 列表（供需要兼容 SymbolManager 的調用方使用）
func (bm *BotManager) ListSymbolRuntimes() []*SymbolRuntime {
	bm.runtimesMu.RLock()
	defer bm.runtimesMu.RUnlock()
	list := make([]*SymbolRuntime, 0, len(bm.runtimes))
	for _, br := range bm.runtimes {
		if br.Inner != nil {
			list = append(list, br.Inner)
		}
	}
	return list
}

// UpdateRuntimeTradingParams 更新運行中的 Bot 交易參數（熱更新）
// 始終同步 Config 到運行時，確保 smart_order 等非交易參數變更也能反映到 GetBot 返回的詳情中
func (bm *BotManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedBotIDs []string) {
	for _, botCfg := range latestCfg.Bots {
		botID := botCfg.ID
		if botID == "" {
			botID = config.GenerateBotID(botCfg.Exchange, botCfg.Symbol, botCfg.GetMarketType())
		}
		bm.runtimesMu.RLock()
		br, ok := bm.runtimes[botID]
		bm.runtimesMu.RUnlock()
		if !ok || br.Inner == nil || br.Inner.SuperPositionManager == nil {
			continue
		}
		symCfg := config.BotConfigToSymbolConfig(botCfg)
		changed := br.Inner.SuperPositionManager.UpdateTradingParams(
			symCfg.PriceInterval,
			symCfg.ProfitSpread,
			symCfg.OrderQuantity,
			symCfg.BuyWindowSize,
			symCfg.SellWindowSize,
		)
		br.Inner.SuperPositionManager.SetSpotInventoryPolicy(symCfg.SpotInventoryPolicy)
		// 始終同步 Config，確保 smart_order、風控等配置變更在刷新頁面時正確顯示
		br.configMu.Lock()
		br.Config = botCfg
		br.Config.OpenPositionControl = config.CloneOpenPositionControl(botCfg.OpenPositionControl)
		br.publishRiskControlsLocked()
		br.configMu.Unlock()
		br.Inner.Config = symCfg
		if changed {
			updatedBotIDs = append(updatedBotIDs, botID)
		}
	}
	return
}

// ClosePositions 平倉（支持市價/限價）
func (br *BotRuntime) ClosePositions(ctx context.Context, cfg config.ClosePositionConfig) (*position.ClosePositionRecord, error) {
	if br.Inner == nil {
		return nil, fmt.Errorf("bot not initialized")
	}
	if br.Inner.shutdownCloseUnverifiedReason() != "" {
		return nil, fmt.Errorf("previous account close remains unverified: %w", errShutdownCloseUnverified)
	}
	if br.Inner.CloseForManual != nil {
		record, err := br.Inner.CloseForManual(ctx, cfg)
		if record != nil {
			br.Inner.recordSpecializedClose(record)
		}
		return record, err
	}
	if br.Inner.SuperPositionManager == nil {
		return nil, fmt.Errorf("manual close is not supported for this strategy runtime; use its strategy-owned close workflow")
	}

	// 獲取交易所
	exchange := br.Inner.Exchange
	if exchange == nil {
		return nil, fmt.Errorf("exchange not initialized")
	}

	// 按實際持倉計算平倉方向與數量（本 Bot 槽位淨持倉，並以交易所持倉封頂）
	plan, err := br.planClosePosition(ctx, cfg.QuantityRatio)
	if err != nil {
		return nil, fmt.Errorf("bot %s 計算平倉數量失敗: %w", br.BotID, err)
	}

	closeMgr, shutdown, err := br.ownedCloseManager(ctx)
	if err != nil {
		return nil, err
	}
	if shutdown {
		defer closeMgr.Stop()
	}

	record, err := closeMgr.ClosePositions(ctx, plan.Side, plan.Quantity, cfg)
	if err != nil {
		return record, fmt.Errorf("bot %s 平倉下單失敗 (%s %.8f): %w", br.BotID, plan.Side, plan.Quantity, err)
	}
	if shutdown {
		return closeMgr.WaitRecord(ctx, record.RecordID)
	}

	return record, nil
}

// planClosePosition 依據本 Bot 槽位淨持倉與交易所持倉計算平倉方向和數量
// ratio 為平倉比例 0~1，0 或 1 表示全倉
func (br *BotRuntime) planClosePosition(ctx context.Context, ratio float64) (*position.ClosePlan, error) {
	spm := br.Inner.SuperPositionManager
	ex := br.Inner.Exchange
	symbol := br.Config.Symbol

	botNet := spm.GetNetPositionQty()

	if config.IsSpotMarketType(br.Config.MarketType) {
		// 現貨無合約持倉概念，按本地槽位計算
		return position.PlanCloseOrder(botNet, 0, false, ratio, ex.GetQuantityDecimals())
	}

	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("獲取 %s 持倉失敗: %w", symbol, err)
	}
	var exchangeNet float64
	hasExchange := false
	for _, p := range positions {
		if p == nil {
			continue
		}
		if p.Symbol != "" && !strings.EqualFold(p.Symbol, symbol) {
			continue
		}
		exchangeNet += p.Size
		hasExchange = true
	}
	if !hasExchange && len(positions) > 0 {
		logger.Warn("⚠️ [平倉] %s 交易所持倉返回的交易對名稱與配置不匹配，僅按本地槽位持倉計算", symbol)
	}

	return position.PlanCloseOrder(botNet, exchangeNet, hasExchange, ratio, ex.GetQuantityDecimals())
}

// GetCloseRecords 獲取平倉記錄
func (br *BotRuntime) GetCloseRecords() []*position.ClosePositionRecord {
	if br.Inner == nil {
		return nil
	}
	br.Inner.closeManagerMu.Lock()
	mgr := br.Inner.closeManager
	br.Inner.closeManagerMu.Unlock()
	if mgr == nil {
		return br.Inner.specializedCloseRecords()
	}
	return append(mgr.ListRecords(), br.Inner.specializedCloseRecords()...)
}

// GetSlotFilter 獲取槽位過濾器
func (br *BotRuntime) GetSlotFilter() *config.SlotFilterConfig {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return nil
	}
	return br.Inner.SuperPositionManager.GetSlotFilter()
}

// SetSlotFilter 設置槽位過濾器
func (br *BotRuntime) SetSlotFilter(filter *config.SlotFilterConfig) {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return
	}
	br.Inner.SuperPositionManager.SetSlotFilter(filter)
}

// GetSlots 獲取所有槽位信息
func (br *BotRuntime) GetSlots() []map[string]interface{} {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return []map[string]interface{}{}
	}

	slots := br.Inner.SuperPositionManager.GetAllSlotsDetailed()
	result := make([]map[string]interface{}, len(slots))
	for i, slot := range slots {
		result[i] = map[string]interface{}{
			"price":           slot.Price,
			"position_status": slot.PositionStatus,
			"position_qty":    slot.PositionQty,
			"order_id":        slot.OrderID,
			"order_side":      slot.OrderSide,
			"order_status":    slot.OrderStatus,
			"order_price":     slot.OrderPrice,
			"slot_status":     slot.SlotStatus,
		}
	}
	return result
}

// GetBotRiskControl 获取 Bot 风控配置
func (br *BotRuntime) GetBotRiskControl() *config.BotRiskControl {
	br.configMu.RLock()
	defer br.configMu.RUnlock()
	if br.Config.OpenPositionControl.BotRiskControl == nil {
		return &config.BotRiskControl{}
	}
	copy := *br.Config.OpenPositionControl.BotRiskControl
	return &copy
}

// SetBotRiskControl 设置 Bot 风控配置
func (br *BotRuntime) SetBotRiskControl(riskControl *config.BotRiskControl) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()

	copy := config.BotRiskControl{}
	if riskControl != nil {
		copy = *riskControl
	}
	br.Config.OpenPositionControl.BotRiskControl = &copy
	br.publishRiskControlsLocked()
	return nil
}

// GetGridRiskControl 獲取網格風控配置
func (br *BotRuntime) GetGridRiskControl() config.GridRiskControl {
	br.configMu.RLock()
	defer br.configMu.RUnlock()
	return br.Config.GridRiskControl
}

// SetGridRiskControl 設置網格風控配置（運行時熱更新 + 同步到 SuperPositionManager）
func (br *BotRuntime) SetGridRiskControl(grc config.GridRiskControl) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()
	br.Config.GridRiskControl = grc
	br.publishRiskControlsLocked()
	return nil
}

// SetRiskControls atomically applies a combined Bot/grid API patch to the real
// executor snapshot; readers never observe only half of the request.
func (br *BotRuntime) SetRiskControls(rc *config.BotRiskControl, grid config.GridRiskControl) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()
	copy := config.BotRiskControl{}
	if rc != nil {
		copy = *rc
	}
	br.Config.OpenPositionControl.BotRiskControl = &copy
	br.Config.GridRiskControl = grid
	br.publishRiskControlsLocked()
	return nil
}

func (br *BotRuntime) publishRiskControlsLocked() {
	if spm := br.superPositionManager(); spm != nil {
		spm.SetRiskControls(config.RiskControls{Open: br.Config.OpenPositionControl, Grid: br.Config.GridRiskControl})
	}
	if br.Inner != nil && br.Inner.DynamicAdjuster != nil {
		br.Inner.DynamicAdjuster.RefreshRiskControls()
	}
}

// PauseOpening 暂停开仓
func (br *BotRuntime) PauseOpening(reason string) {
	br.configMu.Lock()

	// 更新 OpenPositionControl 中的 PauseOpening 状态
	br.Config.OpenPositionControl = config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	br.Config.OpenPositionControl.PauseOpening = true

	// 同时更新 BotRiskControl 中的状态
	if br.Config.OpenPositionControl.BotRiskControl == nil {
		br.Config.OpenPositionControl.BotRiskControl = &config.BotRiskControl{}
	}
	br.Config.OpenPositionControl.BotRiskControl.PauseOpening = true
	br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason = reason
	autoResumeSec := br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter
	br.configMu.Unlock()

	// C2：br.Config 只是展示用拷贝，SPM 持有自己的配置；必須直接通知 SPM 才能真正停止開倉
	// （SPM.PauseOpening 會撤銷開倉委託並記錄風控事件）。在釋放 configMu 後調用，避免持鎖做網絡請求。
	if spm := br.superPositionManager(); spm != nil {
		spm.PauseOpening(reason)
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Block("manual")
		storage.AppendBotRiskControlEvent(br.BotID, "paused", reason, "runtime_gate")
	} else {
		storage.AppendBotRiskControlEvent(br.BotID, "paused", reason, "config")
	}

	// 🔥 如果设置了自动恢复时间，启动自动恢复 goroutine
	if autoResumeSec > 0 {
		go br.autoResumeAfter(autoResumeSec)
	}
}

// superPositionManager 返回運行中的 SPM（未初始化時為 nil）
func (br *BotRuntime) superPositionManager() *position.SuperPositionManager {
	if br.Inner == nil {
		return nil
	}
	return br.Inner.SuperPositionManager
}

// SetRiskDataUnavailable implements the independently owned equity-data hold.
func (br *BotRuntime) SetRiskDataUnavailable(paused bool) {
	if spm := br.superPositionManager(); spm != nil {
		if changed := spm.SetEquityRiskPaused(paused); changed && paused {
			go spm.CancelResidualOpeningOrders()
		}
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Unblock("manual")
	}
}

func (br *BotRuntime) AllocationRiskStatus() (bool, string, error) {
	spm := br.superPositionManager()
	if spm == nil || spm.GetAllocationManager() == nil {
		return false, "", fmt.Errorf("Bot allocation manager is unavailable")
	}
	br.configMu.RLock()
	exchangeName, symbol := br.Config.Exchange, br.Config.Symbol
	br.configMu.RUnlock()
	manager := spm.GetAllocationManager()
	statuses := manager.GetAllStatuses()
	var match *position.AllocationStatus
	for _, status := range statuses {
		if status != nil && strings.EqualFold(status.Exchange, exchangeName) && strings.EqualFold(status.Symbol, symbol) {
			match = status
			break
		}
	}
	if match == nil || match.MaxAmount < 0 || match.UsedAmount < 0 || math.IsNaN(match.MaxAmount) || math.IsInf(match.MaxAmount, 0) || math.IsNaN(match.UsedAmount) || math.IsInf(match.UsedAmount, 0) {
		return false, "", fmt.Errorf("Bot allocation limit or usage sample is unavailable or invalid")
	}
	limit := match.MaxAmount
	if manager.PercentageBasedLimitsConfigured(exchangeName, symbol) {
		if match.LimitObservedAt.IsZero() || time.Since(match.LimitObservedAt) > 5*time.Minute || match.EffectiveLimit <= 0 || math.IsNaN(match.EffectiveLimit) || math.IsInf(match.EffectiveLimit, 0) {
			return false, "", fmt.Errorf("percentage-based allocation limit has no fresh authoritative effective-limit sample")
		}
		limit = match.EffectiveLimit
		if match.MaxAmount > 0 && match.MaxAmount < limit {
			limit = match.MaxAmount
		}
	}
	if match.UsedAmount > limit+1e-8 {
		return true, fmt.Sprintf("%s:%s used %.8f exceeds allocation limit %.8f USDT", match.Exchange, match.Symbol, match.UsedAmount, limit), nil
	}
	return false, "", nil
}

func (br *BotRuntime) SetAllocationRiskHold(held bool) {
	spm := br.superPositionManager()
	if spm == nil {
		return
	}
	gate := spm.OpeningGate()
	const source = "allocation_risk_unverified"
	if held {
		wasBlocked := gate.HasBlock(source)
		gate.Block(source)
		if !wasBlocked {
			go spm.CancelResidualOpeningOrders()
		}
		return
	}
	gate.Unblock(source)
}

// ResumeOpening 恢复开仓
func (br *BotRuntime) ResumeOpening() {
	br.resumeOpening("config")
}

// ResumeOpeningManually is reserved for an explicit user recovery request.
func (br *BotRuntime) ResumeOpeningManually() error {
	if br.Inner != nil && br.Inner.DynamicAdjuster != nil {
		if err := br.Inner.DynamicAdjuster.ResumeVolatilityManually(); err != nil {
			return err
		}
	}
	if spm := br.superPositionManager(); spm != nil {
		if err := spm.ResumeOpeningManually(); err != nil {
			return err
		}
	}
	br.resumeOpening("manual")
	return nil
}

func (br *BotRuntime) resumeOpening(source string) {
	br.configMu.Lock()
	// 更新 OpenPositionControl 中的 PauseOpening 状态
	br.Config.OpenPositionControl = config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	br.Config.OpenPositionControl.PauseOpening = false

	// 同时更新 BotRiskControl 中的状态
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		br.Config.OpenPositionControl.BotRiskControl.PauseOpening = false
		br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason = ""
	}
	br.configMu.Unlock()

	// C2：同步恢復 SPM 的開倉開關（SPM 內部會記錄 resumed 事件）
	if spm := br.superPositionManager(); spm != nil {
		spm.ResumeOpening()
		return
	}
	if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Unblock("manual")
	}
	storage.AppendBotRiskControlEvent(br.BotID, "resumed", "", source)
}

// GetPositionStatus 获取仓位状态（包括是否达到限制）
func (br *BotRuntime) GetPositionStatus() map[string]interface{} {
	if br.Inner != nil && br.Inner.SuperPositionManager == nil && br.Inner.OpeningGate != nil {
		paused := br.Inner.OpeningGate.Blocked()
		return map[string]interface{}{
			"paused": paused, "pause_reason": "specialized_strategy_gate",
			"valuation_available":          false,
			"position_managed_by_strategy": true,
		}
	}
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return map[string]interface{}{
			"error": "bot not initialized",
		}
	}

	spm := br.Inner.SuperPositionManager

	currentPrice := spm.GetLastMarketPrice()
	totalPositionQty, totalPositionValue, positionLayers, valued := spm.GetPositionExposure(currentPrice)
	if !valued {
		currentPrice = 0
	}

	// 获取杠杆并计算实际占用资金
	leverage := spm.GetLeverage()
	if leverage <= 0 {
		leverage = 1
	}
	actualMargin := totalPositionValue / float64(leverage)

	// Use the actual executor revision, not a display-only configuration copy.
	maxQty, maxValue, maxLayers := spm.GetRiskControls().Open.PositionLimits()
	paused := spm.IsOpeningPaused()

	status := map[string]interface{}{
		"total_position_qty":     totalPositionQty,
		"total_position_value":   totalPositionValue,
		"total_actual_margin":    actualMargin,
		"leverage":               leverage,
		"position_layers":        positionLayers,
		"current_price":          currentPrice,
		"paused":                 paused,
		"pause_reason":           spm.GetOpeningPauseReason(),
		"protective_liquidation": spm.GetProtectiveLiquidationStatus(),
		"valuation_available":    valued,
	}

	// 检查是否达到数量限制
	reachedLimitQty := false
	if maxQty > 0 {
		status["max_position_qty"] = maxQty
		reachedLimitQty = totalPositionQty >= maxQty
	}
	status["reached_limit_qty"] = reachedLimitQty

	// All limit checks use nominal position value, never leveraged margin.
	reachedLimitValue := false
	if maxValue > 0 {
		status["max_position_value"] = maxValue
		reachedLimitValue = totalPositionValue >= maxValue
	}
	status["reached_limit_value"] = reachedLimitValue

	// 检查是否达到层数限制
	reachedLimitLayers := false
	if maxLayers > 0 {
		status["max_position_layers"] = maxLayers
		reachedLimitLayers = positionLayers >= maxLayers
	}
	status["reached_limit_layers"] = reachedLimitLayers

	// 是否应该停止开仓
	status["should_stop_opening"] = reachedLimitQty || reachedLimitValue || reachedLimitLayers || paused || (maxValue > 0 && !valued && totalPositionQty > 0)
	if br.Inner.DynamicAdjuster != nil {
		status["volatility_evidence"] = br.Inner.DynamicAdjuster.GetVolatilityEvidenceStatus()
	}
	if br.Inner.ExchangeExecutor != nil {
		if exposure := br.Inner.ExchangeExecutor.ExposureSnapshot(); exposure != nil {
			status["execution_exposure"] = exposure
			if !exposure.Ready || !exposure.OpeningAvailable {
				status["should_stop_opening"] = true
			}
		}
	}

	return status
}

// CancelAllOpenOrders 取消所有开仓订单
func (br *BotRuntime) CancelAllOpenOrders() error {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return fmt.Errorf("bot not initialized")
	}
	if br.Inner.ExchangeExecutor == nil {
		return fmt.Errorf("Bot-owned order executor is unavailable; refusing unverified cancellation")
	}
	const emergencyCancelBlock = "bot_emergency_cancel"
	gate := br.Inner.SuperPositionManager.OpeningGate()
	gate.Block(emergencyCancelBlock)
	defer gate.Unblock(emergencyCancelBlock)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := br.Inner.ExchangeExecutor.CancelOwnedOpeningOrders(ctx); err != nil {
		return fmt.Errorf("Bot %s opening-order cancellation is not verified: %w", br.BotID, err)
	}
	return nil
}

// CloseAllPositions 平掉所有仓位（熔断 / 复合风控 / 紧急中心调用，每个 Bot 独立超时 ctx）。
//
// 所有方向（LONG/SHORT/BOTH 按槽位腿别）统一走核实型全平仓 LiquidateAllVerified：
// 穿价限价 → 等待成交 → 撤剩余 → 按交易所持仓市价 ReduceOnly 补平 → 清理挂单，残留时返回错误。
// method 仅用于日志：紧急平仓以「确认平干净」为准，不再按 limit 挂单后台重试。
// 本方法会阻塞至多 timeout 秒，调用方（risk.closePositionsOnBots）不持有 spm.mu / 槽位锁。
func (br *BotRuntime) CloseAllPositions(ctx context.Context, method string, timeout int) error {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return fmt.Errorf("bot not initialized")
	}
	if br.Inner.shutdownCloseUnverifiedReason() != "" {
		return fmt.Errorf("previous account close remains unverified: %w", errShutdownCloseUnverified)
	}
	if br.Inner.Exchange == nil {
		return fmt.Errorf("exchange not initialized")
	}
	spm := br.Inner.SuperPositionManager
	logger.Warn("🚨 [%s] 全部平仓（请求 method=%s，按核实型全平仓执行，超时 %ds）", br.BotID, method, timeout)
	venue := spm.NewLiquidationVenue(br.Inner.Exchange)
	if err := spm.LiquidateAllVerified(ctx, venue, time.Duration(timeout)*time.Second); err != nil {
		return fmt.Errorf("bot %s 全部平仓未核实完成: %w", br.BotID, err)
	}
	return nil
}

// CloseScopeKey identifies Bot controllers that share one netted venue position.
func (br *BotRuntime) CloseScopeKey() string {
	if br == nil || br.Inner == nil {
		return ""
	}
	if br.Inner.AccountScope == "" {
		return strings.ToLower(br.Inner.Config.Exchange) + "|unknown-account|" + strings.ToLower(br.Inner.AccountMarketType) + "|" + strings.ToUpper(br.Inner.Config.Symbol)
	}
	return shutdownRuntimeScopeKey(br.Inner)
}

// GetPositionSummary 获取仓位摘要信息
func (br *BotRuntime) GetPositionSummary() (float64, float64, error) {
	if br.Inner == nil || br.Inner.SuperPositionManager == nil {
		return 0, 0, fmt.Errorf("bot not initialized")
	}

	spm := br.Inner.SuperPositionManager
	currentPrice := spm.GetLastMarketPrice()
	if currentPrice == 0 {
		return 0, 0, fmt.Errorf("unable to get current price")
	}

	// 获取未实现盈亏
	unrealizedPnL := spm.GetUnrealizedPnL(currentPrice)

	// 获取总持仓价值
	totalValue := spm.GetTotalPositionValueUSDT()

	return unrealizedPnL, totalValue, nil
}

// autoResumeAfter 在指定秒数后自动恢复开仓
func (br *BotRuntime) autoResumeAfter(seconds int) {
	if seconds <= 0 {
		return
	}

	logger.Info("⏰ [%s] 自动恢复定时器已启动，将在 %d 秒后恢复开仓", br.BotID, seconds)

	time.Sleep(time.Duration(seconds) * time.Second)

	// 检查是否仍处于暂停状态
	br.configMu.RLock()
	stillPaused := br.Config.OpenPositionControl.PauseOpening
	pauseReason := ""
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		pauseReason = br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason
	}
	br.configMu.RUnlock()

	if stillPaused {
		logger.Info("🔔 [%s] 自动恢复定时器触发，恢复开仓（暂停原因: %s）", br.BotID, pauseReason)
		br.resumeOpening("auto_timer")
	} else {
		logger.Info("ℹ️ [%s] 自动恢复定时器触发，但开仓已恢复，跳过", br.BotID)
	}
}

// IsBotEnabledInDB 检查數據庫中的 Bot 啟停狀態（公開方法，供 StartSymbol 等調用方使用）
// 返回值: (是否啟用, 原因)
// 如果數據庫中沒有記錄，默認返回 (true, "")（使用配置文件的值）
func (bm *BotManager) IsBotEnabledInDB(botID string) (bool, string) {
	return bm.isBotEnabledInDB(botID)
}

// isBotEnabledInDB 检查數據庫中的 Bot 啟停狀態
// 返回值: (是否啟用, 原因)
// 如果數據庫中沒有記錄，默認返回 (true, "")（使用配置文件的值）
// 若存儲不可用或查詢失敗，嘗試從文件 fallback 讀取（與 saveBotStateToDB 的寫入邏輯對應）
// 若文件也無記錄，保守返回 (false, reason)，避免已停止的 Bot 重啟後自動運行
func (bm *BotManager) isBotEnabledInDB(botID string) (bool, string) {
	if bm.storageService == nil {
		// 存儲未初始化時，與 store==nil 一樣嘗試文件 fallback
		// 否則 EnableBot 寫入文件後，StartBot 仍會因不讀文件而拒絕啟動
		if enabled, found := bm.isBotEnabledFromFile(botID); found {
			return enabled, "from_file"
		}
		logger.Warn("存儲服務未初始化，保守跳過 Bot 自動啟動（避免已停止狀態遺失）[%s]", botID)
		return false, "storage_unavailable"
	}

	store := bm.storageService.GetStorage()
	if store == nil {
		// 存儲未啟用（如 storage.enabled=false），嘗試文件 fallback
		if enabled, found := bm.isBotEnabledFromFile(botID); found {
			return enabled, "from_file"
		}
		logger.Warn("存儲未初始化，保守跳過 Bot 自動啟動（避免已停止狀態遺失）[%s]", botID)
		return false, "storage_unavailable"
	}

	state, err := store.GetBotState(botID)
	if err != nil {
		logger.Warn("查詢 Bot 狀態失敗: %v，保守視為已禁用", err)
		return false, "query_failed"
	}

	if state == nil {
		// 數據庫中沒有記錄，使用配置文件的值
		return true, ""
	}

	return state.Enabled, state.Reason
}

// saveBotStateToDB 保存 Bot 啟停狀態到數據庫（存儲不可用時 fallback 到文件）
func (bm *BotManager) saveBotStateToDB(botID string, enabled bool, updatedBy, reason string) {
	state := &storage.BotState{
		BotID:     botID,
		Enabled:   enabled,
		UpdatedAt: time.Now(),
		UpdatedBy: updatedBy,
		Reason:    reason,
	}

	if bm.storageService != nil {
		store := bm.storageService.GetStorage()
		if store != nil {
			if err := store.SetBotState(state); err != nil {
				logger.Error("保存 Bot 狀態失敗: %v", err)
			} else {
				logger.Info("✅ [%s] Bot 狀態已保存到數據庫: enabled=%v, reason=%s", botID, enabled, reason)
				return
			}
		}
	}

	// 存儲不可用時 fallback 到文件，確保停止狀態能持久化
	if err := bm.saveBotStateToFile(state); err != nil {
		logger.Error("保存 Bot 狀態到文件失敗: %v", err)
	} else {
		logger.Info("✅ [%s] Bot 狀態已保存到文件: enabled=%v, reason=%s", botID, enabled, reason)
	}
}

// botStatesFilePath 返回 bot_states 文件路徑
func (bm *BotManager) botStatesFilePath() string {
	if bm != nil && bm.botStatesFileOverride != "" {
		return bm.botStatesFileOverride
	}
	return filepath.Join("./data", "bot_states.json")
}

// isBotEnabledFromFile 從文件讀取 Bot 啟停狀態（存儲不可用時的 fallback）
func (bm *BotManager) isBotEnabledFromFile(botID string) (enabled bool, found bool) {
	path := bm.botStatesFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var m map[string]struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return false, false
	}
	s, ok := m[botID]
	if !ok {
		return false, false
	}
	return s.Enabled, true
}

// GetStoppedAt 返回已停止 Bot 的停止時間（ISO 8601）。若 Bot 正在運行或無停止記錄則返回空。
func (bm *BotManager) GetStoppedAt(botID string) (string, bool) {
	bm.runtimesMu.RLock()
	_, running := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	if running {
		return "", false
	}
	if bm.storageService != nil {
		store := bm.storageService.GetStorage()
		if store != nil {
			state, err := store.GetBotState(botID)
			if err == nil && state != nil && !state.Enabled {
				return state.UpdatedAt.Format(time.RFC3339), true
			}
		}
	}
	// 文件 fallback：若存儲不可用，嘗試從文件讀取
	if stoppedAt, ok := bm.getStoppedAtFromFile(botID); ok {
		return stoppedAt, true
	}
	return "", false
}

// getStoppedAtFromFile 從 bot_states.json 讀取停止時間（僅當 enabled=false 時有效）
func (bm *BotManager) getStoppedAtFromFile(botID string) (string, bool) {
	path := bm.botStatesFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var m map[string]struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", false
	}
	s, ok := m[botID]
	if !ok || s.Enabled || s.UpdatedAt == "" {
		return "", false
	}
	return s.UpdatedAt, true
}

// saveBotStateToFile 保存 Bot 狀態到文件（存儲不可用時的 fallback）
func (bm *BotManager) saveBotStateToFile(state *storage.BotState) error {
	path := bm.botStatesFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	m := make(map[string]struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	})
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	m[state.BotID] = struct {
		Enabled   bool   `json:"enabled"`
		UpdatedAt string `json:"updated_at"`
		UpdatedBy string `json:"updated_by"`
		Reason    string `json:"reason"`
	}{state.Enabled, state.UpdatedAt.Format(time.RFC3339), state.UpdatedBy, state.Reason}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
