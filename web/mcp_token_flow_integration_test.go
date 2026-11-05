package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"quantmesh/config"
	"quantmesh/mcp"
)

const (
	mcpFlowLegacyToken = "mcp-flow-legacy-fixture"
	mcpFlowReadTool    = "fixture_read"
	mcpFlowWriteToolA  = "fixture_write_a"
	mcpFlowWriteToolB  = "fixture_write_b"
	mcpFlowFailedTool  = "fixture_write_failed"
	mcpFlowNotifyWait  = 2 * time.Second
	mcpFlowRubyTimeout = 30 * time.Second
)

// Only fake tools are registered. The real issue/revoke/resolver/observer path
// runs against memory, and the notification sink cannot reach any network.
func TestMCPTokenClientFlow(t *testing.T) {
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		t.Fatal("MCP HTTP acceptance requires Ruby with Minitest available")
	}
	script, err := filepath.Abs("../scripts/tests/mcp_token_flow_test.rb")
	if err != nil {
		t.Fatal("cannot resolve MCP acceptance script")
	}
	for _, scenario := range []struct {
		name       string
		legacy     bool
		allowWrite bool
	}{
		{name: "no_legacy"},
		{name: "legacy_write", legacy: true, allowWrite: true},
		{name: "legacy_read", legacy: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			provider := &mcpTokenCheckSettingsProvider{enabled: true, allowWrite: scenario.allowWrite}
			if scenario.legacy {
				provider.token = mcpFlowLegacyToken
			}
			server := newMCPFlowFixture(t, provider)
			reports := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), mcpFlowRubyTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, ruby, script, server.URL, reports, scenario.name)
			// The Ruby script is a test dependency: read it while the request is
			// still PENDING so Go's cached-exec logic keys on its contents too.
			if _, err := os.ReadFile(script); err != nil {
				t.Fatalf("MCP acceptance script is unreadable at runtime: %v", err)
			}
			output, runErr := cmd.CombinedOutput()
			// Never forward subprocess output: HTTP failures can embed a secret URL.
			if strings.Contains(string(output), mcpTokenPrefix) || strings.Contains(string(output), "?token=") {
				t.Error("Ruby output contained credential material (output suppressed)")
			}
			if ctx.Err() != nil {
				t.Fatal("Ruby MCP acceptance exceeded its 30-second deadline")
			}
			var report struct {
				Scenario string `json:"scenario"`
				Cases    []struct {
					Status     string   `json:"status"`
					Assertions int      `json:"assertions"`
					Stage      string   `json:"stage"`
					Check      string   `json:"check"`
					Failures   []string `json:"failure_types"`
				} `json:"cases"`
			}
			for _, filename := range []string{"results.json", "results.md"} {
				body, readErr := os.ReadFile(filepath.Join(reports, filename))
				if readErr != nil || len(body) == 0 {
					t.Fatalf("Ruby acceptance did not produce %s (process_failed=%v)", filename, runErr != nil)
				}
				if strings.Contains(string(body), mcpTokenPrefix) || strings.Contains(string(body), "?token=") {
					t.Fatalf("%s contained credential material (contents suppressed)", filename)
				}
				if filename == "results.json" && json.Unmarshal(body, &report) != nil {
					t.Fatal("Ruby acceptance JSON report is invalid")
				}
			}
			if report.Scenario != scenario.name || len(report.Cases) != 1 {
				t.Fatal("Ruby acceptance report has an unexpected scenario or case count")
			}
			result := report.Cases[0]
			if runErr != nil || result.Status != "passed" || result.Assertions == 0 {
				// Only fixed check names and exception classes; never an HTTP body.
				t.Fatalf("Ruby MCP acceptance failed: stage=%q check=%q failures=%v assertions=%d process_failed=%v",
					result.Stage, result.Check, result.Failures, result.Assertions, runErr != nil)
			}
			t.Logf("Ruby HTTP acceptance passed: %d assertions; JSON and Markdown reports verified", result.Assertions)
		})
	}
}

type mcpFlowNotice struct {
	RouteOK    bool `json:"route_ok"`
	HasWriteA  bool `json:"has_write_a"`
	HasWriteB  bool `json:"has_write_b"`
	HasRead    bool `json:"has_read"`
	HasFailed  bool `json:"has_failed"`
	SecretSafe bool `json:"secret_safe"`
}

type mcpFlowState struct {
	mu               sync.Mutex
	Reads            int             `json:"reads"`
	Writes           int             `json:"writes"`
	ExpectedWrites   int             `json:"expected_writes"`
	FailedWrites     int             `json:"failed_writes"`
	Callbacks        int             `json:"callbacks"`
	ObserverTimeout  bool            `json:"observer_timeout"`
	ObserverExtra    bool            `json:"observer_extra"`
	ObserverWait     int             `json:"observer_wait"`
	ObserverSettled  bool            `json:"observer_settled"`
	ObserverWaitBase int             `json:"observer_wait_base"`
	Notices          []mcpFlowNotice `json:"notices"`
}

func (s *mcpFlowState) writeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Writes
}

// waitForWriteObserved maps the Ruby turn (an HTTP POST body with the labeled
// turn sequence) so a failed-turn observer fires regardless of the order in
// which Ruby asserts counts.
func (s *mcpFlowState) waitForWriteObserved(t *testing.T, base int, observed <-chan struct{}, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-observed:
	case <-timer.C:
		s.mu.Lock()
		s.ObserverTimeout = true
		s.mu.Unlock()
	}
}

func newMCPFlowFixture(t *testing.T, provider *mcpTokenCheckSettingsProvider) *httptest.Server {
	t.Helper()
	originalSettings, originalNotifier, originalConfig := systemSettingsProvider, mcpNotifier, mcpConfigReader
	originalMode := gin.Mode()
	mcpTokenCacheMu.Lock()
	originalCache := mcpTokenCache
	mcpTokenCache = map[string]mcp.Access{}
	mcpTokenCacheMu.Unlock()
	systemSettingsProvider = provider
	gin.SetMode(gin.TestMode)

	state := &mcpFlowState{Notices: []mcpFlowNotice{}}
	mcpConfigReader = func() *config.Config { return nil }
	mcpNotifier = func(cfg *config.Config, channel, target, title, body string) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.Notices = append(state.Notices, mcpFlowNotice{
			RouteOK:    cfg == nil && channel == mcp.NotifyEmail && target == "fixture@example.invalid" && title != "",
			HasWriteA:  strings.Contains(body, mcpFlowWriteToolA),
			HasWriteB:  strings.Contains(body, mcpFlowWriteToolB),
			HasRead:    strings.Contains(body, mcpFlowReadTool),
			HasFailed:  strings.Contains(body, mcpFlowFailedTool),
			SecretSafe: !strings.Contains(body, mcpTokenPrefix) && !strings.Contains(body, mcpFlowLegacyToken) && !strings.Contains(body, "?token="),
		})
		return nil
	}

	s := mcp.NewServer("mcp-flow-fixture", MCPResolveToken)
	for _, tool := range []struct {
		name  string
		write bool
		fail  bool
	}{
		{name: mcpFlowReadTool},
		{name: mcpFlowWriteToolA, write: true},
		{name: mcpFlowWriteToolB, write: true},
		{name: mcpFlowFailedTool, write: true, fail: true},
	} {
		s.Register(mcp.ToolEntry{
			Tool:  mcp.Tool{Name: tool.name, Description: "Isolated acceptance fixture", InputSchema: map[string]any{"type": "object"}},
			Write: tool.write,
			Handler: func(context.Context, json.RawMessage) (any, error) {
				state.mu.Lock()
				defer state.mu.Unlock()
				if tool.fail {
					state.FailedWrites++
					return nil, errors.New("fixture write rejected")
				}
				if tool.write {
					state.Writes++
				} else {
					state.Reads++
				}
				return "fixture completed", nil
			},
		})
	}

	// Closing the gate waits for entered observers and makes late callbacks
	// harmless before globals are restored, even when the Ruby process fails.
	var observerGate sync.Mutex
	closed := false
	observed := make(chan struct{})
	observedOnce := sync.Once{}
	s.SetWriteObserver(func(ctx context.Context, notice mcp.WriteNotice) {
		observerGate.Lock()
		defer observerGate.Unlock()
		if closed {
			return
		}
		NotifyMCPWrites(ctx, notice)
		state.mu.Lock()
		observerWaitBase := state.ObserverWaitBase
		count := state.Writes - observerWaitBase
		state.Callbacks++
		state.ObserverWait = count
		state.mu.Unlock()
		if count > 0 {
			observedOnce.Do(func() { close(observed) })
		}
	})

	router := gin.New() // No request logger: query credentials must stay private.
	var requests sync.Mutex
	router.Use(func(c *gin.Context) {
		// The existing settings fake is deliberately simple; serialize all HTTP
		// requests so its map and legacy fields never race across handlers.
		requests.Lock()
		defer requests.Unlock()
		before := state.writeCount()
		c.Next()
		if state.writeCount() == before {
			return
		}
		timer := time.NewTimer(mcpFlowNotifyWait)
		defer timer.Stop()
		select {
		case <-observed:
		case <-timer.C:
			state.mu.Lock()
			state.ObserverTimeout = true
			state.mu.Unlock()
		}
	})
	router.POST("/api/mcp/tokens", postMCPIssueToken)
	router.DELETE("/api/mcp/tokens/:id", deleteMCPTokenByID)
	router.GET("/api/mcp/config", getMCPConfig)
	router.GET("/__test/state", func(c *gin.Context) {
		state.mu.Lock()
		defer state.mu.Unlock()
		c.JSON(http.StatusOK, state)
	})
	s.Mount(router.Group("/"), "mcp")
	server := httptest.NewServer(router)
	t.Cleanup(func() {
		server.Close()
		observerGate.Lock()
		defer observerGate.Unlock()
		closed = true
		systemSettingsProvider, mcpNotifier, mcpConfigReader = originalSettings, originalNotifier, originalConfig
		mcpTokenCacheMu.Lock()
		mcpTokenCache = originalCache
		mcpTokenCacheMu.Unlock()
		gin.SetMode(originalMode)
	})
	return server
}
