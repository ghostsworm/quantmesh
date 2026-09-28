package position

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// 開倉控制器自身設置的暫停原因。控制器只會解除這些原因的暫停，
// 熔斷器 / 複合風控 / 手動 / 波動率等其他來源的暫停一律不覆蓋、不解除。
const (
	openingPauseReasonPositionLimit = "position_limit"
	openingPauseReasonSchedule      = "schedule"
	openingPauseReasonPeriodic      = "periodic"
)

// openingControllerCheckInterval 開倉控制器檢查間隔
const openingControllerCheckInterval = 1 * time.Minute

// isOpeningControllerPauseReason 判斷暫停原因是否由開倉控制器設置
func isOpeningControllerPauseReason(reason string) bool {
	switch reason {
	case openingPauseReasonPositionLimit, openingPauseReasonSchedule, openingPauseReasonPeriodic:
		return true
	}
	return false
}

// isPositionLimitPauseReason 僅匹配限倉暫停
func isPositionLimitPauseReason(reason string) bool {
	return reason == openingPauseReasonPositionLimit
}

// OpeningController 開倉控制器：限倉檢查、定時規則、週期規則
type OpeningController struct {
	spm            *SuperPositionManager
	configPtr      *config.SymbolConfig
	ticker         Ticker
	stopCh         chan struct{}
	running        bool
	periodicState  bool      // 當前週期狀態：true=開倉中，false=關倉中
	periodicSwitch time.Time // 下次切換時間
	mu             sync.RWMutex
}

// NewOpeningController 創建開倉控制器
func NewOpeningController(spm *SuperPositionManager, configPtr *config.SymbolConfig) *OpeningController {
	copy := *configPtr
	copy.OpenPositionControl = config.CloneOpenPositionControl(configPtr.OpenPositionControl)
	if spm != nil {
		spm.SetOpenPositionControl(copy.OpenPositionControl)
	}
	return &OpeningController{
		spm:           spm,
		configPtr:     &copy,
		stopCh:        make(chan struct{}),
		periodicState: true, // 初始為開倉狀態
	}
}

// Start 啟動開倉控制器（每分鐘檢查一次）
func (oc *OpeningController) Start() {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if oc.running {
		logger.Warn("🔄 [開倉管理] 開倉控制器已在運行 [%s:%s]", oc.configPtr.Exchange, oc.configPtr.Symbol)
		return
	}
	oc.stopCh = make(chan struct{})
	oc.ticker = oc.clock().NewTicker(openingControllerCheckInterval)
	oc.running = true
	go oc.run(oc.ticker, oc.stopCh)
	logger.Info("🔄 [開倉管理] 開倉控制器已啟動 [%s:%s]", oc.configPtr.Exchange, oc.configPtr.Symbol)
}

// Stop 停止開倉控制器
func (oc *OpeningController) Stop() {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if !oc.running {
		return
	}
	if oc.ticker != nil {
		oc.ticker.Stop()
		oc.ticker = nil
	}
	close(oc.stopCh)
	oc.stopCh = nil
	oc.running = false
	logger.Info("🔄 [開倉管理] 開倉控制器已停止 [%s:%s]", oc.configPtr.Exchange, oc.configPtr.Symbol)
}

// clock 使用倉位管理器的時鐘（未綁定倉位管理器時為牆鐘）
func (oc *OpeningController) clock() Clock {
	if oc.spm == nil {
		return RealClock()
	}
	return oc.spm.Clock()
}

func (oc *OpeningController) run(ticker Ticker, stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C():
			oc.check()
		}
	}
}

func (oc *OpeningController) check() {
	if oc.spm == nil {
		return
	}
	cfg := oc.spm.GetRiskControls().Open

	// 1. 限倉檢查
	if oc.checkPositionLimit(&cfg) {
		return
	}

	// 2. 定時規則檢查
	if oc.checkScheduleRules(&cfg) {
		return
	}

	// 3. 週期規則檢查
	if oc.checkPeriodicRule(&cfg) {
		return
	}

	// 僅對限倉規則：若當前未超限且之前因限倉而暫停，則恢復開倉
	// 定時/週期規則只在觸發時刻切換，不做自動恢復
	oc.spm.ResumeOpeningIfOwned(isPositionLimitPauseReason)
}

// pause 以控制器來源暫停開倉；已被其他來源暫停時保持原狀
func (oc *OpeningController) pause(reason string) {
	if !oc.spm.PauseOpeningUnlessHeld(reason, isOpeningControllerPauseReason) {
		logger.Info("ℹ️ [開倉管理] 開倉已被其他來源暫停（%s），%s 規則不覆蓋", oc.spm.GetOpeningPauseReason(), reason)
	}
}

// resume 只解除控制器自己設置的暫停，絕不解除風控等其他來源的暫停
func (oc *OpeningController) resume(rule string) {
	if oc.spm.IsOpeningPaused() && !oc.spm.ResumeOpeningIfOwned(isOpeningControllerPauseReason) {
		logger.Warn("⏸️ [開倉管理] %s 規則到點恢復，但開倉被其他來源暫停（%s），不予解除", rule, oc.spm.GetOpeningPauseReason())
	}
}

// checkPositionLimit 限倉檢查：超限則暫停開倉並撤銷開倉委託
func (oc *OpeningController) checkPositionLimit(cfg *config.OpenPositionControl) bool {
	maxQty, maxValue, maxLayers := cfg.PositionLimits()
	if maxQty <= 0 && maxValue <= 0 && maxLayers <= 0 {
		return false
	}

	currentPrice := oc.spm.GetLastMarketPrice()
	totalQty, totalValue, layers, valued := oc.spm.GetPositionExposure(currentPrice)

	shouldPause := false
	if maxValue > 0 && (totalValue >= maxValue || (!valued && totalQty > 0)) {
		shouldPause = true
	}
	if maxQty > 0 && totalQty >= maxQty {
		shouldPause = true
	}
	if maxLayers > 0 && layers >= maxLayers {
		logger.Warn("🚫 [開倉管理] 持倉層數 %d 已達上限 %d，暫停開倉", layers, maxLayers)
		shouldPause = true
	}

	if shouldPause {
		oc.pause(openingPauseReasonPositionLimit)
		return true
	}
	return false
}

// checkScheduleRules 定時規則檢查
func (oc *OpeningController) checkScheduleRules(cfg *config.OpenPositionControl) bool {
	if len(cfg.ScheduleRules) == 0 {
		return false
	}

	now := oc.clock().Now().UTC()
	currentMinutes := now.Hour()*60 + now.Minute()
	weekday := int(now.Weekday()) // 0=Sunday, 6=Saturday

	for _, rule := range cfg.ScheduleRules {
		if !rule.Enabled {
			continue
		}

		ruleMinutes, ok := parseTimeHHMM(rule.Time)
		if !ok {
			continue
		}

		// 檢查星期
		if len(rule.Weekdays) > 0 {
			found := false
			for _, wd := range rule.Weekdays {
				if wd == weekday {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// 在該時間點執行：允許 1 分鐘誤差（同一分鐘內觸發）
		if currentMinutes >= ruleMinutes && currentMinutes < ruleMinutes+1 {
			if rule.Action == "pause" {
				oc.pause(openingPauseReasonSchedule)
				return true
			}
			if rule.Action == "resume" {
				oc.resume(openingPauseReasonSchedule)
				return true
			}
		}
	}

	return false
}

// parseTimeHHMM 解析 "HH:MM" 格式，返回當天從 0:00 起的分鐘數
func parseTimeHHMM(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// checkPeriodicRule 週期規則檢查
func (oc *OpeningController) checkPeriodicRule(cfg *config.OpenPositionControl) bool {
	if cfg.PeriodicRule == nil || !cfg.PeriodicRule.Enabled {
		return false
	}
	pr := cfg.PeriodicRule
	if pr.OpenDurationMin <= 0 || pr.CloseDurationMin <= 0 {
		return false
	}

	oc.mu.Lock()
	defer oc.mu.Unlock()

	now := oc.clock().Now()
	if now.Before(oc.periodicSwitch) {
		return oc.periodicState
	}

	if oc.periodicState {
		// 當前為開倉期，切換到關倉期
		oc.periodicState = false
		oc.periodicSwitch = now.Add(time.Duration(pr.CloseDurationMin) * time.Minute)
		oc.pause(openingPauseReasonPeriodic)
		logger.Info("🔄 [開倉管理] 週期規則：進入關倉期 %d 分鐘", pr.CloseDurationMin)
		return true
	}
	// 當前為關倉期，切換到開倉期
	oc.periodicState = true
	oc.periodicSwitch = now.Add(time.Duration(pr.OpenDurationMin) * time.Minute)
	oc.resume(openingPauseReasonPeriodic)
	logger.Info("🔄 [開倉管理] 週期規則：進入開倉期 %d 分鐘", pr.OpenDurationMin)
	return true
}

// UpdateConfig 更新配置指針（熱更新時調用）
func (oc *OpeningController) UpdateConfig(configPtr *config.SymbolConfig) {
	copy := *configPtr
	copy.OpenPositionControl = config.CloneOpenPositionControl(configPtr.OpenPositionControl)
	if oc.spm != nil {
		oc.spm.SetOpenPositionControl(copy.OpenPositionControl)
	}
	oc.mu.Lock()
	oc.configPtr = &copy
	oc.mu.Unlock()
}
