package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// "quantmesh/ai" // AI 功能已迁移到商业插件
	"quantmesh/config"
	"quantmesh/database"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/i18n"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/metrics"
	"quantmesh/monitor"
	"quantmesh/notify"
	"quantmesh/order"
	"quantmesh/plugin"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

// Version 版本号
var Version = "3.3.3"

// 全局日志存储实例（用于清理任务和 WebSocket 推送）
var globalLogStorage *storage.LogStorage

// webAuthnLoggerAdapter WebAuthn 日志适配器
type webAuthnLoggerAdapter struct{}

func (w *webAuthnLoggerAdapter) Infof(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func (w *webAuthnLoggerAdapter) Warnf(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func (w *webAuthnLoggerAdapter) Errorf(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func (w *webAuthnLoggerAdapter) Debugf(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

// reconciliationStorageAdapter 对账存储适配器
type reconciliationStorageAdapter struct {
	storageService *storage.StorageService
}

func (a *reconciliationStorageAdapter) SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	return a.storageService.SaveReconciliationHistoryDirect(symbol, reconcileTime, localPosition, exchangePosition, positionDiff,
		activeBuyOrders, activeSellOrders, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit)
}

// AI适配器（用于Web API）
// 注意：AI 功能已迁移到商业插件，开源版不再包含
// 如需使用 AI 功能，请购买商业插件：https://quantmesh.io/plugins

/*
type aiMarketAdapter struct {
	analyzer *ai.MarketAnalyzer
}

func (a *aiMarketAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiMarketAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiMarketAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiParamAdapter struct {
	optimizer *ai.ParameterOptimizer
}

func (a *aiParamAdapter) GetLastOptimization() interface{} {
	return a.optimizer.GetLastOptimization()
}

func (a *aiParamAdapter) GetLastOptimizationTime() time.Time {
	return a.optimizer.GetLastOptimizationTime()
}

func (a *aiParamAdapter) PerformOptimization() error {
	return a.optimizer.TriggerOptimization()
}

type aiRiskAdapter struct {
	analyzer *ai.RiskAnalyzer
}

func (a *aiRiskAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiRiskAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiRiskAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiSentimentAdapter struct {
	analyzer *ai.SentimentAnalyzer
}

func (a *aiSentimentAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiSentimentAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiSentimentAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiPolymarketAdapter struct {
	analyzer *ai.PolymarketSignalAnalyzer
}

func (a *aiPolymarketAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiPolymarketAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiPolymarketAdapter) PerformAnalysis() error {
	return a.analyzer.TriggerAnalysis()
}

type aiPromptAdapter struct {
	manager *ai.PromptManager
}

func (a *aiPromptAdapter) GetAllPrompts() (map[string]interface{}, error) {
	prompts, err := a.manager.GetAllPrompts()
	if err != nil {
		return nil, err
	}
	result := make(map[string]interface{})
	for k, v := range prompts {
		result[k] = map[string]interface{}{
			"module":        v.Module,
			"template":      v.Template,
			"system_prompt": v.SystemPrompt,
		}
	}
	return result, nil
}

func (a *aiPromptAdapter) UpdatePrompt(module, template, systemPrompt string) error {
	return a.manager.UpdatePrompt(module, template, systemPrompt)
}
*/

// reconciliationRestoreAdapter 对账恢复适配器（用于从数据库恢复对账统计）
type reconciliationRestoreAdapter struct {
	storage storage.Storage
}

func (a *reconciliationRestoreAdapter) GetLatestReconciliationHistory(symbol string) (interface{}, error) {
	if a.storage == nil {
		return nil, nil
	}
	return a.storage.GetLatestReconciliationHistory(symbol)
}

func (a *reconciliationRestoreAdapter) GetReconciliationCount(symbol string) (int64, error) {
	if a.storage == nil {
		return 0, nil
	}
	return a.storage.GetReconciliationCount(symbol)
}

// tradeStorageAdapter 交易存储适配器
type tradeStorageAdapter struct {
	storageService *storage.StorageService
}

func (a *tradeStorageAdapter) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl float64, createdAt time.Time) error {
	if a.storageService == nil {
		return nil
	}
	st := a.storageService.GetStorage()
	if st == nil {
		return nil
	}
	return st.SaveTrade(&storage.Trade{
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Exchange:    exchange,
		Symbol:      symbol,
		BuyPrice:    buyPrice,
		SellPrice:   sellPrice,
		Quantity:    quantity,
		PnL:         pnl,
		CreatedAt:   createdAt,
	})
}

// symbolManagerWebAdapter SymbolManager Web API 适配器
type symbolManagerWebAdapter struct {
	manager         *SymbolManager
	ctx             context.Context
	cfg             *config.Config
	eventBus        *event.EventBus
	storageService  *storage.StorageService
	distributedLock lock.DistributedLock
}

func (a *symbolManagerWebAdapter) Get(exchange, symbol string) (interface{}, bool) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, false
	}
	return rt, true
}

func (a *symbolManagerWebAdapter) List() []interface{} {
	runtimes := a.manager.List()
	result := make([]interface{}, len(runtimes))
	for i, rt := range runtimes {
		result[i] = rt
	}
	return result
}

func (a *symbolManagerWebAdapter) StartSymbol(exchange, symbol string) error {
	// 检查是否已经运行
	if _, ok := a.manager.Get(exchange, symbol); ok {
		return fmt.Errorf("交易对 %s:%s 已经在运行", exchange, symbol)
	}

	// 从配置中查找对应的 SymbolConfig
	var symCfg *config.SymbolConfig
	for i := range a.cfg.Trading.Symbols {
		if strings.EqualFold(a.cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(a.cfg.Trading.Symbols[i].Symbol, symbol) {
			symCfg = &a.cfg.Trading.Symbols[i]
			break
		}
	}

	if symCfg == nil {
		return fmt.Errorf("未找到交易对配置: %s:%s", exchange, symbol)
	}

	// 启动 SymbolRuntime
	rt, err := startSymbolRuntime(a.ctx, a.cfg, *symCfg, a.eventBus, a.storageService, a.distributedLock)
	if err != nil {
		return fmt.Errorf("启动失败: %w", err)
	}

	// 添加到管理器
	a.manager.Add(rt)

	// 注册到 Web API
	if a.storageService != nil {
		status := &web.SystemStatus{
			Running:       true,
			Exchange:      exchange,
			Symbol:        symbol,
			CurrentPrice:  0,
			TotalPnL:      0,
			TotalTrades:   0,
			RiskTriggered: false,
			Uptime:        0,
		}
		web.RegisterSymbolProviders(exchange, symbol, &web.SymbolScopedProviders{
			Status:   status,
			Price:    rt.PriceMonitor,
			Exchange: &exchangeProviderAdapter{exchange: rt.Exchange},
			Position: web.NewPositionManagerAdapter(rt.SuperPositionManager),
			Risk:     rt.RiskMonitor,
			Storage:  web.NewStorageServiceAdapter(a.storageService),
		})
	}

	logger.Info("✅ [%s:%s] 交易已启动", exchange, symbol)
	return nil
}

func (a *symbolManagerWebAdapter) StopSymbol(exchange, symbol string) error {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return fmt.Errorf("交易对 %s:%s 未运行", exchange, symbol)
	}

	// 停止运行时
	if rt.Stop != nil {
		rt.Stop()
	}

	// 从管理器中移除（需要添加 Remove 方法）
	// 暂时保留在管理器中，只是停止运行

	logger.Info("⏹️ [%s:%s] 交易已停止", exchange, symbol)
	return nil
}

func (a *symbolManagerWebAdapter) ClosePositions(exchange, symbol string) (*web.ClosePositionsResponse, error) {
	rt, ok := a.manager.Get(exchange, symbol)
	if !ok {
		return nil, fmt.Errorf("交易对 %s:%s 未找到", exchange, symbol)
	}

	// 创建上下文（带超时）
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()

	// 调用平仓函数并获取结果
	successCount, failCount, err := closeAllPositionsWithResult(ctx, rt.Exchange, symbol, rt.PriceMonitor)
	if err != nil {
		return nil, err
	}

	message := fmt.Sprintf("平仓完成: 成功 %d, 失败 %d", successCount, failCount)
	if successCount == 0 && failCount == 0 {
		message = "当前没有持仓需要平仓"
	}

	return &web.ClosePositionsResponse{
		SuccessCount: successCount,
		FailCount:    failCount,
		Message:      message,
	}, nil
}

func main() {
	// 检查版本参数
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Printf("QuantMesh Market Maker\n")
		fmt.Printf("Version: %s\n", Version)
		os.Exit(0)
	}

	// 解析调试参数（-debug / --debug）
	debugMode := false
	filteredArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-debug", "--debug":
			debugMode = true
		default:
			filteredArgs = append(filteredArgs, arg)
		}
	}
	if debugMode {
		log.Printf("[INFO] Debug 模式已启用：Gin 将输出全量请求日志")
	}
	os.Args = filteredArgs

	// 注意：不再设置 time.Local，避免竞态条件
	// 时区处理统一使用 utils.GlobalLocation（通过 init() 或 config 设置）
	// 所有时间操作应使用 utils.ToConfiguredTimezone()、utils.ToUTC()、utils.NowConfiguredTimezone() 等工具函数

	// 1. 最早初始化日志存储（在配置加载之前，使用默认路径）
	logStoragePath := "./logs.db"
	if len(os.Args) > 2 && os.Args[1] == "--log-db" {
		logStoragePath = os.Args[2]
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}

	logStorage, err := storage.NewLogStorage(logStoragePath)
	if err != nil {
		log.Printf("[WARN] 初始化日志存储失败: %v，将继续运行但不保存日志到数据库", err)
		logStorage = nil
	} else {
		globalLogStorage = logStorage
		logger.InitLogStorage(func(level, message string) {
			if logStorage != nil {
				logStorage.WriteLog(level, message)
			}
		})
		log.Printf("[INFO] 日志存储已初始化: %s", logStoragePath)

		// 启动定期日志清理任务
		go func() {
			// 每天凌晨2点执行清理
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()

			// 计算到下一个凌晨2点的时间
			now := time.Now()
			nextCleanup := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, now.Location())
			if nextCleanup.Before(now) {
				nextCleanup = nextCleanup.Add(24 * time.Hour)
			}
			initialDelay := nextCleanup.Sub(now)

			// 等待到第一个清理时间
			time.Sleep(initialDelay)

			// 立即执行一次清理
			logger.Info("🧹 开始定期清理日志...")
			rowsAffected, err := logStorage.CleanOldLogsByLevel(7, []string{"INFO", "WARN"})
			if err != nil {
				logger.Warn("⚠️ 清理日志失败: %v", err)
			} else {
				logger.Info("✅ 已清理 %d 条 INFO/WARN 级别日志（7天前）", rowsAffected)
			}

			// 执行 VACUUM 优化
			if err := logStorage.Vacuum(); err != nil {
				logger.Warn("⚠️ 数据库优化失败: %v", err)
			} else {
				logger.Info("✅ 日志数据库优化完成")
			}

			// 定期执行
			for {
				select {
				case <-ticker.C:
					logger.Info("🧹 开始定期清理日志...")
					rowsAffected, err := logStorage.CleanOldLogsByLevel(7, []string{"INFO", "WARN"})
					if err != nil {
						logger.Warn("⚠️ 清理日志失败: %v", err)
					} else {
						logger.Info("✅ 已清理 %d 条 INFO/WARN 级别日志（7天前）", rowsAffected)
					}

					// 执行 VACUUM 优化
					if err := logStorage.Vacuum(); err != nil {
						logger.Warn("⚠️ 数据库优化失败: %v", err)
					} else {
						logger.Info("✅ 日志数据库优化完成")
					}
				}
			}
		}()
	}

	logger.Info("🚀 QuantMesh 做市商系统启动...")
	logger.Info("📦 版本号: %s", Version)

	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// 检查配置文件是否存在
	var cfg *config.Config
	var configComplete bool
	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		// 配置文件不存在，创建最小化配置
		logger.Info("ℹ️ 配置文件不存在，创建最小化配置（仅启用 Web 服务）")
		cfg = config.CreateMinimalConfig()
		configComplete = false

		// 保存最小化配置到文件（不验证，因为配置不完整）
		if err := config.SaveConfigWithoutValidation(cfg, configPath); err != nil {
			logger.Warn("⚠️ 保存最小化配置失败: %v，将继续运行", err)
		} else {
			logger.Info("✅ 已创建最小化配置文件: %s", configPath)
		}
	} else {
		// 配置文件存在，加载配置
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			logger.Fatalf("❌ 加载配置失败: %v", err)
		}

		// 检查配置是否完整（是否有交易所配置和交易对配置）
		configComplete = cfg.App.CurrentExchange != "" &&
			len(cfg.Exchanges) > 0 &&
			cfg.Exchanges[cfg.App.CurrentExchange].APIKey != "" &&
			cfg.Exchanges[cfg.App.CurrentExchange].SecretKey != "" &&
			len(cfg.Trading.Symbols) > 0 &&
			cfg.Trading.Symbols[0].Symbol != ""

		if !configComplete {
			logger.Info("ℹ️ 配置不完整，仅启动 Web 服务，请通过引导页面完成配置")
		}
	}

	if err := utils.SetLocation(cfg.System.Timezone); err != nil {
		logger.Warn("⚠️ 加载时区 %s 失败: %v，将使用默认时区 Asia/Shanghai", cfg.System.Timezone, err)
		utils.SetLocation("Asia/Shanghai")
	} else {
		logger.Info("✅ 系统时区设置为: %s", cfg.System.Timezone)
	}
	logger.SetLocation(utils.GlobalLocation)

	if debugMode {
		cfg.System.LogLevel = "debug"
	}

	logLevel := logger.ParseLogLevel(cfg.System.LogLevel)
	logger.SetLevel(logLevel)
	logger.Info("日志级别设置为: %s", logLevel.String())

	// 初始化 i18n 系统
	logLang := cfg.System.LogLanguage
	if logLang == "" {
		logLang = "zh-CN" // 默认中文
	}
	if err := i18n.Init(logLang); err != nil {
		logger.Warn("⚠️ 初始化 i18n 失败: %v，将使用默认语言", err)
	} else {
		logger.Info("✅ i18n 系统已初始化，日志语言: %s", logLang)
	}

	// 设置 logger 的语言和翻译函数
	logger.SetLogLanguage(logLang)
	logger.SetTranslateFunc(i18n.T)

	logger.Info("✅ 配置加载成功: 交易对数量=%d, 当前默认交易所=%s",
		len(cfg.Trading.Symbols), cfg.App.CurrentExchange)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 事件总线 & 通知 & 存储
	logger.Info("🔧 正在初始化事件总线...")
	eventBus := event.NewEventBus(1000)
	logger.Info("🔧 正在初始化通知服务...")
	notifier := notify.NewNotificationService(cfg)

	logger.Info("🔧 正在初始化存储服务...")
	storageService, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		logger.Warn("⚠️ 初始化存储服务失败: %v (将继续运行，但不保存数据)", err)
		storageService = nil
	} else if cfg.Storage.Enabled {
		storageService.Start()
	}
	logger.Info("✅ 存储服务初始化完成")

	// 初始化数据库（可选，用于未来迁移）
	var db database.Database
	if cfg.Database.Type != "" && cfg.Database.DSN != "" {
		dbConfig := &database.Config{
			Type:            cfg.Database.Type,
			DSN:             cfg.Database.DSN,
			MaxOpenConns:    cfg.Database.MaxOpenConns,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
			LogLevel:        cfg.Database.LogLevel,
		}
		db, err = database.NewDatabase(dbConfig)
		if err != nil {
			logger.Warn("⚠️ 初始化数据库失败: %v (将继续使用现有存储)", err)
			db = nil
		} else {
			defer db.Close()
			logger.Info("✅ 数据库已初始化 (类型: %s)", cfg.Database.Type)
		}
	}

	// 初始化事件中心
	logger.Info("🔧 正在初始化事件中心...")
	eventCenterConfig := &event.EventCenterConfig{
		Enabled:                  cfg.EventCenter.Enabled,
		PriceVolatilityThreshold: cfg.EventCenter.PriceVolatilityThreshold,
		MonitoredSymbols:         cfg.EventCenter.MonitoredSymbols,
		CleanupInterval:          cfg.EventCenter.CleanupInterval,
		Retention: event.RetentionConfig{
			CriticalDays:     cfg.EventCenter.Retention.CriticalDays,
			WarningDays:      cfg.EventCenter.Retention.WarningDays,
			InfoDays:         cfg.EventCenter.Retention.InfoDays,
			CriticalMaxCount: cfg.EventCenter.Retention.CriticalMaxCount,
			WarningMaxCount:  cfg.EventCenter.Retention.WarningMaxCount,
			InfoMaxCount:     cfg.EventCenter.Retention.InfoMaxCount,
		},
	}
	
	var eventCenter *event.EventCenter
	if db != nil {
		eventCenter = event.NewEventCenter(db, eventBus, notifier, eventCenterConfig)
		if err := eventCenter.Start(); err != nil {
			logger.Warn("⚠️ 启动事件中心失败: %v", err)
		}
		defer eventCenter.Stop()
	} else {
		logger.Warn("⚠️ 数据库未初始化，事件中心将不可用")
	}
	logger.Info("✅ 事件中心初始化完成")

	// 旧的事件处理器（保留用于存储服务）
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-eventBus.Subscribe():
				if evt == nil {
					continue
				}
				go func(e *event.Event) {
					if storageService != nil {
						storageService.Save(string(e.Type), e.Data)
					}
				}(evt)
			}
		}
	}()

	// 初始化 Prometheus 系统指标采集器
	logger.Info("🔧 正在初始化 Prometheus 系统指标采集器...")
	systemMetricsCollector := metrics.NewSystemMetricsCollector(10 * time.Second)
	systemMetricsCollector.Start()
	logger.Info("✅ Prometheus 系统指标采集器已启动")

	// 初始化分布式锁（多实例模式）
	logger.Info("🔧 正在初始化分布式锁...")
	var distributedLock lock.DistributedLock
	lockConfig := &lock.Config{
		Enabled:    cfg.DistributedLock.Enabled,
		Type:       cfg.DistributedLock.Type,
		Prefix:     cfg.DistributedLock.Prefix,
		DefaultTTL: time.Duration(cfg.DistributedLock.DefaultTTL) * time.Second,
		Redis: lock.RedisConfig{
			Addr:     cfg.DistributedLock.Redis.Addr,
			Password: cfg.DistributedLock.Redis.Password,
			DB:       cfg.DistributedLock.Redis.DB,
			PoolSize: cfg.DistributedLock.Redis.PoolSize,
		},
	}
	distributedLock, err = lock.NewDistributedLock(lockConfig)
	if err != nil {
		logger.Fatalf("❌ 初始化分布式锁失败: %v", err)
	}
	defer distributedLock.Close()

	if cfg.DistributedLock.Enabled {
		logger.Info("✅ 分布式锁已启用 (类型: %s, 实例: %s)", cfg.DistributedLock.Type, cfg.Instance.ID)
	} else {
		logger.Info("ℹ️ 分布式锁未启用（单机模式）")
	}

	// 初始化 Watchdog（系统监控）
	logger.Info("🔧 正在初始化系统监控...")
	var watchdog *monitor.Watchdog
	if cfg.Watchdog.Enabled {
		watchdog = monitor.NewWatchdog(cfg, storageService, globalLogStorage, notifier)
		if err := watchdog.Start(ctx); err != nil {
			logger.Error("❌ 启动 Watchdog 失败: %v", err)
		} else {
			logger.Info("✅ Watchdog 系统监控已启动")
		}
	}

	// 初始化插件系统
	var pluginLoader *plugin.PluginLoader
	if cfg.Plugins.Enabled {
		logger.Info("🔌 开始加载插件系统...")
		pluginLoader = plugin.NewPluginLoader()

		// 从目录加载所有插件
		pluginDir := cfg.Plugins.Directory
		if pluginDir == "" {
			pluginDir = "./plugins"
		}

		logger.Info("📂 插件目录: %s", pluginDir)
		if err := pluginLoader.LoadPluginsFromDirectory(pluginDir, cfg.Plugins.Licenses); err != nil {
			logger.Warn("⚠️ 加载插件失败: %v", err)
		} else {
			// 初始化每个已加载的插件
			loadedPlugins := pluginLoader.ListPlugins()
			logger.Info("📦 已发现 %d 个插件", len(loadedPlugins))

			for _, p := range loadedPlugins {
				pluginConfig, exists := cfg.Plugins.Config[p.Name]
				if !exists {
					pluginConfig = make(map[string]interface{})
				}

				if err := pluginLoader.InitializePlugin(p.Name, pluginConfig); err != nil {
					logger.Warn("⚠️ 初始化插件 %s 失败: %v", p.Name, err)
				} else {
					logger.Info("✅ 插件 %s (版本 %s) 初始化成功", p.Name, p.Version)
				}
			}

			logger.Info("✅ 插件系统启动完成")
		}

		// 在程序退出时卸载所有插件
		defer func() {
			if pluginLoader != nil {
				pluginLoader.UnloadAll()
				logger.Info("✅ 所有插件已卸载")
			}
		}()
	} else {
		logger.Info("ℹ️ 插件系统未启用")
	}

	// Web 服务器
	var webServer *web.WebServer
	if cfg.Web.Enabled {
		logger.Info("🌐 开始初始化 Web 服务器...")
		// 初始化密码管理器
		passwordManager, err := web.NewPasswordManager("./data")
		if err != nil {
			logger.Error("❌ 初始化密码管理器失败: %v", err)
		} else {
			web.SetPasswordManager(passwordManager)
			logger.Info("✅ 密码管理器已初始化")
		}

		// 初始化 WebAuthn 管理器
		rpID := "localhost"
		rpOrigin := fmt.Sprintf("http://%s:%d", cfg.Web.Host, cfg.Web.Port)
		if cfg.Web.Host == "0.0.0.0" {
			rpOrigin = fmt.Sprintf("http://localhost:%d", cfg.Web.Port)
		}
		webauthnManager, err := web.NewWebAuthnManager(&webAuthnLoggerAdapter{}, "./data", rpID, rpOrigin)
		if err != nil {
			logger.Error("❌ 初始化 WebAuthn 管理器失败: %v", err)
		} else {
			web.SetWebAuthnManager(webauthnManager)
			logger.Info("✅ WebAuthn 管理器已初始化 (rpID=%s, rpOrigin=%s)", rpID, rpOrigin)
		}

		// 初始化配置管理器
		configManager := web.NewConfigManager(configPath)
		web.SetConfigManager(configManager)
		logger.Info("✅ 配置管理器已初始化")

		// 设置版本号
		web.SetVersion(Version)
		logger.Info("✅ 版本号已设置: %s", Version)

		// 初始化配置备份管理器
		backupManager := config.NewBackupManager()
		web.SetConfigBackupManager(backupManager)
		logger.Info("✅ 配置备份管理器已初始化")

		// 初始化配置热更新器
		hotReloader := config.NewHotReloader(cfg)
		web.SetConfigHotReloader(hotReloader)
		logger.Info("✅ 配置热更新器已初始化")

		// 设置日志存储提供者（用于Web API日志查询）
		if globalLogStorage != nil {
			logStorageAdapter := web.NewLogStorageAdapter(globalLogStorage)
			web.SetLogStorageProvider(logStorageAdapter)
			logger.Info("✅ 日志存储提供者已设置")
		}

		logger.Info("🔧 正在创建 Web 服务器实例...")
		webServer = web.NewWebServer(cfg)
		if webServer == nil {
			logger.Warn("⚠️ Web 服务器未创建（可能配置中 Web.Enabled=false）")
		} else {
			logger.Info("🔧 正在启动 Web 服务器...")
			if err := webServer.Start(ctx); err != nil {
				logger.Error("❌ 启动Web服务器失败: %v", err)
			} else {
				logger.Info("✅ Web服务器已启动，可通过 http://%s:%d 访问", cfg.Web.Host, cfg.Web.Port)
				// 等待一下，确保 goroutine 中的日志也能输出
				time.Sleep(200 * time.Millisecond)
			}
		}
	} else {
		logger.Info("ℹ️ Web 服务未启用（配置中 web.enabled=false）")
	}

	symbolManager := NewSymbolManager(cfg)

	// 创建 SymbolManager 适配器（用于 Web API）
	symbolManagerAdapter := &symbolManagerWebAdapter{
		manager:         symbolManager,
		ctx:             ctx,
		cfg:             cfg,
		eventBus:        eventBus,
		storageService: storageService,
		distributedLock: distributedLock,
	}
	web.RegisterSymbolManager(symbolManagerAdapter)

	// 只有在配置完整时才启动交易系统
	var firstRuntime *SymbolRuntime
	if configComplete {
		// 启动所有交易对
		for _, symCfg := range cfg.Trading.Symbols {
			rt, err := startSymbolRuntime(ctx, cfg, symCfg, eventBus, storageService, distributedLock)
			if err != nil {
				logger.Error("❌ [%s:%s] 启动失败: %v", symCfg.Exchange, symCfg.Symbol, err)
				continue
			}
			symbolManager.Add(rt)
			if firstRuntime == nil {
				firstRuntime = rt
			}
		}

		if firstRuntime == nil {
			logger.Warn("⚠️ 所有交易对启动失败，但 Web 服务将继续运行")
			configComplete = false // 标记为不完整，避免后续绑定数据
		}
	} else {
		logger.Info("ℹ️ 配置不完整，跳过交易系统启动，仅运行 Web 服务")
	}

	// Web 绑定数据提供者（兼容旧前端：使用第一个运行时，同时注册多交易对）
	if webServer != nil && configComplete && firstRuntime != nil {
		statusMap := make(map[string]*web.SystemStatus)
		for _, rt := range symbolManager.List() {
			if rt == nil {
				continue
			}
			status := &web.SystemStatus{
				Running:       true,
				Exchange:      rt.Config.Exchange,
				Symbol:        rt.Config.Symbol,
				CurrentPrice:  0,
				TotalPnL:      0,
				TotalTrades:   0,
				RiskTriggered: false,
				Uptime:        0,
			}
			statusMap[fmt.Sprintf("%s:%s", rt.Config.Exchange, rt.Config.Symbol)] = status

			web.RegisterSymbolProviders(rt.Config.Exchange, rt.Config.Symbol, &web.SymbolScopedProviders{
				Status:   status,
				Price:    rt.PriceMonitor,
				Exchange: &exchangeProviderAdapter{exchange: rt.Exchange},
				Position: web.NewPositionManagerAdapter(rt.SuperPositionManager),
				Risk:     rt.RiskMonitor,
				Storage:  web.NewStorageServiceAdapter(storageService),
			})

			startTime := time.Now()
			go func(r *SymbolRuntime, st *web.SystemStatus, started time.Time) {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				dbQueryCounter := 0
				for {
					select {
					case <-ctx.Done():
						st.Running = false
						web.SetStatusProvider(st)
						return
					case <-ticker.C:
						if r.PriceMonitor != nil {
							st.CurrentPrice = r.PriceMonitor.GetLastPrice()
							if st.CurrentPrice > 0 {
								st.Running = true
							}
						}
						if r.RiskMonitor != nil {
							st.RiskTriggered = r.RiskMonitor.IsTriggered()
						}

						// 更新统计信息
						if r.SuperPositionManager != nil {
							// 增加计数器，每 10 秒（5个周期）从数据库同步一次真实数据
							dbQueryCounter++

							useEstimation := true
							if storageService != nil && storageService.GetStorage() != nil {
								// 每 10 秒更新一次，或者如果当前 PnL 还是 0 则更新
								if dbQueryCounter >= 5 || st.TotalPnL == 0 {
									dbQueryCounter = 0
									// 获取今日 00:00:00 的时间（系统配置时区）
									now := utils.NowConfiguredTimezone()
									todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

									// 转换为 UTC 时间进行数据库查询，确保时区一致
									pnlSummary, err := storageService.GetStorage().GetPnLBySymbol(r.Config.Symbol, utils.ToUTC(todayStart), utils.ToUTC(now))
									if err == nil {
										st.TotalPnL = pnlSummary.TotalPnL
										st.TotalTrades = pnlSummary.TotalTrades
										useEstimation = false
									}
								} else {
									// 在非更新周期，保持之前的值，不使用估算
									useEstimation = false
								}
							}

							// 如果无法从数据库获取（或未启用存储），回退到估算逻辑
							if useEstimation {
								totalBuyQty := r.SuperPositionManager.GetTotalBuyQty()
								totalSellQty := r.SuperPositionManager.GetTotalSellQty()
								priceInterval := r.SuperPositionManager.GetPriceInterval()

								// 修正盈亏估算：仅作为参考
								st.TotalPnL = totalSellQty * priceInterval

								// 修正成交次数估算：数量之和 / (单笔数量 * 2)
								if st.CurrentPrice > 0 {
									orderQtyInBase := r.Config.OrderQuantity / st.CurrentPrice
									if orderQtyInBase > 0 {
										st.TotalTrades = int((totalBuyQty + totalSellQty) / (orderQtyInBase * 2))
									}
								}
							}
						}

						st.Uptime = int64(time.Since(started).Seconds())
						if r == firstRuntime {
							// 兼容旧接口
							web.SetStatusProvider(st)
						}
					}
				}
			}(rt, status, startTime)
		}

		if firstRuntime != nil {
			web.SetDefaultSymbolKey(firstRuntime.Config.Exchange, firstRuntime.Config.Symbol)
			web.SetStatusProvider(statusMap[fmt.Sprintf("%s:%s", firstRuntime.Config.Exchange, firstRuntime.Config.Symbol)])
			web.SetOrderQuantityConfig(firstRuntime.Config.OrderQuantity)
		}

		// 资金费率监控（复用旧逻辑，默认主流交易对）
		if storageService != nil {
			symbols := []string{
				"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
				"ADAUSDT", "DOGEUSDT", "DOTUSDT", "MATICUSDT", "AVAXUSDT",
			}
			fundingMonitor := monitor.NewFundingMonitor(
				storageService.GetStorage(),
				firstRuntime.Exchange,
				symbols,
				8,
			)
			fundingMonitor.Start()
			web.RegisterFundingProvider(firstRuntime.Config.Exchange, firstRuntime.Config.Symbol, fundingMonitor)
			web.SetFundingMonitorProvider(fundingMonitor)

			// 初始化价差监控
			if cfg.BasisMonitor.Enabled {
				logger.Info("🔍 初始化价差监控...")
				basisMonitor := monitor.NewBasisMonitor(
					storageService.GetStorage(),
					firstRuntime.Exchange,
					cfg.BasisMonitor.Symbols,
					cfg.BasisMonitor.IntervalMinutes,
				)
				basisMonitor.Start()
				web.SetBasisMonitorProvider(basisMonitor)
				logger.Info("✅ 价差监控已启动")
			}
		}

		// 设置系统监控数据提供者
		if watchdog != nil {
			systemMetricsProvider := web.NewSystemMetricsProvider(storageService, watchdog)
			web.SetSystemMetricsProvider(systemMetricsProvider)
			logger.Info("✅ 系统监控数据提供者已设置")
		}

		// 设置事件中心提供者
		if db != nil {
			web.SetEventProvider(db)
			logger.Info("✅ 事件中心提供者已设置")
		}

		logger.Info("✅ 所有交易对已初始化，进入运行状态")
	} else if webServer != nil {
		// 配置不完整，只设置存储服务提供者
		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
		}

		// 设置系统监控数据提供者
		if watchdog != nil {
			systemMetricsProvider := web.NewSystemMetricsProvider(storageService, watchdog)
			web.SetSystemMetricsProvider(systemMetricsProvider)
			logger.Info("✅ 系统监控数据提供者已设置")
		}
		
		// 设置事件中心提供者
		if db != nil {
			web.SetEventProvider(db)
			logger.Info("✅ 事件中心提供者已设置")
		}

		logger.Info("ℹ️ Web 服务已启动，等待配置完成")
	}

	// 所有初始化完成，程序进入运行状态
	logger.Info("✅ 系统初始化完成，程序正在运行中...")
	logger.Info("💡 按 Ctrl+C 退出程序")

	// 等待退出信号（SIGINT 或 SIGTERM）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("🛑 收到退出信号，开始优雅关闭...")

	// 发布系统停止事件
	if eventBus != nil {
		eventBus.Publish(&event.Event{
			Type: event.EventTypeSystemStop,
			Data: map[string]interface{}{
				"reason": "收到退出信号",
			},
		})
	}

	// 🔥 第一优先级：撤销各交易对的订单（仅在配置完整时）
	if configComplete {
		if cfg.System.CancelOnExit {
			for _, rt := range symbolManager.List() {
				logger.Info("🔄 [%s:%s] 正在撤销所有订单...", rt.Config.Exchange, rt.Config.Symbol)
				cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
				if err := rt.Exchange.CancelAllOrders(cancelCtx, rt.Config.Symbol); err != nil {
					logger.Error("❌ [%s:%s] 撤销订单失败: %v", rt.Config.Exchange, rt.Config.Symbol, err)
				} else {
					logger.Info("✅ [%s:%s] 已撤销所有订单", rt.Config.Exchange, rt.Config.Symbol)
				}
				cancelTimeout()
			}
		}

		// 🔥 平仓（可选）
		if cfg.System.ClosePositionsOnExit {
			for _, rt := range symbolManager.List() {
				logger.Info("🔄 [%s:%s] 正在平掉所有持仓...", rt.Config.Exchange, rt.Config.Symbol)
				closeCtx, closeTimeout := context.WithTimeout(context.Background(), 30*time.Second)
				closeAllPositions(closeCtx, rt.Exchange, rt.Config.Symbol, rt.PriceMonitor)
				closeTimeout()
			}
		}

		// 🔥 停止所有交易对组件
		for _, rt := range symbolManager.List() {
			if rt.Stop != nil {
				rt.Stop()
			}
		}
	}

	// 🔥 第三优先级：停止所有协程（取消 context）
	// 这会通知所有使用 ctx 的协程停止工作（包括事件处理协程）
	cancel()

	// 等待一小段时间，让事件处理协程完成清理（确保事件队列被处理完）
	time.Sleep(500 * time.Millisecond)

	// 🔥 第四优先级：停止存储服务（确保所有事件都已处理完毕）
	logger.Info("⏹️ 正在停止存储服务...")
	if storageService != nil {
		storageService.Stop()
	}

	// 再等待一小段时间，让存储服务完成最后的写入
	time.Sleep(200 * time.Millisecond)

	// 打印最终状态（仅在配置完整时）
	if configComplete {
		for _, rt := range symbolManager.List() {
			if rt.SuperPositionManager != nil {
				rt.SuperPositionManager.PrintPositions()
			}
		}
	}

	// 关闭文件日志
	logger.Close()

	// 关闭日志存储
	if globalLogStorage != nil {
		if err := globalLogStorage.Close(); err != nil {
			logger.Error("❌ 关闭日志存储失败: %v", err)
		}
	}

	logger.Info("✅ 系统已安全退出 QuantMesh")
}

// loggerAdapter 适配 logger 到 WebAuthnLogger 接口
type loggerAdapter struct{}

func (l *loggerAdapter) Infof(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func (l *loggerAdapter) Warnf(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func (l *loggerAdapter) Errorf(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func (l *loggerAdapter) Debugf(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

// positionExchangeAdapter 适配器，将 exchange.IExchange 转换为 position.IExchange
type positionExchangeAdapter struct {
	exchange exchange.IExchange
}

func (a *positionExchangeAdapter) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	positions, err := a.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	// 转换为 position.PositionInfo 切片
	result := make([]*position.PositionInfo, len(positions))
	for i, pos := range positions {
		result[i] = &position.PositionInfo{
			Symbol: pos.Symbol,
			Size:   pos.Size,
		}
	}

	return result, nil
}

func (a *positionExchangeAdapter) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return a.exchange.GetOpenOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

func (a *positionExchangeAdapter) GetBaseAsset() string {
	return a.exchange.GetBaseAsset()
}

func (a *positionExchangeAdapter) GetName() string {
	return a.exchange.GetName()
}

func (a *positionExchangeAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	return a.exchange.CancelAllOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetAccount(ctx context.Context) (interface{}, error) {
	return a.exchange.GetAccount(ctx)
}

// exchangeProviderAdapter 适配器，将 exchange.IExchange 转换为 web.ExchangeProvider
type exchangeProviderAdapter struct {
	exchange exchange.IExchange
}

func (a *exchangeProviderAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return a.exchange.GetHistoricalKlines(ctx, symbol, interval, limit)
}

// exchangeExecutorAdapter 适配器，将 order.ExchangeOrderExecutor 转换为 position.OrderExecutorInterface
type exchangeExecutorAdapter struct {
	executor *order.ExchangeOrderExecutor
	eventBus *event.EventBus
	symbol   string
}

func (a *exchangeExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,      // 传递 PostOnly 参数
		ClientOrderID: req.ClientOrderID, // 传递 ClientOrderID
	}
	ord, err := a.executor.PlaceOrder(orderReq)
	if err != nil {
		return nil, err
	}

	// 发布订单下单事件
	if a.eventBus != nil {
		a.eventBus.Publish(&event.Event{
			Type: event.EventTypeOrderPlaced,
			Data: map[string]interface{}{
				"order_id":        ord.OrderID,
				"client_order_id": ord.ClientOrderID,
				"symbol":          ord.Symbol,
				"side":            ord.Side,
				"price":           ord.Price,
				"quantity":        ord.Quantity,
				"status":          ord.Status,
				"created_at":      ord.CreatedAt,
			},
		})
	}

	return &position.Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID, // 返回 ClientOrderID
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		Status:        ord.Status,
		CreatedAt:     ord.CreatedAt,
	}, nil
}

func (a *exchangeExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := a.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError
}

func (a *exchangeExecutorAdapter) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	orderReqs := make([]*order.OrderRequest, len(orders))
	for i, req := range orders {
		orderReqs[i] = &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Price:         req.Price,
			Quantity:      req.Quantity,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,      // 传递 PostOnly 参数
			ClientOrderID: req.ClientOrderID, // 传递 ClientOrderID
		}
	}
	batchResult := a.executor.BatchPlaceOrdersWithDetails(orderReqs)

	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, len(batchResult.PlacedOrders)),
		HasMarginError:   batchResult.HasMarginError,
		ReduceOnlyErrors: batchResult.ReduceOnlyErrors,
	}

	for i, ord := range batchResult.PlacedOrders {
		result.PlacedOrders[i] = &position.Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID, // 返回 ClientOrderID
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Status:        ord.Status,
			CreatedAt:     ord.CreatedAt,
		}

		// 发布订单下单事件
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeOrderPlaced,
				Data: map[string]interface{}{
					"order_id":        ord.OrderID,
					"client_order_id": ord.ClientOrderID,
					"symbol":          ord.Symbol,
					"side":            ord.Side,
					"price":           ord.Price,
					"quantity":        ord.Quantity,
					"status":          ord.Status,
					"created_at":      ord.CreatedAt,
				},
			})
		}
	}
	return result
}

func (a *exchangeExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}

// closeAllPositions 平掉所有持仓（退出时使用）
func closeAllPositions(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) {
	// 1. 查询所有持仓
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查询持仓失败，无法平仓: %v", err)
		return
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 当前没有持仓，无需平仓")
		return
	}

	// 2. 获取当前价格（用于平仓单）
	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	// 如果价格监控器没有价格，尝试从交易所获取
	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 无法获取当前价格，将使用持仓标记价格平仓")
		}
	}

	// 3. 统计需要平仓的持仓
	needCloseCount := 0
	for _, pos := range positions {
		// Size 正数表示多仓，负数表示空仓，为0表示无持仓
		if pos.Size != 0 {
			needCloseCount++
		}
	}

	if needCloseCount == 0 {
		logger.Info("ℹ️ 当前没有有效持仓，无需平仓")
		return
	}

	logger.Info("🔄 发现 %d 个持仓需要平仓", needCloseCount)

	// 4. 对每个持仓下平仓单
	successCount := 0
	failCount := 0

	for _, pos := range positions {
		// 跳过无持仓
		if pos.Size == 0 {
			continue
		}

		// 确定平仓方向和数量
		var side exchange.Side
		quantity := pos.Size
		if quantity > 0 {
			// 多仓，需要下 SELL 单平仓
			side = exchange.SideSell
		} else {
			// 空仓，需要下 BUY 单平仓（注意 Size 是负数）
			side = exchange.SideBuy
			quantity = -quantity // 转为正数
		}

		// 确定平仓价格：优先使用当前价格，否则使用标记价格，最后使用开仓价格
		closePrice := currentPrice
		if closePrice <= 0 && pos.MarkPrice > 0 {
			closePrice = pos.MarkPrice
		}
		if closePrice <= 0 && pos.EntryPrice > 0 {
			closePrice = pos.EntryPrice
		}

		if closePrice <= 0 {
			logger.Error("❌ [平仓] 无法确定价格，跳过持仓 %s (Size: %.6f)", pos.Symbol, pos.Size)
			failCount++
			continue
		}

		// 下单平仓
		logger.Info("🔄 [平仓] %s %s %.6f @ %.2f (ReduceOnly)", side, pos.Symbol, quantity, closePrice)

		orderReq := &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeLimit,
			TimeInForce:   exchange.TimeInForceGTC,
			Quantity:      quantity,
			Price:         closePrice,
			ReduceOnly:    true, // 只减仓
			PostOnly:      false,
			PriceDecimals: ex.GetPriceDecimals(),
		}

		_, err := ex.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("❌ [平仓] 下单失败 %s %.6f @ %.2f: %v", side, quantity, closePrice, err)
			failCount++
		} else {
			logger.Info("✅ [平仓] 已下单 %s %.6f @ %.2f", side, quantity, closePrice)
			successCount++
		}

		// 避免请求过快，稍微延迟
		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("📊 [平仓完成] 成功: %d, 失败: %d", successCount, failCount)

	// 5. 等待一段时间，让平仓单成交（可选）
	if successCount > 0 {
		logger.Info("⏳ 等待平仓单成交...")
		time.Sleep(2 * time.Second)
	}
}

// closeAllPositionsWithResult 平掉所有持仓并返回结果（用于 API）
func closeAllPositionsWithResult(ctx context.Context, ex exchange.IExchange, symbol string, priceMonitor *monitor.PriceMonitor) (successCount, failCount int, err error) {
	// 1. 查询所有持仓
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Error("❌ 查询持仓失败，无法平仓: %v", err)
		return 0, 0, err
	}

	if len(positions) == 0 {
		logger.Info("ℹ️ 当前没有持仓，无需平仓")
		return 0, 0, nil
	}

	// 2. 获取当前价格（用于平仓单）
	currentPrice := 0.0
	if priceMonitor != nil {
		currentPrice = priceMonitor.GetLastPrice()
	}

	// 如果价格监控器没有价格，尝试从交易所获取
	if currentPrice <= 0 {
		var priceErr error
		currentPrice, priceErr = ex.GetLatestPrice(ctx, symbol)
		if priceErr != nil || currentPrice <= 0 {
			logger.Warn("⚠️ 无法获取当前价格，将使用持仓标记价格平仓")
		}
	}

	// 3. 统计需要平仓的持仓
	needCloseCount := 0
	for _, pos := range positions {
		if pos.Size != 0 {
			needCloseCount++
		}
	}

	if needCloseCount == 0 {
		logger.Info("ℹ️ 当前没有有效持仓，无需平仓")
		return 0, 0, nil
	}

	logger.Info("🔄 发现 %d 个持仓需要平仓", needCloseCount)

	// 0. 先取消所有挂单，确保平仓单能顺利下单
	logger.Info("🧹 [平仓] 正在取消 %s 的所有挂单...", symbol)
	if err := ex.CancelAllOrders(ctx, symbol); err != nil {
		logger.Warn("⚠️ [平仓] 取消挂单失败: %v (将继续尝试平仓)", err)
	}

	// 4. 对每个持仓下平仓单
	successCount = 0
	failCount = 0

	for _, pos := range positions {
		if pos.Size == 0 {
			continue
		}

		var side exchange.Side
		quantity := pos.Size
		if quantity > 0 {
			side = exchange.SideSell
		} else {
			side = exchange.SideBuy
			quantity = -quantity
		}

		logger.Info("🔄 [平仓] %s %s %.6f (市价 ReduceOnly)", side, symbol, quantity)

		orderReq := &exchange.OrderRequest{
			Symbol:        symbol,
			Side:          side,
			Type:          exchange.OrderTypeMarket, // 使用市价单确保立即平仓
			Quantity:      quantity,
			ReduceOnly:    true,
			PriceDecimals: ex.GetPriceDecimals(),
		}

		_, err := ex.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("❌ [平仓] 下单失败 %s %.6f: %v", side, quantity, err)
			failCount++
		} else {
			logger.Info("✅ [平仓] 已下单 %s %.6f", side, quantity)
			successCount++
		}

		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("📊 [平仓完成] 成功: %d, 失败: %d", successCount, failCount)
	return successCount, failCount, nil
}
