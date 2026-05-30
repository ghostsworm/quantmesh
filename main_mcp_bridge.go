package main

import (
	"errors"
	"sync/atomic"

	"quantmesh/mcp"
)

// 全局 SymbolManager 句柄：MCP server 在 SymbolManager 还没建好时就构造了，
// 启停 bot 这类写操作需要等运行时落定后再有意义。用 atomic.Pointer 让两边
// 解耦——bridge 取的总是当前最新的引用。
var currentSymbolManager atomic.Pointer[SymbolManager]

// SetCurrentSymbolManager main 在 NewSymbolManager 之后立刻调用。
func SetCurrentSymbolManager(sm *SymbolManager) {
	currentSymbolManager.Store(sm)
}

// mcpBotController 把 SymbolManager 内的 BotManager 适配到 mcp.BotController 接口。
//
// 启停的真实语义：
//   - EnableBot → 数据库标记可启动；后续策略循环自动拉起
//   - DisableBot → 立即停止运行时（StopBotWithReason）
//
// 注意：StartBot 这种"立即拉起"操作我们故意不暴露给 MCP——agent 不掌握
// 启动需要的 BotConfig 上下文，盲启容易出事。
type mcpBotController struct{}

func (c *mcpBotController) bm() *BotManager {
	sm := currentSymbolManager.Load()
	if sm == nil {
		return nil
	}
	return sm.GetBotManager()
}

func (c *mcpBotController) ListBotIDs() []string {
	bm := c.bm()
	if bm == nil {
		return nil
	}
	rts := bm.List()
	ids := make([]string, 0, len(rts))
	for _, r := range rts {
		if r != nil {
			ids = append(ids, r.BotID)
		}
	}
	return ids
}

func (c *mcpBotController) EnableBot(botID, _ string) error {
	bm := c.bm()
	if bm == nil {
		return errors.New("bot manager 未初始化")
	}
	return bm.EnableBot(botID)
}

func (c *mcpBotController) DisableBot(botID, reason string) error {
	bm := c.bm()
	if bm == nil {
		return errors.New("bot manager 未初始化")
	}
	if reason == "" {
		reason = "通过 MCP 停用"
	}
	return bm.StopBotWithReason(botID, "mcp", reason)
}

// 编译期接口断言
var _ mcp.BotController = (*mcpBotController)(nil)

// NewMCPBotController bridge 构造器，给 main_helpers 使用。
func NewMCPBotController() mcp.BotController {
	return &mcpBotController{}
}
