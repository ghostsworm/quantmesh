package order

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
)

const capitalReleaseIntentLockRetryInterval = 5 * time.Millisecond

// The caller owns intentMu only on success. No background locker survives a
// cancelled proof and takes the mutex later, blocking normal order recovery.
func (oe *ExchangeOrderExecutor) lockCapitalReleaseIntents(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("capital release intent lock requires context")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if oe.intentMu.TryLock() {
			if err := ctx.Err(); err != nil {
				oe.intentMu.Unlock()
				return err
			}
			return nil
		}
		if err := waitForOrderRetry(ctx, capitalReleaseIntentLockRetryInterval); err != nil {
			return err
		}
	}
}

// VerifyCapitalReleaseOwner requires the startup owner, journal, and exact
// exchange client instance to agree. Exchange name/symbol alone cannot prove
// account identity when two clients use different accounts on the same venue.
func (oe *ExchangeOrderExecutor) VerifyCapitalReleaseOwner(ctx context.Context, expected execution.IntentScope, venue exchange.IExchange) error {
	if err := oe.lockCapitalReleaseIntents(ctx); err != nil {
		return err
	}
	defer oe.intentMu.Unlock()
	if !oe.journalRequired || !oe.journalLoaded || oe.intentJournal == nil || expected != oe.intentScope {
		return fmt.Errorf("capital release owner journal binding is unverified")
	}
	key, err := expected.Key()
	if err != nil || key != oe.intentScopeKey || expected.Symbol != oe.symbol || expected.Bot != oe.botID {
		return fmt.Errorf("capital release owner identity is incomplete or mismatched")
	}
	if venue == nil || oe.exchange == nil || reflect.TypeOf(venue) != reflect.TypeOf(oe.exchange) ||
		!reflect.ValueOf(venue).Comparable() || !reflect.ValueOf(oe.exchange).Comparable() {
		return fmt.Errorf("capital release exchange instance binding is unverified")
	}
	if value := reflect.ValueOf(venue); value.Kind() == reflect.Ptr && value.IsNil() {
		return fmt.Errorf("capital release exchange instance is unavailable")
	}
	if venue != oe.exchange || venue.GetName() != expected.Exchange || venue.GetMarketType() != expected.Market {
		return fmt.Errorf("capital release exchange owner binding is mismatched")
	}
	return ctx.Err()
}

// VerifyCapitalReleaseIntents proves only that this owner's local and durable
// intents are economically settled. It does NOT prove venue/inventory flatness.
// The caller must hold the submission coordination lease and drain barrier,
// verify live inventory and strategy accounting, and keep those barriers held
// through the allocator's revision-checked release.
func (oe *ExchangeOrderExecutor) VerifyCapitalReleaseIntents(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("capital release intent verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := oe.lockCapitalReleaseIntents(ctx); err != nil {
		return err
	}
	defer oe.intentMu.Unlock()
	if !oe.journalRequired || !oe.journalLoaded || oe.intentJournal == nil {
		return fmt.Errorf("capital release requires a loaded durable intent journal")
	}
	key, err := oe.intentScope.Key()
	if err != nil || key != oe.intentScopeKey {
		return fmt.Errorf("capital release intent owner scope is unverified")
	}
	for _, intent := range oe.intents {
		if err := capitalReleaseIntentSettled(intent); err != nil {
			return fmt.Errorf("local capital release intent is unresolved: %w", err)
		}
	}
	// Re-read storage, rather than trusting a once-loaded map: failed writes or
	// another process can leave a durable hold absent from this process's map.
	seen := make(map[string]struct{})
	var after int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := oe.intentJournal.LoadExecutionIntents(ctx, key, after, intentJournalPageSize)
		if err != nil {
			return fmt.Errorf("read capital release intent journal: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(page) > intentJournalPageSize {
			return fmt.Errorf("oversized capital release intent page")
		}
		for _, record := range page {
			if record.ID <= after || record.Revision <= 0 || record.ClientOrderID == "" {
				return fmt.Errorf("invalid capital release intent cursor or identity")
			}
			if _, duplicate := seen[record.ClientOrderID]; duplicate {
				return fmt.Errorf("duplicate capital release intent identity")
			}
			seen[record.ClientOrderID] = struct{}{}
			after = record.ID
			intent, err := oe.decodeJournalIntent(record)
			if err != nil {
				return fmt.Errorf("decode capital release intent journal: %w", err)
			}
			if err := capitalReleaseIntentSettled(intent); err != nil {
				return fmt.Errorf("durable capital release intent is unresolved: %w", err)
			}
		}
		if len(page) < intentJournalPageSize {
			return nil
		}
	}
}

func capitalReleaseIntentSettled(intent *ownedIntent) error {
	if intent == nil || intent.unknown || intent.ledgerPending || len(intent.ledgerPayload) != 0 {
		return fmt.Errorf("intent or trade ledger requires reconciliation: %w", execution.ErrOrderUnknown)
	}
	if intent.rejected && intent.order == nil {
		return nil // Known local refusal, never a venue order or uncertain outcome.
	}
	ord := intent.order
	if intent.rejected || !intent.settled || ord == nil || !terminalOrderStatus(ord.Status) ||
		intent.request.Quantity <= 0 || math.IsNaN(intent.request.Quantity) || math.IsInf(intent.request.Quantity, 0) ||
		ord.OrderID <= 0 || ord.ClientOrderID != intent.request.ClientOrderID ||
		ord.Symbol != intent.request.Symbol || ord.Side != intent.request.Side ||
		ord.Quantity != intent.request.Quantity || ord.ExecutedQty < 0 ||
		ord.ExecutedQty > ord.Quantity || math.IsNaN(ord.ExecutedQty) || math.IsInf(ord.ExecutedQty, 0) {
		return fmt.Errorf("intent is not verifiably economically settled: %w", execution.ErrOrderUnknown)
	}
	return nil
}
