package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/notify"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
	"quantmesh/web"
)

// Version 版本号
var Version = "v3.3.2"

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
	}

	logger.Info("🚀 QuantMesh 做市商系统启动...")
	logger.Info("📦 版本号: %s", Version)

	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Fatalf("❌ 加载配置失败: %v", err)
	}

	if err := utils.SetLocation(cfg.System.Timezone); err != nil {
		logger.Warn("⚠️ 加载时区 %s 失败: %v，将使用默认时区 Asia/Shanghai", cfg.System.Timezone, err)
		utils.SetLocation("Asia/Shanghai")
	} else {
		logger.Info("✅ 系统时区设置为: %s", cfg.System.Timezone)
	}
	logger.SetLocation(utils.GlobalLocation)

	logLevel := logger.ParseLogLevel(cfg.System.LogLevel)
	logger.SetLevel(logLevel)
	logger.Info("日志级别设置为: %s", logLevel.String())

	logger.Info("✅ 配置加载成功: 交易对数量=%d, 当前默认交易所=%s",
		len(cfg.Trading.Symbols), cfg.App.CurrentExchange)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 事件总线 & 通知 & 存储
	eventBus := event.NewEventBus(1000)
	notifier := notify.NewNotificationService(cfg)

	storageService, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		logger.Warn("⚠️ 初始化存储服务失败: %v (将继续运行，但不保存数据)", err)
		storageService = nil
	} else if cfg.Storage.Enabled {
		storageService.Start()
	}

	// 事件处理器
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
					if notifier != nil {
						notifier.Send(e)
					}
					if storageService != nil {
						storageService.Save(string(e.Type), e.Data)
					}
				}(evt)
			}
		}
	}()

	// Web 服务器
	var webServer *web.WebServer
	if cfg.Web.Enabled {
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

		// 初始化配置备份管理器
		backupManager := config.NewBackupManager()
		web.SetConfigBackupManager(backupManager)
		logger.Info("✅ 配置备份管理器已初始化")

		// 初始化配置热更新器
		hotReloader := config.NewHotReloader(cfg)
		web.SetConfigHotReloader(hotReloader)
		logger.Info("✅ 配置热更新器已初始化")

		webServer = web.NewWebServer(cfg)
		if err := webServer.Start(ctx); err != nil {
			logger.Error("❌ 启动Web服务器失败: %v", err)
		} else {
			logger.Info("✅ Web服务器已启动，可通过 http://%s:%d 访问", cfg.Web.Host, cfg.Web.Port)
		}
	}

	symbolManager := NewSymbolManager(cfg)

	// 启动所有交易对
	var firstRuntime *SymbolRuntime
	for _, symCfg := range cfg.Trading.Symbols {
		rt, err := startSymbolRuntime(ctx, cfg, symCfg, eventBus, storageService)
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
		logger.Fatalf("❌ 所有交易对启动失败，无法继续运行")
	}

	// Web 绑定数据提供者（兼容旧前端：使用第一个运行时，同时注册多交易对）
	if webServer != nil {
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
						if r.SuperPositionManager != nil {
							totalBuyQty := r.SuperPositionManager.GetTotalBuyQty()
							totalSellQty := r.SuperPositionManager.GetTotalSellQty()
							priceInterval := r.SuperPositionManager.GetPriceInterval()
							st.TotalPnL = totalSellQty * priceInterval
							st.TotalTrades = int((totalBuyQty + totalSellQty) / (r.Config.OrderQuantity * 2))
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

		if storageService != nil {
			storageAdapter := web.NewStorageServiceAdapter(storageService)
			web.SetStorageServiceProvider(storageAdapter)
		}
	}

	// 资金费率监控（复用旧逻辑，默认主流交易对）
	if webServer != nil && storageService != nil && firstRuntime != nil {
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
	}

	logger.Info("✅ 所有交易对已初始化，进入运行状态")

	// 6. 等待从 WebSocket 获取初始价格
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

	// 🔥 第一优先级：撤销各交易对的订单
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
	for _, rt := range symbolManager.List() {
		if rt.SuperPositionManager != nil {
			rt.SuperPositionManager.PrintPositions()
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
