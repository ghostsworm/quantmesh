package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func buildAccountWalletCapitalClaim(cfg *config.Config, exchangeName, market, quoteAsset string, amount, available float64) (storage.AccountWalletCapitalClaim, error) {
	if cfg == nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || math.IsNaN(available) || math.IsInf(available, 0) || available <= 0 {
		return storage.AccountWalletCapitalClaim{}, fmt.Errorf("wallet capital claim requires finite positive amount and verified balance")
	}
	exchangeName = strings.TrimSpace(exchangeName)
	market = strings.ToLower(strings.TrimSpace(market))
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if exchangeName == "" || market == "" || quoteAsset == "" || !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
		return storage.AccountWalletCapitalClaim{}, fmt.Errorf("wallet capital claim requires verified exchange, market, quote asset, and credential scope")
	}
	material := equityAccountScopeID(exchangeName, exchangeCfg) + "|" + market + "|" + quoteAsset
	walletKey := fmt.Sprintf("%x", sha256.Sum256([]byte(material)))
	token, err := newAccountWalletCapitalReservationToken()
	if err != nil {
		return storage.AccountWalletCapitalClaim{}, err
	}
	return storage.AccountWalletCapitalClaim{WalletKey: walletKey, ReservationToken: token, Amount: amount, Available: available}, nil
}

func newAccountWalletCapitalReservationToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("create account wallet reservation generation token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func reserveAccountWalletCapital(ctx context.Context, cfg *config.Config, storageService *storage.StorageService, distributedLock lock.DistributedLock, botID string, claims []storage.AccountWalletCapitalClaim) error {
	if ctx == nil || cfg == nil || strings.TrimSpace(botID) == "" || len(claims) == 0 {
		return fmt.Errorf("account wallet reservation requires context, config, Bot identity, and claims")
	}
	if storageService == nil || storageService.GetStorage() == nil {
		return fmt.Errorf("account wallet reservation requires persistent SQL storage")
	}
	store, ok := storageService.GetStorage().(storage.AccountWalletCapitalReservationStore)
	if !ok {
		return fmt.Errorf("SQL storage does not support atomic account wallet reservations")
	}
	if cfg.Instance.Total > 1 {
		shared, ok := store.(storage.MultiProcessAccountWalletCapitalStore)
		if !ok || !shared.SupportsMultiProcessAccountWalletCapital() {
			return fmt.Errorf("multi-instance account wallet reservations require shared MySQL storage")
		}
		if _, localOnly := distributedLock.(*lock.NopLock); localOnly || distributedLock == nil {
			return fmt.Errorf("multi-instance account wallet reservations require an enabled distributed lock")
		}
	}
	for index := range claims {
		if claims[index].ReservationToken == "" {
			token, err := newAccountWalletCapitalReservationToken()
			if err != nil {
				return err
			}
			claims[index].ReservationToken = token
		}
	}
	reserveCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := store.ReserveAccountWalletCapital(reserveCtx, botID, claims); err != nil {
		return fmt.Errorf("atomically reserve Bot %s account wallet capital: %w", botID, err)
	}
	return nil
}

func reserveRuntimeAccountWalletCapital(ctx context.Context, cfg *config.Config, storageService *storage.StorageService, distributedLock lock.DistributedLock, botID string, claims []storage.AccountWalletCapitalClaim, gate *execution.OpeningGate) error {
	if gate == nil || !gate.HasBlock(accountWalletCapitalReservationPendingBlock) {
		return fmt.Errorf("Bot opening gate must remain blocked until its wallet capital reservation commits")
	}
	if err := reserveAccountWalletCapital(ctx, cfg, storageService, distributedLock, botID, claims); err != nil {
		return err
	}
	gate.Unblock(accountWalletCapitalReservationPendingBlock)
	return nil
}

func verifyAndReleaseAccountWalletCapital(ctx context.Context, store storage.AccountWalletCapitalReservationStore, botID string, claims []storage.AccountWalletCapitalClaim, verifyFlat func(context.Context) error) error {
	return verifyAndReleaseAccountWalletCapitalGuarded(ctx, store, botID, claims, verifyFlat, nil)
}

func verifyAndReleaseAccountWalletCapitalGuarded(ctx context.Context, store storage.AccountWalletCapitalReservationStore, botID string, claims []storage.AccountWalletCapitalClaim, verifyFlat func(context.Context) error, guardBeforeRelease func() error) error {
	if ctx == nil || store == nil || strings.TrimSpace(botID) == "" || len(claims) == 0 || verifyFlat == nil {
		return fmt.Errorf("verified account wallet capital release requires context, store, Bot identity, claims, and verifier")
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, 15*time.Second)
	if err := verifyFlat(verifyCtx); err != nil {
		cancelVerify()
		return fmt.Errorf("refuse account wallet capital release without verified flatness: %w", err)
	}
	if err := verifyCtx.Err(); err != nil {
		cancelVerify()
		return fmt.Errorf("refuse account wallet capital release after verification deadline: %w", err)
	}
	cancelVerify()
	if guardBeforeRelease != nil {
		if err := guardBeforeRelease(); err != nil {
			return fmt.Errorf("refuse account wallet capital release because ownership guard failed: %w", err)
		}
	}
	releaseCtx, cancelRelease := context.WithTimeout(ctx, 10*time.Second)
	defer cancelRelease()
	if err := releaseCtx.Err(); err != nil {
		return fmt.Errorf("refuse account wallet capital release after verification: %w", err)
	}
	if guardBeforeRelease != nil {
		if err := guardBeforeRelease(); err != nil {
			return fmt.Errorf("refuse account wallet capital release because ownership guard failed: %w", err)
		}
	}
	if err := store.ReleaseAccountWalletCapital(releaseCtx, botID, claims); err != nil {
		return fmt.Errorf("release verified account wallet capital reservation: %w", err)
	}
	return nil
}

func verifyStandardRuntimeFlat(ctx context.Context, ex exchange.IExchange, market, symbol string) error {
	if ctx == nil || ex == nil || strings.TrimSpace(symbol) == "" {
		return fmt.Errorf("standard runtime flatness verification requires context, exchange, and symbol")
	}
	if strings.ToLower(strings.TrimSpace(market)) != "futures" {
		return fmt.Errorf("capital reservation release is unsupported for %q: spot inventory cannot be proven flat by the generic position interface", market)
	}
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		return fmt.Errorf("read live %s positions for capital release: %w", symbol, err)
	}
	if positions == nil {
		return fmt.Errorf("read live %s positions for capital release: nil response is not an authoritative empty snapshot", symbol)
	}
	for index, current := range positions {
		if current == nil {
			return fmt.Errorf("live %s position response item %d is nil", symbol, index)
		}
		if current.Symbol != "" && !strings.EqualFold(current.Symbol, symbol) {
			return fmt.Errorf("live position response symbol %q does not match %q", current.Symbol, symbol)
		}
		if math.IsNaN(current.Size) || math.IsInf(current.Size, 0) || current.Size != 0 {
			return fmt.Errorf("live %s position size %v is not verifiably flat", symbol, current.Size)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("capital release verification deadline after position read: %w", err)
	}
	orders, err := ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("read live %s open orders for capital release: %w", symbol, err)
	}
	if orders == nil {
		return fmt.Errorf("read live %s open orders for capital release: nil response is not an authoritative empty snapshot", symbol)
	}
	for index, openOrder := range orders {
		if openOrder == nil {
			return fmt.Errorf("live %s open order response item %d is nil", symbol, index)
		}
		if openOrder.Symbol != "" && !strings.EqualFold(openOrder.Symbol, symbol) {
			return fmt.Errorf("live open order response symbol %q does not match %q", openOrder.Symbol, symbol)
		}
		return fmt.Errorf("live %s open order %d remains active", symbol, openOrder.OrderID)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("capital release verification deadline after open-order read: %w", err)
	}
	return nil
}

func verifyStandardSpotRuntimeFlat(ctx context.Context, ex exchange.IExchange, symbol string, verifyOwnedInventory func() error) error {
	if ctx == nil || ex == nil || strings.TrimSpace(symbol) == "" || verifyOwnedInventory == nil {
		return fmt.Errorf("spot capital release requires context, exchange, symbol, and owned-inventory verifier")
	}
	if err := verifyOwnedInventory(); err != nil {
		return fmt.Errorf("spot Bot-owned inventory is not verifiably flat: %w", err)
	}
	inventoryReader, ok := ex.(exchange.SpotInventoryReader)
	if !ok {
		return fmt.Errorf("spot exchange does not expose authoritative total base-asset inventory")
	}
	baseAsset := strings.ToUpper(strings.TrimSpace(ex.GetBaseAsset()))
	if baseAsset == "" {
		return fmt.Errorf("spot base asset is unavailable for capital-release verification")
	}
	balance, err := inventoryReader.SpotInventoryQty(ctx)
	if err != nil {
		return fmt.Errorf("read total spot %s inventory for capital release: %w", baseAsset, err)
	}
	if math.IsNaN(balance) || math.IsInf(balance, 0) || balance < 0 || balance != 0 {
		return fmt.Errorf("spot %s balance %v is not verifiably empty", baseAsset, balance)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("spot capital-release deadline after base-balance read: %w", err)
	}
	orders, err := ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("read live %s spot orders for capital release: %w", symbol, err)
	}
	if orders == nil {
		return fmt.Errorf("read live %s spot orders for capital release: nil response is not an authoritative empty snapshot", symbol)
	}
	if len(orders) != 0 {
		return fmt.Errorf("live %s spot open-order snapshot contains %d orders", symbol, len(orders))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("spot capital-release deadline after open-order read: %w", err)
	}
	return nil
}

func verifyStandardSpotMarginRuntimeFlat(ctx context.Context, ex exchange.IExchange, verifyOwnedInventory func() error) error {
	if ctx == nil || ex == nil || verifyOwnedInventory == nil {
		return fmt.Errorf("spot-margin capital release requires context, exchange, and owned-inventory verifier")
	}
	if err := verifyOwnedInventory(); err != nil {
		return fmt.Errorf("spot-margin Bot-owned inventory is not verifiably flat: %w", err)
	}
	verifier, ok := ex.(exchange.SpotMarginFlatnessVerifier)
	if !ok {
		return fmt.Errorf("spot-margin exchange does not expose authoritative account-wide debt and order verification")
	}
	if err := verifier.VerifySpotMarginAccountFlat(ctx); err != nil {
		return fmt.Errorf("spot-margin account is not verifiably flat: %w", err)
	}
	return nil
}

func verifyStandardSpotBotInventoryFlat(grid *position.SuperPositionManager, strategies *strategy.StrategyManager, symbol string) error {
	if grid == nil || !grid.GridRuntimeStateIsVerifiedEmpty() {
		return fmt.Errorf("grid inventory state is missing, unverified, or non-empty")
	}
	if strategies == nil {
		return fmt.Errorf("spot strategy inventory manager is unavailable")
	}
	for name, currentStrategy := range strategies.GetAllStrategies() {
		if currentStrategy == nil {
			return fmt.Errorf("spot strategy %s is nil", name)
		}
		for index, current := range currentStrategy.GetPositions() {
			if current == nil || (current.Symbol != "" && !strings.EqualFold(current.Symbol, symbol)) ||
				math.IsNaN(current.Size) || math.IsInf(current.Size, 0) || current.Size != 0 {
				return fmt.Errorf("spot strategy %s position %d is not verifiably flat for %s", name, index, symbol)
			}
		}
		for index, current := range currentStrategy.GetOrders() {
			if current == nil || (current.Symbol != "" && !strings.EqualFold(current.Symbol, symbol)) || !terminalOrderUpdate(current.Status) {
				return fmt.Errorf("spot strategy %s order %d is not in a verified terminal state", name, index)
			}
		}
	}
	return nil
}
