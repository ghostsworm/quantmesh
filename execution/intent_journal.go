package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

// IntentJournalIdentityProvider optionally exposes a stable identity for an
// intent journal binding. Implementations must not include credentials.
// Journals that do not implement it are accepted only when their dynamic value
// has a stable pointer identity for the lifetime of the executor binding.
type IntentJournalIdentityProvider interface {
	IntentJournalIdentity() (string, error)
}

// IntentJournalRecordReader provides an exact owner-scoped readback for a CAS
// write whose acknowledgement may have been lost.
type IntentJournalRecordReader interface {
	LoadExecutionIntent(context.Context, string, string) (IntentJournalRecord, bool, error)
}

// StableIntentJournalIdentity returns an explicit backend identity when
// available, otherwise a conservative in-process identity for pointer-backed
// implementations. A single embedded journal delegate inherits its backend
// identity so transparent decorators do not appear to change the store.
func StableIntentJournalIdentity(journal IntentJournal) (string, error) {
	return stableIntentJournalIdentity(journal, make(map[string]bool), 0)
}

func stableIntentJournalIdentity(journal IntentJournal, visiting map[string]bool, depth int) (string, error) {
	if journal == nil {
		return "", errors.New("intent journal is unavailable")
	}
	if depth > 16 {
		return "", errors.New("intent journal wrapper nesting is too deep")
	}
	if provider, ok := journal.(IntentJournalIdentityProvider); ok {
		identity, err := provider.IntentJournalIdentity()
		if err != nil {
			return "", err
		}
		identity = strings.TrimSpace(identity)
		if identity == "" {
			return "", errors.New("intent journal returned an empty backend identity")
		}
		return "provider:" + identity, nil
	}

	value := reflect.ValueOf(journal)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return "", fmt.Errorf("intent journal %T has no stable backend identity", journal)
	}
	pointerIdentity := fmt.Sprintf("pointer:%T:%x", journal, value.Pointer())
	if visiting[pointerIdentity] {
		return "", errors.New("cyclic intent journal wrapper")
	}
	visiting[pointerIdentity] = true
	defer delete(visiting, pointerIdentity)

	elem := value.Elem()
	if elem.Kind() == reflect.Struct {
		var delegate IntentJournal
		for index := 0; index < elem.NumField(); index++ {
			field := elem.Field(index)
			if !field.CanInterface() || !field.Type().Implements(reflect.TypeOf((*IntentJournal)(nil)).Elem()) {
				continue
			}
			candidate, ok := field.Interface().(IntentJournal)
			if !ok || candidate == nil {
				continue
			}
			if delegate != nil {
				return "", fmt.Errorf("intent journal %T has multiple embedded journal delegates", journal)
			}
			delegate = candidate
		}
		if delegate != nil {
			return stableIntentJournalIdentity(delegate, visiting, depth+1)
		}
	}
	return pointerIdentity, nil
}
