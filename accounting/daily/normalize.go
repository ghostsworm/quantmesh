package daily

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	maxDecimalLength = 48
	maxDecimalDigits = 36
	maxDecimalScale  = 18
)

var toleranceRat = mustDecimal(Tolerance)

func mustDecimal(value string) *big.Rat {
	rat, err := parseDecimal(value)
	if err != nil {
		panic("daily: invalid package decimal: " + value)
	}
	return rat
}

// DayBounds returns the half-open UTC interval [start, end) of a local calendar
// day in the fixed reconciliation timezone.
func DayBounds(date string) (time.Time, time.Time, error) {
	if date == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: date is required", ErrInvalid)
	}
	start, err := time.ParseInLocation("2006-01-02", date, reconcileLocation())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid date %q: %v", ErrInvalid, date, err)
	}
	if start.Format("2006-01-02") != date {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: non-canonical date %q", ErrInvalid, date)
	}
	// Calendar-day arithmetic, not a fixed 24h duration.
	end := start.AddDate(0, 0, 1)
	return start.UTC(), end.UTC(), nil
}

func reconcileLocation() *time.Location {
	if loc, err := time.LoadLocation(Timezone); err == nil {
		return loc
	}
	// Asia/Shanghai has used a fixed +08:00 offset since 1991. The fallback keeps
	// the package deterministic on systems without an installed tzdata database.
	return time.FixedZone(Timezone, 8*60*60)
}

func parseDecimal(value string) (*big.Rat, error) {
	if value == "" {
		return nil, fmt.Errorf("%w: empty decimal", ErrInvalid)
	}
	if len(value) > maxDecimalLength {
		return nil, fmt.Errorf("%w: decimal %q exceeds %d characters", ErrInvalid, value, maxDecimalLength)
	}
	digits := 0
	scale := 0
	seenDot := false
	index := 0
	if value[0] == '-' || value[0] == '+' {
		index++
		if index == len(value) {
			return nil, fmt.Errorf("%w: decimal %q has no digits", ErrInvalid, value)
		}
	}
	for ; index < len(value); index++ {
		ch := value[index]
		if ch == '.' {
			if seenDot {
				return nil, fmt.Errorf("%w: decimal %q has multiple points", ErrInvalid, value)
			}
			seenDot = true
			continue
		}
		if ch < '0' || ch > '9' {
			return nil, fmt.Errorf("%w: decimal %q is not a plain decimal", ErrInvalid, value)
		}
		digits++
		if digits > maxDecimalDigits {
			return nil, fmt.Errorf("%w: decimal %q has too many digits", ErrInvalid, value)
		}
		if seenDot {
			scale++
			if scale > maxDecimalScale {
				return nil, fmt.Errorf("%w: decimal %q has too many fractional digits", ErrInvalid, value)
			}
		}
	}
	if digits == 0 {
		return nil, fmt.Errorf("%w: decimal %q has no digits", ErrInvalid, value)
	}
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("%w: decimal %q cannot be parsed", ErrInvalid, value)
	}
	return rat, nil
}

func formatDecimal(rat *big.Rat) string {
	if rat == nil || rat.Sign() == 0 {
		return "0"
	}
	numerator := new(big.Int).Set(rat.Num())
	denominator := new(big.Int).Set(rat.Denom())
	originalDenominator := new(big.Int).Set(denominator)

	two := big.NewInt(2)
	five := big.NewInt(5)
	remainder := new(big.Int)
	twos := 0
	for {
		remainder.Mod(denominator, two)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Quo(denominator, two)
		twos++
	}
	fives := 0
	for {
		remainder.Mod(denominator, five)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Quo(denominator, five)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		// Decimal arithmetic in this package always terminates. Keep a bounded
		// fallback so a caller-supplied rational can never explode output size.
		return trimDecimal(rat.FloatString(maxDecimalScale))
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	factor := new(big.Int).Quo(pow10, originalDenominator)
	scaled := new(big.Int).Mul(numerator, factor)

	sign := ""
	if scaled.Sign() < 0 {
		sign = "-"
		scaled.Abs(scaled)
	}
	text := scaled.String()
	if scale == 0 {
		return sign + text
	}
	if len(text) <= scale {
		text = strings.Repeat("0", scale-len(text)+1) + text
	}
	integerPart := text[:len(text)-scale]
	fractionPart := strings.TrimRight(text[len(text)-scale:], "0")
	if fractionPart == "" {
		return sign + integerPart
	}
	return sign + integerPart + "." + fractionPart
}

func trimDecimal(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	return strings.TrimSuffix(value, ".")
}

func utc(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return value.UTC()
}

func decimalString(value string) (string, error) {
	rat, err := parseDecimal(value)
	if err != nil {
		return "", err
	}
	return formatDecimal(rat), nil
}

// PrepareEvidence returns a canonical copy of evidence: UTC timestamps,
// canonical decimal strings, sorted slices and duplicate source identities
// removed. Identical duplicates are accepted; conflicting duplicates fail.
func PrepareEvidence(evidence Evidence) (Evidence, error) {
	out := evidence

	if _, _, err := DayBounds(evidence.Date); err != nil {
		return Evidence{}, err
	}

	out.Scope = normalizeScope(evidence.Scope)
	opening, err := normalizeSnapshot(evidence.Opening)
	if err != nil {
		return Evidence{}, fmt.Errorf("opening snapshot: %w", err)
	}
	closing, err := normalizeSnapshot(evidence.Closing)
	if err != nil {
		return Evidence{}, fmt.Errorf("closing snapshot: %w", err)
	}
	out.Opening = opening
	out.Closing = closing

	events, err := normalizeEvents(evidence.Events)
	if err != nil {
		return Evidence{}, err
	}
	out.Events = events

	coverage, err := normalizeCoverageList(evidence.Coverage)
	if err != nil {
		return Evidence{}, err
	}
	out.Coverage = coverage

	out.FuturesSymbols = normalizeSymbols(evidence.FuturesSymbols)
	return out, nil
}

func normalizeScope(scope Scope) Scope {
	out := scope
	out.HistoryStart = utc(scope.HistoryStart)
	out.EffectiveFrom = utc(scope.EffectiveFrom)
	out.EffectiveThrough = utc(scope.EffectiveThrough)
	out.Wallets = append([]string(nil), scope.Wallets...)
	sort.Strings(out.Wallets)
	return out
}

func normalizeSnapshot(snapshot Snapshot) (Snapshot, error) {
	out := snapshot
	out.At = utc(snapshot.At)
	out.ObservedAt = utc(snapshot.ObservedAt)

	out.Balances = append([]Balance(nil), snapshot.Balances...)
	for index := range out.Balances {
		balance := &out.Balances[index]
		var err error
		if balance.Quantity, err = normalizeDecimalField(balance.Quantity); err != nil {
			return Snapshot{}, fmt.Errorf("balance %s/%s quantity: %w", balance.Wallet, balance.Asset, err)
		}
		if balance.Principal, err = normalizeZeroDecimal(balance.Principal); err != nil {
			return Snapshot{}, fmt.Errorf("balance %s/%s principal: %w", balance.Wallet, balance.Asset, err)
		}
		if balance.Interest, err = normalizeZeroDecimal(balance.Interest); err != nil {
			return Snapshot{}, fmt.Errorf("balance %s/%s interest: %w", balance.Wallet, balance.Asset, err)
		}
		if balance.Price, err = normalizeDecimalField(balance.Price); err != nil {
			return Snapshot{}, fmt.Errorf("balance %s/%s price: %w", balance.Wallet, balance.Asset, err)
		}
		balance.PriceAt = utc(balance.PriceAt)
	}
	sort.Slice(out.Balances, func(i, j int) bool {
		left, right := out.Balances[i], out.Balances[j]
		if left.Wallet != right.Wallet {
			return left.Wallet < right.Wallet
		}
		if left.Asset != right.Asset {
			return left.Asset < right.Asset
		}
		if left.Quantity != right.Quantity {
			return left.Quantity < right.Quantity
		}
		if left.Principal != right.Principal {
			return left.Principal < right.Principal
		}
		if left.Interest != right.Interest {
			return left.Interest < right.Interest
		}
		if left.Price != right.Price {
			return left.Price < right.Price
		}
		if left.PriceSource != right.PriceSource {
			return left.PriceSource < right.PriceSource
		}
		return left.PriceAt.Before(right.PriceAt)
	})

	out.Positions = append([]Position(nil), snapshot.Positions...)
	for index := range out.Positions {
		position := &out.Positions[index]
		var err error
		if position.Quantity, err = normalizeDecimalField(position.Quantity); err != nil {
			return Snapshot{}, fmt.Errorf("position %s quantity: %w", position.Symbol, err)
		}
		if position.EntryPrice, err = normalizeDecimalField(position.EntryPrice); err != nil {
			return Snapshot{}, fmt.Errorf("position %s entryPrice: %w", position.Symbol, err)
		}
		if position.MarkPrice, err = normalizeDecimalField(position.MarkPrice); err != nil {
			return Snapshot{}, fmt.Errorf("position %s markPrice: %w", position.Symbol, err)
		}
		if position.Unrealized, err = normalizeDecimalField(position.Unrealized); err != nil {
			return Snapshot{}, fmt.Errorf("position %s unrealized: %w", position.Symbol, err)
		}
		position.PriceAt = utc(position.PriceAt)
	}
	sort.Slice(out.Positions, func(i, j int) bool {
		left, right := out.Positions[i], out.Positions[j]
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		if left.Quantity != right.Quantity {
			return left.Quantity < right.Quantity
		}
		if left.EntryPrice != right.EntryPrice {
			return left.EntryPrice < right.EntryPrice
		}
		if left.MarkPrice != right.MarkPrice {
			return left.MarkPrice < right.MarkPrice
		}
		if left.Unrealized != right.Unrealized {
			return left.Unrealized < right.Unrealized
		}
		if left.PriceSource != right.PriceSource {
			return left.PriceSource < right.PriceSource
		}
		return left.PriceAt.Before(right.PriceAt)
	})
	return out, nil
}

// normalizeDecimalField canonicalizes values that may be omitted; the omitted
// form is left empty and handled as a business completeness reason later.
// Malformed decimals are structural damage and fail closed.
func normalizeDecimalField(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return decimalString(value)
}

func normalizeZeroDecimal(value string) (string, error) {
	if value == "" {
		return "0", nil
	}
	return decimalString(value)
}

func normalizeEvents(events []Event) ([]Event, error) {
	out := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Source == "" || event.SourceID == "" || event.Wallet == "" || event.Asset == "" {
			return nil, fmt.Errorf("%w: event has empty source, sourceId, wallet or asset", ErrInvalid)
		}
		if event.At.IsZero() {
			return nil, fmt.Errorf("%w: event %s/%s has no time", ErrInvalid, event.Source, event.SourceID)
		}
		if event.Kind == "" {
			return nil, fmt.Errorf("%w: event %s/%s has no kind", ErrInvalid, event.Source, event.SourceID)
		}
		amount, err := decimalString(event.Amount)
		if err != nil {
			return nil, fmt.Errorf("event %s/%s amount: %w", event.Source, event.SourceID, err)
		}
		principalDelta, err := normalizeDelta(event.PrincipalDelta)
		if err != nil {
			return nil, fmt.Errorf("event %s/%s principalDelta: %w", event.Source, event.SourceID, err)
		}
		interestDelta, err := normalizeDelta(event.InterestDelta)
		if err != nil {
			return nil, fmt.Errorf("event %s/%s interestDelta: %w", event.Source, event.SourceID, err)
		}
		event.At = utc(event.At)
		event.Amount = amount
		event.PrincipalDelta = principalDelta
		event.InterestDelta = interestDelta
		out = append(out, event)
	}

	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.SourceID != right.SourceID {
			return left.SourceID < right.SourceID
		}
		if left.Wallet != right.Wallet {
			return left.Wallet < right.Wallet
		}
		if !left.At.Equal(right.At) {
			return left.At.Before(right.At)
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.EconomicID < right.EconomicID
	})

	deduped := make([]Event, 0, len(out))
	for index, event := range out {
		if index > 0 && sameSourceIdentity(out[index-1], event) {
			if !sameEventPayload(out[index-1], event) {
				return nil, fmt.Errorf("%w: event %s/%s/%s has conflicting duplicate content", ErrConflict, event.Source, event.SourceID, event.Wallet)
			}
			continue
		}
		deduped = append(deduped, event)
	}
	return deduped, nil
}

func normalizeDelta(value string) (string, error) {
	if value == "" {
		return "0", nil
	}
	return decimalString(value)
}

func sameSourceIdentity(left, right Event) bool {
	return left.Source == right.Source && left.SourceID == right.SourceID && left.Wallet == right.Wallet
}

func sameEventPayload(left, right Event) bool {
	return left.EconomicID == right.EconomicID &&
		left.Asset == right.Asset &&
		left.At.Equal(right.At) &&
		left.Kind == right.Kind &&
		left.Amount == right.Amount &&
		left.PrincipalDelta == right.PrincipalDelta &&
		left.InterestDelta == right.InterestDelta &&
		left.LedgerRef == right.LedgerRef &&
		left.TransferID == right.TransferID &&
		left.CounterpartyWallet == right.CounterpartyWallet
}

func normalizeCoverageList(coverage []Coverage) ([]Coverage, error) {
	out := make([]Coverage, 0, len(coverage))
	for _, receipt := range coverage {
		receipt.From = utc(receipt.From)
		receipt.Through = utc(receipt.Through)
		receipt.ObservedAt = utc(receipt.ObservedAt)
		receipt.Digest = strings.ToLower(strings.TrimSpace(receipt.Digest))
		out = append(out, receipt)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Wallet != right.Wallet {
			return left.Wallet < right.Wallet
		}
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		if !left.From.Equal(right.From) {
			return left.From.Before(right.From)
		}
		if !left.Through.Equal(right.Through) {
			return left.Through.Before(right.Through)
		}
		if !left.ObservedAt.Equal(right.ObservedAt) {
			return left.ObservedAt.Before(right.ObservedAt)
		}
		if left.Complete != right.Complete {
			return !left.Complete
		}
		if left.Pages != right.Pages {
			return left.Pages < right.Pages
		}
		if left.ExpectedCount != right.ExpectedCount {
			return left.ExpectedCount < right.ExpectedCount
		}
		if left.ActualCount != right.ActualCount {
			return left.ActualCount < right.ActualCount
		}
		return left.Digest < right.Digest
	})
	deduped := make([]Coverage, 0, len(out))
	for index, receipt := range out {
		if index > 0 && sameCoverageReceipt(out[index-1], receipt) {
			continue
		}
		deduped = append(deduped, receipt)
	}
	return deduped, nil
}

func sameCoverageWindow(left, right Coverage) bool {
	return left.Source == right.Source && left.Wallet == right.Wallet && left.Symbol == right.Symbol &&
		left.From.Equal(right.From) && left.Through.Equal(right.Through)
}

func sameCoverageReceipt(left, right Coverage) bool {
	return sameCoverageWindow(left, right) && sameCoveragePayload(left, right)
}

func sameCoveragePayload(left, right Coverage) bool {
	return left.ObservedAt.Equal(right.ObservedAt) &&
		left.Complete == right.Complete &&
		left.Pages == right.Pages &&
		left.ExpectedCount == right.ExpectedCount &&
		left.ActualCount == right.ActualCount &&
		left.Digest == right.Digest
}

func normalizeSymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		if _, exists := seen[symbol]; exists {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}

func normalizeEntries(entries []LedgerEntry) ([]LedgerEntry, error) {
	out := make([]LedgerEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return nil, fmt.Errorf("%w: ledger entry has empty id", ErrInvalid)
		}
		if entry.At.IsZero() {
			return nil, fmt.Errorf("%w: ledger entry %s has no time", ErrInvalid, entry.ID)
		}
		if entry.Kind == "" {
			return nil, fmt.Errorf("%w: ledger entry %s has no kind", ErrInvalid, entry.ID)
		}
		if entry.Amount != "" {
			amount, err := decimalString(entry.Amount)
			if err != nil {
				return nil, fmt.Errorf("ledger entry %s amount: %w", entry.ID, err)
			}
			entry.Amount = amount
		}
		if entry.Quantity != "" {
			quantity, err := decimalString(entry.Quantity)
			if err != nil {
				return nil, fmt.Errorf("ledger entry %s quantity: %w", entry.ID, err)
			}
			entry.Quantity = quantity
		}
		entry.At = utc(entry.At)
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].At.Before(out[j].At)
	})
	deduped := make([]LedgerEntry, 0, len(out))
	for index, entry := range out {
		if index > 0 && entry.ID == out[index-1].ID {
			if !sameLedgerEntry(out[index-1], entry) {
				return nil, fmt.Errorf("%w: ledger entry %s has conflicting duplicate content", ErrConflict, entry.ID)
			}
			continue
		}
		deduped = append(deduped, entry)
	}
	return deduped, nil
}

func sameLedgerEntry(left, right LedgerEntry) bool {
	return left.Kind == right.Kind &&
		left.Market == right.Market &&
		left.Symbol == right.Symbol &&
		left.Asset == right.Asset &&
		left.At.Equal(right.At) &&
		left.Amount == right.Amount &&
		left.Known == right.Known &&
		left.Quantity == right.Quantity &&
		left.Side == right.Side
}

// EvidenceHash hashes canonical evidence only. It does not include ledger
// facts; callers that need a full snapshot use InputDigest.
func EvidenceHash(evidence Evidence) (string, error) {
	normalized, err := PrepareEvidence(evidence)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: encode evidence: %v", ErrInvalid, err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// InputDigest binds evidence, ledger projections and blockers into one full
// input snapshot. Ledger entries are the authoritative economic amounts.
func InputDigest(inputs Inputs) (string, error) {
	evidence, err := PrepareEvidence(inputs.Evidence)
	if err != nil {
		return "", err
	}
	entries, err := normalizeEntries(inputs.Entries)
	if err != nil {
		return "", err
	}
	ledgerCoverage, err := normalizeCoverageList(inputs.LedgerCoverage)
	if err != nil {
		return "", err
	}
	blockers := append([]string(nil), inputs.Blockers...)
	sort.Strings(blockers)

	canonical := struct {
		Evidence         Evidence      `json:"evidence"`
		EvidenceRevision int64         `json:"evidenceRevision"`
		Entries          []LedgerEntry `json:"entries"`
		LedgerCoverage   []Coverage    `json:"ledgerCoverage"`
		Blockers         []string      `json:"blockers"`
	}{
		Evidence:         evidence,
		EvidenceRevision: inputs.EvidenceRevision,
		Entries:          entries,
		LedgerCoverage:   ledgerCoverage,
		Blockers:         blockers,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: encode daily equity inputs: %v", ErrInvalid, err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
