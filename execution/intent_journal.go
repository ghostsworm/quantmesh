package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrIntentJournalConflict = errors.New("intent journal revision conflict")

// IntentScope uses a non-secret full account identity, never an API-key prefix.
type IntentScope struct {
	Account  string
	Exchange string
	Market   string
	Symbol   string
	Bot      string
}

func (s IntentScope) Key() (string, error) {
	for _, v := range []string{s.Account, s.Exchange, s.Market, s.Symbol, s.Bot} {
		if strings.TrimSpace(v) == "" || len(v) > 191 {
			return "", fmt.Errorf("incomplete intent owner scope")
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

type IntentJournalRecord struct {
	ID            int64
	ClientOrderID string
	Revision      int64
	Payload       []byte
}

// Writes are synchronous CAS operations. Load uses a stable ascending keyset,
// not status-limited pages: terminal orders may still need economic settlement.
type IntentJournal interface {
	SaveExecutionIntent(context.Context, string, string, int64, []byte) error
	LoadExecutionIntents(context.Context, string, int64, int) ([]IntentJournalRecord, error)
}
