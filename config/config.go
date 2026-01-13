package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GridRiskControl 网格策略风控配置
type GridRiskControl struct {
	Enabled                 bool    `yaml:"enabled" json:"enabled"`
	MaxGridLayers           int     `yaml:"max_grid_layers" json:"max_grid_layers"`                       // 最大允许买入层数
	StopLossRatio           float64 `yaml:"stop_loss_ratio" json:"stop_loss_ratio"`                       // 单币种最大浮亏比例（如 0.1 表示 10%）
	TakeProfitTriggerRatio  float64 `yaml:"take_profit_trigger_ratio" json:"take_profit_trigger_ratio"`   // 盈利达到此比例后开启回撤止盈（如 0.08 表示 8%）
	TrailingTakeProfitRatio float64 `yaml:"trailing_take_profit_ratio" json:"trailing_take_profit_ratio"` // 盈利回撤比例（如 0.03 表示回撤 3% 止盈）
	TrendFilterEnabled      bool    `yaml:"trend_filter_enabled" json:"trend_filter_enabled"`             // 是否开启趋势过滤
}

// Config 做市商系统配置
type Config struct {
	// 应用配置
	App struct {
		CurrentExchange string `yaml:"current_exchange"` // 当前使用的交易所
	} `yaml:"app"`

	// 多交易所配置
	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`

	Trading struct {
		// 兼容旧配置：单交易对字段（若启用多交易对，将自动转换为 Symbols 列表）
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
		// 多交易对配置
		Symbols []SymbolConfig `yaml:"symbols"`
		// 注意：price_decimals 和 quantity_decimals 已废弃，现在从交易所自动获取

		// 动态调整网格参数
		DynamicAdjustment struct {
			Enabled bool `yaml:"enabled"`

			PriceInterval struct {
				Enabled             bool    `yaml:"enabled"`
				Min                 float64 `yaml:"min"`                  // 最小价格间隔
				Max                 float64 `yaml:"max"`                  // 最大价格间隔
				VolatilityWindow    int     `yaml:"volatility_window"`    // 波动率计算窗口（K线数量）
				VolatilityThreshold float64 `yaml:"volatility_threshold"` // 波动率阈值
				AdjustmentStep      float64 `yaml:"adjustment_step"`      // 每次调整步长
				CheckInterval       int     `yaml:"check_interval"`       // 检查间隔（秒）
			} `yaml:"price_interval"`

			WindowSize struct {
				Enabled   bool `yaml:"enabled"`
				BuyWindow struct {
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
				Enabled       bool   `yaml:"enabled"`
				Window        int    `yaml:"window"`         // 趋势判断窗口（价格数量）
				Method        string `yaml:"method"`         // 方法：ma/ema
				ShortPeriod   int    `yaml:"short_period"`   // 短期均线周期
				LongPeriod    int    `yaml:"long_period"`    // 长期均线周期
				CheckInterval int    `yaml:"check_interval"` // 检查间隔（秒）
			} `yaml:"trend_detection"`

			WindowAdjustment struct {
				Enabled        bool    `yaml:"enabled"`
				MaxAdjustment  float64 `yaml:"max_adjustment"`  // 最大调整比例
				AdjustmentStep int     `yaml:"adjustment_step"` // 每次调整步长
				MinBuyWindow   int     `yaml:"min_buy_window"`  // 最小买单窗口
				MinSellWindow  int     `yaml:"min_sell_window"` // 最小卖单窗口
			} `yaml:"window_adjustment"`
		} `yaml:"smart_position"`

		GridRiskControl GridRiskControl `yaml:"grid_risk_control"`
	} `yaml:"trading"`

	System struct {
		LogLevel             string `yaml:"log_level"`
		Timezone             string `yaml:"timezone"`     // 时区，如 "Asia/Shanghai"
		LogLanguage          string `yaml:"log_language"` // 日志语言，如 "zh-CN" 或 "en-US"
		CancelOnExit         bool   `yaml:"cancel_on_exit"`
		ClosePositionsOnExit bool   `yaml:"close_positions_on_exit"` // 退出时是否平仓（默认false）
		LogRetentionDays     int    `yaml:"log_retention_days"`      // 日志保留天数（默认30天，0表示不清理）
	} `yaml:"system"`

	// 实例配置（多实例部署）
	Instance struct {
		ID    string `yaml:"id"`    // 实例唯一标识，默认为空（单实例模式）
		Index int    `yaml:"index"` // 实例索引，用于交易对分配，默认0
		Total int    `yaml:"total"` // 总实例数，默认1
	} `yaml:"instance"`

	// 数据库配置（支持 SQLite、PostgreSQL、MySQL）
	Database struct {
		Type            string `yaml:"type"`              // 数据库类型: sqlite, postgres, mysql，默认 sqlite
		DSN             string `yaml:"dsn"`               // 数据源名称，默认 ./data/quantmesh.db
		MaxOpenConns    int    `yaml:"max_open_conns"`    // 最大打开连接数，默认100
		MaxIdleConns    int    `yaml:"max_idle_conns"`    // 最大空闲连接数，默认10
		ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // 连接最大生命周期（秒），默认3600
		LogLevel        string `yaml:"log_level"`         // 日志级别: silent, error, warn, info，默认 error
	} `yaml:"database"`

	// 分布式锁配置（多实例部署）
	DistributedLock struct {
		Enabled    bool   `yaml:"enabled"`     // 是否启用分布式锁，默认false（单实例模式）
		Type       string `yaml:"type"`        // 锁类型: redis, etcd, database，默认 redis
		Prefix     string `yaml:"prefix"`      // 锁键前缀，默认 "quantmesh:lock:"
		DefaultTTL int    `yaml:"default_ttl"` // 默认锁过期时间（秒），默认5

		Redis struct {
			Addr     string `yaml:"addr"`      // Redis 地址，默认 localhost:6379
			Password string `yaml:"password"`  // Redis 密码，默认为空
			DB       int    `yaml:"db"`        // Redis 数据库，默认0
			PoolSize int    `yaml:"pool_size"` // 连接池大小，默认10
		} `yaml:"redis"`
	} `yaml:"distributed_lock"`

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

		// 飞书（Feishu/Lark）配置
		Feishu struct {
			Enabled bool   `yaml:"enabled"`
			Webhook string `yaml:"webhook"` // 飞书机器人 Webhook URL
		} `yaml:"feishu"`

		// 钉钉（DingTalk）配置
		DingTalk struct {
			Enabled bool   `yaml:"enabled"`
			Webhook string `yaml:"webhook"` // 钉钉机器人 Webhook URL
			Secret  string `yaml:"secret"`  // 钉钉机器人签名密钥（可选）
		} `yaml:"dingtalk"`

		// 企业微信（WeChat Work）配置
		WeChatWork struct {
			Enabled bool   `yaml:"enabled"`
			Webhook string `yaml:"webhook"` // 企业微信机器人 Webhook URL
		} `yaml:"wechat_work"`

		// Slack 配置
		Slack struct {
			Enabled bool   `yaml:"enabled"`
			Webhook string `yaml:"webhook"` // Slack Incoming Webhook URL
		} `yaml:"slack"`

		// 通知规则：哪些事件需要通知
		Rules struct {
			OrderPlaced        bool `yaml:"order_placed"`
			OrderFilled        bool `yaml:"order_filled"`
			RiskTriggered      bool `yaml:"risk_triggered"`
			StopLoss           bool `yaml:"stop_loss"`
			Error              bool `yaml:"error"`
			MarginInsufficient bool `yaml:"margin_insufficient"` // 保证金不足
			AllocationExceeded bool `yaml:"allocation_exceeded"` // 超出资金分配限制
		} `yaml:"rules"`
	} `yaml:"notifications"`

	// 存储配置
	Storage struct {
		Enabled       bool   `yaml:"enabled"`
		Type          string `yaml:"type"`           // sqlite
		Path          string `yaml:"path"`           // 数据库文件路径
		BufferSize    int    `yaml:"buffer_size"`    // 缓冲区大小（默认1000）
		BatchSize     int    `yaml:"batch_size"`     // 批量写入大小（默认100）
		FlushInterval int    `yaml:"flush_interval"` // 刷新间隔（秒，默认5）
	} `yaml:"storage"`

	// Web 服务配置
	Web struct {
		Enabled bool   `yaml:"enabled"`
		Host    string `yaml:"host"`    // 监听地址（默认 0.0.0.0）
		Port    int    `yaml:"port"`    // 监听端口（默认 8080）
		APIKey  string `yaml:"api_key"` // API 密钥（可选，用于认证）
		
		// pprof 性能分析配置
		Pprof struct {
			Enabled     bool     `yaml:"enabled"`      // 是否启用 pprof，默认 false（生产环境建议禁用）
			RequireAuth bool     `yaml:"require_auth"` // 是否需要认证，默认 true
			AllowedIPs  []string `yaml:"allowed_ips"` // IP 白名单（可选，为空则允许所有 IP）
		} `yaml:"pprof"`
	} `yaml:"web"`

	// 插件配置
	Plugins struct {
		Enabled   bool                              `yaml:"enabled"`   // 是否启用插件系统，默认false
		Directory string                            `yaml:"directory"` // 插件目录，默认 ./plugins
		Licenses  map[string]string                 `yaml:"licenses"`  // 插件 License Keys
		Config    map[string]map[string]interface{} `yaml:"config"`    // 插件配置
	} `yaml:"plugins"`

	// 价差监控配置
	BasisMonitor struct {
		Enabled         bool     `yaml:"enabled"`          // 是否启用价差监控，默认false
		IntervalMinutes int      `yaml:"interval_minutes"` // 检查间隔（分钟），默认1
		Symbols         []string `yaml:"symbols"`          // 监控的交易对列表
	} `yaml:"basis_monitor"`

	// 事件中心配置
	EventCenter struct {
		Enabled                  bool     `yaml:"enabled"`                     // 是否启用事件中心，默认true
		PriceVolatilityThreshold float64  `yaml:"price_volatility_threshold"`  // 价格波动阈值（百分比），默认5.0
		MonitoredSymbols         []string `yaml:"monitored_symbols"`           // 监控价格波动的交易对
		
		// 事件保留策略
		Retention struct {
			CriticalDays int `yaml:"critical_days"` // Critical 事件保留天数，默认365
			WarningDays  int `yaml:"warning_days"`  // Warning 事件保留天数，默认90
			InfoDays     int `yaml:"info_days"`     // Info 事件保留天数，默认30
			
			CriticalMaxCount int `yaml:"critical_max_count"` // Critical 事件最大保留数量，默认1000000
			WarningMaxCount  int `yaml:"warning_max_count"`  // Warning 事件最大保留数量，默认500000
			InfoMaxCount     int `yaml:"info_max_count"`     // Info 事件最大保留数量，默认300000
		} `yaml:"retention"`
		
		CleanupInterval int `yaml:"cleanup_interval"` // 清理间隔（小时），默认24
	} `yaml:"event_center"`

	// 多策略配置
	Strategies struct {
		Enabled bool `yaml:"enabled"`

		// 资金分配配置
		CapitalAllocation struct {
			Mode         string  `yaml:"mode"`          // fixed/dynamic/both
			TotalCapital float64 `yaml:"total_capital"` // 总资金（USDT）

			// 固定分配
			Fixed struct {
				Enabled          bool `yaml:"enabled"`
				RebalanceOnStart bool `yaml:"rebalance_on_start"` // 启动时重新分配
			} `yaml:"fixed"`

			// 动态分配
			DynamicAllocation struct {
				Enabled               bool    `yaml:"enabled"`
				RebalanceInterval     int     `yaml:"rebalance_interval"`       // 重新平衡间隔（秒，默认3600）
				MaxChangePerRebalance float64 `yaml:"max_change_per_rebalance"` // 每次最大调整比例（默认0.05）
				MinWeight             float64 `yaml:"min_weight"`               // 最小权重（默认0.1）
				MaxWeight             float64 `yaml:"max_weight"`               // 最大权重（默认0.7）

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

	// 仓位资金分配管理
	PositionAllocation struct {
		Enabled     bool                `yaml:"enabled"`
		Allocations []SymbolAllocation  `yaml:"allocations"`
	} `yaml:"position_allocation"`

	// 监控配置
	Metrics struct {
		Enabled         bool `yaml:"enabled"`
		CollectInterval int  `yaml:"collect_interval"` // 收集间隔（秒，默认60）
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
				WindowMinutes    int     `yaml:"window_minutes"`     // 时间窗口（分钟）
				CPUIncrease      float64 `yaml:"cpu_increase"`       // CPU占用在窗口内上涨超过此值时通知
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

	// AI配置
	AI struct {
		Enabled      bool   `yaml:"enabled"`
		Provider     string `yaml:"provider"` // gemini, openai
		APIKey       string `yaml:"api_key"`
		GeminiAPIKey string `yaml:"gemini_api_key"` // Gemini API 密钥（优先使用，如果为空则使用 api_key）
		BaseURL      string `yaml:"base_url"`       // 可选，用于自定义API端点
		
		// 访问模式配置
		AccessMode string `yaml:"access_mode"` // native: 直接访问 Google Gemini API, proxy: 通过中转服务访问
		
		// 代理服务配置（当 access_mode 为 proxy 时使用）
		Proxy struct {
			BaseURL  string `yaml:"base_url"`  // 代理服务地址，默认 https://gemini.facev.app
			Username string `yaml:"username"`   // Basic Auth 用户名，默认 admin123
			Password string `yaml:"password"`   // Basic Auth 密码，默认 admin123
		} `yaml:"proxy"`

		// 各模块开关
		Modules struct {
			MarketAnalysis struct {
				Enabled        bool `yaml:"enabled"`
				UpdateInterval int  `yaml:"update_interval"` // 秒
			} `yaml:"market_analysis"`

			ParameterOptimization struct {
				Enabled              bool `yaml:"enabled"`
				OptimizationInterval int  `yaml:"optimization_interval"` // 秒
				AutoApply            bool `yaml:"auto_apply"`            // 是否自动应用优化结果
			} `yaml:"parameter_optimization"`

			RiskAnalysis struct {
				Enabled          bool `yaml:"enabled"`
				AnalysisInterval int  `yaml:"analysis_interval"` // 秒
			} `yaml:"risk_analysis"`

			SentimentAnalysis struct {
				Enabled          bool `yaml:"enabled"`
				AnalysisInterval int  `yaml:"analysis_interval"` // 秒
				DataSources      struct {
					News struct {
						Enabled       bool     `yaml:"enabled"`
						RSSFeeds      []string `yaml:"rss_feeds"`
						FetchInterval int      `yaml:"fetch_interval"` // 秒
					} `yaml:"news"`

					FearGreedIndex struct {
						Enabled       bool   `yaml:"enabled"`
						APIURL        string `yaml:"api_url"`
						FetchInterval int    `yaml:"fetch_interval"` // 秒
					} `yaml:"fear_greed_index"`

					SocialMedia struct {
						Enabled    bool     `yaml:"enabled"`
						Subreddits []string `yaml:"subreddits"` // Reddit子版块列表
						PostLimit  int      `yaml:"post_limit"` // 每个子版块获取的帖子数量
					} `yaml:"social_media"`
				} `yaml:"data_sources"`
			} `yaml:"sentiment_analysis"`

			StrategyGeneration struct {
				Enabled bool `yaml:"enabled"` // 实验性功能
			} `yaml:"strategy_generation"`

			PolymarketSignal struct {
				Enabled          bool   `yaml:"enabled"`
				AnalysisInterval int    `yaml:"analysis_interval"` // 秒
				APIURL           string `yaml:"api_url"`           // Polymarket API地址
				Markets          struct {
					Keywords        []string `yaml:"keywords"`           // 关注的市场关键词
					MinLiquidity    float64  `yaml:"min_liquidity"`      // 最小流动性（USDC）
					MinVolume24h    float64  `yaml:"min_volume_24h"`     // 最小24小时交易量（USDC）
					MinDaysToExpiry int      `yaml:"min_days_to_expiry"` // 最小到期天数
					MaxDaysToExpiry int      `yaml:"max_days_to_expiry"` // 最大到期天数
				} `yaml:"markets"`
				SignalGeneration struct {
					BuyThreshold      float64 `yaml:"buy_threshold"`       // 买入信号阈值（概率>此值）
					SellThreshold     float64 `yaml:"sell_threshold"`      // 卖出信号阈值（概率<此值）
					MinSignalStrength float64 `yaml:"min_signal_strength"` // 最小信号强度
					MinConfidence     float64 `yaml:"min_confidence"`      // 最小置信度
				} `yaml:"signal_generation"`
			} `yaml:"polymarket_signal"`
		} `yaml:"modules"`

		// 决策模式
		DecisionMode string `yaml:"decision_mode"` // advisor, executor, hybrid

		// 执行模式规则
		ExecutionRules struct {
			HighRiskThreshold   float64 `yaml:"high_risk_threshold"`  // 高风险场景：仅建议
			LowRiskThreshold    float64 `yaml:"low_risk_threshold"`   // 低风险场景：可直接执行
			RequireConfirmation bool    `yaml:"require_confirmation"` // 需要人工确认的场景
		} `yaml:"execution_rules"`
	} `yaml:"ai"`
}

// WithdrawalPolicy 提现策略（利润保护）
type WithdrawalPolicy struct {
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Threshold float64 `yaml:"threshold" json:"threshold"` // 触发提现的利润比例 (如 0.1 表示 10%)

	// ===== 划转模式 =====
	Mode string `yaml:"mode" json:"mode"` // threshold(阈值触发), fixed(固定金额), tiered(阶梯), scheduled(定时)

	// ===== 固定金额模式 =====
	FixedAmount float64 `yaml:"fixed_amount" json:"fixed_amount"` // 每次划转的固定金额 (USDT)

	// ===== 阶梯划转模式 =====
	TieredRules []TieredWithdrawRule `yaml:"tiered_rules" json:"tiered_rules"` // 阶梯划转规则

	// ===== 划转比例 =====
	WithdrawRatio float64 `yaml:"withdraw_ratio" json:"withdraw_ratio"` // 划转比例 (0-1)，如 0.5 表示划转利润的 50%

	// ===== 本金保护 =====
	PrincipalProtection PrincipalProtection `yaml:"principal_protection" json:"principal_protection"`

	// ===== 定时划转 =====
	Schedule WithdrawSchedule `yaml:"schedule" json:"schedule"`

	// ===== 复利设置 =====
	CompoundRatio float64 `yaml:"compound_ratio" json:"compound_ratio"` // 复利比例 (0-1)，剩余部分划转

	// ===== 目标账户 =====
	TargetWallet string `yaml:"target_wallet" json:"target_wallet"` // spot(现货), funding(资金账户), external(外部地址)
}

// TieredWithdrawRule 阶梯划转规则
type TieredWithdrawRule struct {
	ProfitThreshold float64 `yaml:"profit_threshold" json:"profit_threshold"` // 利润阈值 (如 0.1 表示 10%)
	WithdrawRatio   float64 `yaml:"withdraw_ratio" json:"withdraw_ratio"`     // 达到该阈值时划转的比例
}

// PrincipalProtection 本金保护设置
type PrincipalProtection struct {
	Enabled              bool    `yaml:"enabled" json:"enabled"`
	BreakevenProtection  bool    `yaml:"breakeven_protection" json:"breakeven_protection"`     // 回本即保护（设置保本止损）
	WithdrawPrincipal    bool    `yaml:"withdraw_principal" json:"withdraw_principal"`         // 盈利足够时划转本金
	PrincipalWithdrawAt  float64 `yaml:"principal_withdraw_at" json:"principal_withdraw_at"`   // 利润达到多少时划转本金 (如 1.0 表示利润=本金时)
	MaxLossRatio         float64 `yaml:"max_loss_ratio" json:"max_loss_ratio"`                 // 最大亏损比例 (如 0.2 表示最多亏损本金的 20%)
}

// WithdrawSchedule 定时划转设置
type WithdrawSchedule struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Frequency string `yaml:"frequency" json:"frequency"` // daily, weekly, monthly
	DayOfWeek int    `yaml:"day_of_week" json:"day_of_week"` // 周几 (1-7, 仅 weekly 模式)
	DayOfMonth int   `yaml:"day_of_month" json:"day_of_month"` // 每月几号 (1-31, 仅 monthly 模式)
	TimeOfDay  string `yaml:"time_of_day" json:"time_of_day"` // 时间 (如 "23:00")
}

// StrategyInstance 币种下的策略实例
type StrategyInstance struct {
	Type   string                 `yaml:"type" json:"type"`     // grid, dca, etc.
	Weight float64                `yaml:"weight" json:"weight"` // 资金占比 (0-1)
	Config map[string]interface{} `yaml:"config" json:"config"` // 策略专属配置
}

// SymbolConfig 单个交易对配置（可指定所属交易所及交易参数）
type SymbolConfig struct {
	Exchange              string           `yaml:"exchange" json:"exchange"`                                 // 所属交易所，默认为 app.current_exchange
	Symbol                string           `yaml:"symbol" json:"symbol"`                                     // 交易对，如 BTCUSDT
	TotalAllocatedCapital float64          `yaml:"total_allocated_capital" json:"total_allocated_capital"`   // 该币种分配的总资金
	Strategies            []StrategyInstance `yaml:"strategies" json:"strategies"`                       // 运行在该币种上的策略列表
	WithdrawalPolicy      WithdrawalPolicy   `yaml:"withdrawal_policy" json:"withdrawal_policy"`             // 提现策略
	PriceInterval         float64          `yaml:"price_interval" json:"price_interval"`                     // 价格间隔
	OrderQuantity         float64          `yaml:"order_quantity" json:"order_quantity"`                     // 每单金额（USDT/USDC）
	MinOrderValue         float64          `yaml:"min_order_value" json:"min_order_value"`                   // 最小订单价值
	BuyWindowSize         int              `yaml:"buy_window_size" json:"buy_window_size"`                   // 买单窗口
	SellWindowSize        int              `yaml:"sell_window_size" json:"sell_window_size"`                 // 卖单窗口
	ReconcileInterval     int              `yaml:"reconcile_interval" json:"reconcile_interval"`             // 对账间隔（秒）
	OrderCleanupThreshold int              `yaml:"order_cleanup_threshold" json:"order_cleanup_threshold"`   // 订单清理上限
	CleanupBatchSize      int              `yaml:"cleanup_batch_size" json:"cleanup_batch_size"`             // 清理批次大小
	MarginLockDurationSec int              `yaml:"margin_lock_duration_seconds" json:"margin_lock_duration"` // 保证金锁定时间（秒）
	PositionSafetyCheck   int              `yaml:"position_safety_check" json:"position_safety_check"`       // 持仓安全性检查
	GridRiskControl       GridRiskControl  `yaml:"grid_risk_control" json:"grid_risk_control"`               // 网格策略风控
}

// StrategyConfig 策略配置
type StrategyConfig struct {
	Enabled bool                   `yaml:"enabled" json:"enabled"`
	Type    string                 `yaml:"type" json:"type"`     // 策略类型 (grid, dca, martingale, dca_enhanced, combo)
	Weight  float64                `yaml:"weight" json:"weight"` // 资金权重
	Config  map[string]interface{} `yaml:"config" json:"config"`
}

// ExchangeConfig 交易所配置
type ExchangeConfig struct {
	APIKey     string  `yaml:"api_key" json:"api_key"`
	SecretKey  string  `yaml:"secret_key" json:"secret_key"`
	Passphrase string  `yaml:"passphrase" json:"passphrase"` // Bitget 需要
	FeeRate    float64 `yaml:"fee_rate" json:"fee_rate"`     // 手续费率（例如 0.0002 表示 0.02%）
	Testnet    bool    `yaml:"testnet" json:"testnet"`       // 是否使用测试网（默认 false）
}

// SymbolAllocation 单个币种的资金分配配置
type SymbolAllocation struct {
	Exchange      string  `yaml:"exchange"`
	Symbol        string  `yaml:"symbol"`
	MaxAmountUSDT float64 `yaml:"max_amount_usdt"` // 固定金额限制
	MaxPercentage float64 `yaml:"max_percentage"`  // 账户余额百分比限制
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

// LoadConfigFromBytes 从字节数组加载配置（用于测试）
func LoadConfigFromBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %v", err)
	}

	return &cfg, nil
}

// SaveConfig 保存配置到文件
func SaveConfig(cfg *Config, configPath string) error {
	// 验证配置
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("配置验证失败: %v", err)
	}

	// 序列化为YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	// 写入文件
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// SaveConfigWithoutValidation 保存配置到文件（不验证，用于保存最小化配置）
func SaveConfigWithoutValidation(cfg *Config, configPath string) error {
	// 序列化为YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	// 写入文件
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// CreateMinimalConfig 创建最小化配置（仅用于启动 Web 服务）
func CreateMinimalConfig() *Config {
	cfg := &Config{}

	// 应用配置
	cfg.App.CurrentExchange = ""

	// 交易所配置（空）
	cfg.Exchanges = make(map[string]ExchangeConfig)

	// 交易配置（空）
	cfg.Trading.Symbol = ""
	cfg.Trading.PriceInterval = 0
	cfg.Trading.OrderQuantity = 0
	cfg.Trading.MinOrderValue = 20
	cfg.Trading.BuyWindowSize = 0
	cfg.Trading.SellWindowSize = 0
	cfg.Trading.ReconcileInterval = 60
	cfg.Trading.OrderCleanupThreshold = 50
	cfg.Trading.CleanupBatchSize = 10
	cfg.Trading.MarginLockDurationSec = 10
	cfg.Trading.PositionSafetyCheck = 100

	// 系统配置
	cfg.System.LogLevel = "INFO"
	cfg.System.Timezone = "Asia/Shanghai"
	cfg.System.CancelOnExit = true
	cfg.System.ClosePositionsOnExit = false
	cfg.System.LogRetentionDays = 30 // 默认保留30天

	// Web 服务配置（启用）
	cfg.Web.Enabled = true
	cfg.Web.Host = "0.0.0.0"
	cfg.Web.Port = 28888
	cfg.Web.APIKey = ""

	// 其他配置使用默认值
	cfg.RiskControl.Enabled = true
	cfg.RiskControl.Interval = "1m"
	cfg.RiskControl.VolumeMultiplier = 3.0
	cfg.RiskControl.AverageWindow = 20
	cfg.RiskControl.RecoveryThreshold = 3
	cfg.RiskControl.MaxLeverage = 10
	cfg.RiskControl.MonitorSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}

	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = "./data/quantmesh.db"
	cfg.Storage.BufferSize = 1000
	cfg.Storage.BatchSize = 100
	cfg.Storage.FlushInterval = 5

	cfg.Notifications.Enabled = false
	cfg.Notifications.Webhook.Timeout = 3
	cfg.Notifications.Email.Provider = "smtp"

	cfg.Metrics.Enabled = true
	cfg.Metrics.CollectInterval = 60

	cfg.Watchdog.Enabled = true
	cfg.Watchdog.Sampling.Interval = 60
	cfg.Watchdog.Retention.DetailDays = 7
	cfg.Watchdog.Retention.DailyDays = 90
	cfg.Watchdog.Notifications.Enabled = true
	cfg.Watchdog.Notifications.FixedThreshold.Enabled = true
	cfg.Watchdog.Notifications.FixedThreshold.CPUPercent = 80
	cfg.Watchdog.Notifications.FixedThreshold.MemoryMB = 1024
	cfg.Watchdog.Notifications.RateThreshold.Enabled = true
	cfg.Watchdog.Notifications.RateThreshold.WindowMinutes = 5
	cfg.Watchdog.Notifications.RateThreshold.CPUIncrease = 30
	cfg.Watchdog.Notifications.RateThreshold.MemoryIncreaseMB = 200
	cfg.Watchdog.Notifications.CooldownMinutes = 30
	cfg.Watchdog.Aggregation.Enabled = true
	cfg.Watchdog.Aggregation.Schedule = "00:00"

	cfg.AI.Enabled = false
	cfg.AI.Provider = "gemini"
	cfg.AI.AccessMode = "native" // 默认使用原生方式
	cfg.AI.Proxy.BaseURL = "https://gemini.facev.app"
	cfg.AI.Proxy.Username = "admin123"
	cfg.AI.Proxy.Password = "admin123"
	cfg.AI.DecisionMode = "hybrid"
	cfg.AI.ExecutionRules.HighRiskThreshold = 0.8
	cfg.AI.ExecutionRules.LowRiskThreshold = 0.3
	cfg.AI.ExecutionRules.RequireConfirmation = true

	cfg.Strategies.Enabled = false
	cfg.Strategies.CapitalAllocation.Mode = "fixed"
	cfg.Strategies.CapitalAllocation.TotalCapital = 5000

	// 时间间隔配置
	cfg.Timing.WebSocketReconnectDelay = 5
	cfg.Timing.WebSocketWriteWait = 10
	cfg.Timing.WebSocketPongWait = 60
	cfg.Timing.WebSocketPingInterval = 20
	cfg.Timing.ListenKeyKeepAliveInterval = 30
	cfg.Timing.PriceSendInterval = 50
	cfg.Timing.RateLimitRetryDelay = 1
	cfg.Timing.OrderRetryDelay = 500
	cfg.Timing.PricePollInterval = 500
	cfg.Timing.StatusPrintInterval = 1
	cfg.Timing.OrderCleanupInterval = 60

	return cfg
}

// SetupData 引导配置数据
type SetupData struct {
	Exchange       string  `json:"exchange"`
	APIKey         string  `json:"api_key"`
	SecretKey      string  `json:"secret_key"`
	Passphrase     string  `json:"passphrase,omitempty"`
	Symbol         string  `json:"symbol"`
	PriceInterval  float64 `json:"price_interval"`
	OrderQuantity  float64 `json:"order_quantity"`
	MinOrderValue  float64 `json:"min_order_value,omitempty"`
	BuyWindowSize  int     `json:"buy_window_size"`
	SellWindowSize int     `json:"sell_window_size"`
	Testnet        bool    `json:"testnet,omitempty"`
	FeeRate        float64 `json:"fee_rate,omitempty"`
}

// CreateConfigFromSetup 从引导数据创建完整配置
func CreateConfigFromSetup(setup *SetupData) (*Config, error) {
	// 创建最小化配置作为基础
	cfg := CreateMinimalConfig()

	// 设置交易所
	cfg.App.CurrentExchange = setup.Exchange

	// 设置交易所配置
	exchangeCfg := ExchangeConfig{
		APIKey:     setup.APIKey,
		SecretKey:  setup.SecretKey,
		Passphrase: setup.Passphrase,
		Testnet:    setup.Testnet,
		FeeRate:    setup.FeeRate,
	}

	// 如果手续费率未设置，使用默认值
	if exchangeCfg.FeeRate <= 0 {
		exchangeCfg.FeeRate = 0.0002
	}

	cfg.Exchanges[setup.Exchange] = exchangeCfg

	// 设置交易配置
	cfg.Trading.Symbol = setup.Symbol
	cfg.Trading.PriceInterval = setup.PriceInterval
	cfg.Trading.OrderQuantity = setup.OrderQuantity

	if setup.MinOrderValue > 0 {
		cfg.Trading.MinOrderValue = setup.MinOrderValue
	} else {
		cfg.Trading.MinOrderValue = 20
	}

	cfg.Trading.BuyWindowSize = setup.BuyWindowSize
	cfg.Trading.SellWindowSize = setup.SellWindowSize

	// 设置默认值
	if cfg.Trading.SellWindowSize <= 0 {
		cfg.Trading.SellWindowSize = cfg.Trading.BuyWindowSize
	}

	// 创建交易对配置
	cfg.Trading.Symbols = []SymbolConfig{
		{
			Exchange:              setup.Exchange,
			Symbol:                setup.Symbol,
			PriceInterval:         setup.PriceInterval,
			OrderQuantity:         setup.OrderQuantity,
			MinOrderValue:         cfg.Trading.MinOrderValue,
			BuyWindowSize:         setup.BuyWindowSize,
			SellWindowSize:        cfg.Trading.SellWindowSize,
			ReconcileInterval:     60,
			OrderCleanupThreshold: 50,
			CleanupBatchSize:      10,
			MarginLockDurationSec: 10,
			PositionSafetyCheck:   100,
		},
	}

	return cfg, nil
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

	// ==== 多交易对配置校验（兼容旧配置）====
	normalizeSymbol := func(sc SymbolConfig) (SymbolConfig, error) {
		// 交易所
		if sc.Exchange == "" {
			sc.Exchange = c.App.CurrentExchange
		}
		exCfg, ok := c.Exchanges[sc.Exchange]
		if !ok {
			return sc, fmt.Errorf("交易所 %s 的配置不存在", sc.Exchange)
		}
		if exCfg.APIKey == "" || exCfg.SecretKey == "" {
			return sc, fmt.Errorf("交易所 %s 的 API 配置不完整", sc.Exchange)
		}
		if exCfg.FeeRate < 0 {
			return sc, fmt.Errorf("交易所 %s 的手续费率不能为负数", sc.Exchange)
		}

		// 交易对
		if sc.Symbol == "" {
			return sc, fmt.Errorf("交易对不能为空")
		}

		// 数值默认
		if sc.PriceInterval <= 0 {
			sc.PriceInterval = c.Trading.PriceInterval
		}
		if sc.PriceInterval <= 0 {
			return sc, fmt.Errorf("交易对 %s 的价格间隔必须大于0", sc.Symbol)
		}

		if sc.OrderQuantity <= 0 {
			sc.OrderQuantity = c.Trading.OrderQuantity
		}
		if sc.OrderQuantity <= 0 {
			return sc, fmt.Errorf("交易对 %s 的订单金额必须大于0", sc.Symbol)
		}

		if sc.MinOrderValue <= 0 {
			if c.Trading.MinOrderValue > 0 {
				sc.MinOrderValue = c.Trading.MinOrderValue
			} else {
				sc.MinOrderValue = 20.0
			}
		}

		if sc.BuyWindowSize <= 0 {
			sc.BuyWindowSize = c.Trading.BuyWindowSize
		}
		if sc.BuyWindowSize <= 0 {
			return sc, fmt.Errorf("交易对 %s 的买单窗口大小必须大于0", sc.Symbol)
		}

		if sc.SellWindowSize <= 0 {
			if c.Trading.SellWindowSize > 0 {
				sc.SellWindowSize = c.Trading.SellWindowSize
			} else {
				sc.SellWindowSize = sc.BuyWindowSize
			}
		}

		if sc.ReconcileInterval <= 0 {
			if c.Trading.ReconcileInterval > 0 {
				sc.ReconcileInterval = c.Trading.ReconcileInterval
			} else {
				sc.ReconcileInterval = 60
			}
		}

		if sc.OrderCleanupThreshold <= 0 {
			if c.Trading.OrderCleanupThreshold > 0 {
				sc.OrderCleanupThreshold = c.Trading.OrderCleanupThreshold
			} else {
				sc.OrderCleanupThreshold = 50
			}
		}

		if sc.CleanupBatchSize <= 0 {
			if c.Trading.CleanupBatchSize > 0 {
				sc.CleanupBatchSize = c.Trading.CleanupBatchSize
			} else {
				sc.CleanupBatchSize = 10
			}
		}

		if sc.MarginLockDurationSec <= 0 {
			if c.Trading.MarginLockDurationSec > 0 {
				sc.MarginLockDurationSec = c.Trading.MarginLockDurationSec
			} else {
				sc.MarginLockDurationSec = 10
			}
		}

		if sc.PositionSafetyCheck <= 0 {
			if c.Trading.PositionSafetyCheck > 0 {
				sc.PositionSafetyCheck = c.Trading.PositionSafetyCheck
			} else {
				sc.PositionSafetyCheck = 100
			}
		}

		// 风控配置继承
		if !sc.GridRiskControl.Enabled && c.Trading.GridRiskControl.Enabled {
			sc.GridRiskControl = c.Trading.GridRiskControl
		} else if sc.GridRiskControl.Enabled {
			// 如果启用了但某些字段没填，可以考虑从全局继承，但通常启用表示要自定义
			if sc.GridRiskControl.MaxGridLayers == 0 {
				sc.GridRiskControl.MaxGridLayers = c.Trading.GridRiskControl.MaxGridLayers
			}
			if sc.GridRiskControl.StopLossRatio == 0 {
				sc.GridRiskControl.StopLossRatio = c.Trading.GridRiskControl.StopLossRatio
			}
			if sc.GridRiskControl.TakeProfitTriggerRatio == 0 {
				sc.GridRiskControl.TakeProfitTriggerRatio = c.Trading.GridRiskControl.TakeProfitTriggerRatio
			}
			if sc.GridRiskControl.TrailingTakeProfitRatio == 0 {
				sc.GridRiskControl.TrailingTakeProfitRatio = c.Trading.GridRiskControl.TrailingTakeProfitRatio
			}
		}

		// 验证策略占比
		if len(sc.Strategies) > 0 {
			var totalWeight float64
			for _, s := range sc.Strategies {
				totalWeight += s.Weight
			}
			if totalWeight > 1.001 { // 允许微小误差
				return sc, fmt.Errorf("交易对 %s 的策略权重总和 (%.2f) 不能超过 1.0", sc.Symbol, totalWeight)
			}
		}

		return sc, nil
	}

	// 若未配置 symbols，则兼容旧配置转换为单元素
	if len(c.Trading.Symbols) == 0 {
		if c.Trading.Symbol == "" {
			return fmt.Errorf("交易对不能为空")
		}
		c.Trading.Symbols = []SymbolConfig{{
			Exchange:              c.App.CurrentExchange,
			Symbol:                c.Trading.Symbol,
			PriceInterval:         c.Trading.PriceInterval,
			OrderQuantity:         c.Trading.OrderQuantity,
			MinOrderValue:         c.Trading.MinOrderValue,
			BuyWindowSize:         c.Trading.BuyWindowSize,
			SellWindowSize:        c.Trading.SellWindowSize,
			ReconcileInterval:     c.Trading.ReconcileInterval,
			OrderCleanupThreshold: c.Trading.OrderCleanupThreshold,
			CleanupBatchSize:      c.Trading.CleanupBatchSize,
			MarginLockDurationSec: c.Trading.MarginLockDurationSec,
			PositionSafetyCheck:   c.Trading.PositionSafetyCheck,
			GridRiskControl:       c.Trading.GridRiskControl,
		}}
	}

	normalized := make([]SymbolConfig, 0, len(c.Trading.Symbols))
	for _, sc := range c.Trading.Symbols {
		norm, err := normalizeSymbol(sc)
		if err != nil {
			return err
		}
		normalized = append(normalized, norm)
	}
	c.Trading.Symbols = normalized

	// 兼容旧字段：保持首个交易对到旧字段，供未改造代码使用
	if len(c.Trading.Symbols) > 0 {
		primary := c.Trading.Symbols[0]
		c.Trading.Symbol = primary.Symbol
		c.Trading.PriceInterval = primary.PriceInterval
		c.Trading.OrderQuantity = primary.OrderQuantity
		c.Trading.MinOrderValue = primary.MinOrderValue
		c.Trading.BuyWindowSize = primary.BuyWindowSize
		c.Trading.SellWindowSize = primary.SellWindowSize
		c.Trading.ReconcileInterval = primary.ReconcileInterval
		c.Trading.OrderCleanupThreshold = primary.OrderCleanupThreshold
		c.Trading.CleanupBatchSize = primary.CleanupBatchSize
		c.Trading.MarginLockDurationSec = primary.MarginLockDurationSec
		c.Trading.PositionSafetyCheck = primary.PositionSafetyCheck
		c.Trading.GridRiskControl = primary.GridRiskControl
	}

	// 设置默认时间间隔
	if c.System.Timezone == "" {
		c.System.Timezone = "Asia/Shanghai" // 默认东8区
	}
	if c.System.LogRetentionDays <= 0 {
		c.System.LogRetentionDays = 30 // 默认保留30天
	}

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
		c.Storage.Path = "./data/quantmesh.db" // 默认路径
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
	
	// 设置 pprof 配置默认值
	if len(c.Web.Pprof.AllowedIPs) == 0 {
		// 默认允许本地访问
		c.Web.Pprof.AllowedIPs = []string{"127.0.0.1", "::1"}
	}
	// pprof.Enabled 默认为 false（生产环境安全）
	// pprof.RequireAuth 默认为 true（需要认证）

	// 设置实例配置默认值
	if c.Instance.ID == "" {
		c.Instance.ID = "default-instance" // 默认实例ID
	}
	if c.Instance.Total <= 0 {
		c.Instance.Total = 1 // 默认单实例
	}

	// 设置数据库配置默认值
	if c.Database.Type == "" {
		c.Database.Type = "sqlite" // 默认 SQLite（单机模式）
	}
	if c.Database.DSN == "" {
		if c.Database.Type == "sqlite" {
			c.Database.DSN = "./data/quantmesh.db" // 默认 SQLite 路径
		}
	}
	if c.Database.MaxOpenConns <= 0 {
		c.Database.MaxOpenConns = 100 // 默认100
	}
	if c.Database.MaxIdleConns <= 0 {
		c.Database.MaxIdleConns = 10 // 默认10
	}
	if c.Database.ConnMaxLifetime <= 0 {
		c.Database.ConnMaxLifetime = 3600 // 默认1小时
	}
	if c.Database.LogLevel == "" {
		c.Database.LogLevel = "error" // 默认只记录错误
	}

	// 设置分布式锁配置默认值
	// 注意：默认不启用分布式锁（单机模式）
	if c.DistributedLock.Type == "" {
		c.DistributedLock.Type = "redis" // 默认使用 Redis
	}
	if c.DistributedLock.Prefix == "" {
		c.DistributedLock.Prefix = "quantmesh:lock:" // 默认前缀
	}
	if c.DistributedLock.DefaultTTL <= 0 {
		c.DistributedLock.DefaultTTL = 5 // 默认5秒
	}
	if c.DistributedLock.Redis.Addr == "" {
		c.DistributedLock.Redis.Addr = "localhost:6379" // 默认 Redis 地址
	}
	if c.DistributedLock.Redis.PoolSize <= 0 {
		c.DistributedLock.Redis.PoolSize = 10 // 默认连接池大小
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
			"total_pnl":    0.4,
			"sharpe_ratio": 0.3,
			"win_rate":     0.2,
			"max_drawdown": 0.1,
		}
	}

	// 设置事件中心配置默认值
	// 默认启用事件中心
	if c.EventCenter.PriceVolatilityThreshold <= 0 {
		c.EventCenter.PriceVolatilityThreshold = 5.0 // 默认5%波动
	}
	if c.EventCenter.Retention.CriticalDays <= 0 {
		c.EventCenter.Retention.CriticalDays = 365 // Critical 事件保留1年
	}
	if c.EventCenter.Retention.WarningDays <= 0 {
		c.EventCenter.Retention.WarningDays = 90 // Warning 事件保留3个月
	}
	if c.EventCenter.Retention.InfoDays <= 0 {
		c.EventCenter.Retention.InfoDays = 30 // Info 事件保留1个月
	}
	if c.EventCenter.Retention.CriticalMaxCount <= 0 {
		c.EventCenter.Retention.CriticalMaxCount = 1000000 // Critical 最多保留100万条
	}
	if c.EventCenter.Retention.WarningMaxCount <= 0 {
		c.EventCenter.Retention.WarningMaxCount = 500000 // Warning 最多保留50万条
	}
	if c.EventCenter.Retention.InfoMaxCount <= 0 {
		c.EventCenter.Retention.InfoMaxCount = 300000 // Info 最多保留30万条
	}
	if c.EventCenter.CleanupInterval <= 0 {
		c.EventCenter.CleanupInterval = 24 // 默认每24小时清理一次
	}

	return nil
}
