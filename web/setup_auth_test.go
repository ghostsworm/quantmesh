package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
)

func TestDirectLoopbackRejectsProxiesAndSpoofedHeaders(t *testing.T) {
	for _, tc := range []struct {
		peer, header, value string
		want                bool
	}{
		{"127.0.0.1:1234", "", "", true}, {"[::1]:1234", "", "", true},
		{"192.0.2.1:1234", "", "", false}, {"", "", "", false},
		{"127.0.0.1:1234", "X-Forwarded-For", "192.0.2.1", false},
		{"127.0.0.1:1234", "Forwarded", "for=192.0.2.1", false},
		{"192.0.2.1:1234", "X-Real-IP", "127.0.0.1", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.peer
		if tc.header != "" {
			req.Header.Set(tc.header, tc.value)
		}
		if got := isDirectLoopbackRequest(req); got != tc.want {
			t.Errorf("peer=%s header=%s got=%v", tc.peer, tc.header, got)
		}
	}
}

func TestInstalledSetupRequiresSessionEvenOnLoopback(t *testing.T) {
	originalPM := globalPasswordManager
	originalSM := GetSessionManager()
	t.Cleanup(func() { globalPasswordManager, globalSessionManager = originalPM, originalSM })
	pm, err := NewPasswordManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pm.db.Close() })
	if err := pm.SetPassword("admin", "unit-test-strong-password"); err != nil {
		t.Fatal(err)
	}
	globalPasswordManager = pm
	sm := &SessionManager{sessions: make(map[string]*Session), sessionTimeout: time.Hour}
	globalSessionManager = sm
	session, err := sm.CreateSession("admin", "admin", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		authenticated, missingManager bool
		want                          int
	}{
		{false, false, http.StatusUnauthorized}, {true, false, http.StatusBadRequest}, {true, true, http.StatusServiceUnavailable},
	} {
		globalSessionManager = sm
		if tc.missingManager {
			globalSessionManager = nil
		}
		req := httptest.NewRequest(http.MethodPost, "/api/setup/init", strings.NewReader("{"))
		req.RemoteAddr = "127.0.0.1:1234"
		if tc.authenticated {
			req.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
		}
		w := httptest.NewRecorder()
		setupSetupAPITestRouter().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("authenticated=%v missingManager=%v status=%d want=%d", tc.authenticated, tc.missingManager, w.Code, tc.want)
		}
	}
}

func TestLocalDevServerRejectsPublicBind(t *testing.T) {
	original := storageProvider
	SetStorageProvider(localDevSettingsProvider{})
	t.Cleanup(func() { SetStorageProvider(original) })
	for _, host := range []string{"", "0.0.0.0", "::", "192.0.2.1"} {
		cfg := &config.Config{}
		cfg.Web.Host = host
		server := &WebServer{cfg: cfg}
		// There is no HTTP server: success would start an unintended goroutine.
		if err := server.Start(context.Background()); err == nil {
			t.Errorf("accepted host %q", host)
		}
	}
	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		if !isLoopbackBindHost(host) {
			t.Errorf("rejected loopback %q", host)
		}
	}
}

func TestSetupMutationAuthenticationFailsClosed(t *testing.T) {
	originalPM := globalPasswordManager
	t.Cleanup(func() { globalPasswordManager = originalPM })
	for _, kind := range []string{"missing_manager", "database_error", "installed_database_error", "remote_first_setup"} {
		t.Run(kind, func(t *testing.T) {
			pm, err := NewPasswordManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pm.db.Close() })
			globalPasswordManager = pm
			want := http.StatusServiceUnavailable
			switch kind {
			case "missing_manager":
				globalPasswordManager = nil
			case "database_error":
				pm.db.Close()
			case "installed_database_error":
				if err := pm.createInstalledMarker(); err != nil {
					t.Fatal(err)
				}
				pm.db.Close()
			case "remote_first_setup":
				want = http.StatusUnauthorized
			}
			req := httptest.NewRequest(http.MethodPost, "/api/setup/init", strings.NewReader("{"))
			req.RemoteAddr = "192.0.2.1:1234"
			w := httptest.NewRecorder()
			setupSetupAPITestRouter().ServeHTTP(w, req)
			if w.Code != want {
				t.Fatalf("status=%d want=%d", w.Code, want)
			}
		})
	}
}
