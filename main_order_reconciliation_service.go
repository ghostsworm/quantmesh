package main

import (
	"fmt"

	"quantmesh/storage"
	"quantmesh/web"
)

// buildProductionOrderReconciliationService accepts the storage abstraction
// exposed by StorageService but only enables the durable SQL implementation.
// Schema migration remains owned by StorageService startup.
func buildProductionOrderReconciliationService(backend storage.Storage, bots *BotManager) (*web.OrderReconciliationService, error) {
	store, ok := backend.(*storage.SQLStorage)
	if !ok || store == nil {
		return nil, fmt.Errorf("order reconciliation service: initialized SQL storage is required")
	}
	return newProductionOrderReconciliationService(store, bots)
}

// newProductionOrderReconciliationService binds the already-initialized
// durable case/audit repository and read-only runtime evidence provider.
// StorageService owns schema migration; Accounting and Gate intentionally
// remain unset until independently verified implementations exist.
func newProductionOrderReconciliationService(store *storage.SQLStorage, bots *BotManager) (*web.OrderReconciliationService, error) {
	if store == nil {
		return nil, fmt.Errorf("order reconciliation service: SQL storage is required")
	}
	if bots == nil {
		return nil, fmt.Errorf("order reconciliation service: Bot manager is required")
	}
	return &web.OrderReconciliationService{
		Repository: store,
		Evidence:   newRuntimeTrustedOrderEvidenceProvider(bots),
		// Fail closed: no strategy accounting or quarantine release bridge is
		// installed by this composition helper.
		Accounting: nil,
		Gate:       nil,
	}, nil
}
