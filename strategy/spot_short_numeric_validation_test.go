package strategy

import (
	"context"
	"math"
	"testing"

	"quantmesh/event"
)

type spotShortPriceEvidenceExchange struct {
	signalTestExchange
	price float64
}

func (e *spotShortPriceEvidenceExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return e.price, nil
}

func TestSpotShortRejectsInvalidPriceBeforeDebtOrOrderIntent(t *testing.T) {
	prices := []struct {
		name  string
		value float64
	}{
		{"zero", 0}, {"negative", -1}, {"nan", math.NaN()},
		{"infinity", math.Inf(1)}, {"negative infinity", math.Inf(-1)},
		{"rounds to zero", 0.000001}, {"rounding overflow", math.MaxFloat64},
	}
	for _, side := range []string{"SELL", "BUY"} {
		for _, price := range prices {
			t.Run(side+"/"+price.name, func(t *testing.T) {
				margin := &mockMarginExchange{}
				executor := &signalTestExecutor{}
				s := newSpotShortForTest(executor, &spotShortPriceEvidenceExchange{price: price.value}, margin)
				var err error
				if side == "SELL" {
					err = s.increaseShort(context.Background(), 0.25)
				} else {
					err = s.decreaseShort(context.Background(), 0.25)
				}
				if err == nil {
					t.Fatal("invalid price must return an explicit error")
				}
				if len(margin.borrowed) != 0 || len(executor.orders) != 0 || len(s.pendingBorrow) != 0 || len(s.pendingBuy) != 0 {
					t.Fatalf("invalid price created debt/order intent: borrowed=%v orders=%v borrow=%v buy=%v", margin.borrowed, executor.orders, s.pendingBorrow, s.pendingBuy)
				}
			})
		}
	}
}

func TestSpotShortRejectsInvalidQuantityBeforeTrading(t *testing.T) {
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, math.MaxFloat64} {
		for _, side := range []string{"SELL", "BUY"} {
			margin := &mockMarginExchange{}
			executor := &signalTestExecutor{}
			s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
			var err error
			if side == "SELL" {
				err = s.increaseShort(context.Background(), amount)
			} else {
				err = s.decreaseShort(context.Background(), amount)
			}
			if err == nil || len(margin.borrowed) != 0 || len(executor.orders) != 0 || len(s.pendingBorrow) != 0 || len(s.pendingBuy) != 0 {
				t.Fatalf("invalid %s quantity %v accepted or persisted: err=%v", side, amount, err)
			}
		}
	}
}

func TestSpotShortInvalidHedgeTargetNotifiesRiskWithoutTrading(t *testing.T) {
	for _, target := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		margin := &mockMarginExchange{}
		executor := &signalTestExecutor{}
		s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
		notified := false
		s.SetUnresolvedDebtHandler(func(error) { notified = true })
		s.onHedgeSignal(&event.Event{Data: map[string]interface{}{"symbol": "BTCUSDT", "target_spot_short": target}})
		if !notified || len(margin.borrowed) != 0 || len(executor.orders) != 0 {
			t.Fatalf("invalid hedge target %v was not blocked and reported", target)
		}
	}
}
