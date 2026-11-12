package daily

import (
	"fmt"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
)

const (
	// FuturesEvidenceSource names the read-only Binance USD-M account sample
	// that this import copies. It is a candidate capture, not a historical
	// opening or closing endpoint.
	FuturesEvidenceSource = "binance_usdm_account_evidence"

	// ReasonFuturesEvidenceCandidate keeps a futures-only import incomplete:
	// the sample is not a day boundary snapshot.
	ReasonFuturesEvidenceCandidate = "futures_evidence_candidate_sample"
	// ReasonWalletEvidenceMissing names a wallet the import cannot observe.
	// Spot and cross margin have no account-evidence source here, so a
	// futures-only import must stay incomplete for exactly these two.
	ReasonWalletEvidenceMissing = "wallet_evidence_missing"
)

// FuturesEvidenceImport is one closed natural day plus the single futures
// wallet sample that an existing read-only adapter already produced.
type FuturesEvidenceImport struct {
	Query    Query
	Testnet  bool
	Revision int64
	Now      time.Time
	Snapshot accounting.Snapshot
}

// BuildFuturesWalletEvidence copies one futures candidate sample into daily
// evidence. It never fabricates spot or cross-margin balances, and it never
// marks the day complete: the caller still has to publish through the normal
// repository path, which keeps the three-wallet contract intact.
func BuildFuturesWalletEvidence(in FuturesEvidenceImport) (Evidence, []string, error) {
	if strings.ToLower(strings.TrimSpace(in.Query.Exchange)) != "binance" {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence import only accepts binance", ErrInvalid)
	}
	if strings.TrimSpace(in.Query.AccountScope) == "" {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence import requires an account scope", ErrInvalid)
	}
	if in.Revision < 0 {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence scope revision must not be negative", ErrInvalid)
	}
	start, end, err := DayBounds(in.Query.Date)
	if err != nil {
		return Evidence{}, nil, err
	}
	now := in.Now.UTC()
	if now.Before(end.Add(SafetyLag)) {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence import requires a closed day past the safety lag", ErrInvalid)
	}
	wallet, err := futuresWallet(in.Snapshot)
	if err != nil {
		return Evidence{}, nil, err
	}
	if wallet.From.After(start) {
		return Evidence{}, nil, fmt.Errorf("%w: futures income cursor starts after the day opened", ErrInvalid)
	}
	if wallet.Through.Before(end) {
		return Evidence{}, nil, fmt.Errorf("%w: futures income cursor ends before the day closed", ErrInvalid)
	}
	if in.Snapshot.ObservedAt.IsZero() || in.Snapshot.ObservedAt.UTC().Before(end) || in.Snapshot.ObservedAt.UTC().After(now) {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence observation is outside the closed day", ErrInvalid)
	}
	if strings.TrimSpace(in.Snapshot.Currency) != Asset {
		return Evidence{}, nil, fmt.Errorf("%w: futures evidence currency must be %s", ErrInvalid, Asset)
	}

	balance := in.Snapshot.Wallet.Balance
	if balance == "" {
		balance = wallet.Balance
	}
	if _, err := accounting.Decimal(balance); err != nil {
		return Evidence{}, nil, fmt.Errorf("%w: futures wallet balance: %v", ErrInvalid, err)
	}
	events := make([]Event, 0, len(in.Snapshot.Entries))
	for _, entry := range in.Snapshot.Entries {
		event, ok, convErr := futuresEvent(entry, start, end)
		if convErr != nil {
			return Evidence{}, nil, convErr
		}
		if ok {
			events = append(events, event)
		}
	}

	evidence := Evidence{
		Scope: Scope{
			Exchange:         "binance",
			AccountScope:     strings.TrimSpace(in.Query.AccountScope),
			Testnet:          in.Testnet,
			Revision:         in.Revision,
			Wallets:          []string{WalletSpot, WalletMargin, WalletFutures},
			HistoryStart:     start,
			EffectiveFrom:    start,
			EffectiveThrough: end,
		},
		Date:     in.Query.Date,
		Timezone: Timezone,
		Method:   MethodVersion,
		Opening: Snapshot{
			SourceID:      FuturesEvidenceSource,
			At:            start,
			ObservedAt:    in.Snapshot.ObservedAt.UTC(),
			ScopeRevision: in.Revision,
			Complete:      false,
		},
		Closing: Snapshot{
			SourceID:      FuturesEvidenceSource,
			At:            end,
			ObservedAt:    in.Snapshot.ObservedAt.UTC(),
			ScopeRevision: in.Revision,
			Complete:      false,
			Balances: []Balance{{
				Wallet:      WalletFutures,
				Asset:       Asset,
				Quantity:    balance,
				Price:       "1",
				PriceSource: FuturesEvidenceSource,
				PriceAt:     in.Snapshot.ObservedAt.UTC(),
			}},
		},
		Events: events,
		Notes: []string{
			ReasonFuturesEvidenceCandidate,
			ReasonWalletEvidenceMissing + ":" + WalletSpot,
			ReasonWalletEvidenceMissing + ":" + WalletMargin,
		},
	}
	return evidence, append([]string(nil), evidence.Notes...), nil
}

func futuresWallet(snapshot accounting.Snapshot) (accounting.Wallet, error) {
	wallet, ok := snapshot.Wallets[WalletFutures]
	if !ok {
		wallet = snapshot.Wallet
	}
	if wallet.From.IsZero() || wallet.Through.IsZero() || wallet.Through.Before(wallet.From) {
		return accounting.Wallet{}, fmt.Errorf("%w: futures evidence has no income window", ErrInvalid)
	}
	if strings.TrimSpace(wallet.Balance) == "" && strings.TrimSpace(snapshot.Wallet.Balance) == "" {
		return accounting.Wallet{}, fmt.Errorf("%w: futures evidence has no wallet balance", ErrInvalid)
	}
	return wallet, nil
}

func futuresEvent(entry accounting.Entry, start, end time.Time) (Event, bool, error) {
	if entry.At.Before(start) || !entry.At.Before(end) {
		return Event{}, false, nil
	}
	if strings.TrimSpace(entry.Currency) != Asset {
		return Event{}, false, fmt.Errorf("%w: futures income %s is not %s", ErrInvalid, entry.ID, Asset)
	}
	if strings.TrimSpace(entry.ID) == "" {
		return Event{}, false, fmt.Errorf("%w: futures income has no identity", ErrInvalid)
	}
	if _, err := accounting.Decimal(entry.Amount); err != nil {
		return Event{}, false, fmt.Errorf("%w: futures income %s amount: %v", ErrInvalid, entry.ID, err)
	}
	kind, ok := futuresKind(entry.Kind)
	if !ok {
		return Event{}, false, fmt.Errorf("%w: futures income %s kind %q is not classified for daily evidence", ErrInvalid, entry.ID, entry.Kind)
	}
	return Event{
		Source:   FuturesEvidenceSource,
		SourceID: entry.ID,
		Wallet:   WalletFutures,
		Asset:    Asset,
		At:       entry.At.UTC(),
		Kind:     kind,
		Amount:   entry.Amount,
	}, true, nil
}

func futuresKind(kind string) (Kind, bool) {
	switch kind {
	case "transfer_in":
		return Deposit, true
	case "transfer_out":
		return Withdrawal, true
	case "funding":
		return Funding, true
	case "realized_pnl":
		return RealizedPnL, true
	case "fee":
		return OtherFee, true
	case "insurance_clear":
		return OtherFee, true
	case "rebate":
		return Deposit, true
	default:
		return "", false
	}
}
