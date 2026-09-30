package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/logger"
	"quantmesh/position"
)

func (s *DCAEnhancedStrategy) commissionInQuote(commission float64, asset string, fillPrice float64) (float64, bool) {
	return commissionInQuote(s.exchange, commission, asset, fillPrice)
}

func (s *DCAEnhancedStrategy) supportsSpotBaseFee() bool {
	return s.cfg != nil && strings.EqualFold(strings.TrimSpace(s.cfg.Trading.MarketType), "spot")
}

func (s *DCAEnhancedStrategy) hasUnmappedBaseFee(commission float64, asset string, baseFeeQty float64) bool {
	if s.exchange == nil || commission <= 0 || baseFeeQty > 0 {
		return false
	}
	baseAsset := strings.TrimSpace(s.exchange.GetBaseAsset())
	return baseAsset != "" && strings.EqualFold(strings.TrimSpace(asset), baseAsset)
}

func (s *DCAEnhancedStrategy) requireDCAOrderReconciliation(update *position.OrderUpdate, reason string) {
	if tracker, ok := s.executor.(interface {
		MarkOrderReconciliationRequired(int64, string, string) error
	}); ok {
		if err := tracker.MarkOrderReconciliationRequired(update.OrderID, update.ClientOrderID, reason); err != nil {
			logger.Error("[%s] 手續費无法折算且无法持久化执行对账锁: order=%d err=%v", s.name, update.OrderID, err)
		}
	} else {
		logger.Error("[%s] 手續費无法折算且执行器不支持持久化对账锁: order=%d reason=%s", s.name, update.OrderID, reason)
	}
}

// handleCloseOrderUpdate accounts for fills while the order is live. A terminal
// cancel releases only its remainder and never reverses an executed trade.
func (s *DCAEnhancedStrategy) handleCloseOrderUpdate(update *position.OrderUpdate) {
	if !update.CommissionKnown && update.ExecutedQty > s.closeProgress.Quantity+entryQtyEpsilon {
		s.requireDCAOrderReconciliation(update, "DCA close fee evidence is not authoritative")
		return
	}
	if !finiteNumber(update.BaseFeeQty) || update.BaseFeeQty < 0 {
		s.requireDCAOrderReconciliation(update, "DCA close base-asset fee is invalid")
		return
	}
	if update.BaseFeeQty > 0 {
		// A base-denominated sell fee consumes extra inventory. The current DCA
		// trade ledger has no fee-lot split for that extra quantity, so do not
		// advance the close cursor or report a settled trade.
		s.requireDCAOrderReconciliation(update, "DCA close base-asset fee requires inventory-cost reconciliation")
		return
	}
	if s.hasUnmappedBaseFee(update.Commission, update.CommissionAsset, update.BaseFeeQty) {
		s.requireDCAOrderReconciliation(update, "DCA close reports a base-asset commission without its inventory fee quantity")
		return
	}
	if !finiteNumber(update.ExecutedQty) || update.ExecutedQty < 0 {
		s.requireDCAOrderReconciliation(update, "DCA close execution quantity is not finite and non-negative")
		return
	}
	if signalOrderStatusFilled(update.Status) && update.ExecutedQty <= 0 {
		// A terminal label cannot substitute for the venue's cumulative fill.
		// Keep the close intent active so strategy state is not falsely cleared.
		return
	}
	if !finiteNumber(s.closeRequestedQty) || s.closeRequestedQty <= 0 || update.ExecutedQty > s.closeRequestedQty+entryQtyEpsilon {
		s.requireDCAOrderReconciliation(update, "DCA close execution exceeds the persisted requested quantity")
		return
	}
	if update.ExecutedQty > s.closeProgress.Quantity {
		if !finiteNumber(update.AvgPrice) {
			s.requireDCAOrderReconciliation(update, "DCA close execution average price is non-finite")
			return
		}
		if update.AvgPrice <= 0 {
			return
		}
	}
	quantity, _ := entryFillFromUpdate(update)
	if quantity > s.closeProgress.Quantity && !dcaCumulativeNotionalAdvances(quantity, update.AvgPrice, s.closeProgress.Notional) {
		s.requireDCAOrderReconciliation(update, "DCA close cumulative fill notional regressed")
		return
	}
	nextProgress := s.closeProgress
	delta, price := nextProgress.Advance(quantity, update.AvgPrice, 0)
	if delta > 0 {
		available, cost, openingFee := s.closingInventory()
		if !finiteNumber(available) || available <= 0 || !finiteNumber(cost) || cost <= 0 ||
			!finiteNumber(openingFee) || delta > available+entryQtyEpsilon {
			s.requireDCAOrderReconciliation(update, "DCA close execution exceeds strategy-attributed inventory")
			return
		}
		closed := delta
		if closed > 0 {
			closeFee, feeKnown := s.commissionInQuote(update.Commission, update.CommissionAsset, price)
			if !feeKnown {
				s.requireDCAOrderReconciliation(update, "DCA close fee is not denominated in a supported quote asset")
				return
			}
			entryPrice := cost / available
			fee := openingFee*closed/available + closeFee
			pnl := closed * (price - entryPrice)
			executionKey := dcaFillExecutionKey(s, update.OrderID, nextProgress.Quantity)
			if !s.saveCloseTrade(executionKey, update.OrderID, entryPrice, price, closed, pnl, fee, update.RealizedPnL) {
				s.requireDCAOrderReconciliation(update, "DCA trade ledger persistence failed")
				return
			}
			s.closeProgress = nextProgress
			if s.supportsSpotBaseFee() {
				s.closeFeeVerifiedQty += delta
			}
			s.recordCloseStats(pnl-fee, closed*price)
			if s.closeLayer != nil {
				s.reduceLayer(s.closeLayer, closed)
			} else {
				s.reduceAllLayers(closed)
			}
		}
	} else {
		s.closeProgress = nextProgress
	}
	if signalOrderStatusFilled(update.Status) || signalOrderStatusTerminal(update.Status) {
		s.isClosing = false
		s.closeOrderID = 0
		s.closeLayer = nil
		s.closeProgress = position.FillProgress{}
		s.closeFeeVerifiedQty = 0
		s.closeRequestedQty, s.closeLimitPrice = 0, 0
		s.highestProfit = 0
		s.takeProfitTriggered = false
	}
}

func dcaFillExecutionKey(s *DCAEnhancedStrategy, orderID int64, cumulativeQty float64) string {
	marketType := "futures"
	if s.cfg != nil && strings.TrimSpace(s.cfg.Trading.MarketType) != "" {
		marketType = strings.ToLower(strings.TrimSpace(s.cfg.Trading.MarketType))
	}
	identity := fmt.Sprintf("dca|%s|%s|%s|%d|%s|%s", strings.TrimSpace(s.effectiveBotID()), strings.ToLower(s.exchange.GetName()), marketType, orderID, s.strategyCfg.Symbol, strconv.FormatFloat(cumulativeQty, 'f', -1, 64))
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func (s *DCAEnhancedStrategy) closingInventory() (quantity, cost, openingFee float64) {
	if s.closeLayer != nil {
		return s.closeLayer.Quantity, s.closeLayer.Cost, s.closeLayer.OpeningFee
	}
	for _, layer := range s.filledLayers() {
		quantity += layer.Quantity
		cost += layer.Cost
		openingFee += layer.OpeningFee
	}
	return
}

func dcaCumulativeNotionalAdvances(quantity, averagePrice, previousNotional float64) bool {
	if !finiteNumber(quantity) || quantity <= 0 || !finiteNumber(averagePrice) || averagePrice <= 0 ||
		!finiteNumber(previousNotional) || previousNotional < 0 {
		return false
	}
	notional := quantity * averagePrice
	return finiteNumber(notional) && notional > previousNotional
}
