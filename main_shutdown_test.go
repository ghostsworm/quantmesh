package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestShutdownRunner_RunsLIFOOnce(t *testing.T) {
	r := newShutdownRunner()
	r.dumpTo = nil
	var mu sync.Mutex
	var order []string
	add := func(name string) {
		r.Defer(name, func() {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
		})
	}
	add("a")
	add("b")
	add("c")

	if timedOut := r.Run(); len(timedOut) != 0 {
		t.Fatalf("unexpected timeouts: %v", timedOut)
	}
	if got := strings.Join(order, ","); got != "c,b,a" {
		t.Fatalf("order = %q, want c,b,a", got)
	}
	if timedOut := r.Run(); len(timedOut) != 0 || len(order) != 3 {
		t.Fatalf("second Run must be a no-op, order=%v", order)
	}
}

// 回归：某个收尾步骤永久阻塞时，Run 必须在限时内返回并报告该步骤，其余步骤照常执行。
func TestShutdownRunner_BlockedStepIsBounded(t *testing.T) {
	r := newShutdownRunner()
	r.stepTimeout = 100 * time.Millisecond
	r.budget = time.Second
	var dump bytes.Buffer
	r.dumpTo = &dump

	block := make(chan struct{})
	defer close(block)
	ranAfter := make(chan struct{}, 1)

	r.Defer("最后执行", func() { ranAfter <- struct{}{} })
	r.Defer("永久阻塞", func() { <-block })
	r.Defer("panic 步骤", func() { panic("boom") })

	start := time.Now()
	timedOut := r.Run()
	elapsed := time.Since(start)

	if len(timedOut) != 1 || timedOut[0] != "永久阻塞" {
		t.Fatalf("timedOut = %v, want [永久阻塞]", timedOut)
	}
	if elapsed > time.Second {
		t.Fatalf("Run took %v, want bounded by step timeout", elapsed)
	}
	select {
	case <-ranAfter:
	default:
		t.Fatal("steps after the blocked one must still run")
	}
	if !strings.Contains(dump.String(), "goroutine dump") || !strings.Contains(dump.String(), "永久阻塞") {
		t.Fatalf("expected goroutine dump naming the blocked step, got %q", dump.String())
	}
}

func TestShutdownRunner_TotalBudget(t *testing.T) {
	r := newShutdownRunner()
	r.stepTimeout = 200 * time.Millisecond
	r.budget = 300 * time.Millisecond
	r.dumpTo = nil
	block := make(chan struct{})
	defer close(block)
	for i := 0; i < 5; i++ {
		r.Defer("阻塞", func() { <-block })
	}
	start := time.Now()
	timedOut := r.Run()
	if elapsed := time.Since(start); elapsed > 700*time.Millisecond {
		t.Fatalf("Run took %v, want capped by budget", elapsed)
	}
	if len(timedOut) != 5 {
		t.Fatalf("timedOut = %d, want 5", len(timedOut))
	}
}

func TestCloseLogStorageWithTimeout(t *testing.T) {
	if err := closeLogStorageWithTimeout(nil, time.Second); err != nil {
		t.Fatalf("nil closeFn: %v", err)
	}
	want := errors.New("x")
	if err := closeLogStorageWithTimeout(func() error { return want }, time.Second); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	err := closeLogStorageWithTimeout(func() error { <-block; return nil }, 50*time.Millisecond)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("expected timeout error quickly, err=%v", err)
	}
}

func TestWatchForceExitSignal(t *testing.T) {
	first := time.Unix(1000, 0)
	var now time.Time
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	setNow := func(v time.Time) {
		mu.Lock()
		defer mu.Unlock()
		now = v
	}

	sigCh := make(chan os.Signal, 4)
	exited := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchForceExitSignal(sigCh, first, 2*time.Second, clock, nil, func(code int) { exited <- code })
	}()

	// 宽限期内的重复信号（stop 脚本同时发给进程组与 PID）被忽略
	setNow(first.Add(500 * time.Millisecond))
	sigCh <- syscall.SIGINT
	select {
	case <-exited:
		t.Fatal("signal within grace must be ignored")
	case <-time.After(100 * time.Millisecond):
	}

	// 宽限期之后再次收到信号：强制退出
	setNow(first.Add(3 * time.Second))
	sigCh <- syscall.SIGTERM
	select {
	case code := <-exited:
		if code != forceExitCode {
			t.Fatalf("exit code = %d, want %d", code, forceExitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("expected force exit")
	}
	<-done
}
