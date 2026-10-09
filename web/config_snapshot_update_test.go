package web

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestGlobalSaveSnapshotCannotOverwriteInterveningBotMutation(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	baseline, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(baseline)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Name = "older-request-name"
	newer, err := cloneConfigSnapshot(baseline)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(newer, id).SmartOrder.MaxOpenOrders = 7
	if err := fileConfigManager.UpdateConfig(newer); err != nil {
		t.Fatal(err)
	}
	err = fileConfigManager.updateConfigFromSnapshot(context.Background(), baseline, next, "fixture_snapshot")
	current, readErr := GetLatestConfig()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, errConfigSnapshotChanged) || !reflect.DeepEqual(current, newer) {
		t.Fatal("older global save erased the intervening Bot mutation")
	}
	durable, readErr := loadConfigFromPrimaryDB()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if durable == nil || botCfgByID(durable, id).SmartOrder.MaxOpenOrders != 7 {
		t.Fatal("durable Bot mutation not preserved")
	}
}

func TestGlobalSaveSnapshotConcurrentSameBaselineHasOneWinner(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	baseline, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"request-a", "request-b"} {
		next, err := cloneConfigSnapshot(baseline)
		if err != nil {
			t.Fatal(err)
		}
		botCfgByID(next, id).Name = name
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- fileConfigManager.updateConfigFromSnapshot(context.Background(), baseline, next, "fixture_snapshot")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, errConfigSnapshotChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("overlapping saves not serialized against baseline: winners=%d conflicts=%d", winners, conflicts)
	}
}

func TestGlobalSaveSnapshotCancellationDoesNotPersist(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	baseline, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(baseline)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Name = "cancelled-global-write"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = fileConfigManager.updateConfigFromSnapshot(ctx, baseline, next, "fixture_snapshot")
	current, readErr := GetLatestConfig()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(current, baseline) {
		t.Fatal("cancelled global request persisted configuration")
	}
}

func TestGlobalSaveSnapshotNormalWriteAndInputIsolation(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	baseline, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(baseline)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Name = "fresh-global-write"
	if err := fileConfigManager.updateConfigFromSnapshot(context.Background(), baseline, next, "fixture_snapshot"); err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).SmartOrder.MaxOpenOrders = 999
	current, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if botCfgByID(current, id).Name != "fresh-global-write" || botCfgByID(current, id).SmartOrder.MaxOpenOrders == 999 {
		t.Fatal("normal update removed or retained mutable request reference")
	}
}
