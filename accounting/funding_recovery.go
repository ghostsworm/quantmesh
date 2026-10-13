package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxDurableRecoveryRevision is the largest revision representable by the
// signed BIGINT/INTEGER columns used by the supported SQL stores.
const MaxDurableRecoveryRevision uint64 = 1<<63 - 1

type RecoveryState string

const (
	RecoveryPrepared          RecoveryState = "prepared"
	RecoveryLedgerCommitted   RecoveryState = "ledger_committed"
	RecoveryProjectionPending RecoveryState = "projection_pending"
	RecoveryProjected         RecoveryState = "projected"
	RecoveryReleasePending    RecoveryState = "release_pending"
	RecoveryReleased          RecoveryState = "released"
	RecoveryReopened          RecoveryState = "reopened"
	RecoveryStale             RecoveryState = "stale"
	RecoveryBlocked           RecoveryState = "blocked"
)

type ChildState string

const (
	ChildProvisional ChildState = "provisional"
	ChildBound       ChildState = "bound"
	ChildTerminal    ChildState = "terminal"
	ChildUnknown     ChildState = "unknown"
)

type RecoveryEpisode struct {
	ID                 string                        `json:"episode_id"`
	Bot                string                        `json:"bot_id"`
	Strategy           string                        `json:"strategy"`
	AccountScopes      []string                      `json:"account_scopes"`
	OwnerGeneration    string                        `json:"owner_generation"`
	Revision           uint64                        `json:"revision"`
	RecoveryGeneration uint64                        `json:"recovery_generation"`
	NextActionOrdinal  uint64                        `json:"next_action_ordinal"`
	State              RecoveryState                 `json:"state"`
	ActionPlan         []RecoveryAction              `json:"action_plan"`
	OwnerTakeovers     []RecoveryOwnerTakeoverRecord `json:"owner_takeovers,omitempty"`
	CreatedAt          time.Time                     `json:"created_at"`
	UpdatedAt          time.Time                     `json:"updated_at"`
}

type RecoveryOwnerTakeoverAuthorization struct {
	EpisodeID               string `json:"episode_id"`
	OperationID             string `json:"operation_id"`
	Actor                   string `json:"actor"`
	AuditRef                string `json:"audit_ref"`
	ExpectedOwnerGeneration string `json:"expected_owner_generation"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	Confirmed               bool   `json:"confirmed"`
}

func (a RecoveryOwnerTakeoverAuthorization) Validate() error {
	if strings.TrimSpace(a.EpisodeID) == "" || strings.TrimSpace(a.OperationID) == "" || strings.TrimSpace(a.Actor) == "" || strings.TrimSpace(a.AuditRef) == "" || strings.TrimSpace(a.ExpectedOwnerGeneration) == "" || a.ExpectedEpisodeRevision == 0 || !a.Confirmed {
		return errors.New("incomplete recovery owner takeover authorization")
	}
	return nil
}

type RecoveryOwnerTakeoverRecord struct {
	OperationID             string    `json:"operation_id"`
	Actor                   string    `json:"actor"`
	AuditRef                string    `json:"audit_ref"`
	EvidenceHash            string    `json:"evidence_hash"`
	PreviousOwnerGeneration string    `json:"previous_owner_generation"`
	OwnerGeneration         string    `json:"owner_generation"`
	EpisodeRevision         uint64    `json:"episode_revision"`
	CreatedAt               time.Time `json:"created_at"`
}

// RecoveryOriginIntent binds an ordinary execution-journal intent to exactly
// one durable, accounting-only recovery episode. The key deliberately omits
// runtime owner generation so restart/takeover cannot duplicate the business
// episode for the same immutable intent.
type RecoveryOriginIntent struct {
	Bot            string   `json:"bot_id"`
	Strategy       string   `json:"strategy"`
	Exchange       string   `json:"exchange"`
	Market         string   `json:"market"`
	AccountScope   string   `json:"account_scope"`
	OwnerScopes    []string `json:"owner_scopes"`
	Symbol         string   `json:"symbol"`
	ClientOrderID  string   `json:"client_order_id"`
	Side           string   `json:"side"`
	IntentRevision int64    `json:"intent_revision"`
	IntentHash     string   `json:"intent_hash"`
}

func (i RecoveryOriginIntent) Key() (string, error) {
	if strings.TrimSpace(i.Bot) == "" || strings.TrimSpace(i.Strategy) == "" || strings.TrimSpace(i.Exchange) == "" ||
		strings.TrimSpace(i.Market) == "" || strings.TrimSpace(i.AccountScope) == "" || len(i.OwnerScopes) == 0 || strings.TrimSpace(i.Symbol) == "" ||
		strings.TrimSpace(i.ClientOrderID) == "" || (i.Side != "BUY" && i.Side != "SELL") || i.IntentRevision <= 0 || !validRecoveryHash(i.IntentHash) {
		return "", errors.New("incomplete recovery origin intent identity")
	}
	for _, field := range []string{i.Bot, i.Strategy, i.Exchange, i.Market, i.AccountScope, i.Symbol, i.ClientOrderID} {
		if strings.TrimSpace(field) != field || strings.ContainsAny(field, "\t\r\n") {
			return "", errors.New("recovery origin intent identity is not canonical")
		}
	}
	if i.Exchange != strings.ToLower(i.Exchange) || i.Market != strings.ToLower(i.Market) || i.Symbol != strings.ToUpper(i.Symbol) {
		return "", errors.New("recovery origin exchange, market, or symbol casing is not canonical")
	}
	ownerScopes := append([]string(nil), i.OwnerScopes...)
	for _, scope := range ownerScopes {
		if strings.TrimSpace(scope) == "" || strings.TrimSpace(scope) != scope {
			return "", errors.New("invalid recovery origin owner scope")
		}
	}
	sort.Strings(ownerScopes)
	if !sameRecoveryStrings(ownerScopes, i.OwnerScopes) {
		return "", errors.New("recovery origin owner scopes must be canonical and unique")
	}
	for index := 1; index < len(ownerScopes); index++ {
		if ownerScopes[index-1] == ownerScopes[index] {
			return "", errors.New("recovery origin owner scopes must be canonical and unique")
		}
	}
	// The origin key is stable business identity. Runtime owner scopes fence
	// writes but are deliberately excluded: a changed scope set must conflict
	// against this same origin and require an explicit audited takeover, never
	// create a second episode for one client order ID.
	encoded, err := json.Marshal(struct {
		Bot           string `json:"bot_id"`
		Strategy      string `json:"strategy"`
		Exchange      string `json:"exchange"`
		Market        string `json:"market"`
		AccountScope  string `json:"account_scope"`
		Symbol        string `json:"symbol"`
		ClientOrderID string `json:"client_order_id"`
	}{i.Bot, i.Strategy, i.Exchange, i.Market, i.AccountScope, i.Symbol, i.ClientOrderID})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sameRecoveryStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type RecoveryChild struct {
	ID                 string     `json:"child_id"`
	EpisodeID          string     `json:"episode_id"`
	Role               string     `json:"role"`
	Exchange           string     `json:"exchange"`
	Market             string     `json:"market"`
	AccountScope       string     `json:"account_scope"`
	Bot                string     `json:"bot"`
	Symbol             string     `json:"symbol"`
	StrategyName       string     `json:"strategy_name"`
	StrategyType       string     `json:"strategy_type"`
	ClientOrderID      string     `json:"client_order_id"`
	Side               string     `json:"side,omitempty"`
	OrderIntentHash    string     `json:"order_intent_hash,omitempty"`
	SourceIntentHash   string     `json:"source_intent_hash,omitempty"`
	VenueOrderID       string     `json:"venue_order_id,omitempty"`
	Revision           uint64     `json:"revision"`
	FillCursor         Cursor     `json:"fill_cursor"`
	TerminalRevision   string     `json:"terminal_revision,omitempty"`
	CompletenessMarker string     `json:"completeness_marker,omitempty"`
	State              ChildState `json:"state"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type RecoveryActionKind string

const (
	RecoveryActionOrder  RecoveryActionKind = "order"
	RecoveryActionBorrow RecoveryActionKind = "borrow"
	RecoveryActionRepay  RecoveryActionKind = "repay"
)

type RecoveryAction struct {
	ID                   string             `json:"action_id"`
	Ordinal              uint64             `json:"ordinal"`
	Kind                 RecoveryActionKind `json:"action_kind"`
	Role                 string             `json:"role"`
	AccountScope         string             `json:"account_scope"`
	Market               string             `json:"market,omitempty"`
	Symbol               string             `json:"symbol,omitempty"`
	Asset                string             `json:"asset,omitempty"`
	Amount               string             `json:"amount,omitempty"`
	PrecedingActionID    string             `json:"preceding_action_id,omitempty"`
	PrecedingRevision    uint64             `json:"preceding_revision"`
	OwnerGeneration      string             `json:"owner_generation"`
	GateSourceExceptions []string           `json:"gate_source_exceptions,omitempty"`
	OrderIntentHash      string             `json:"order_intent_hash,omitempty"`
	Status               string             `json:"status"`
}

// RecoveryOrderIntent is the immutable submission envelope authorized by an
// order action. Decimal strings are canonical and never reconstructed from a
// venue acknowledgement.
type RecoveryOrderIntent struct {
	SchemaVersion uint32 `json:"schema_version"`
	Exchange      string `json:"exchange"`
	Market        string `json:"market"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	OrderType     string `json:"order_type"`
	TimeInForce   string `json:"time_in_force"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
	PriceDecimals int    `json:"price_decimals"`
	ReduceOnly    bool   `json:"reduce_only"`
	PostOnly      bool   `json:"post_only"`
	PositionSide  string `json:"position_side,omitempty"`
	ClientOrderID string `json:"client_order_id"`
}

func (i RecoveryOrderIntent) CanonicalHash() (string, error) {
	if i.SchemaVersion != 1 || strings.TrimSpace(i.Exchange) == "" || strings.TrimSpace(i.Market) == "" || strings.TrimSpace(i.Symbol) == "" || i.ClientOrderID == "" || i.PriceDecimals < 0 ||
		(i.Side != "BUY" && i.Side != "SELL") || strings.TrimSpace(i.OrderType) == "" {
		return "", errors.New("incomplete recovery order intent")
	}
	if err := ValidateCanonicalDecimal(i.Price); err != nil || strings.HasPrefix(i.Price, "-") || i.Price == "0" {
		return "", errors.New("recovery order intent has invalid price")
	}
	if err := ValidateCanonicalDecimal(i.Quantity); err != nil || strings.HasPrefix(i.Quantity, "-") || i.Quantity == "0" {
		return "", errors.New("recovery order intent has invalid quantity")
	}
	encoded, err := json.Marshal(i)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

type RecoveryEventKind string

const (
	RecoveryFillEvent     RecoveryEventKind = "fill"
	RecoveryFeeEvent      RecoveryEventKind = "fee"
	RecoveryBorrowEvent   RecoveryEventKind = "borrow"
	RecoveryRepayEvent    RecoveryEventKind = "repay"
	RecoveryInterestEvent RecoveryEventKind = "interest"
	RecoveryFundingEvent  RecoveryEventKind = "funding"
	RecoveryTerminalEvent RecoveryEventKind = "terminal_zero_fill"
	// RecoveryOpeningBalanceEvent is an audited account snapshot fact used as
	// a ledger baseline. It is not realized PnL and must never authorize release.
	RecoveryOpeningBalanceEvent RecoveryEventKind = "opening_balance"
)

const RecoveryManualFeeAdjustmentSource = "manual_fee_adjustment_v1"
const RecoveryManualOpeningBalanceSource = "manual_opening_balance_v1"

type RecoveryEvent struct {
	ID                 string                             `json:"event_id"`
	Source             string                             `json:"source"`
	Exchange           string                             `json:"exchange"`
	Market             string                             `json:"market"`
	AccountScope       string                             `json:"account_scope"`
	Symbol             string                             `json:"symbol,omitempty"`
	Kind               RecoveryEventKind                  `json:"event_type"`
	ExternalID         string                             `json:"external_id"`
	RelatedExternalID  string                             `json:"related_external_id,omitempty"`
	EpisodeID          string                             `json:"episode_id"`
	ChildID            string                             `json:"child_id,omitempty"`
	CursorSequence     uint64                             `json:"cursor_sequence,omitempty"`
	Asset              string                             `json:"asset"`
	Amount             string                             `json:"amount"`
	SourceRevision     string                             `json:"source_revision"`
	SourceHash         string                             `json:"source_hash"`
	ObservedAt         time.Time                          `json:"observed_at"`
	CompletenessMarker string                             `json:"completeness_marker,omitempty"`
	FillEvidence       *RecoveryFillEvidence              `json:"fill_evidence,omitempty"`
	DebtEvidence       *RecoveryDebtEvidence              `json:"debt_evidence,omitempty"`
	OpeningBalance     *RecoveryOpeningBalanceEvidence    `json:"opening_balance_evidence,omitempty"`
	FundingAdjustment  *RecoveryFundingAdjustmentEvidence `json:"funding_adjustment_evidence,omitempty"`
}

// RecoveryDebtEvidence preserves the source-provided raw components of a
// borrow/repay amount. Borrow Amount is positive principal plus interest;
// repay Amount is the negative principal-plus-interest cash outflow.
type RecoveryDebtEvidence struct {
	PrincipalRaw string `json:"principal_raw"`
	InterestRaw  string `json:"interest_raw"`
}

// RecoveryOpeningBalanceEvidence ties a manually entered ledger baseline to
// the exact account snapshot and an auditable operator reference. The amount
// remains a raw decimal and is not treated as income, expense, or strategy PnL.
type RecoveryOpeningBalanceEvidence struct {
	Actor        string    `json:"actor"`
	AuditRef     string    `json:"audit_ref"`
	SnapshotHash string    `json:"snapshot_hash"`
	SnapshotAt   time.Time `json:"snapshot_at"`
}

type RecoveryOpeningBalanceEntry struct {
	Asset     string `json:"asset"`
	AmountRaw string `json:"amount_raw"`
}

// RecoveryOpeningBalanceSnapshot is the exact operator-attested account
// snapshot used to establish an opening watermark. Entries are asset-sorted
// before hashing; amounts are never parsed through float64.
type RecoveryOpeningBalanceSnapshot struct {
	Exchange     string                        `json:"exchange"`
	Market       string                        `json:"market"`
	AccountScope string                        `json:"account_scope"`
	SnapshotAt   time.Time                     `json:"snapshot_at"`
	Assets       []RecoveryOpeningBalanceEntry `json:"assets"`
}

func (s RecoveryOpeningBalanceSnapshot) CanonicalHash() (string, error) {
	if s.Exchange == "" || s.Exchange != strings.ToLower(strings.TrimSpace(s.Exchange)) || s.Market == "" || s.Market != strings.ToLower(strings.TrimSpace(s.Market)) ||
		!validRecoveryHash(s.AccountScope) || s.SnapshotAt.IsZero() || len(s.Assets) == 0 {
		return "", errors.New("opening balance snapshot identity, timestamp, and assets are required")
	}
	assets := append([]RecoveryOpeningBalanceEntry(nil), s.Assets...)
	for _, entry := range assets {
		if entry.Asset == "" || entry.Asset != strings.ToUpper(strings.TrimSpace(entry.Asset)) || strings.ContainsAny(entry.Asset, " \t\r\n") {
			return "", errors.New("opening balance asset must be a canonical uppercase symbol")
		}
		if err := validateRawRecoveryDecimal(entry.AmountRaw, true); err != nil {
			return "", fmt.Errorf("opening balance for %s: %w", entry.Asset, err)
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Asset < assets[j].Asset })
	for index := 1; index < len(assets); index++ {
		if assets[index-1].Asset == assets[index].Asset {
			return "", fmt.Errorf("duplicate opening balance asset %s", assets[index].Asset)
		}
	}
	s.Assets = assets
	s.SnapshotAt = s.SnapshotAt.UTC()
	encoded, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// RecoverySourceWindow is a complete, half-open [From, Through) source range.
// ExpectedRevision is zero only for the initial checkpoint. Rows must equal
// TotalRows and CompletionMarker must explicitly attest that pagination ended.
type RecoverySourceWindow struct {
	Source                 string            `json:"source"`
	Exchange               string            `json:"exchange"`
	Market                 string            `json:"market"`
	AccountScope           string            `json:"account_scope"`
	Symbol                 string            `json:"symbol,omitempty"`
	Kind                   RecoveryEventKind `json:"event_type"`
	Asset                  string            `json:"asset"`
	From                   time.Time         `json:"from"`
	Through                time.Time         `json:"through"`
	ExpectedRevision       uint64            `json:"expected_revision"`
	Rows                   uint64            `json:"complete_rows"`
	TotalRows              uint64            `json:"total_rows"`
	CompletionMarker       string            `json:"completion_marker"`
	CollectorEvidenceHash  string            `json:"collector_evidence_hash,omitempty"`
	InitialFrom            time.Time         `json:"initial_from,omitempty"`
	HistoricalEvidenceHash string            `json:"historical_evidence_hash,omitempty"`
	EvidenceHash           string            `json:"source_evidence_hash"`
}

// RecoverySourceIdentity is the stable lookup key for a durable source cursor.
// It deliberately excludes window bounds and evidence so callers can read the
// prior checkpoint before constructing their next complete window.
type RecoverySourceIdentity struct {
	Source       string            `json:"source"`
	Exchange     string            `json:"exchange"`
	Market       string            `json:"market"`
	AccountScope string            `json:"account_scope"`
	Symbol       string            `json:"symbol,omitempty"`
	Kind         RecoveryEventKind `json:"event_type"`
	Asset        string            `json:"asset"`
}

const (
	RecoverySourceBinanceFuturesIncomeV1 = "binance_futures_income_v1"
	RecoverySourceBinanceMarginDebtV1    = "binance_spot_margin_debt_v1"
)

// RecoverySourceWindowEvidenceHash binds a source's complete half-open window
// and exact raw events. The source collector must compute this only after it
// has independently established pagination completion and range safety.
func RecoverySourceWindowEvidenceHash(window RecoverySourceWindow, events []RecoveryEvent) (string, error) {
	window.EvidenceHash = strings.Repeat("0", 64)
	if err := window.Validate(); err != nil {
		return "", err
	}
	if uint64(len(events)) != window.Rows {
		return "", errors.New("source window evidence row count mismatch")
	}
	canonicalEvents := append([]RecoveryEvent(nil), events...)
	sort.Slice(canonicalEvents, func(i, j int) bool {
		if canonicalEvents[i].ExternalID == canonicalEvents[j].ExternalID {
			return canonicalEvents[i].ID < canonicalEvents[j].ID
		}
		return canonicalEvents[i].ExternalID < canonicalEvents[j].ExternalID
	})
	for index, event := range canonicalEvents {
		if err := event.Validate(); err != nil {
			return "", fmt.Errorf("source window event %d: %w", index, err)
		}
		if event.Source != window.Source || event.Exchange != window.Exchange || event.Market != window.Market ||
			event.AccountScope != window.AccountScope || event.Symbol != window.Symbol || event.Kind != window.Kind ||
			event.Asset != window.Asset || event.ObservedAt.Before(window.From) || !event.ObservedAt.Before(window.Through) {
			return "", errors.New("source window event differs from the attested dimensions or bounds")
		}
		if index > 0 && canonicalEvents[index-1].ExternalID == event.ExternalID {
			return "", errors.New("source window contains a duplicate external event identity")
		}
	}
	canonicalWindow := struct {
		Source                 string            `json:"source"`
		Exchange               string            `json:"exchange"`
		Market                 string            `json:"market"`
		AccountScope           string            `json:"account_scope"`
		Symbol                 string            `json:"symbol,omitempty"`
		Kind                   RecoveryEventKind `json:"event_type"`
		Asset                  string            `json:"asset"`
		From                   time.Time         `json:"from"`
		Through                time.Time         `json:"through"`
		Rows                   uint64            `json:"complete_rows"`
		TotalRows              uint64            `json:"total_rows"`
		CompletionMarker       string            `json:"completion_marker"`
		CollectorEvidenceHash  string            `json:"collector_evidence_hash,omitempty"`
		InitialFrom            time.Time         `json:"initial_from,omitempty"`
		HistoricalEvidenceHash string            `json:"historical_evidence_hash,omitempty"`
		Events                 []RecoveryEvent   `json:"events"`
	}{window.Source, window.Exchange, window.Market, window.AccountScope, window.Symbol, window.Kind, window.Asset, window.From.UTC(), window.Through.UTC(), window.Rows, window.TotalRows, window.CompletionMarker, window.CollectorEvidenceHash, window.InitialFrom.UTC(), window.HistoricalEvidenceHash, canonicalEvents}
	encoded, err := json.Marshal(canonicalWindow)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (w RecoverySourceWindow) Identity() RecoverySourceIdentity {
	return RecoverySourceIdentity{Source: w.Source, Exchange: w.Exchange, Market: w.Market, AccountScope: w.AccountScope, Symbol: w.Symbol, Kind: w.Kind, Asset: w.Asset}
}

func (i RecoverySourceIdentity) Validate() error {
	if strings.TrimSpace(i.Source) == "" || strings.TrimSpace(i.Exchange) == "" || strings.TrimSpace(i.Market) == "" ||
		strings.TrimSpace(i.AccountScope) == "" || strings.TrimSpace(i.Asset) == "" ||
		(i.Kind != RecoveryBorrowEvent && i.Kind != RecoveryRepayEvent && i.Kind != RecoveryInterestEvent && i.Kind != RecoveryFundingEvent) {
		return errors.New("incomplete recovery source identity")
	}
	if i.Kind == RecoveryFundingEvent {
		if i.Symbol == "" || i.Symbol != strings.ToUpper(strings.TrimSpace(i.Symbol)) || strings.ContainsAny(i.Symbol, " \t\r\n") {
			return errors.New("funding recovery source identity requires a canonical symbol")
		}
	} else if i.Symbol != "" {
		return errors.New("non-funding recovery source identity cannot have a symbol")
	}
	return nil
}

const RecoverySourceWindowCompleteV1 = "complete_v1"

func (w RecoverySourceWindow) Validate() error {
	if strings.TrimSpace(w.Source) == "" || strings.TrimSpace(w.Exchange) == "" || strings.TrimSpace(w.Market) == "" ||
		strings.TrimSpace(w.AccountScope) == "" || strings.TrimSpace(w.Asset) == "" ||
		(w.Kind != RecoveryBorrowEvent && w.Kind != RecoveryRepayEvent && w.Kind != RecoveryInterestEvent && w.Kind != RecoveryFundingEvent) ||
		w.From.IsZero() || w.Through.IsZero() || !w.From.Before(w.Through) || w.From.Nanosecond()%int(time.Millisecond) != 0 || w.Through.Nanosecond()%int(time.Millisecond) != 0 ||
		w.CompletionMarker != RecoverySourceWindowCompleteV1 ||
		w.Rows != w.TotalRows || !validRecoveryHash(w.EvidenceHash) {
		return errors.New("incomplete recovery source window")
	}
	if err := w.Identity().Validate(); err != nil {
		return fmt.Errorf("invalid recovery source identity: %w", err)
	}
	if w.Source == RecoverySourceBinanceFuturesIncomeV1 {
		if w.InitialFrom.IsZero() || w.InitialFrom.After(w.From) || w.InitialFrom.Nanosecond()%int(time.Millisecond) != 0 || !validRecoveryHash(w.HistoricalEvidenceHash) {
			return errors.New("Binance funding source window requires a stable initial watermark and historical scan digest")
		}
	}
	return nil
}

// RecoverySourceCursor is the durable last accepted complete window for one
// exact source/exchange/market/account/kind/asset identity.
type RecoverySourceCursor struct {
	RecoverySourceWindow
	EpisodeID string    `json:"episode_id"`
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RecoveryFillEvidence preserves venue-provided decimal strings without
// reformatting float values. Empty optional quote/PNL fields mean the source
// did not provide that fact; CommissionKnown distinguishes an explicit zero
// fee from an absent fee report.
type RecoveryFillEvidence struct {
	ClientOrderID    string `json:"client_order_id"`
	VenueOrderID     string `json:"venue_order_id"`
	Symbol           string `json:"symbol"`
	OrderIntentHash  string `json:"order_intent_hash"`
	Side             string `json:"side"`
	BaseAsset        string `json:"base_asset"`
	PriceRaw         string `json:"price_raw"`
	QuantityRaw      string `json:"quantity_raw"`
	QuoteAsset       string `json:"quote_asset,omitempty"`
	QuoteQuantityRaw string `json:"quote_quantity_raw,omitempty"`
	CommissionRaw    string `json:"commission_raw,omitempty"`
	CommissionAsset  string `json:"commission_asset,omitempty"`
	CommissionKnown  bool   `json:"commission_known"`
	RealizedPnLRaw   string `json:"realized_pnl_raw,omitempty"`
	RealizedPnLAsset string `json:"realized_pnl_asset,omitempty"`
	RealizedPnLKnown bool   `json:"realized_pnl_known"`
}

var rawRecoveryDecimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func validateRawRecoveryDecimal(value string, allowNegative bool) error {
	if !rawRecoveryDecimalPattern.MatchString(value) || (!allowNegative && strings.HasPrefix(value, "-")) {
		return fmt.Errorf("%q is not a valid raw decimal", value)
	}
	return nil
}

func rawRecoveryDecimalIsZero(value string) bool {
	return !strings.ContainsAny(value, "123456789")
}

func rawRecoveryDecimalsEqual(left, right string) bool {
	leftRat, ok := new(big.Rat).SetString(left)
	if !ok {
		return false
	}
	rightRat, ok := new(big.Rat).SetString(right)
	return ok && leftRat.Cmp(rightRat) == 0
}

type RecoveryOperation struct {
	EpisodeID               string                `json:"episode_id"`
	OperationID             string                `json:"operation_id"`
	HashVersion             uint32                `json:"request_hash_version"`
	RequestHash             string                `json:"request_hash"`
	ExpectedEpisodeRevision uint64                `json:"expected_episode_revision"`
	ExpectedRuntimeRevision string                `json:"expected_runtime_revision"`
	ExpectedChildRevisions  map[string]uint64     `json:"expected_child_revisions"`
	ExpectedEventRevisions  map[string]uint64     `json:"expected_event_revisions"`
	Children                []RecoveryChild       `json:"children"`
	Events                  []RecoveryEvent       `json:"events"`
	SourceWindow            *RecoverySourceWindow `json:"source_window,omitempty"`
	SourceAnomalyID         string                `json:"source_anomaly_id,omitempty"`
	ExpectedAnomalyRevision uint64                `json:"expected_anomaly_revision,omitempty"`
	VerifiedAnomalyIDs      []string              `json:"verified_anomaly_ids,omitempty"`
	Actor                   string                `json:"actor"`
	AuditRef                string                `json:"audit_ref"`
	EvidenceHash            string                `json:"evidence_hash"`
	EvidenceReadAt          time.Time             `json:"evidence_read_at"`
}

type RecoveryReceipt struct {
	EpisodeID               string        `json:"episode_id"`
	OperationID             string        `json:"operation_id"`
	RequestHash             string        `json:"request_hash"`
	EpisodeRevision         uint64        `json:"episode_revision"`
	RecoveryGeneration      uint64        `json:"recovery_generation,omitempty"`
	State                   RecoveryState `json:"state"`
	ProjectionHash          string        `json:"projection_hash"`
	Actor                   string        `json:"actor,omitempty"`
	AuditRef                string        `json:"audit_ref,omitempty"`
	EvidenceHash            string        `json:"evidence_hash,omitempty"`
	PreviousOwnerGeneration string        `json:"previous_owner_generation,omitempty"`
	OwnerGeneration         string        `json:"owner_generation,omitempty"`
	CreatedAt               time.Time     `json:"created_at"`
}

type RecoveryReleaseAuthorization struct {
	EpisodeID               string `json:"episode_id"`
	OperationID             string `json:"operation_id"`
	Actor                   string `json:"actor"`
	EvidenceHash            string `json:"evidence_hash"`
	ProjectionHash          string `json:"projection_hash"`
	RuntimeRevision         string `json:"runtime_revision"`
	OwnerGeneration         string `json:"owner_generation"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	RecoveryGeneration      uint64 `json:"recovery_generation"`
	ExpectedHoldRevision    uint64 `json:"expected_hold_revision"`
	Confirmed               bool   `json:"confirmed"`
}

func (r RecoveryReleaseAuthorization) Validate() error {
	if r.EpisodeID == "" || r.OperationID == "" || r.Actor == "" || r.EvidenceHash == "" || r.ProjectionHash == "" || r.RuntimeRevision == "" || r.OwnerGeneration == "" || r.ExpectedEpisodeRevision == 0 || r.RecoveryGeneration == 0 || r.ExpectedHoldRevision == 0 || !r.Confirmed {
		return errors.New("incomplete recovery release authorization")
	}
	return nil
}

type RecoveryOutbox struct {
	EpisodeID               string          `json:"episode_id"`
	OperationID             string          `json:"operation_id"`
	ProjectionPayload       json.RawMessage `json:"projection_payload"`
	ExpectedRuntimeRevision string          `json:"expected_runtime_revision"`
	TargetEpisodeRevision   uint64          `json:"target_episode_revision"`
	ProjectionHash          string          `json:"projection_hash"`
	AppliedOperationID      string          `json:"applied_operation_id,omitempty"`
	AppliedRevision         uint64          `json:"applied_revision,omitempty"`
	AppliedHash             string          `json:"applied_hash,omitempty"`
	State                   RecoveryState   `json:"state"`
	UpdatedAt               time.Time       `json:"updated_at"`
}

type RecoveryHold struct {
	ID                  string    `json:"hold_id"`
	EpisodeID           string    `json:"episode_id"`
	Bot                 string    `json:"bot_id"`
	Strategy            string    `json:"strategy"`
	AccountScopes       []string  `json:"account_scopes"`
	Generation          uint64    `json:"recovery_generation"`
	Revision            uint64    `json:"hold_revision"`
	Source              string    `json:"source"`
	State               string    `json:"state"`
	CreatedByOperation  string    `json:"created_by_operation"`
	ReleasedByOperation string    `json:"released_by_operation,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type PermitState string

const (
	PermitAuthorized PermitState = "authorized"
	PermitConsumed   PermitState = "consumed"
	PermitRevoked    PermitState = "revoked"
)

type OneShotPermit struct {
	ID                   string               `json:"permit_id"`
	EpisodeID            string               `json:"episode_id"`
	Generation           uint64               `json:"recovery_generation"`
	ActionID             string               `json:"action_id"`
	ActionOrdinal        uint64               `json:"action_ordinal"`
	Kind                 RecoveryActionKind   `json:"action_kind"`
	AccountScope         string               `json:"account_scope"`
	OwnerGeneration      string               `json:"owner_generation"`
	LeaseID              uint64               `json:"lease_id"`
	GateSourceExceptions []string             `json:"gate_source_exceptions,omitempty"`
	OrderIntentHash      string               `json:"order_intent_hash,omitempty"`
	OrderIntent          *RecoveryOrderIntent `json:"order_intent,omitempty"`
	ExpectedRevision     uint64               `json:"expected_episode_revision"`
	State                PermitState          `json:"state"`
	CreatedAt            time.Time            `json:"created_at"`
	ConsumedAt           time.Time            `json:"consumed_at,omitempty"`
}

// Clone returns an immutable-by-ownership snapshot of a permit and all of its
// reference fields. Callers crossing storage, strategy, or executor boundaries
// must not retain aliases to its slices or order intent.
func (p OneShotPermit) Clone() OneShotPermit {
	p.GateSourceExceptions = append([]string(nil), p.GateSourceExceptions...)
	if p.OrderIntent != nil {
		intent := *p.OrderIntent
		p.OrderIntent = &intent
	}
	return p
}

type RecoverySubmissionState string

const (
	RecoverySubmissionUnknown      RecoverySubmissionState = "outcome_unknown"
	RecoverySubmissionAcknowledged RecoverySubmissionState = "acknowledged"
	RecoverySubmissionFound        RecoverySubmissionState = "found"
	RecoverySubmissionNotSent      RecoverySubmissionState = "not_submitted_confirmed"
)

// RecoverySubmissionAttempt is the durable identity of the single physical
// venue attempt claimed by a consumed permit. Unknown is the safe restart state.
type RecoverySubmissionAttempt struct {
	EpisodeID       string                  `json:"episode_id"`
	Generation      uint64                  `json:"recovery_generation"`
	ActionID        string                  `json:"action_id"`
	PermitID        string                  `json:"permit_id"`
	AccountScope    string                  `json:"account_scope"`
	OwnerGeneration string                  `json:"owner_generation"`
	LeaseID         uint64                  `json:"lease_id"`
	IntentHash      string                  `json:"order_intent_hash"`
	Intent          RecoveryOrderIntent     `json:"order_intent"`
	ChildID         string                  `json:"child_id"`
	State           RecoverySubmissionState `json:"state"`
	Revision        uint64                  `json:"revision"`
	VenueOrderID    string                  `json:"venue_order_id,omitempty"`
	EvidenceHash    string                  `json:"evidence_hash,omitempty"`
	Actor           string                  `json:"actor,omitempty"`
	AuditRef        string                  `json:"audit_ref,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type RecoverySubmissionAdjudication struct {
	EpisodeID               string                  `json:"episode_id"`
	OperationID             string                  `json:"operation_id"`
	PermitID                string                  `json:"permit_id"`
	ExpectedAttemptRevision uint64                  `json:"expected_attempt_revision"`
	ExpectedEpisodeRevision uint64                  `json:"expected_episode_revision"`
	Decision                RecoverySubmissionState `json:"decision"`
	VenueOrderID            string                  `json:"venue_order_id,omitempty"`
	Actor                   string                  `json:"actor"`
	AuditRef                string                  `json:"audit_ref"`
	EvidenceHash            string                  `json:"evidence_hash"`
	Confirmed               bool                    `json:"confirmed"`
}

func (a RecoverySubmissionAdjudication) Validate() error {
	if strings.TrimSpace(a.EpisodeID) == "" || strings.TrimSpace(a.OperationID) == "" || strings.TrimSpace(a.PermitID) == "" ||
		a.ExpectedAttemptRevision == 0 || a.ExpectedEpisodeRevision == 0 || strings.TrimSpace(a.Actor) == "" ||
		strings.TrimSpace(a.AuditRef) == "" || !validRecoveryHash(a.EvidenceHash) || !a.Confirmed {
		return errors.New("incomplete recovery submission adjudication")
	}
	if a.Decision != RecoverySubmissionFound || strings.TrimSpace(a.VenueOrderID) == "" {
		return errors.New("only positively verified venue orders may be adjudicated; absence remains unknown")
	}
	return nil
}

func (a RecoverySubmissionAttempt) Validate() error {
	if a.EpisodeID == "" || a.Generation == 0 || a.ActionID == "" || a.PermitID == "" || a.AccountScope == "" || a.OwnerGeneration == "" || a.LeaseID == 0 || a.Revision == 0 || a.ChildID == "" {
		return errors.New("incomplete recovery submission attempt identity")
	}
	hash, err := a.Intent.CanonicalHash()
	if err != nil || hash != a.IntentHash {
		return errors.New("recovery submission attempt intent hash mismatch")
	}
	switch a.State {
	case RecoverySubmissionUnknown, RecoverySubmissionAcknowledged, RecoverySubmissionFound, RecoverySubmissionNotSent:
	default:
		return errors.New("invalid recovery submission attempt state")
	}
	if a.State == RecoverySubmissionAcknowledged && a.VenueOrderID == "" {
		return errors.New("acknowledged recovery submission requires venue order ID")
	}
	if a.State == RecoverySubmissionFound && (a.VenueOrderID == "" || !validRecoveryHash(a.EvidenceHash) || a.Actor == "" || a.AuditRef == "") {
		return errors.New("found recovery submission requires venue identity and audit evidence")
	}
	return nil
}

var ErrRecoveryConflict = errors.New("recovery episode revision conflict")
var ErrRecoveryOperationConflict = errors.New("recovery operation id payload conflict")

func (e RecoveryEpisode) Validate() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Bot) == "" || strings.TrimSpace(e.Strategy) == "" || strings.TrimSpace(e.OwnerGeneration) == "" || e.Revision == 0 || e.RecoveryGeneration == 0 {
		return errors.New("incomplete recovery episode identity")
	}
	if len(e.AccountScopes) == 0 {
		return errors.New("recovery episode requires account scopes")
	}
	seen := map[string]struct{}{}
	for _, scope := range e.AccountScopes {
		if strings.TrimSpace(scope) == "" {
			return errors.New("empty account scope")
		}
		if _, ok := seen[scope]; ok {
			return errors.New("duplicate account scope")
		}
		seen[scope] = struct{}{}
	}
	if e.State != RecoveryPrepared && e.State != RecoveryBlocked && e.State != RecoveryReopened {
		return fmt.Errorf("invalid initial recovery state %q", e.State)
	}
	for i, action := range e.ActionPlan {
		if action.Ordinal != uint64(i+1) || action.ID == "" || action.AccountScope == "" || !recoveryOwnerGenerationKnown(e, action.OwnerGeneration) {
			return fmt.Errorf("invalid action plan item %d", i)
		}
		if _, ok := seen[action.AccountScope]; !ok {
			return fmt.Errorf("action plan item %d uses an unknown account scope", i)
		}
		if err := ValidateGateSourceExceptions(action.GateSourceExceptions); err != nil {
			return fmt.Errorf("action plan item %d gate source exceptions: %w", i, err)
		}
		switch action.Kind {
		case RecoveryActionOrder:
			if strings.TrimSpace(action.Market) == "" || strings.TrimSpace(action.Symbol) == "" || !validRecoveryHash(action.OrderIntentHash) {
				return fmt.Errorf("action plan item %d requires market, symbol and immutable order intent hash", i)
			}
		case RecoveryActionBorrow, RecoveryActionRepay:
			if action.OrderIntentHash != "" {
				return fmt.Errorf("action plan item %d cannot bind an order intent hash", i)
			}
			if action.Asset == "" || ValidateCanonicalDecimal(action.Amount) != nil {
				return fmt.Errorf("action plan item %d has invalid financial amount", i)
			}
		default:
			return fmt.Errorf("action plan item %d has invalid action kind", i)
		}
	}
	for i, takeover := range e.OwnerTakeovers {
		if takeover.OperationID == "" || takeover.Actor == "" || takeover.AuditRef == "" || !validRecoveryHash(takeover.EvidenceHash) || takeover.PreviousOwnerGeneration == "" || takeover.OwnerGeneration == "" || takeover.PreviousOwnerGeneration == takeover.OwnerGeneration || takeover.EpisodeRevision == 0 || takeover.CreatedAt.IsZero() {
			return fmt.Errorf("invalid owner takeover record %d", i)
		}
		if i > 0 && e.OwnerTakeovers[i-1].OwnerGeneration != takeover.PreviousOwnerGeneration {
			return fmt.Errorf("disconnected owner takeover history at record %d", i)
		}
	}
	if len(e.OwnerTakeovers) > 0 && e.OwnerTakeovers[len(e.OwnerTakeovers)-1].OwnerGeneration != e.OwnerGeneration {
		return errors.New("owner takeover history does not match current owner generation")
	}
	return nil
}

func recoveryOwnerGenerationKnown(episode RecoveryEpisode, ownerGeneration string) bool {
	if ownerGeneration == "" {
		return false
	}
	if ownerGeneration == episode.OwnerGeneration {
		return true
	}
	for _, takeover := range episode.OwnerTakeovers {
		if ownerGeneration == takeover.PreviousOwnerGeneration || ownerGeneration == takeover.OwnerGeneration {
			return true
		}
	}
	return false
}

// HasCurrentOwnerTakeover reports whether the current owner explicitly took
// over this episode. Such episodes remain accounting-only; they cannot issue
// another execution permit.
func (e RecoveryEpisode) HasCurrentOwnerTakeover() bool {
	return len(e.OwnerTakeovers) > 0 && e.OwnerTakeovers[len(e.OwnerTakeovers)-1].OwnerGeneration == e.OwnerGeneration
}

func (c RecoveryChild) Validate() error {
	for name, value := range map[string]string{"id": c.ID, "episode_id": c.EpisodeID, "role": c.Role, "exchange": c.Exchange, "market": c.Market, "account_scope": c.AccountScope, "bot": c.Bot, "symbol": c.Symbol, "strategy_name": c.StrategyName, "strategy_type": c.StrategyType, "client_order_id": c.ClientOrderID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("child %s is required", name)
		}
	}
	if err := c.FillCursor.Validate(); err != nil {
		return err
	}
	if c.State == ChildBound && strings.TrimSpace(c.VenueOrderID) == "" {
		return errors.New("bound child requires venue order id")
	}
	if c.SourceIntentHash != "" && !validRecoveryHash(c.SourceIntentHash) {
		return errors.New("source intent hash is invalid")
	}
	if c.State == ChildBound || c.OrderIntentHash != "" {
		if c.Side != "BUY" && c.Side != "SELL" {
			return errors.New("order child side must be BUY or SELL")
		}
		if !validRecoveryHash(c.OrderIntentHash) {
			return errors.New("order child requires a valid order intent hash")
		}
	} else if c.Side != "" && (c.State != ChildUnknown || !validRecoveryHash(c.SourceIntentHash) || (c.Side != "BUY" && c.Side != "SELL")) {
		return errors.New("unbound source child side requires an unknown state and valid source intent hash")
	}
	return nil
}

func (e RecoveryEvent) Validate() error {
	for name, value := range map[string]string{"id": e.ID, "source": e.Source, "exchange": e.Exchange, "market": e.Market, "account_scope": e.AccountScope, "external_id": e.ExternalID, "episode_id": e.EpisodeID, "asset": e.Asset, "source_revision": e.SourceRevision, "source_hash": e.SourceHash} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("event %s is required", name)
		}
	}
	if e.Kind == RecoveryFundingEvent {
		if e.Symbol == "" || e.Symbol != strings.ToUpper(strings.TrimSpace(e.Symbol)) || strings.ContainsAny(e.Symbol, " \t\r\n") {
			return errors.New("funding recovery event requires a canonical symbol")
		}
	} else if e.Symbol != "" {
		return errors.New("non-funding recovery event cannot have a symbol")
	}
	if e.Kind == RecoveryFillEvent || e.Kind == RecoveryFeeEvent {
		if err := validateRawRecoveryDecimal(e.Amount, false); err != nil {
			if e.Kind != RecoveryFeeEvent || validateRawRecoveryDecimal(e.Amount, true) != nil {
				return fmt.Errorf("event amount: %w", err)
			}
		}
	} else if e.Kind == RecoveryBorrowEvent || e.Kind == RecoveryRepayEvent || e.Kind == RecoveryInterestEvent || e.Kind == RecoveryFundingEvent {
		if err := validateRawRecoveryDecimal(e.Amount, true); err != nil {
			return fmt.Errorf("event raw amount: %w", err)
		}
		if e.Kind != RecoveryFundingEvent && rawRecoveryDecimalIsZero(e.Amount) {
			return errors.New("debt event amount must be non-zero")
		}
		if !validRecoveryHash(e.SourceHash) {
			return errors.New("debt and funding event requires a source evidence digest")
		}
	} else if e.Kind == RecoveryOpeningBalanceEvent {
		if err := validateRawRecoveryDecimal(e.Amount, true); err != nil {
			return fmt.Errorf("opening balance raw amount: %w", err)
		}
		if !validRecoveryHash(e.SourceHash) {
			return errors.New("opening balance requires an evidence digest")
		}
	} else if err := ValidateCanonicalDecimal(e.Amount); err != nil {
		return fmt.Errorf("event amount: %w", err)
	}
	if e.ObservedAt.IsZero() {
		return errors.New("event observed_at is required")
	}
	switch e.Kind {
	case RecoveryFillEvent, RecoveryFeeEvent, RecoveryBorrowEvent, RecoveryRepayEvent, RecoveryInterestEvent, RecoveryFundingEvent, RecoveryTerminalEvent, RecoveryOpeningBalanceEvent:
	default:
		return errors.New("unsupported recovery event type")
	}
	if e.Kind == RecoveryFillEvent && e.ChildID == "" {
		return errors.New("fill event requires child")
	}
	if e.Kind == RecoveryFillEvent && e.CursorSequence == 0 {
		return errors.New("fill event requires cursor sequence")
	}
	if e.Kind == RecoveryFillEvent && e.FillEvidence == nil {
		return errors.New("fill event requires raw fill evidence")
	}
	if e.Kind == RecoveryFeeEvent && e.Source == RecoveryManualFeeAdjustmentSource {
		if strings.TrimSpace(e.ChildID) == "" || strings.TrimSpace(e.RelatedExternalID) == "" {
			return errors.New("fee adjustment requires child and related fill identity")
		}
		if !validRecoveryHash(e.SourceHash) {
			return errors.New("fee adjustment requires a sha256 evidence digest")
		}
	}
	if e.Kind == RecoveryFundingEvent && e.Source == RecoveryManualFundingAdjustmentSource {
		if e.FundingAdjustment == nil || e.RelatedExternalID != e.FundingAdjustment.OriginalExternalID || e.SourceHash != e.FundingAdjustment.EvidenceHash || e.Amount != e.FundingAdjustment.DeltaAmountRaw {
			return errors.New("manual funding adjustment event does not match its immutable evidence")
		}
		if err := e.FundingAdjustment.Validate(); err != nil {
			return err
		}
	} else if e.FundingAdjustment != nil {
		return errors.New("funding adjustment evidence requires the manual funding adjustment source")
	}
	if e.Kind == RecoveryBorrowEvent || e.Kind == RecoveryRepayEvent {
		if e.DebtEvidence == nil {
			return errors.New("borrow and repay events require raw debt components")
		}
		if err := validateRawRecoveryDecimal(e.DebtEvidence.PrincipalRaw, false); err != nil {
			return fmt.Errorf("debt principal: %w", err)
		}
		if err := validateRawRecoveryDecimal(e.DebtEvidence.InterestRaw, false); err != nil {
			return fmt.Errorf("debt interest: %w", err)
		}
		if rawRecoveryDecimalIsZero(e.DebtEvidence.PrincipalRaw) && rawRecoveryDecimalIsZero(e.DebtEvidence.InterestRaw) {
			return errors.New("debt principal and interest cannot both be zero")
		}
		total := new(big.Rat)
		principal, _ := new(big.Rat).SetString(e.DebtEvidence.PrincipalRaw)
		interest, _ := new(big.Rat).SetString(e.DebtEvidence.InterestRaw)
		total.Add(principal, interest)
		amount, _ := new(big.Rat).SetString(e.Amount)
		if e.Kind == RecoveryBorrowEvent && (principal.Sign() <= 0 || amount.Sign() <= 0 || total.Cmp(amount) != 0) {
			return errors.New("borrow amount must equal positive principal plus interest")
		}
		if e.Kind == RecoveryRepayEvent && (amount.Sign() >= 0 || total.Sign() <= 0 || new(big.Rat).Neg(total).Cmp(amount) != 0) {
			return errors.New("repay amount must equal negative principal plus interest outflow")
		}
	} else if e.DebtEvidence != nil {
		return errors.New("debt evidence is only valid on borrow and repay events")
	}
	if e.Kind == RecoveryOpeningBalanceEvent {
		if e.Source != RecoveryManualOpeningBalanceSource || e.OpeningBalance == nil || strings.TrimSpace(e.OpeningBalance.Actor) == "" ||
			strings.TrimSpace(e.OpeningBalance.AuditRef) == "" || !validRecoveryHash(e.OpeningBalance.SnapshotHash) || e.OpeningBalance.SnapshotAt.IsZero() ||
			e.OpeningBalance.SnapshotAt.After(e.ObservedAt) {
			return errors.New("opening balance requires manual source, operator, audit reference, snapshot hash, and snapshot time")
		}
	} else if e.OpeningBalance != nil {
		return errors.New("opening balance evidence is only valid on opening balance events")
	}
	if e.FillEvidence != nil {
		if e.Kind != RecoveryFillEvent {
			return errors.New("raw fill evidence is only valid on fill events")
		}
		for name, value := range map[string]string{
			"client order id": e.FillEvidence.ClientOrderID,
			"venue order id":  e.FillEvidence.VenueOrderID,
			"symbol":          e.FillEvidence.Symbol,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("fill evidence %s is required", name)
			}
		}
		if !validRecoveryHash(e.FillEvidence.OrderIntentHash) {
			return errors.New("fill evidence requires a valid order intent hash")
		}
		if e.FillEvidence.Side != "BUY" && e.FillEvidence.Side != "SELL" {
			return errors.New("fill evidence side must be BUY or SELL")
		}
		if strings.TrimSpace(e.FillEvidence.BaseAsset) == "" {
			return errors.New("fill evidence base asset is required")
		}
		if err := validateRawRecoveryDecimal(e.FillEvidence.PriceRaw, false); err != nil {
			return fmt.Errorf("fill evidence price: %w", err)
		}
		if err := validateRawRecoveryDecimal(e.FillEvidence.QuantityRaw, false); err != nil {
			return fmt.Errorf("fill evidence quantity: %w", err)
		}
		if rawRecoveryDecimalIsZero(e.FillEvidence.PriceRaw) || rawRecoveryDecimalIsZero(e.FillEvidence.QuantityRaw) {
			return errors.New("fill evidence price and quantity must be positive")
		}
		if !rawRecoveryDecimalsEqual(e.Amount, e.FillEvidence.QuantityRaw) {
			return errors.New("fill event amount must equal raw fill quantity")
		}
		if (e.FillEvidence.QuoteQuantityRaw == "") != (e.FillEvidence.QuoteAsset == "") {
			return errors.New("fill evidence quote amount and asset must be provided together")
		}
		if e.FillEvidence.QuoteQuantityRaw != "" {
			if err := validateRawRecoveryDecimal(e.FillEvidence.QuoteQuantityRaw, false); err != nil {
				return fmt.Errorf("fill evidence quote quantity: %w", err)
			}
			if rawRecoveryDecimalIsZero(e.FillEvidence.QuoteQuantityRaw) {
				return errors.New("fill evidence quote quantity must be positive")
			}
		}
		if e.FillEvidence.CommissionKnown {
			if strings.TrimSpace(e.FillEvidence.CommissionAsset) == "" {
				return errors.New("known fill commission requires an asset")
			}
			if err := validateRawRecoveryDecimal(e.FillEvidence.CommissionRaw, false); err != nil {
				return fmt.Errorf("fill evidence commission: %w", err)
			}
		} else if e.FillEvidence.CommissionRaw != "" || e.FillEvidence.CommissionAsset != "" {
			return errors.New("unknown fill commission cannot carry an amount or asset")
		}
		if e.FillEvidence.RealizedPnLKnown {
			if e.FillEvidence.RealizedPnLAsset == "" {
				return errors.New("known fill realized PnL requires an asset")
			}
			if err := validateRawRecoveryDecimal(e.FillEvidence.RealizedPnLRaw, true); err != nil {
				return fmt.Errorf("fill evidence realized PnL: %w", err)
			}
		} else if e.FillEvidence.RealizedPnLRaw != "" {
			return errors.New("unknown fill realized PnL cannot carry a raw amount")
		}
	}
	if e.Kind != RecoveryFillEvent && e.CursorSequence != 0 {
		return errors.New("non-fill event cannot advance fill cursor")
	}
	return nil
}

func (r RecoveryOperation) CanonicalHash() (string, error) {
	if r.HashVersion != 1 || strings.TrimSpace(r.EpisodeID) == "" || strings.TrimSpace(r.OperationID) == "" || strings.TrimSpace(r.Actor) == "" || strings.TrimSpace(r.AuditRef) == "" || r.ExpectedEpisodeRevision == 0 || strings.TrimSpace(r.ExpectedRuntimeRevision) == "" || !validRecoveryHash(r.EvidenceHash) || r.EvidenceReadAt.IsZero() {
		return "", errors.New("incomplete recovery operation")
	}
	copy := r
	copy.RequestHash = ""
	copy.VerifiedAnomalyIDs = append([]string(nil), r.VerifiedAnomalyIDs...)
	sort.Strings(copy.VerifiedAnomalyIDs)
	copy.EvidenceReadAt = copy.EvidenceReadAt.UTC()
	copy.Children = append([]RecoveryChild(nil), r.Children...)
	copy.Events = append([]RecoveryEvent(nil), r.Events...)
	sort.Slice(copy.Children, func(i, j int) bool { return copy.Children[i].ID < copy.Children[j].ID })
	sort.Slice(copy.Events, func(i, j int) bool { return recoveryEventKey(copy.Events[i]) < recoveryEventKey(copy.Events[j]) })
	if r.SourceWindow != nil {
		window := *r.SourceWindow
		copy.SourceWindow = &window
		copy.SourceWindow.From = copy.SourceWindow.From.UTC()
		copy.SourceWindow.Through = copy.SourceWindow.Through.UTC()
	}
	b, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func recoveryEventKey(e RecoveryEvent) string {
	return strings.Join([]string{e.Source, e.Exchange, e.Market, e.AccountScope, string(e.Kind), e.ExternalID}, "\x00")
}

func (p OneShotPermit) Validate() error {
	if p.ID == "" || p.EpisodeID == "" || p.Generation == 0 || p.ActionID == "" || p.ActionOrdinal == 0 || p.AccountScope == "" || p.OwnerGeneration == "" || p.LeaseID == 0 || p.ExpectedRevision == 0 || p.State != PermitAuthorized {
		return errors.New("invalid one-shot permit")
	}
	if err := ValidateGateSourceExceptions(p.GateSourceExceptions); err != nil {
		return err
	}
	switch p.Kind {
	case RecoveryActionOrder:
		if !validRecoveryHash(p.OrderIntentHash) {
			return errors.New("order permit requires an immutable order intent hash")
		}
		if p.OrderIntent == nil {
			return errors.New("order permit requires its immutable order intent")
		}
		hash, err := p.OrderIntent.CanonicalHash()
		if err != nil || hash != p.OrderIntentHash {
			return errors.New("order permit intent does not match its immutable hash")
		}
	case RecoveryActionBorrow, RecoveryActionRepay:
		if p.OrderIntentHash != "" || p.OrderIntent != nil {
			return errors.New("non-order permit cannot bind an order intent hash")
		}
	default:
		return errors.New("invalid recovery permit action kind")
	}
	return nil
}

func validRecoveryHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

// ValidateGateSourceExceptions accepts only the narrowly scoped sources
// approved for one-shot recovery actions. The episode source is implicit.
func ValidateGateSourceExceptions(sources []string) error {
	allowed := map[string]struct{}{
		"execution_intent_recovery":             {},
		"funding_carry_exposure_unverified":     {},
		"funding_carry_reconciliation_required": {},
	}
	previous := ""
	for _, source := range sources {
		if source == "" || source <= previous {
			return errors.New("gate source exceptions must be non-empty, sorted, and unique")
		}
		if _, ok := allowed[source]; !ok {
			return fmt.Errorf("gate source %q is not eligible for recovery exception", source)
		}
		previous = source
	}
	return nil
}
