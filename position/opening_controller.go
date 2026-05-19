package position

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// OpeningController 開倉控制器：限倉檢查、定時規則、週期規則
type OpeningController struct {
	spm            *SuperPositionManager
	configPtr      *config.SymbolConfig
	ticker         *time.Ticker
	stopCh         chan struct{}
	running        bool
	periodicState  bool      // 當前週期狀態：true=開倉中，false=關倉中
	periodicSwitch time.Time // 下次切換時間
	mu             sync.RWMutex
}

// NewOpeningController 創建開倉控制器
func NewOpeningController(spm *SuperPositionManager, configPtr *config.SymbolConfig) *OpeningController {
	return &OpeningController{
		spm:           spm,
		configPtr:     configPtr,
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
	oc.ticker = time.NewTicker(1 * time.Minute)
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

func (oc *OpeningController) run(ticker *time.Ticker, stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			oc.check()
		}
	}
}

func (oc *OpeningController) check() {
	oc.mu.RLock()
	cfg := oc.configPtr.OpenPositionControl
	oc.mu.RUnlock()

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
	reason := oc.spm.GetOpeningPauseReason()
	if oc.spm.IsOpeningPaused() && reason == "position_limit" {
		oc.spm.ResumeOpening()
	}
}

// checkPositionLimit 限倉檢查：超限則暫停開倉並撤銷開倉委託
func (oc *OpeningController) checkPositionLimit(cfg *config.OpenPositionControl) bool {
	if cfg.MaxPositionValue <= 0 && cfg.MaxPositionLayers <= 0 {
		return false
	}

	currentPrice := oc.spm.GetLastMarketPrice()
	if currentPrice <= 0 {
		return false
	}

	totalValue := oc.spm.GetTotalPositionValueAtPrice(currentPrice)
	layers := oc.spm.GetActiveLayers()

	shouldPause := false
	if cfg.MaxPositionValue > 0 {
		// 計算實際占用資金（保證金）= 倉位價值 / 杠桿倍數
		leverage := oc.spm.GetLeverage()
		if leverage <= 0 {
			leverage = 1 // 默認無槓桿
		}
		actualMargin := totalValue / float64(leverage)
		if actualMargin >= cfg.MaxPositionValue {
			logger.Warn("🚫 [開倉管理] 實際占用資金 %.2f USDT（倉位價值 %.2f USDT / %dx槓桿）已達上限 %.2f USDT，暫停開倉",
				actualMargin, totalValue, leverage, cfg.MaxPositionValue)
			shouldPause = true
		}
	}
	if cfg.MaxPositionLayers > 0 && layers >= cfg.MaxPositionLayers {
		logger.Warn("🚫 [開倉管理] 持倉層數 %d 已達上限 %d，暫停開倉", layers, cfg.MaxPositionLayers)
		shouldPause = true
	}

	if shouldPause {
		oc.spm.PauseOpening("position_limit")
		return true
	}
	return false
}

// checkScheduleRules 定時規則檢查
func (oc *OpeningController) checkScheduleRules(cfg *config.OpenPositionControl) bool {
	if len(cfg.ScheduleRules) == 0 {
		return false
	}

	now := time.Now().UTC()
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
				oc.spm.PauseOpening("schedule")
				return true
			}
			if rule.Action == "resume" {
				oc.spm.ResumeOpening()
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

	now := time.Now()
	if now.Before(oc.periodicSwitch) {
		return oc.periodicState
	}

	if oc.periodicState {
		// 當前為開倉期，切換到關倉期
		oc.periodicState = false
		oc.periodicSwitch = now.Add(time.Duration(pr.CloseDurationMin) * time.Minute)
		oc.spm.PauseOpening("periodic")
		logger.Info("🔄 [開倉管理] 週期規則：進入關倉期 %d 分鐘", pr.CloseDurationMin)
		return true
	}
	// 當前為關倉期，切換到開倉期
	oc.periodicState = true
	oc.periodicSwitch = now.Add(time.Duration(pr.OpenDurationMin) * time.Minute)
	oc.spm.ResumeOpening()
	logger.Info("🔄 [開倉管理] 週期規則：進入開倉期 %d 分鐘", pr.OpenDurationMin)
	return true
}

// UpdateConfig 更新配置指針（熱更新時調用）
func (oc *OpeningController) UpdateConfig(configPtr *config.SymbolConfig) {
	oc.mu.Lock()
	oc.configPtr = configPtr
	oc.mu.Unlock()
}
