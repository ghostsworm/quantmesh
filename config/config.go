package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BoolPtr 回傳 bool 值的指標（用於 SymbolConfig.Enabled 等欄位）
func BoolPtr(b bool) *bool {
	return &b
}

// DefaultGoldKeywords 回傳預設的黃金新聞監控關鍵詞
func DefaultGoldKeywords() []string {
	return []string{
		"gold", "XAU", "黃金", "金價", "gold price",
		"Federal Reserve", "Fed", "美聯儲", "interest rate", "利率",
		"Dollar", "DXY", "美元指數", "US dollar",
		"Inflation", "CPI", "PCE", "通脹",
		"geopolitical", "地緣政治", "war", "戰爭", "sanctions", "制裁",
		"central bank", "央行", "gold reserve", "黃金儲備", "gold holdings",
		"safe haven", "避險", "volatility", "市場波動",
	}
}

// DefaultNewsKeywords 回傳預設的新聞監控關鍵詞（影響比特幣或類似幣價格）
func DefaultNewsKeywords() []string {
	return []string{
		// 加密貨幣核心
		"bitcoin", "btc", "cryptocurrency", "crypto", "ethereum", "eth",
		// 交易所和機構
		"binance", "coinbase", "ftx", "tether", "usdt", "stablecoin",
		"grayscale", "blackrock bitcoin", "bitcoin etf",
		// 監管和政策
		"sec crypto", "crypto regulation", "crypto ban", "crypto legislation",
		"cbdc", "federal reserve crypto",
		// 地緣政治
		"iran attack", "iran explosion", "israel attack", "middle east conflict",
		"russia sanctions", "china crypto", "trump crypto", "biden crypto",
		// 總體經濟
		"interest rate", "fed rate", "inflation", "tariff", "trade war",
		"economic crisis", "bank failure", "banking crisis",
		// 安全事件
		"crypto hack", "exchange hack", "rug pull", "crypto scam", "wallet hack",
		// 市場動態
		"bitcoin crash", "crypto crash", "bitcoin rally", "whale transfer",
		"bitcoin liquidation", "crypto liquidation",
	}
}

// AssetConfig 新聞監控多資產配置
type AssetConfig struct {
	AssetType string   `yaml:"asset_type" json:"asset_type"` // crypto_btc, commodity_gold
	Symbol    string   `yaml:"symbol" json:"symbol"`         // BTCUSDT, PAXGUSDT
	Keywords  []string `yaml:"keywords" json:"keywords"`     // 該資產專用關鍵詞
	Enabled   bool     `yaml:"enabled" json:"enabled"`       // 是否啟用
}

// InspectorFocusSymbol 智子巡檢關注的交易對
type InspectorFocusSymbol struct {
	Symbol       string `yaml:"symbol"`
	Exchange     string `yaml:"exchange"`
	AnalysisType string `yaml:"analysis_type"` // full, standard
}

// CompositeRiskThresholds 複合風控分數閾值
type CompositeRiskThresholds struct {
	Caution        float64 `yaml:"caution"`
	ReducePosition float64 `yaml:"reduce_position"`
	PauseBuying    float64 `yaml:"pause_buying"`
	StopTrading    float64 `yaml:"stop_trading"`
}

// CompositeRiskFactorsConfig 複合風控各因子配置
type CompositeRiskFactorsConfig struct {
	News        CompositeRiskFactorOpts `yaml:"news"`
	Trend       CompositeRiskFactorOpts `yaml:"trend"`
	FundingRate CompositeRiskFactorOpts `yaml:"funding_rate"`
	Depth       CompositeRiskFactorOpts `yaml:"depth"`
	Kline       CompositeRiskFactorOpts `yaml:"kline"`
}

// CompositeRiskFactorOpts 單一因子開關與權重
type CompositeRiskFactorOpts struct {
	Enabled                    bool    `yaml:"enabled"`
	Weight                     float64 `yaml:"weight"`
	UseRSI                     bool    `yaml:"use_rsi"`
	UseMACD                    bool    `yaml:"use_macd"`
	ConsecutiveNegativePeriods int     `yaml:"consecutive_negative_periods"`
}

// GridRiskControl 網格策略風控配置
type GridRiskControl struct {
	Enabled                 bool    `yaml:"enabled" json:"enabled"`
	MaxGridLayers           int     `yaml:"max_grid_layers" json:"max_grid_layers"`                       // 最大允許買入層數
	StopLossRatio           float64 `yaml:"stop_loss_ratio" json:"stop_loss_ratio"`                       // 單幣種最大浮虧比例（如 0.1 表示 10%）
	TakeProfitTriggerRatio  float64 `yaml:"take_profit_trigger_ratio" json:"take_profit_trigger_ratio"`   // 盈利達到此比例後開啟回撤止盈（如 0.08 表示 8%）
	TrailingTakeProfitRatio float64 `yaml:"trailing_take_profit_ratio" json:"trailing_take_profit_ratio"` // 盈利回撤比例（如 0.03 表示回撤 3% 止盈）
	TrendFilterEnabled      bool    `yaml:"trend_filter_enabled" json:"trend_filter_enabled"`             // 是否開啟趨勢過濾
}

// FundingRateConfig 資金費率監控與套利配置
type FundingRateConfig struct {
	Enabled         bool    `yaml:"enabled" json:"enabled"`                   // 是否啟用資金費率監控
	MonitorInterval int     `yaml:"monitor_interval" json:"monitor_interval"` // 監控間隔（秒），預設 60
	AlertThreshold  float64 `yaml:"alert_threshold" json:"alert_threshold"`   // 告警閾值，預設 0.001 (0.1%)

	// 偏向策略配置
	BiasEnabled       bool    `yaml:"bias_enabled" json:"bias_enabled"`               // 是否啟用費率偏向策略
	HighRateThreshold float64 `yaml:"high_rate_threshold" json:"high_rate_threshold"` // 高費率閾值，預設 0.001 (0.1%)
	PauseBuyThreshold float64 `yaml:"pause_buy_threshold" json:"pause_buy_threshold"` // 暫停買入閾值，預設 0.0015 (0.15%)
	TrendSyncEnabled  bool    `yaml:"trend_sync_enabled" json:"trend_sync_enabled"`   // 是否啟用費率與趨勢聯動，預設 true

	// 期現套利配置
	ArbitrageEnabled   bool    `yaml:"arbitrage_enabled" json:"arbitrage_enabled"`       // 是否啟用期現套利
	HedgeMinPosition   float64 `yaml:"hedge_min_position" json:"hedge_min_position"`     // 最小對沖倉位（USDT），預設 100
	HedgeRateThreshold float64 `yaml:"hedge_rate_threshold" json:"hedge_rate_threshold"` // 開啟對沖的費率閾值，預設 0.001
	MaxSpreadPercent   float64 `yaml:"max_spread_percent" json:"max_spread_percent"`     // 最大價差百分比，超過則暫停對沖，預設 0.5%
}

// OrderbookOptimization 訂單簿優化掛單配置
type OrderbookOptimization struct {
	Enabled              bool `yaml:"enabled" json:"enabled"`                             // 是否啟用訂單簿優化掛單，預設 false
	DepthLevels          int  `yaml:"depth_levels" json:"depth_levels"`                   // 取得訂單簿檔位數，預設 20
	MinDepthUSDT         int  `yaml:"min_depth_usdt" json:"min_depth_usdt"`               // 低於此視為空洞（USDT），需微調，預設 5000
	LookbackLevels       int  `yaml:"lookback_levels" json:"lookback_levels"`             // 檢查候選價前後 N 檔，預設 3
	OptimizationInterval int  `yaml:"optimization_interval" json:"optimization_interval"` // 優化間隔（秒），0 表示每次 AdjustOrders 都優化，預設 30
}

// Config 做市商系統配置
type Config struct {
	// 應用配置
	App struct {
		CurrentExchange string `yaml:"current_exchange"` // 當前使用的交易所
	} `yaml:"app"`

	// 多交易所配置
	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`

	Trading struct {
		// 相容舊配置：單交易對欄位（若啟用多交易對，將自動轉換為 Symbols 列表）
		Symbol                string  `yaml:"symbol"`
		MarketType            string  `yaml:"market_type"` // 市場類型：spot 現貨 / futures 合約，預設 futures
		PriceInterval         float64 `yaml:"price_interval"`
		OrderQuantity         float64 `yaml:"order_quantity"`  // 每單購買金額（USDT/USDC）
		MinOrderValue         float64 `yaml:"min_order_value"` // 最小訂單價值（USDT），預設 6U，小於此值不掛單
		BuyWindowSize         int     `yaml:"buy_window_size"`
		SellWindowSize        int     `yaml:"sell_window_size"` // 賣單視窗大小
		ReconcileInterval     int     `yaml:"reconcile_interval"`
		OrderCleanupThreshold int     `yaml:"order_cleanup_threshold"`      // 訂單清理上限（預設 100）
		CleanupBatchSize      int     `yaml:"cleanup_batch_size"`           // 清理批次大小（預設 10）
		MarginLockDurationSec int     `yaml:"margin_lock_duration_seconds"` // 保證金鎖定時間（秒，預設 10）
		PositionSafetyCheck   int     `yaml:"position_safety_check"`        // 持倉安全性檢查（預設 100，最少能向下持有多少倉）
		Direction             string  `yaml:"direction"`                    // 交易方向：LONG 做多 / SHORT 做空，預設 LONG
		// 多交易對配置
		Symbols []SymbolConfig `yaml:"symbols"`
		// 注意：price_decimals 和 quantity_decimals 已廢棄，現在從交易所自动獲取

		// 動態調整网格参數
		DynamicAdjustment struct {
			Enabled bool `yaml:"enabled"`

			PriceInterval struct {
				Enabled             bool    `yaml:"enabled"`
				Min                 float64 `yaml:"min"`                  // 最小價格間隔
				Max                 float64 `yaml:"max"`                  // 最大價格間隔
				VolatilityWindow    int     `yaml:"volatility_window"`    // 波动率计算窗口（K線數量）
				VolatilityThreshold float64 `yaml:"volatility_threshold"` // 波动率阈值
				AdjustmentStep      float64 `yaml:"adjustment_step"`      // 每次調整步长
				CheckInterval       int     `yaml:"check_interval"`       // 檢查间隔（秒）
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
				AdjustmentStep       int     `yaml:"adjustment_step"`       // 每次調整步长
				CheckInterval        int     `yaml:"check_interval"`        // 檢查间隔（秒）
			} `yaml:"window_size"`

			OrderQuantity struct {
				Enabled            bool    `yaml:"enabled"`
				Min                float64 `yaml:"min"`
				Max                float64 `yaml:"max"`
				FrequencyThreshold int     `yaml:"frequency_threshold"` // 交易频率阈值（次/分钟）
				AdjustmentStep     float64 `yaml:"adjustment_step"`
				CheckInterval      int     `yaml:"check_interval"` // 檢查间隔（秒），預設 60
			} `yaml:"order_quantity"`
		} `yaml:"dynamic_adjustment"`

		// 智能倉位管理
		SmartPosition struct {
			Enabled bool `yaml:"enabled"`

			TrendDetection struct {
				Enabled       bool   `yaml:"enabled"`
				Window        int    `yaml:"window"`         // 趋势判断窗口（價格數量）
				Method        string `yaml:"method"`         // 方法：ma/ema
				ShortPeriod   int    `yaml:"short_period"`   // 短期均線周期
				LongPeriod    int    `yaml:"long_period"`    // 长期均線周期
				CheckInterval int    `yaml:"check_interval"` // 檢查间隔（秒）
			} `yaml:"trend_detection"`

			WindowAdjustment struct {
				Enabled        bool    `yaml:"enabled"`
				MaxAdjustment  float64 `yaml:"max_adjustment"`  // 最大調整比例
				AdjustmentStep int     `yaml:"adjustment_step"` // 每次調整步长
				MinBuyWindow   int     `yaml:"min_buy_window"`  // 最小買單窗口
				MinSellWindow  int     `yaml:"min_sell_window"` // 最小賣單視窗
			} `yaml:"window_adjustment"`
		} `yaml:"smart_position"`

		GridRiskControl       GridRiskControl       `yaml:"grid_risk_control"`
		OrderbookOptimization OrderbookOptimization `yaml:"orderbook_optimization"`
	} `yaml:"trading"`

	System struct {
		LogLevel             string `yaml:"log_level"`
		Timezone             string `yaml:"timezone"`     // 時区，如 "Asia/Shanghai"
		LogLanguage          string `yaml:"log_language"` // 日志语言，如 "zh-CN" 或 "en-US"
		CancelOnExit         bool   `yaml:"cancel_on_exit"`
		ClosePositionsOnExit bool   `yaml:"close_positions_on_exit"` // 退出時是否平倉（預設false）
		LogRetentionDays     int    `yaml:"log_retention_days"`      // 日志保留天數（預設30天，0表示不清理）
		DryRun               bool   `yaml:"dry_run"`                 // 模拟运行模式（不實際下單，只記錄日志）
	} `yaml:"system"`

	// 實例配置（多實例部署）
	Instance struct {
		ID    string `yaml:"id"`    // 實例唯一標识，預設為空（單實例模式）
		Index int    `yaml:"index"` // 實例索引，用於交易對分配，預設0
		Total int    `yaml:"total"` // 總實例數，預設1
	} `yaml:"instance"`

	// 數據库配置（支援 SQLite、PostgreSQL、MySQL）
	Database struct {
		Type            string `yaml:"type"`              // 數據库類型: sqlite, postgres, mysql，預設 sqlite
		DSN             string `yaml:"dsn"`               // 數據源名称，預設 ./data/quantmesh.db
		MaxOpenConns    int    `yaml:"max_open_conns"`    // 最大打开連接數，預設 100
		MaxIdleConns    int    `yaml:"max_idle_conns"`    // 最大空闲連接數，預設 10
		ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // 連接最大生命周期（秒），預設3600
		LogLevel        string `yaml:"log_level"`         // 日志级别: silent, error, warn, info，預設 error
	} `yaml:"database"`

	// 分布式鎖配置（多實例部署）
	DistributedLock struct {
		Enabled    bool   `yaml:"enabled"`     // 是否啟用分布式鎖，預設false（單實例模式）
		Type       string `yaml:"type"`        // 鎖類型: redis, etcd, database，預設 redis
		Prefix     string `yaml:"prefix"`      // 鎖键前缀，預設 "quantmesh:lock:"
		DefaultTTL int    `yaml:"default_ttl"` // 預設鎖過期時间（秒），預設5

		Redis struct {
			Addr     string `yaml:"addr"`      // Redis 地址，預設 localhost:6379
			Password string `yaml:"password"`  // Redis 密碼，預設為空
			DB       int    `yaml:"db"`        // Redis 數據库，預設0
			PoolSize int    `yaml:"pool_size"` // 連接池大小，預設 10
		} `yaml:"redis"`
	} `yaml:"distributed_lock"`

	// 主动安全風控配置
	RiskControl struct {
		Enabled           bool     `yaml:"enabled"`            // 是否啟用风控，預設true
		MonitorSymbols    []string `yaml:"monitor_symbols"`    // 監控币种，如 ["BTCUSDT", "ETHUSDT"]
		Interval          string   `yaml:"interval"`           // K線週期，如 "1m", "3m", "5m"
		VolumeMultiplier  float64  `yaml:"volume_multiplier"`  // 成交量倍數阈值，預設3.0
		AverageWindow     int      `yaml:"average_window"`     // 移动平均窗口大小，預設20
		RecoveryThreshold int      `yaml:"recovery_threshold"` // 恢復交易所需的正常币种數量，預設3
		MaxLeverage       int      `yaml:"max_leverage"`       // 最大允許杠杆倍數，預設 10（設置為0表示不限制）

		// 深度監控配置
		DepthMonitor struct {
			Enabled           bool    `yaml:"enabled"`            // 是否啟用深度監控，預設false
			CheckInterval     int     `yaml:"check_interval"`     // 檢查间隔（秒），預設5
			DepthLevels       int     `yaml:"depth_levels"`       // 監控前几檔，預設 10
			DropThreshold     float64 `yaml:"drop_threshold"`     // 深度下降阈值（0-1），預設0.5（50%）
			RecoveryThreshold float64 `yaml:"recovery_threshold"` // 恢復阈值（0-1），預設0.7（70%）
			MinDepthUSDT      float64 `yaml:"min_depth_usdt"`     // 最小深度（USDT），低於此值触发风控，預設 10000
		} `yaml:"depth_monitor"`
	} `yaml:"risk_control"`

	// 複合風控引擎配置
	CompositeRisk struct {
		Enabled           bool                        `yaml:"enabled"`
		EvaluateInterval  int                         `yaml:"evaluate_interval"`
		Thresholds        CompositeRiskThresholds     `yaml:"thresholds"`
		Factors           CompositeRiskFactorsConfig  `yaml:"factors"`
	} `yaml:"composite_risk"`

	// 資金費率監控與套利配置
	FundingRate FundingRateConfig `yaml:"funding_rate" json:"funding_rate"`

	// 新聞監控配置
	NewsMonitor struct {
		Enabled              bool     `yaml:"enabled"`               // 是否啟用新聞監控，預設false
		EnableAnalysis       *bool    `yaml:"enable_analysis"`       // 是否啟用新聞分析功能（Gemini分析），nil表示未設置（默認true），false表示明確關閉
		CheckInterval        string   `yaml:"check_interval"`        // 相容舊配置，等同於 analysis_interval
		AnalysisInterval     string   `yaml:"analysis_interval"`     // AI分析间隔，預設"30m"，支持 "5m", "15m", "30m", "1h", "2h", "4h", "8h", "24h"
		NewsCollectInterval  string   `yaml:"news_collect_interval"` // NewsAPI收集间隔，預設"5m"
		UseGeminiSearch      bool     `yaml:"use_gemini_search"`     // 是否使用Gemini實時搜索，預設true（兼容旧配置）
		Sources              []string `yaml:"sources"`               // 新闻源列表，如 ["newsapi", "rss"]
		NewsAPIKey           string   `yaml:"news_api_key"`          // NewsAPI密钥（可選）
		RSSFeeds             []string `yaml:"rss_feeds"`             // RSS源列表（可選）
		CustomRSSFeeds       []string `yaml:"custom_rss_feeds"`      // 用戶自定義 RSS 源（與 rss_feeds 合併使用）
		Keywords             []string `yaml:"keywords"`              // NewsAPI關注的關鍵詞（可在UI修改，用於BTC）
		RiskThreshold        float64  `yaml:"risk_threshold"`        // 相容舊配置，风險阈值（0-100）
		PredictionTimeframes []string `yaml:"prediction_timeframes"` // 預测時间窗口，如["2h","4h","6h","12h","24h"]
		RiskThresholds       struct {
			StopTradingProbability    float64 `yaml:"stop_trading_probability"`    // 概率超過此值暂停交易，預設0.7
			ReducePositionProbability float64 `yaml:"reduce_position_probability"` // 概率超過此值减少倉位，預設0.5
		} `yaml:"risk_thresholds"`
		HistoryRetentionDays int           `yaml:"history_retention_days"` // 历史記錄保留天數，預設30
		Assets               []AssetConfig `yaml:"assets"`                 // 多资產配置（crypto_btc, commodity_gold）
		// AI Provider 配置
		AIProvider struct {
			Provider string `yaml:"provider"` // gemini, openai, claude, poe，預設 gemini
			Model    string `yaml:"model"`    // 模型名稱，如 "gpt-4", "claude-3-opus"，預設為各provider的默認模型
			APIKey   string `yaml:"api_key"`  // Provider 的 API Key
			BaseURL  string `yaml:"base_url"` // 可選，自定義 API 端點（用於 Poe 等代理）
		} `yaml:"ai_provider"`
	} `yaml:"news_monitor"`

	// 時间间隔配置（單位：秒，除非特别說明）
	Timing struct {
		// WebSocket相关
		WebSocketReconnectDelay    int `yaml:"websocket_reconnect_delay"`     // WebSocket断線重连等待時间（秒，預設5）
		WebSocketWriteWait         int `yaml:"websocket_write_wait"`          // WebSocket写入等待時间（秒，預設 10）
		WebSocketPongWait          int `yaml:"websocket_pong_wait"`           // WebSocket PONG等待時间（秒，預設60）
		WebSocketPingInterval      int `yaml:"websocket_ping_interval"`       // WebSocket PING间隔（秒，預設20）
		ListenKeyKeepAliveInterval int `yaml:"listen_key_keepalive_interval"` // listenKey保活间隔（分钟，預設30）

		// 價格監控相关
		PriceSendInterval int `yaml:"price_send_interval"` // 定期发送價格的间隔（毫秒，預設50）

		// 訂單執行相关
		RateLimitRetryDelay  int `yaml:"rate_limit_retry_delay"` // 速率限制重試等待時间（秒，預設1）
		OrderRetryDelay      int `yaml:"order_retry_delay"`      // 其他錯误重試等待時间（毫秒，預設500）
		PricePollInterval    int `yaml:"price_poll_interval"`    // 等待獲取價格的輪詢间隔（毫秒，預設500）
		StatusPrintInterval  int `yaml:"status_print_interval"`  // 定期打印狀態的间隔（分钟，預設1）
		OrderCleanupInterval int `yaml:"order_cleanup_interval"` // 訂單清理檢查间隔（秒，預設60）
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
			Timeout int    `yaml:"timeout"` // 超時時间（秒，預設3）
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
			Secret  string `yaml:"secret"`  // 钉钉机器人签名密钥（可選）
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
			InspectorReport    bool `yaml:"inspector_report"`    // 智子巡檢報告（定時/緊急）
		} `yaml:"rules"`
	} `yaml:"notifications"`

	// 存儲配置
	Storage struct {
		Enabled       bool   `yaml:"enabled"`
		Type          string `yaml:"type"`           // sqlite
		Path          string `yaml:"path"`           // 數據库文件路径
		BufferSize    int    `yaml:"buffer_size"`    // 缓冲区大小（預設 1000）
		BatchSize     int    `yaml:"batch_size"`     // 批量写入大小（預設 100）
		FlushInterval int    `yaml:"flush_interval"` // 刷新间隔（秒，預設5）
	} `yaml:"storage"`

	// Web 服務配置
	Web struct {
		Enabled bool   `yaml:"enabled"`
		Host    string `yaml:"host"`    // 監听地址（預設 0.0.0.0）
		Port    int    `yaml:"port"`    // 監听端口（預設 8080）
		Domain  string `yaml:"domain"`  // 外部访问域名（用於 WebAuthn RPID 配置）
		APIKey  string `yaml:"api_key"` // API 密钥（可選，用於认证）

		// TLS/HTTPS 配置
		TLS *struct {
			Enabled  bool   `yaml:"enabled"`   // 是否启用 HTTPS
			CertFile string `yaml:"cert_file"` // 証書文件路徑
			KeyFile  string `yaml:"key_file"`  // 私钥文件路徑
		} `yaml:"tls"`

		// pprof 性能分析配置
		Pprof struct {
			Enabled     bool     `yaml:"enabled"`      // 是否啟用 pprof，預設 false（生產环境建议禁用）
			RequireAuth bool     `yaml:"require_auth"` // 是否需要认证，預設 true
			AllowedIPs  []string `yaml:"allowed_ips"`  // IP 白名單（可選，為空则允許所有 IP）
		} `yaml:"pprof"`
	} `yaml:"web"`

	// 插件配置
	Plugins struct {
		Enabled   bool                              `yaml:"enabled"`   // 是否啟用插件系统，預設false
		Directory string                            `yaml:"directory"` // 插件目錄，預設 ./plugins
		Licenses  map[string]string                 `yaml:"licenses"`  // 插件 License Keys
		Config    map[string]map[string]interface{} `yaml:"config"`    // 插件配置
	} `yaml:"plugins"`

	// 價差監控配置
	BasisMonitor struct {
		Enabled         bool     `yaml:"enabled"`          // 是否啟用價差監控，預設false
		IntervalMinutes int      `yaml:"interval_minutes"` // 檢查间隔（分钟），預設1
		Symbols         []string `yaml:"symbols"`          // 監控的交易對列表
	} `yaml:"basis_monitor"`

	// 事件中心配置
	EventCenter struct {
		Enabled                  bool     `yaml:"enabled"`                    // 是否啟用事件中心，預設true
		PriceVolatilityThreshold float64  `yaml:"price_volatility_threshold"` // 價格波動阈值（百分比），預設5.0
		MonitoredSymbols         []string `yaml:"monitored_symbols"`          // 監控價格波動的交易對

		// 事件保留策略
		Retention struct {
			CriticalDays int `yaml:"critical_days"` // Critical 事件保留天數，預設365
			WarningDays  int `yaml:"warning_days"`  // Warning 事件保留天數，預設90
			InfoDays     int `yaml:"info_days"`     // Info 事件保留天數，預設30

			CriticalMaxCount int `yaml:"critical_max_count"` // Critical 事件最大保留數量，預設 1000000
			WarningMaxCount  int `yaml:"warning_max_count"`  // Warning 事件最大保留數量，預設500000
			InfoMaxCount     int `yaml:"info_max_count"`     // Info 事件最大保留數量，預設300000
		} `yaml:"retention"`

		CleanupInterval int `yaml:"cleanup_interval"` // 清理间隔（小時），預設24
	} `yaml:"event_center"`

	// 多策略配置
	Strategies struct {
		Enabled bool `yaml:"enabled"`

		// 资金分配配置
		CapitalAllocation struct {
			Mode         string  `yaml:"mode"`          // fixed/dynamic/both
			TotalCapital float64 `yaml:"total_capital"` // 總资金（USDT）

			// 固定分配
			Fixed struct {
				Enabled          bool `yaml:"enabled"`
				RebalanceOnStart bool `yaml:"rebalance_on_start"` // 啟动時重新分配
			} `yaml:"fixed"`

			// 动態分配
			DynamicAllocation struct {
				Enabled               bool    `yaml:"enabled"`
				RebalanceInterval     int     `yaml:"rebalance_interval"`       // 重新平衡间隔（秒，預設3600）
				MaxChangePerRebalance float64 `yaml:"max_change_per_rebalance"` // 每次最大調整比例（預設0.05）
				MinWeight             float64 `yaml:"min_weight"`               // 最小权重（預設0.1）
				MaxWeight             float64 `yaml:"max_weight"`               // 最大权重（預設0.7）

				// 评估指標权重
				PerformanceWeights map[string]float64 `yaml:"performance_weights"`
			} `yaml:"dynamic"`
		} `yaml:"capital_allocation"`

		// 策略配置
		Configs map[string]StrategyConfig `yaml:"configs"`
	} `yaml:"strategies"`

	// 回测配置
	Backtest struct {
		Enabled        bool    `yaml:"enabled"`
		StartTime      string  `yaml:"start_time"`      // 开始時间（格式：2006-01-02 15:04:05）
		EndTime        string  `yaml:"end_time"`        // 結束時间
		InitialCapital float64 `yaml:"initial_capital"` // 初始资金
	} `yaml:"backtest"`

	// 倉位资金分配管理
	PositionAllocation struct {
		Enabled     bool               `yaml:"enabled"`
		Allocations []SymbolAllocation `yaml:"allocations"`
	} `yaml:"position_allocation"`

	// 監控配置
	Metrics struct {
		Enabled         bool `yaml:"enabled"`
		CollectInterval int  `yaml:"collect_interval"` // 收集间隔（秒，預設60）
	} `yaml:"metrics"`

	// 看门狗配置
	Watchdog struct {
		Enabled bool `yaml:"enabled"`

		// 采样配置
		Sampling struct {
			Interval int `yaml:"interval"` // 采样间隔（秒，預設120秒=2分钟）
		} `yaml:"sampling"`

		// 數據保留
		Retention struct {
			DetailDays int `yaml:"detail_days"` // 细粒度數據保留天數（預設7天）
			DailyDays  int `yaml:"daily_days"`  // 每日彙總保留天數（預設365天）
		} `yaml:"retention"`

		// 通知配置
		Notifications struct {
			Enabled bool `yaml:"enabled"`

			// 固定阈值通知
			FixedThreshold struct {
				Enabled    bool    `yaml:"enabled"`
				CPUPercent float64 `yaml:"cpu_percent"` // CPU占用超過此值時通知
				MemoryMB   float64 `yaml:"memory_mb"`   // 記憶體占用超過此值時通知（可選，0表示不檢查）
			} `yaml:"fixed_threshold"`

			// 变化率阈值通知
			RateThreshold struct {
				Enabled          bool    `yaml:"enabled"`
				WindowMinutes    int     `yaml:"window_minutes"`     // 時间窗口（分钟）
				CPUIncrease      float64 `yaml:"cpu_increase"`       // CPU占用在窗口内上涨超過此值時通知
				MemoryIncreaseMB float64 `yaml:"memory_increase_mb"` // 記憶體占用在窗口内上涨超過此值時通知（可選，0表示不檢查）
			} `yaml:"rate_threshold"`

			// 通知冷却時间（避免频繁通知）
			CooldownMinutes int `yaml:"cooldown_minutes"` // 冷却時间（分钟，預設30分钟）
		} `yaml:"notifications"`

		// 每日彙總配置
		Aggregation struct {
			Enabled  bool   `yaml:"enabled"`
			Schedule string `yaml:"schedule"` // 每日彙總執行時间（格式：HH:MM，預設00:00）
		} `yaml:"aggregation"`
	} `yaml:"watchdog"`

	// 智子巡檢配置
	Inspector struct {
		Enabled  bool   `yaml:"enabled"`
		Name     string `yaml:"name"` // 顯示名稱，預設「智子巡檢」
		Schedule struct {
			RegularInterval string `yaml:"regular_interval"`  // 常規間隔，如 "1h"
			QuietHoursStart int    `yaml:"quiet_hours_start"` // 靜默時段開始（0-23）
			QuietHoursEnd   int    `yaml:"quiet_hours_end"`   // 靜默時段結束（0-23）
			QuietInterval   string `yaml:"quiet_interval"`    // 靜默時段內發送間隔，如 "4h"
		} `yaml:"schedule"`
		Thresholds struct {
			PnLAlert          float64 `yaml:"pnl_change_alert"`   // 單筆盈虧超過此值立即通知（USDT）
			RiskScoreChange   float64 `yaml:"risk_score_change"`  // 新聞風險評分變化閾值
			FundingRateAlert  float64 `yaml:"funding_rate_alert"` // 資金費率異常閾值
			CorrelationChange float64 `yaml:"correlation_change"` // 黃金與 BTC 相關性變化閾值
			BalanceChangePct  float64 `yaml:"balance_change_pct"` // 賬戶餘額變化百分比閾值
		} `yaml:"thresholds"`
		FocusSymbols []InspectorFocusSymbol `yaml:"focus_symbols"`
		AI           struct {
			Provider      string `yaml:"provider"` // gemini
			Model         string `yaml:"model"`
			AnalysisDepth string `yaml:"analysis_depth"` // brief, standard, detailed
		} `yaml:"ai"`
		Report struct {
			Format                   string `yaml:"format"` // markdown
			IncludeAIInsights        bool   `yaml:"include_ai_insights"`
			IncludeTechnicalAnalysis bool   `yaml:"include_technical_analysis"`
			MaxNewsItems             int    `yaml:"max_news_items"`
		} `yaml:"report"`
	} `yaml:"inspector"`

	// AI配置
	AI struct {
		Enabled      bool   `yaml:"enabled"`
		Provider     string `yaml:"provider"` // gemini, openai
		APIKey       string `yaml:"api_key"`
		GeminiAPIKey string `yaml:"gemini_api_key"` // Gemini API 密钥（优先使用，如果為空则使用 api_key）
		BaseURL      string `yaml:"base_url"`       // 可選，用於自定义API端点

		// 各模塊开关
		Modules struct {
			MarketAnalysis struct {
				Enabled        bool `yaml:"enabled"`
				UpdateInterval int  `yaml:"update_interval"` // 秒
			} `yaml:"market_analysis"`

			ParameterOptimization struct {
				Enabled              bool `yaml:"enabled"`
				OptimizationInterval int  `yaml:"optimization_interval"` // 秒
				AutoApply            bool `yaml:"auto_apply"`            // 是否自動套用優化結果
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
						Subreddits []string `yaml:"subreddits"` // Reddit子版塊列表
						PostLimit  int      `yaml:"post_limit"` // 每個子版塊獲取的帖子數量
					} `yaml:"social_media"`
				} `yaml:"data_sources"`
			} `yaml:"sentiment_analysis"`

			StrategyGeneration struct {
				Enabled bool `yaml:"enabled"` // 實驗性功能
			} `yaml:"strategy_generation"`

			PolymarketSignal struct {
				Enabled          bool   `yaml:"enabled"`
				AnalysisInterval int    `yaml:"analysis_interval"` // 秒
				APIURL           string `yaml:"api_url"`           // Polymarket API地址
				Markets          struct {
					Keywords        []string `yaml:"keywords"`           // 关注的市场关键词
					MinLiquidity    float64  `yaml:"min_liquidity"`      // 最小流动性（USDC）
					MinVolume24h    float64  `yaml:"min_volume_24h"`     // 最小24小時交易量（USDC）
					MinDaysToExpiry int      `yaml:"min_days_to_expiry"` // 最小到期天數
					MaxDaysToExpiry int      `yaml:"max_days_to_expiry"` // 最大到期天數
				} `yaml:"markets"`
				SignalGeneration struct {
					BuyThreshold      float64 `yaml:"buy_threshold"`       // 買入信号阈值（概率>此值）
					SellThreshold     float64 `yaml:"sell_threshold"`      // 賣出信号阈值（概率<此值）
					MinSignalStrength float64 `yaml:"min_signal_strength"` // 最小信号强度
					MinConfidence     float64 `yaml:"min_confidence"`      // 最小置信度
				} `yaml:"signal_generation"`
			} `yaml:"polymarket_signal"`
		} `yaml:"modules"`

		// 决策模式
		DecisionMode string `yaml:"decision_mode"` // advisor, executor, hybrid

		// 執行模式规则
		ExecutionRules struct {
			HighRiskThreshold   float64 `yaml:"high_risk_threshold"`  // 高风險场景：僅建议
			LowRiskThreshold    float64 `yaml:"low_risk_threshold"`   // 低风險场景：可直接執行
			RequireConfirmation bool    `yaml:"require_confirmation"` // 需要人工确认的场景
		} `yaml:"execution_rules"`
	} `yaml:"ai"`

	// 合规配置（订單/成交审计日志 + OSS 上傳）
	Compliance struct {
		Enabled bool `yaml:"enabled"`

		// 审计日志配置
		AuditLog struct {
			Enabled   bool   `yaml:"enabled"`
			Format    string `yaml:"format"`    // csv, jsonl, both
			Directory string `yaml:"directory"` // 存儲目錄，預設 ./data/audit
		} `yaml:"audit_log"`

		// OSS 上傳配置（阿里云）
		OSS struct {
			Enabled         bool   `yaml:"enabled"`
			Provider        string `yaml:"provider"` // aliyun
			Endpoint        string `yaml:"endpoint"`
			Bucket          string `yaml:"bucket"`
			AccessKeyID     string `yaml:"access_key_id"`
			AccessKeySecret string `yaml:"access_key_secret"`
			Prefix          string `yaml:"prefix"`      // 文件前缀，如 audit/
			UploadTime      string `yaml:"upload_time"` // 每日上傳時间，如 "02:00"
		} `yaml:"oss"`
	} `yaml:"compliance"`

	// 自動回測配置
	AutoBacktest struct {
		Enabled               bool                 `yaml:"enabled"`                 // 是否啟用自動回測，預設 false
		ScheduleIntervalHours int                  `yaml:"schedule_interval_hours"` // 調度間隔（小時），預設 6
		MaxConcurrentTasks    int                  `yaml:"max_concurrent_tasks"`    // 最大並行任務數，預設 3
		DefaultCapital        float64              `yaml:"default_capital"`         // 預設回測資金，預設 10000
		Symbols               []AutoBacktestSymbol `yaml:"symbols"`                 // 要自動回測的交易對和策略
	} `yaml:"auto_backtest"`
}

// AutoBacktestSymbol 自動回測交易對配置
type AutoBacktestSymbol struct {
	Symbol     string   `yaml:"symbol" json:"symbol"`           // 交易對，如 BTCUSDT
	Exchange   string   `yaml:"exchange" json:"exchange"`       // 交易所，如 binance
	MarketType string   `yaml:"market_type" json:"market_type"` // 市場類型，spot 或 futures
	Strategies []string `yaml:"strategies" json:"strategies"`   // 要回測的策略列表，如 ["grid", "dca"]
}

// WithdrawalPolicy 提現策略（利润保护）
type WithdrawalPolicy struct {
	Enabled   bool    `yaml:"enabled" json:"enabled"`
	Threshold float64 `yaml:"threshold" json:"threshold"` // 触发提現的利润比例 (如 0.1 表示 10%)

	// ===== 划轉模式 =====
	Mode string `yaml:"mode" json:"mode"` // threshold(阈值触发), fixed(固定金額), tiered(阶梯), scheduled(定時)

	// ===== 固定金額模式 =====
	FixedAmount float64 `yaml:"fixed_amount" json:"fixed_amount"` // 每次划轉的固定金額 (USDT)

	// ===== 阶梯划轉模式 =====
	TieredRules []TieredWithdrawRule `yaml:"tiered_rules" json:"tiered_rules"` // 阶梯划轉规则

	// ===== 划轉比例 =====
	WithdrawRatio float64 `yaml:"withdraw_ratio" json:"withdraw_ratio"` // 划轉比例 (0-1)，如 0.5 表示划轉利润的 50%

	// ===== 本金保护 =====
	PrincipalProtection PrincipalProtection `yaml:"principal_protection" json:"principal_protection"`

	// ===== 定時划轉 =====
	Schedule WithdrawSchedule `yaml:"schedule" json:"schedule"`

	// ===== 複利設置 =====
	CompoundRatio float64 `yaml:"compound_ratio" json:"compound_ratio"` // 複利比例 (0-1)，剩餘部分划轉

	// ===== 目標账戶 =====
	TargetWallet string `yaml:"target_wallet" json:"target_wallet"` // spot(現貨), funding(资金账戶), external(外部地址)
}

// TieredWithdrawRule 阶梯划轉规则
type TieredWithdrawRule struct {
	ProfitThreshold float64 `yaml:"profit_threshold" json:"profit_threshold"` // 利润阈值 (如 0.1 表示 10%)
	WithdrawRatio   float64 `yaml:"withdraw_ratio" json:"withdraw_ratio"`     // 达到該阈值時划轉的比例
}

// PrincipalProtection 本金保护設置
type PrincipalProtection struct {
	Enabled             bool    `yaml:"enabled" json:"enabled"`
	BreakevenProtection bool    `yaml:"breakeven_protection" json:"breakeven_protection"`   // 回本即保护（設置保本止损）
	WithdrawPrincipal   bool    `yaml:"withdraw_principal" json:"withdraw_principal"`       // 盈利足够時划轉本金
	PrincipalWithdrawAt float64 `yaml:"principal_withdraw_at" json:"principal_withdraw_at"` // 利润达到多少時划轉本金 (如 1.0 表示利润=本金時)
	MaxLossRatio        float64 `yaml:"max_loss_ratio" json:"max_loss_ratio"`               // 最大亏损比例 (如 0.2 表示最多亏损本金的 20%)
}

// WithdrawSchedule 定時划轉設置
type WithdrawSchedule struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Frequency  string `yaml:"frequency" json:"frequency"`       // daily, weekly, monthly
	DayOfWeek  int    `yaml:"day_of_week" json:"day_of_week"`   // 周几 (1-7, 僅 weekly 模式)
	DayOfMonth int    `yaml:"day_of_month" json:"day_of_month"` // 每月几号 (1-31, 僅 monthly 模式)
	TimeOfDay  string `yaml:"time_of_day" json:"time_of_day"`   // 時间 (如 "23:00")
}

// StrategyInstance 币种下的策略實例
type StrategyInstance struct {
	Type   string                 `yaml:"type" json:"type"`     // grid, dca, etc.
	Weight float64                `yaml:"weight" json:"weight"` // 资金占比 (0-1)
	Config map[string]interface{} `yaml:"config" json:"config"` // 策略专属配置
}

// ProfileConfig 配置档案（用于多套配置自动切换）
type ProfileConfig struct {
	PriceInterval     float64 `yaml:"price_interval" json:"price_interval"`         // 價格間隔
	OrderQuantity     float64 `yaml:"order_quantity" json:"order_quantity"`       // 每單金額（USDT/USDC）
	BuyWindowSize     int     `yaml:"buy_window_size" json:"buy_window_size"`     // 買單窗口
	SellWindowSize    int     `yaml:"sell_window_size" json:"sell_window_size"`  // 賣單視窗
	MinOrderValue     float64 `yaml:"min_order_value,omitempty" json:"min_order_value,omitempty"` // 最小訂單價值（可選，繼承主配置）
}

// SwitchRules 切换规则（根据资金费率和手续费率自动切换配置档案）
type SwitchRules struct {
	FundingRate struct {
		Threshold float64 `yaml:"threshold" json:"threshold"` // 资金费率阈值，超过此值切换到对应 profile
	} `yaml:"funding_rate" json:"funding_rate"`
	FeeRate struct {
		Threshold float64 `yaml:"threshold" json:"threshold"` // 手续费率阈值，超过此值切换到对应 profile
	} `yaml:"fee_rate" json:"fee_rate"`
	CooldownSeconds int `yaml:"cooldown_seconds" json:"cooldown_seconds"` // 切换冷却时间（秒），避免频繁切换，預設 300
}

// SymbolConfig 單個交易對配置（可指定所属交易所及交易参數）
type SymbolConfig struct {
	Enabled               *bool              `yaml:"enabled" json:"enabled"`                                   // 是否啟用自动交易，預設為 true（使用指針确保 false 時也會被序列化）
	Exchange              string             `yaml:"exchange" json:"exchange"`                                 // 所属交易所，預設為 app.current_exchange
	Symbol                string             `yaml:"symbol" json:"symbol"`                                     // 交易對，如 BTCUSDT
	MarketType            string             `yaml:"market_type" json:"market_type"`                           // 市场類型：spot 現貨 / futures 合約，預設 futures
	TotalAllocatedCapital float64            `yaml:"total_allocated_capital" json:"total_allocated_capital"`   // 該幣種分配的總资金
	Strategies            []StrategyInstance `yaml:"strategies" json:"strategies"`                             // 运行在該幣種上的策略列表
	WithdrawalPolicy      WithdrawalPolicy   `yaml:"withdrawal_policy" json:"withdrawal_policy"`               // 提現策略
	PriceInterval         float64            `yaml:"price_interval" json:"price_interval"`                     // 價格間隔（主配置，未配置 profiles 时使用）
	OrderQuantity         float64            `yaml:"order_quantity" json:"order_quantity"`                     // 每單金額（USDT/USDC）（主配置，未配置 profiles 时使用）
	MinOrderValue         float64            `yaml:"min_order_value" json:"min_order_value"`                   // 最小訂單價值
	BuyWindowSize         int                `yaml:"buy_window_size" json:"buy_window_size"`                   // 買單窗口（主配置，未配置 profiles 时使用）
	SellWindowSize        int                `yaml:"sell_window_size" json:"sell_window_size"`                 // 賣單視窗（主配置，未配置 profiles 时使用）
	ReconcileInterval     int                `yaml:"reconcile_interval" json:"reconcile_interval"`             // 對账间隔（秒）
	OrderCleanupThreshold int                `yaml:"order_cleanup_threshold" json:"order_cleanup_threshold"`   // 訂單清理上限
	CleanupBatchSize      int                `yaml:"cleanup_batch_size" json:"cleanup_batch_size"`             // 清理批次大小
	MarginLockDurationSec int                `yaml:"margin_lock_duration_seconds" json:"margin_lock_duration"` // 保證金鎖定時间（秒）
	PositionSafetyCheck   int                `yaml:"position_safety_check" json:"position_safety_check"`       // 持倉安全性檢查
	GridRiskControl       GridRiskControl    `yaml:"grid_risk_control" json:"grid_risk_control"`               // 網格策略风控
	Direction             string             `yaml:"direction" json:"direction"`                               // 交易方向：LONG 做多 / SHORT 做空，預設 LONG
	// 多套配置自动切换
	Profiles              map[string]ProfileConfig `yaml:"profiles,omitempty" json:"profiles,omitempty"`       // 配置档案（如 positive, negative）
	SwitchRules           SwitchRules              `yaml:"switch_rules,omitempty" json:"switch_rules,omitempty"` // 切换规则
}

// IsEnabled 返回交易對是否啟用（nil 預設為 true）
func (sc *SymbolConfig) IsEnabled() bool {
	if sc.Enabled == nil {
		return true
	}
	return *sc.Enabled
}

// GetMarketType 返回市場類型，空時預設為 futures（向后兼容）
func (sc *SymbolConfig) GetMarketType() string {
	if sc.MarketType == "spot" {
		return "spot"
	}
	return "futures"
}

// IsSpot 是否為現貨交易對
func (sc *SymbolConfig) IsSpot() bool {
	return sc.GetMarketType() == "spot"
}

// GetDirection 返回交易方向，空時預設 LONG（向后兼容）
func (sc *SymbolConfig) GetDirection() string {
	if sc.Direction == "SHORT" {
		return "SHORT"
	}
	return "LONG"
}

// SetEnabled 設置交易對啟用狀態
func (sc *SymbolConfig) SetEnabled(enabled bool) {
	sc.Enabled = &enabled
}

// UnmarshalYAML 為 SymbolConfig 提供預設值。
//
// 相容歷史配置：舊版 config.yaml 沒有 enabled 欄位時，應預設啟用自動交易（enabled=true）。
func (sc *SymbolConfig) UnmarshalYAML(value *yaml.Node) error {
	type raw SymbolConfig
	// 預設啟用
	defaultEnabled := true
	r := raw{Enabled: &defaultEnabled}
	if err := value.Decode(&r); err != nil {
		return err
	}
	// 如果 YAML 中没有 enabled 欄位，r.Enabled 仍然是我们設置的預設值
	*sc = SymbolConfig(r)
	return nil
}

// StrategyConfig 策略配置
type StrategyConfig struct {
	Enabled bool                   `yaml:"enabled" json:"enabled"`
	Type    string                 `yaml:"type" json:"type"`     // 策略類型 (grid, dca, martingale, dca_enhanced, combo)
	Weight  float64                `yaml:"weight" json:"weight"` // 资金权重
	Config  map[string]interface{} `yaml:"config" json:"config"`
}

// ExchangeConfig 交易所配置
type ExchangeConfig struct {
	APIKey     string  `yaml:"api_key" json:"api_key"`
	SecretKey  string  `yaml:"secret_key" json:"secret_key"`
	Passphrase string  `yaml:"passphrase" json:"passphrase"` // Bitget 需要
	FeeRate    float64 `yaml:"fee_rate" json:"fee_rate"`     // 手续费率（例如 0.0002 表示 0.02%）
	Testnet    bool    `yaml:"testnet" json:"testnet"`       // 是否使用測試網（預設 false）
	Leverage   int     `yaml:"leverage" json:"leverage"`     // 杠杆倍數（僅 Gate.io 支援，0 表示不設置）
}

// SymbolAllocation 單個币种的资金分配配置
type SymbolAllocation struct {
	Exchange      string  `yaml:"exchange"`
	Symbol        string  `yaml:"symbol"`
	MaxAmountUSDT float64 `yaml:"max_amount_usdt"` // 固定金額限制（正常限額）
	MaxPercentage float64 `yaml:"max_percentage"`  // 账戶餘額百分比限制

	// 分级限額配置
	TieredLimits struct {
		Enabled        bool    `yaml:"enabled"`         // 是否啟用分级限額
		EmergencyLimit float64 `yaml:"emergency_limit"` // 紧急限額（USDT）

		// 触发条件（满足任一条件即触发紧急限額）
		Triggers struct {
			PriceDropPercent  float64 `yaml:"price_drop_percent"`  // 價格下跌百分比（相對於锚点價格）
			PositionLayers    int     `yaml:"position_layers"`     // 持倉层數达到此值
			UnrealizedLossUSD float64 `yaml:"unrealized_loss_usd"` // 未實現亏损超過此值（USDT）
		} `yaml:"triggers"`

		// 恢復条件（满足所有条件才恢復正常限額）
		Recovery struct {
			PriceRecoverPercent float64 `yaml:"price_recover_percent"` // 價格恢復到此百分比以上
			CooldownSeconds     int     `yaml:"cooldown_seconds"`      // 冷却時间（秒）
		} `yaml:"recovery"`

		// 通知配置
		Notification struct {
			OnTrigger  bool `yaml:"on_trigger"`  // 触发紧急限額時通知
			OnRecovery bool `yaml:"on_recovery"` // 恢復正常限額時通知
		} `yaml:"notification"`
	} `yaml:"tiered_limits"`
}

// LoadConfig 加載配置文件
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 自动解密敏感字段（支持明文和密文）
	// DecryptAPIKey 函数会自动检测：如果有加密前缀则解密，否则返回原字符串（明文）
	// 尝试加载主密钥，如果不存在则跳过解密（明文模式）
	masterKey, _ := LoadMasterKey("")

	// 解密交易所配置中的敏感字段
	for name, exchangeCfg := range cfg.Exchanges {
		if exchangeCfg.APIKey != "" {
			// 如果检测到加密前缀但主密钥不存在，直接返回错误
			if IsEncrypted(exchangeCfg.APIKey) && masterKey == nil {
				return nil, fmt.Errorf("检测到加密的 API Key（交易所 %s），但主密钥不存在（请设置 %s 环境变量或创建 %s 文件）", name, MasterKeyEnvVar, DefaultMasterKeyPath)
			}
			// 如果有主密钥或明文，尝试解密（DecryptAPIKey 会自动处理明文）
			if masterKey != nil {
				decrypted, err := DecryptAPIKey(exchangeCfg.APIKey, masterKey)
				if err != nil {
					return nil, fmt.Errorf("解密交易所 %s 的 API Key 失败: %v（请检查主密钥是否正确）", name, err)
				}
				exchangeCfg.APIKey = decrypted
			}
			// 如果主密钥不存在且是明文，直接使用（无需解密）
		}
		if exchangeCfg.SecretKey != "" {
			if IsEncrypted(exchangeCfg.SecretKey) && masterKey == nil {
				return nil, fmt.Errorf("检测到加密的 Secret Key（交易所 %s），但主密钥不存在（请设置 %s 环境变量或创建 %s 文件）", name, MasterKeyEnvVar, DefaultMasterKeyPath)
			}
			if masterKey != nil {
				decrypted, err := DecryptAPIKey(exchangeCfg.SecretKey, masterKey)
				if err != nil {
					return nil, fmt.Errorf("解密交易所 %s 的 Secret Key 失败: %v（请检查主密钥是否正确）", name, err)
				}
				exchangeCfg.SecretKey = decrypted
			}
		}
		if exchangeCfg.Passphrase != "" {
			if IsEncrypted(exchangeCfg.Passphrase) && masterKey == nil {
				return nil, fmt.Errorf("检测到加密的 Passphrase（交易所 %s），但主密钥不存在（请设置 %s 环境变量或创建 %s 文件）", name, MasterKeyEnvVar, DefaultMasterKeyPath)
			}
			if masterKey != nil {
				decrypted, err := DecryptAPIKey(exchangeCfg.Passphrase, masterKey)
				if err != nil {
					return nil, fmt.Errorf("解密交易所 %s 的 Passphrase 失败: %v（请检查主密钥是否正确）", name, err)
				}
				exchangeCfg.Passphrase = decrypted
			}
		}
		cfg.Exchanges[name] = exchangeCfg
	}

	// 解密 AI 配置中的敏感字段
	if cfg.AI.APIKey != "" {
		if IsEncrypted(cfg.AI.APIKey) && masterKey == nil {
			return nil, fmt.Errorf("检测到加密的 AI API Key，但主密钥不存在（请设置 %s 环境变量或创建 %s 文件）", MasterKeyEnvVar, DefaultMasterKeyPath)
		}
		if masterKey != nil {
			decrypted, err := DecryptAPIKey(cfg.AI.APIKey, masterKey)
			if err != nil {
				return nil, fmt.Errorf("解密 AI API Key 失败: %v（请检查主密钥是否正确）", err)
			}
			cfg.AI.APIKey = decrypted
		}
	}
	if cfg.AI.GeminiAPIKey != "" {
		if IsEncrypted(cfg.AI.GeminiAPIKey) && masterKey == nil {
			return nil, fmt.Errorf("检测到加密的 AI Gemini API Key，但主密钥不存在（请设置 %s 环境变量或创建 %s 文件）", MasterKeyEnvVar, DefaultMasterKeyPath)
		}
		if masterKey != nil {
			decrypted, err := DecryptAPIKey(cfg.AI.GeminiAPIKey, masterKey)
			if err != nil {
				return nil, fmt.Errorf("解密 AI Gemini API Key 失败: %v（请检查主密钥是否正确）", err)
			}
			cfg.AI.GeminiAPIKey = decrypted
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置驗证失败: %v", err)
	}

	return &cfg, nil
}

// LoadConfigFromBytes 從字节數组加載配置（用於测試）
func LoadConfigFromBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置驗证失败: %v", err)
	}

	return &cfg, nil
}

// SaveConfig 保存配置到文件
func SaveConfig(cfg *Config, configPath string) error {
	// 驗证配置
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("配置驗证失败: %v", err)
	}

	// 序列化為YAML
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

// SanitizeForExport 創建脱敏后的配置副本，用於導出下載（遮盖 API Key、Secret 等敏感信息）
func SanitizeForExport(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	out := *cfg
	// 深拷贝 Exchanges 並脱敏
	if len(cfg.Exchanges) > 0 {
		out.Exchanges = make(map[string]ExchangeConfig)
		for k, v := range cfg.Exchanges {
			ec := v
			if len(ec.APIKey) > 4 {
				ec.APIKey = ec.APIKey[:4] + "****"
			} else if ec.APIKey != "" {
				ec.APIKey = "****"
			}
			if ec.SecretKey != "" {
				ec.SecretKey = "****"
			}
			if ec.Passphrase != "" {
				ec.Passphrase = "****"
			}
			out.Exchanges[k] = ec
		}
	}
	// AI 配置脱敏
	if cfg.AI.APIKey != "" {
		out.AI.APIKey = "****"
	}
	if cfg.AI.GeminiAPIKey != "" {
		out.AI.GeminiAPIKey = "****"
	}
	return &out
}

// SaveConfigWithoutValidation 保存配置到文件（不驗证，用於保存最小化配置）
func SaveConfigWithoutValidation(cfg *Config, configPath string) error {
	// 序列化為YAML
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

// CreateMinimalConfig 創建最小化配置（僅用於啟动 Web 服務）
func CreateMinimalConfig() *Config {
	cfg := &Config{}

	// 應用配置
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
	cfg.System.LogRetentionDays = 30 // 預設保留30天

	// Web 服務配置（啟用）
	cfg.Web.Enabled = true
	cfg.Web.Host = "0.0.0.0"
	cfg.Web.Port = 28888
	cfg.Web.APIKey = ""

	// 其他配置使用預設值
	cfg.RiskControl.Enabled = true
	cfg.RiskControl.Interval = "1m"
	cfg.RiskControl.VolumeMultiplier = 3.0
	cfg.RiskControl.AverageWindow = 20
	cfg.RiskControl.RecoveryThreshold = 3
	cfg.RiskControl.MaxLeverage = 10
	cfg.RiskControl.MonitorSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}

	// 資金費率監控預設值
	cfg.FundingRate.Enabled = false // 預設關閉，需要手動啟用
	cfg.FundingRate.MonitorInterval = 60
	cfg.FundingRate.AlertThreshold = 0.001 // 0.1%
	cfg.FundingRate.BiasEnabled = false
	cfg.FundingRate.HighRateThreshold = 0.001  // 0.1%
	cfg.FundingRate.PauseBuyThreshold = 0.0015 // 0.15%
	cfg.FundingRate.ArbitrageEnabled = false
	cfg.FundingRate.HedgeMinPosition = 100     // 100 USDT
	cfg.FundingRate.HedgeRateThreshold = 0.001 // 0.1%
	cfg.FundingRate.MaxSpreadPercent = 0.5     // 0.5%

	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = "./data/quantmesh.db"
	cfg.Storage.BufferSize = 1000
	cfg.Storage.BatchSize = 100
	cfg.Storage.FlushInterval = 5

	cfg.Notifications.Enabled = false
	cfg.Notifications.Webhook.Timeout = 3
	cfg.Notifications.Email.Provider = "smtp"
	cfg.Notifications.Rules.InspectorReport = true

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

	cfg.Inspector.Enabled = false
	cfg.Inspector.Name = "智子巡檢"
	cfg.Inspector.Schedule.RegularInterval = "1h"
	cfg.Inspector.Schedule.QuietHoursStart = 23
	cfg.Inspector.Schedule.QuietHoursEnd = 7
	cfg.Inspector.Schedule.QuietInterval = "4h"
	cfg.Inspector.Thresholds.PnLAlert = 100
	cfg.Inspector.Thresholds.RiskScoreChange = 20
	cfg.Inspector.Thresholds.FundingRateAlert = 0.001
	cfg.Inspector.Thresholds.CorrelationChange = 0.2
	cfg.Inspector.Thresholds.BalanceChangePct = 5
	cfg.Inspector.AI.Provider = "gemini"
	cfg.Inspector.AI.AnalysisDepth = "detailed"
	cfg.Inspector.Report.Format = "markdown"
	cfg.Inspector.Report.IncludeAIInsights = true
	cfg.Inspector.Report.MaxNewsItems = 5

	cfg.AI.Enabled = false

	// 設置數據库配置預設值（AI 异步任務需要數據库支援）
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = "./data/quantmesh.db"
	cfg.Database.MaxOpenConns = 100
	cfg.Database.MaxIdleConns = 10
	cfg.Database.ConnMaxLifetime = 3600
	cfg.Database.LogLevel = "error"
	cfg.AI.Provider = "gemini"
	cfg.AI.DecisionMode = "hybrid"
	cfg.AI.ExecutionRules.HighRiskThreshold = 0.8
	cfg.AI.ExecutionRules.LowRiskThreshold = 0.3
	cfg.AI.ExecutionRules.RequireConfirmation = true

	cfg.Strategies.Enabled = false
	cfg.Strategies.CapitalAllocation.Mode = "fixed"
	cfg.Strategies.CapitalAllocation.TotalCapital = 5000

	// 時间间隔配置
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

	// 合规配置預設值
	cfg.Compliance.Enabled = false
	cfg.Compliance.AuditLog.Enabled = false
	cfg.Compliance.AuditLog.Format = "both"
	cfg.Compliance.AuditLog.Directory = "./data/audit"
	cfg.Compliance.OSS.Enabled = false
	cfg.Compliance.OSS.Provider = "aliyun"
	cfg.Compliance.OSS.Prefix = "audit/"
	cfg.Compliance.OSS.UploadTime = "02:00"

	return cfg
}

// SetupData 引導配置數據
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

// CreateConfigFromSetup 從引導數據創建完整配置
func CreateConfigFromSetup(setup *SetupData) (*Config, error) {
	// 創建最小化配置作為基础
	cfg := CreateMinimalConfig()

	// 設置交易所
	cfg.App.CurrentExchange = setup.Exchange

	// 設置交易所配置
	exchangeCfg := ExchangeConfig{
		APIKey:     setup.APIKey,
		SecretKey:  setup.SecretKey,
		Passphrase: setup.Passphrase,
		Testnet:    setup.Testnet,
		FeeRate:    setup.FeeRate,
	}

	// 如果手续费率未設置，使用預設值
	if exchangeCfg.FeeRate <= 0 {
		exchangeCfg.FeeRate = 0.0002
	}

	cfg.Exchanges[setup.Exchange] = exchangeCfg

	// 設置交易配置
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

	// 設置預設值
	if cfg.Trading.SellWindowSize <= 0 {
		cfg.Trading.SellWindowSize = cfg.Trading.BuyWindowSize
	}

	// 創建交易對配置
	cfg.Trading.Symbols = []SymbolConfig{
		{
			Enabled:               BoolPtr(true),
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

// Validate 驗证配置
func (c *Config) Validate() error {
	// 驗证交易所配置
	if c.App.CurrentExchange == "" {
		return fmt.Errorf("必須指定當前使用的交易所 (app.current_exchange)")
	}

	// 驗证多交易所配置
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

	// 驗证手续费率配置
	if exchangeCfg.FeeRate < 0 {
		return fmt.Errorf("交易所 %s 的手续费率不能為负數", c.App.CurrentExchange)
	}

	// ==== 多交易對配置校驗（相容舊配置）====
	normalizeSymbol := func(sc SymbolConfig) (SymbolConfig, error) {
		// enabled 預設值（避免 nil 導致保存時出現 enabled: null）
		if sc.Enabled == nil {
			sc.SetEnabled(true)
		}

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
			return sc, fmt.Errorf("交易所 %s 的手续费率不能為负數", sc.Exchange)
		}

		// 交易對
		if sc.Symbol == "" {
			return sc, fmt.Errorf("交易對不能為空")
		}

		// 市场類型：僅允許 spot 或 futures，空時預設為 futures
		if sc.MarketType == "" {
			sc.MarketType = "futures"
		}
		if sc.MarketType != "spot" && sc.MarketType != "futures" {
			return sc, fmt.Errorf("交易對 %s 的 market_type 無效: %s（只支援 spot 或 futures）", sc.Symbol, sc.MarketType)
		}

		// 數值預設
		if sc.PriceInterval <= 0 {
			sc.PriceInterval = c.Trading.PriceInterval
		}
		if sc.PriceInterval <= 0 {
			return sc, fmt.Errorf("交易對 %s 的價格間隔必須大於0", sc.Symbol)
		}

		if sc.OrderQuantity <= 0 {
			sc.OrderQuantity = c.Trading.OrderQuantity
		}
		if sc.OrderQuantity <= 0 {
			return sc, fmt.Errorf("交易對 %s 的订單金額必須大於0", sc.Symbol)
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
			return sc, fmt.Errorf("交易對 %s 的買單窗口大小必須大於0", sc.Symbol)
		}

		if sc.SellWindowSize <= 0 {
			if c.Trading.SellWindowSize > 0 {
				sc.SellWindowSize = c.Trading.SellWindowSize
			} else {
				sc.SellWindowSize = sc.BuyWindowSize
			}
		}

		// 方向：空時預設 LONG
		if sc.Direction == "" {
			if c.Trading.Direction == "SHORT" {
				sc.Direction = "SHORT"
			} else {
				sc.Direction = "LONG"
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

		// 風控配置继承
		if !sc.GridRiskControl.Enabled && c.Trading.GridRiskControl.Enabled {
			sc.GridRiskControl = c.Trading.GridRiskControl
		} else if sc.GridRiskControl.Enabled {
			// 如果啟用了但某些欄位没填，可以考虑從全局继承，但通常啟用表示要自定义
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

		// 驗证策略占比
		if len(sc.Strategies) > 0 {
			var totalWeight float64
			for _, s := range sc.Strategies {
				totalWeight += s.Weight
			}
			if totalWeight > 1.001 { // 允許微小误差
				return sc, fmt.Errorf("交易對 %s 的策略权重總和 (%.2f) 不能超過 1.0", sc.Symbol, totalWeight)
			}
		}

		return sc, nil
	}

	// 若未配置 symbols，则相容舊配置轉换為單元素
	if len(c.Trading.Symbols) == 0 {
		if c.Trading.Symbol == "" {
			return fmt.Errorf("交易對不能為空")
		}
		direction := c.Trading.Direction
		if direction != "SHORT" {
			direction = "LONG"
		}
		c.Trading.Symbols = []SymbolConfig{{
			Enabled:               BoolPtr(true),
			Exchange:              c.App.CurrentExchange,
			Symbol:                c.Trading.Symbol,
			PriceInterval:         c.Trading.PriceInterval,
			OrderQuantity:         c.Trading.OrderQuantity,
			MinOrderValue:         c.Trading.MinOrderValue,
			BuyWindowSize:         c.Trading.BuyWindowSize,
			SellWindowSize:        c.Trading.SellWindowSize,
			Direction:             direction,
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

	// 兼容舊欄位：保持首個交易對到舊欄位，供未改造代碼使用
	// 若用戶已在 trading 頂層設置 position_safety_check，則不讓首個交易對覆蓋（避免 Web 表單默認 100 覆蓋用戶改的 30）
	tradingPositionSafetyCheckSet := c.Trading.PositionSafetyCheck > 0
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
		if !tradingPositionSafetyCheckSet {
			c.Trading.PositionSafetyCheck = primary.PositionSafetyCheck
		}
		c.Trading.Direction = primary.GetDirection()
		c.Trading.GridRiskControl = primary.GridRiskControl
	}

	// 設置預設時间间隔
	if c.System.Timezone == "" {
		c.System.Timezone = "Asia/Shanghai" // 預設东8区
	}
	if c.System.LogRetentionDays <= 0 {
		c.System.LogRetentionDays = 30 // 預設保留30天
	}

	if c.Timing.WebSocketReconnectDelay <= 0 {
		c.Timing.WebSocketReconnectDelay = 5 // 預設5秒
	}
	if c.Timing.WebSocketWriteWait <= 0 {
		c.Timing.WebSocketWriteWait = 10 // 預設 10秒
	}
	if c.Timing.WebSocketPongWait <= 0 {
		c.Timing.WebSocketPongWait = 60 // 預設60秒
	}
	if c.Timing.WebSocketPingInterval <= 0 {
		c.Timing.WebSocketPingInterval = 20 // 預設20秒
	}
	if c.Timing.ListenKeyKeepAliveInterval <= 0 {
		c.Timing.ListenKeyKeepAliveInterval = 30 // 預設30分钟
	}
	if c.Timing.PriceSendInterval <= 0 {
		c.Timing.PriceSendInterval = 50 // 預設50毫秒
	}
	if c.Timing.RateLimitRetryDelay <= 0 {
		c.Timing.RateLimitRetryDelay = 1 // 預設1秒
	}
	if c.Timing.OrderRetryDelay <= 0 {
		c.Timing.OrderRetryDelay = 500 // 預設500毫秒
	}
	if c.Timing.PricePollInterval <= 0 {
		c.Timing.PricePollInterval = 500 // 預設500毫秒
	}
	if c.Timing.StatusPrintInterval <= 0 {
		c.Timing.StatusPrintInterval = 1 // 預設1分钟
	}
	if c.Timing.OrderCleanupInterval <= 0 {
		c.Timing.OrderCleanupInterval = 60 // 預設60秒
	}

	// 驗证風控配置並設置預設值
	if c.RiskControl.Interval == "" {
		c.RiskControl.Interval = "1m" // 預設1分钟
	}
	if c.RiskControl.VolumeMultiplier <= 0 {
		c.RiskControl.VolumeMultiplier = 3.0 // 預設3倍
	}
	if c.RiskControl.AverageWindow <= 0 {
		c.RiskControl.AverageWindow = 20 // 預設20根K線
	}
	if len(c.RiskControl.MonitorSymbols) == 0 {
		c.RiskControl.MonitorSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	}

	// 驗证恢復阈值配置
	monitorCount := len(c.RiskControl.MonitorSymbols)
	if c.RiskControl.RecoveryThreshold <= 0 {
		c.RiskControl.RecoveryThreshold = 3 // 預設3個币种
	} else if c.RiskControl.RecoveryThreshold < 1 {
		c.RiskControl.RecoveryThreshold = 1 // 最小1個
	} else if c.RiskControl.RecoveryThreshold > monitorCount {
		c.RiskControl.RecoveryThreshold = monitorCount // 最大為監控币种數量
	}

	// 設置深度監控配置預設值
	if c.RiskControl.DepthMonitor.CheckInterval <= 0 {
		c.RiskControl.DepthMonitor.CheckInterval = 5 // 預設5秒
	}
	if c.RiskControl.DepthMonitor.DepthLevels <= 0 {
		c.RiskControl.DepthMonitor.DepthLevels = 10 // 預設 10檔
	}
	if c.RiskControl.DepthMonitor.DropThreshold <= 0 {
		c.RiskControl.DepthMonitor.DropThreshold = 0.5 // 預設50%
	}
	if c.RiskControl.DepthMonitor.RecoveryThreshold <= 0 {
		c.RiskControl.DepthMonitor.RecoveryThreshold = 0.7 // 預設70%
	}
	if c.RiskControl.DepthMonitor.MinDepthUSDT <= 0 {
		c.RiskControl.DepthMonitor.MinDepthUSDT = 10000 // 預設 10000 USDT
	}

	// 複合風控閾值與因子權重預設值
	if c.CompositeRisk.Thresholds.Caution <= 0 {
		c.CompositeRisk.Thresholds.Caution = 25
	}
	if c.CompositeRisk.Thresholds.ReducePosition <= 0 {
		c.CompositeRisk.Thresholds.ReducePosition = 45
	}
	if c.CompositeRisk.Thresholds.PauseBuying <= 0 {
		c.CompositeRisk.Thresholds.PauseBuying = 65
	}
	if c.CompositeRisk.Thresholds.StopTrading <= 0 {
		c.CompositeRisk.Thresholds.StopTrading = 80
	}
	if c.CompositeRisk.EvaluateInterval <= 0 {
		c.CompositeRisk.EvaluateInterval = 30
	}
	if c.CompositeRisk.Factors.News.Weight <= 0 {
		c.CompositeRisk.Factors.News.Weight = 0.30
	}
	if c.CompositeRisk.Factors.Trend.Weight <= 0 {
		c.CompositeRisk.Factors.Trend.Weight = 0.25
	}
	if c.CompositeRisk.Factors.FundingRate.Weight <= 0 {
		c.CompositeRisk.Factors.FundingRate.Weight = 0.20
	}
	if c.CompositeRisk.Factors.Depth.Weight <= 0 {
		c.CompositeRisk.Factors.Depth.Weight = 0.10
	}
	if c.CompositeRisk.Factors.Kline.Weight <= 0 {
		c.CompositeRisk.Factors.Kline.Weight = 0.15
	}
	if c.CompositeRisk.Factors.FundingRate.ConsecutiveNegativePeriods <= 0 {
		c.CompositeRisk.Factors.FundingRate.ConsecutiveNegativePeriods = 3
	}

	// 設置資金費率監控預設值
	if c.FundingRate.MonitorInterval <= 0 {
		c.FundingRate.MonitorInterval = 60 // 預設 60 秒
	}
	if c.FundingRate.AlertThreshold <= 0 {
		c.FundingRate.AlertThreshold = 0.001 // 預設 0.1%
	}
	if c.FundingRate.HighRateThreshold <= 0 {
		c.FundingRate.HighRateThreshold = 0.001 // 預設 0.1%
	}
	if c.FundingRate.PauseBuyThreshold <= 0 {
		c.FundingRate.PauseBuyThreshold = 0.0015 // 預設 0.15%
	}
	if c.FundingRate.HedgeMinPosition <= 0 {
		c.FundingRate.HedgeMinPosition = 100 // 預設 100 USDT
	}
	if c.FundingRate.HedgeRateThreshold <= 0 {
		c.FundingRate.HedgeRateThreshold = 0.001 // 預設 0.1%
	}
	if c.FundingRate.MaxSpreadPercent <= 0 {
		c.FundingRate.MaxSpreadPercent = 0.5 // 預設 0.5%
	}

	// 設置通知配置預設值
	if c.Notifications.Webhook.Timeout <= 0 {
		c.Notifications.Webhook.Timeout = 3 // 預設3秒
	}
	if c.Notifications.Email.Provider == "" {
		c.Notifications.Email.Provider = "smtp" // 預設SMTP
	}

	// 設置存儲配置預設值
	if c.Storage.Type == "" {
		c.Storage.Type = "sqlite" // 預設SQLite
	}
	if c.Storage.Path == "" {
		c.Storage.Path = "./data/quantmesh.db" // 預設路径
	}
	// 若已配置路徑與類型，則視為啟用存儲（避免僅填路徑卻未勾選 enabled 導致回測等不可用）
	if c.Storage.Path != "" && c.Storage.Type != "" {
		c.Storage.Enabled = true
	}
	if c.Storage.BufferSize <= 0 {
		c.Storage.BufferSize = 1000 // 預設 1000
	}
	if c.Storage.BatchSize <= 0 {
		c.Storage.BatchSize = 100 // 預設 100
	}
	if c.Storage.FlushInterval <= 0 {
		c.Storage.FlushInterval = 5 // 預設5秒
	}

	// 統一 SQLite 路徑：Database 與 Storage 同時使用 sqlite 時，以 database.dsn 為準，避免冗餘配置導致雙文件
	if c.Database.Type == "sqlite" && c.Database.DSN != "" && c.Storage.Enabled && c.Storage.Type == "sqlite" {
		c.Storage.Path = c.Database.DSN
	}

	// 設置 Web 服務配置預設值
	if c.Web.Host == "" {
		c.Web.Host = "0.0.0.0" // 預設監听所有地址
	}
	if c.Web.Port <= 0 {
		c.Web.Port = 28888 // 預設端口（使用10000以上端口，避免常见端口冲突）
	}

	// 設置 pprof 配置預設值
	if len(c.Web.Pprof.AllowedIPs) == 0 {
		// 預設允許本地访问
		c.Web.Pprof.AllowedIPs = []string{"127.0.0.1", "::1"}
	}
	// pprof.Enabled 預設為 false（生產环境安全）
	// pprof.RequireAuth 預設為 true（需要认证）

	// 設置實例配置預設值
	if c.Instance.ID == "" {
		c.Instance.ID = "default-instance" // 預設實例ID
	}
	if c.Instance.Total <= 0 {
		c.Instance.Total = 1 // 預設單實例
	}

	// 設置數據库配置預設值
	if c.Database.Type == "" {
		c.Database.Type = "sqlite" // 預設 SQLite（單机模式）
	}
	if c.Database.DSN == "" {
		if c.Database.Type == "sqlite" {
			c.Database.DSN = "./data/quantmesh.db" // 預設 SQLite 路径
		}
	}
	if c.Database.MaxOpenConns <= 0 {
		c.Database.MaxOpenConns = 100 // 預設 100
	}
	if c.Database.MaxIdleConns <= 0 {
		c.Database.MaxIdleConns = 10 // 預設 10
	}
	if c.Database.ConnMaxLifetime <= 0 {
		c.Database.ConnMaxLifetime = 3600 // 預設1小時
	}
	if c.Database.LogLevel == "" {
		c.Database.LogLevel = "error" // 預設只記錄錯误
	}

	// 設置分布式鎖配置預設值
	// 注意：預設不啟用分布式鎖（單机模式）
	if c.DistributedLock.Type == "" {
		c.DistributedLock.Type = "redis" // 預設使用 Redis
	}
	if c.DistributedLock.Prefix == "" {
		c.DistributedLock.Prefix = "quantmesh:lock:" // 預設前缀
	}
	if c.DistributedLock.DefaultTTL <= 0 {
		c.DistributedLock.DefaultTTL = 5 // 預設5秒
	}
	if c.DistributedLock.Redis.Addr == "" {
		c.DistributedLock.Redis.Addr = "localhost:6379" // 預設 Redis 地址
	}
	if c.DistributedLock.Redis.PoolSize <= 0 {
		c.DistributedLock.Redis.PoolSize = 10 // 預設連接池大小
	}

	// 設置監控配置預設值
	if c.Metrics.CollectInterval <= 0 {
		c.Metrics.CollectInterval = 60 // 預設60秒
	}

	// 設置新聞監控配置預設值
	// 如果啟用了新聞監控且未明確設置 enable_analysis，默認啟用分析功能
	if c.NewsMonitor.Enabled && c.NewsMonitor.EnableAnalysis == nil {
		enableAnalysis := true
		c.NewsMonitor.EnableAnalysis = &enableAnalysis
	}
	if c.NewsMonitor.CheckInterval == "" {
		c.NewsMonitor.CheckInterval = "5m" // 預設5分钟
	}
	if c.NewsMonitor.AnalysisInterval == "" {
		c.NewsMonitor.AnalysisInterval = "30m" // Gemini分析间隔，預設30分钟
	}
	if c.NewsMonitor.NewsCollectInterval == "" {
		c.NewsMonitor.NewsCollectInterval = "5m" // NewsAPI收集间隔，預設5分钟
	}
	if len(c.NewsMonitor.Sources) == 0 {
		c.NewsMonitor.Sources = []string{"newsapi"} // 預設使用NewsAPI
	}
	if len(c.NewsMonitor.Keywords) == 0 {
		c.NewsMonitor.Keywords = DefaultNewsKeywords()
	}
	if c.NewsMonitor.RiskThreshold <= 0 {
		c.NewsMonitor.RiskThreshold = 70 // 預設风險阈值70
	}
	if len(c.NewsMonitor.PredictionTimeframes) == 0 {
		c.NewsMonitor.PredictionTimeframes = []string{"2h", "4h", "6h", "12h", "24h"}
	}
	if c.NewsMonitor.RiskThresholds.StopTradingProbability <= 0 {
		c.NewsMonitor.RiskThresholds.StopTradingProbability = 0.7
	}
	if c.NewsMonitor.RiskThresholds.ReducePositionProbability <= 0 {
		c.NewsMonitor.RiskThresholds.ReducePositionProbability = 0.5
	}
	if c.NewsMonitor.HistoryRetentionDays <= 0 {
		c.NewsMonitor.HistoryRetentionDays = 30
	}
	if len(c.NewsMonitor.Assets) == 0 {
		c.NewsMonitor.Assets = []AssetConfig{
			{AssetType: "crypto_btc", Symbol: "BTCUSDT", Keywords: DefaultNewsKeywords(), Enabled: true},
			{AssetType: "commodity_gold", Symbol: "PAXGUSDT", Keywords: DefaultGoldKeywords(), Enabled: true},
		}
	}
	// 設置 AI Provider 配置預設值
	if c.NewsMonitor.AIProvider.Provider == "" {
		c.NewsMonitor.AIProvider.Provider = "gemini" // 預設使用 Gemini
	}
	// 如果未配置 API Key，嘗試從全局 AI 配置繼承
	if c.NewsMonitor.AIProvider.APIKey == "" {
		if c.NewsMonitor.AIProvider.Provider == "gemini" {
			if c.AI.GeminiAPIKey != "" {
				c.NewsMonitor.AIProvider.APIKey = c.AI.GeminiAPIKey
			} else if c.AI.APIKey != "" {
				c.NewsMonitor.AIProvider.APIKey = c.AI.APIKey
			}
		} else if c.AI.APIKey != "" {
			c.NewsMonitor.AIProvider.APIKey = c.AI.APIKey
		}
	}

	// 設置智子巡檢配置預設值
	if c.Inspector.Name == "" {
		c.Inspector.Name = "智子巡檢"
	}
	if c.Inspector.Schedule.RegularInterval == "" {
		c.Inspector.Schedule.RegularInterval = "1h"
	}
	if c.Inspector.Schedule.QuietHoursStart == 0 && c.Inspector.Schedule.QuietHoursEnd == 0 {
		c.Inspector.Schedule.QuietHoursStart = 23
		c.Inspector.Schedule.QuietHoursEnd = 7
	}
	if c.Inspector.Schedule.QuietInterval == "" {
		c.Inspector.Schedule.QuietInterval = "4h"
	}
	if c.Inspector.Thresholds.PnLAlert <= 0 {
		c.Inspector.Thresholds.PnLAlert = 100
	}
	if c.Inspector.Thresholds.RiskScoreChange <= 0 {
		c.Inspector.Thresholds.RiskScoreChange = 20
	}
	if c.Inspector.Thresholds.FundingRateAlert <= 0 {
		c.Inspector.Thresholds.FundingRateAlert = 0.001
	}
	if c.Inspector.Thresholds.CorrelationChange <= 0 {
		c.Inspector.Thresholds.CorrelationChange = 0.2
	}
	if c.Inspector.Thresholds.BalanceChangePct <= 0 {
		c.Inspector.Thresholds.BalanceChangePct = 5
	}
	if c.Inspector.AI.Provider == "" {
		c.Inspector.AI.Provider = "gemini"
	}
	if c.Inspector.Report.Format == "" {
		c.Inspector.Report.Format = "markdown"
	}
	if c.Inspector.Report.MaxNewsItems <= 0 {
		c.Inspector.Report.MaxNewsItems = 5
	}

	// 設置策略配置預設值
	if c.Strategies.CapitalAllocation.Mode == "" {
		c.Strategies.CapitalAllocation.Mode = "fixed" // 預設固定分配
	}
	if c.Strategies.CapitalAllocation.TotalCapital <= 0 {
		c.Strategies.CapitalAllocation.TotalCapital = 5000 // 預設5000 USDT
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval = 3600 // 預設1小時
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance = 0.05 // 預設5%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MinWeight <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MinWeight = 0.1 // 預設 10%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight <= 0 {
		c.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight = 0.7 // 預設70%
	}
	if c.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights == nil {
		c.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights = map[string]float64{
			"total_pnl":    0.4,
			"sharpe_ratio": 0.3,
			"win_rate":     0.2,
			"max_drawdown": 0.1,
		}
	}

	// 設置事件中心配置預設值
	// 預設啟用事件中心
	if c.EventCenter.PriceVolatilityThreshold <= 0 {
		c.EventCenter.PriceVolatilityThreshold = 5.0 // 預設5%波动
	}
	if c.EventCenter.Retention.CriticalDays <= 0 {
		c.EventCenter.Retention.CriticalDays = 365 // Critical 事件保留1年
	}
	if c.EventCenter.Retention.WarningDays <= 0 {
		c.EventCenter.Retention.WarningDays = 90 // Warning 事件保留3個月
	}
	if c.EventCenter.Retention.InfoDays <= 0 {
		c.EventCenter.Retention.InfoDays = 30 // Info 事件保留1個月
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
		c.EventCenter.CleanupInterval = 24 // 預設每24小時清理一次
	}

	// 合规配置預設值
	if c.Compliance.AuditLog.Directory == "" {
		c.Compliance.AuditLog.Directory = "./data/audit"
	}
	if c.Compliance.AuditLog.Format == "" {
		c.Compliance.AuditLog.Format = "both" // csv, jsonl, both
	}
	if c.Compliance.OSS.Provider == "" {
		c.Compliance.OSS.Provider = "aliyun"
	}
	if c.Compliance.OSS.Prefix == "" {
		c.Compliance.OSS.Prefix = "audit/"
	}
	if c.Compliance.OSS.UploadTime == "" {
		c.Compliance.OSS.UploadTime = "02:00"
	}

	return nil
}
