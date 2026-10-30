package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWriteAccessIsEnforcedPerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const readToken = "read-token"
	const writeToken = "write-token"
	const legacyToken = "legacy-token"
	s := NewServer("test", func(in string) (Access, bool) {
		switch in {
		case readToken:
			return Access{TokenID: "read", Name: "只读", Mode: AccessReadOnly}, true
		case writeToken:
			return Access{
				TokenID: "write", Name: "读写", Mode: AccessReadWrite, AllowWrite: true,
				Notify: &NotifySpec{Channel: NotifyEmail, Target: "ops@example.com"},
			}, true
		case legacyToken:
			return Access{TokenID: "legacy", Mode: AccessReadWrite, AllowWrite: true, Legacy: true}, true
		default:
			return Access{}, false
		}
	})
	s.Register(ToolEntry{
		Tool:  Tool{Name: "qm_bot_set_enabled", Description: "写", InputSchema: emptyObjectSchema()},
		Write: true,
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	})

	var mu sync.Mutex
	var notices []WriteNotice
	done := make(chan struct{}, 2)
	s.SetWriteObserver(func(_ context.Context, notice WriteNotice) {
		mu.Lock()
		notices = append(notices, notice)
		mu.Unlock()
		done <- struct{}{}
	})

	r := gin.New()
	s.Mount(r.Group("/"), "mcp")
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"qm_bot_set_enabled","arguments":{}}}`)

	read := postMCP(t, r, readToken, body)
	if !bytes.Contains(read, []byte(ErrTextReadOnly)) {
		t.Fatalf("只读令牌没有被拒绝: %s", read)
	}

	write := postMCP(t, r, writeToken, body)
	if bytes.Contains(write, []byte(`"isError":true`)) {
		t.Fatalf("读写令牌没有执行成功: %s", write)
	}
	legacy := postMCP(t, r, legacyToken, body)
	if bytes.Contains(legacy, []byte(`"isError":true`)) {
		t.Fatalf("旧令牌没有保持可写: %s", legacy)
	}

	waitNotices(t, done, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(notices) != 2 || len(notices[0].Calls) != 1 || notices[0].Calls[0].Tool != "qm_bot_set_enabled" {
		t.Fatalf("写操作通知不完整: %+v", notices)
	}
	text := notices[0].Text()
	if strings.Contains(text, writeToken) || strings.Contains(text, readToken) {
		t.Fatalf("通知泄露了令牌密钥: %s", text)
	}
}

func TestWriteTokenWithoutNoticeIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := NewServer("test", func(string) (Access, bool) {
		return Access{Mode: AccessReadWrite, AllowWrite: true}, true
	})
	s.Register(ToolEntry{
		Tool:    Tool{Name: "qm_set_setting", InputSchema: emptyObjectSchema()},
		Write:   true,
		Handler: func(context.Context, json.RawMessage) (any, error) { return "changed", nil },
	})
	r := gin.New()
	s.Mount(r.Group("/"), "mcp")

	body := postMCP(t, r, "token", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"qm_set_setting","arguments":{}}}`))
	if !bytes.Contains(body, []byte(ErrTextNotifyRequired)) || bytes.Contains(body, []byte("changed")) {
		t.Fatalf("缺少通知配置的读写令牌仍执行了写操作: %s", body)
	}
}

func postMCP(t *testing.T, h http.Handler, token string, body []byte) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func waitNotices(t *testing.T, done <-chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("等待第 %d 条写操作通知超时", i+1)
		}
	}
}
