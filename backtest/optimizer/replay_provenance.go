package optimizer

import "quantmesh/backtest/replay"

// VerifiedFile is one checksum-verified market data file. The digest covers the
// bytes read from disk, so a zip is hashed as a zip.
type VerifiedFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// ReplayProvenance names the frozen dataset a replay run actually read.
type ReplayProvenance struct {
	Source       string
	Files        []VerifiedFile
	FundingFiles []VerifiedFile
	// Funding is the verified settlement series, carried without a mark price.
	Funding []replay.FundingPoint
}

// WithReplayProvenance attaches the verified file list to one optimizer. A run
// without it records no data source and keeps the previous experiment digest.
func WithReplayProvenance(p ReplayProvenance) func(*UniversalOptimizer) {
	return func(u *UniversalOptimizer) { u.provenance = &p }
}
