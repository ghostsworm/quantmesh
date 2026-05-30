package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 这是一个极简端到端 smoke：注册一个假工具 → 验证 initialize / tools/list /
// tools/call 三步流的 happy path 与鉴权。覆盖不到所有 corner case，但能在
// 重构时第一时间发现协议级回归。
func TestMCPServer_Smoke(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const tok = "tok-abc"
	s := NewServer("test-version", func(in string) bool { return in == tok })
	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "echo",
			Description: "回显输入字符串",
			InputSchema: schemaObject(map[string]any{
				"msg": schemaString("内容"),
			}, "msg"),
		},
		Handler: func(_ context.Context, args json.RawMessage) (any, error) {
			var p struct {
				Msg string `json:"msg"`
			}
			_ = json.Unmarshal(args, &p)
			return "echo:" + p.Msg, nil
		},
	})

	r := gin.New()
	g := r.Group("/")
	s.Mount(g, "mcp")

	// 1. 没有 token 应 401
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}

	// 2. initialize
	req = httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test","version":"1"}}}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var initResp jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("initialize unmarshal: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error: %+v", initResp.Error)
	}

	// 3. tools/list
	req = httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"name":"echo"`)) {
		t.Fatalf("tools/list missing echo: %s", w.Body.String())
	}

	// 4. tools/call
	req = httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hi"}}}`)))
	req.Header.Set("X-MCP-Token", tok) // 用另一种 token 来源
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/call: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`echo:hi`)) {
		t.Fatalf("tools/call result missing echo:hi: %s", w.Body.String())
	}

	// 5. 未知工具应返回 isError
	req = httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unknown tool: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"code":-32601`)) {
		t.Fatalf("expected MethodNotFound for unknown tool: %s", w.Body.String())
	}
}
