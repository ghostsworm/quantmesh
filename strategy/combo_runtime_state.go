package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type comboChildRuntimeStateStore struct {
	store     RuntimeStateStore
	comboName string
	childName string
}

func (s comboChildRuntimeStateStore) key(strategyName string) (string, error) {
	if s.store == nil || strings.TrimSpace(s.comboName) == "" || strings.TrimSpace(s.childName) == "" || strategyName != s.childName {
		return "", fmt.Errorf("invalid combo child runtime state identity")
	}
	digest := sha256.Sum256([]byte(s.comboName))
	key := "combo:" + hex.EncodeToString(digest[:16]) + ":" + s.childName
	if len(key) > 128 {
		return "", fmt.Errorf("combo child runtime state key exceeds storage limit")
	}
	return key, nil
}

func (s comboChildRuntimeStateStore) LoadRuntimeState(strategyName string) (int, string, bool, error) {
	key, err := s.key(strategyName)
	if err != nil {
		return 0, "", false, err
	}
	return s.store.LoadRuntimeState(key)
}

func (s comboChildRuntimeStateStore) SaveRuntimeState(strategyName string, version int, payload string) error {
	key, err := s.key(strategyName)
	if err != nil {
		return err
	}
	return s.store.SaveRuntimeState(key, version, payload)
}

const comboRuntimeStateSchemaVersion = 1

type comboRuntimeState struct {
	BotID        string  `json:"bot_id"`
	StrategyName string  `json:"strategy_name"`
	Symbol       string  `json:"symbol"`
	PeakEquity   float64 `json:"peak_equity"`
}

func (s *ComboStrategy) runtimeStateIdentity() (string, string) {
	botID, symbol := "", ""
	if s.cfg != nil {
		botID = strings.TrimSpace(s.cfg.Trading.BotID)
		symbol = strings.TrimSpace(s.cfg.Trading.Symbol)
	}
	if s.strategyCfg != nil && strings.TrimSpace(s.strategyCfg.Symbol) != "" {
		symbol = strings.TrimSpace(s.strategyCfg.Symbol)
	}
	return botID, symbol
}

func (s *ComboStrategy) restoreRuntimeState() error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	version, payload, found, err := store.LoadRuntimeState(s.name)
	if err != nil {
		return fmt.Errorf("load combo runtime state: %w", err)
	}
	if !found {
		return nil
	}
	if version != comboRuntimeStateSchemaVersion {
		return fmt.Errorf("unsupported combo runtime state schema version %d", version)
	}
	var state comboRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode combo runtime state: %w", err)
	}
	botID, symbol := s.runtimeStateIdentity()
	if state.BotID == "" || state.BotID != botID || state.StrategyName != s.name || state.Symbol == "" || state.Symbol != symbol {
		return fmt.Errorf("combo runtime state identity mismatch")
	}
	if math.IsNaN(state.PeakEquity) || math.IsInf(state.PeakEquity, 0) || state.PeakEquity < 0 {
		return fmt.Errorf("combo runtime state contains invalid peak equity")
	}
	s.mu.Lock()
	s.peakEquity = state.PeakEquity
	s.runtimeStateDirty = false
	s.runtimeStateFound = true
	s.mu.Unlock()
	return nil
}

func (s *ComboStrategy) persistRuntimeState() error {
	s.mu.RLock()
	store, peak := s.runtimeStateStore, s.peakEquity
	s.mu.RUnlock()
	if store == nil {
		return fmt.Errorf("combo runtime state store is unavailable")
	}
	botID, symbol := s.runtimeStateIdentity()
	if botID == "" || symbol == "" || math.IsNaN(peak) || math.IsInf(peak, 0) || peak < 0 {
		return fmt.Errorf("combo runtime state identity or peak equity is invalid")
	}
	payload, err := json.Marshal(comboRuntimeState{BotID: botID, StrategyName: s.name, Symbol: symbol, PeakEquity: peak})
	if err != nil {
		return fmt.Errorf("encode combo runtime state: %w", err)
	}
	if err := store.SaveRuntimeState(s.name, comboRuntimeStateSchemaVersion, string(payload)); err != nil {
		return fmt.Errorf("persist combo runtime state: %w", err)
	}
	s.mu.Lock()
	if s.peakEquity == peak {
		s.runtimeStateDirty = false
	}
	s.runtimeStateFound = true
	s.mu.Unlock()
	return nil
}

func (s *ComboStrategy) reportRuntimeStateError(err error) {
	s.mu.RLock()
	handler := s.runtimeStateErrorHandler
	s.mu.RUnlock()
	if handler != nil {
		handler(err)
	}
}
