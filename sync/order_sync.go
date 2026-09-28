package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

const defaultOrderSyncInterval = 5 * time.Minute

// OrderSyncService 订单同步服务
// 定期从交易所拉取历史成交记录，补全缺失的订单
type OrderSyncService struct {
	exchange     exchange.IExchange
	storage      storage.Storage
	symbol       string
	accountID    string
	exchangeName string
	marketType   string
	accountScope string
	syncInterval time.Duration
	mu           sync.RWMutex
	isRunning    bool
	stopC        chan struct{}
}

func (s *OrderSyncService) SetTradeScope(marketType, accountScope string) {
	s.marketType = marketType
	s.accountScope = accountScope
}

// NewOrderSyncService 创建订单同步服务
func NewOrderSyncService(
	ex exchange.IExchange,
	st storage.Storage,
	symbol, accountID, exchangeName string,
	syncInterval time.Duration,
) *OrderSyncService {
	return &OrderSyncService{
		exchange:     ex,
		storage:      st,
		symbol:       symbol,
		accountID:    accountID,
		exchangeName: exchangeName,
		syncInterval: syncInterval,
	}
}

// Start 启动订单同步服务
func (s *OrderSyncService) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		logger.Warn("⚠️ [订单同步] 服务已在运行")
		return
	}
	if s.syncInterval <= 0 {
		logger.Warn("⚠️ [订单同步] 同步间隔配置无效: %v，使用默认值 %v", s.syncInterval, defaultOrderSyncInterval)
		s.syncInterval = defaultOrderSyncInterval
	}
	stopC := make(chan struct{})
	s.stopC = stopC
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [订单同步] 启动订单同步服务 (symbol=%s, interval=%v)", s.symbol, s.syncInterval)

	go func() {
		ticker := time.NewTicker(s.syncInterval)
		defer ticker.Stop()
		defer s.markStopped(stopC)

		// 启动时立即同步一次
		if err := s.Sync(ctx); err != nil {
			logger.Error("❌ [订单同步] 初始同步失败: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ [订单同步] 服务已停止")
				return
			case <-stopC:
				logger.Info("⏹️ [订单同步] 服务已停止")
				return
			case <-ticker.C:
				if err := s.Sync(ctx); err != nil {
					logger.Error("❌ [订单同步] 同步失败: %v", err)
				}
			}
		}
	}()
}

func (s *OrderSyncService) markStopped(stopC chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopC != stopC {
		return
	}
	s.stopC = nil
	s.isRunning = false
}

// Stop 停止订单同步服务
func (s *OrderSyncService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	close(s.stopC)
	s.stopC = nil
	s.isRunning = false
	logger.Info("⏹️ [订单同步] 订单同步服务已停止")
}

// Sync 执行订单同步
func (s *OrderSyncService) Sync(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.exchange == nil || s.storage == nil {
		logger.Warn("⚠️ [订单同步] 交易所或存储为空，跳过同步")
		return nil
	}

	const maxHistoryWindow = 30*24*time.Hour - time.Second
	now := time.Now().UTC()
	start := now.Add(-maxHistoryWindow)
	startTime := start.UnixMilli()
	endTime := now.UnixMilli()

	historySource, ok := s.exchange.(exchange.OrderHistoryPageSource)
	if !ok {
		return fmt.Errorf("order history sync is unsupported for exchange %q: paginated execution history is unavailable", s.exchangeName)
	}

	// Each source owns venue-specific cursor semantics; coverage advances only at an explicit terminal page.
	const pageSize = 100
	const maxTradePages = 1000
	if s.marketType == "" || s.accountScope == "" {
		return fmt.Errorf("order synchronization requires complete account scope")
	}
	coverageReader, ok := s.storage.(interface {
		GetOrderFillCoverage(exchange, marketType, symbol, accountScope string) (*storage.OrderFillCoverage, error)
	})
	if !ok {
		return fmt.Errorf("order synchronization requires persistent history coverage")
	}
	coverageWriter, ok := s.storage.(interface {
		AdvanceOrderFillCoverage(exchange, marketType, symbol, accountScope string, from, through time.Time) error
	})
	if !ok {
		return fmt.Errorf("order synchronization requires persistent history coverage")
	}
	coverage, err := coverageReader.GetOrderFillCoverage(s.exchangeName, s.marketType, s.symbol, s.accountScope)
	if err != nil {
		return fmt.Errorf("load persistent history coverage: %w", err)
	}
	if coverage != nil {
		start = coverage.CoveredThrough.Add(-10 * time.Minute)
		startTime = start.UnixMilli()
	}
	if windowEnd := start.Add(maxHistoryWindow); windowEnd.UnixMilli() < endTime {
		endTime = windowEnd.UnixMilli()
	}
	logger.Info("🔄 [订单同步] 开始同步订单 (symbol=%s, startTime=%s, endTime=%s)",
		s.symbol, time.UnixMilli(startTime).Format("2006-01-02 15:04:05"),
		time.UnixMilli(endTime).Format("2006-01-02 15:04:05"))
	orderIDReader, ok := s.storage.(interface {
		GetExistingOrderIDsForScope(exchange, marketType, symbol, accountScope string, orderIDs []int64) (map[int64]bool, error)
	})
	if !ok {
		return fmt.Errorf("order synchronization requires scoped order lookup storage")
	}
	fillWriter, ok := s.storage.(interface {
		SaveOrderFill(*storage.OrderFill) error
	})
	if !ok {
		return fmt.Errorf("order synchronization requires idempotent execution-ledger storage")
	}
	cursor := ""
	var totalTrades, syncedOrders int
	complete := false
	for page := 0; page < maxTradePages; page++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("order history sync canceled before all pages were collected: %w", err)
		}
		result, err := historySource.GetOrderHistoryPage(ctx, s.symbol, startTime, endTime, cursor, pageSize)
		if err != nil {
			return fmt.Errorf("fetch %s execution history page %d: %w", s.exchangeName, page+1, err)
		}
		if len(result.Fills) == 0 {
			if result.HasMore {
				return fmt.Errorf("%s returned an empty execution page with more pages available", s.exchangeName)
			}
			complete = true
			break
		}
		inWindow := make([]*exchange.OrderFill, 0, len(result.Fills))
		pastWindow := false
		for _, row := range result.Fills {
			if row == nil || row.TradeID == "" || row.OrderID <= 0 || row.TradeTime <= 0 || row.Symbol != s.symbol {
				return fmt.Errorf("exchange returned an invalid execution history row")
			}
			if row.TradeTime < startTime {
				return fmt.Errorf("exchange returned execution older than requested history window")
			}
			if row.TradeTime > endTime {
				pastWindow = true
				continue
			}
			inWindow = append(inWindow, row)
		}
		added, err := s.persistTradePage(inWindow, orderIDReader, fillWriter)
		if err != nil {
			return fmt.Errorf("persist order history page %d: %w", page+1, err)
		}
		totalTrades += len(inWindow)
		syncedOrders += added
		if pastWindow || !result.HasMore {
			complete = true
			break
		}
		if result.NextCursor == "" || result.NextCursor == cursor {
			return fmt.Errorf("%s returned an invalid execution pagination cursor", s.exchangeName)
		}
		cursor = result.NextCursor
		if page == maxTradePages-1 {
			return fmt.Errorf("order history exceeded %d pages; refusing to advance sync watermark", maxTradePages)
		}
	}
	if !complete {
		return fmt.Errorf("order history pagination ended without a completeness boundary")
	}
	if totalTrades == 0 {
		logger.Debug("📭 [订单同步] 没有新的成交记录")
	} else {
		logger.Info("📊 [订单同步] 完整读取 %d 条成交记录", totalTrades)
	}
	if err := coverageWriter.AdvanceOrderFillCoverage(s.exchangeName, s.marketType, s.symbol, s.accountScope,
		time.UnixMilli(startTime).UTC(), time.UnixMilli(endTime).UTC()); err != nil {
		return fmt.Errorf("persist complete history coverage: %w", err)
	}
	if syncedOrders > 0 {
		logger.Info("✅ [订单同步] 同步完成: 新增 %d 个订单", syncedOrders)
	}
	return nil
}

func (s *OrderSyncService) persistTradePage(trades []*exchange.OrderFill, orderIDReader interface {
	GetExistingOrderIDsForScope(exchange, marketType, symbol, accountScope string, orderIDs []int64) (map[int64]bool, error)
}, fillWriter interface {
	SaveOrderFill(*storage.OrderFill) error
}) (int, error) {
	orderIDs := make([]int64, 0, len(trades))
	seenOrderIDs := make(map[int64]bool, len(trades))
	for _, trade := range trades {
		if trade == nil {
			return 0, fmt.Errorf("exchange returned an empty trade record")
		}
		if trade.TradeID == "" || trade.OrderID <= 0 || trade.TradeTime <= 0 || trade.Symbol != s.symbol {
			return 0, fmt.Errorf("exchange returned a trade without valid identity, time, or requested symbol")
		}
		if !seenOrderIDs[trade.OrderID] {
			orderIDs = append(orderIDs, trade.OrderID)
			seenOrderIDs[trade.OrderID] = true
		}
	}
	existingOrderIDs, err := orderIDReader.GetExistingOrderIDsForScope(s.exchangeName, s.marketType, s.symbol, s.accountScope, orderIDs)
	if err != nil {
		return 0, fmt.Errorf("query scoped existing orders: %w", err)
	}
	syncedCount := 0
	for _, trade := range trades {
		fill := &storage.OrderFill{
			Exchange: s.exchangeName, MarketType: s.marketType, AccountScope: s.accountScope,
			Account: s.accountID, Symbol: trade.Symbol, TradeID: trade.TradeID,
			OrderID: trade.OrderID, Side: string(trade.Side), Price: trade.Price, Quantity: trade.Quantity,
			QuoteQuantity: trade.QuoteQuantity,
			Commission:    trade.Commission, CommissionAsset: trade.CommissionAsset,
			CommissionQuote: trade.CommissionQuote, CommissionQuoteRate: trade.CommissionQuoteRate, CommissionQuoteKnown: trade.CommissionQuoteKnown,
			TradeTime: time.UnixMilli(trade.TradeTime).UTC(),
		}
		if trade.RealizedPnLKnown {
			pnl := trade.RealizedPnL
			fill.RealizedPnL = &pnl
		}
		if err := fillWriter.SaveOrderFill(fill); err != nil {
			return syncedCount, fmt.Errorf("persist exchange execution %s: %w", trade.TradeID, err)
		}

		// 检查订单是否已存在
		if existingOrderIDs[trade.OrderID] {
			continue
		}

		// 保存订单（包含交易所已实现盈亏）
		var realizedPnL *float64
		if trade.RealizedPnL != 0 {
			v := trade.RealizedPnL
			realizedPnL = &v
		}
		order := &storage.Order{
			OrderID:       trade.OrderID,
			ClientOrderID: "", // 成交记录中没有ClientOrderID
			Symbol:        trade.Symbol,
			Side:          string(trade.Side),
			Exchange:      s.exchangeName,
			Account:       s.accountID,
			MarketType:    s.marketType,
			AccountScope:  s.accountScope,
			Price:         trade.Price,
			Quantity:      trade.Quantity,
			FilledQty:     trade.Quantity, // 成交记录 = 已成交
			Status:        "FILLED",       // 成交记录都是已成交的
			RealizedPnL:   realizedPnL,
			CreatedAt:     time.UnixMilli(trade.TradeTime).UTC(),
			UpdatedAt:     time.UnixMilli(trade.TradeTime).UTC(),
		}

		if err := s.storage.SaveOrder(order); err != nil {
			return syncedCount, fmt.Errorf("save imported order %d after persisting execution %s: %w", trade.OrderID, trade.TradeID, err)
		}

		syncedCount++
		existingOrderIDs[trade.OrderID] = true
		logger.Debug("✅ [订单同步] 同步订单: OrderID=%d, Side=%s, Price=%.2f, Quantity=%.4f, RealizedPnL=%.4f",
			trade.OrderID, trade.Side, trade.Price, trade.Quantity, trade.RealizedPnL)
	}
	return syncedCount, nil
}
