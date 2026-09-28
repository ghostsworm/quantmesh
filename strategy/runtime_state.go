package strategy

// RuntimeStateStore persists versioned snapshots under a bot/strategy identity.
type RuntimeStateStore interface {
	LoadRuntimeState(strategyName string) (schemaVersion int, payload string, found bool, err error)
	SaveRuntimeState(strategyName string, schemaVersion int, payload string) error
}
