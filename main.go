package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/notify"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/safety"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
	watchdogMonitor "quantmesh/monitor"
)

// Version 版本号
var Version = "v3.3.1"

// 全局日志存储实例（用于清理任务和 WebSocket 推送）
var globalLogStorage *storage.LogStorage

// reconciliationStorageAdapter 对账存储适配器
type reconciliationStorageAdapter struct {
	storageService *storage.StorageService
}

func (a *reconciliationStorageAdapter) SaveReconciliationHistory(symbol string, reconcileTime time.Time, localPosition, exchangePosition, positionDiff float64,
	activeBuyOrders, activeSellOrders int, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit float64) error {
	return a.storageService.SaveReconciliationHistoryDirect(symbol, reconcileTime, localPosition, exchangePosition, positionDiff,
		activeBuyOrders, activeSellOrders, pendingSellQty, totalBuyQty, totalSellQty, estimatedProfit)
}

// tradeStorageAdapter 交易存储适配器
type tradeStorageAdapter struct {
	storageService *storage.StorageService
}

func (a *tradeStorageAdapter) SaveTrade(buyOrderID, sellOrderID int64, symbol string, buyPrice, sellPrice, quantity, pnl float64, createdAt time.Time) error {
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
		Symbol:      symbol,
		BuyPrice:    buyPrice,
		SellPrice:   sellPrice,
		Quantity:    quantity,
		PnL:         pnl,
		CreatedAt:   createdAt,
	})
}

func main() {
	// 0. 最早初始化日志存储（在配置加载之前，使用默认路径）
	// 这样即使配置加载失败，也能记录日志
	logStoragePath := "./logs.db"
	if len(os.Args) > 2 && os.Args[1] == "--log-db" {
		logStoragePath = os.Args[2]
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}
	
	logStorage, err := storage.NewLogStorage(logStoragePath)
	if err != nil {
		// 初始化失败，但不退出程序（使用标准库输出错误）
		log.Printf("[WARN] 初始化日志存储失败: %v，将继续运行但不保存日志到数据库", err)
		logStorage = nil
	} else {
		globalLogStorage = logStorage
		// 注册日志写入器
		logger.InitLogStorage(func(level, message string) {
			if logStorage != nil {
				logStorage.WriteLog(level, message)
			}
		})
		log.Printf("[INFO] 日志存储已初始化: %s", logStoragePath)
	}

	logger.Info("🚀 QuantMesh 做市商系统启动...")
	logger.Info("📦 版本号: %s", Version)

	// 1. 加载配置
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Fatalf("❌ 加载配置失败: %v", err)
	}

	// 初始化日志级别
	logLevel := logger.ParseLogLevel(cfg.System.LogLevel)
	logger.SetLevel(logLevel)
	logger.Info("日志级别设置为: %s", logLevel.String())

	logger.Info("✅ 配置加载成功: 交易对=%s, 窗口大小=%d, 当前交易所=%s",
		cfg.Trading.Symbol, cfg.Trading.BuyWindowSize, cfg.App.CurrentExchange)

	// 2. 创建交易所实例（使用工厂模式）
	ex, err := exchange.NewExchange(cfg)
	if err != nil {
		logger.Fatalf("❌ 创建交易所实例失败: %v", err)
	}
	logger.Info("✅ 使用交易所: %s", ex.GetName())

	// 3. 创建价格监控组件（全局唯一的价格来源）
	// 架构说明：
	// - 这是整个系统中唯一的价格流启动点
	// - WebSocket 是唯一的价格来源，不使用 REST API 轮询
	// - 所有组件需要价格时，都应该通过 priceMonitor.GetLastPrice() 获取
	// - 必须在其他组件初始化前启动，确保价格数据就绪
	priceMonitor := monitor.NewPriceMonitor(
		ex,
		cfg.Trading.Symbol,
		cfg.Timing.PriceSendInterval,
	)

	// 4. 先启动 Web 服务器（即使价格监控失败，也能访问 Web 界面）
	// 创建主 context（用于整个应用生命周期）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 提前声明所有后续可能用到的变量，避免 goto 跳过变量声明
	var webServer *web.WebServer
	var currentPrice float64
	var currentPriceStr string
	var dynamicAdjuster *strategy.DynamicAdjuster
	var trendDetector *strategy.TrendDetector
	var watchdog *watchdogMonitor.Watchdog
	var orderCleaner *safety.OrderCleaner
	var reconciler *safety.Reconciler
	var riskMonitor *safety.RiskMonitor
	var priceDecimals int
	var quantityDecimals int
	var feeRate float64
	var eventBus *event.EventBus
	var notifier *notify.NotificationService
	var storageService *storage.StorageService
	var exchangeExecutor *order.ExchangeOrderExecutor
	var executorAdapter *exchangeExecutorAdapter
	var exchangeAdapter *positionExchangeAdapter
	var superPositionManager *position.SuperPositionManager
	var strategyManager *strategy.StrategyManager
	var totalCapital float64
	var multiExecutor *strategy.MultiStrategyExecutor
	var requiredPositions int
	var exchangeCfg config.ExchangeConfig
	var pollInterval time.Duration
	var maxLeverage int

	if cfg.Web.Enabled {
		webServer = web.NewWebServer(cfg)
		if err := webServer.Start(ctx); err != nil {
			logger.Error("❌ 启动Web服务器失败: %v", err)
		} else {
			logger.Info("✅ Web服务器已启动，可通过 http://%s:%d 访问", cfg.Web.Host, cfg.Web.Port)
			
			// === 初始化系统状态提供者（提前初始化，确保前端能看到状态）===
			startTime := time.Now()
			systemStatus := &web.SystemStatus{
				Running:       false, // 初始状态为停止，等交易系统启动后更新为 true
				Exchange:      cfg.App.CurrentExchange,
				Symbol:        cfg.Trading.Symbol,
				CurrentPrice:  0,
				TotalPnL:      0,
				TotalTrades:   0,
				RiskTriggered: false,
				Uptime:        0,
			}
			web.SetStatusProvider(systemStatus)

			// 启动状态更新协程（每2秒更新一次）
			go func() {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						// 系统停止时更新状态
						systemStatus.Running = false
						web.SetStatusProvider(systemStatus)
						return
					case <-ticker.C:
						// 更新当前价格
						if priceMonitor != nil {
							currentPrice := priceMonitor.GetLastPrice()
							systemStatus.CurrentPrice = currentPrice
							// 如果价格大于0，说明系统正在运行
							if currentPrice > 0 {
								systemStatus.Running = true
							}
						}

						// 更新风控状态
						if riskMonitor != nil {
							systemStatus.RiskTriggered = riskMonitor.IsTriggered()
						}

						// 更新总盈亏（使用预计盈利：价格间隔 * 卖出数量）
						if superPositionManager != nil {
							totalBuyQty := superPositionManager.GetTotalBuyQty()
							totalSellQty := superPositionManager.GetTotalSellQty()
							priceInterval := superPositionManager.GetPriceInterval()
							// 预计盈利 = 卖出数量 * 价格间隔
							systemStatus.TotalPnL = totalSellQty * priceInterval
							// 总交易数可以近似为买入和卖出数量的平均值（简化处理）
							// 或者使用已完成订单的数量
							systemStatus.TotalTrades = int((totalBuyQty + totalSellQty) / (cfg.Trading.OrderQuantity * 2))
						}

						// 更新运行时间
						systemStatus.Uptime = int64(time.Since(startTime).Seconds())

						// 更新状态提供者
						web.SetStatusProvider(systemStatus)
					}
				}
			}()
		}
	}

	// 5. 启动价格监控（WebSocket 必须成功）
	logger.Info("🔗 启动 WebSocket 价格流...")
	if err := priceMonitor.Start(); err != nil {
		logger.Error("❌ 启动价格流失败（WebSocket 是唯一价格来源）: %v", err)
		logger.Warn("⚠️ 价格监控失败，但 Web 服务器已启动，可通过 Web 界面查看状态")
		logger.Info("💡 提示：请检查网络连接或代理设置，然后重启服务")
		// 不退出，允许 Web 服务器继续运行，等待信号退出
		// 继续执行到信号监听部分，允许优雅退出
		goto waitForSignal
	}

	// 6. 等待从 WebSocket 获取初始价格
	logger.Debugln("⏳ 等待 WebSocket 推送初始价格...")
	pollInterval = time.Duration(cfg.Timing.PricePollInterval) * time.Millisecond
	for i := 0; i < 10; i++ {
		currentPrice = priceMonitor.GetLastPrice()
		currentPriceStr = priceMonitor.GetLastPriceString()
		if currentPrice > 0 {
			break
		}
		time.Sleep(pollInterval)
	}

	if currentPrice <= 0 {
		logger.Error("❌ 无法从 WebSocket 获取价格（超时）")
		logger.Warn("⚠️ 价格监控失败，但 Web 服务器已启动，可通过 Web 界面查看状态")
		logger.Info("💡 提示：请检查网络连接或代理设置，然后重启服务")
		// 不退出，允许 Web 服务器继续运行，等待信号退出
		// 继续执行到信号监听部分，允许优雅退出
		goto waitForSignal
	}

	// 从交易所获取精度信息
	priceDecimals = ex.GetPriceDecimals()
	quantityDecimals = ex.GetQuantityDecimals()
	logger.Info("ℹ️ 交易精度 - 价格精度:%d, 数量精度:%d", priceDecimals, quantityDecimals)
	logger.Debug("📊 当前价格: %.*f", priceDecimals, currentPrice)

	// 6. 持仓安全性检查（必须在开始交易之前执行）
	requiredPositions = cfg.Trading.PositionSafetyCheck
	if requiredPositions <= 0 {
		requiredPositions = 100 // 默认100
	}

	// 获取当前交易所的手续费率
	exchangeCfg = cfg.Exchanges[cfg.App.CurrentExchange]
	feeRate = exchangeCfg.FeeRate
	// 注意：支持0费率，不需要特殊处理

	// 执行持仓安全性检查（使用独立的 safety 包）
	// 变量已在前面声明，这里直接赋值
	maxLeverage = cfg.RiskControl.MaxLeverage
	if err := safety.CheckAccountSafety(
		ex,
		cfg.Trading.Symbol,
		currentPrice,
		cfg.Trading.OrderQuantity,
		cfg.Trading.PriceInterval,
		feeRate,
		requiredPositions,
		priceDecimals,
		maxLeverage,
	); err != nil {
		logger.Error("❌ 持仓安全性检查失败: %v", err)
		logger.Warn("⚠️ 系统将以【仅监控模式】运行，不会进行实际交易")
		logger.Info("💡 提示：请配置正确的 API Key 后重启服务以启用交易功能")
		logger.Info("💡 Web 服务器已启动，可通过 Web 界面查看系统状态")
		// 不退出，允许 Web 服务器继续运行，等待信号退出
		goto waitForSignal
	}
	logger.Info("✅ 持仓安全性检查通过，开始初始化交易组件...")

	// 8. 创建事件系统、通知服务和存储服务
	eventBus = event.NewEventBus(1000) // 缓冲区1000

	// 创建通知服务
	notifier = notify.NewNotificationService(cfg)

	// 创建存储服务（使用前面创建的 ctx）

	storageService, err = storage.NewStorageService(cfg, ctx)
	if err != nil {
		logger.Warn("⚠️ 初始化存储服务失败: %v (将继续运行，但不保存数据)", err)
		storageService = nil
	} else if cfg.Storage.Enabled {
		storageService.Start()
	}

	// 启动事件处理器（在 main.go 中实现，避免循环依赖）
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-eventBus.Subscribe():
				if evt == nil {
					continue
				}
				// 异步处理：不阻塞
				go func(e *event.Event) {
					// 1. 发送通知（异步，不等待）
					if notifier != nil {
						notifier.Send(e)
					}

					// 2. 保存到数据库（异步，不等待）
					if storageService != nil {
						storageService.Save(string(e.Type), e.Data)
					}
				}(evt)
			}
		}
	}()

	// 发布系统启动事件
	eventBus.Publish(&event.Event{
		Type: event.EventTypeSystemStart,
		Data: map[string]interface{}{
			"exchange": cfg.App.CurrentExchange,
			"symbol":   cfg.Trading.Symbol,
			"version":  Version,
		},
	})

	// 9. 创建核心组件
	exchangeExecutor = order.NewExchangeOrderExecutor(
		ex,
		cfg.Trading.Symbol,
		cfg.Timing.RateLimitRetryDelay,
		cfg.Timing.OrderRetryDelay,
	)
	// 创建带事件发布的执行器适配器
	executorAdapter = &exchangeExecutorAdapter{
		executor:  exchangeExecutor,
		eventBus:  eventBus,
		symbol:    cfg.Trading.Symbol,
	}

	// 创建交易所适配器（匹配 position.IExchange 接口）
	exchangeAdapter = &positionExchangeAdapter{exchange: ex}
	superPositionManager = position.NewSuperPositionManager(cfg, executorAdapter, exchangeAdapter, priceDecimals, quantityDecimals)
	// 设置交易存储适配器（用于保存交易记录）
	if storageService != nil {
		tradeStorageAdapter := &tradeStorageAdapter{storageService: storageService}
		superPositionManager.SetTradeStorage(tradeStorageAdapter)
	}

	// === 多策略系统集成（提前声明，以便在订单更新回调中使用） ===
	// 变量已在前面声明

	// === 新增：初始化风控监视器 ===
	riskMonitor = safety.NewRiskMonitor(cfg, ex)
	// 设置存储服务（用于保存检查历史）
	if storageService != nil {
		riskMonitor.SetStorage(storageService.GetStorage())
	}

	// === 创建对账器（从仓位管理器剖离） ===
	reconciler = safety.NewReconciler(cfg, exchangeAdapter, superPositionManager)
	// 将风控状态注入到对账器，用于暂停对账日志
	reconciler.SetPauseChecker(func() bool {
		return riskMonitor.IsTriggered()
	})
	// 将对账存储服务注入到对账器
	if storageService != nil {
		reconciler.SetStorage(&reconciliationStorageAdapter{storageService: storageService})
	}

	// 🔥 关键修复：先启动订单流，再下单（避免错过成交推送）
	// 启动订单流（通过交易所接口）
	// 架构说明：
	// - 订单流与价格流共用同一个 WebSocket 连接（对于支持的交易所）
	// - 订单更新通过回调函数实时推送给 SuperPositionManager
	//logger.Info("🔗 启动 WebSocket 订单流...")
	if err := ex.StartOrderStream(ctx, func(updateInterface interface{}) {
		// 使用反射提取字段（兼容匿名结构体）
		v := reflect.ValueOf(updateInterface)
		if v.Kind() != reflect.Struct {
			logger.Warn("⚠️ [main.go] 订单更新不是结构体类型: %T", updateInterface)
			return
		}

		// 提取字段值的辅助函数
		getInt64Field := func(name string) int64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanInt() {
				return field.Int()
			}
			return 0
		}

		getStringField := func(name string) string {
			field := v.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.String {
				return field.String()
			}
			return ""
		}

		getFloat64Field := func(name string) float64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanFloat() {
				return field.Float()
			}
			return 0.0
		}

		// 提取所有字段
		posUpdate := position.OrderUpdate{
			OrderID:       getInt64Field("OrderID"),
			ClientOrderID: getStringField("ClientOrderID"), // 🔥 关键：传递 ClientOrderID
			Symbol:        getStringField("Symbol"),
			Status:        getStringField("Status"),
			ExecutedQty:   getFloat64Field("ExecutedQty"),
			Price:         getFloat64Field("Price"),
			AvgPrice:      getFloat64Field("AvgPrice"),
			Side:          getStringField("Side"),
			Type:          getStringField("Type"),
			UpdateTime:    getInt64Field("UpdateTime"),
		}

		logger.Debug("🔍 [main.go] 收到订单更新回调: ID=%d, ClientOID=%s, Price=%.2f, Status=%s",
			posUpdate.OrderID, posUpdate.ClientOrderID, posUpdate.Price, posUpdate.Status)

		// 发布订单更新事件
		var eventType event.EventType
		switch posUpdate.Status {
		case "FILLED":
			eventType = event.EventTypeOrderFilled
		case "CANCELED":
			eventType = event.EventTypeOrderCanceled
		default:
			// 其他状态不发布事件
		}

		if eventType != "" {
			eventBus.Publish(&event.Event{
				Type: eventType,
				Data: map[string]interface{}{
					"order_id":       posUpdate.OrderID,
					"client_order_id": posUpdate.ClientOrderID,
					"symbol":          posUpdate.Symbol,
					"side":            posUpdate.Side,
					"price":           posUpdate.Price,
					"executed_qty":    posUpdate.ExecutedQty,
					"status":          posUpdate.Status,
				},
			})
		}

		superPositionManager.OnOrderUpdate(posUpdate)

		// === 多策略系统：通知所有策略订单更新 ===
		if strategyManager != nil {
			strategyManager.OnOrderUpdate(&posUpdate)
		}
	}); err != nil {
		logger.Warn("⚠️ 启动订单流失败: %v (将继续运行，但订单状态更新可能延迟)", err)
	} else {
		logger.Info("✅ [%s] 订单流已启动", ex.GetName())
	}

	// 初始化超级仓位管理器（设置价格锚点并创建初始槽位）
	// 注意：必须在订单流启动后再初始化，避免错过买单成交推送
	if err := superPositionManager.Initialize(currentPrice, currentPriceStr); err != nil {
		logger.Fatalf("❌ 初始化超级仓位管理器失败: %v", err)
	}

	// 启动持仓对账（使用独立的 Reconciler）
	reconciler.Start(ctx)

	// === 创建订单清理器（从仓位管理器剥离） ===
	// 变量已在前面声明，这里直接赋值
	orderCleaner = safety.NewOrderCleaner(cfg, exchangeExecutor, superPositionManager)
	// 启动订单清理协程
	orderCleaner.Start(ctx)

	// 启动价格监控（WebSocket 是唯一的价格来源）
	// 注意：毫秒级量化系统不支持 REST API 轮询，WebSocket 失败时系统将停止
	go func() {
		// 检查是否已经在运行
		if err := priceMonitor.Start(); err != nil {
			// 忽略"已在运行"的错误
			if err.Error() != "价格监控已在运行" {
				logger.Fatalf("❌ 启动价格监控失败（WebSocket 必须可用）: %v", err)
			}
		}
	}()

	// 启动风控监控
	go riskMonitor.Start(ctx)

	// === 新增：启动看门狗监控 ===
	// 变量已在前面声明，这里直接使用
	if cfg.Watchdog.Enabled {
		watchdog = watchdogMonitor.NewWatchdog(cfg, storageService, notifier)
		if err := watchdog.Start(ctx); err != nil {
			logger.Error("❌ 启动看门狗监控失败: %v", err)
		}
	}

	// === 初始化Web服务器的系统监控数据提供者和日志存储提供者（Web服务器已在前面启动）===
	if webServer != nil {
		if watchdog != nil && storageService != nil {
			metricsProvider := web.NewSystemMetricsProvider(storageService, watchdog)
			web.SetSystemMetricsProvider(metricsProvider)
		}
		// 注册日志存储提供者
		if globalLogStorage != nil {
			logAdapter := web.NewLogStorageAdapter(globalLogStorage)
			web.SetLogStorageProvider(logAdapter)
			// 设置日志存储用于 WebSocket 推送
			web.SetLogStorage(globalLogStorage)
		}

		// 设置槽位数据提供者（在 superPositionManager 创建并初始化后）
		if superPositionManager != nil {
			positionAdapter := web.NewPositionManagerAdapter(superPositionManager)
			web.SetPositionManagerProvider(positionAdapter)
			// 设置订单金额配置（用于计算订单数量）
			web.SetOrderQuantityConfig(cfg.Trading.OrderQuantity)
		}

		// 设置价格提供者（用于计算持仓价值）
		if priceMonitor != nil {
			web.SetPriceProvider(priceMonitor)
		}

		// 设置交易所提供者（用于获取K线数据）
		if ex != nil {
			exchangeAdapter := &exchangeProviderAdapter{exchange: ex}
			web.SetExchangeProvider(exchangeAdapter)
		}

		// 设置存储服务提供者（用于查询历史数据）
		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
		}

		// 设置风控监控提供者
		if riskMonitor != nil {
			web.SetRiskMonitorProvider(riskMonitor)
		}

		// 初始化认证系统
		dataDir := "./data"
		if cfg.Storage.Enabled && cfg.Storage.Path != "" {
			// 使用存储配置的数据目录
			dataDir = filepath.Dir(cfg.Storage.Path)
		}

		// 创建密码管理器
		passwordManager, err := web.NewPasswordManager(dataDir)
		if err != nil {
			logger.Warn("⚠️ 初始化密码管理器失败: %v（认证功能将不可用）", err)
		} else {
			web.SetPasswordManager(passwordManager)
			logger.Info("✅ 密码管理器已初始化")
		}

		// 创建会话管理器
		sessionManager := web.GetSessionManager()
		web.SetSessionManager(sessionManager)
		logger.Info("✅ 会话管理器已初始化")

		// 创建 WebAuthn 管理器
		// 确定 RPID 和 RPOrigin
		rpID := cfg.Web.Host
		if rpID == "" || rpID == "0.0.0.0" {
			rpID = "localhost"
		}
		// 移除端口号（如果有）
		if idx := strings.Index(rpID, ":"); idx != -1 {
			rpID = rpID[:idx]
		}
		// 检查是否是 IP 地址，如果是则使用 localhost
		if net.ParseIP(rpID) != nil {
			rpID = "localhost"
		}

		// 确定 RPOrigin
		port := cfg.Web.Port
		if port == 0 {
			port = 8080
		}
		protocol := "http"
		rpOrigin := protocol + "://" + rpID
		if port != 80 && port != 443 {
			rpOrigin = fmt.Sprintf("%s://%s:%d", protocol, rpID, port)
		}

		webauthnLogger := &loggerAdapter{}
		webauthnManager, err := web.NewWebAuthnManager(webauthnLogger, dataDir, rpID, rpOrigin)
		if err != nil {
			logger.Warn("⚠️ WebAuthn 初始化失败（功能将不可用）: %v", err)
		} else {
			web.SetWebAuthnManager(webauthnManager)
			logger.Info("✅ WebAuthn 管理器已初始化，RPID: %s, RPOrigin: %s", rpID, rpOrigin)
		}
	}

	// === 新增：启动动态调整器和趋势检测器 ===
	// 变量已在前面声明，这里直接使用

	if cfg.Trading.DynamicAdjustment.Enabled {
		dynamicAdjuster = strategy.NewDynamicAdjuster(cfg, priceMonitor, superPositionManager)
		dynamicAdjuster.Start()
	}

	if cfg.Trading.SmartPosition.Enabled {
		trendDetector = strategy.NewTrendDetector(cfg, priceMonitor)
		trendDetector.Start()
	}

	// === 多策略系统集成 ===
	if cfg.Strategies.Enabled {
		// 获取总资金（从配置或账户余额）
		totalCapital = cfg.Strategies.CapitalAllocation.TotalCapital
		if totalCapital <= 0 {
			// 如果没有配置，尝试从账户获取余额
			balance, err := ex.GetBalance(ctx, "USDT")
			if err == nil && balance > 0 {
				totalCapital = balance
				logger.Info("💰 从账户获取总资金: %.2f USDT", totalCapital)
			} else {
				totalCapital = 5000 // 默认值
				logger.Warn("⚠️ 无法获取账户余额，使用默认总资金: %.2f USDT", totalCapital)
			}
		}

		// 创建策略管理器
		strategyManager = strategy.NewStrategyManager(cfg, totalCapital)

		// 创建多策略订单执行器
		multiExecutor = strategy.NewMultiStrategyExecutor(exchangeExecutor, strategyManager.GetCapitalAllocator())

		// 注册网格策略（如果启用）
		if gridCfg, exists := cfg.Strategies.Configs["grid"]; exists && gridCfg.Enabled {
			// 网格策略使用原有的 executorAdapter（因为 SuperPositionManager 需要它）
			gridStrategy := strategy.NewGridStrategy("grid", cfg, executorAdapter, exchangeAdapter, superPositionManager)
			fixedPool := 0.0
			if pool, ok := gridCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("grid", gridStrategy, gridCfg.Weight, fixedPool)
			logger.Info("✅ 网格策略已注册 (权重: %.2f%%)", gridCfg.Weight*100)
		}

		// 注册趋势跟踪策略（如果启用）
		if trendCfg, exists := cfg.Strategies.Configs["trend"]; exists && trendCfg.Enabled {
			trendExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "trend")
			trendStrategy := strategy.NewTrendFollowingStrategy("trend", cfg, trendExecutor, exchangeAdapter, trendCfg.Config)
			fixedPool := 0.0
			if pool, ok := trendCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("trend", trendStrategy, trendCfg.Weight, fixedPool)
			logger.Info("✅ 趋势跟踪策略已注册 (权重: %.2f%%)", trendCfg.Weight*100)
		}

		// 注册均值回归策略（如果启用）
		if meanCfg, exists := cfg.Strategies.Configs["mean_reversion"]; exists && meanCfg.Enabled {
			meanExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "mean_reversion")
			meanStrategy := strategy.NewMeanReversionStrategy("mean_reversion", cfg, meanExecutor, exchangeAdapter, meanCfg.Config)
			fixedPool := 0.0
			if pool, ok := meanCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("mean_reversion", meanStrategy, meanCfg.Weight, fixedPool)
			logger.Info("✅ 均值回归策略已注册 (权重: %.2f%%)", meanCfg.Weight*100)
		}

		// 注册动量策略（如果启用）
		if momentumCfg, exists := cfg.Strategies.Configs["momentum"]; exists && momentumCfg.Enabled {
			momentumExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "momentum")
			momentumStrategy := strategy.NewMomentumStrategy("momentum", cfg, momentumExecutor, exchangeAdapter, momentumCfg.Config)
			fixedPool := 0.0
			if pool, ok := momentumCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("momentum", momentumStrategy, momentumCfg.Weight, fixedPool)
			logger.Info("✅ 动量策略已注册 (权重: %.2f%%)", momentumCfg.Weight*100)
		}

		// 启动所有策略
		if err := strategyManager.StartAll(); err != nil {
			logger.Error("❌ 启动策略管理器失败: %v", err)
		} else {
			logger.Info("✅ 多策略系统已启动")
		}

		// 设置策略资金分配提供者（在策略管理器启动后）
		if webServer != nil && strategyManager != nil {
			allocator := strategyManager.GetCapitalAllocator()
			strategyAdapter := web.NewStrategyProviderAdapter(func() map[string]web.StrategyCapitalInfo {
				capitalMap := allocator.GetAllStrategiesCapital()
				result := make(map[string]web.StrategyCapitalInfo)
				for name, capital := range capitalMap {
					result[name] = web.StrategyCapitalInfo{
						Allocated: capital.Allocated,
						Used:      capital.Used,
						Available: capital.Available,
						Weight:    capital.Weight,
						FixedPool: capital.FixedPool,
					}
				}
				return result
			})
			web.SetStrategyProvider(strategyAdapter)
		}
	}

	// 10. 监听价格变化,调整订单窗口（实时调整，不打印价格变化日志）
	go func() {
		priceCh := priceMonitor.Subscribe()
		var lastTriggered bool // 记录上一次的风控状态，用于检测状态切换

		for priceChange := range priceCh {
			// === 风控检查：触发时撤销所有买单并暂停交易 ===
			isTriggered := riskMonitor.IsTriggered()

			if isTriggered {
				// 检测状态切换：从未触发 -> 触发（首次触发）
				if !lastTriggered {
					logger.Warn("🚨 [风控触发] 市场异常，正在撤销所有买单并暂停交易...")
					superPositionManager.CancelAllBuyOrders() // 🔥 只撤销买单，保留卖单
					lastTriggered = true

					// 发布风控触发事件
					eventBus.Publish(&event.Event{
						Type: event.EventTypeRiskTriggered,
						Data: map[string]interface{}{
							"price": priceChange.NewPrice,
						},
					})
				}
				// 风控触发期间跳过后续下单逻辑
				continue
			}

			// 检测状态切换：从触发 -> 未触发（风控解除）
			if lastTriggered {
				logger.Info("✅ [风控解除] 市场恢复正常，恢复自动交易")
				lastTriggered = false

				// 发布风控解除事件
				eventBus.Publish(&event.Event{
					Type: event.EventTypeRiskRecovered,
					Data: map[string]interface{}{
						"price": priceChange.NewPrice,
					},
				})
			}

			// === 多策略系统：通知所有策略价格变化 ===
			if strategyManager != nil {
				strategyManager.OnPriceChange(priceChange.NewPrice)
			}

			// === 智能仓位管理：根据趋势调整窗口大小 ===
			if trendDetector != nil && cfg.Trading.SmartPosition.WindowAdjustment.Enabled {
				buyWindow, sellWindow := trendDetector.AdjustWindows()
				// 临时更新配置中的窗口大小（用于本次 AdjustOrders）
				originalBuyWindow := cfg.Trading.BuyWindowSize
				originalSellWindow := cfg.Trading.SellWindowSize
				cfg.Trading.BuyWindowSize = buyWindow
				cfg.Trading.SellWindowSize = sellWindow
				// 调整订单（仅网格策略）
				if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
					logger.Error("❌ 调整订单失败: %v", err)
				}
				// 恢复原始窗口大小（避免影响其他逻辑）
				cfg.Trading.BuyWindowSize = originalBuyWindow
				cfg.Trading.SellWindowSize = originalSellWindow
			} else {
				// 实时调整订单，不打印价格变化日志（避免日志过多）
				// 注意：如果启用了多策略系统，网格策略会通过策略管理器处理
				if strategyManager == nil || !cfg.Strategies.Enabled {
					if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
						logger.Error("❌ 调整订单失败: %v", err)
					}
				}
			}
		}
	}()

	// 13. 定期打印持仓和订单状态
	go func() {
		statusInterval := time.Duration(cfg.Timing.StatusPrintInterval) * time.Minute
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 风控触发时不打印状态
				if !riskMonitor.IsTriggered() {
					superPositionManager.PrintPositions()
				}
			}
		}
	}()

	// 14. 启动日志清理任务（每天清理一次超过7天的日志）
	if globalLogStorage != nil {
		go func() {
			// 计算到下一个凌晨的时间
			now := time.Now()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			initialDelay := nextMidnight.Sub(now)
			
			// 等待到第一个凌晨
			time.Sleep(initialDelay)
			
			// 每天凌晨执行一次清理
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			
			// 立即执行一次清理（启动时）
			if err := globalLogStorage.CleanOldLogs(7); err != nil {
				logger.Warn("⚠️ 清理旧日志失败: %v", err)
			} else {
				logger.Info("✅ 已清理超过7天的日志")
			}
			
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := globalLogStorage.CleanOldLogs(7); err != nil {
						logger.Warn("⚠️ 清理旧日志失败: %v", err)
					} else {
						logger.Debug("✅ 已清理超过7天的日志")
					}
				}
			}
		}()
	}

	// 15. 等待退出信号
waitForSignal:
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

	// 🔥 第一优先级：立即撤销所有订单（最重要！）
	// 使用独立的超时 context，确保撤单请求能发送成功
	if cfg.System.CancelOnExit {
		logger.Info("🔄 正在撤销所有订单（最高优先级）...")
		cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
		if err := ex.CancelAllOrders(cancelCtx, cfg.Trading.Symbol); err != nil {
			logger.Error("❌ 撤销订单失败: %v", err)
		} else {
			logger.Info("✅ 所有订单已成功撤销")
		}
		cancelTimeout()
	}

	// 🔥 第二优先级：优雅停止各个组件（按依赖关系从上到下）
	// 注意：这些组件的 Stop() 方法内部会处理 WebSocket 关闭等清理工作
	logger.Info("⏹️ 正在停止价格监控...")
	if priceMonitor != nil {
		priceMonitor.Stop()
	}

	logger.Info("⏹️ 正在停止订单流...")
	ex.StopOrderStream()

	logger.Info("⏹️ 正在停止风控监视器...")
	if riskMonitor != nil {
		riskMonitor.Stop()
	}

	logger.Info("⏹️ 正在停止动态调整器...")
	if dynamicAdjuster != nil {
		dynamicAdjuster.Stop()
	}

	logger.Info("⏹️ 正在停止趋势检测器...")
	if trendDetector != nil {
		trendDetector.Stop()
	}

	logger.Info("⏹️ 正在停止策略管理器...")
	if strategyManager != nil {
		strategyManager.StopAll()
	}

	logger.Info("⏹️ 正在停止看门狗监控...")
	if watchdog != nil {
		watchdog.Stop()
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

	// 打印最终状态
	if superPositionManager != nil {
		superPositionManager.PrintPositions()
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
				"order_id":       ord.OrderID,
				"client_order_id": ord.ClientOrderID,
				"symbol":          ord.Symbol,
				"side":            ord.Side,
				"price":           ord.Price,
				"quantity":       ord.Quantity,
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
					"order_id":       ord.OrderID,
					"client_order_id": ord.ClientOrderID,
					"symbol":          ord.Symbol,
					"side":            ord.Side,
					"price":           ord.Price,
					"quantity":       ord.Quantity,
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
