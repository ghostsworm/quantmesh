package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quantmesh/storage"
)

// saveBotStateToFile serializes every owner sharing this fallback document.
// Readers see a complete old or new file, never a truncated in-place write.
func (bm *BotManager) saveBotStateToFile(state *storage.BotState) error {
	if state == nil || state.BotID == "" {
		return fmt.Errorf("fallback Bot state requires identity")
	}
	path := bm.botStatesFilePath()
	unlock, err := lockBotStatePath(context.Background(), path)
	if err != nil {
		return err
	}
	defer unlock()
	states := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &states); err != nil {
			return fmt.Errorf("parse fallback Bot states before update: %w", err)
		}
		if states == nil {
			return fmt.Errorf("fallback Bot states must be a JSON object")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read fallback Bot states before update: %w", err)
	}
	fields := make(map[string]json.RawMessage)
	if existing, ok := states[state.BotID]; ok {
		if err := json.Unmarshal(existing, &fields); err != nil || fields == nil {
			return fmt.Errorf("invalid existing fallback Bot state")
		}
	}
	known := map[string]any{
		"enabled": state.Enabled, "updated_at": state.UpdatedAt.Format(time.RFC3339),
		"updated_by": state.UpdatedBy, "reason": state.Reason,
	}
	for key, value := range known {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[key] = encoded
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	states[state.BotID] = encoded
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	return publishBotStateFile(path, data)
}

func publishBotStateFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".bot-states-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncStopJournalDirectory(path)
}
