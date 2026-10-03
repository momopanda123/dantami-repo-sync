package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "__Host-DantamiSession"

var loginName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{2,39}$`)

type Account struct {
	Username string `json:"username"`
	Hash     string `json:"hash"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}
type AccountDB struct {
	Users []Account `json:"users"`
}
type AppSession struct {
	User    string
	Expires time.Time
}
type loginLimit struct {
	Count int
	Until time.Time
}
type AuthApp struct {
	mu       sync.Mutex
	app      *App
	db       AccountDB
	sessions map[string]AppSession
	limits   map[string]loginLimit
	dummy    []byte
}

func validPassword(p string) bool { return len(p) >= 12 && len(p) <= 72 }
func newAuthApp(a *App) (*AuthApp, error) {
	x := &AuthApp{app: a, sessions: map[string]AppSession{}, limits: map[string]loginLimit{}}
	b, e := os.ReadFile(filepath.Join(a.dir, "accounts.json"))
	if e != nil {
		return nil, errors.New("앱 관리자 계정이 없어요. 패키지 설치/업데이트 화면에서 관리자 계정을 설정해 주세요")
	}
	if json.Unmarshal(b, &x.db) != nil || len(x.db.Users) == 0 {
		return nil, errors.New("account data invalid")
	}
	names := map[string]bool{}
	admins := 0
	for _, u := range x.db.Users {
		if !loginName.MatchString(u.Username) || names[u.Username] || (u.Role != "admin" && u.Role != "viewer") {
			return nil, errors.New("account data invalid")
		}
		if _, e := bcrypt.Cost([]byte(u.Hash)); e != nil {
			return nil, errors.New("account hash invalid")
		}
		names[u.Username] = true
		if u.Role == "admin" && !u.Disabled {
			admins++
		}
	}
	if admins == 0 {
		return nil, errors.New("no active administrator")
	}
	x.dummy, e = bcrypt.GenerateFromPassword([]byte("dummy-password-not-a-login"), 12)
	if e != nil {
		return nil, e
	}
	return x, nil
}
func initializeAccounts(dir, user, password, confirm string) error {
	p := filepath.Join(dir, "accounts.json")
	if _, e := os.Stat(p); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if !loginName.MatchString(user) || !validPassword(password) || password != confirm {
		return errors.New("관리자 이름은 영문/숫자/._- 3~40자, 비밀번호는 12~72바이트이며 확인 값과 같아야 해요")
	}
	h, e := bcrypt.GenerateFromPassword([]byte(password), 12)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	return writeJSON(p, AccountDB{Users: []Account{{Username: user, Hash: string(h), Role: "admin"}}})
}
func sessionKey(raw string) string { h := sha256.Sum256([]byte(raw)); return hex.EncodeToString(h[:]) }
func secureOrigin(r *http.Request) bool {
	u, e := url.Parse(r.Header.Get("Origin"))
	return e == nil && u.Scheme == "https" && u.Host == r.Host && u.User == nil && u.Path == ""
}
func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}
func (x *AuthApp) user(name string) *Account {
	for i := range x.db.Users {
		if x.db.Users[i].Username == name {
			return &x.db.Users[i]
		}
	}
	return nil
}
func (x *AuthApp) session(r *http.Request) (Account, string, bool) {
	c, e := r.Cookie(sessionCookie)
	if e != nil || len(c.Value) != 64 {
		return Account{}, "", false
	}
	key := sessionKey(c.Value)
	s, ok := x.sessions[key]
	if !ok || time.Now().After(s.Expires) {
		delete(x.sessions, key)
		return Account{}, "", false
	}
	u := x.user(s.User)
	if u == nil || u.Disabled {
		return Account{}, "", false
	}
	return *u, key, true
}
func authJSON(r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("JSON required")
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 8192))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (x *AuthApp) handler(w http.ResponseWriter, r *http.Request) {
	w = withLocale(w, r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'self'")
	if r.Method != "GET" && r.Method != "POST" {
		fail(w, 405, "지원하지 않는 요청이에요")
		return
	}
	if r.Method == "POST" && !secureOrigin(r) {
		fail(w, 403, "HTTPS 앱 화면에서 다시 시도해 주세요")
		return
	}
	if r.URL.Path == "/auth/login" {
		x.login(w, r)
		return
	}
	x.mu.Lock()
	u, key, ok := x.session(r)
	x.mu.Unlock()
	if r.URL.Path == "/" || r.URL.Path == "/asset" {
		if r.Method != "GET" {
			fail(w, 405, "GET required")
			return
		}
		asset := r.URL.Query().Get("name")
		if r.URL.Path == "/" {
			if ok {
				asset = "index.html"
			} else {
				asset = "login.html"
			}
		}
		public := asset == "login.css" || asset == "login.js" || asset == "language.js"
		if !ok && !public && asset != "login.html" {
			fail(w, 401, "로그인이 필요해요")
			return
		}
		if asset != "index.html" && asset != "app.js" && asset != "dantami-app.css" && asset != "login.html" && !public {
			http.NotFound(w, r)
			return
		}
		if requestLanguage(r) == "en" {
			if v, ok := map[string]string{"index.html": "index.en.html", "login.html": "login.en.html", "app.js": "app.en.js", "login.js": "login.en.js"}[asset]; ok {
				asset = v
			}
		}
		b, e := webFiles.ReadFile("web/" + asset)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		typ := "text/html; charset=utf-8"
		if strings.HasSuffix(asset, ".css") {
			typ = "text/css"
		}
		if strings.HasSuffix(asset, ".js") {
			typ = "application/javascript"
		}
		w.Header().Set("Content-Type", typ)
		w.Write(b)
		return
	}
	if !ok {
		fail(w, 401, "로그인이 필요해요")
		return
	}
	if r.Method == "POST" && !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(x.app.csrf(key))) {
		fail(w, 403, "요청 확인에 실패했어요. 새로고침해 주세요")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/auth/") {
		x.manage(w, r, u, key)
		return
	}
	if u.Role != "admin" && (r.Method != "GET" || r.URL.Path != "/status") {
		fail(w, 403, "관리자 권한이 필요해요")
		return
	}
	if r.URL.Path == "/diagnostics" {
		sendJSON(w, map[string]any{"checks": runtimeDiagnostics(x.app.dir)})
		return
	}
	r.Header.Set("X-Session-ID", key)
	r.Header.Set("X-App-User", u.Username)
	r.Header.Set("X-App-Role", u.Role)
	x.app.handler(w, r)
}
func (x *AuthApp) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if authJSON(r, &in) != nil || len(in.Username) > 40 || len(in.Password) > 72 {
		fail(w, 400, "입력을 확인해 주세요")
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	now := time.Now()
	for k, v := range x.limits {
		if now.After(v.Until) {
			delete(x.limits, k)
		}
	}
	for _, k := range []string{"*", in.Username} {
		v := x.limits[k]
		limit := 5
		if k == "*" {
			limit = 30
		}
		if v.Count >= limit {
			w.Header().Set("Retry-After", "60")
			fail(w, 429, "시도가 많아요. 잠시 뒤 다시 로그인해 주세요")
			return
		}
	}
	hash := x.dummy
	u := x.user(in.Username)
	if u != nil {
		hash = []byte(u.Hash)
	}
	e := bcrypt.CompareHashAndPassword(hash, []byte(in.Password))
	if e != nil || u == nil || u.Disabled {
		for _, k := range []string{"*", in.Username} {
			v := x.limits[k]
			if v.Count == 0 {
				v.Until = now.Add(time.Minute)
			}
			v.Count++
			x.limits[k] = v
		}
		fail(w, 401, "아이디 또는 비밀번호를 확인해 주세요")
		return
	}
	delete(x.limits, in.Username)
	for k, v := range x.sessions {
		if now.After(v.Expires) {
			delete(x.sessions, k)
		}
	}
	if len(x.sessions) >= 1024 {
		fail(w, 503, "활성 세션이 너무 많아요")
		return
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		fail(w, 500, "세션 생성 실패")
		return
	}
	raw := hex.EncodeToString(b)
	x.sessions[sessionKey(raw)] = AppSession{u.Username, now.Add(8 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: raw, Path: "/", MaxAge: 28800, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	sendJSON(w, map[string]bool{"ok": true})
}
func (x *AuthApp) invalidate(name string) {
	for k, s := range x.sessions {
		if s.User == name {
			delete(x.sessions, k)
		}
	}
}
func (x *AuthApp) manage(w http.ResponseWriter, r *http.Request, actor Account, key string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	current, _, valid := x.session(r)
	if !valid {
		fail(w, 401, "다시 로그인해 주세요")
		return
	}
	actor = current
	if r.URL.Path == "/auth/me" && r.Method == "GET" {
		sendJSON(w, map[string]any{"username": actor.Username, "role": actor.Role, "csrf": x.app.csrf(key)})
		return
	}
	if r.URL.Path == "/auth/users" && r.Method == "GET" {
		if actor.Role != "admin" {
			fail(w, 403, "관리자 권한이 필요해요")
			return
		}
		out := []map[string]any{}
		for _, u := range x.db.Users {
			out = append(out, map[string]any{"username": u.Username, "role": u.Role, "disabled": u.Disabled})
		}
		sendJSON(w, out)
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return
	}
	if r.URL.Path == "/auth/logout" {
		delete(x.sessions, key)
		clearSession(w)
		sendJSON(w, map[string]bool{"ok": true})
		return
	}
	var in struct {
		Operation string `json:"operation"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Current   string `json:"current_password"`
		Role      string `json:"role"`
	}
	if authJSON(r, &in) != nil {
		fail(w, 400, "입력을 확인해 주세요")
		return
	}
	own := x.user(actor.Username)
	if own == nil || bcrypt.CompareHashAndPassword([]byte(own.Hash), []byte(in.Current)) != nil {
		fail(w, 403, "현재 비밀번호를 확인해 주세요")
		return
	}
	backup := append([]Account(nil), x.db.Users...)
	target := in.Username
	if r.URL.Path == "/auth/password" {
		target = actor.Username
		in.Operation = "password"
	} else if r.URL.Path != "/auth/users" || actor.Role != "admin" {
		fail(w, 403, "관리자 권한이 필요해요")
		return
	}
	u := x.user(target)
	switch in.Operation {
	case "create":
		if !loginName.MatchString(target) || u != nil || len(x.db.Users) >= 50 || (in.Role != "admin" && in.Role != "viewer") || !validPassword(in.Password) {
			fail(w, 400, "아이디·권한·비밀번호를 확인해 주세요")
			return
		}
		h, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
		if e != nil {
			fail(w, 500, "계정 생성 실패")
			return
		}
		x.db.Users = append(x.db.Users, Account{Username: target, Hash: string(h), Role: in.Role})
	case "password":
		if u == nil || !validPassword(in.Password) {
			fail(w, 400, "12~72바이트의 새 비밀번호를 입력해 주세요")
			return
		}
		h, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
		if e != nil {
			fail(w, 500, "변경 실패")
			return
		}
		u.Hash = string(h)
	case "disable", "enable", "delete":
		if u == nil {
			fail(w, 404, "계정이 없어요")
			return
		}
		if target == actor.Username {
			fail(w, 400, "자기 계정은 여기서 비활성화하거나 삭제할 수 없어요")
			return
		}
		if in.Operation == "delete" {
			v := []Account{}
			for _, a := range x.db.Users {
				if a.Username != target {
					v = append(v, a)
				}
			}
			x.db.Users = v
		} else {
			u.Disabled = in.Operation == "disable"
		}
	default:
		fail(w, 400, "지원하지 않는 계정 작업이에요")
		return
	}
	admins := 0
	for _, a := range x.db.Users {
		if a.Role == "admin" && !a.Disabled {
			admins++
		}
	}
	if admins == 0 {
		x.db.Users = backup
		fail(w, 400, "관리자는 한 명 이상 필요해요")
		return
	}
	if e := writeJSON(filepath.Join(x.app.dir, "accounts.json"), x.db); e != nil {
		x.db.Users = backup
		fail(w, 500, "계정을 저장하지 못했어요")
		return
	}
	x.invalidate(target)
	if target == actor.Username {
		clearSession(w)
	}
	sendJSON(w, map[string]bool{"ok": true})
}
