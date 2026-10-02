package strategy

import "context"

// Private recovery/accounting evidence is independent of display inventory.
// Implementations must verify memory and durable owner state without clearing
// unresolved work. An absent capability is not a flatness proof.
type CapitalReleaseStateVerifier interface {
	VerifyCapitalReleaseState(context.Context) error
}
