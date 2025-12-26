package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 做市商系统配置
type Config struct {
	// 应用配置
	App struct {
		CurrentExchange string `yaml:"current_exchange"` // 当前使用的交易所
	} `yaml:"app"`

	// 多交易所配置
	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`

	Trading struct {
		Symbol                string  `yaml:"symbol"`
		PriceInterval         float64 `yaml:"price_interval"`
		OrderQuantity         float64 `yaml:"order_quantity"`  // 每单购买金额（USDT/USDC）
		MinOrderValue         float64 `yaml:"min_order_value"` // 最小订单价值（USDT），默认6U，小于此值不挂单
		BuyWindowSize         int     `yaml:"buy_window_size"`
		SellWindowSize        int     `yaml:"sell_window_size"` // 卖单窗口大小
		ReconcileInterval     int     `yaml:"reconcile_interval"`
		OrderCleanupThreshold int     `yaml:"order_cleanup_threshold"`      // 订单清理上限（默认100）
		CleanupBatchSize      int     `yaml:"cleanup_batch_size"`           // 清理批次大小（默认10）
		MarginLockDurationSec int     `yaml:"margin_lock_duration_seconds"` // 保证金锁定时间（秒，默认10）
		PositionSafetyCheck   int     `yaml:"position_safety_check"`        // 持仓安全性检查（默认100，最少能向下持有多少仓）
		// 注意：price_decimals 和 quantity_decimals 已废弃，现在从交易所自动获取

		// 动态调整网格参数
		DynamicAdjustment struct {
			Enabled bool `yaml:"enabled"`

			PriceInterval struct {
				Enabled            bool    `yaml:"enabled"`
				Min                float64 `yaml:"min"`                 // 最小价格间隔
				Max                float64 `yaml:"max"`                 // 最大价格间隔
				VolatilityWindow   int     `yaml:"volatility_window"`   // 波动率计算窗口（K线数量）
				VolatilityThreshold float64 `yaml:"volatility_threshold"` // 波动率阈值
				AdjustmentStep     float64 `yaml:"adjustment_step"`     // 每次调整步长
				CheckInterval      int     `yaml:"check_interval"`      // 检查间隔（秒）
			} `yaml:"price_interval"`

			WindowSize struct {
				Enabled              bool    `yaml:"enabled"`
				BuyWindow            struct {
					Min int `yaml:"min"`
					Max int `yaml:"max"`
				} `yaml:"buy_window"`
				SellWindow struct {
					Min int `yaml:"min"`
					Max int `yaml:"max"`
				} `yaml:"sell_window"`
				UtilizationThreshold float64 `yaml:"utilization_threshold"` // 资金利用率阈值
				AdjustmentStep       int     `yaml:"adjustment_step"`       // 每次调整步长
				CheckInterval        int     `yaml:"check_interval"`        // 检查间隔（秒）
			} `yaml:"window_size"`

			OrderQuantity struct {
				Enabled            bool    `yaml:"enabled"`
				Min                float64 `yaml:"min"`
				Max                float64 `yaml:"max"`
				FrequencyThreshold int     `yaml:"frequency_threshold"` // 交易频率阈值（次/分钟）
				AdjustmentStep     float64 `yaml:"adjustment_step"`
			} `yaml:"order_quantity"`
		} `yaml:"dynamic_adjustment"`

		// 智能仓位管理
		SmartPosition struct {
			Enabled bool `yaml:"enabled"`

			TrendDetection struct {
				Enabled     bool   `yaml:"enabled"`
				Window      int    `yaml:"window"`       // 趋势判断窗口（价格数量）
				Method      string `yaml:"method"`       // 方法：ma/ema
				ShortPeriod int    `yaml:"short_period"` // 短期均线周期
				LongPeriod  int    `yaml:"long_period"`  // 长期均线周期
				CheckInterval int  `yaml:"check_interval"` // 检查间隔（秒）
			} `yaml:"trend_detection"`

			WindowAdjustment struct {
				Enabled        bool    `yaml:"enabled"`
				MaxAdjustment  float64 `yaml:"max_adjustment"`  // 最大调整比例
				AdjustmentStep int     `yaml:"adjustment_step"` // 每次调整步长
				MinBuyWindow   int     `yaml:"min_buy_window"`  // 最小买单窗口
				MinSellWindow  int     `yaml:"min_sell_window"` // 最小卖单窗口
			} `yaml:"window_adjustment"`
		} `yaml:"smart_position"`
	} `yaml:"trading"`

	System struct {
		LogLevel     string `yaml:"log_level"`
		CancelOnExit bool   `yaml:"cancel_on_exit"`
	} `yaml:"system"`

	// 主动安全风控配置
	RiskControl struct {
		Enabled           bool     `yaml:"enabled"`            // 是否启用风控，默认true
		MonitorSymbols    []string `yaml:"monitor_symbols"`    // 监控币种，如 ["BTCUSDT", "ETHUSDT"]
		Interval          string   `yaml:"interval"`           // K线周期，如 "1m", "3m", "5m"
		VolumeMultiplier  float64  `yaml:"volume_multiplier"`  // 成交量倍数阈值，默认3.0
		AverageWindow     int      `yaml:"average_window"`     // 移动平均窗口大小，默认20
		RecoveryThreshold int      `yaml:"recovery_threshold"` // 恢复交易所需的正常币种数量，默认3
		MaxLeverage       int      `yaml:"max_leverage"`       // 最大允许杠杆倍数，默认10（设置为0表示不限制）
	} `yaml:"risk_control"`

	// 时间间隔配置（单位：秒，除非特别说明）
	Timing struct {
		// WebSocket相关
		WebSocketReconnectDelay    int `yaml:"websocket_reconnect_delay"`     // WebSocket断线重连等待时间（秒，默认5）
		WebSocketWriteWait         int `yaml:"websocket_write_wait"`          // WebSocket写入等待时间（秒，默认10）
		WebSocketPongWait          int `yaml:"websocket_pong_wait"`           // WebSocket PONG等待时间（秒，默认60）
		WebSocketPingInterval      int `yaml:"websocket_ping_interval"`       // WebSocket PING间隔（秒，默认20）
		ListenKeyKeepAliveInterval int `yaml:"listen_key_keepalive_interval"` // listenKey保活间隔（分钟，默认30）

		// 价格监控相关
		PriceSendInterval int `yaml:"price_send_interval"` // 定期发送价格的间隔（毫秒，默认50）

		// 订单执行相关
		RateLimitRetryDelay  int `yaml:"rate_limit_retry_delay"` // 速率限制重试等待时间（秒，默认1）
		OrderRetryDelay      int `yaml:"order_retry_delay"`      // 其他错误重试等待时间（毫秒，默认500）
		PricePollInterval    int `yaml:"price_poll_interval"`    // 等待获取价格的轮询间隔（毫秒，默认500）
		StatusPrintInterval  int `yaml:"status_print_interval"`  // 定期打印状态的间隔（分钟，默认1）
		OrderCleanupInterval int `yaml:"order_cleanup_interval"` // 订单清理检查间隔（秒，默认60）
	} `yaml:"timing"`

	// 通知配置
	Notifications struct {
		Enabled bool `yaml:"enabled"`

		Telegram struct {
			Enabled  bool   `yaml:"enabled"`
			BotToken string `yaml:"bot_token"`
			ChatID   string `yaml:"chat_id"`
		} `yaml:"telegram"`

		Webhook struct {
			Enabled bool   `yaml:"enabled"`
			URL     string `yaml:"url"`
			Timeout int    `yaml:"timeout"` // 超时时间（秒，默认3）
		} `yaml:"webhook"`

		Email struct {
			Enabled  bool   `yaml:"enabled"`
			Provider string `yaml:"provider"` // smtp/resend/mailgun

			// SMTP 配置
			SMTP struct {
				Host     string `yaml:"host"`
				Port     int    `yaml:"port"`
				Username string `yaml:"username"`
				Password string `yaml:"password"`
			} `yaml:"smtp"`

			// Resend 配置
			Resend struct {
				APIKey string `yaml:"api_key"`
			} `yaml:"resend"`

			// Mailgun 配置
			Mailgun struct {
				APIKey string `yaml:"api_key"`
				Domain string `yaml:"domain"`
			} `yaml:"mailgun"`

			From    string `yaml:"from"`
			To      string `yaml:"to"`
			Subject string `yaml:"subject"`
		} `yaml:"email"`

		// 通知规则：哪些事件需要通知
		Rules struct {
			OrderPlaced   bool `yaml:"order_placed"`
			OrderFilled   bool `yaml:"order_filled"`
			RiskTriggered bool `yaml:"risk_triggered"`
			StopLoss      bool `yaml:"stop_loss"`
			Error         bool `yaml:"error"`
		} `yaml:"rules"`
	} `yaml:"notifications"`

	// 存储配置
	Storage struct {
		Enabled       bool   `yaml:"enabled"`
		Type          string `yaml:"type"`           // sqlite
		Path          string `yaml:"path"`           // 数据库文件路径
		BufferSize    int    `yaml:"buffer_size"`   // 缓冲区大小（默认1000）
		BatchSize     int    `yaml:"batch_size"`     // 批量写入大小（默认100）
		FlushInterval int    `yaml:"flush_interval"` // 刷新间隔（秒，默认5）
	} `yaml:"storage"`

	// Web 服务配置
	Web struct {
		Enabled bool   `yaml:"enabled"`
		Host    string `yaml:"host"`    // 监听地址（默认 0.0.0.0）
		Port    int    `yaml:"port"`    // 监听端口（默认 8080）
		APIKey  string `yaml:"api_key"` // API 密钥（可选，用于认证）
	} `yaml:"web"`

	// 多策略配置
	Strategies struct {
		Enabled bool `yaml:"enabled"`

		// 资金分配配置
		CapitalAllocation struct {
			Mode        string  `yaml:"mode"`         // fixed/dynamic/both
			TotalCapital float64 `yaml:"total_capital"` // 总资金（USDT）

			// 固定分配
			Fixed struct {
				Enabled          bool `yaml:"enabled"`
				RebalanceOnStart bool `yaml:"rebalance_on_start"` // 启动时重新分配
			} `yaml:"fixed"`

			// 动态分配
			DynamicAllocation struct {
				Enabled              bool    `yaml:"enabled"`
				RebalanceInterval    int     `yaml:"rebalance_interval"`     // 重新平衡间隔（秒，默认3600）
				MaxChangePerRebalance float64 `yaml:"max_change_per_rebalance"` // 每次最大调整比例（默认0.05）
				MinWeight            float64 `yaml:"min_weight"`              // 最小权重（默认0.1）
				MaxWeight            float64 `yaml:"max_weight"`              // 最大权重（默认0.7）

				// 评估指标权重
				PerformanceWeights map[string]float64 `yaml:"performance_weights"`
			} `yaml:"dynamic"`
		} `yaml:"capital_allocation"`

		// 策略配置
		Configs map[string]StrategyConfig `yaml:"configs"`
	} `yaml:"strategies"`

	// 回测配置
	Backtest struct {
		Enabled        bool    `yaml:"enabled"`
		StartTime      string  `yaml:"start_time"`      // 开始时间（格式：2006-01-02 15:04:05）
		EndTime        string  `yaml:"end_time"`        // 结束时间
		InitialCapital float64 `yaml:"initial_capital"` // 初始资金
	} `yaml:"backtest"`

	// 监控配置
	Metrics struct {
		Enabled        bool `yaml:"enabled"`
		CollectInterval int `yaml:"collect_interval"` // 收集间隔（秒，默认60）
	} `yaml:"metrics"`

	// 看门狗配置
	Watchdog struct {
		Enabled bool `yaml:"enabled"`

		// 采样配置
		Sampling struct {
			Interval int `yaml:"interval"` // 采样间隔（秒，默认120秒=2分钟）
		} `yaml:"sampling"`

		// 数据保留
		Retention struct {
			DetailDays int `yaml:"detail_days"` // 细粒度数据保留天数（默认7天）
			DailyDays  int `yaml:"daily_days"`  // 每日汇总保留天数（默认365天）
		} `yaml:"retention"`

		// 通知配置
		Notifications struct {
			Enabled bool `yaml:"enabled"`

			// 固定阈值通知
			FixedThreshold struct {
				Enabled    bool    `yaml:"enabled"`
				CPUPercent float64 `yaml:"cpu_percent"` // CPU占用超过此值时通知
				MemoryMB   float64 `yaml:"memory_mb"`   // 内存占用超过此值时通知（可选，0表示不检查）
			} `yaml:"fixed_threshold"`

			// 变化率阈值通知
			RateThreshold struct {
				Enabled          bool    `yaml:"enabled"`
				WindowMinutes    int     `yaml:"window_minutes"`    // 时间窗口（分钟）
				CPUIncrease      float64 `yaml:"cpu_increase"`      // CPU占用在窗口内上涨超过此值时通知
				MemoryIncreaseMB float64 `yaml:"memory_increase_mb"` // 内存占用在窗口内上涨超过此值时通知（可选，0表示不检查）
			} `yaml:"rate_threshold"`

			// 通知冷却时间（避免频繁通知）
			CooldownMinutes int `yaml:"cooldown_minutes"` // 冷却时间（分钟，默认30分钟）
		} `yaml:"notifications"`

		// 每日汇总配置
		Aggregation struct {
			Enabled  bool   `yaml:"enabled"`
			Schedule string `yaml:"schedule"` // 每日汇总执行时间（格式：HH:MM，默认00:00）
		} `yaml:"aggregation"`
	} `yaml:"watchdog"`
}

// StrategyConfig 策略配置
type StrategyConfig struct {
	Enabled bool                   `yaml:"enabled"`
	Weight  float64                `yaml:"weight"` // 资金权重
	Config  map[string]interface{} `yaml:"config"`
}

// ExchangeConfig 交易所配置
type ExchangeConfig struct {
	APIKey     string  `yaml:"api_key"`
	SecretKey  string  `yaml:"secret_key"`
	Passphrase string  `yaml:"passphrase"` // Bitget 需要
	FeeRate    float64 `yaml:"fee_rate"`   // 手续费率（例如 0.0002 表示 0.02%）
	Testnet    bool    `yaml:"testnet"`    // 是否使用测试网（默认 false）
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %v", err)
	}

	return &cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证交易所配置
	if c.App.CurrentExchange == "" {
		return fmt.Errorf("必须指定当前使用的交易所 (app.current_exchange)")
	}

	// 验证多交易所配置
	if len(c.Exchanges) == 0 {
		return fmt.Errorf("未配置任何交易所，请在 exchanges 中添加配置")
	}

	exchangeCfg, exists := c.Exchanges[c.App.CurrentExchange]
	if !exists {
		return fmt.Errorf("交易所 %s 的配置不存在", c.App.CurrentExchange)
	}

	if exchangeCfg.APIKey == "" || exchangeCfg.SecretKey == "" {
		return fmt.Errorf("交易所 %s 的 API 配置不完整", c.App.CurrentExchange)
	}

	// 验证手续费率配置
	if exchangeCfg.FeeRate < 0 {
		return fmt.Errorf("交易所 %s 的手续费率不能为负数", c.App.CurrentExchange)
	}

	if c.Trading.Symbol == "" {
		return fmt.Errorf("交易对不能为空")
	}
	if c.Trading.OrderQuantity <= 0 {
		return fmt.Errorf("订单金额必须大于0")
	}
	if c.Trading.BuyWindowSize <= 0 {
		return fmt.Errorf("买单窗口大小必须大于0")
	}
	if c.Trading.SellWindowSize <= 0 {
		c.Trading.SellWindowSize = c.Trading.BuyWindowSize // 默认与买单窗口相同
	}
	if c.Trading.CleanupBatchSize <= 0 {
		c.Trading.CleanupBatchSize = 10 // 默认10
	}
	// 注意：price_decimals 和 quantity_decimals 已从配置中移除，现在从交易所自动获取
	if c.Trading.MinOrderValue <= 0 {
		c.Trading.MinOrderValue = 20.0 // 默认6U (币安通常最小5U)
	}

	// 设置默认时间间隔
	if c.Timing.WebSocketReconnectDelay <= 0 {
		c.Timing.WebSocketReconnectDelay = 5 // 默认5秒
	}
	if c.Timing.WebSocketWriteWait <= 0 {
		c.Timing.WebSocketWriteWait = 10 // 默认10秒
	}
	if c.Timing.WebSocketPongWait <= 0 {
		c.Timing.WebSocketPongWait = 60 // 默认60秒
	}
	if c.Timing.WebSocketPingInterval <= 0 {
		c.Timing.WebSocketPingInterval = 20 // 默认20秒
	}
	if c.Timing.ListenKeyKeepAliveInterval <= 0 {
		c.Timing.ListenKeyKeepAliveInterval = 30 // 默认30分钟
	}
	if c.Timing.PriceSendInterval <= 0 {
		c.Timing.PriceSendInterval = 50 // 默认50毫秒
	}
	if c.Timing.RateLimitRetryDelay <= 0 {
		c.Timing.RateLimitRetryDelay = 1 // 默认1秒
	}
	if c.Timing.OrderRetryDelay <= 0 {
		c.Timing.OrderRetryDelay = 500 // 默认500毫秒
	}
	if c.Timing.PricePollInterval <= 0 {
		c.Timing.PricePollInterval = 500 // 默认500毫秒
	}
	if c.Timing.StatusPrintInterval <= 0 {
		c.Timing.StatusPrintInterval = 1 // 默认1分钟
	}
	if c.Timing.OrderCleanupInterval <= 0 {
		c.Timing.OrderCleanupInterval = 60 // 默认60秒
	}

	// 验证风控配置并设置默认值
	if c.RiskControl.Interval == "" {
		c.RiskControl.Interval = "1m" // 默认1分钟
	}
	if c.RiskControl.VolumeMultiplier <= 0 {
		c.RiskControl.VolumeMultiplier = 3.0 // 默认3倍
	}
	if c.RiskControl.AverageWindow <= 0 {
		c.RiskControl.AverageWindow = 20 // 默认20根K线
	}
	if len(c.RiskControl.MonitorSymbols) == 0 {
		c.RiskControl.MonitorSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	}

	// 验证恢复阈值配置
	monitorCount := len(c.RiskControl.MonitorSymbols)
	if c.RiskControl.RecoveryThreshold <= 0 {
		c.RiskControl.RecoveryThreshold = 3 // 默认3个币种
	} else if c.RiskControl.RecoveryThreshold < 1 {
		c.RiskControl.RecoveryThreshold = 1 // 最小1个
	} else if c.RiskControl.RecoveryThreshold > monitorCount {
		c.RiskControl.RecoveryThreshold = monitorCount // 最大为监控币种数量
	}

	// 设置通知配置默认值
	if c.Notifications.Webhook.Timeout <= 0 {
		c.Notifications.Webhook.Timeout = 3 // 默认3秒
	}
	if c.Notifications.Email.Provider == "" {
		c.Notifications.Email.Provider = "smtp" // 默认SMTP
	}

	// 设置存储配置默认值
	if c.Storage.Type == "" {
		c.Storage.Type = "sqlite" // 默认SQLite
	}
	if c.Storage.Path == "" {
		c.Storage.Path = "./data/opensqt.db" // 默认路径
	}
	if c.Storage.BufferSize <= 0 {
		c.Storage.BufferSize = 1000 // 默认1000
	}
	if c.Storage.BatchSize <= 0 {
		c.Storage.BatchSize = 100 // 默认100
	}
	if c.Storage.FlushInterval <= 0 {
		c.Storage.FlushInterval = 5 // 默认5秒
	}

	// 设置 Web 服务配置默认值
	if c.Web.Host == "" {
		c.Web.Host = "0.0.0.0" // 默认监听所有地址
	}
	if c.Web.Port <= 0 {
		c.Web.Port = 28888 // 默认端口（使用10000以上端口，避免常见端口冲突）
	}

	// 设置监控配置默认值
	if c.Metrics.CollectInterval <= 0 {
		c.Metrics.CollectInterval = 60 // 默认60秒
	}

	// 设置策略配置默认值
	if c.Strategies.CapitalAllocation.Mode == "" {
		c.Strategies.CapitalAllocation.Mode = "fixed" // 默认固定分配
	}
	if c.Strategies.CapitalAllocation.TotalCapital <= 0 {
		c.Strategies.CapitalAllocation.TotalCapital = 5000 // 默认5000 USDT
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval = 3600 // 默认1小时
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance = 0.05 // 默认5%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MinWeight <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MinWeight = 0.1 // 默认10%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight = 0.7 // 默认70%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights == nil {
		c.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights = map[string]float64{
			"total_pnl":   0.4,
			"sharpe_ratio": 0.3,
			"win_rate":    0.2,
			"max_drawdown": 0.1,
		}
	}

	return nil
}
