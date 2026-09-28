package main

import (
	"context"
	"fmt"
	"quantmesh/position"
)

func (br *BotRuntime) ownedCloseManager(ctx context.Context) (*position.ClosePositionManager, bool, error) {
	rt := br.Inner
	if rt == nil || rt.ExchangeExecutor == nil || rt.Exchange == nil {
		return nil, false, fmt.Errorf("owned close executor unavailable")
	}
	shutdown := rt.ExchangeExecutor.IsShutdownCloseContext(ctx)
	newManager := func() *position.ClosePositionManager {
		adapter := &exchangeExecutorAdapter{executor: rt.ExchangeExecutor, exchange: rt.Exchange.GetName()}
		return position.NewClosePositionManager(position.NewOwnedExchangeAdapterWrapper(rt.Exchange, adapter, rt.ExchangeExecutor.ObserveOrder), br.BotID, rt.Config.Symbol)
	}
	if shutdown {
		return newManager(), true, nil
	}
	rt.closeManagerMu.Lock()
	defer rt.closeManagerMu.Unlock()
	if rt.closeManager == nil {
		rt.closeManager = newManager()
	}
	return rt.closeManager, false, nil
}

func (rt *SymbolRuntime) stopManagedClose(ctx context.Context) error {
	rt.closeManagerMu.Lock()
	mgr := rt.closeManager
	rt.closeManagerMu.Unlock()
	if mgr != nil {
		return mgr.StopContext(ctx)
	}
	return nil
}
