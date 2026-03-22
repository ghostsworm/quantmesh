package web

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/storage"
)

const basisMonitorConfigKey = "basis_monitor_config"

// BasisMonitorConfig 价差监控配置（用于 API 和数据库存储）
type BasisMonitorConfig struct {
	Enabled         bool     `json:"enabled" yaml:"enabled"`
	IntervalMinutes int      `json:"interval_minutes" yaml:"interval_minutes"`
	Symbols         []string `json:"symbols" yaml:"symbols"`
}

// BasisMonitorController 价差监控控制器，支持运行时启停和配置更新
type BasisMonitorController struct {
	mu        sync.RWMutex
	storage   storage.Storage
	getExch   func() exchange.IExchange
	current   *monitor.BasisMonitor
	cfg       *config.Config
}

// NewBasisMonitorController 创建价差监控控制器
func NewBasisMonitorController(st storage.Storage, getExch func() exchange.IExchange, cfg *config.Config) *BasisMonitorController {
	return &BasisMonitorController{storage: st, getExch: getExch, cfg: cfg}
}

// GetEffectiveConfig 获取有效配置（数据库覆盖 config.yaml）
func (c *BasisMonitorController) GetEffectiveConfig(ctx context.Context) BasisMonitorConfig {
	cfg := BasisMonitorConfig{
		Enabled:         false,
		IntervalMinutes: 1,
		Symbols:         []string{"BTCUSDT", "ETHUSDT"},
	}
	if c.cfg != nil {
		cfg.Enabled = c.cfg.BasisMonitor.Enabled
		cfg.IntervalMinutes = c.cfg.BasisMonitor.IntervalMinutes
		if cfg.IntervalMinutes <= 0 {
			cfg.IntervalMinutes = 1
		}
		if len(c.cfg.BasisMonitor.Symbols) > 0 {
			cfg.Symbols = append([]string{}, c.cfg.BasisMonitor.Symbols...)
		}
	}

	// 数据库覆盖
	p := systemSettingsProvider
	if p == nil {
		return cfg
	}
	var dbCfg BasisMonitorConfig
	if err := GetSystemSettingJSONFromProvider(ctx, p, basisMonitorConfigKey, &dbCfg); err == nil {
		if dbCfg.IntervalMinutes > 0 {
			cfg.IntervalMinutes = dbCfg.IntervalMinutes
		}
		if len(dbCfg.Symbols) > 0 {
			cfg.Symbols = dbCfg.Symbols
		}
		cfg.Enabled = dbCfg.Enabled
	}
	return cfg
}

// ApplyConfig 应用配置，启停监控
func (c *BasisMonitorController) ApplyConfig(ctx context.Context, cfg BasisMonitorConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current != nil {
		c.current.Stop()
		c.current = nil
		SetBasisMonitorProvider(nil)
	}

	if !cfg.Enabled {
		logger.Info("⏹️ 價差監控已關閉（配置 enabled=false）")
		return
	}

	ex := c.getExch()
	if ex == nil {
		logger.Warn("⚠️ 無法啟動價差監控：未獲取到交易所實例（需至少配置一個交易對）")
		return
	}

	bm := monitor.NewBasisMonitor(c.storage, ex, cfg.Symbols, cfg.IntervalMinutes)
	bm.Start()
	c.current = bm
	SetBasisMonitorProvider(bm)
	logger.Info("✅ 價差監控已啟動 (symbols=%v, interval=%d min)", cfg.Symbols, cfg.IntervalMinutes)
}

// getBasisConfig 獲取價差監控配置（合併數據庫與 config.yaml）
// GET /api/basis/config
func getBasisConfig(c *gin.Context) {
	ctrl := getBasisMonitorController()
	if ctrl == nil {
		// 無控制器時，返回 config.yaml 的配置
		cfg := globalConfig
		out := BasisMonitorConfig{
			Enabled:         cfg != nil && cfg.BasisMonitor.Enabled,
			IntervalMinutes: 1,
			Symbols:         []string{"BTCUSDT", "ETHUSDT"},
		}
		if cfg != nil {
			out.IntervalMinutes = cfg.BasisMonitor.IntervalMinutes
			if out.IntervalMinutes <= 0 {
				out.IntervalMinutes = 1
			}
			if len(cfg.BasisMonitor.Symbols) > 0 {
				out.Symbols = append([]string{}, cfg.BasisMonitor.Symbols...)
			}
		}
		c.JSON(200, gin.H{"config": out, "source": "config_only"})
		return
	}

	effective := ctrl.GetEffectiveConfig(c.Request.Context())
	c.JSON(200, gin.H{
		"config": effective,
		"source": "merged",
	})
}

// putBasisConfig 更新價差監控配置（寫入數據庫並生效）
// PUT /api/basis/config
func putBasisConfig(c *gin.Context) {
	var req struct {
		Enabled         *bool    `json:"enabled"`
		IntervalMinutes *int     `json:"interval_minutes"`
		Symbols         []string `json:"symbols"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, 400, "errors.invalid_request", map[string]interface{}{"detail": err.Error()})
		return
	}

	p := systemSettingsProvider
	if p == nil {
		respondError(c, 503, "errors.service_unavailable")
		return
	}

	ctx := c.Request.Context()

	// 讀取當前有效配置
	ctrl := getBasisMonitorController()
	effective := BasisMonitorConfig{Enabled: false, IntervalMinutes: 1, Symbols: []string{"BTCUSDT", "ETHUSDT"}}
	if ctrl != nil {
		effective = ctrl.GetEffectiveConfig(ctx)
	} else {
		cfg := globalConfig
		if cfg != nil {
			effective.Enabled = cfg.BasisMonitor.Enabled
			effective.IntervalMinutes = cfg.BasisMonitor.IntervalMinutes
			if effective.IntervalMinutes <= 0 {
				effective.IntervalMinutes = 1
			}
			if len(cfg.BasisMonitor.Symbols) > 0 {
				effective.Symbols = append([]string{}, cfg.BasisMonitor.Symbols...)
			}
		}
	}

	// 合併請求
	if req.Enabled != nil {
		effective.Enabled = *req.Enabled
	}
	if req.IntervalMinutes != nil && *req.IntervalMinutes > 0 {
		effective.IntervalMinutes = *req.IntervalMinutes
	}
	if len(req.Symbols) > 0 {
		effective.Symbols = req.Symbols
	}

	if err := SetSystemSettingJSONToProvider(ctx, p, basisMonitorConfigKey, effective); err != nil {
		respondError(c, 500, "errors.internal_error", err)
		return
	}

	if ctrl != nil {
		ctrl.ApplyConfig(ctx, effective)
	}

	c.JSON(200, gin.H{
		"ok":     true,
		"config": effective,
	})
}

var (
	basisMonitorController     *BasisMonitorController
	basisMonitorControllerMu   sync.RWMutex
)

// SetBasisMonitorController 設置價差監控控制器
func SetBasisMonitorController(ctrl *BasisMonitorController) {
	basisMonitorControllerMu.Lock()
	defer basisMonitorControllerMu.Unlock()
	basisMonitorController = ctrl
}

func getBasisMonitorController() *BasisMonitorController {
	basisMonitorControllerMu.RLock()
	defer basisMonitorControllerMu.RUnlock()
	return basisMonitorController
}
