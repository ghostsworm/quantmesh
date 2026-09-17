package main

// 进程退出收尾：把原来散落在 main 里的 defer 清理统一登记到 shutdownRunner，
// 在打印「系统已安全退出」之前按登记的逆序（与 defer 相同）逐个执行，每步限时、总时长封顶，
// 超时的步骤记录名称并把 goroutine 栈写到标准错误，然后照常继续，最后由 main 显式 os.Exit。
//
// 背景：旧实现在最终日志之后才执行 defer（K 线收集器、事件中心、分布式锁、GORM 数据库关闭等），
// 其中任何一步阻塞都会让进程在打印「系统已安全退出」之后迟迟不退出，而且没有任何日志能看出卡在哪一步；
// 同时 SIGINT/SIGTERM 已被 signal.Notify 接管，再发信号也只是写进无人读取的 channel，只能等 SIGKILL。

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"quantmesh/logger"
)

const (
	// shutdownStepTimeout 单个收尾步骤的最长等待时间
	shutdownStepTimeout = 3 * time.Second
	// shutdownCleanupBudget 全部收尾步骤的总时长上限；用完后剩余步骤不再等待（仍会启动执行）
	shutdownCleanupBudget = 8 * time.Second
	// logStorageCloseTimeout 关闭日志存储（刷新剩余日志）的最长等待时间
	logStorageCloseTimeout = 3 * time.Second
	// forceExitSignalGrace 收到第一个退出信号后，在此时间内重复到达的信号视为同一次（stop 脚本会同时发给进程组和 PID）
	forceExitSignalGrace = 2 * time.Second
	// forceExitCode 第二次退出信号强制退出时的退出码
	forceExitCode = 1
)

// shutdownStep 一个收尾步骤
type shutdownStep struct {
	name string
	fn   func()
}

// shutdownRunner 按 LIFO 顺序执行登记的收尾步骤（语义同 defer），每步限时
type shutdownRunner struct {
	mu    sync.Mutex
	steps []shutdownStep
	ran   bool

	stepTimeout time.Duration
	budget      time.Duration
	// dumpTo 超时时写 goroutine 栈的位置；nil 表示不写
	dumpTo io.Writer
	// now 便于测试替换
	now func() time.Time
}

func newShutdownRunner() *shutdownRunner {
	return &shutdownRunner{
		stepTimeout: shutdownStepTimeout,
		budget:      shutdownCleanupBudget,
		dumpTo:      os.Stderr,
		now:         time.Now,
	}
}

// Defer 登记收尾步骤；执行顺序与登记顺序相反
func (r *shutdownRunner) Defer(name string, fn func()) {
	if r == nil || fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, shutdownStep{name: name, fn: fn})
}

// Run 执行全部收尾步骤（只执行一次），返回超时未完成的步骤名称
func (r *shutdownRunner) Run() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.ran {
		r.mu.Unlock()
		return nil
	}
	r.ran = true
	steps := make([]shutdownStep, len(r.steps))
	copy(steps, r.steps)
	r.mu.Unlock()

	deadline := r.now().Add(r.budget)
	var timedOut []string
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		wait := r.stepTimeout
		if remaining := deadline.Sub(r.now()); remaining < wait {
			wait = remaining
		}
		if wait < 0 {
			wait = 0
		}
		start := r.now()
		if !runWithTimeout(step.fn, wait) {
			timedOut = append(timedOut, step.name)
			logger.Warn("⚠️ 退出收尾步骤「%s」%v 内未完成，跳过等待继续退出", step.name, wait)
			r.dumpGoroutines(fmt.Sprintf("收尾步骤「%s」超时", step.name))
			continue
		}
		if elapsed := r.now().Sub(start); elapsed > time.Second {
			logger.Info("⏱️ 退出收尾步骤「%s」耗时 %v", step.name, elapsed)
		}
	}
	return timedOut
}

func (r *shutdownRunner) dumpGoroutines(reason string) {
	if r.dumpTo == nil {
		return
	}
	writeGoroutineDump(r.dumpTo, reason)
}

// runWithTimeout 在独立 goroutine 中执行 fn，timeout 内完成返回 true。
// fn 内的 panic 会被恢复并视为已完成，避免收尾阶段的 panic 让进程以非零码崩溃且丢失后续清理。
func runWithTimeout(fn func(), timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("❌ 退出收尾步骤 panic: %v", rec)
			}
		}()
		fn()
	}()
	if timeout <= 0 {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// writeGoroutineDump 把全部 goroutine 栈写到 w，用于定位退出阶段卡住的位置
func writeGoroutineDump(w io.Writer, reason string) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "===== goroutine dump: %s =====\n", reason)
	if p := pprof.Lookup("goroutine"); p != nil {
		_ = p.WriteTo(w, 2)
	}
	fmt.Fprintf(w, "===== end goroutine dump =====\n")
}

// watchForceExitSignal 在优雅关闭期间监听后续退出信号：
// 距第一个信号超过 grace 后再次收到 SIGINT/SIGTERM，说明调用方（stop 脚本、systemd、Ctrl+C 两次）已不愿再等，
// 写出 goroutine 栈后立即以 exitFn(forceExitCode) 退出。grace 内的重复信号忽略（stop 脚本会同时发给进程组和 PID）。
func watchForceExitSignal(sigCh <-chan os.Signal, first time.Time, grace time.Duration, now func() time.Time, dumpTo io.Writer, exitFn func(int)) {
	for sig := range sigCh {
		if now().Sub(first) < grace {
			continue
		}
		logger.Warn("⚠️ 优雅关闭期间再次收到 %v，强制退出", sig)
		writeGoroutineDump(dumpTo, fmt.Sprintf("强制退出（%v）", sig))
		exitFn(forceExitCode)
		return
	}
}

// closeLogStorageWithTimeout 关闭日志存储（刷新队列中剩余日志后关闭数据库），超时则放弃等待
func closeLogStorageWithTimeout(closeFn func() error, timeout time.Duration) error {
	if closeFn == nil {
		return nil
	}
	var err error
	if !runWithTimeout(func() { err = closeFn() }, timeout) {
		return fmt.Errorf("关闭日志存储超过 %v", timeout)
	}
	return err
}
