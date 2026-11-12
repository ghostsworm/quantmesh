// Package daily reconciles a closed, explicitly scoped account day. It never
// submits exchange actions or derives withdrawal authorization.
package daily

import (
	"context"
	"errors"
	"time"
)

const (
	MethodVersion        = "usdt-three-wallets-v1"
	Timezone             = "Asia/Shanghai"
	Asset                = "USDT"
	Tolerance            = "0.00000001"
	SafetyLag            = 5 * time.Minute
	InterestHistoryLimit = 90 * 24 * time.Hour
	WalletSpot           = "spot"
	WalletMargin         = "spot_margin"
	WalletFutures        = "futures"
	SourceCashflow       = "cashflow"
	SourceExecution      = "execution"
	SourceFunding        = "funding"
	SourceInterest       = "interest"
	SourcePositions      = "positions"
)

type Kind string

const (
	Deposit            Kind = "deposit"
	Withdrawal         Kind = "withdrawal"
	Transfer           Kind = "transfer"
	BorrowPrincipal    Kind = "borrow_principal"
	RepayPrincipal     Kind = "repay_principal"
	InterestSettlement Kind = "interest_settlement"
	RealizedPnL        Kind = "realized_pnl"
	TradingFee         Kind = "trading_fee"
	Funding            Kind = "funding"
	InterestAccrual    Kind = "interest_accrual"
	OtherFee           Kind = "other_fee"
	Unknown            Kind = "unknown"
)

var (
	ErrNotFound = errors.New("daily equity evidence not found")
	ErrConflict = errors.New("daily equity evidence conflict")
	ErrInvalid  = errors.New("invalid daily equity request or evidence")
)

// Query identifies a single credential scope, not a configurable aggregate.
type Query struct {
	Exchange     string `json:"exchange"`
	AccountScope string `json:"accountScope"`
	Date         string `json:"date"`
}

type Scope struct {
	Exchange         string    `json:"exchange"`
	AccountScope     string    `json:"accountScope"`
	Testnet          bool      `json:"testnet"`
	Revision         int64     `json:"revision"`
	Wallets          []string  `json:"wallets"`
	HistoryStart     time.Time `json:"historyStart"`
	EffectiveFrom    time.Time `json:"effectiveFrom"`
	EffectiveThrough time.Time `json:"effectiveThrough"`
}

// Balance is gross asset quantity with separately stated debt. An already-net
// exchange balance must not be placed here and have its liabilities subtracted again.
type Balance struct {
	Wallet      string    `json:"wallet"`
	Asset       string    `json:"asset"`
	Quantity    string    `json:"quantity"`
	Principal   string    `json:"principal"`
	Interest    string    `json:"interest"`
	Price       string    `json:"price"`
	PriceSource string    `json:"priceSource"`
	PriceAt     time.Time `json:"priceAt"`
}

type Position struct {
	Symbol      string    `json:"symbol"`
	Quantity    string    `json:"quantity"`
	EntryPrice  string    `json:"entryPrice"`
	MarkPrice   string    `json:"markPrice"`
	Unrealized  string    `json:"unrealized"`
	PriceSource string    `json:"priceSource"`
	PriceAt     time.Time `json:"priceAt"`
}

type Snapshot struct {
	SourceID      string     `json:"sourceId"`
	At            time.Time  `json:"at"`
	ObservedAt    time.Time  `json:"observedAt"`
	ScopeRevision int64      `json:"scopeRevision"`
	Complete      bool       `json:"complete"`
	Balances      []Balance  `json:"balances"`
	Positions     []Position `json:"positions"`
}

// Event describes wallet movements and links existing economic ledger facts.
// Source+SourceID+Wallet is stable across business reclassification. Dedicated
// PnL/fee/funding/interest events must reference exactly one existing LedgerEntry.
type Event struct {
	Source             string    `json:"source"`
	SourceID           string    `json:"sourceId"`
	EconomicID         string    `json:"economicId"`
	Wallet             string    `json:"wallet"`
	Asset              string    `json:"asset"`
	At                 time.Time `json:"at"`
	Kind               Kind      `json:"kind"`
	Amount             string    `json:"amount"`
	PrincipalDelta     string    `json:"principalDelta"`
	InterestDelta      string    `json:"interestDelta"`
	LedgerRef          string    `json:"ledgerRef,omitempty"`
	TransferID         string    `json:"transferId,omitempty"`
	CounterpartyWallet string    `json:"counterpartyWallet,omitempty"`
}

// Coverage is an archived complete source-window receipt. An empty list without
// this receipt does not prove zero activity. Digest binds the source response;
// Entries are independently read from existing SQL ledgers and checked below.
type Coverage struct {
	Source        string    `json:"source"`
	Wallet        string    `json:"wallet"`
	Symbol        string    `json:"symbol,omitempty"`
	From          time.Time `json:"from"`
	Through       time.Time `json:"through"`
	ObservedAt    time.Time `json:"observedAt"`
	Complete      bool      `json:"complete"`
	Pages         int       `json:"pages"`
	ExpectedCount int       `json:"expectedCount"`
	ActualCount   int       `json:"actualCount"`
	Digest        string    `json:"digest"`
}

type Evidence struct {
	Scope          Scope      `json:"scope"`
	Date           string     `json:"date"`
	Timezone       string     `json:"timezone"`
	Method         string     `json:"method"`
	Opening        Snapshot   `json:"opening"`
	Closing        Snapshot   `json:"closing"`
	Events         []Event    `json:"events"`
	Coverage       []Coverage `json:"coverage"`
	FuturesSymbols []string   `json:"futuresSymbols"`
	// Notes records why this evidence cannot be complete. They are copied into
	// the published reasons and never authorize a verified day on their own.
	Notes []string `json:"notes,omitempty"`
}

// LedgerEntry is a projection, not a second ledger. SQL adapters read it from
// order_fills, funding_payments, and margin_interest_payments. Amount is signed
// PnL (fees/interest negative). Quantity and Side apply only to fill PnL entries.
type LedgerEntry struct {
	ID       string    `json:"id"`
	Kind     Kind      `json:"kind"`
	Market   string    `json:"market"`
	Symbol   string    `json:"symbol,omitempty"`
	Asset    string    `json:"asset"`
	At       time.Time `json:"at"`
	Amount   string    `json:"amount"`
	Known    bool      `json:"known"`
	Quantity string    `json:"quantity,omitempty"`
	Side     string    `json:"side,omitempty"`
}

type Inputs struct {
	Evidence         Evidence      `json:"evidence"`
	EvidenceRevision int64         `json:"evidenceRevision"`
	Entries          []LedgerEntry `json:"entries"`
	LedgerCoverage   []Coverage    `json:"ledgerCoverage"`
	Blockers         []string      `json:"blockers"`
	Digest           string        `json:"-"`
}

type Amounts struct {
	OpeningEquity      string `json:"openingEquity"`
	ClosingEquity      string `json:"closingEquity"`
	NetExternalCapital string `json:"netExternalCapital"`
	TradingGross       string `json:"tradingGross"`
	TradingFees        string `json:"tradingFees"`
	TradingNet         string `json:"tradingNet"`
	FundingNet         string `json:"fundingNet"`
	InterestCost       string `json:"interestCost"`
	UnrealizedChange   string `json:"unrealizedChange"`
	ValuationChange    string `json:"valuationChange"`
	OtherCosts         string `json:"otherCosts"`
	EquityProfit       string `json:"equityProfit"`
	ExplainedProfit    string `json:"explainedProfit"`
	Residual           string `json:"residual"`
}

type Result struct {
	Query                        Query     `json:"query"`
	Scope                        Scope     `json:"scope"`
	Timezone                     string    `json:"timezone"`
	Method                       string    `json:"method"`
	Asset                        string    `json:"asset"`
	PeriodStart                  time.Time `json:"periodStart"`
	PeriodEnd                    time.Time `json:"periodEnd"`
	Revision                     int64     `json:"revision"`
	EvidenceRevision             int64     `json:"evidenceRevision"`
	InputDigest                  string    `json:"inputDigest"`
	ComputedAt                   time.Time `json:"computedAt"`
	Amounts                      Amounts   `json:"amounts"`
	Tolerance                    string    `json:"tolerance"`
	EquityReconciliationComplete bool      `json:"equityReconciliationComplete"`
	Reasons                      []string  `json:"reasons"`
}

// Repository is implemented by real SQL storage. Commit must recheck the full
// input digest, including mutable ledger facts, in the publication transaction.
type Repository interface {
	ImportDailyEquityEvidence(context.Context, Evidence, int64) (int64, error)
	LoadDailyEquityInputs(context.Context, Query) (Inputs, error)
	CommitDailyEquityResult(context.Context, Query, string, Result) (Result, error)
	LoadDailyEquityResult(context.Context, Query) (Result, error)
}
