// Package mcp 内嵌一个 Model Context Protocol 服务，按 MCP 2025-06-18 规范
// 的 streamable HTTP transport 暴露 quantmesh 内部能力给 LLM agent（Claude
// Desktop、Cursor、自研 agent 等）。
//
// 只实现规范的子集，足够 agent 调用工具：
//   - initialize / initialized
//   - tools/list / tools/call
//   - ping
//
// 不实现 resources、prompts、sampling、roots —— quantmesh 用不到。
package mcp

import (
	"encoding/json"
)

// ProtocolVersion 我们宣告的 MCP 版本。客户端不匹配时直接返回我们的版本，
// 由客户端决定是否回退/拒绝（大多数客户端是宽松的）。
const ProtocolVersion = "2025-06-18"

// ServerName 在 initialize 响应里告诉客户端我们叫什么。
const ServerName = "quantmesh"

// JSON-RPC 2.0 信封 —————————————————————————————————————————————————

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // 通知没有 id
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// 标准错误码
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603
)

// MCP 通用结构 ——————————————————————————————————————————————————————

// initializeParams 客户端 initialize 时传过来的协议+能力声明，我们目前
// 只读 protocolVersion / clientInfo，剩下原样接住即可。
type initializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ClientInfo      *implementation `json:"clientInfo,omitempty"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeResult 我们告诉客户端「能干啥」。
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    serverCaps     `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
}

type serverCaps struct {
	Tools *toolsCap `json:"tools,omitempty"`
}

type toolsCap struct {
	// listChanged 我们不支持运行期工具变更，省略即可（客户端会按 false 处理）
	ListChanged bool `json:"listChanged,omitempty"`
}

// Tool 和 ToolResult —————————————————————————————————————————————————

// Tool MCP 工具描述，inputSchema 必须是 JSON Schema 对象。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolCallResult content 是一个 block 列表（text/image/resource…），我们目前
// 只输出 text 块。isError=true 时客户端会把内容当作工具失败展示。
type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func newTextResult(text string) *toolCallResult {
	return &toolCallResult{Content: []contentBlock{{Type: "text", Text: text}}}
}

func newErrorResult(text string) *toolCallResult {
	return &toolCallResult{
		Content: []contentBlock{{Type: "text", Text: text}},
		IsError: true,
	}
}
