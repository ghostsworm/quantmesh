package monitor

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"syscall"
	"time"

	"quantmesh/logger"
)

// StackDumpDir 是 goroutine 栈快照的输出目录，可在启动前覆盖。
var StackDumpDir = "logs"

// StartGoroutineDumpHandler 注册一个按需 goroutine 栈快照处理器。
//
// 用途：排查"启动后偶发 CPU 100%、Ctrl+C 重启又正常"这类 busy-loop。
// 当进程 CPU 飙满时，执行：
//
//	kill -USR1 <pid>
//
// 即可把全部 goroutine 的栈快照写入 logs/goroutine_dump_<时间戳>.txt，
// 同时在日志里打印「goroutine 总数」以及「按相同栈聚合后的 Top 榜」。
// 一个正在空转的 goroutine 会表现为：同一份栈帧出现成百上千次——
// 这就是真凶，比静态读代码靠谱得多。
//
// 该处理器只读、零侵入，不会改变任何业务行为；收到信号才工作，平时完全静默。
func StartGoroutineDumpHandler() {
	ch := make(chan os.Signal, 1)
	// SIGUSR1：写文件 + 打印聚合摘要（推荐）
	// SIGUSR2：只在日志里打印聚合摘要（不落盘，适合容器环境）
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)

	go func() {
		for sig := range ch {
			toFile := sig == syscall.SIGUSR1
			dumpGoroutines(toFile)
		}
	}()

	logger.Info("🩺 [诊断] goroutine 栈快照已就绪：CPU 异常时执行 `kill -USR1 %d` 抓现场（输出目录 %s）", os.Getpid(), StackDumpDir)
}

// dumpGoroutines 采集并输出当前所有 goroutine 栈。
func dumpGoroutines(toFile bool) {
	count := runtime.NumGoroutine()

	var buf bytes.Buffer
	// debug=2：完整栈，含每个 goroutine 的状态与调用链
	if p := pprof.Lookup("goroutine"); p != nil {
		_ = p.WriteTo(&buf, 2)
	}

	// 按"栈签名"聚合，找出重复最多的栈——busy-loop 真凶通常霸榜
	top := topStacks(buf.Bytes(), 5)
	logger.Warn("🩺 [诊断] 收到栈快照信号：当前 goroutine 总数=%d", count)
	for i, s := range top {
		logger.Warn("🩺 [诊断] Top%d ×%d 份：%s", i+1, s.count, s.head)
	}

	if !toFile {
		return
	}

	if err := os.MkdirAll(StackDumpDir, 0o755); err != nil {
		logger.Error("🩺 [诊断] 创建快照目录失败: %v", err)
		return
	}
	path := filepath.Join(StackDumpDir, fmt.Sprintf("goroutine_dump_%s.txt", time.Now().Format("2006-01-02_15-04-05")))
	header := fmt.Sprintf("# goroutine dump @ %s\n# total=%d\n\n", time.Now().Format(time.RFC3339), count)
	if err := os.WriteFile(path, append([]byte(header), buf.Bytes()...), 0o644); err != nil {
		logger.Error("🩺 [诊断] 写入快照文件失败: %v", err)
		return
	}
	logger.Warn("🩺 [诊断] 已写入 goroutine 栈快照: %s", path)
}

type stackStat struct {
	count int
	head  string
}

// topStacks 把 pprof debug=2 输出按 goroutine 分块，用"第一条非头部行"作为签名聚合，
// 返回出现次数最多的前 n 种栈。
func topStacks(raw []byte, n int) []stackStat {
	blocks := strings.Split(string(raw), "\n\n")
	sig := make(map[string]int)
	for _, b := range blocks {
		lines := strings.Split(strings.TrimSpace(b), "\n")
		if len(lines) < 2 {
			continue
		}
		// lines[0] 形如 "goroutine 17 [running]:"，含 id，不能直接当签名；
		// 取 lines[1]（最内层函数）作为聚合键，足以区分不同的循环。
		key := strings.TrimSpace(lines[1])
		if key == "" {
			continue
		}
		sig[key]++
	}

	stats := make([]stackStat, 0, len(sig))
	for k, c := range sig {
		stats = append(stats, stackStat{count: c, head: k})
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].count > stats[j].count })
	if len(stats) > n {
		stats = stats[:n]
	}
	return stats
}
