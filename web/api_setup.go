package web

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

// SetupStatusResponse 配置状态响应
type SetupStatusResponse struct {
	NeedsSetup bool   `json:"needs_setup"`
	ConfigPath string `json:"config_path"`
}

// getSetupStatusHandler 获取配置状态
// GET /api/setup/status
func getSetupStatusHandler(c *gin.Context) {
	configPath := "config.yaml"
	if configManager != nil {
		configPath = configManager.GetConfigPath()
	}

	// 检查配置文件是否存在
	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPath,
		})
		return
	}

	// 检查配置是否完整
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		c.JSON(http.StatusOK, SetupStatusResponse{
			NeedsSetup: true,
			ConfigPath: configPath,
		})
		return
	}

	// 检查配置是否完整
	needsSetup := cfg.App.CurrentExchange == "" ||
		len(cfg.Exchanges) == 0 ||
		cfg.Exchanges[cfg.App.CurrentExchange].APIKey == "" ||
		cfg.Exchanges[cfg.App.CurrentExchange].SecretKey == "" ||
		len(cfg.Trading.Symbols) == 0 ||
		cfg.Trading.Symbols[0].Symbol == ""

	c.JSON(http.StatusOK, SetupStatusResponse{
		NeedsSetup: needsSetup,
		ConfigPath: configPath,
	})
}

// SetupInitRequest 配置初始化请求
type SetupInitRequest struct {
	Exchange       string  `json:"exchange" binding:"required"`
	APIKey         string  `json:"api_key" binding:"required"`
	SecretKey      string  `json:"secret_key" binding:"required"`
	Passphrase     string  `json:"passphrase,omitempty"`
	Symbol         string  `json:"symbol" binding:"required"`
	PriceInterval  float64 `json:"price_interval" binding:"required,gt=0"`
	OrderQuantity  float64 `json:"order_quantity" binding:"required,gt=0"`
	MinOrderValue  float64 `json:"min_order_value,omitempty"`
	BuyWindowSize  int     `json:"buy_window_size" binding:"required,gt=0"`
	SellWindowSize int     `json:"sell_window_size,omitempty"`
	Testnet        bool    `json:"testnet,omitempty"`
	FeeRate        float64 `json:"fee_rate,omitempty"`
}

// SetupInitResponse 配置初始化响应
type SetupInitResponse struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	RequiresRestart bool   `json:"requires_restart"`
}

// initSetupHandler 初始化配置
// POST /api/setup/init
func initSetupHandler(c *gin.Context) {
	var req SetupInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, SetupInitResponse{
			Success: false,
			Message: "请求参数错误: " + err.Error(),
		})
		return
	}

	// 创建配置数据
	setupData := &config.SetupData{
		Exchange:       req.Exchange,
		APIKey:         req.APIKey,
		SecretKey:      req.SecretKey,
		Passphrase:     req.Passphrase,
		Symbol:         req.Symbol,
		PriceInterval:  req.PriceInterval,
		OrderQuantity:  req.OrderQuantity,
		MinOrderValue:  req.MinOrderValue,
		BuyWindowSize:  req.BuyWindowSize,
		SellWindowSize: req.SellWindowSize,
		Testnet:        req.Testnet,
		FeeRate:        req.FeeRate,
	}

	// 如果卖单窗口大小未设置，使用买单窗口大小
	if setupData.SellWindowSize <= 0 {
		setupData.SellWindowSize = setupData.BuyWindowSize
	}

	// 从引导数据创建配置
	cfg, err := config.CreateConfigFromSetup(setupData)
	if err != nil {
		logger.Error("❌ 创建配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, SetupInitResponse{
			Success: false,
			Message: "创建配置失败: " + err.Error(),
		})
		return
	}

	// 获取配置文件路径
	configPath := "config.yaml"
	if configManager != nil {
		configPath = configManager.GetConfigPath()
	}

	// 检查配置文件是否已存在且配置完整，防止覆盖
	_, err = os.Stat(configPath)
	if err == nil {
		// 配置文件存在，检查是否配置完整
		existingCfg, loadErr := config.LoadConfig(configPath)
		if loadErr == nil {
			// 检查配置是否完整
			isComplete := existingCfg.App.CurrentExchange != "" &&
				len(existingCfg.Exchanges) > 0 &&
				existingCfg.Exchanges[existingCfg.App.CurrentExchange].APIKey != "" &&
				existingCfg.Exchanges[existingCfg.App.CurrentExchange].SecretKey != "" &&
				len(existingCfg.Trading.Symbols) > 0 &&
				existingCfg.Trading.Symbols[0].Symbol != ""

			if isComplete {
				// 配置已完整，拒绝覆盖
				logger.Warn("⚠️ 尝试覆盖已存在的完整配置，操作被拒绝")
				c.JSON(http.StatusBadRequest, SetupInitResponse{
					Success: false,
					Message: "配置文件已存在且配置完整，无法覆盖。如需修改配置，请使用配置管理页面。",
				})
				return
			}
		}
		// 如果加载失败或配置不完整，允许继续保存
	}

	// 保存配置
	if err := config.SaveConfig(cfg, configPath); err != nil {
		logger.Error("❌ 保存配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, SetupInitResponse{
			Success: false,
			Message: "保存配置失败: " + err.Error(),
		})
		return
	}

	// 更新配置管理器中的配置
	if configManager != nil {
		configManager.mu.Lock()
		configManager.currentConfig = cfg
		configManager.mu.Unlock()
	}

	logger.Info("✅ 配置初始化成功: 交易所=%s, 交易对=%s", req.Exchange, req.Symbol)

	c.JSON(http.StatusOK, SetupInitResponse{
		Success:         true,
		Message:         "配置已保存，请重启系统以应用配置",
		RequiresRestart: true,
	})
}
