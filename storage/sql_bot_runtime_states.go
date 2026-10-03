package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// BotStrategyRuntimeStateContextLister reads every persisted strategy identity
// for one Bot, including strategies no longer present in its configuration.
// A non-nil empty slice with nil error means a confirmed absence of records.
// On any failure, no partial result may be used as complete safety evidence.
type BotStrategyRuntimeStateContextLister interface {
	ListBotStrategyRuntimeStatesContext(context.Context, string) ([]*StrategyRuntimeState, error)
}

var _ BotStrategyRuntimeStateContextLister = (*SQLStorage)(nil)

func (s *SQLStorage) ListBotStrategyRuntimeStatesContext(ctx context.Context, botID string) (states []*StrategyRuntimeState, readErr error) {
	if ctx == nil || s == nil || s.db == nil {
		return nil, fmt.Errorf("Bot strategy runtime state list requires context and SQL storage")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, fmt.Errorf("Bot strategy runtime state list requires Bot identity")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bot_id, strategy_name, schema_version, payload, updated_at
		FROM strategy_runtime_states WHERE bot_id = ? ORDER BY strategy_name`, botID)
	if err != nil {
		return nil, fmt.Errorf("list Bot strategy runtime states: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			states = nil
			readErr = errors.Join(readErr, fmt.Errorf("close Bot strategy runtime state rows: %w", err))
		}
	}()
	states = make([]*StrategyRuntimeState, 0)
	for rows.Next() {
		state := &StrategyRuntimeState{}
		if err := rows.Scan(&state.BotID, &state.StrategyName, &state.SchemaVersion, &state.Payload, &state.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan Bot strategy runtime state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Bot strategy runtime states: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return states, nil
}
