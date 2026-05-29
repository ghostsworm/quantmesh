package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
)

// ToolHandler 工具实现签名：拿到原始 arguments，返回一段文本结果或 error。
//
// 返回 error → MCP 包成 isError=true 的文本块返回给客户端。
// 返回非空 string → 当作 text 块返回。
// 返回结构化对象 → 用 json.Marshal 转字符串。
type ToolHandler func(ctx context.Context, args json.RawMessage) (any, error)

// ToolEntry 注册表里的一项。
type ToolEntry struct {
	Tool    Tool
	Handler ToolHandler
	Write   bool // 是否是写操作；写工具需要 allowWrite=true 才注册
}

// Server 一个进程一份。Register 完所有工具后 Mount 到 gin。
type Server struct {
	mu         sync.RWMutex
	tools      map[string]ToolEntry
	tokenCheck func(token string) bool
	version    string
}

// NewServer 创建空 server。
//   - tokenCheck 收到请求的 token 是否有效；返回 false 则 401
//   - version  quantmesh 当前版本号，会出现在 initialize 响应里
func NewServer(version string, tokenCheck func(token string) bool) *Server {
	if tokenCheck == nil {
		tokenCheck = func(string) bool { return false }
	}
	return &Server{
		tools:      make(map[string]ToolEntry),
		tokenCheck: tokenCheck,
		version:    version,
	}
}

// Register 注册一个工具。重复名以最后一次为准。
func (s *Server) Register(entry ToolEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[entry.Tool.Name] = entry
}

// Mount 把 /mcp 路由挂到 gin（任意路径前缀都行）。
//
// MCP streamable HTTP 规范要求一个 single endpoint 同时接受 POST（JSON-RPC）
// 与 GET（开 SSE 接收服务端推送）。我们目前不主动推送，GET 仅返回 200 keep-alive，
// 让客户端不报错；后续要做工具变更通知再扩展。
func (s *Server) Mount(rg *gin.RouterGroup, path string) {
	rg.POST(path, s.handlePost)
	rg.GET(path, s.handleGet)
	rg.OPTIONS(path, s.handleOptions)
}

// ServeOne 给"代理 handler"用：调用方已经收到了请求，根据方法分派到 handle*。
// 用于 main 启动时序晚于路由注册的场景。
func (s *Server) ServeOne(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodPost:
		s.handlePost(c)
	case http.MethodGet:
		s.handleGet(c)
	case http.MethodOptions:
		s.handleOptions(c)
	default:
		c.Status(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleOptions(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version, X-MCP-Token")
	c.Status(http.StatusNoContent)
}

// ToolCount 给设置页显示用：当前注册了多少工具。
func (s *Server) ToolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tools)
}

func (s *Server) handleGet(c *gin.Context) {
	if !s.checkAuth(c) {
		return
	}
	// SSE 占位：不主动推，但保持连接打开 30s 防止客户端不停重连。
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.Status(http.StatusOK)
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-c.Request.Context().Done():
			return
		case <-time.After(10 * time.Second):
			_, _ = c.Writer.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) handlePost(c *gin.Context) {
	if !s.checkAuth(c) {
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.writeRPCError(c, nil, errCodeParseError, "read body failed", nil)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		s.writeRPCError(c, nil, errCodeInvalidRequest, "empty body", nil)
		return
	}

	// 可以是单个请求或批量
	if body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			s.writeRPCError(c, nil, errCodeParseError, err.Error(), nil)
			return
		}
		responses := make([]*jsonRPCResponse, 0, len(batch))
		for _, raw := range batch {
			if resp := s.dispatch(c.Request.Context(), raw); resp != nil {
				responses = append(responses, resp)
			}
		}
		if len(responses) == 0 {
			c.Status(http.StatusAccepted)
			return
		}
		c.JSON(http.StatusOK, responses)
		return
	}

	resp := s.dispatch(c.Request.Context(), body)
	if resp == nil {
		// 通知（无 id）—— 返回 202 表示已收
		c.Status(http.StatusAccepted)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// dispatch 解析单条请求并执行。通知（id 为空）返回 nil。
func (s *Server) dispatch(ctx context.Context, raw []byte) *jsonRPCResponse {
	var req jsonRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcErr(nil, errCodeParseError, err.Error(), nil)
	}
	if req.JSONRPC != "2.0" {
		return rpcErr(req.ID, errCodeInvalidRequest, "jsonrpc must be 2.0", nil)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		return nil // 通知
	case "ping":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		// 通知性 method（以 notifications/ 开头）静默吃掉
		if len(req.Method) > len("notifications/") && req.Method[:len("notifications/")] == "notifications/" {
			return nil
		}
		if len(req.ID) == 0 {
			return nil
		}
		return rpcErr(req.ID, errCodeMethodNotFound, "method not found: "+req.Method, nil)
	}
}

func (s *Server) handleInitialize(req jsonRPCRequest) *jsonRPCResponse {
	var p initializeParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &p)
	}
	result := initializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: serverCaps{
			Tools: &toolsCap{},
		},
		ServerInfo: implementation{
			Name:    ServerName,
			Version: s.version,
		},
	}
	if p.ClientInfo != nil {
		logger.Info("MCP 客户端已连接: %s/%s", p.ClientInfo.Name, p.ClientInfo.Version)
	}
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req jsonRPCRequest) *jsonRPCResponse {
	s.mu.RLock()
	tools := make([]Tool, 0, len(s.tools))
	for _, e := range s.tools {
		tools = append(tools, e.Tool)
	}
	s.mu.RUnlock()
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: toolsListResult{Tools: tools}}
}

func (s *Server) handleToolsCall(ctx context.Context, req jsonRPCRequest) *jsonRPCResponse {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcErr(req.ID, errCodeInvalidParams, err.Error(), nil)
	}
	s.mu.RLock()
	entry, ok := s.tools[p.Name]
	s.mu.RUnlock()
	if !ok {
		return rpcErr(req.ID, errCodeMethodNotFound, "tool not found: "+p.Name, nil)
	}

	// 给工具加一个 30s 超时兜底，避免某个慢工具卡死 agent
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := safeCall(callCtx, entry.Handler, p.Arguments)
	if err != nil {
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: newErrorResult(err.Error())}
	}
	text := stringify(out)
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: newTextResult(text)}
}

func safeCall(ctx context.Context, h ToolHandler, args json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tool panic: %v", r)
		}
	}()
	return h(ctx, args)
}

func stringify(v any) string {
	if v == nil {
		return "ok"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func (s *Server) checkAuth(c *gin.Context) bool {
	tok := extractToken(c.Request)
	if tok == "" {
		c.Header("WWW-Authenticate", `Bearer realm="quantmesh-mcp"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return false
	}
	if !s.tokenCheck(tok) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return false
	}
	return true
}

// extractToken 接受 Authorization: Bearer xxx / X-MCP-Token: xxx / query ?token=xxx
// 三种来源，方便不同 agent 客户端。
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
			return h[7:]
		}
		return h
	}
	if h := r.Header.Get("X-MCP-Token"); h != "" {
		return h
	}
	return r.URL.Query().Get("token")
}

func (s *Server) writeRPCError(c *gin.Context, id json.RawMessage, code int, msg string, data any) {
	c.JSON(http.StatusOK, rpcErr(id, code, msg, data))
}

func rpcErr(id json.RawMessage, code int, msg string, data any) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg, Data: data},
	}
}

// ErrNotImplemented 工具实现里用得上：明确告诉调用方某能力还没接通。
var ErrNotImplemented = errors.New("not implemented")
