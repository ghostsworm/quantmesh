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
	Amount    float64
	Available float64
}

// FundingSpreadCapitalClaim remains a source-compatible name for older callers.
type FundingSpreadCapitalClaim = AccountWalletCapitalClaim

// AccountWalletCapitalReservationStore provides atomic multi-wallet claims.
type AccountWalletCapitalReservationStore interface {
	ReserveAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error
	ReleaseAccountWalletCapital(ctx context.Context, botID string, claims []AccountWalletCapitalClaim) error
}

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

//go:embed migrations/2026093001_funding_spread_capital_*.sql
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
	name := "migrations/2026093001_funding_spread_capital_" + dialect + ".up.sql"
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
	return nil
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

	type write struct {
		walletKey string
		amount    float64
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
		if !current.Valid || amount > current.Float64 {
			var others float64
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount), 0) FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key <> ?`, claim.WalletKey, botKey).Scan(&others); err != nil {
				return fmt.Errorf("sum existing account wallet reservations: %w", err)
			}
			if math.IsNaN(others) || math.IsInf(others, 0) || others < 0 || others > math.MaxFloat64-amount || others+amount > claim.Available {
				return fmt.Errorf("wallet %s cannot safely reserve %.12g quote units; other reservations %.12g, verified available %.12g", claim.WalletKey, amount, others, claim.Available)
			}
		}
		writes = append(writes, write{walletKey: claim.WalletKey, amount: amount})
	}

	query := `INSERT INTO funding_spread_capital_reservations (wallet_key, bot_key, amount, updated_at)
		VALUES (?, ?, ?, ?) ON CONFLICT(wallet_key, bot_key) DO UPDATE SET amount=excluded.amount, updated_at=excluded.updated_at`
	if s.dbType == "mysql" {
		query = `INSERT INTO funding_spread_capital_reservations (wallet_key, bot_key, amount, updated_at)
			VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE amount=VALUES(amount), updated_at=VALUES(updated_at)`
	}
	for _, item := range writes {
		if _, err := tx.ExecContext(ctx, query, item.walletKey, botKey, item.amount, time.Now().UTC()); err != nil {
			return fmt.Errorf("write account wallet capital reservation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account wallet capital reservation: %w", err)
	}
	return nil
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
		if _, err := tx.ExecContext(ctx, `DELETE FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, botKey); err != nil {
			return fmt.Errorf("release account wallet capital reservation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account wallet capital reservation release: %w", err)
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
	for _, claim := range result {
		if !isFundingSpreadDigest(claim.WalletKey) {
			return nil, errors.New("funding spread reservation wallet key must be a SHA-256 hex digest")
		}
		if requireAmounts && (math.IsNaN(claim.Amount) || math.IsInf(claim.Amount, 0) || claim.Amount <= 0 || math.IsNaN(claim.Available) || math.IsInf(claim.Available, 0) || claim.Available <= 0) {
			return nil, errors.New("funding spread reservation requires positive finite amount and available balance")
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
