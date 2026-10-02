package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// AccountWalletCapitalClaim reserves one Bot's quote-currency budget in one
// verified wallet. WalletKey and BotID are opaque stable identifiers.
type AccountWalletCapitalClaim struct {
	WalletKey string
	// ReservationToken identifies one runtime generation. A stale runtime can
	// release only the generation it created, never a replacement claim.
	ReservationToken string
	Amount           float64
	Available        float64
	// ObservationSequence is issued by shared storage before a balance query.
	ObservationSequence int64
	// ObservedAt is diagnostic metadata only; ordering uses ObservationSequence.
	ObservedAt time.Time
	Exchange   string
	Market     string
	QuoteAsset string
	Symbol     string
}

// AccountWalletCapitalReservation is a read-only, credential-free audit view.
type AccountWalletCapitalReservation struct {
	WalletKey  string    `json:"wallet_key"`
	BotKey     string    `json:"bot_key"`
	Amount     float64   `json:"amount"`
	UpdatedAt  time.Time `json:"updated_at"`
	Exchange   string    `json:"exchange,omitempty"`
	Market     string    `json:"market,omitempty"`
	QuoteAsset string    `json:"quote_asset,omitempty"`
	Symbol     string    `json:"symbol,omitempty"`
	Mapped     bool      `json:"mapped"`
}

// FundingSpreadCapitalClaim remains a source-compatible name for older callers.
type FundingSpreadCapitalClaim = AccountWalletCapitalClaim

// AccountWalletCapitalReservationStore provides atomic multi-wallet claims.
type AccountWalletCapitalReservationStore interface {
	ReserveAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error
	ReleaseAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error
}

// AccountWalletBalanceObservationIssuer serializes balance query starts across
// application instances without relying on their wall clocks.
type AccountWalletBalanceObservationIssuer interface {
	BeginAccountWalletBalanceObservation(ctx context.Context, walletKey string) (int64, error)
}

type AccountWalletCapitalReservationReader interface {
	ListAccountWalletCapitalReservations(ctx context.Context, afterWalletKey, afterBotKey string, limit int) ([]AccountWalletCapitalReservation, error)
}

// AccountWalletCapitalReservationBotChecker checks whether a Bot still owns any persistent wallet claim.
type AccountWalletCapitalReservationBotChecker interface {
	HasAccountWalletCapitalReservation(ctx context.Context, botID string) (bool, error)
}

const AccountWalletCapitalReservationAuditPageSize = 100

// FundingSpreadCapitalReservationStore remains for source compatibility.
type FundingSpreadCapitalReservationStore interface {
	ReserveFundingSpreadCapital(ctx context.Context, botID string, claims []FundingSpreadCapitalClaim) error
	ReleaseFundingSpreadCapital(ctx context.Context, botID string, claims []FundingSpreadCapitalClaim) error
}

// MultiProcessAccountWalletCapitalStore identifies backends whose transaction
// and row-lock semantics are suitable for separate application processes.
type MultiProcessAccountWalletCapitalStore interface {
	AccountWalletCapitalReservationStore
	SupportsMultiProcessAccountWalletCapital() bool
}

// MultiProcessFundingSpreadCapitalStore remains for source compatibility.
type MultiProcessFundingSpreadCapitalStore interface {
	FundingSpreadCapitalReservationStore
	SupportsMultiProcessFundingSpreadCapital() bool
}

//go:embed migrations/*_funding_spread_capital_*.sql
var fundingSpreadCapitalMigrations embed.FS

func (s *SQLStorage) SupportsMultiProcessAccountWalletCapital() bool {
	return s != nil && s.dbType == "mysql"
}

func (s *SQLStorage) SupportsMultiProcessFundingSpreadCapital() bool {
	return s.SupportsMultiProcessAccountWalletCapital()
}

func migrateFundingSpreadCapitalTables(db *sql.DB) error {
	return applyFundingSpreadCapitalMigration(db, "sqlite")
}

func migrateFundingSpreadCapitalTablesMySQL(db *sql.DB) error {
	return applyFundingSpreadCapitalMigration(db, "mysql")
}

func applyFundingSpreadCapitalMigration(db *sql.DB, dialect string) error {
	for _, version := range []string{"2026093001", "2026093002", "2026093003", "2026093004", "2026100201"} {
		if version == "2026093003" {
			exists, err := fundingSpreadReservationTokenColumnExists(db, dialect)
			if err != nil {
				return fmt.Errorf("check account wallet reservation generation column: %w", err)
			}
			if exists {
				continue
			}
		}
		name := "migrations/" + version + "_funding_spread_capital_" + dialect + ".up.sql"
		data, err := fundingSpreadCapitalMigrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read funding spread capital migration %s: %w", name, err)
		}
		for _, statement := range strings.Split(string(data), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				return fmt.Errorf("apply funding spread capital migration %s: %w", name, err)
			}
		}
	}
	return nil
}

func (s *SQLStorage) BeginAccountWalletBalanceObservation(ctx context.Context, walletKey string) (int64, error) {
	if s == nil || s.db == nil || ctx == nil || !isFundingSpreadDigest(walletKey) {
		return 0, errors.New("wallet balance observation requires context and wallet digest")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("begin wallet balance observation generation: %w", err)
	}
	defer tx.Rollback()
	if err := lockFundingSpreadWallet(ctx, tx, s.dbType, walletKey); err != nil {
		return 0, err
	}
	if s.dbType == "mysql" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO funding_spread_wallet_observation_sequences (wallet_key, latest_sequence) VALUES (?, 0) ON DUPLICATE KEY UPDATE wallet_key=VALUES(wallet_key)`, walletKey); err != nil {
			return 0, fmt.Errorf("initialize wallet observation sequence: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO funding_spread_wallet_observation_sequences (wallet_key, latest_sequence) VALUES (?, 0)`, walletKey); err != nil {
		return 0, fmt.Errorf("initialize wallet observation sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE funding_spread_wallet_observation_sequences SET latest_sequence = latest_sequence + 1 WHERE wallet_key = ?`, walletKey); err != nil {
		return 0, fmt.Errorf("advance wallet observation sequence: %w", err)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT latest_sequence FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`, walletKey).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("read wallet observation sequence: %w", err)
	}
	if sequence <= 0 {
		return 0, errors.New("wallet observation sequence is invalid")
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit wallet observation sequence: %w", err)
	}
	return sequence, nil
}

func fundingSpreadReservationTokenColumnExists(db *sql.DB, dialect string) (bool, error) {
	switch dialect {
	case "sqlite":
		rows, err := db.Query(`PRAGMA table_info(funding_spread_capital_reservations)`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				return false, err
			}
			if name == "reservation_token" {
				return true, nil
			}
		}
		return false, rows.Err()
	case "mysql":
		var column string
		err := db.QueryRow(`SHOW COLUMNS FROM funding_spread_capital_reservations LIKE 'reservation_token'`).Scan(&column, new(string), new(string), new(string), new(sql.NullString), new(sql.NullString))
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	default:
		return false, fmt.Errorf("unsupported migration dialect %q", dialect)
	}
}

func (s *SQLStorage) ReserveAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error {
	if ctx == nil {
		return errors.New("account wallet reservation requires context")
	}
	botKey := fundingSpreadBotKey(botID)
	if botKey == "" {
		return errors.New("account wallet reservation requires Bot identity")
	}
	claims, err := normalizeFundingSpreadClaims(claims, true)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		if claim.Exchange != "" || claim.Market != "" || claim.QuoteAsset != "" || claim.Symbol != "" {
			if err := validateCapitalReservationMetadata(claim); err != nil {
				return err
			}
			if claim.ObservationSequence <= 0 {
				return errors.New("account wallet reservation requires a shared observation sequence for identified wallets")
			}
		}
	}
	// Balance evidence must survive a rejected reservation. Otherwise a low
	// observation that proves existing claims exceed current funds would roll
	// back with the failed claim, letting a delayed stale high sample win later.
	claims, err = s.persistWalletBalanceObservations(ctx, claims)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin account wallet reservation transaction: %w", err)
	}
	defer tx.Rollback()

	for _, claim := range claims {
		if err := lockFundingSpreadWallet(ctx, tx, s.dbType, claim.WalletKey); err != nil {
			return err
		}
	}
	availableByWallet := make(map[string]float64, len(claims))
	for _, claim := range claims {
		var latestSequence int64
		if err := tx.QueryRowContext(ctx, `SELECT latest_sequence FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`, claim.WalletKey).Scan(&latestSequence); err != nil {
			return fmt.Errorf("read latest wallet observation generation: %w", err)
		}
		if claim.ObservationSequence != latestSequence {
			return fmt.Errorf("wallet %s balance observation was superseded before reservation", claim.WalletKey)
		}
		available, err := readLatestWalletBalance(ctx, tx, claim.WalletKey)
		if err != nil {
			return err
		}
		availableByWallet[claim.WalletKey] = available
	}

	type write struct {
		walletKey        string
		reservationToken string
		amount           float64
	}
	writes := make([]write, 0, len(claims))
	for _, claim := range claims {
		var current sql.NullFloat64
		if err := tx.QueryRowContext(ctx, `SELECT amount FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, botKey).Scan(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read existing account wallet reservation: %w", err)
		}
		amount := claim.Amount
		if current.Valid {
			if math.IsNaN(current.Float64) || math.IsInf(current.Float64, 0) || current.Float64 <= 0 {
				return errors.New("existing account wallet reservation is invalid")
			}
			if current.Float64 > amount {
				amount = current.Float64
			}
		}
		var others float64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key <> ?`, claim.WalletKey, botKey).Scan(&others); err != nil {
			return fmt.Errorf("sum existing account wallet reservations: %w", err)
		}
		available := availableByWallet[claim.WalletKey]
		if math.IsNaN(others) || math.IsInf(others, 0) || others < 0 || others > math.MaxFloat64-amount || others+amount > available {
			return fmt.Errorf("wallet %s cannot safely retain %.12g quote units; other reservations %.12g, verified available %.12g", claim.WalletKey, amount, others, available)
		}
		writes = append(writes, write{walletKey: claim.WalletKey, reservationToken: claim.ReservationToken, amount: amount})
	}

	query := `INSERT INTO funding_spread_capital_reservations (wallet_key, bot_key, reservation_token, amount, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(wallet_key, bot_key) DO UPDATE SET
		reservation_token=excluded.reservation_token, amount=excluded.amount, updated_at=excluded.updated_at`
	if s.dbType == "mysql" {
		query = `INSERT INTO funding_spread_capital_reservations (wallet_key, bot_key, reservation_token, amount, updated_at)
			VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE reservation_token=VALUES(reservation_token),
			amount=VALUES(amount), updated_at=VALUES(updated_at)`
	}
	for _, item := range writes {
		if _, err := tx.ExecContext(ctx, query, item.walletKey, botKey, item.reservationToken, item.amount, time.Now().UTC()); err != nil {
			return fmt.Errorf("write account wallet capital reservation: %w", err)
		}
	}
	for _, claim := range claims {
		if claim.Exchange == "" && claim.Market == "" && claim.QuoteAsset == "" && claim.Symbol == "" {
			continue
		}
		if err := validateCapitalReservationMetadata(claim); err != nil {
			return err
		}
		metadataQuery := `INSERT INTO funding_spread_capital_reservation_metadata
			(wallet_key, bot_key, exchange_name, market_type, quote_asset, symbol, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(wallet_key, bot_key) DO UPDATE SET
			exchange_name=excluded.exchange_name, market_type=excluded.market_type,
			quote_asset=excluded.quote_asset, symbol=excluded.symbol, updated_at=excluded.updated_at`
		if s.dbType == "mysql" {
			metadataQuery = `INSERT INTO funding_spread_capital_reservation_metadata
				(wallet_key, bot_key, exchange_name, market_type, quote_asset, symbol, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE
				exchange_name=VALUES(exchange_name), market_type=VALUES(market_type),
				quote_asset=VALUES(quote_asset), symbol=VALUES(symbol), updated_at=VALUES(updated_at)`
		}
		if _, err := tx.ExecContext(ctx, metadataQuery, claim.WalletKey, botKey, claim.Exchange, claim.Market, claim.QuoteAsset, claim.Symbol, time.Now().UTC()); err != nil {
			return fmt.Errorf("write account wallet reservation metadata: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account wallet capital reservation: %w", err)
	}
	return nil
}

func (s *SQLStorage) persistWalletBalanceObservations(ctx context.Context, claims []AccountWalletCapitalClaim) ([]AccountWalletCapitalClaim, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin wallet balance observation transaction: %w", err)
	}
	defer tx.Rollback()
	for _, claim := range claims {
		if err := lockFundingSpreadWallet(ctx, tx, s.dbType, claim.WalletKey); err != nil {
			return nil, err
		}
	}
	for index := range claims {
		if claims[index].ObservationSequence == 0 {
			sequence, err := advanceWalletObservationSequence(ctx, tx, s.dbType, claims[index].WalletKey)
			if err != nil {
				return nil, err
			}
			claims[index].ObservationSequence = sequence
		}
		var latestSequence int64
		if err := tx.QueryRowContext(ctx, `SELECT latest_sequence FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`, claims[index].WalletKey).Scan(&latestSequence); err != nil {
			return nil, fmt.Errorf("read latest wallet observation generation: %w", err)
		}
		if claims[index].ObservationSequence > latestSequence {
			return nil, fmt.Errorf("wallet %s balance observation generation was not issued", claims[index].WalletKey)
		}
		if claims[index].ObservationSequence == latestSequence {
			if _, err := recordWalletBalanceObservation(ctx, tx, claims[index]); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit wallet balance observations: %w", err)
	}
	return claims, nil
}

func readLatestWalletBalance(ctx context.Context, tx *sql.Tx, walletKey string) (float64, error) {
	var available sql.NullFloat64
	var observedAt sql.NullInt64
	var sequence int64
	err := tx.QueryRowContext(ctx, `SELECT available, observed_at_ns, latest_sequence FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`, walletKey).Scan(&available, &observedAt, &sequence)
	if err != nil {
		return 0, fmt.Errorf("read latest verified wallet balance: %w", err)
	}
	if !available.Valid || !observedAt.Valid || sequence <= 0 || math.IsNaN(available.Float64) || math.IsInf(available.Float64, 0) || available.Float64 <= 0 || observedAt.Int64 <= 0 {
		return 0, errors.New("stored verified wallet balance is invalid")
	}
	return available.Float64, nil
}

func recordWalletBalanceObservation(ctx context.Context, tx *sql.Tx, claim AccountWalletCapitalClaim) (float64, error) {
	observedAt := claim.ObservedAt.UTC().UnixNano()
	_, err := tx.ExecContext(ctx, `UPDATE funding_spread_wallet_observation_sequences
		SET available = ?, observed_at_ns = ? WHERE wallet_key = ? AND latest_sequence = ?`,
		claim.Available, observedAt, claim.WalletKey, claim.ObservationSequence)
	if err != nil {
		return 0, fmt.Errorf("persist authoritative wallet balance observation: %w", err)
	}
	var existing sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT available FROM funding_spread_wallet_balances WHERE wallet_key = ?`, claim.WalletKey).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO funding_spread_wallet_balances (wallet_key, available, observed_at_ns) VALUES (?, ?, ?)`, claim.WalletKey, claim.Available, observedAt); err != nil {
			return 0, fmt.Errorf("record initial verified wallet balance: %w", err)
		}
	} else if err != nil {
		return 0, fmt.Errorf("read compatibility wallet balance: %w", err)
	} else if _, err := tx.ExecContext(ctx, `UPDATE funding_spread_wallet_balances SET available = ?, observed_at_ns = ? WHERE wallet_key = ?`, claim.Available, observedAt, claim.WalletKey); err != nil {
		return 0, fmt.Errorf("update compatibility wallet balance: %w", err)
	}
	return claim.Available, nil
}

func advanceWalletObservationSequence(ctx context.Context, tx *sql.Tx, dialect, walletKey string) (int64, error) {
	if dialect == "mysql" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO funding_spread_wallet_observation_sequences (wallet_key, latest_sequence) VALUES (?, 0) ON DUPLICATE KEY UPDATE wallet_key=VALUES(wallet_key)`, walletKey); err != nil {
			return 0, fmt.Errorf("initialize wallet observation sequence: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO funding_spread_wallet_observation_sequences (wallet_key, latest_sequence) VALUES (?, 0)`, walletKey); err != nil {
		return 0, fmt.Errorf("initialize wallet observation sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE funding_spread_wallet_observation_sequences SET latest_sequence = latest_sequence + 1 WHERE wallet_key = ?`, walletKey); err != nil {
		return 0, fmt.Errorf("advance wallet observation sequence: %w", err)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT latest_sequence FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`, walletKey).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("read wallet observation sequence: %w", err)
	}
	return sequence, nil
}

func (s *SQLStorage) ReleaseAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error {
	if ctx == nil {
		return errors.New("account wallet reservation release requires context")
	}
	botKey := fundingSpreadBotKey(botID)
	if botKey == "" {
		return errors.New("account wallet reservation release requires Bot identity")
	}
	claims, err := normalizeFundingSpreadClaims(claims, false)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin account wallet reservation release: %w", err)
	}
	defer tx.Rollback()
	for _, claim := range claims {
		if err := lockFundingSpreadWallet(ctx, tx, s.dbType, claim.WalletKey); err != nil {
			return err
		}
	}
	for _, claim := range claims {
		var currentToken string
		err := tx.QueryRowContext(ctx, `SELECT reservation_token FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, botKey).Scan(&currentToken)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read account wallet reservation generation before release: %w", err)
		}
		if currentToken != claim.ReservationToken {
			return fmt.Errorf("refuse to release account wallet reservation owned by a newer runtime generation")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ? AND reservation_token = ?`, claim.WalletKey, botKey, claim.ReservationToken); err != nil {
			return fmt.Errorf("release account wallet capital reservation: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM funding_spread_capital_reservation_metadata WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, botKey); err != nil {
			return fmt.Errorf("release account wallet reservation metadata: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account wallet capital reservation release: %w", err)
	}
	return nil
}

func (s *SQLStorage) ListAccountWalletCapitalReservations(ctx context.Context, afterWalletKey, afterBotKey string, limit int) ([]AccountWalletCapitalReservation, error) {
	if ctx == nil || limit < 1 || limit > AccountWalletCapitalReservationAuditPageSize ||
		(afterWalletKey == "") != (afterBotKey == "") ||
		(afterWalletKey != "" && (!isFundingSpreadDigest(afterWalletKey) || !isFundingSpreadDigest(afterBotKey))) {
		return nil, errors.New("account wallet reservation listing requires context and a bounded page")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.wallet_key, r.bot_key, r.amount, r.updated_at,
		m.exchange_name, m.market_type, m.quote_asset, m.symbol
		FROM funding_spread_capital_reservations r
		LEFT JOIN funding_spread_capital_reservation_metadata m ON m.wallet_key = r.wallet_key AND m.bot_key = r.bot_key
		WHERE (? = '' OR r.wallet_key > ? OR (r.wallet_key = ? AND r.bot_key > ?))
		ORDER BY r.wallet_key ASC, r.bot_key ASC LIMIT ?`, afterWalletKey, afterWalletKey, afterWalletKey, afterBotKey, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list account wallet capital reservations: %w", err)
	}
	defer rows.Close()
	items := make([]AccountWalletCapitalReservation, 0)
	for rows.Next() {
		var item AccountWalletCapitalReservation
		var exchangeName, market, quote, symbol sql.NullString
		if err := rows.Scan(&item.WalletKey, &item.BotKey, &item.Amount, &item.UpdatedAt, &exchangeName, &market, &quote, &symbol); err != nil {
			return nil, fmt.Errorf("scan account wallet capital reservation: %w", err)
		}
		item.Exchange, item.Market, item.QuoteAsset, item.Symbol = exchangeName.String, market.String, quote.String, symbol.String
		item.Mapped = exchangeName.Valid && market.Valid && quote.Valid && symbol.Valid
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account wallet capital reservations: %w", err)
	}
	return items, nil
}

// HasAccountWalletCapitalReservation reports whether the Bot has any wallet claim still recorded.
func (s *SQLStorage) HasAccountWalletCapitalReservation(ctx context.Context, botID string) (bool, error) {
	if ctx == nil || s == nil || strings.TrimSpace(botID) == "" {
		return false, errors.New("account wallet reservation check requires context, storage, and Bot identity")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_spread_capital_reservations WHERE bot_key = ?`, fundingSpreadBotKey(botID)).Scan(&count); err != nil {
		return false, fmt.Errorf("check account wallet reservations for Bot: %w", err)
	}
	return count > 0, nil
}

func validateCapitalReservationMetadata(claim AccountWalletCapitalClaim) error {
	values := []string{claim.Exchange, claim.Market, claim.QuoteAsset, claim.Symbol}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || len(trimmed) > 128 || strings.ContainsAny(trimmed, "\x00\r\n") {
			return errors.New("account wallet reservation metadata must contain bounded, non-empty identifiers")
		}
	}
	return nil
}

func (s *SQLStorage) ReserveFundingSpreadCapital(ctx context.Context, botID string, claims []FundingSpreadCapitalClaim) error {
	return s.ReserveAccountWalletCapital(ctx, botID, claims)
}

func (s *SQLStorage) ReleaseFundingSpreadCapital(ctx context.Context, botID string, claims []FundingSpreadCapitalClaim) error {
	return s.ReleaseAccountWalletCapital(ctx, botID, claims)
}

func lockFundingSpreadWallet(ctx context.Context, tx *sql.Tx, dbType, walletKey string) error {
	if dbType == "mysql" {
		_, err := tx.ExecContext(ctx, `INSERT INTO funding_spread_wallet_locks (wallet_key, updated_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE updated_at=VALUES(updated_at)`, walletKey, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("lock funding spread wallet %s: %w", walletKey, err)
		}
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT wallet_key FROM funding_spread_wallet_locks WHERE wallet_key = ? FOR UPDATE`, walletKey).Scan(&locked); err != nil {
			return fmt.Errorf("acquire funding spread wallet row %s: %w", walletKey, err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO funding_spread_wallet_locks (wallet_key) VALUES (?)`, walletKey); err != nil {
		return fmt.Errorf("serialize funding spread wallet %s: %w", walletKey, err)
	}
	return nil
}

func normalizeFundingSpreadClaims(claims []FundingSpreadCapitalClaim, requireAmounts bool) ([]FundingSpreadCapitalClaim, error) {
	if len(claims) == 0 {
		return nil, errors.New("funding spread reservation requires wallet claims")
	}
	result := append([]FundingSpreadCapitalClaim(nil), claims...)
	for index := range result {
		claim := &result[index]
		if !isFundingSpreadDigest(claim.WalletKey) {
			return nil, errors.New("funding spread reservation wallet key must be a SHA-256 hex digest")
		}
		if !isFundingSpreadDigest(claim.ReservationToken) {
			return nil, errors.New("funding spread reservation requires a runtime generation token")
		}
		if claim.ObservationSequence < 0 {
			return nil, errors.New("wallet balance observation sequence must not be negative")
		}
		if requireAmounts && (math.IsNaN(claim.Amount) || math.IsInf(claim.Amount, 0) || claim.Amount <= 0 || math.IsNaN(claim.Available) || math.IsInf(claim.Available, 0) || claim.Available <= 0) {
			return nil, errors.New("funding spread reservation requires positive finite amount and available balance")
		}
		if claim.ObservedAt.IsZero() {
			claim.ObservedAt = time.Now().UTC()
		}
		if claim.ObservedAt.After(time.Now().Add(time.Minute)) {
			return nil, errors.New("wallet balance observation time is unreasonably far in the future")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].WalletKey < result[j].WalletKey })
	for i := 1; i < len(result); i++ {
		if result[i-1].WalletKey == result[i].WalletKey {
			return nil, errors.New("duplicate funding spread wallet claim")
		}
	}
	return result, nil
}

func fundingSpreadBotKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func isFundingSpreadDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
