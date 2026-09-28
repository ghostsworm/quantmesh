package utils

import (
	"encoding/base32"

	"github.com/google/uuid"
)

// NewCompactOrderID preserves all 128 random bits within the broker ID budget.
func NewCompactOrderID() string {
	id := uuid.New()
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:])
}
