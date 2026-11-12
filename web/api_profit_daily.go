package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting/daily"
	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
)

// futuresEvidenceReader is the production read-only adapter. Tests replace it
// so the import path never needs a live exchange account.
var futuresEvidenceReader = binanceFuturesEvidenceSource

// getProfitDailyReconciliationHandler exposes only an already-published daily
// report. It never computes or widens a report on read, and a missing report is
// not presented as a verified zero.
func getProfitDailyReconciliationHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return
	}
	date := strings.TrimSpace(c.Query("date"))
	exchange := strings.ToLower(strings.TrimSpace(c.Query("exchange_id")))
	scope := strings.TrimSpace(c.Query("account_scope"))
	if date == "" || exchange == "" || scope == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_requires_date_exchange_and_scope"})
		return
	}
	if _, _, err := daily.DayBounds(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_date_invalid"})
		return
	}
	authorized, _, err := resolveProfitAccountScopesWithHistory(c.Request.Context(), exchange, st)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_scope_unavailable"})
		return
	}
	granted := false
	for _, candidate := range authorized {
		if strings.EqualFold(candidate.exchange, exchange) && candidate.scope == scope {
			granted = true
			break
		}
	}
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "daily_reconciliation_scope_not_authorized"})
		return
	}
	repo, ok := st.(daily.Repository)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_unsupported_storage"})
		return
	}
	result, err := daily.NewService(repo).Read(c.Request.Context(), daily.Query{Exchange: exchange, AccountScope: scope, Date: date}, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, daily.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "daily_reconciliation_not_found"})
		case errors.Is(err, daily.ErrInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_invalid"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "dailyReconciliation": result})
}

// importProfitDailyFuturesEvidenceHandler copies one already-authorized Binance
// USD-M read-only sample into the daily futures wallet and publishes the day.
// The published report stays incomplete: the sample is not a day-boundary
// snapshot, and spot plus cross margin are still unobserved.
func importProfitDailyFuturesEvidenceHandler(c *gin.Context) {
	repo, exchangeName, scope, date, ok := authorizedDailyQuery(c)
	if !ok {
		return
	}
	if exchangeName != "binance" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_futures_evidence_binance_only"})
		return
	}
	source, testnet, err := futuresEvidenceReader(exchangeName)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_futures_evidence_source_unavailable"})
		return
	}
	start, _, err := daily.DayBounds(date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_date_invalid"})
		return
	}
	snapshot, err := source.ReadAccountEvidence(c.Request.Context(), start)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "daily_futures_evidence_read_failed", "detail": err.Error()})
		return
	}
	result, err := publishFuturesEvidence(c.Request.Context(), repo, daily.FuturesEvidenceImport{
		Query:    daily.Query{Exchange: exchangeName, AccountScope: scope, Date: date},
		Testnet:  testnet,
		Now:      time.Now().UTC(),
		Snapshot: snapshot,
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, daily.ErrInvalid) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": false, "error": "daily_futures_evidence_rejected", "detail": err.Error()})
		return
	}
	if result.EquityReconciliationComplete {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "daily_futures_evidence_must_stay_incomplete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "dailyReconciliation": result})
}

func authorizedDailyQuery(c *gin.Context) (daily.Repository, string, string, string, bool) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return nil, "", "", "", false
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return nil, "", "", "", false
	}
	date := strings.TrimSpace(c.Query("date"))
	exchangeName := strings.ToLower(strings.TrimSpace(c.Query("exchange_id")))
	scope := strings.TrimSpace(c.Query("account_scope"))
	if date == "" || exchangeName == "" || scope == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_requires_date_exchange_and_scope"})
		return nil, "", "", "", false
	}
	if _, _, err := daily.DayBounds(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_date_invalid"})
		return nil, "", "", "", false
	}
	authorized, _, err := resolveProfitAccountScopesWithHistory(c.Request.Context(), exchangeName, st)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_scope_unavailable"})
		return nil, "", "", "", false
	}
	granted := false
	for _, candidate := range authorized {
		if strings.EqualFold(candidate.exchange, exchangeName) && candidate.scope == scope {
			granted = true
			break
		}
	}
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "daily_reconciliation_scope_not_authorized"})
		return nil, "", "", "", false
	}
	repo, ok := st.(daily.Repository)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_unsupported_storage"})
		return nil, "", "", "", false
	}
	return repo, exchangeName, scope, date, true
}

func binanceFuturesEvidenceSource(exchangeName string) (accounting.Source, bool, error) {
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return nil, false, fmt.Errorf("load exchange config: %w", err)
	}
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if !ok {
		for name, candidate := range cfg.Exchanges {
			if strings.EqualFold(name, exchangeName) {
				exchangeCfg, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil, false, fmt.Errorf("exchange %s is not configured", exchangeName)
	}
	source, err := exchange.NewAccountEvidenceSource(exchangeName, "futures", exchangeCfg)
	if err != nil {
		return nil, false, err
	}
	return source, exchangeCfg.Testnet, nil
}

func publishFuturesEvidence(ctx context.Context, repo daily.Repository, in daily.FuturesEvidenceImport) (daily.Result, error) {
	evidence, _, err := daily.BuildFuturesWalletEvidence(in)
	if err != nil {
		return daily.Result{}, err
	}
	if _, err := repo.ImportDailyEquityEvidence(ctx, evidence, 0); err != nil {
		return daily.Result{}, err
	}
	loaded, err := repo.LoadDailyEquityInputs(ctx, in.Query)
	if err != nil {
		return daily.Result{}, err
	}
	result, err := daily.Evaluate(loaded, in.Now)
	if err != nil {
		return daily.Result{}, err
	}
	return repo.CommitDailyEquityResult(ctx, in.Query, result.InputDigest, result)
}
