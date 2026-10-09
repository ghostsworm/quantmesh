package risk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"
	"quantmesh/storage"
)

// DefaultBotActionTimeout 單個 Bot 撤單/平倉的預設超時。
// 場景或熔斷配置 timeout <= 0 時使用，避免 ctx 立即過期導致所有平倉單直接失敗。
const DefaultBotActionTimeout = 30 * time.Second

// 操作狀態（紧急中心 / 熔断器共用）
const (
	OperationStatusExecuting = "executing"
	OperationStatusCompleted = "completed"
	OperationStatusPartial   = "partial" // 部分 Bot 失败
	OperationStatusFailed    = "failed"  // 全部失败或动作本身失败
)

// botActionTimeout 將秒數轉為超時；<= 0 時回落到預設值
func botActionTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return DefaultBotActionTimeout
	}
	return time.Duration(seconds) * time.Second
}

// BotActionReport 單個動作在所有 Bot 上的執行彙總
type BotActionReport struct {
	Action    string
	Total     int
	Succeeded int
	Errors    []error
}

// Failed 失败的 Bot 數
func (r BotActionReport) Failed() int {
	return r.Total - r.Succeeded
}

// Status 根據成功/失败數推導狀態
func (r BotActionReport) Status() string {
	switch {
	case r.Failed() == 0:
		return OperationStatusCompleted
	case r.Succeeded == 0:
		return OperationStatusFailed
	default:
		return OperationStatusPartial
	}
}

// Err 聚合所有 Bot 的錯誤；全部成功時返回 nil
func (r BotActionReport) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %d/%d 个 Bot 失败: %w", r.Action, r.Failed(), r.Total, errors.Join(r.Errors...))
}

// Summary 人類可讀摘要
func (r BotActionReport) Summary(verb string) string {
	return fmt.Sprintf("%s %d/%d 个Bot", verb, r.Succeeded, r.Total)
}

// runOnBots 並行對每個 Bot 執行 fn，彙總錯誤（不吞錯）
func runOnBots(action string, bots []BotController, fn func(idx int, bot BotController) error) BotActionReport {
	report := BotActionReport{Action: action, Total: len(bots)}
	if len(bots) == 0 {
		return report
	}

	errs := make([]error, len(bots))
	var wg sync.WaitGroup
	for i, bot := range bots {
		wg.Add(1)
		go func(idx int, b BotController) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs[idx] = fmt.Errorf("bot#%d %s panic: %v", idx, action, r)
				}
			}()
			if err := fn(idx, b); err != nil {
				errs[idx] = fmt.Errorf("bot#%d %s: %w", idx, action, err)
			}
		}(i, bot)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			report.Errors = append(report.Errors, err)
		}
	}
	report.Succeeded = report.Total - len(report.Errors)
	return report
}

// cancelOrdersOnBots 撤銷所有 Bot 掛單
func cancelOrdersOnBots(bots []BotController) BotActionReport {
	return runOnBots("cancel_all_orders", bots, func(_ int, bot BotController) error {
		return bot.CancelAllOpenOrders()
	})
}

// closePositionsOnBots 平掉所有 Bot 倉位；每個 Bot 使用獨立超時 ctx，互不擠占
func closePositionsOnBots(parent context.Context, bots []BotController, method string, timeoutSec int) BotActionReport {
	if parent == nil {
		parent = context.Background()
	}
	timeout := botActionTimeout(timeoutSec)
	effectiveSec := int(timeout / time.Second)
	type scopedBot interface{ CloseScopeKey() string }
	type botEntry struct {
		index int
		bot   BotController
	}
	groups := make(map[string][]botEntry)
	for i, bot := range bots {
		key := ""
		if scoped, ok := bot.(scopedBot); ok {
			key = scoped.CloseScopeKey()
		}
		if key == "" {
			key = fmt.Sprintf("unscoped:%d", i)
		}
		groups[key] = append(groups[key], botEntry{index: i, bot: bot})
	}
	errs := make([]error, len(bots))
	var succeeded atomic.Int32
	var wg sync.WaitGroup
	for _, group := range groups {
		group := group
		wg.Add(1)
		go func() {
			defer wg.Done()
			for position, entry := range group {
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							errs[entry.index] = fmt.Errorf("bot#%d close_all_positions panic: %v", entry.index, recovered)
						}
					}()
					ctx, cancel := context.WithTimeout(parent, timeout)
					defer cancel()
					if err := entry.bot.CloseAllPositions(ctx, method, effectiveSec); err != nil {
						errs[entry.index] = fmt.Errorf("bot#%d close_all_positions: %w", entry.index, err)
					}
				}()
				if errs[entry.index] != nil {
					for _, skipped := range group[position+1:] {
						errs[skipped.index] = fmt.Errorf("same account close skipped after prior Bot result was unverified")
					}
					break
				}
				succeeded.Add(1)
			}
		}()
	}
	wg.Wait()
	report := BotActionReport{Action: "close_all_positions", Total: len(bots), Succeeded: int(succeeded.Load())}
	for _, err := range errs {
		if err != nil {
			report.Errors = append(report.Errors, err)
		}
	}
	return report
}

// OpeningPauseCoordinator 多來源暫停開倉協調器。
// BotController 的暫停只有單一旗標，熔斷器與複合風控各自恢復時會互相覆蓋；
// 這裡按來源記錄暫停，只有所有自動風控來源都解除後才真正 ResumeOpening。
// Web 恢復通過 RunIfUnheld 串行化；紧急中心通过同一协调器持有独立来源。
type OpeningPauseCoordinator struct {
	transitionMu        sync.Mutex
	mu                  sync.Mutex
	holders             map[string]string // source -> reason
	pending             map[string]string // local fail-closed owners whose durable write failed
	stateStore          storage.OpeningPauseStateStore
	ownerID             string
	syncHold            string
	syncHoldApplied     bool
	syncFailureReported bool
	botProvider         func() []BotController
	admissionMu         sync.RWMutex
	admissionChecks     map[string]func() bool
}

// SetOpeningAdmissionCheck registers a pure local predicate, independent of
// transitionMu: constructors hold that mutex while their producers start.
func (c *OpeningPauseCoordinator) SetOpeningAdmissionCheck(source string, check func() bool) {
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.admissionChecks == nil {
		c.admissionChecks = make(map[string]func() bool)
	}
	if check == nil {
		delete(c.admissionChecks, source)
	} else {
		c.admissionChecks[source] = check
	}
}

func (c *OpeningPauseCoordinator) OpeningAdmissionAllowed() bool {
	c.admissionMu.RLock()
	checks := make([]func() bool, 0, len(c.admissionChecks))
	for _, check := range c.admissionChecks {
		checks = append(checks, check)
	}
	c.admissionMu.RUnlock()
	for _, check := range checks {
		if !check() {
			return false
		}
	}
	return true
}

const defaultOpeningPauseSyncInterval = time.Second

const openingPauseOwnerGatePrefix = "opening_pause_owner:"

type nonAutoResumingBot interface {
	PauseOpeningWithoutAutoResume(reason string)
}

type sourceOwnedOpeningPauseBot interface {
	PauseOpeningForSource(source, reason string)
	ResumeOpeningForSource(source string)
	HasOpeningPauseSource(source string) bool
}

func pauseBotWithoutAutoResume(bot BotController, reason string) {
	if bot == nil {
		return
	}
	if pauser, ok := bot.(nonAutoResumingBot); ok {
		pauser.PauseOpeningWithoutAutoResume(reason)
		return
	}
	bot.PauseOpening(reason)
}

// NewOpeningPauseCoordinator 創建協調器
func NewOpeningPauseCoordinator() *OpeningPauseCoordinator {
	return &OpeningPauseCoordinator{holders: make(map[string]string), pending: make(map[string]string)}
}

// NewOpeningPauseCoordinatorWithStore restores durable risk owners before any
// Bot is allowed to start. On read failure it returns a fail-closed coordinator
// together with the error so the caller can keep retrying synchronization.
func NewOpeningPauseCoordinatorWithStore(ctx context.Context, store storage.OpeningPauseStateStore, instanceIdentity ...string) (*OpeningPauseCoordinator, error) {
	if store == nil {
		return nil, fmt.Errorf("durable opening pause state store is required")
	}
	identity := ""
	if len(instanceIdentity) > 0 {
		identity = strings.TrimSpace(instanceIdentity[0])
	}
	ownerID, err := newOpeningPauseOwnerID(identity)
	if err != nil {
		return nil, fmt.Errorf("create opening pause owner identity: %w", err)
	}
	c := &OpeningPauseCoordinator{holders: make(map[string]string), pending: make(map[string]string), stateStore: store, ownerID: ownerID}
	loadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	rows, err := store.LoadOpeningPauseHolders(loadCtx)
	cancel()
	if err != nil {
		c.installSyncFailureHold(nil)
		return c, fmt.Errorf("load durable opening pause owners: %w", err)
	}
	for _, row := range rows {
		source := strings.TrimSpace(row.Source)
		if !validOpeningPauseHolder(row.OwnerID, source) {
			c.installSyncFailureHold(nil)
			return c, fmt.Errorf("durable opening pause state contains an invalid source")
		}
		gateSource := openingPauseGateSource(row.OwnerID, source)
		if _, duplicate := c.holders[gateSource]; duplicate {
			c.installSyncFailureHold(nil)
			return c, fmt.Errorf("durable opening pause state contains duplicate source %q", source)
		}
		c.holders[gateSource] = row.Reason
	}
	return c, nil
}

func newOpeningPauseOwnerID(identity string) (string, error) {
	if identity != "" {
		digest := sha256.Sum256([]byte(identity))
		return hex.EncodeToString(digest[:16]), nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func openingPauseGateSource(ownerID, source string) string {
	if ownerID == "" {
		return source
	}
	return openingPauseOwnerGatePrefix + ownerID + ":" + source
}

func validOpeningPauseHolder(ownerID, source string) bool {
	if source == "" || len(ownerID) > 64 || len(source) > 128 {
		return false
	}
	return ownerID == "" || len(source) <= 96
}

// Pause 以 source 身份暫停所有 Bot 開倉
func (c *OpeningPauseCoordinator) Pause(source, reason string, bots []BotController) error {
	source = strings.TrimSpace(source)
	if c == nil || source == "" {
		return fmt.Errorf("opening pause coordinator and source are required")
	}
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()
	if c.botProvider != nil {
		bots = c.botProvider()
	}
	gateSource := openingPauseGateSource(c.ownerID, source)
	// Local risk admission must not wait for a slow/uncertain database ACK.
	// Retain a pending owner before writing so failed persistence cannot erase
	// the hold during a later shared-state refresh.
	c.mu.Lock()
	c.holders[gateSource] = reason
	if c.stateStore != nil {
		c.pending[gateSource] = reason
	} else {
		delete(c.pending, gateSource)
	}
	c.mu.Unlock()
	for _, bot := range bots {
		if sourceOwned, ok := bot.(sourceOwnedOpeningPauseBot); ok {
			sourceOwned.PauseOpeningForSource(gateSource, reason)
		} else {
			pauseBotWithoutAutoResume(bot, reason)
		}
	}
	var persistErr error
	if c.stateStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		persistErr = c.stateStore.UpsertOpeningPauseHolder(ctx, storage.OpeningPauseHolder{OwnerID: c.ownerID, Source: source, Reason: reason})
		cancel()
		if persistErr == nil {
			c.mu.Lock()
			delete(c.pending, gateSource)
			c.mu.Unlock()
		}
	}
	if persistErr != nil {
		return fmt.Errorf("persist opening pause owner %q: %w", source, persistErr)
	}
	return nil
}

// Release 解除 source 的暫停；若仍有其他來源持有暫停則不恢復，返回是否真正恢復
func (c *OpeningPauseCoordinator) Release(source string, bots []BotController) bool {
	resumed, err := c.ReleaseChecked(source, bots)
	if err != nil {
		logger.Error("risk pause source %q remains held because durable release failed: %v", source, err)
	}
	return resumed
}

// ReleaseChecked preserves the hold and reports persistence errors to callers
// that expose an operator-visible recovery result.
func (c *OpeningPauseCoordinator) ReleaseChecked(source string, bots []BotController) (bool, error) {
	if c == nil || strings.TrimSpace(source) == "" {
		return false, fmt.Errorf("opening pause coordinator and source are required")
	}
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()
	if c.botProvider != nil {
		bots = c.botProvider()
	}
	gateSource := openingPauseGateSource(c.ownerID, source)
	if c.stateStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.stateStore.DeleteOpeningPauseHolder(ctx, storage.OpeningPauseHolder{OwnerID: c.ownerID, Source: source})
		if err == nil {
			c.mu.Lock()
			_, hasLegacySource := c.holders[source]
			c.mu.Unlock()
			if hasLegacySource {
				err = c.stateStore.DeleteOpeningPauseHolder(ctx, storage.OpeningPauseHolder{Source: source})
			}
		}
		if err == nil {
			rows, loadErr := c.stateStore.LoadOpeningPauseHolders(ctx)
			if loadErr != nil {
				err = fmt.Errorf("verify remaining opening pause owners after release: %w", loadErr)
			} else {
				err = c.syncPersistentHoldersLocked(rows, bots)
			}
		}
		cancel()
		if err != nil {
			if c.stateStore != nil {
				c.mu.Lock()
				reason := c.holders[gateSource]
				if reason == "" {
					reason = "解除后的共享风控暂停状态无法核实"
				}
				c.mu.Unlock()
				restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 5*time.Second)
				restoreErr := c.stateStore.UpsertOpeningPauseHolder(restoreCtx, storage.OpeningPauseHolder{OwnerID: c.ownerID, Source: source, Reason: reason})
				restoreCancel()
				c.mu.Lock()
				c.holders[gateSource] = reason
				if restoreErr != nil {
					c.pending[gateSource] = reason
				}
				c.mu.Unlock()
				if restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("restore fail-closed owner after unverified release: %w", restoreErr))
				}
			}
			c.installSyncFailureHold(bots)
			return false, fmt.Errorf("persist release of opening pause owner %q: %w", source, err)
		}
	}
	c.mu.Lock()
	delete(c.pending, gateSource)
	delete(c.holders, gateSource)
	delete(c.holders, source)
	remaining := make([]string, 0, len(c.holders))
	for s := range c.holders {
		remaining = append(remaining, s)
	}
	c.mu.Unlock()
	if c.stateStore == nil {
		for _, bot := range bots {
			if sourceOwned, ok := bot.(sourceOwnedOpeningPauseBot); ok {
				sourceOwned.ResumeOpeningForSource(gateSource)
			}
		}
	}

	if len(remaining) > 0 {
		sort.Strings(remaining)
		logger.Warn("⏸️ [风控协调] %s 已解除，但仍被 %v 暂停开仓，暂不恢复", source, remaining)
		return false, nil
	}
	for _, bot := range bots {
		if _, sourceOwned := bot.(sourceOwnedOpeningPauseBot); !sourceOwned {
			bot.ResumeOpening()
		}
	}
	return true, nil
}

// BeginBotStart serializes startup gate installation against risk transitions.
// The returned unlock function must be called only after the runtime is
// registered (or startup has failed).
func (c *OpeningPauseCoordinator) BeginBotStart() ([]storage.OpeningPauseHolder, func()) {
	c.transitionMu.Lock()
	if c.stateStore != nil {
		var bots []BotController
		if c.botProvider != nil {
			bots = c.botProvider()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rows, err := c.stateStore.LoadOpeningPauseHolders(ctx)
		cancel()
		if err != nil {
			c.installSyncFailureHold(bots)
		} else if err := c.syncPersistentHoldersLocked(rows, bots); err != nil {
			c.installSyncFailureHold(bots)
		}
	}
	c.mu.Lock()
	holders := make([]storage.OpeningPauseHolder, 0, len(c.holders))
	for source, reason := range c.holders {
		holders = append(holders, storage.OpeningPauseHolder{Source: source, Reason: reason})
	}
	c.mu.Unlock()
	sort.Slice(holders, func(i, j int) bool { return holders[i].Source < holders[j].Source })
	return holders, c.transitionMu.Unlock
}

// StartPersistentSync refreshes durable owners so independent processes install
// and release the same named gates. The bounded poll is not exchange-side
// fencing; callers must not treat it as protection for orders already in flight.
func (c *OpeningPauseCoordinator) StartPersistentSync(ctx context.Context, botProvider func() []BotController) {
	if c == nil || c.stateStore == nil || botProvider == nil {
		return
	}
	c.transitionMu.Lock()
	c.botProvider = botProvider
	c.transitionMu.Unlock()
	reportSync := func(syncErr error) {
		c.mu.Lock()
		logFailure := syncErr != nil && !c.syncFailureReported
		logRecovery := syncErr == nil && c.syncFailureReported
		c.syncFailureReported = syncErr != nil
		c.mu.Unlock()
		if logFailure {
			logger.Error("opening pause owner sync failed; fail-closed gate retained: %v", syncErr)
		} else if logRecovery {
			logger.Info("opening pause owner sync recovered; authoritative shared owner set is active")
		}
	}
	reportSync(c.syncPersistentHoldersFromProvider(ctx, botProvider))
	go func() {
		ticker := time.NewTicker(defaultOpeningPauseSyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reportSync(c.syncPersistentHoldersFromProvider(ctx, botProvider))
			}
		}
	}()
}

// SyncPersistentHolders reconciles other process owners into this process's
// running Bot gates. A read failure blocks current and future Bots until a
// later successful read proves the durable owner set again.
func (c *OpeningPauseCoordinator) SyncPersistentHolders(ctx context.Context, bots []BotController) error {
	return c.syncPersistentHoldersFromProvider(ctx, func() []BotController { return bots })
}

func (c *OpeningPauseCoordinator) syncPersistentHoldersFromProvider(ctx context.Context, botProvider func() []BotController) error {
	if c == nil || c.stateStore == nil {
		return fmt.Errorf("durable opening pause state store is required")
	}
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()
	bots := botProvider()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	rows, err := c.stateStore.LoadOpeningPauseHolders(readCtx)
	cancel()
	if err != nil {
		c.installSyncFailureHold(bots)
		return fmt.Errorf("load shared opening pause owners: %w", err)
	}
	if err := c.syncPersistentHoldersLocked(rows, bots); err != nil {
		c.installSyncFailureHold(bots)
		return err
	}
	return nil
}

func (c *OpeningPauseCoordinator) syncPersistentHoldersLocked(rows []storage.OpeningPauseHolder, bots []BotController) error {
	desired := make(map[string]string, len(rows))
	for _, row := range rows {
		source := strings.TrimSpace(row.Source)
		if !validOpeningPauseHolder(row.OwnerID, source) {
			return fmt.Errorf("shared opening pause state contains an invalid source")
		}
		gateSource := openingPauseGateSource(row.OwnerID, source)
		if _, duplicate := desired[gateSource]; duplicate {
			return fmt.Errorf("shared opening pause state contains duplicate source %q", source)
		}
		desired[gateSource] = row.Reason
	}

	c.mu.Lock()
	previous := make(map[string]string, len(c.holders))
	for source, reason := range c.holders {
		previous[source] = reason
	}
	for source := range previous {
		if _, exists := desired[source]; !exists && c.pending[source] == "" {
			delete(c.holders, source)
		}
	}
	for source, reason := range desired {
		c.holders[source] = reason
	}
	if c.syncHold != "" {
		if _, stillDesired := desired[c.syncHold]; !stillDesired {
			delete(c.holders, c.syncHold)
		}
		c.syncHold = ""
		c.syncHoldApplied = false
	}
	toApply := make(map[string]string, len(c.holders))
	for source, reason := range c.holders {
		toApply[source] = reason
	}
	removed := make([]string, 0)
	for source := range previous {
		if _, exists := desired[source]; !exists && c.pending[source] == "" {
			removed = append(removed, source)
		}
	}
	remaining := len(c.holders)
	c.mu.Unlock()
	for source, reason := range toApply {
		_, existed := previous[source]
		for _, bot := range bots {
			if sourceOwned, ok := bot.(sourceOwnedOpeningPauseBot); ok {
				if !sourceOwned.HasOpeningPauseSource(source) {
					sourceOwned.PauseOpeningForSource(source, reason)
				}
			} else if !existed {
				pauseBotWithoutAutoResume(bot, reason)
			}
		}
	}
	for _, source := range removed {
		for _, bot := range bots {
			if sourceOwned, ok := bot.(sourceOwnedOpeningPauseBot); ok {
				sourceOwned.ResumeOpeningForSource(source)
			} else if remaining == 0 {
				bot.ResumeOpening()
			}
		}
	}
	return nil
}

func (c *OpeningPauseCoordinator) installSyncFailureHold(bots []BotController) {
	if c.syncHold == "" {
		c.syncHold = "opening_pause_sync_unverified:" + c.ownerID
	}
	const reason = "共享风控暂停状态无法核实"
	c.mu.Lock()
	c.holders[c.syncHold] = reason
	applyHold := !c.syncHoldApplied && len(bots) > 0
	if applyHold {
		c.syncHoldApplied = true
	}
	c.mu.Unlock()
	if applyHold {
		for _, bot := range bots {
			if sourceOwned, ok := bot.(sourceOwnedOpeningPauseBot); ok {
				sourceOwned.PauseOpeningForSource(c.syncHold, reason)
			} else {
				pauseBotWithoutAutoResume(bot, reason)
			}
		}
	}
}

// RunIfUnheld atomically checks that no coordinated risk source is holding the
// global pause and runs an explicit recovery action while new holds are
// serialized behind it. It returns false without running action when held.
func (c *OpeningPauseCoordinator) RunIfUnheld(action func() error) (bool, error) {
	if action == nil {
		return false, fmt.Errorf("recovery action is required")
	}
	c.transitionMu.Lock()
	defer c.transitionMu.Unlock()
	if c.stateStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rows, err := c.stateStore.LoadOpeningPauseHolders(ctx)
		cancel()
		if err != nil {
			var bots []BotController
			if c.botProvider != nil {
				bots = c.botProvider()
			}
			c.installSyncFailureHold(bots)
			return false, fmt.Errorf("verify shared opening pause owners before recovery: %w", err)
		}
		var bots []BotController
		if c.botProvider != nil {
			bots = c.botProvider()
		}
		if err := c.syncPersistentHoldersLocked(rows, bots); err != nil {
			c.installSyncFailureHold(bots)
			return false, err
		}
	}
	c.mu.Lock()
	if len(c.holders) > 0 {
		c.mu.Unlock()
		return false, nil
	}
	c.mu.Unlock()
	return true, action()
}

// IsHeldBy source 是否持有暫停
func (c *OpeningPauseCoordinator) IsHeldBy(source string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for gateSource := range c.holders {
		if gateSource == source || strings.HasSuffix(gateSource, ":"+source) {
			return true
		}
	}
	return false
}

// Holders 當前持有暫停的來源快照
func (c *OpeningPauseCoordinator) Holders() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.holders))
	for k, v := range c.holders {
		out[k] = v
	}
	return out
}
