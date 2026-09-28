package position

import (
	"testing"
	"time"

	"quantmesh/execution"
)

func TestGridExposureIdentityFromActualAdjustment(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		for _, market := range []string{"spot", "futures"} {
			t.Run(direction+"/"+market, func(t *testing.T) {
				spm, executor := newR5bSPM(t, direction, 4)
				spm.config.Trading.MarketType = market
				opens := adjustAt(t, spm, executor, 100)
				if len(opens) == 0 {
					t.Fatal("no opening orders exercised")
				}
				for _, open := range opens {
					price, _, ok := spm.parseClientOrderID(open.ClientOrderID)
					leg := gridExposureLeg(open.Side, true)
					if !ok || open.ExposureKey != gridExposureKey(price, leg) || open.PositionSide != leg {
						t.Fatalf("opening identity: %+v", open)
					}
					// A new request after restart must derive the same lot from the
					// same restored slot, independent of its new CID and close price.
					closer, closeExecutor := newR5bSPM(t, direction, 0)
					closer.config.Trading.MarketType = market
					fillSlot(closer, price, open.Quantity, open.Price, leg)
					closes := adjustAt(t, closer, closeExecutor, 100)
					var matched *OrderRequest
					for _, close := range closes {
						if close.Side != open.Side && close.ExposureKey == open.ExposureKey {
							matched = close
							break
						}
					}
					if matched == nil || matched.PositionSide != leg {
						t.Fatalf("close lost original lot %s: %+v", open.ExposureKey, closes)
					}
					book, err := execution.NewExposureBook(execution.ExposureLimits{Layers: 1}, time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					if err := book.Seed(nil); err != nil {
						t.Fatal(err)
					}
					now := time.Now()
					if err := book.SetMark(100, now); err != nil {
						t.Fatal(err)
					}
					req := execution.ExposureRequest{ID: open.ClientOrderID, Group: "grid", Lot: open.ExposureKey, Leg: leg, Opening: true, Quantity: open.Quantity, Price: open.Price}
					if err := book.Reserve(req, now); err != nil {
						t.Fatal(err)
					}
					if err := book.Observe(req.ID, execution.ExposureUpdate{Status: "FILLED", OrderQty: req.Quantity, CumulativeQty: req.Quantity}); err != nil {
						t.Fatal(err)
					}
					req.ID, req.Lot, req.Opening = matched.ClientOrderID, matched.ExposureKey, false
					if err := book.Reserve(req, now); err != nil {
						t.Fatal(err)
					}
					if err := book.Observe(req.ID, execution.ExposureUpdate{Status: "FILLED", OrderQty: req.Quantity, CumulativeQty: req.Quantity}); err != nil {
						t.Fatal(err)
					}
					if book.Snapshot(now).Layers != 0 {
						t.Fatal("closed lot retained phantom layer")
					}
				}
			})
		}
	}
}

func TestGridExposureIdentitySeparatesLegsAndIgnoresWorkingPrice(t *testing.T) {
	if gridExposureKey(100, PositionLegLong) == gridExposureKey(100, PositionLegShort) {
		t.Fatal("opposite legs alias")
	}
	for _, direction := range []string{"LONG", "SHORT"} {
		spm, executor := newR5bSPM(t, direction, 0)
		spm.config.Trading.ProfitSpread = 2
		leg, price := PositionLegLong, 99.0
		if direction == "SHORT" {
			leg, price = PositionLegShort, 101
		}
		fillSlot(spm, price, 1, price, leg)
		orders := adjustAt(t, spm, executor, 100)
		if len(orders) != 1 {
			t.Fatalf("close orders: %d", len(orders))
		}
		if orders[0].Price == price || orders[0].ExposureKey != gridExposureKey(price, leg) {
			t.Fatalf("identity follows working price: %+v", orders[0])
		}
	}
}

func TestGridLiquidationExposureIdentity(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT"} {
		size, leg := 1.0, PositionLegLong
		if direction == "SHORT" {
			size, leg = -1, PositionLegShort
		}
		base := newLiqFakeVenue(size)
		base.limitFillRatio = .4
		spm, _ := newLiqTestSPM(t, direction, base)
		v := &marketFillLiquidationExecutor{liqFakeVenue: base, spm: spm}
		spm.executor = v
		fillSlot(spm, liqTestLast, 1, liqTestLast, "") // legacy single-direction slot
		if err := runVerified(t, spm, base); err != nil {
			t.Fatal(err)
		}
		if len(base.limitReqs) != 1 || len(v.marketIntents) != 1 {
			t.Fatal("limit and residual paths not exercised")
		}
		for _, req := range []*OrderRequest{base.limitReqs[0], v.marketIntents[0]} {
			if req.PositionSide != leg || req.ExposureKey != gridExposureKey(liqTestLast, leg) {
				t.Fatalf("liquidation lost economic lot: %+v", req)
			}
		}
	}
}
