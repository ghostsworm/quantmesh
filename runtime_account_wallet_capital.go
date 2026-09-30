package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/lock"
	"quantmesh/storage"
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
	return storage.AccountWalletCapitalClaim{WalletKey: walletKey, Amount: amount, Available: available}, nil
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
	reserveCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := store.ReserveAccountWalletCapital(reserveCtx, botID, claims); err != nil {
		return fmt.Errorf("atomically reserve Bot %s account wallet capital: %w", botID, err)
	}
	return nil
}
