package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func buildAccountWalletCapitalClaim(cfg *config.Config, exchangeName, market, quoteAsset string, amount, available float64) (storage.AccountWalletCapitalClaim, error) {
	return buildAccountWalletCapitalClaimFromObservation(cfg, exchangeName, market, quoteAsset, amount, available, time.Now().UTC())
}

func buildAccountWalletCapitalClaimFromObservation(cfg *config.Config, exchangeName, market, quoteAsset string, amount, available float64, observedAt time.Time) (storage.AccountWalletCapitalClaim, error) {
	if cfg == nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || math.IsNaN(available) || math.IsInf(available, 0) || available <= 0 {
		return storage.AccountWalletCapitalClaim{}, fmt.Errorf("wallet capital claim requires finite positive amount and verified balance")
	}
	if observedAt.IsZero() || observedAt.After(time.Now().Add(time.Minute)) {
		return storage.AccountWalletCapitalClaim{}, fmt.Errorf("wallet capital claim requires a valid balance observation time")
	}
	exchangeName = strings.TrimSpace(exchangeName)
	market = strings.ToLower(strings.TrimSpace(market))
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if exchangeName == "" || market == "" || quoteAsset == "" || !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
		return storage.AccountWalletCapitalClaim{}, fmt.Errorf("wallet capital claim requires verified exchange, market, quote asset, and credential scope")
	}
	walletKey, err := accountWalletCapitalKey(cfg, exchangeName, market, quoteAsset)
	if err != nil {
		return storage.AccountWalletCapitalClaim{}, err
	}
	token, err := newAccountWalletCapitalReservationToken()
	if err != nil {
		return storage.AccountWalletCapitalClaim{}, err
	}
	return storage.AccountWalletCapitalClaim{WalletKey: walletKey, ReservationToken: token, Amount: amount, Available: available, ObservedAt: observedAt.UTC()}, nil
}

func accountWalletCapitalKey(cfg *config.Config, exchangeName, market, quoteAsset string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("wallet identity requires verified configuration")
	}
	exchangeName = strings.TrimSpace(exchangeName)
	market = strings.ToLower(strings.TrimSpace(market))
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if exchangeName == "" || market == "" || quoteAsset == "" || !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
		return "", fmt.Errorf("wallet identity requires verified exchange, market, quote asset, and credential scope")
	}
	material := equityAccountScopeID(exchangeName, exchangeCfg) + "|" + market + "|" + quoteAsset
	return fmt.Sprintf("%x", sha256.Sum256([]byte(material))), nil
}

func beginAccountWalletBalanceObservation(ctx context.Context, storageService *storage.StorageService, walletKey string) (int64, error) {
	if ctx == nil || storageService == nil || storageService.GetStorage() == nil {
		return 0, fmt.Errorf("wallet balance observation requires persistent storage")
	}
	issuer, ok := storageService.GetStorage().(storage.AccountWalletBalanceObservationIssuer)
	if !ok {
		return 0, fmt.Errorf("storage does not support shared wallet observation sequencing")
	}
	sequence, err := issuer.BeginAccountWalletBalanceObservation(ctx, walletKey)
	if err != nil {
		return 0, fmt.Errorf("issue shared wallet balance observation sequence: %w", err)
	}
	return sequence, nil
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

const (
	accountWalletBalanceUnverifiedBlock = "account_wallet_balance_unverified"
	accountWalletCapitalRefreshInterval = time.Minute
)

type accountWalletBalanceReader struct {
	walletKey string
	read      func(context.Context) (accountWalletBalanceObservation, error)
}

type accountWalletBalanceObservation struct {
	Available           float64
	RequestedAt         time.Time
	ObservationSequence int64
}

func revalidateRuntimeAccountWalletCapital(ctx context.Context, cfg *config.Config, storageService *storage.StorageService,
	distributedLock lock.DistributedLock, botID string, claims []storage.AccountWalletCapitalClaim,
	readers []accountWalletBalanceReader, gate *execution.OpeningGate, cancelOpenings func(context.Context) error) (resultErr error) {
	if ctx == nil || gate == nil || len(claims) == 0 || len(readers) != len(claims) {
		return fmt.Errorf("runtime wallet capital revalidation requires complete claims, readers, and opening gate")
	}
	gate.Block(accountWalletBalanceUnverifiedBlock)
	defer func() {
		if resultErr == nil || cancelOpenings == nil {
			return
		}
		cancelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if cancelErr := cancelOpenings(cancelCtx); cancelErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cancel Bot-owned opening orders after wallet revalidation failure: %w", cancelErr))
		}
	}()
	refreshed, err := observeRuntimeWalletCapitalClaims(ctx, claims, readers)
	if err != nil {
		return err
	}
	gate.Block(accountWalletCapitalReservationPendingBlock)
	if err := reserveRuntimeAccountWalletCapital(ctx, cfg, storageService, distributedLock, botID, refreshed, gate); err != nil {
		return fmt.Errorf("revalidate aggregate account wallet reservations: %w", err)
	}
	gate.Unblock(accountWalletBalanceUnverifiedBlock)
	return nil
}

func observeRuntimeWalletCapitalClaims(ctx context.Context, claims []storage.AccountWalletCapitalClaim,
	readers []accountWalletBalanceReader) ([]storage.AccountWalletCapitalClaim, error) {
	refreshed := append([]storage.AccountWalletCapitalClaim(nil), claims...)
	readerByWallet, err := accountWalletCapitalReadersByWallet(readers)
	if err != nil {
		return nil, err
	}
	for index := range refreshed {
		readBalance := readerByWallet[refreshed[index].WalletKey]
		if readBalance == nil {
			return nil, fmt.Errorf("runtime wallet capital balance reader is missing for a reserved wallet")
		}
		available, err := readRuntimeWalletBalance(ctx, refreshed[index].WalletKey, readBalance)
		if err != nil {
			return nil, err
		}
		refreshed[index].Available = available.Available
		refreshed[index].ObservedAt = available.RequestedAt
		refreshed[index].ObservationSequence = available.ObservationSequence
	}
	return refreshed, nil
}

func accountWalletCapitalReadersByWallet(readers []accountWalletBalanceReader) (map[string]func(context.Context) (accountWalletBalanceObservation, error), error) {
	byWallet := make(map[string]func(context.Context) (accountWalletBalanceObservation, error), len(readers))
	for _, reader := range readers {
		if reader.walletKey == "" || reader.read == nil {
			return nil, fmt.Errorf("runtime wallet capital balance reader is incomplete")
		}
		if _, exists := byWallet[reader.walletKey]; exists {
			return nil, fmt.Errorf("duplicate runtime wallet capital balance reader")
		}
		byWallet[reader.walletKey] = reader.read
	}
	return byWallet, nil
}

func readRuntimeWalletBalance(ctx context.Context, walletKey string, read func(context.Context) (accountWalletBalanceObservation, error)) (accountWalletBalanceObservation, error) {
	balanceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	observation, err := read(balanceCtx)
	if err != nil {
		return accountWalletBalanceObservation{}, fmt.Errorf("refresh available balance for wallet %s: %w", walletKey, err)
	}
	if math.IsNaN(observation.Available) || math.IsInf(observation.Available, 0) || observation.Available <= 0 ||
		observation.RequestedAt.IsZero() || observation.RequestedAt.After(time.Now().Add(time.Minute)) || observation.ObservationSequence <= 0 {
		return accountWalletBalanceObservation{}, fmt.Errorf("refreshed available balance evidence for wallet %s is invalid", walletKey)
	}
	return observation, nil
}

// readAccountWalletCapitalValue reads the quote-denominated account equity
// used by wallet-budget validation. Derivatives must use margin equity rather
// than free collateral, which naturally falls when positions/orders are active.
// Spot currently uses quote-asset wallet value; base-asset inventory valuation
// remains an explicit limitation and must not be inferred from free quote.
func readAccountWalletCapitalValue(ctx context.Context, client exchange.IExchange, quoteAsset, symbol string) (float64, error) {
	if ctx == nil || client == nil {
		return 0, fmt.Errorf("wallet capital equity requires context and exchange client")
	}
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if quoteAsset == "" {
		return 0, fmt.Errorf("wallet capital equity requires a quote asset")
	}
	account, err := client.GetAccount(ctx)
	if err != nil {
		return 0, fmt.Errorf("read %s %s account equity: %w", client.GetName(), client.GetMarketType(), err)
	}
	if account == nil {
		return 0, fmt.Errorf("%s %s account equity is unavailable", client.GetName(), client.GetMarketType())
	}
	accountAsset := strings.ToUpper(strings.TrimSpace(account.BalanceAsset))
	var equity float64
	switch strings.ToLower(strings.TrimSpace(client.GetMarketType())) {
	case "futures", "spot_margin":
		equity = account.TotalMarginBalance
	case "spot":
		equity = account.TotalWalletBalance
	default:
		return 0, fmt.Errorf("unsupported market type %q for wallet capital equity", client.GetMarketType())
	}
	if accountAsset != quoteAsset {
		baseAsset := strings.ToUpper(strings.TrimSpace(client.GetBaseAsset()))
		marketQuoteAsset := strings.ToUpper(strings.TrimSpace(client.GetQuoteAsset()))
		if accountAsset != baseAsset || quoteAsset != marketQuoteAsset || baseAsset == "" || marketQuoteAsset == "" || strings.TrimSpace(symbol) == "" {
			return 0, fmt.Errorf("%s %s account equity currency %q cannot be valued in %s for %s", client.GetName(), client.GetMarketType(), accountAsset, quoteAsset, symbol)
		}
		price, err := client.GetLatestPrice(ctx, symbol)
		if err != nil {
			return 0, fmt.Errorf("value %s-denominated account equity in %s using %s: %w", accountAsset, quoteAsset, symbol, err)
		}
		if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 || equity > math.MaxFloat64/price {
			return 0, fmt.Errorf("%s %s account equity conversion price is invalid", client.GetName(), symbol)
		}
		equity *= price
	}
	if math.IsNaN(equity) || math.IsInf(equity, 0) || equity <= 0 {
		return 0, fmt.Errorf("%s %s %s account equity is invalid", client.GetName(), client.GetMarketType(), quoteAsset)
	}
	return equity, nil
}

func startRuntimeAccountWalletCapitalRevalidation(ctx context.Context, cfg *config.Config, storageService *storage.StorageService,
	distributedLock lock.DistributedLock, botID string, claims []storage.AccountWalletCapitalClaim,
	readers []accountWalletBalanceReader, gate *execution.OpeningGate, cancelOpenings func(context.Context) error) func() {
	if ctx == nil || gate == nil || len(claims) == 0 {
		return func() {}
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(accountWalletCapitalRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := revalidateRuntimeAccountWalletCapital(workerCtx, cfg, storageService, distributedLock, botID, claims, readers, gate, cancelOpenings); err != nil {
					logger.ErrorCtx(workerCtx, "[%s] account wallet balance revalidation failed; opening remains blocked: %v", botID, err)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func accountWalletBalanceReaderForClaim(claim storage.AccountWalletCapitalClaim, client exchange.IExchange, storageService *storage.StorageService) accountWalletBalanceReader {
	return accountWalletBalanceReader{walletKey: claim.WalletKey, read: func(ctx context.Context) (accountWalletBalanceObservation, error) {
		if client == nil {
			return accountWalletBalanceObservation{}, fmt.Errorf("exchange client is unavailable")
		}
		sequence, err := beginAccountWalletBalanceObservation(ctx, storageService, claim.WalletKey)
		if err != nil {
			return accountWalletBalanceObservation{}, err
		}
		requestedAt := time.Now().UTC()
		available, err := readAccountWalletCapitalValue(ctx, client, claim.QuoteAsset, claim.Symbol)
		return accountWalletBalanceObservation{Available: available, RequestedAt: requestedAt, ObservationSequence: sequence}, err
	}}
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
		if !strings.EqualFold(strings.TrimSpace(current.Symbol), strings.TrimSpace(symbol)) {
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
		if !strings.EqualFold(strings.TrimSpace(openOrder.Symbol), strings.TrimSpace(symbol)) {
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
