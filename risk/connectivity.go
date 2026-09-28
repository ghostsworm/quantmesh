package risk

import (
	"fmt"
	"time"

	"quantmesh/event"
)

const legacyConnectivityKey = "legacy"

func connectivityKey(ids []string) string {
	if len(ids) > 0 && ids[0] != "" {
		return ids[0]
	}
	return legacyConnectivityKey
}

func connectivityEventKey(evt *event.Event) string {
	if id, ok := evt.Data["connection_id"].(string); ok && id != "" {
		return id
	}
	// Compatibility for emitters that already identify account and stream.
	// An unidentified legacy recovery only clears the legacy connection.
	if len(evt.Data) == 0 {
		return legacyConnectivityKey
	}
	return fmt.Sprintf("%v|%v|%v|%v|%v", evt.Data["exchange"], evt.Data["account"], evt.Data["symbol"], evt.Data["stream"], evt.Data["testnet"])
}

func (gcb *GlobalCircuitBreaker) refreshDisconnectTimeLocked() {
	gcb.lastWSDisconnect = time.Time{}
	for _, at := range gcb.wsDisconnected {
		if gcb.lastWSDisconnect.IsZero() || at.Before(gcb.lastWSDisconnect) {
			gcb.lastWSDisconnect = at
		}
	}
}

// ReportWebSocketStopped unregisters only the stopped stream. It is not evidence
// that any other stream has recovered or that authentication is healthy.
func (gcb *GlobalCircuitBreaker) ReportWebSocketStopped(connectionID string) {
	gcb.statusMu.Lock()
	defer gcb.statusMu.Unlock()
	delete(gcb.wsDisconnected, connectivityKey([]string{connectionID}))
	gcb.refreshDisconnectTimeLocked()
}
