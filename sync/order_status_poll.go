package sync

import (
	"context"
	"reflect"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
)

// OrderStatusPollService 訂單狀態輪詢服務
// 定期輪詢本地活躍訂單的狀態，及時發現外部變化（如手工平倉）
type OrderStatusPollService struct {
	exchange     exchange.IExchange
	pm           IPositionManagerForPoll
	symbol       string
	pollInterval time.Duration
	mu           sync.RWMutex
	isRunning    bool
	stopC        chan struct{}
}

// IPositionManagerForPoll 訂單輪詢所需的倉位管理器接口
type IPositionManagerForPoll interface {
	// 遍歷所有槽位，收集活躍訂單ID
	IterateSlots(fn func(price float64, slot interface{}) bool)
	// 觸發訂單更新處理
	OnOrderUpdate(update position.OrderUpdate)
	// 獲取交易對
	GetSymbol() string
}

// NewOrderStatusPollService 創建訂單狀態輪詢服務
func NewOrderStatusPollService(
	ex exchange.IExchange,
	pm IPositionManagerForPoll,
	symbol string,
	pollInterval time.Duration,
) *OrderStatusPollService {
	return &OrderStatusPollService{
		exchange:     ex,
		pm:           pm,
		symbol:       symbol,
		pollInterval: pollInterval,
		stopC:        make(chan struct{}),
	}
}

// Start 啟動訂單狀態輪詢服務
func (s *OrderStatusPollService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		logger.Warn("⚠️ [訂單輪詢] 服務已在運行")
		return
	}
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [訂單輪詢] 啟動訂單狀態輪詢服務 (symbol=%s, interval=%v)", s.symbol, s.pollInterval)

	go func() {
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()

		// 啟動時立即輪詢一次
		if err := s.PollOrderStatus(ctx); err != nil {
			logger.Error("❌ [訂單輪詢] 初始輪詢失敗: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ [訂單輪詢] 服務已停止")
				return
			case <-s.stopC:
				logger.Info("⏹️ [訂單輪詢] 服務已停止")
				return
			case <-ticker.C:
				if err := s.PollOrderStatus(ctx); err != nil {
					logger.Error("❌ [訂單輪詢] 輪詢失敗: %v", err)
				}
			}
		}
	}()
}

// Stop 停止訂單狀態輪詢服務
func (s *OrderStatusPollService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	close(s.stopC)
	s.isRunning = false
	logger.Info("⏹️ [訂單輪詢] 訂單狀態輪詢服務已停止")
}

// PollOrderStatus 執行訂單狀態輪詢
func (s *OrderStatusPollService) PollOrderStatus(ctx context.Context) error {
	// 1. 收集本地活躍訂單ID
	localActiveOrderIDs := make(map[int64]struct {
		Price     float64
		ClientOID string
		Side      string
		Status    string
	})

	// 訂單狀態常量
	const (
		OrderStatusPlaced          = "PLACED"
		OrderStatusConfirmed       = "CONFIRMED"
		OrderStatusPartiallyFilled = "PARTIALLY_FILLED"
		OrderStatusCancelRequested = "CANCEL_REQUESTED"
	)

	s.pm.IterateSlots(func(price float64, slotRaw interface{}) bool {
		// 使用反射提取槽位字段
		v := reflect.ValueOf(slotRaw)
		if v.Kind() != reflect.Struct {
			return true
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

		orderID := int64(getFloat64Field("OrderID"))
		orderStatus := getStringField("OrderStatus")
		orderSide := getStringField("OrderSide")
		clientOID := getStringField("ClientOID")

		// 只收集活躍訂單
		if orderID > 0 && (orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed ||
			orderStatus == OrderStatusPartiallyFilled || orderStatus == OrderStatusCancelRequested) {
			localActiveOrderIDs[orderID] = struct {
				Price     float64
				ClientOID string
				Side      string
				Status    string
			}{
				Price:     price,
				ClientOID: clientOID,
				Side:      orderSide,
				Status:    orderStatus,
			}
		}

		return true
	})

	if len(localActiveOrderIDs) == 0 {
		logger.Debug("📭 [訂單輪詢] 沒有本地活躍訂單")
		return nil
	}

	logger.Debug("🔍 [訂單輪詢] 開始輪詢 %d 個本地活躍訂單", len(localActiveOrderIDs))

	// 2. 查詢交易所訂單狀態
	updatedCount := 0
	for orderID, slotInfo := range localActiveOrderIDs {
		orderDetail, err := s.exchange.GetOrder(ctx, s.symbol, orderID)
		if err != nil {
			logger.Debug("⚠️ [訂單輪詢] 查詢訂單失敗 (OrderID=%d): %v", orderID, err)
			continue
		}

		if orderDetail == nil {
			logger.Warn("⚠️ [訂單輪詢] 訂單不存在 (OrderID=%d)，可能已被刪除或成交", orderID)
			// 訂單不存在，可能是被外部取消或成交
			// 注意：這裡不觸發更新，因為無法確定實際狀態，應該由對賬服務處理
			continue
		}

		// 3. 檢查訂單狀態是否變化
		exchangeStatus := string(orderDetail.Status)
		if exchangeStatus != slotInfo.Status {
			logger.Info("🔄 [訂單輪詢] 檢測到訂單狀態變化: OrderID=%d, 本地=%s, 交易所=%s",
				orderID, slotInfo.Status, exchangeStatus)

			// 構建 OrderUpdate 並觸發更新
			update := position.OrderUpdate{
				OrderID:       orderDetail.OrderID,
				ClientOrderID: orderDetail.ClientOrderID,
				Symbol:        s.symbol,
				Status:        exchangeStatus,
				ExecutedQty:   orderDetail.ExecutedQty,
				Price:         orderDetail.Price,
				AvgPrice:      orderDetail.AvgPrice,
				Side:          string(orderDetail.Side),
				Type:          string(orderDetail.Type),
				UpdateTime:    orderDetail.UpdateTime,
			}

			s.pm.OnOrderUpdate(update)
			updatedCount++
		}
	}

	if updatedCount > 0 {
		logger.Info("✅ [訂單輪詢] 輪詢完成: 更新了 %d 個訂單狀態", updatedCount)
	} else {
		logger.Debug("✅ [訂單輪詢] 輪詢完成: 沒有狀態變化")
	}

	return nil
}
