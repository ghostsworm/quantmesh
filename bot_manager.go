package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
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
	"quantmesh/risk"
	"quantmesh/storage"
)

// BotRuntime 代表單個 Bot 的運行時，封裝 SymbolRuntime 實現 Bot 級別的邏輯隔離
type BotRuntime struct {
	Config                    config.BotConfig
	BotID                     string
	Inner                     *SymbolRuntime
	EventBus                  *event.EventBus
	configMu                  sync.RWMutex      // 保護 Config 的並發訪問
	conflictScope             *config.BotConfig // immutable after registration; guarded by manager runtimesMu
	strategySnapshotReady     bool              // protected by configMu; do not rewrite published strategy references on repeated registration
	pauseTransitionMu         sync.Mutex
	autoResumeGeneration      uint64
	stopTransitionInProgress  atomic.Bool // lifecycle callback is active; distinct from durable-write failure
	stopPersistencePending    atomic.Bool
	stopOwnershipPending      atomic.Bool       // financial stop completed; ownership release still unverified
	stopDrainPending          atomic.Bool       // producers/submissions not yet drained; financial outcomes not certified
	stopVerificationPending   atomic.Bool       // stopped financial phase requires read-only final verification
	stopCleanupPending        atomic.Bool       // stopped runtime still owns unconfirmed stream cleanup
	stopReconciliationPending atomic.Bool       // generic stop failure requires reconciliation, not financial replay
	stopIntent                *storage.BotState // guarded by the owning Bot lifecycle mutex
	stopJournalOperation      string            // guarded by the owning Bot lifecycle mutex
	stopJournalMode           string            // guarded by the owning Bot lifecycle mutex
	stopJournalFile           string            // stable for the admitted stop operation
	shutdownCallbackCompleted atomic.Bool       // set after StopAll's financial shutdown verifies
}

const equityDataUnavailableBlock = "equity_data_unverified"

func (br *BotRuntime) stopTransitionPending() bool {
	return br.stopTransitionInProgress.Load() || br.stopPersistencePending.Load() || br.stopOwnershipPending.Load() || br.stopDrainPending.Load() || br.stopVerificationPending.Load() || br.stopCleanupPending.Load() || br.stopReconciliationPending.Load()
}

const botStartFailureCode = "bot_start_failed_diagnostic_withheld"

// botStartFailure 只保留可安全提供給 Web API 的錯誤碼，不保存底層錯誤文本。
type botStartFailure struct {
	At time.Time
}

// BotManager 管理多個 BotRuntime，按 BotID 進行生命週期管理
type BotManager struct {
	runtimeAdmissions            execution.OpeningGate // drains admitted start/stop transitions during process shutdown
	shutdownTransitionUnverified atomic.Bool
	equityConfigRefreshMu        sync.Mutex
	equityScopeMu                sync.RWMutex
	equityScope                  equityScopeSnapshot
	equityScopeChangeHandler     func(func())
	retiredEquityAccounts        *retiredEquityAccountsStore
	equityResetterMu             sync.RWMutex
	equityBaselineResetter       equityBaselineResetter
	cfg                          *config.Config
	runtimes                     map[string]*BotRuntime
	pendingStarts                map[string]config.BotConfig // protected by runtimesMu; admitted initialization scopes
	runtimesMu                   sync.RWMutex
	botLifecycleLocksMu          sync.Mutex
	botLifecycleLocks            map[string]*sync.Mutex
	startConfigValidatorMu       sync.RWMutex
	startConfigValidator         func(config.BotConfig) error
	openingPauseCoordinatorMu    sync.RWMutex
	openingPauseCoordinator      *risk.OpeningPauseCoordinator
	groupLegAlerted              map[string]bool
	groupLegTimers               map[string]*time.Timer
	singleLegGraceSec            int
	eventBus                     *event.EventBus
	storageService               *storage.StorageService
	distributedLock              lock.DistributedLock
	feeRateFetcher               func(*config.Config, string, string) (float64, float64, error)
	botStatesFileOverride        string             // 測試用，空時用默認 ./data/bot_states.json
	stopJournalDirectorySync     func(string) error // optional filesystem fault injection for journal durability tests
	stopRecovery                 func(context.Context, *botStopJournal) error
	stopRecoveryMu               sync.Mutex
	stopRecoveryLeases           map[string][]*runtimeOwnershipLease
	startFailMu                  sync.RWMutex
	startFail                    map[string]botStartFailure
	primaryYAMLPath              string // 命令行主配置路徑（非空時啟動前與主庫一併刷新內存配置）
}

// SetDurableStopRecovery installs the explicit stopped-runtime reconciliation
// path. It is invoked only for an incomplete durable stop journal after the
// Bot runtime is absent; the default remains fail-closed.
func (bm *BotManager) SetDurableStopRecovery(recover func(context.Context, *botStopJournal) error) {
	if bm == nil {
		return
	}
	bm.stopRecovery = recover
}

// NewBotManager 創建 Bot 管理器。primaryYAMLPath 為啟動時傳入的主 YAML 路徑（無則傳空），用於與 app_config 一致的刷新順序。
func NewBotManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock, primaryYAMLPath string) *BotManager {
	bm := &BotManager{
		cfg:               cfg,
		runtimes:          make(map[string]*BotRuntime),
		groupLegAlerted:   make(map[string]bool),
		groupLegTimers:    make(map[string]*time.Timer),
		singleLegGraceSec: 30,
		eventBus:          eventBus,
		storageService:    storageService,
		distributedLock:   distributedLock,
		feeRateFetcher:    feerate.FetchFromExchangeAPI,
		startFail:         make(map[string]botStartFailure),
		primaryYAMLPath:   strings.TrimSpace(primaryYAMLPath),
	}
	if storageService != nil {
		if backend, ok := storageService.GetStorage().(riskCheckpointBackend); ok {
			bm.retiredEquityAccounts = newRetiredEquityAccountsStore(backend)
		}
	}
	bm.updateEquityScopeConfig(cfg)
	return bm
}

func (bm *BotManager) botLifecycleMutex(botID string) *sync.Mutex {
	bm.botLifecycleLocksMu.Lock()
	if bm.botLifecycleLocks == nil {
		bm.botLifecycleLocks = make(map[string]*sync.Mutex)
	}
	mu := bm.botLifecycleLocks[botID]
	if mu == nil {
		mu = &sync.Mutex{}
		bm.botLifecycleLocks[botID] = mu
	}
	bm.botLifecycleLocksMu.Unlock()
	return mu
}

func (bm *BotManager) lockBotLifecycle(botID string) func() {
	mu := bm.botLifecycleMutex(botID)
	mu.Lock()
	return mu.Unlock
}

// SetStartConfigValidator installs a persisted-config check executed under the Bot lifecycle lock.
func (bm *BotManager) SetStartConfigValidator(validator func(config.BotConfig) error) {
	if bm == nil {
		return
	}
	bm.startConfigValidatorMu.Lock()
	bm.startConfigValidator = validator
	bm.startConfigValidatorMu.Unlock()
}

// SetOpeningPauseCoordinator installs restored global risk holds into each
// runtime before its strategies are allowed to start.
func (bm *BotManager) SetOpeningPauseCoordinator(coordinator *risk.OpeningPauseCoordinator) {
	if bm == nil {
		return
	}
	bm.openingPauseCoordinatorMu.Lock()
	bm.openingPauseCoordinator = coordinator
	bm.openingPauseCoordinatorMu.Unlock()
}

// SetEquityScopeChangeHandler invalidates equity-derived risk evidence before
// a changed account scope becomes visible to readers. It must not perform
// exchange or storage I/O; callers install it during startup.
func (bm *BotManager) SetEquityScopeChangeHandler(handler func(func())) {
	if bm == nil {
		return
	}
	bm.equityConfigRefreshMu.Lock()
	bm.equityScopeChangeHandler = handler
	bm.equityConfigRefreshMu.Unlock()
}

func (bm *BotManager) SetEquityBaselineResetter(resetter equityBaselineResetter) {
	if bm == nil {
		return
	}
	bm.equityResetterMu.Lock()
	bm.equityBaselineResetter = resetter
	bm.equityResetterMu.Unlock()
}

func (bm *BotManager) updateEquityScopeConfig(cfg *config.Config) {
	if bm == nil {
		return
	}
	snapshot := buildEquityScopeSnapshot(cfg)
	bm.equityScopeMu.RLock()
	previous := bm.equityScope
	bm.equityScopeMu.RUnlock()
	snapshot.revision = previous.revision
	changed := snapshot.configured != previous.configured || snapshot.scope != previous.scope || snapshot.err != previous.err || !sameEquityAccountEvidenceConfigs(snapshot.accounts, previous.accounts)
	if changed {
		snapshot.revision++
	}
	publish := func() {
		bm.equityScopeMu.Lock()
		bm.equityScope = snapshot
		bm.equityScopeMu.Unlock()
	}
	if changed && bm.equityScopeChangeHandler != nil {
		// Serialize invalidation and publication with feeder sampling so an old
		// sample cannot be published as healthy after this scope is changed.
		bm.equityScopeChangeHandler(publish)
		return
	}
	publish()
}

func (bm *BotManager) equityScopeSnapshot() equityScopeSnapshot {
	if bm == nil {
		return equityScopeSnapshot{}
	}
	bm.equityScopeMu.RLock()
	defer bm.equityScopeMu.RUnlock()
	return bm.equityScope
}

func (bm *BotManager) registerEquityScopeConfig(cfg *config.Config) {
	if bm == nil {
		return
	}
	bm.equityConfigRefreshMu.Lock()
	bm.updateEquityScopeConfig(cfg)
	bm.equityConfigRefreshMu.Unlock()
}

// PrepareEquityScopeConfig durably archives any account credentials that the
// next configuration would retire before the caller persists that change.
func (bm *BotManager) PrepareEquityScopeConfig(ctx context.Context, cfg *config.Config) error {
	if bm == nil || cfg == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("equity account-scope preparation requires context")
	}
	next := buildEquityScopeSnapshot(cfg)
	bm.equityConfigRefreshMu.Lock()
	defer bm.equityConfigRefreshMu.Unlock()
	previous := bm.equityScopeSnapshot()
	if next.err != "" && (previous.scope != next.scope || !sameEquityAccountEvidenceConfigs(previous.accounts, next.accounts)) {
		return fmt.Errorf("cannot change equity account scope to an unsupported configuration")
	}
	if len(removedEquityAccounts(previous, next)) == 0 {
		return nil
	}
	if bm.retiredEquityAccounts == nil {
		return fmt.Errorf("retired account archive storage is unavailable; account-scope change rejected")
	}
	bm.openingPauseCoordinatorMu.RLock()
	pauseCoordinator := bm.openingPauseCoordinator
	bm.openingPauseCoordinatorMu.RUnlock()
	if pauseCoordinator == nil {
		return fmt.Errorf("opening pause coordinator is unavailable; account-scope change rejected")
	}
	if err := bm.retiredEquityAccounts.archiveRemoved(ctx, previous, next, time.Now()); err != nil {
		return fmt.Errorf("account-scope change rejected because the old account could not be archived: %w", err)
	}
	if err := pauseCoordinator.Pause(retiredEquityAccountPauseSource, "舊帳戶須持續唯讀核驗至可明確重置", nil); err != nil {
		return fmt.Errorf("account-scope change rejected because retired-account opening pause could not be verified: %w", err)
	}
	return nil
}

// RestoreRetiredEquityAccountHold re-establishes a durable opening hold before
// any runtime starts whenever old account credentials remain under review.
func (bm *BotManager) RestoreRetiredEquityAccountHold(ctx context.Context) error {
	if bm == nil || ctx == nil {
		return fmt.Errorf("retired-account startup verification requires manager and context")
	}
	bm.openingPauseCoordinatorMu.RLock()
	pauseCoordinator := bm.openingPauseCoordinator
	bm.openingPauseCoordinatorMu.RUnlock()
	if pauseCoordinator == nil {
		return fmt.Errorf("opening pause coordinator is unavailable")
	}
	if bm.retiredEquityAccounts == nil {
		return fmt.Errorf("retired account archive storage is unavailable")
	}
	accounts, _, err := bm.retiredEquityAccounts.load(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return nil
	}
	return pauseCoordinator.Pause(retiredEquityAccountPauseSource, "舊帳戶尚未完成核驗與明確重置", nil)
}

func (bm *BotManager) refreshConfigBeforeBotStart() error {
	if bm == nil || bm.cfg == nil {
		return nil
	}
	var st storage.Storage
	if bm.storageService != nil {
		st = bm.storageService.GetStorage()
	}
	bm.equityConfigRefreshMu.Lock()
	defer bm.equityConfigRefreshMu.Unlock()
	if err := storage.RefreshTradingConfigFromPrimarySource(bm.primaryYAMLPath, st, &bm.cfg); err != nil {
		return err
	}
	bm.updateEquityScopeConfig(bm.cfg)
	return nil
}

// resolveLatestStartConfig 在真正啟動前重新對齊 Bot 配置：
// 1. 優先使用剛刷新的 bm.cfg.Bots；
// 2. 若主庫存在 bot_configs 快照，則再用該 Bot 專屬快照覆蓋。
// 避免 StartBot 調用入口傳入的是保存前/刷新前的舊副本。
func (bm *BotManager) resolveLatestStartConfig(botCfg config.BotConfig) (config.BotConfig, error) {
	if bm == nil {
		return botCfg, nil
	}
	botID := config.BotIDOrGenerate(botCfg)
	latest := botCfg
	configuredBotFound := false

	if bm.cfg != nil {
		if len(bm.cfg.Bots) > 0 {
			for i := range bm.cfg.Bots {
				candidate := bm.cfg.Bots[i]
				if config.BotIDOrGenerate(candidate) == botID {
					latest = candidate
					configuredBotFound = true
					break
				}
			}
		} else {
			for i := range bm.cfg.Trading.Symbols {
				candidate := bm.cfg.Trading.Symbols[i]
				exchangeName := candidate.Exchange
				if exchangeName == "" {
					exchangeName = bm.cfg.App.CurrentExchange
				}
				candidateID := candidate.ID
				if candidateID == "" {
					candidateID = config.GenerateBotID(exchangeName, candidate.Symbol, candidate.GetMarketType())
				}
				if candidateID == botID {
					candidate.Exchange = exchangeName
					exchangeCfg := bm.cfg.Exchanges[exchangeName]
					latest = config.SymbolConfigToBotConfig(candidate, exchangeCfg.Testnet)
					configuredBotFound = true
					break
				}
			}
		}
		if (len(bm.cfg.Bots) > 0 || len(bm.cfg.Trading.Symbols) > 0) && !configuredBotFound {
			return botCfg, fmt.Errorf("Bot %s 不存在於最新主配置，拒絕使用呼叫方舊快照啟動", botID)
		}
	}

	if bm.storageService == nil {
		return latest, nil
	}
	ss, ok := bm.storageService.GetStorage().(*storage.SQLStorage)
	if !ok || ss == nil {
		return latest, nil
	}
	doc, err := ss.GetBotConfigDocument(context.Background(), botID)
	if err != nil {
		return latest, fmt.Errorf("啟動前讀取 Bot 配置失敗(%s): %w", botID, err)
	}
	if doc == nil || strings.TrimSpace(doc.Content) == "" {
		return latest, nil
	}
	var bf config.BotConfigFile
	if err := json.Unmarshal([]byte(doc.Content), &bf); err != nil {
		return latest, fmt.Errorf("啟動前解析 Bot 配置失敗(%s): %w", botID, err)
	}
	// BotConfigFile 不帶 Enabled：沿用主配置（或調用方）中的值，CreatedAt/ID 為空時也沿用
	latest = config.MergeBotConfigFileInto(latest, &bf)
	if latest.ID == "" {
		latest.ID = botID
	}
	return latest, nil
}

func (bm *BotManager) prepareBotStartConfig(botCfg config.BotConfig) (config.BotConfig, error) {
	if err := bm.refreshConfigBeforeBotStart(); err != nil {
		return botCfg, fmt.Errorf("啟動前刷新主交易配置失敗: %w", err)
	}
	latest, err := bm.resolveLatestStartConfig(botCfg)
	if err != nil {
		return botCfg, err
	}
	return latest, nil
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
	if bm == nil || botCfg.Exchange == "" || botCfg.Symbol == "" {
		return
	}
	bm.equityConfigRefreshMu.Lock()
	if bm.cfg == nil || bm.cfg.Timing.SkipExchangeFeeOnBotStart {
		bm.equityConfigRefreshMu.Unlock()
		return
	}
	lookupConfig := feeLookupConfig(bm.cfg)
	fetcher := bm.feeRateFetcher
	bm.equityConfigRefreshMu.Unlock()
	if fetcher == nil {
		fetcher = feerate.FetchFromExchangeAPI
	}
	maker, taker, err := fetcher(lookupConfig, botCfg.Exchange, botCfg.Symbol)
	if err != nil {
		logger.Info("ℹ️ 啟動前從交易所拉取手續費跳過: %v", err)
		return
	}
	if taker <= 0 {
		return
	}
	if expected, ok := lookupConfig.Exchanges[botCfg.Exchange]; ok && bm.storeFetchedExchangeFee(botCfg.Exchange, expected, taker) {
		logger.Info("💳 啟動前已依交易所接口更新 %s Taker 手續費: %.4f%%（maker %.4f%%，用於持倉安全檢查）",
			botCfg.Exchange, taker*100, maker*100)
	}
}

// runPeriodicFeeRefresh 先從主庫/YAML 刷新內存配置，再按各交易所拉取 Taker 費率寫入內存（不強制寫回數據庫）。
func (bm *BotManager) runPeriodicFeeRefresh() {
	if bm == nil {
		return
	}
	bm.equityConfigRefreshMu.Lock()
	if bm.cfg == nil {
		bm.equityConfigRefreshMu.Unlock()
		return
	}
	var st storage.Storage
	if bm.storageService != nil {
		st = bm.storageService.GetStorage()
	}
	if err := storage.RefreshTradingConfigFromPrimarySource(bm.primaryYAMLPath, st, &bm.cfg); err != nil {
		logger.Warn("⚠️ 定期刷新主配置失敗（仍嘗試拉取交易所費率）: %v", err)
	}
	bm.updateEquityScopeConfig(bm.cfg)

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
	lookupConfig := feeLookupConfig(bm.cfg)
	fetcher := bm.feeRateFetcher
	bm.equityConfigRefreshMu.Unlock()
	if fetcher == nil {
		fetcher = feerate.FetchFromExchangeAPI
	}
	for _, p := range pairs {
		maker, taker, err := fetcher(lookupConfig, p.ex, p.sym)
		if err != nil {
			logger.Info("ℹ️ 定期拉取 %s 手續費失敗: %v", p.ex, err)
			continue
		}
		if taker <= 0 {
			continue
		}
		if expected, ok := lookupConfig.Exchanges[p.ex]; ok && bm.storeFetchedExchangeFee(p.ex, expected, taker) {
			logger.Info("💳 定期同步 %s Taker 手續費: %.4f%%（maker %.4f%%）", p.ex, taker*100, maker*100)
		}
	}
}

func feeLookupConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	snapshot := *cfg
	snapshot.Exchanges = make(map[string]config.ExchangeConfig, len(cfg.Exchanges))
	for exchangeName, exchangeConfig := range cfg.Exchanges {
		snapshot.Exchanges[exchangeName] = exchangeConfig
	}
	return &snapshot
}

func sameExchangeCredentials(left, right config.ExchangeConfig) bool {
	return left.APIKey == right.APIKey && left.SecretKey == right.SecretKey && left.Passphrase == right.Passphrase && left.Testnet == right.Testnet
}

func (bm *BotManager) storeFetchedExchangeFee(exchangeName string, expected config.ExchangeConfig, taker float64) bool {
	if bm == nil {
		return false
	}
	bm.equityConfigRefreshMu.Lock()
	defer bm.equityConfigRefreshMu.Unlock()
	if bm.cfg == nil {
		return false
	}
	current, ok := bm.cfg.Exchanges[exchangeName]
	if !ok || !sameExchangeCredentials(current, expected) {
		return false
	}
	current.FeeRate = taker
	bm.cfg.Exchanges[exchangeName] = current
	return true
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
		// Missing scope is not evidence that a financial runtime is unrelated.
		if br.conflictScope == nil || config.BotsConflict(br.conflictScope, newBot) {
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
	bm.startFail[botID] = botStartFailure{At: time.Now()}
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
	return botStartFailureCode, rec.At, true
}

// StartBot 啟動指定 Bot
func (bm *BotManager) StartBot(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	return bm.startBotWithValidation(ctx, botCfg)
}

func (bm *BotManager) startBotWithValidation(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	if bm == nil || ctx == nil {
		return nil, fmt.Errorf("Bot start requires manager and context")
	}
	botID := config.BotIDOrGenerate(botCfg)
	// A runtime remains registered while its stop callback drains and verifies
	// exchange state. Treat a duplicate start during that interval as an
	// idempotent no-op instead of waiting on the lifecycle lock (which the stop
	// callback may itself need to let go of external synchronization).
	bm.runtimesMu.RLock()
	existing := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	if existing != nil && existing.stopTransitionPending() {
		if existing.stopTransitionInProgress.Load() {
			return nil, nil
		}
		return nil, fmt.Errorf("Bot stop persistence is pending")
	}
	if existing != nil {
		return nil, nil
	}
	var runtime *BotRuntime
	err := bm.WithBotStrategyConfigurationContext(ctx, botID, func(managed bool) error {
		if managed {
			br, _ := bm.Get(botID)
			if br != nil && br.stopTransitionPending() {
				if br.stopTransitionInProgress.Load() {
					return nil
				}
				return fmt.Errorf("Bot stop persistence is pending")
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		unlockJournal, err := bm.lockStopJournal(ctx, botID)
		if err != nil {
			return err
		}
		defer unlockJournal()
		if err := bm.retireCompletedShutdownIntent(botID); err != nil {
			return err
		}
		bm.startConfigValidatorMu.RLock()
		configuredValidator := bm.startConfigValidator
		bm.startConfigValidatorMu.RUnlock()
		if configuredValidator != nil {
			if err := configuredValidator(botCfg); err != nil {
				startErr := fmt.Errorf("validate Bot configuration before start: %w", err)
				bm.recordStartFailure(botID, startErr)
				return startErr
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var startErr error
		runtime, startErr = bm.startBotUnderTransition(ctx, botCfg)
		return startErr
	})
	return runtime, err
}

func (bm *BotManager) startBotUnderTransition(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	botID := config.BotIDOrGenerate(botCfg)
	botCfg.ID = botID
	var err error
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
				"error":    botStartFailureCode,
			},
		})
		err := fmt.Errorf("bot_disabled_in_database: %s", reason)
		bm.recordStartFailure(botID, err)
		return nil, err
	}

	botCfg, err = bm.prepareBotStartConfig(botCfg)
	if err != nil {
		bm.recordStartFailure(botID, err)
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    botStartFailureCode,
			},
		})
		return nil, err
	}
	finishStartup, err := bm.reserveBotStartup(botCfg)
	if err != nil {
		bm.recordStartFailure(botID, err)
		return nil, err
	}
	defer finishStartup()
	botCfg.Strategies = config.CloneStrategyInstances(botCfg.Strategies)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bm.applyExchangeFeeFromAPIForBot(botCfg)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	symCfg := config.BotConfigToSymbolConfig(botCfg)
	onRequestStop := func(botID string) {
		_ = bm.StopBotWithReason(botID, "close_condition", "關閉條件觸發")
	}
	bm.openingPauseCoordinatorMu.RLock()
	pauseCoordinator := bm.openingPauseCoordinator
	bm.openingPauseCoordinatorMu.RUnlock()
	var startupPauseHolders []storage.OpeningPauseHolder
	finishPauseAdmission := func() {}
	if pauseCoordinator != nil {
		startupPauseHolders, finishPauseAdmission = pauseCoordinator.BeginBotStart()
		ctx = execution.WithOpeningAdmissionCheck(ctx, pauseCoordinator.OpeningAdmissionAllowed)
	}
	defer finishPauseAdmission()
	rt, err := startSymbolRuntime(ctx, bm.cfg, symCfg, bm.eventBus, bm.storageService, bm.distributedLock, onRequestStop, startupPauseHolders)
	if err != nil {
		// 发布启动失败事件
		bm.eventBus.Publish(&event.Event{
			Type: event.EventTypeTradingStartFailed,
			Data: map[string]interface{}{
				"bot_id":   botID,
				"exchange": botCfg.Exchange,
				"symbol":   botCfg.Symbol,
				"error":    botStartFailureCode,
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
		Config:                botCfg,
		strategySnapshotReady: true,
		BotID:                 botID,
		Inner:                 rt,
		EventBus:              bm.eventBus,
	}
	scope := botConflictScopeSnapshot(botCfg)
	br.conflictScope = &scope
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
	if bm == nil || botID == "" {
		return fmt.Errorf("Bot stop requires manager and Bot identity")
	}
	unlockLifecycle := bm.lockBotLifecycle(botID)
	defer unlockLifecycle()
	finishTransition, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return fmt.Errorf("process shutdown owns Bot stop: %w", err)
	}
	defer finishTransition()
	return bm.stopBotWithReason(botID, updatedBy, reason)
}

// StopBotsAndPersistRemoval keeps Bot starts excluded until all requested
// runtimes are safely stopped and their removal is durably persisted.
func (bm *BotManager) StopBotsAndPersistRemoval(botIDs []string, persistRemoval func() error) error {
	if bm == nil || persistRemoval == nil {
		return fmt.Errorf("Bot removal requires a manager and persistence callback")
	}
	orderedIDs := append([]string(nil), botIDs...)
	sort.Strings(orderedIDs)
	uniqueIDs := orderedIDs[:0]
	for _, botID := range orderedIDs {
		if botID == "" {
			return fmt.Errorf("Bot removal contains an empty Bot identity")
		}
		if len(uniqueIDs) == 0 || uniqueIDs[len(uniqueIDs)-1] != botID {
			uniqueIDs = append(uniqueIDs, botID)
		}
	}
	unlockLifecycle := make([]func(), 0, len(uniqueIDs))
	for _, botID := range uniqueIDs {
		unlockLifecycle = append(unlockLifecycle, bm.lockBotLifecycle(botID))
	}
	defer func() {
		for index := len(unlockLifecycle) - 1; index >= 0; index-- {
			unlockLifecycle[index]()
		}
	}()
	finishTransition, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return fmt.Errorf("process shutdown owns Bot removal: %w", err)
	}
	defer finishTransition()
	for _, botID := range uniqueIDs {
		if err := bm.stopBotWithReason(botID, "web_ui", "用戶通過 Web UI 刪除"); err != nil {
			return err
		}
	}
	return persistRemoval()
}

func (bm *BotManager) stopBotWithReason(botID, updatedBy, reason string) error {
	unlockJournal, err := bm.lockStopJournal(context.Background(), botID)
	if err != nil {
		return err
	}
	defer unlockJournal()
	bm.runtimesMu.Lock()
	br, ok := bm.runtimes[botID]
	if !ok {
		bm.runtimesMu.Unlock()
		return bm.recoverDurableStop(botID, updatedBy, reason)
	}
	bm.runtimesMu.Unlock()
	br.stopTransitionInProgress.Store(true)
	defer br.stopTransitionInProgress.Store(false)

	state := br.stopIntent
	if state == nil {
		state = &storage.BotState{BotID: botID, Enabled: false, UpdatedAt: time.Now(), UpdatedBy: updatedBy, Reason: reason}
	}
	if err := bm.beginDurableStop(br, state); err != nil {
		blockRuntimeOpeningForUnverifiedStop(br)
		return fmt.Errorf("persist Bot stop intent before shutdown: %w", err)
	}
	if br.stopIntent == nil && br.Inner != nil && br.Inner.StopWithError != nil && !br.shutdownCallbackCompleted.Load() {
		if stopErr := br.Inner.StopWithError(); stopErr != nil {
			classifyRuntimeStopFailure(br, stopErr)
			if bm.runtimeAdmissions.Blocked() {
				bm.shutdownTransitionUnverified.Store(true)
			}
			return fmt.Errorf("stop Bot %s safely: %w", botID, stopErr)
		}
	} else if br.stopIntent == nil && br.Inner != nil && br.Inner.Stop != nil && !br.shutdownCallbackCompleted.Load() {
		br.Inner.Stop()
		if br.Inner.shutdownCloseUnverifiedReason() != "" {
			if bm.runtimeAdmissions.Blocked() {
				bm.shutdownTransitionUnverified.Store(true)
			}
			return fmt.Errorf("legacy Bot stop remains unverified")
		}
	}
	if br.stopIntent == nil {
		br.stopIntent = state
		br.stopPersistencePending.Store(true)
		if br.Inner != nil {
			br.Inner.controllerStopCompleted.Store(true)
		}
	}
	var stopClaims []storage.AccountWalletCapitalClaim
	if br.Inner != nil {
		stopClaims = br.Inner.capitalReservationClaims
	}
	journal := &botStopJournal{
		State: br.stopIntent, Operation: br.stopJournalOperation, Mode: botStopJournalModeDisable, Complete: true,
		CapitalClaims: durableStopClaims(stopClaims), Path: br.stopJournalFile,
	}
	if err := bm.writeStopJournal(journal, false); err != nil {
		return fmt.Errorf("persist verified Bot stop intent: %w", err)
	}
	// Keep registration while durable intent is unconfirmed. Retrying this
	// transition must never repeat the completed financial shutdown callback.
	if err := bm.saveBotStateToDB(botID, false, br.stopIntent.UpdatedBy, br.stopIntent.Reason); err != nil {
		return fmt.Errorf("persist Bot stop state: %w", err)
	}
	if err := bm.retireStopJournal(journal); err != nil {
		return fmt.Errorf("retire durable Bot stop intent: %w", err)
	}
	unregisterWebSymbolProvidersForRuntime(&br.Config)
	// Keep the owner registered until its stop/close has finished. Otherwise a
	// concurrent StartBot can claim the same symbol while the old Bot is closing.
	bm.runtimesMu.Lock()
	if bm.runtimes[botID] == br {
		delete(bm.runtimes, botID)
	}
	bm.runtimesMu.Unlock()

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
	if bm == nil || botID == "" {
		return fmt.Errorf("Bot enable requires manager and identity")
	}
	unlock := bm.lockBotLifecycle(botID)
	defer unlock()
	unlockJournal, err := bm.lockStopJournal(context.Background(), botID)
	if err != nil {
		return err
	}
	defer unlockJournal()
	if journal, err := bm.readStopJournal(botID); err != nil {
		return fmt.Errorf("durable Bot stop intent requires reconciliation before enable: %w", err)
	} else if journal != nil {
		if err := bm.retireCompletedShutdownIntent(botID); err != nil {
			return fmt.Errorf("durable Bot stop intent requires reconciliation before enable: %w", err)
		}
	}
	if br, ok := bm.Get(botID); ok && br.stopTransitionPending() {
		return fmt.Errorf("Bot stop persistence is pending")
	}
	state := &storage.BotState{BotID: botID, Enabled: true, UpdatedAt: time.Now(), UpdatedBy: "web_ui", Reason: "用戶通過 Web UI 啟用"}
	if bm.storageService != nil {
		if store := bm.storageService.GetStorage(); store != nil {
			// Start reads the primary store when available. A file fallback cannot
			// establish enablement if the authoritative database write failed.
			if err := store.SetBotState(state); err != nil {
				return fmt.Errorf("persist Bot enable state: %w", err)
			}
			logger.Info("✅ [%s] Bot 已在數據庫中標記為啟用", botID)
			return nil
		}
	}
	if err := bm.saveBotStateToFile(state); err != nil {
		return fmt.Errorf("persist fallback Bot enable state: %w", err)
	}
	logger.Info("✅ [%s] Bot 已在回退文件中標記為啟用", botID)
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
	// Snapshot before taking the manager lock; hot callbacks can inspect the
	// manager while holding configMu, so never nest configMu below runtimesMu.
	br.configMu.Lock()
	if !br.strategySnapshotReady {
		br.Config.Strategies = config.CloneStrategyInstances(br.Config.Strategies)
		br.strategySnapshotReady = true
	}
	scope := botConflictScopeSnapshot(br.Config)
	br.configMu.Unlock()
	bm.runtimesMu.Lock()
	if existing := bm.runtimes[br.BotID]; existing != br || br.conflictScope == nil {
		br.conflictScope = &scope
	}
	if bm.groupLegAlerted == nil {
		bm.groupLegAlerted = make(map[string]bool)
	}
	bm.runtimes[br.BotID] = br
	bm.runtimesMu.Unlock()
	bm.checkGroupLegConsistencyForBot(br.BotID)
}

// StopAll journals each runtime shutdown before financial callbacks. A clean
// shutdown preserves the Bot's enabled state; interrupted stops block restart.
func (bm *BotManager) StopAll() error {
	finishTransition, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return fmt.Errorf("process shutdown owns Bot stop-all transition: %w", err)
	}
	defer finishTransition()

	runtimes := bm.List()
	var stopErrors []error
	for _, br := range runtimes {
		if br == nil {
			continue
		}
		if err := bm.stopAllRuntimeWithLifecycle(br); err != nil {
			stopErrors = append(stopErrors, err)
		}
	}
	if len(stopErrors) == 0 {
		bm.runtimesMu.Lock()
		bm.groupLegAlerted = make(map[string]bool)
		for _, timer := range bm.groupLegTimers {
			if timer != nil {
				timer.Stop()
			}
		}
		bm.groupLegTimers = make(map[string]*time.Timer)
		bm.runtimesMu.Unlock()
	}
	return errors.Join(stopErrors...)
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
		if running == total {
			source := singleLegPauseSource(group.ID)
			for _, id := range group.BotIDs {
				if bot, ok := bm.Get(id); ok && bot != nil {
					bot.ResumeOpeningForSource(source)
				}
			}
		}
	}
}

func singleLegPauseSource(groupID string) string {
	return "single_leg_group:" + groupID
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

	source := singleLegPauseSource(groupID)
	for _, id := range runningIDs {
		if br, ok := bm.Get(id); ok && br != nil {
			br.PauseOpeningForSource(source, "single_leg_running")
		}
	}

	bm.runtimesMu.RLock()
	fullyRunning := true
	for _, id := range targetGroup.BotIDs {
		if _, ok := bm.runtimes[id]; !ok {
			fullyRunning = false
			break
		}
	}
	bm.runtimesMu.RUnlock()
	if fullyRunning {
		for _, id := range targetGroup.BotIDs {
			if br, ok := bm.Get(id); ok && br != nil {
				br.ResumeOpeningForSource(source)
			}
		}
		return
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
	symbol, marketType := br.closePositionScopeSnapshot()

	botNet, netVerified := spm.GetNetPositionQtyVerified()
	if !netVerified {
		return nil, fmt.Errorf("local Bot position quantity is not finite or cannot be verified")
	}

	if config.IsSpotMarketType(marketType) {
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

func (br *BotRuntime) closePositionScopeSnapshot() (string, string) {
	br.configMu.RLock()
	defer br.configMu.RUnlock()
	return br.Config.Symbol, br.Config.GetMarketType()
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
	br.pauseTransitionMu.Lock()
	defer br.pauseTransitionMu.Unlock()
	br.configMu.Lock()
	defer br.configMu.Unlock()
	if !br.supportsRiskControlUpdates() {
		return fmt.Errorf("runtime does not support hot risk-control updates")
	}
	previousOpen := config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	previousGrid := br.Config.GridRiskControl

	copy := config.BotRiskControl{}
	if riskControl != nil {
		copy = *riskControl
	}
	br.Config.OpenPositionControl.BotRiskControl = &copy
	if err := br.publishRiskControlsLocked(); err != nil {
		return br.rollbackRiskControlsLocked(previousOpen, previousGrid, err)
	}
	if autoResumeSettingsChanged(previousOpen, br.Config.OpenPositionControl) {
		br.autoResumeGeneration++
	}
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
	if err := grc.Validate("bot.grid_risk_control"); err != nil {
		return err
	}
	br.configMu.Lock()
	defer br.configMu.Unlock()
	if br.Inner != nil && br.Inner.SuperPositionManager == nil {
		if br.Inner.UpdateOpenControl == nil || br.Config.GridRiskControl != grc {
			return fmt.Errorf("specialized runtime does not support grid-risk-control updates")
		}
	}
	previousOpen := config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	previousGrid := br.Config.GridRiskControl
	br.Config.GridRiskControl = grc
	return br.rollbackRiskControlsLocked(previousOpen, previousGrid, br.publishRiskControlsLocked())
}

// SetRiskControls atomically applies a combined Bot/grid API patch to the real
// executor snapshot; readers never observe only half of the request.
func (br *BotRuntime) SetRiskControls(rc *config.BotRiskControl, grid config.GridRiskControl) error {
	if err := grid.Validate("bot.grid_risk_control"); err != nil {
		return err
	}
	br.pauseTransitionMu.Lock()
	defer br.pauseTransitionMu.Unlock()
	br.configMu.Lock()
	defer br.configMu.Unlock()
	if !br.supportsRiskControlUpdates() {
		return fmt.Errorf("runtime does not support hot risk-control updates")
	}
	if br.Inner.SuperPositionManager == nil && br.Config.GridRiskControl != grid {
		return fmt.Errorf("specialized runtime does not support grid-risk-control updates")
	}
	previousOpen := config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	previousGrid := br.Config.GridRiskControl
	copy := config.BotRiskControl{}
	if rc != nil {
		copy = *rc
	}
	br.Config.OpenPositionControl.BotRiskControl = &copy
	br.Config.GridRiskControl = grid
	if err := br.publishRiskControlsLocked(); err != nil {
		return br.rollbackRiskControlsLocked(previousOpen, previousGrid, err)
	}
	if autoResumeSettingsChanged(previousOpen, br.Config.OpenPositionControl) {
		br.autoResumeGeneration++
	}
	return nil
}

func autoResumeSettingsChanged(previous, current config.OpenPositionControl) bool {
	if previous.PauseOpening != current.PauseOpening {
		return true
	}
	previousRisk, currentRisk := previous.BotRiskControl, current.BotRiskControl
	if previousRisk == nil {
		previousRisk = &config.BotRiskControl{}
	}
	if currentRisk == nil {
		currentRisk = &config.BotRiskControl{}
	}
	return previousRisk.PauseOpening != currentRisk.PauseOpening ||
		previousRisk.PauseOpeningReason != currentRisk.PauseOpeningReason ||
		previousRisk.AutoResumeAfter != currentRisk.AutoResumeAfter
}

func (br *BotRuntime) rollbackRiskControlsLocked(previousOpen config.OpenPositionControl, previousGrid config.GridRiskControl, applyErr error) error {
	if applyErr == nil {
		return nil
	}
	br.Config.OpenPositionControl = previousOpen
	br.Config.GridRiskControl = previousGrid
	return applyErr
}

func (br *BotRuntime) supportsRiskControlUpdates() bool {
	return br != nil && br.Inner != nil && (br.Inner.SuperPositionManager != nil || br.Inner.UpdateOpenControl != nil)
}

func (br *BotRuntime) publishRiskControlsLocked() error {
	if br.Inner != nil && br.Inner.verifiedCapitalBudget > 0 {
		if err := applyBotCapitalLimit(&br.Config.OpenPositionControl, br.Inner.verifiedCapitalBudget); err != nil {
			if br.Inner.OpeningGate != nil {
				br.Inner.OpeningGate.Block("verified_capital_budget_invalid")
			}
			return fmt.Errorf("verified Bot capital ceiling is invalid: %w", err)
		}
	}
	if spm := br.superPositionManager(); spm != nil {
		spm.SetRiskControls(config.RiskControls{Open: br.Config.OpenPositionControl, Grid: br.Config.GridRiskControl})
		if br.Inner.DynamicAdjuster != nil {
			br.Inner.DynamicAdjuster.RefreshRiskControls()
		}
		if br.Inner.OpeningGate != nil {
			br.Inner.OpeningGate.Unblock("risk_control_update_unverified")
		}
		return nil
	}
	if br.Inner != nil && br.Inner.UpdateOpenControl != nil {
		if err := br.Inner.UpdateOpenControl(config.CloneOpenPositionControl(br.Config.OpenPositionControl)); err != nil {
			if br.Inner.OpeningGate != nil {
				br.Inner.OpeningGate.Block("risk_control_update_unverified")
			}
			return err
		}
		if br.Inner.OpeningGate != nil {
			br.Inner.OpeningGate.Unblock("risk_control_update_unverified")
		}
		return nil
	}
	if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Block("risk_control_update_unverified")
	}
	return fmt.Errorf("runtime has no risk-control application path")
}

// PauseOpening 暂停开仓。自动恢复必须由显式限时暂停请求指定，风控调用默认保持暂停。
func (br *BotRuntime) PauseOpening(reason string) {
	br.pauseOpening(reason, 0, false)
}

// PauseOpeningWithoutAutoResume 用于熔断、紧急操作及其他必须由来源显式解除的暂停。
func (br *BotRuntime) PauseOpeningWithoutAutoResume(reason string) {
	br.pauseOpening(reason, 0, false)
}

// PauseOpeningManually records an independent user-owned gate so automated
// risk-source recovery cannot clear an explicit operator pause.
func (br *BotRuntime) PauseOpeningManually(reason string) {
	br.pauseOpening(reason, 0, true)
}

// PauseOpeningWithAutoResume 仅供用户明确指定时长的限时暂停使用。
func (br *BotRuntime) PauseOpeningWithAutoResume(reason string, seconds int) {
	if seconds <= 0 || int64(seconds) > (1<<63-1)/int64(time.Second) {
		seconds = 0
	}
	br.pauseOpening(reason, seconds, true)
}

// PauseOpeningManuallyWithAutoResume is the explicit, timed user pause path.
func (br *BotRuntime) PauseOpeningManuallyWithAutoResume(reason string, seconds int) {
	if seconds <= 0 || int64(seconds) > (1<<63-1)/int64(time.Second) {
		seconds = 0
	}
	br.pauseOpening(reason, seconds, true)
}

// PauseOpeningForSource adds a runtime gate owned by a named subsystem.
func (br *BotRuntime) PauseOpeningForSource(source, reason string) {
	if source == "" || source == "manual" || source == "opening_manager" {
		return
	}
	if spm := br.superPositionManager(); spm != nil {
		spm.PauseOpeningForSource(source, reason)
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Block(source)
		storage.AppendBotRiskControlEvent(br.BotID, "paused", reason, source)
	}
}

// HasOpeningPauseSource reports whether this runtime's gate currently holds a
// named source, allowing shared-state reconciliation to be idempotent.
func (br *BotRuntime) HasOpeningPauseSource(source string) bool {
	if spm := br.superPositionManager(); spm != nil {
		return spm.OpeningGate().HasBlock(source)
	}
	return br.Inner != nil && br.Inner.OpeningGate != nil && br.Inner.OpeningGate.HasBlock(source)
}

// ResumeOpeningForSource releases only the gate owned by source.
func (br *BotRuntime) ResumeOpeningForSource(source string) {
	if source == "" {
		return
	}
	if spm := br.superPositionManager(); spm != nil {
		spm.ResumeOpeningForSource(source)
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Unblock(source)
		state, reason := "resumed", ""
		if br.Inner.OpeningGate.Blocked() {
			state, reason = "paused", "another opening gate remains active"
		}
		storage.AppendBotRiskControlEvent(br.BotID, state, reason, source)
	}
}

func (br *BotRuntime) pauseOpening(reason string, autoResumeSec int, manual bool) {
	br.pauseTransitionMu.Lock()
	defer br.pauseTransitionMu.Unlock()
	br.autoResumeGeneration++
	generation := br.autoResumeGeneration
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
	br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter = 0
	if manual && autoResumeSec > 0 {
		br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter = autoResumeSec
	}
	br.configMu.Unlock()

	// C2：br.Config 只是展示用拷贝，SPM 持有自己的配置；必須直接通知 SPM 才能真正停止開倉
	// （SPM.PauseOpening 會撤銷開倉委託並記錄風控事件）。在釋放 configMu 後調用，避免持鎖做網絡請求。
	if spm := br.superPositionManager(); spm != nil {
		if manual {
			spm.PauseOpeningManually(reason)
		} else {
			spm.PauseOpening(reason)
		}
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		if manual {
			br.Inner.OpeningGate.Block("manual")
		} else {
			br.Inner.OpeningGate.Block("opening_manager")
		}
		storage.AppendBotRiskControlEvent(br.BotID, "paused", reason, "runtime_gate")
	} else {
		storage.AppendBotRiskControlEvent(br.BotID, "paused", reason, "config")
	}

	// 🔥 如果设置了自动恢复时间，启动自动恢复 goroutine
	if autoResumeSec > 0 {
		go br.autoResumeAfter(autoResumeSec, generation)
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
		gate := br.Inner.OpeningGate
		wasBlocked := gate.HasBlock(equityDataUnavailableBlock)
		if paused {
			gate.Block(equityDataUnavailableBlock)
		} else {
			gate.Unblock(equityDataUnavailableBlock)
		}
		if paused && !wasBlocked && br.Inner.CancelOpeningOrders != nil {
			cancelOpenings := br.Inner.CancelOpeningOrders
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := cancelOpenings(ctx); err != nil {
					logger.Error("Bot %s: equity-data hold could not verify cancellation of owned opening orders: %v", br.BotID, err)
				}
			}()
		}
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
		if spm.OpeningGate().HasBlock("manual") {
			if err := spm.ReleaseManualOpeningPause(); err != nil {
				return err
			}
			if spm.IsOpeningPaused() {
				spm.HoldManualOpeningPause()
				return fmt.Errorf("opening remains blocked by an independent risk or recovery gate")
			}
			br.resumeOpening("manual_gate")
			return nil
		}
		if err := spm.ResumeOpeningManually(); err != nil {
			return err
		}
	} else if br.Inner != nil && br.Inner.OpeningGate != nil && br.Inner.OpeningGate.HasBlock("manual") {
		br.Inner.OpeningGate.Unblock("manual")
		if br.Inner.OpeningGate.Blocked() {
			br.Inner.OpeningGate.Block("manual")
			return fmt.Errorf("opening remains blocked by an independent risk or recovery gate")
		}
		br.resumeOpening("manual_gate")
		return nil
	}
	br.resumeOpening("manual")
	return nil
}

func (br *BotRuntime) resumeOpening(source string) {
	br.pauseTransitionMu.Lock()
	defer br.pauseTransitionMu.Unlock()
	br.resumeOpeningLocked(source)
}

func (br *BotRuntime) resumeOpeningLocked(source string) {
	// Release only the source owned by this resume path, then derive the visible
	// pause state from the independent gate owners still present.
	if spm := br.superPositionManager(); spm != nil {
		if source == "auto_timer" || source == "manual_gate" {
			if source == "auto_timer" {
				spm.OpeningGate().Unblock("manual")
			}
		} else {
			spm.ResumeOpening()
		}
		if source == "manual" {
			spm.OpeningGate().Unblock("manual")
		}
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		if source == "auto_timer" || source == "manual_gate" {
			if source == "auto_timer" {
				br.Inner.OpeningGate.Unblock("manual")
			}
		} else {
			br.Inner.OpeningGate.Unblock("opening_manager")
		}
		if source == "manual" {
			br.Inner.OpeningGate.Unblock("manual")
		}
	}

	manualPaused := false
	if spm := br.superPositionManager(); spm != nil {
		manualPaused = spm.OpeningGate().HasBlock("manual")
	} else if br.Inner != nil && br.Inner.OpeningGate != nil {
		manualPaused = br.Inner.OpeningGate.HasBlock("manual")
	}
	br.configMu.Lock()
	br.Config.OpenPositionControl = config.CloneOpenPositionControl(br.Config.OpenPositionControl)
	br.Config.OpenPositionControl.PauseOpening = manualPaused
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		br.Config.OpenPositionControl.BotRiskControl.PauseOpening = manualPaused
		if !manualPaused {
			br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason = ""
			br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter = 0
		}
	}
	br.configMu.Unlock()
	if br.superPositionManager() != nil && source != "auto_timer" && source != "manual_gate" {
		// SuperPositionManager emitted the transition for its own opening_manager source.
		return
	}
	state, reason := "resumed", ""
	if br.GetPositionStatus()["paused"] == true {
		state, reason = "paused", "another opening gate remains active"
	}
	storage.AppendBotRiskControlEvent(br.BotID, state, reason, source)
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
	totalValue, verified := spm.GetTotalPositionValueUSDTVerified()
	if !verified {
		return 0, 0, fmt.Errorf("position value is not finite or cannot be verified")
	}

	return unrealizedPnL, totalValue, nil
}

// RiskPnLQuoteAsset identifies the denomination returned by GetPositionSummary.
func (br *BotRuntime) RiskPnLQuoteAsset() string {
	if br == nil || br.Inner == nil || br.Inner.Exchange == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(br.Inner.Exchange.GetQuoteAsset()))
}

// autoResumeAfter 在指定秒数后自动恢复开仓
func (br *BotRuntime) autoResumeAfter(seconds int, generation uint64) {
	if seconds <= 0 {
		return
	}

	logger.Info("⏰ [%s] 自动恢复定时器已启动，将在 %d 秒后恢复开仓", br.BotID, seconds)

	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	<-timer.C

	br.pauseTransitionMu.Lock()
	defer br.pauseTransitionMu.Unlock()
	if generation != br.autoResumeGeneration {
		logger.Info("ℹ️ [%s] 自动恢复定时器触发，但暂停代际已变化，跳过旧恢复请求", br.BotID)
		return
	}

	// 检查是否仍处于暂停状态
	br.configMu.RLock()
	stillPaused := br.Config.OpenPositionControl.PauseOpening
	autoResumeSec := 0
	pauseReason := ""
	if br.Config.OpenPositionControl.BotRiskControl != nil {
		autoResumeSec = br.Config.OpenPositionControl.BotRiskControl.AutoResumeAfter
		pauseReason = br.Config.OpenPositionControl.BotRiskControl.PauseOpeningReason
	}
	br.configMu.RUnlock()

	if stillPaused && autoResumeSec == seconds {
		br.resumeOpeningLocked("auto_timer")
		br.configMu.RLock()
		stillPaused = br.Config.OpenPositionControl.PauseOpening
		br.configMu.RUnlock()
		if stillPaused {
			logger.Info("🔔 [%s] 人工暂停时长已到，但其他风险/恢复门闩仍有效，继续保持暂停（原因: %s）", br.BotID, pauseReason)
		} else {
			logger.Info("🔔 [%s] 人工暂停时长已到，人工暂停来源已释放（原因: %s）", br.BotID, pauseReason)
		}
	} else if stillPaused {
		logger.Info("ℹ️ [%s] 自动恢复定时器触发，但自动恢复配置已变化，跳过", br.BotID)
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
	journal, journalErr := bm.readStopJournal(botID)
	if journalErr != nil {
		return false, "durable_stop_intent_unresolved"
	}
	if journal != nil && (journal.Mode != botStopJournalModeShutdown || !journal.Complete) {
		return false, "durable_stop_intent_unresolved"
	}
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
func (bm *BotManager) saveBotStateToDB(botID string, enabled bool, updatedBy, reason string) error {
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
				// Startup reads the primary when available. A fallback cannot
				// prove that its enabled flag has been durably disabled there.
				return fmt.Errorf("persist primary Bot state: %w", err)
			} else {
				logger.Info("✅ [%s] Bot 狀態已保存到數據庫: enabled=%v, reason=%s", botID, enabled, reason)
				return nil
			}
		}
	}

	// 存儲不可用時 fallback 到文件，確保停止狀態能持久化
	if err := bm.saveBotStateToFile(state); err != nil {
		return fmt.Errorf("persist fallback Bot state: %w", err)
	} else {
		logger.Info("✅ [%s] Bot 狀態已保存到文件: enabled=%v, reason=%s", botID, enabled, reason)
	}
	return nil
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
