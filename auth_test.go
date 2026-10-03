package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPassword = "test-only-long-password"

func authFixture(t *testing.T) *AuthApp {
	t.Helper()
	a := seeded(t)
	if e := initializeAccounts(a.dir, "owner", testPassword, testPassword); e != nil {
		t.Fatal(e)
	}
	x, e := newAuthApp(a)
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func authReq(x *AuthApp, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "https://nas.example"+path, strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://nas.example")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	x.handler(w, r)
	return w
}
func loginAs(t *testing.T, x *AuthApp, user, pass string) (*http.Cookie, string) {
	t.Helper()
	w := authReq(x, "POST", "/auth/login", map[string]string{"username": user, "password": pass}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	c := w.Result().Cookies()[0]
	return c, x.app.csrf(sessionKey(c.Value))
}
func TestStandaloneAuthenticationBoundary(t *testing.T) {
	x := authFixture(t)
	for _, p := range []string{"/status", "/diagnostics", "/auth/users", "/asset?name=app.js", "/asset?name=index.html", "/asset?name=dantami-app.css"} {
		w := authReq(x, "GET", p, nil, nil, "")
		if w.Code != 401 {
			t.Fatal(p, w.Code)
		}
	}
	w := authReq(x, "GET", "/", nil, nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "loginForm") || strings.Contains(w.Body.String(), "id=\"pairs\"") {
		t.Fatal("login screen boundary")
	}
	c, csrf := loginAs(t, x, "owner", testPassword)
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatal("cookie flags")
	}
	w = authReq(x, "GET", "/status", nil, c, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), testPassword) {
		t.Fatal(w.Code)
	}
	if w = authReq(x, "POST", "/pause", map[string]string{"id": "default"}, c, ""); w.Code != 403 {
		t.Fatal("missing CSRF allowed")
	}
	if w = authReq(x, "POST", "/auth/logout", map[string]string{}, c, csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = authReq(x, "GET", "/status", nil, c, ""); w.Code != 401 {
		t.Fatal("logout not invalidated")
	}
}
func TestStandaloneRateLimitAndOrigin(t *testing.T) {
	x := authFixture(t)
	r := httptest.NewRequest("POST", "https://nas.example/auth/login", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	x.handler(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin")
	}
	for i := 0; i < 5; i++ {
		w := authReq(x, "POST", "/auth/login", map[string]string{"username": "owner", "password": "wrong"}, nil, "")
		if w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
	if w := authReq(x, "POST", "/auth/login", map[string]string{"username": "owner", "password": testPassword}, nil, ""); w.Code != 429 {
		t.Fatal("rate limit")
	}
}
func TestAccountLifecycleAndViewerAccess(t *testing.T) {
	x := authFixture(t)
	admin, csrf := loginAs(t, x, "owner", testPassword)
	body := map[string]string{"operation": "create", "username": "reader", "password": testPassword, "current_password": testPassword, "role": "viewer"}
	if w := authReq(x, "POST", "/auth/users", body, admin, csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	reader, rcsrf := loginAs(t, x, "reader", testPassword)
	if w := authReq(x, "GET", "/status", nil, reader, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := authReq(x, "POST", "/pause", map[string]string{"id": "default"}, reader, rcsrf); w.Code != 403 {
		t.Fatal("viewer wrote")
	}
	if w := authReq(x, "GET", "/auth/users", nil, reader, ""); w.Code != 403 {
		t.Fatal("viewer listed users")
	}
	body = map[string]string{"operation": "disable", "username": "reader", "current_password": testPassword}
	if w := authReq(x, "POST", "/auth/users", body, admin, csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := authReq(x, "GET", "/status", nil, reader, ""); w.Code != 401 {
		t.Fatal("disabled session remained")
	}
	body["operation"] = "delete"
	if w := authReq(x, "POST", "/auth/users", body, admin, csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
	body["username"] = "owner"
	if w := authReq(x, "POST", "/auth/users", body, admin, csrf); w.Code != 400 {
		t.Fatal("self admin deleted")
	}
	w := authReq(x, "GET", "/auth/users", nil, admin, "")
	if strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "$2") {
		t.Fatal("hash leaked")
	}
	b, e := os.ReadFile(filepath.Join(x.app.dir, "accounts.json"))
	if e != nil || strings.Contains(string(b), testPassword) {
		t.Fatal("plaintext password stored")
	}
}
func TestPasswordChangeExpiresAllSessions(t *testing.T) {
	x := authFixture(t)
	c, csrf := loginAs(t, x, "owner", testPassword)
	other, _ := loginAs(t, x, "owner", testPassword)
	w := authReq(x, "POST", "/auth/password", map[string]string{"current_password": testPassword, "password": "replacement-test-password"}, c, csrf)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, cookie := range []*http.Cookie{c, other} {
		if w := authReq(x, "GET", "/status", nil, cookie, ""); w.Code != 401 {
			t.Fatal("session survived password change")
		}
	}
	loginAs(t, x, "owner", "replacement-test-password")
}
func TestFirstAdminNeverClaimedOverHTTPOrOverwritten(t *testing.T) {
	d := t.TempDir()
	if e := initializeAccounts(d, "owner", "short", "short"); e == nil {
		t.Fatal("weak password accepted")
	}
	if e := initializeAccounts(d, "owner", testPassword, testPassword); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(d, "accounts.json"))
	if e := initializeAccounts(d, "attacker", "another-long-password", "another-long-password"); e != nil {
		t.Fatal(e)
	}
	a, _ := os.ReadFile(filepath.Join(d, "accounts.json"))
	if string(a) != string(b) {
		t.Fatal("existing admin replaced")
	}
	st, _ := os.Stat(filepath.Join(d, "accounts.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal("account file permission")
	}
}
func TestExpiredAndForgedSessionsRejected(t *testing.T) {
	x := authFixture(t)
	c, _ := loginAs(t, x, "owner", testPassword)
	k := sessionKey(c.Value)
	s := x.sessions[k]
	s.Expires = time.Now().Add(-time.Second)
	x.sessions[k] = s
	if w := authReq(x, "GET", "/status", nil, c, ""); w.Code != 401 {
		t.Fatal("expired session")
	}
	c.Value = strings.Repeat("a", 64)
	if w := authReq(x, "GET", "/status", nil, c, ""); w.Code != 401 {
		t.Fatal("forged session")
	}
}
