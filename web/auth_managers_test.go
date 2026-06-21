package web

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("open session db: %v", err)
	}
	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		sessionTimeout: time.Hour,
		db:             db,
	}
	if err := sm.initDatabase(); err != nil {
		t.Fatalf("init session db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return sm
}

func TestSessionManagerPersistenceCookiesAndDeletion(t *testing.T) {
	sm := newTestSessionManager(t)

	session, err := sm.CreateSession("alice", "admin", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(session.SessionID) < 32 || strings.Contains(session.SessionID, "=") {
		t.Fatalf("unexpected session id: %q", session.SessionID)
	}

	loaded, ok := sm.GetSession(session.SessionID)
	if !ok || loaded.Username != "alice" || loaded.Role != "admin" {
		t.Fatalf("memory session not loaded: %#v ok=%v", loaded, ok)
	}

	delete(sm.sessions, session.SessionID)
	loaded, ok = sm.GetSession(session.SessionID)
	if !ok || loaded.UserAgent != "test-agent" {
		t.Fatalf("db-backed session not loaded: %#v ok=%v", loaded, ok)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	if fromReq, ok := sm.GetSessionFromRequest(req); !ok || fromReq.SessionID != session.SessionID {
		t.Fatalf("request session = %#v ok=%v", fromReq, ok)
	}
	if _, ok := sm.GetSessionFromRequest(httptest.NewRequest(http.MethodGet, "/", nil)); ok {
		t.Fatalf("request without cookie should not yield session")
	}

	w := httptest.NewRecorder()
	sm.SetSessionCookie(w, session.SessionID, true)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session_id" || cookies[0].Secure {
		t.Fatalf("unexpected session cookie: %#v", cookies)
	}

	w = httptest.NewRecorder()
	sm.ClearSessionCookie(w)
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("unexpected clear cookie: %#v", cookies)
	}

	second, err := sm.CreateSession("alice", "user", "10.0.0.1", "other")
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	sm.DeleteSessionsForUser("alice")
	if _, ok := sm.GetSession(session.SessionID); ok {
		t.Fatalf("first user session should be deleted")
	}
	if _, ok := sm.GetSession(second.SessionID); ok {
		t.Fatalf("second user session should be deleted")
	}

	expired := &Session{SessionID: "expired", Username: "bob", Role: "user", CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour)}
	sm.sessions[expired.SessionID] = expired
	if err := sm.saveSessionToDB(expired); err != nil {
		t.Fatalf("save expired session: %v", err)
	}
	if _, ok := sm.GetSession(expired.SessionID); ok {
		t.Fatalf("expired session should be rejected")
	}
	if err := sm.deleteSessionsFromDB(nil); err != nil {
		t.Fatalf("empty delete should be no-op: %v", err)
	}
}

func TestPasswordManagerInstallRecoveryAndSecurityStates(t *testing.T) {
	pm, err := NewPasswordManager(t.TempDir())
	if err != nil {
		t.Fatalf("new password manager: %v", err)
	}
	defer pm.Close()

	if pm.IsInstalled() {
		t.Fatalf("fresh manager should not be installed")
	}
	compromised, err := pm.IsSecurityCompromised()
	if err != nil || compromised {
		t.Fatalf("fresh security compromised=%v err=%v", compromised, err)
	}
	hasPassword, err := pm.HasPassword("admin")
	if err != nil || hasPassword {
		t.Fatalf("fresh has password=%v err=%v", hasPassword, err)
	}

	if err := pm.SetPassword("admin", "old-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if !pm.IsInstalled() {
		t.Fatalf("set password should create installed marker")
	}
	if ok, err := pm.VerifyPassword("admin", "old-password"); err != nil || !ok {
		t.Fatalf("verify password ok=%v err=%v", ok, err)
	}
	if ok, err := pm.VerifyPassword("admin", "bad-password"); err != nil || ok {
		t.Fatalf("bad password ok=%v err=%v", ok, err)
	}
	if ok, err := pm.VerifyPassword("missing", "anything"); err != nil || ok {
		t.Fatalf("missing password ok=%v err=%v", ok, err)
	}

	code, err := pm.GenerateRecoveryCode("admin")
	if err != nil {
		t.Fatalf("generate recovery code: %v", err)
	}
	if !strings.HasPrefix(code, "QMREC-") {
		t.Fatalf("unexpected recovery code format: %q", code)
	}
	if normalizeRecoveryCode(strings.ToLower(strings.ReplaceAll(code, "-", " - "))) != code {
		t.Fatalf("normalize recovery code failed")
	}
	if err := pm.RecoverPasswordWithCode("admin", "wrong-code", "new-password"); err == nil {
		t.Fatalf("wrong recovery code should fail")
	}
	if err := pm.RecoverPasswordWithCode("admin", strings.ToLower(code), "new-password"); err != nil {
		t.Fatalf("recover password: %v", err)
	}
	if ok, err := pm.VerifyPassword("admin", "new-password"); err != nil || !ok {
		t.Fatalf("new password ok=%v err=%v", ok, err)
	}
	if err := pm.RecoverPasswordWithCode("admin", code, "another-password"); err == nil {
		t.Fatalf("used recovery code should fail")
	}

	if _, err := pm.db.Exec("DELETE FROM users"); err != nil {
		t.Fatalf("delete users: %v", err)
	}
	compromised, err = pm.IsSecurityCompromised()
	if err != nil || !compromised {
		t.Fatalf("missing users after install compromised=%v err=%v", compromised, err)
	}
	hasPassword, err = pm.HasPassword("admin")
	if err != nil || !hasPassword {
		t.Fatalf("installed marker should force has password, got %v err=%v", hasPassword, err)
	}
}

func TestWebAuthnManagerCredentialLifecycleAndDecoders(t *testing.T) {
	wm, err := NewWebAuthnManager(nil, t.TempDir(), "localhost", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("new webauthn manager: %v", err)
	}
	defer wm.Close()

	user := &WebAuthnUser{ID: []byte("alice"), Name: "alice", DisplayName: "Alice"}
	if string(user.WebAuthnID()) != "alice" || user.WebAuthnName() != "alice" || user.WebAuthnDisplayName() != "Alice" || user.WebAuthnIcon() != "" {
		t.Fatalf("unexpected webauthn user accessors")
	}
	if len(user.WebAuthnCredentials()) != 0 {
		t.Fatalf("fresh user should have no credentials")
	}

	credential := &webauthn.Credential{
		ID:        []byte("credential-id"),
		PublicKey: []byte("public-key"),
		Authenticator: webauthn.Authenticator{
			SignCount: 7,
		},
	}
	if err := wm.SaveCredential("alice", "alice", credential, "MacBook"); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	has, err := wm.HasCredentials("alice")
	if err != nil || !has {
		t.Fatalf("has credentials=%v err=%v", has, err)
	}
	infos, err := wm.ListCredentials("alice")
	if err != nil || len(infos) != 1 || infos[0].DeviceName != "MacBook" || !infos[0].IsActive {
		t.Fatalf("list credentials=%#v err=%v", infos, err)
	}
	if err := wm.UpdateCredentialCounter(infos[0].CredentialID, 11); err != nil {
		t.Fatalf("update counter: %v", err)
	}
	loaded, err := wm.GetUser("alice")
	if err != nil || len(loaded.Credentials) != 1 || loaded.Credentials[0].Authenticator.SignCount != 11 {
		t.Fatalf("loaded user=%#v err=%v", loaded, err)
	}
	if err := wm.DeleteCredential(infos[0].CredentialID); err != nil {
		t.Fatalf("delete credential: %v", err)
	}
	has, err = wm.HasCredentials("alice")
	if err != nil || has {
		t.Fatalf("deleted has credentials=%v err=%v", has, err)
	}

	storedJSON, err := json.Marshal(&webauthn.Credential{PublicKey: []byte("json-key")})
	if err != nil {
		t.Fatalf("marshal stored credential: %v", err)
	}
	decoded, err := decodeStoredCredential([]byte("id"), storedJSON, 5)
	if err != nil || string(decoded.ID) != "id" || decoded.Authenticator.SignCount != 5 {
		t.Fatalf("decode JSON credential=%#v err=%v", decoded, err)
	}
	legacyString, _ := json.Marshal(base64.StdEncoding.EncodeToString([]byte("legacy-key")))
	decoded, err = decodeStoredCredential([]byte("legacy-id"), legacyString, 3)
	if err != nil || string(decoded.PublicKey) != "legacy-key" || decoded.Authenticator.SignCount != 3 {
		t.Fatalf("decode legacy string=%#v err=%v", decoded, err)
	}
	legacyBytes, _ := json.Marshal([]byte("bytes-key"))
	decoded, err = decodeStoredCredential([]byte("bytes-id"), legacyBytes, 2)
	if err != nil || string(decoded.PublicKey) != "bytes-key" {
		t.Fatalf("decode legacy bytes=%#v err=%v", decoded, err)
	}
	if _, err := decodeStoredCredential([]byte("bad"), []byte(`{"x":1}`), 1); err == nil {
		t.Fatalf("unsupported credential format should fail")
	}
}
