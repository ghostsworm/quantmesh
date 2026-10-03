package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// AccountScopeID preserves the runtime's existing opaque account identity.
// Never log its input or use it as authentication/account-balance evidence.
func AccountScopeID(name string, cfg ExchangeConfig) string {
	identity, _ := json.Marshal([]interface{}{name, cfg.Testnet, cfg.APIKey})
	return fmt.Sprintf("%x", sha256.Sum256(identity))
}
