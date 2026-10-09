package main

import (
	"context"

	"quantmesh/storage"
)

// Private constructor dependency boundary for SQL post-commit fault fixtures.
// Production uses the original store; flatness and ownership guards are always
// executed by verifyAndReleaseAccountWalletCapitalGuarded before this call.
type fundingCarryCapitalReleaseStore struct {
	storage.AccountWalletCapitalReservationStore
	release func(context.Context, storage.AccountWalletCapitalReservationStore, string, []storage.AccountWalletCapitalClaim) error
}

func (s *fundingCarryCapitalReleaseStore) ReleaseAccountWalletCapital(ctx context.Context, botID string, claims []storage.AccountWalletCapitalClaim) error {
	return s.release(ctx, s.AccountWalletCapitalReservationStore, botID, claims)
}
