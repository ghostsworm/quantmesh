package main

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/storage"
	"quantmesh/web"
)

// runtimeTrustedOrderEvidenceProvider reads a single Bot's loaded owned intent
// and independently verifies its terminal order and complete venue fill ledger.
// It is deliberately read-only and never consumes client-supplied economics.
type runtimeTrustedOrderEvidenceProvider struct {
	bots *BotManager
}

func newRuntimeTrustedOrderEvidenceProvider(bots *BotManager) *runtimeTrustedOrderEvidenceProvider {
	return &runtimeTrustedOrderEvidenceProvider{bots: bots}
}

var _ web.TrustedOrderEvidenceProvider = (*runtimeTrustedOrderEvidenceProvider)(nil)

func (p *runtimeTrustedOrderEvidenceProvider) ReadVerifiedOrderEvidence(ctx context.Context, item storage.OrderReconciliationCase) (web.VerifiedOrderEvidence, error) {
	if p == nil || p.bots == nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("Bot runtime registry unavailable")
	}
	if err := ctx.Err(); err != nil {
		return web.VerifiedOrderEvidence{}, err
	}
	if err := item.Owner.Validate(); err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("invalid reconciliation owner: %w", err)
	}
	if strings.TrimSpace(item.ExpectedIntentRevision) == "" {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("expected owned-intent revision is required")
	}
	bot, found := p.bots.Get(item.Owner.Bot)
	if !found || bot == nil || bot.Inner == nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("unique Bot runtime is unavailable")
	}
	runtime := bot.Inner
	if bot.BotID != item.Owner.Bot || runtime.AccountScope == "" || runtime.AccountScope != item.Owner.AccountScope ||
		runtime.Exchange == nil || runtime.ExchangeExecutor == nil ||
		runtime.Exchange.GetName() != item.Owner.Exchange || runtime.AccountMarketType != item.Owner.Market ||
		runtime.Config.Symbol != item.Owner.Symbol {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("Bot account, exchange, market, or symbol scope mismatch")
	}

	intent, err := runtime.ExchangeExecutor.ReadOwnedIntentSnapshot(parseVenueOrderID(item.Owner.VenueOrderID), item.Owner.ClientOrderID)
	if err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("read exact owned-intent snapshot: %w", err)
	}
	revision := strconv.FormatInt(intent.Revision, 10)
	if intent.BotID != item.Owner.Bot || intent.Symbol != item.Owner.Symbol ||
		intent.StrategyName != item.Owner.StrategyName || intent.StrategyType != item.Owner.StrategyType ||
		intent.VenueOrderID != parseVenueOrderID(item.Owner.VenueOrderID) || intent.ClientOrderID != item.Owner.ClientOrderID ||
		revision != item.ExpectedIntentRevision || intent.LedgerPending || intent.Rejected {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("owned-intent identity, revision, or recovery state mismatch")
	}
	positionSide := strings.ToUpper(strings.TrimSpace(intent.PositionSide))
	if positionSide == "" || (intent.Opening && intent.ReduceOnly) || (!intent.Opening && intent.ReduceOnly && positionSide == "") {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("owned-intent position side or open/close role is incomplete")
	}
	orderRole := "exit"
	if intent.Opening {
		orderRole = "entry"
	}

	venueOrder, err := runtime.Exchange.GetOrder(ctx, item.Owner.Symbol, intent.VenueOrderID)
	if err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("read venue order: %w", err)
	}
	if venueOrder == nil || venueOrder.OrderID != intent.VenueOrderID || venueOrder.Symbol != item.Owner.Symbol ||
		venueOrder.ClientOrderID != item.Owner.ClientOrderID || string(venueOrder.Side) != intent.Side ||
		venueOrder.Quantity != intent.RequestedQuantity || venueOrder.ExecutedQty != intent.ExecutedQuantity ||
		!terminalVenueStatus(venueOrder.Status) || !intent.Terminal {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("venue order identity, terminal state, or executed quantity mismatch")
	}
	if !finitePositive(venueOrder.Quantity) || !finiteNonNegative(venueOrder.ExecutedQty) || venueOrder.ExecutedQty > venueOrder.Quantity {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("venue order quantity is invalid")
	}

	venueFills, err := runtime.Exchange.GetOrderFills(ctx, item.Owner.Symbol, venueOrder.OrderID)
	if err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("read venue order fills: %w", err)
	}
	if len(venueFills) == 0 && venueOrder.ExecutedQty != 0 {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fill history is incomplete")
	}
	fills := make([]*exchange.OrderFill, len(venueFills))
	copy(fills, venueFills)
	for _, fill := range fills {
		if fill == nil || fill.OrderID != venueOrder.OrderID || fill.Symbol != item.Owner.Symbol ||
			string(fill.Side) != intent.Side || strings.TrimSpace(fill.TradeID) == "" ||
			!finitePositive(fill.Price) || !finitePositive(fill.Quantity) || !finiteNumber(fill.Commission) ||
			strings.TrimSpace(fill.CommissionAsset) == "" || fill.TradeTime <= 0 {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fill identity, quantity, price, fee, or time is incomplete")
		}
		// The shared exchange.OrderFill contract has no CommissionKnown bit.
		// A zero value can therefore mean either an explicit zero fee or missing
		// venue data; do not certify it as complete without an authoritative signal.
		if fill.Commission == 0 {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fill fee amount is not provably present")
		}
	}
	sort.Slice(fills, func(i, j int) bool {
		if fills[i].TradeTime != fills[j].TradeTime {
			return fills[i].TradeTime < fills[j].TradeTime
		}
		return fills[i].TradeID < fills[j].TradeID
	})

	seenTradeIDs := make(map[string]struct{}, len(fills))
	verified := make([]accounting.VerifiedFill, 0, len(fills))
	positionDelta := new(big.Rat)
	realizedPnL := make(map[string]*big.Rat)
	fmtExecuted, err := exactCanonicalFloat(venueOrder.ExecutedQty)
	if err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("venue executed quantity is not losslessly representable: %w", err)
	}
	filled := new(big.Rat)
	for index, fill := range fills {
		tradeID := strings.TrimSpace(fill.TradeID)
		if tradeID != fill.TradeID {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("venue trade ID is not canonical")
		}
		if _, duplicate := seenTradeIDs[tradeID]; duplicate {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("duplicate venue trade ID")
		}
		seenTradeIDs[tradeID] = struct{}{}
		price, priceErr := exactCanonicalFloat(fill.Price)
		quantity, quantityErr := exactCanonicalFloat(fill.Quantity)
		commission, commissionErr := exactCanonicalFloat(fill.Commission)
		if priceErr != nil || quantityErr != nil || commissionErr != nil {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fill contains a value that cannot be represented losslessly")
		}
		if fill.BaseFeeQty != 0 {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("base-asset fee is not represented by the reconciliation fill contract")
		}
		quantityRat, ok := new(big.Rat).SetString(quantity)
		if !ok {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fill quantity is invalid")
		}
		filled.Add(filled, quantityRat)
		if err := accumulateOrderEconomics(positionDelta, realizedPnL, quantityRat, fill, positionSide, orderRole); err != nil {
			return web.VerifiedOrderEvidence{}, err
		}
		verified = append(verified, accounting.VerifiedFill{
			TradeID: tradeID, CursorSequence: uint64(index + 1), Side: string(fill.Side),
			PositionSide: positionSide, OrderRole: orderRole, Price: price, Quantity: quantity,
			CommissionAmount: commission, CommissionAsset: strings.TrimSpace(fill.CommissionAsset),
			TradeTime: time.UnixMilli(fill.TradeTime).UTC(),
		})
	}
	executedRat, ok := new(big.Rat).SetString(fmtExecuted)
	if !ok || filled.Cmp(executedRat) != 0 {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("venue fills do not exactly match order executed quantity")
	}

	target := accounting.Cursor{}
	if len(verified) > 0 {
		last := verified[len(verified)-1]
		target = accounting.Cursor{Sequence: last.CursorSequence, TradeID: last.TradeID}
	}
	if err := target.Validate(); err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("invalid terminal fill cursor: %w", err)
	}
	netQuantity, err := canonicalRat(positionDelta)
	if err != nil {
		return web.VerifiedOrderEvidence{}, fmt.Errorf("order net quantity is not canonical: %w", err)
	}
	assets := make([]string, 0, len(realizedPnL))
	for asset := range realizedPnL {
		assets = append(assets, asset)
	}
	sort.Strings(assets)
	verifiedPnL := make([]accounting.AssetAmount, 0, len(assets))
	for _, asset := range assets {
		amount, amountErr := canonicalRat(realizedPnL[asset])
		if amountErr != nil {
			return web.VerifiedOrderEvidence{}, fmt.Errorf("realized PnL for %s is not canonical: %w", asset, amountErr)
		}
		verifiedPnL = append(verifiedPnL, accounting.AssetAmount{Asset: asset, Amount: amount})
	}
	return web.VerifiedOrderEvidence{
		Owner: item.Owner, IntentRevision: revision, OwnerMatched: true, VenueTerminal: true,
		FillsComplete: true, FeesComplete: true, Target: target, Fills: verified,
		ExpectedResult: accounting.EconomicResult{NetQuantity: netQuantity, RealizedPnL: verifiedPnL},
		Summary: accounting.EvidenceSummary{
			Source: "trusted_exchange_order_and_fills", Summary: "server-side order and complete per-trade fill/fee readback",
			VerifiedBy: "trusted_runtime_provider", VerifiedAt: time.Now().UTC(),
		},
	}, nil
}

func accumulateOrderEconomics(positionDelta *big.Rat, realizedPnL map[string]*big.Rat, quantity *big.Rat, fill *exchange.OrderFill, positionSide, orderRole string) error {
	side := strings.ToUpper(strings.TrimSpace(string(fill.Side)))
	expectedSide := "SELL"
	sign := int64(-1)
	if positionSide == "LONG" {
		if orderRole == "entry" {
			expectedSide, sign = "BUY", 1
		}
	} else if positionSide == "SHORT" {
		if orderRole == "entry" {
			expectedSide, sign = "SELL", -1
		} else {
			expectedSide, sign = "BUY", 1
		}
	} else {
		return fmt.Errorf("unsupported position side for economic verification")
	}
	if side != expectedSide {
		return fmt.Errorf("venue side conflicts with position side and open/close role")
	}
	positionDelta.Add(positionDelta, new(big.Rat).Mul(quantity, new(big.Rat).SetInt64(sign)))
	if orderRole == "exit" && !fill.RealizedPnLKnown {
		return fmt.Errorf("venue realized PnL is not authoritative for a closing fill")
	}
	if fill.RealizedPnLKnown {
		asset := strings.TrimSpace(fill.RealizedPnLAsset)
		if asset == "" || asset != fill.RealizedPnLAsset || !finiteNumber(fill.RealizedPnL) {
			return fmt.Errorf("venue realized PnL asset or amount is incomplete")
		}
		amountText, err := exactCanonicalFloat(fill.RealizedPnL)
		if err != nil {
			return fmt.Errorf("venue realized PnL is not losslessly representable: %w", err)
		}
		amount, ok := new(big.Rat).SetString(amountText)
		if !ok {
			return fmt.Errorf("venue realized PnL is invalid")
		}
		if realizedPnL[asset] == nil {
			realizedPnL[asset] = new(big.Rat)
		}
		realizedPnL[asset].Add(realizedPnL[asset], amount)
	}
	return nil
}

func canonicalRat(value *big.Rat) (string, error) {
	if value == nil || value.Sign() == 0 {
		return "0", nil
	}
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	countTwo, countFive := 0, 0
	for new(big.Int).Mod(denominator, two).Sign() == 0 {
		denominator.Div(denominator, two)
		countTwo++
	}
	for new(big.Int).Mod(denominator, five).Sign() == 0 {
		denominator.Div(denominator, five)
		countFive++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("value has a repeating decimal representation")
	}
	scale := countTwo
	if countFive > scale {
		scale = countFive
	}
	decimal := value.FloatString(scale)
	if strings.Contains(decimal, ".") {
		decimal = strings.TrimRight(strings.TrimRight(decimal, "0"), ".")
	}
	if decimal == "-0" || decimal == "" {
		decimal = "0"
	}
	if err := accounting.ValidateCanonicalDecimal(decimal); err != nil {
		return "", err
	}
	return decimal, nil
}

func parseVenueOrderID(value string) int64 {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0
	}
	return id
}

func terminalVenueStatus(status exchange.OrderStatus) bool {
	switch strings.ToUpper(strings.TrimSpace(string(status))) {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
		return true
	default:
		return false
	}
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// exactCanonicalFloat emits a canonical decimal that round-trips to the same
// finite float64; values that would lose machine precision are rejected.
func exactCanonicalFloat(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("non-finite number")
	}
	if value == 0 {
		return "0", nil
	}
	decimal := strconv.FormatFloat(value, 'f', -1, 64)
	parsed, err := strconv.ParseFloat(decimal, 64)
	if err != nil || parsed != value {
		return "", fmt.Errorf("decimal round-trip failed")
	}
	if err := accounting.ValidateCanonicalDecimal(decimal); err != nil {
		return "", err
	}
	return decimal, nil
}
