package strategy

import (
	"context"
	"testing"
)

func TestMartingaleStrategyStartStopRefreshesContext(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	s := NewMartingaleStrategy("martin", "BTCUSDT", nil, nil, nil, nil)
	oldCtx := s.ctx
	if err := s.Start(parent); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if s.ctx == nil || s.ctx == oldCtx {
		t.Fatal("expected Start to install a fresh run context")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	select {
	case <-s.ctx.Done():
	default:
		t.Fatal("expected Stop to cancel run context")
	}
}

func TestTrendFollowingStrategyStartStopRefreshesContext(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	s := NewTrendFollowingStrategy("trend", nil, nil, nil, nil)
	oldCtx := s.ctx
	if err := s.Start(parent); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if s.ctx == nil || s.ctx == oldCtx {
		t.Fatal("expected Start to install a fresh run context")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	select {
	case <-s.ctx.Done():
	default:
		t.Fatal("expected Stop to cancel run context")
	}
}
