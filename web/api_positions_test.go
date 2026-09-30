package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"quantmesh/exchange"
)

func TestNormalizeMarketPriceRejectsNonFiniteAndNonPositiveValues(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{name: "valid", input: 100, want: 100},
		{name: "zero"},
		{name: "negative", input: -1},
		{name: "nan", input: math.NaN()},
		{name: "positive infinity", input: math.Inf(1)},
		{name: "negative infinity", input: math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMarketPrice(tt.input); got != tt.want {
				t.Fatalf("normalizeMarketPrice(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnavailablePositionSummaryDoesNotClaimVerifiedFlatAccount(t *testing.T) {
	got := unavailablePositionSummary()
	if got.PositionDataAvailable {
		t.Fatal("missing position provider must report position data unavailable")
	}
	if got.CostBasisVerified || got.EntryFeesVerified || got.UnrealizedPnLVerified {
		t.Fatalf("missing position provider must not imply verified zero exposure: %+v", got)
	}
	if got.TotalQuantity != 0 || got.PositionCount != 0 || len(got.Positions) != 0 {
		t.Fatalf("unavailable summary should preserve an empty response shape: %+v", got)
	}
}

func TestPositionSlotFinancialsValidRejectsMalformedFilledSlots(t *testing.T) {
	base := SlotInfo{PositionStatus: "FILLED", PositionQty: 1, Price: 100, AvgBuyPrice: 99, BuyFee: 0.1}
	tests := []struct {
		name   string
		mutate func(*SlotInfo)
		valid  bool
	}{
		{name: "valid", valid: true},
		{name: "small positive exposure remains visible", mutate: func(slot *SlotInfo) { slot.PositionQty = 1e-9; slot.Price = 1e-9 }, valid: true},
		{name: "non-finite quantity", mutate: func(slot *SlotInfo) { slot.PositionQty = math.Inf(1) }},
		{name: "negative quantity", mutate: func(slot *SlotInfo) { slot.PositionQty = -0.1 }},
		{name: "non-finite price", mutate: func(slot *SlotInfo) { slot.Price = math.NaN() }},
		{name: "zero price with exposure", mutate: func(slot *SlotInfo) { slot.Price = 0 }},
		{name: "negative fee", mutate: func(slot *SlotInfo) { slot.BuyFee = -1 }},
		{name: "missing verified cost basis", mutate: func(slot *SlotInfo) { slot.AvgBuyPrice = 0 }},
		{name: "unverified cost basis may be absent", mutate: func(slot *SlotInfo) { slot.AvgBuyPrice = 0; slot.CostBasisUnverified = true }, valid: true},
		{name: "non-finite unverified cost basis", mutate: func(slot *SlotInfo) { slot.AvgBuyPrice = math.NaN(); slot.CostBasisUnverified = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot := base
			if tt.mutate != nil {
				tt.mutate(&slot)
			}
			if got := positionSlotFinancialsValid(slot); got != tt.valid {
				t.Fatalf("positionSlotFinancialsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestAssessPositionMarketPriceMarksFallbacksUnverified(t *testing.T) {
	tests := []struct {
		name      string
		market    float64
		reference float64
		wantPrice float64
		verified  bool
	}{
		{name: "normal movement", market: 105, reference: 100, wantPrice: 105, verified: true},
		{name: "unit correction guess", market: 10000, reference: 100, wantPrice: 100, verified: false},
		{name: "large unexplained deviation", market: 200, reference: 100, wantPrice: 100, verified: false},
		{name: "missing market price", reference: 100, wantPrice: 100, verified: false},
		{name: "invalid market price", market: math.NaN(), reference: 100, wantPrice: 100, verified: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, verified := assessPositionMarketPrice(tt.market, tt.reference)
			if price != tt.wantPrice || verified != tt.verified {
				t.Fatalf("assessPositionMarketPrice() = %v, %v; want %v, %v", price, verified, tt.wantPrice, tt.verified)
			}
		})
	}
}

func TestFinitePositionEntryPriceDoesNotExposeUnverifiedCost(t *testing.T) {
	tests := []struct {
		name string
		slot SlotInfo
		want float64
	}{
		{name: "verified", slot: SlotInfo{AvgBuyPrice: 123}, want: 123},
		{name: "unverified", slot: SlotInfo{AvgBuyPrice: 123, CostBasisUnverified: true}},
		{name: "non-finite", slot: SlotInfo{AvgBuyPrice: math.Inf(1)}},
		{name: "non-positive", slot: SlotInfo{AvgBuyPrice: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := finitePositionEntryPrice(tt.slot); got != tt.want {
				t.Fatalf("finitePositionEntryPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPositionDiscrepancyArithmeticFailsClosedOnOverflow(t *testing.T) {
	tests := []struct {
		name        string
		left        float64
		right       float64
		baseline    float64
		wantValid   bool
		wantDiff    float64
		wantPercent float64
	}{
		{name: "finite difference", left: 110, right: 100, baseline: 100, wantValid: true, wantDiff: 10, wantPercent: 10},
		{name: "difference overflow", left: math.MaxFloat64, right: -math.MaxFloat64, baseline: 1},
		{name: "percentage overflow", left: math.MaxFloat64, right: 0, baseline: math.SmallestNonzeroFloat64},
		{name: "invalid baseline", left: 110, right: 100, baseline: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, percent, valid := finitePositionDifferencePercent(tt.left, tt.right, tt.baseline)
			if valid != tt.wantValid || diff != tt.wantDiff || percent != tt.wantPercent {
				t.Fatalf("finitePositionDifferencePercent() = %v, %v, %v; want %v, %v, %v", diff, percent, valid, tt.wantDiff, tt.wantPercent, tt.wantValid)
			}
		})
	}
}

func TestExchangePnLRequiresMatchingSlotExposureAndDirection(t *testing.T) {
	tests := []struct {
		name        string
		exchange    []*exchange.Position
		slots       []SlotInfo
		direction   string
		wantMatches bool
	}{
		{name: "matching long net", exchange: []*exchange.Position{{Size: 2, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101, UnrealizedPNL: 2}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "LONG", wantMatches: true},
		{name: "matching short net", exchange: []*exchange.Position{{Size: -2, PositionSide: "NET", EntryPrice: 100, MarkPrice: 99, UnrealizedPNL: 2}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "SHORT", wantMatches: true},
		{name: "Bybit-style positive short hedge size", exchange: []*exchange.Position{{Size: 2, PositionSide: "SHORT", EntryPrice: 100, MarkPrice: 99, UnrealizedPNL: 2}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "SHORT", wantMatches: true},
		{name: "matching long precision", exchange: []*exchange.Position{{Size: 2.000000001, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "LONG", wantMatches: true},
		{name: "opposite side", exchange: []*exchange.Position{{Size: -2, PositionSide: "NET", EntryPrice: 100, MarkPrice: 99}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "LONG"},
		{name: "external unmatched size", exchange: []*exchange.Position{{Size: 3, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "LONG"},
		{name: "exchange-only position", exchange: []*exchange.Position{{Size: 2, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101}}, direction: "LONG"},
		{name: "matching hedge both legs", exchange: []*exchange.Position{{Size: 2, PositionSide: "LONG", EntryPrice: 100, MarkPrice: 101}, {Size: 1, PositionSide: "SHORT", EntryPrice: 100, MarkPrice: 99}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2, PositionLeg: "LONG"}, {PositionStatus: "FILLED", PositionQty: 1, PositionLeg: "SHORT"}}, direction: "BOTH", wantMatches: true},
		{name: "hedge quantities must match each leg", exchange: []*exchange.Position{{Size: 3, PositionSide: "LONG", EntryPrice: 100, MarkPrice: 101}, {Size: 1, PositionSide: "SHORT", EntryPrice: 100, MarkPrice: 99}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2, PositionLeg: "LONG"}, {PositionStatus: "FILLED", PositionQty: 1, PositionLeg: "SHORT"}}, direction: "BOTH"},
		{name: "hedge snapshot cannot be assigned to one-way long", exchange: []*exchange.Position{{Size: 2, PositionSide: "SHORT", EntryPrice: 100, MarkPrice: 99}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "LONG"},
		{name: "net snapshot cannot prove both hedge legs", exchange: []*exchange.Position{{Size: 1, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2, PositionLeg: "LONG"}, {PositionStatus: "FILLED", PositionQty: 1, PositionLeg: "SHORT"}}, direction: "BOTH"},
		{name: "both direction requires leg identity", exchange: []*exchange.Position{{Size: 2, PositionSide: "LONG", EntryPrice: 100, MarkPrice: 101}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "BOTH"},
		{name: "unknown direction", exchange: []*exchange.Position{{Size: 2, PositionSide: "NET", EntryPrice: 100, MarkPrice: 101}}, slots: []SlotInfo{{PositionStatus: "FILLED", PositionQty: 2}}, direction: "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := summarizeExchangePositions(tt.exchange)
			if got := exchangePositionMatchesSlots(snapshot, tt.slots, tt.direction); got != tt.wantMatches {
				t.Fatalf("exchangePositionMatchesSlots() = %v, want %v (snapshot=%+v)", got, tt.wantMatches, snapshot)
			}
		})
	}
}

func TestUnavailablePositionSummaryResponseDoesNotClaimVerifiedZeros(t *testing.T) {
	response := unavailablePositionSummaryResponse()
	for _, key := range []string{"position_data_available", "cost_basis_verified", "entry_fees_verified", "unrealized_pnl_verified"} {
		if value, ok := response[key].(bool); !ok || value {
			t.Fatalf("%s should be false when position data is unavailable, got %#v", key, response[key])
		}
	}
}

func TestGetPositionsSummaryMissingProviderReportsUnavailable(t *testing.T) {
	resetProviderInfraGlobals(t)
	c, recorder := newProviderInfraContext("/api/positions/summary")
	getPositionsSummary(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"position_data_available", "cost_basis_verified", "entry_fees_verified", "unrealized_pnl_verified"} {
		if value, ok := response[key].(bool); !ok || value {
			t.Fatalf("%s should be false without a provider, got %#v", key, response[key])
		}
	}
}

func TestGetPositionsSummaryRejectsMalformedSlotFinancials(t *testing.T) {
	resetProviderInfraGlobals(t)
	SetPositionManagerProvider(&providerInfraPosition{slots: []SlotInfo{{
		PositionStatus: "FILLED", PositionQty: 1, Price: 100, AvgBuyPrice: 99, BuyFee: math.NaN(),
	}}})
	c, recorder := newProviderInfraContext("/api/positions/summary")
	getPositionsSummary(c)
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["position_data_available"] != false || response["total_quantity"] != float64(0) || response["cost_basis_verified"] != false {
		t.Fatalf("malformed slot should fail closed: %+v", response)
	}
}

type positionResponseExchange struct {
	positions []*exchange.Position
}

func (positionResponseExchange) GetHistoricalKlines(context.Context, string, string, int) ([]*exchange.Candle, error) {
	return nil, nil
}

func (positionResponseExchange) GetFundingRate(context.Context, string) (float64, error) {
	return 0, nil
}

func (provider positionResponseExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return provider.positions, nil
}

func TestGetPositionsMarksFallbackValuationUnverifiedWithoutMarketPrice(t *testing.T) {
	resetProviderInfraGlobals(t)
	SetPositionManagerProvider(&providerInfraPosition{slots: []SlotInfo{{
		PositionStatus: "FILLED", PositionQty: 1, Price: 100, AvgBuyPrice: 99,
	}}})
	c, recorder := newProviderInfraContext("/api/positions")
	getPositions(c)
	var response struct {
		Summary PositionSummary `json:"summary"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Summary.PositionValueVerified {
		t.Fatalf("fallback slot-price valuation must not be marked verified: %+v", response.Summary)
	}
	if response.Summary.Positions[0].ValueVerified {
		t.Fatalf("fallback slot-price position value must not be marked verified: %+v", response.Summary.Positions[0])
	}
	if response.Summary.UnrealizedPnLVerified {
		t.Fatalf("PnL without a market price must not be marked verified: %+v", response.Summary)
	}
}

func TestGetPositionsSummaryDoesNotApplyMismatchedExchangePnL(t *testing.T) {
	resetProviderInfraGlobals(t)
	SetPositionManagerProvider(&providerInfraPosition{slots: []SlotInfo{{
		PositionStatus: "FILLED", PositionQty: 1, Price: 100, AvgBuyPrice: 100,
	}}})
	SetPriceProvider(&providerInfraPrice{price: 110})
	SetExchangeProvider(positionResponseExchange{positions: []*exchange.Position{{
		Symbol: "BTCUSDT", Size: 2, EntryPrice: 100, MarkPrice: 110, UnrealizedPNL: 50,
	}}})
	c, recorder := newProviderInfraContext("/api/positions/summary?exchange=binance&symbol=BTCUSDT")
	getPositionsSummary(c)
	var response struct {
		UnrealizedPnL float64 `json:"unrealized_pnl"`
		ExchangeData  struct {
			PnLApplied bool `json:"pnl_applied"`
		} `json:"exchange_data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ExchangeData.PnLApplied {
		t.Fatal("exchange PnL must not be applied when exchange quantity differs from local slots")
	}
	if response.UnrealizedPnL != 10 {
		t.Fatalf("unrealized PnL = %v, want verified slot PnL 10", response.UnrealizedPnL)
	}
}

func TestSummarizeExchangePositionsFailsClosedOnMalformedExposure(t *testing.T) {
	valid := func(size, pnl float64) *exchange.Position {
		return &exchange.Position{Size: size, EntryPrice: 100, MarkPrice: 101, UnrealizedPNL: pnl, Leverage: 5}
	}
	validSide := func(size, pnl float64, side string) *exchange.Position {
		position := valid(size, pnl)
		position.PositionSide = side
		return position
	}
	tests := []struct {
		name         string
		positions    []*exchange.Position
		wantData     bool
		wantValid    bool
		wantQuantity float64
		wantLongQty  float64
		wantShortQty float64
	}{
		{name: "valid position", positions: []*exchange.Position{valid(2, 3)}, wantData: true, wantValid: true, wantQuantity: 2},
		{name: "net legs offset", positions: []*exchange.Position{valid(2, 3), valid(-2, -2)}, wantData: true, wantValid: true},
		{name: "positive short size normalized by explicit side", positions: []*exchange.Position{validSide(2, 3, "SHORT")}, wantData: true, wantValid: true, wantQuantity: -2, wantShortQty: 2},
		{name: "hedge legs retain gross quantities", positions: []*exchange.Position{validSide(2, 3, "LONG"), validSide(2, -2, "SHORT")}, wantData: true, wantValid: true, wantLongQty: 2, wantShortQty: 2},
		{name: "mixed net and hedge modes", positions: []*exchange.Position{valid(2, 3), validSide(1, 1, "SHORT")}, wantData: true, wantValid: false},
		{name: "empty snapshot", wantValid: false},
		{name: "nil position", positions: []*exchange.Position{nil}, wantData: true, wantValid: false},
		{name: "nan size", positions: []*exchange.Position{valid(math.NaN(), 3)}, wantData: true, wantValid: false},
		{name: "infinite pnl", positions: []*exchange.Position{valid(2, math.Inf(1))}, wantData: true, wantValid: false},
		{name: "invalid mark", positions: []*exchange.Position{{Size: 2, EntryPrice: 100, MarkPrice: math.NaN()}}, wantData: true, wantValid: false},
		{name: "overflow aggregate", positions: []*exchange.Position{valid(math.MaxFloat64, 1), valid(math.MaxFloat64, 1)}, wantData: true, wantValid: false},
		{name: "zero size ignored", positions: []*exchange.Position{valid(0, 0)}, wantValid: false},
		{name: "zero size with non-zero pnl", positions: []*exchange.Position{valid(0, 1)}, wantData: true, wantValid: false},
		{name: "zero size with invalid pnl", positions: []*exchange.Position{valid(0, math.NaN())}, wantData: true, wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeExchangePositions(tt.positions)
			if got.HasData != tt.wantData || got.Verified != tt.wantValid {
				t.Fatalf("snapshot data/verified = %v/%v, want %v/%v", got.HasData, got.Verified, tt.wantData, tt.wantValid)
			}
			if tt.wantValid && (got.Quantity != tt.wantQuantity || got.LongQuantity != tt.wantLongQty || got.ShortQuantity != tt.wantShortQty) {
				t.Fatalf("snapshot quantities = net:%v long:%v short:%v, want net:%v long:%v short:%v", got.Quantity, got.LongQuantity, got.ShortQuantity, tt.wantQuantity, tt.wantLongQty, tt.wantShortQty)
			}
			for _, value := range []float64{got.Quantity, got.UnrealizedPnL, got.MarkPrice, got.EntryPrice} {
				if !isFiniteNumber(value) {
					t.Fatalf("snapshot contains non-finite value: %+v", got)
				}
			}
		})
	}
}

func TestSlotUnrealizedPnLRespectsPositionDirection(t *testing.T) {
	tests := []struct {
		name        string
		direction   string
		leg         string
		current     float64
		entryFee    float64
		feeUnknown  bool
		pendingFees int
		want        float64
		verified    bool
	}{
		{name: "long gain", direction: "LONG", current: 110, want: 10, verified: true},
		{name: "long loss", direction: "LONG", current: 90, want: -10, verified: true},
		{name: "short gain", direction: "SHORT", current: 90, want: 10, verified: true},
		{name: "short loss", direction: "SHORT", current: 110, want: -10, verified: true},
		{name: "both short leg", direction: "BOTH", leg: "SHORT", current: 90, want: 10, verified: true},
		{name: "both long leg", direction: "BOTH", leg: "LONG", current: 90, want: -10, verified: true},
		{name: "both missing leg", direction: "BOTH", current: 90, want: 0},
		{name: "entry fee deducted", direction: "LONG", current: 110, entryFee: 0.5, want: 9.5, verified: true},
		{name: "entry fee unknown", direction: "LONG", current: 110, feeUnknown: true},
		{name: "entry fee pending", direction: "LONG", current: 110, pendingFees: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot := SlotInfo{PositionLeg: tt.leg, BuyFee: tt.entryFee, FeeValuationUnknown: tt.feeUnknown, PendingFeeSupplements: tt.pendingFees}
			got, verified := positionSlotUnrealizedPnL(tt.current, 100, 1, slot, tt.direction)
			if got != tt.want {
				t.Fatalf("unrealized pnl = %v, want %v", got, tt.want)
			}
			if verified != tt.verified {
				t.Fatalf("unrealized pnl verified = %v, want %v", verified, tt.verified)
			}
		})
	}
}
