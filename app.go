package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Settings struct {
	GiteaBase   string `json:"gitea_base,omitempty"`
	GiteaUser   string `json:"gitea_user,omitempty"`
	GithubToken string `json:"github_token,omitempty"`
	GiteaToken  string `json:"gitea_token,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	Direction   string `json:"-"`
}
type Event struct {
	At      string `json:"at"`
	Message string `json:"message"`
}
type Pair struct {
	ID         string    `json:"id"`
	GithubRepo string    `json:"github_repo"`
	GiteaRepo  string    `json:"gitea_repo"`
	Direction  string    `json:"direction"`
	Interval   int       `json:"interval"`
	Enabled    bool      `json:"enabled"`
	Archived   bool      `json:"archived"`
	Checked    bool      `json:"checked"`
	Busy       bool      `json:"busy"`
	Phase      string    `json:"phase"`
	Error      string    `json:"error"`
	Last       string    `json:"last"`
	Next       time.Time `json:"next"`
	Plan       []RefPlan `json:"plan"`
	Events     []Event   `json:"events"`
	Queued     string    `json:"queued"`
	cancel     context.CancelFunc
}
type State struct {
	Version  int             `json:"version"`
	Settings Settings        `json:"settings"`
	Pairs    []*Pair         `json:"pairs"`
	Seen     map[string]bool `json:"seen,omitempty"`
	Checked  bool            `json:"checked,omitempty"`
}
type Repo struct {
	AccessChecked bool   `json:"access_checked"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Permissions   struct {
		Push bool `json:"push"`
		Pull bool `json:"pull"`
	} `json:"permissions"`
}
type App struct {
	verifyPair       func(context.Context, Settings, string, string) error
	nasConnections   []NASConnection
	detecting        bool
	detectMessage    string
	detectVersion    int
	mu               sync.Mutex
	state            State
	dir              string
	secret           []byte
	repos            map[string][]Repo
	discovering      bool
	discoveryError   string
	discoveryVersion int
	repoVerifiedAt   time.Time
	discoveryNotice  string
	run              func(context.Context, Settings, map[string]bool, bool, string, string, string) (SyncResult, error)
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var pairID = regexp.MustCompile(`^(default|[a-f0-9]{24})$`)

func newPair(id, gh, gt, dir string) *Pair {
	return &Pair{ID: id, GithubRepo: gh, GiteaRepo: gt, Direction: dir, Interval: 3, Plan: []RefPlan{}, Events: []Event{}, Phase: "idle"}
}
func validDirection(s string) bool {
	return s == "both" || s == "github_to_gitea" || s == "gitea_to_github"
}
func newApp(dir string) (*App, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return nil, e
	}
	a := &App{dir: dir, secret: make([]byte, 32), repos: map[string][]Repo{"github": {}, "gitea": {}}, run: syncRepos, verifyPair: verifyPairAccess}
	if _, e := rand.Read(a.secret); e != nil {
		return nil, e
	}
	raw, e := os.ReadFile(filepath.Join(dir, "state.json"))
	exists := e == nil
	if exists {
		if json.Unmarshal(raw, &a.state) != nil {
			return nil, errors.New("저장된 설정을 읽을 수 없어요")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if giteaBase(a.state.Settings) != "" && !allowedGiteaBase(giteaBase(a.state.Settings)) {
		return nil, errors.New("invalid Gitea endpoint")
	}
	if a.state.Version > 3 {
		return nil, errors.New("unsupported version")
	}
	if a.state.Version < 2 {
		// Older single-pair releases did not persist repository identities.
		// Keep legacy cache/ledger files in place; never guess a user's repository.
		a.state.Settings.Enabled = false
		a.state.Version = 2
	}
	if a.state.Version < 3 {
		kept := []*Pair{}
		legacy := []*Pair{}
		for _, p := range a.state.Pairs {
			if p != nil && p.ID == "default" {
				p.Enabled = false
				p.Checked = false
				legacy = append(legacy, p)
			} else {
				kept = append(kept, p)
			}
		}
		if len(legacy) > 0 {
			backup := filepath.Join(dir, "legacy-default-backup.json")
			if _, err := os.Stat(backup); os.IsNotExist(err) {
				if err = writeJSON(backup, legacy); err != nil {
					return nil, err
				}
			} else if err != nil {
				return nil, err
			}
		}
		a.state.Pairs = kept
		a.state.Version = 3
	}

	if a.state.Pairs == nil {
		a.state.Pairs = []*Pair{}
	}
	if a.state.Settings.GiteaBase == "" {
		for _, p := range a.state.Pairs {
			p.Enabled = false
			p.Checked = false
			p.Next = time.Time{}
		}
	}

	seenID, seenGH, seenGT := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, p := range a.state.Pairs {
		if p == nil || !pairID.MatchString(p.ID) || !repoName.MatchString(p.GithubRepo) || !repoName.MatchString(p.GiteaRepo) || !validDirection(p.Direction) || p.Interval < 1 || p.Interval > 1440 || seenID[p.ID] {
			return nil, errors.New("invalid saved pair")
		}
		seenID[p.ID] = true
		if !p.Archived {
			gh, gt := strings.ToLower(p.GithubRepo), strings.ToLower(p.GiteaRepo)
			if seenGH[gh] || seenGT[gt] {
				return nil, errors.New("overlapping pair")
			}
			seenGH[gh] = true
			seenGT[gt] = true
		}
		p.Busy = false
		p.Queued = ""
		p.Checked = false
		p.Phase = "idle"
		p.Next = time.Time{}
		p.cancel = nil
		if p.Plan == nil {
			p.Plan = []RefPlan{}
		}
		if p.Events == nil {
			p.Events = []Event{}
		}
	}
	if e = a.save(); e != nil {
		return nil, e
	}
	return a, nil
}
func (a *App) save() error { return writeJSON(filepath.Join(a.dir, "state.json"), a.state) }
func (a *App) csrf(s string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(s))
	return hex.EncodeToString(m.Sum(nil))
}
func (p *Pair) event(s string) {
	p.Events = append([]Event{{time.Now().Format(time.RFC3339), s}}, p.Events...)
	if len(p.Events) > 40 {
		p.Events = p.Events[:40]
	}
}
func (a *App) anyBusy() bool {
	for _, p := range a.state.Pairs {
		if p.Busy || p.Queued != "" {
			return true
		}
	}
	return false
}
func (a *App) find(id string) *Pair {
	for _, p := range a.state.Pairs {
		if p.ID == id {
			return p
		}
	}
	return nil
}
func (a *App) known(side, name string) bool {
	for _, r := range a.repos[side] {
		if r.FullName == name && r.AccessChecked {
			return true
		}
	}
	return false
}
func (a *App) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	sid := r.Header.Get("X-Session-ID")
	if sid == "" {
		fail(w, 401, "앱 로그인이 필요해요")
		return
	}
	if r.Method == "GET" {
		if r.URL.Path != "/status" {
			fail(w, 404, "없는 화면이에요")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		sendJSON(w, map[string]any{"version": "1.5.0", "csrf": a.csrf(sid), "github_saved": a.state.Settings.GithubToken != "", "gitea_saved": a.state.Settings.GiteaToken != "", "pairs": a.state.Pairs, "repos": a.repos, "discovering": a.discovering, "discovery_error": a.discoveryError, "discovery_notice": a.discoveryNotice, "repo_version": a.discoveryVersion, "user": r.Header.Get("X-App-User"), "role": r.Header.Get("X-App-Role"), "gitea_base": giteaBase(a.state.Settings), "gitea_user": a.state.Settings.GiteaUser, "nas_connections": a.nasConnections, "detecting": a.detecting, "detect_message": a.detectMessage, "detect_version": a.detectVersion})
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "지원하지 않는 요청이에요")
		return
	}
	if !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(a.csrf(sid))) {
		fail(w, 403, "화면을 새로 열어 주세요")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "JSON 요청만 허용해요")
		return
	}
	var req struct {
		GiteaBase   string `json:"gitea_base"`
		GiteaUser   string `json:"gitea_user"`
		ID          string `json:"id"`
		GithubToken string `json:"github_token"`
		GiteaToken  string `json:"gitea_token"`
		Consent     bool   `json:"consent"`
		GithubRepo  string `json:"github_repo"`
		GiteaRepo   string `json:"gitea_repo"`
		Direction   string `json:"direction"`
		Interval    int    `json:"interval"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 12000))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		fail(w, 400, "입력 내용을 확인해 주세요")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch r.URL.Path {
	case "/detect":
		if a.detecting {
			fail(w, 409, "이미 NAS 연결을 확인하고 있어요")
			return
		}
		a.detectNAS()
	case "/save":
		if a.anyBusy() || a.discovering {
			fail(w, 409, "진행 중인 작업이 끝난 뒤 연결 키를 바꿔 주세요")
			return
		}
		if !req.Consent {
			fail(w, 400, "연결 키의 NAS 저장에 동의해 주세요")
			return
		}
		s := a.state.Settings
		req.GiteaBase = strings.TrimRight(strings.TrimSpace(req.GiteaBase), "/")
		if req.GiteaBase == "" {
			req.GiteaBase = s.GiteaBase
		}
		approved := strings.HasPrefix(req.GiteaBase, "https://") || req.GiteaBase == s.GiteaBase
		for _, c := range a.nasConnections {
			if c.URL == req.GiteaBase {
				approved = true
			}
		}
		if !approved || !allowedGiteaBase(req.GiteaBase) {
			fail(w, 400, "Gitea HTTPS 주소를 입력하거나 자동 탐색된 내부 연결을 선택해 주세요")
			return
		}
		if req.GiteaBase != s.GiteaBase && s.GiteaToken != "" && req.GiteaToken == "" {
			fail(w, 400, "서버를 바꾸면 새 서버의 Gitea 토큰을 다시 입력해 주세요")
			return
		}
		s.GiteaBase = req.GiteaBase

		if req.GiteaUser != "" {
			if len(req.GiteaUser) > 100 || strings.ContainsAny(req.GiteaUser, " /:\r\n\t") {
				fail(w, 400, "Gitea 사용자 이름을 확인해 주세요")
				return
			}
			s.GiteaUser = req.GiteaUser
		}
		for _, v := range []string{req.GithubToken, req.GiteaToken} {
			if len(v) > 4096 || strings.ContainsAny(v, "\r\n\t ") {
				fail(w, 400, "접근 토큰에 공백이 들어 있어요")
				return
			}
		}
		if req.GithubToken != "" {
			s.GithubToken = req.GithubToken
		}
		if req.GiteaToken != "" {
			s.GiteaToken = req.GiteaToken
		}
		if s.GithubToken == "" || s.GiteaToken == "" {
			fail(w, 400, "두 서비스의 접근 토큰을 연결해 주세요")
			return
		}
		old := a.state.Settings
		a.state.Settings = s
		for _, p := range a.state.Pairs {
			p.Enabled = false
			p.Checked = false
			p.Next = time.Time{}
		}
		if a.save() != nil {
			a.state.Settings = old
			fail(w, 500, "설정을 저장하지 못했어요. 자동 작업은 일시정지했어요")
			return
		}
		a.repos = map[string][]Repo{"github": {}, "gitea": {}}
		a.discoveryVersion++
		a.discover()
	case "/discover":
		if a.discovering {
			fail(w, 409, "저장소 목록을 불러오는 중이에요")
			return
		}
		if a.state.Settings.GithubToken == "" || a.state.Settings.GiteaToken == "" || !allowedGiteaBase(a.state.Settings.GiteaBase) {
			fail(w, 400, "계정 연결을 먼저 완료해 주세요")
			return
		}
		a.discover()
	case "/delete":
		p := a.find(req.ID)
		if p == nil {
			fail(w, 404, "제거할 연결을 찾지 못했어요")
			return
		}
		if !req.Consent {
			fail(w, 400, "연결 설정과 내부 캐시 제거를 확인해 주세요")
			return
		}
		if p.Busy || p.Queued != "" {
			fail(w, 409, "진행 중인 작업을 일시정지하고 완료된 뒤 제거해 주세요")
			return
		}
		old := a.state.Pairs
		kept := make([]*Pair, 0, len(old)-1)
		for _, entry := range old {
			if entry.ID != p.ID {
				kept = append(kept, entry)
			}
		}
		a.state.Pairs = kept
		if err := a.save(); err != nil {
			a.state.Pairs = old
			fail(w, 500, "연결 설정을 저장하지 못해 제거하지 않았어요")
			return
		}
		// Validated IDs are single path components; RemoveAll does not follow symlinks.
		if err := os.RemoveAll(filepath.Join(a.dir, "pairs", p.ID)); err != nil {
			sendJSON(w, map[string]any{"ok": true, "warning": "연결은 제거했지만 내부 캐시 정리는 실패했어요"})
			return
		}

	case "/add":
		if req.Interval < 0 || req.Interval > 1440 {
			fail(w, 400, "자동 확인 간격을 확인해 주세요")
			return
		}
		if a.discovering || a.repoVerifiedAt.IsZero() || time.Since(a.repoVerifiedAt) > 5*time.Minute {
			fail(w, 409, "저장소 목록을 새로 불러와 접근 권한을 확인해 주세요")
			return
		}
		if !a.known("github", req.GithubRepo) || !a.known("gitea", req.GiteaRepo) || !validDirection(req.Direction) {
			fail(w, 400, "불러온 목록에서 두 저장소와 방향을 선택해 주세요")
			return
		}
		settings, revision := a.state.Settings, a.discoveryVersion
		a.mu.Unlock()
		checkCtx, checkCancel := context.WithTimeout(r.Context(), 15*time.Second)
		checkErr := a.verifyPair(checkCtx, settings, req.GithubRepo, req.GiteaRepo)
		checkCancel()
		a.mu.Lock()
		if checkErr != nil {
			fail(w, 400, "선택한 저장소의 현재 토큰 접근을 확인하지 못했어요. 권한과 연결을 확인해 주세요")
			return
		}
		if revision != a.discoveryVersion || a.discovering {
			fail(w, 409, "계정 또는 목록이 바뀌었어요. 다시 선택해 주세요")
			return
		}
		if len(a.state.Pairs) >= 100 {
			fail(w, 400, "최대 100개까지 등록할 수 있어요")
			return
		}
		for _, p := range a.state.Pairs {
			if !p.Archived && (strings.EqualFold(p.GithubRepo, req.GithubRepo) || strings.EqualFold(p.GiteaRepo, req.GiteaRepo)) {
				fail(w, 409, "이미 연결된 저장소예요. 기존 연결의 상세 화면을 확인해 주세요")
				return
			}
		}
		var id [12]byte
		if _, e := rand.Read(id[:]); e != nil {
			fail(w, 500, "연결 ID 생성 실패")
			return
		}
		p := newPair(hex.EncodeToString(id[:]), req.GithubRepo, req.GiteaRepo, req.Direction)
		if req.Interval != 0 {
			p.Interval = req.Interval
		}
		a.state.Pairs = append(a.state.Pairs, p)
		if a.save() != nil {
			a.state.Pairs = a.state.Pairs[:len(a.state.Pairs)-1]
			fail(w, 500, "연결을 저장하지 못했어요")
			return
		}
		p.event("저장소 쌍을 추가했어요. 자동 실행 전에 연결 확인을 눌러 주세요")
	case "/check", "/start", "/sync", "/pause", "/update", "/archive", "/restore":
		p := a.find(req.ID)
		if p == nil {
			fail(w, 404, "저장소 연결을 찾지 못했어요")
			return
		}
		if p.Archived && r.URL.Path != "/restore" {
			fail(w, 409, "보관한 연결을 먼저 복원해 주세요")
			return
		}
		if (p.Busy || p.Queued != "") && r.URL.Path != "/pause" {
			fail(w, 409, "이 저장소는 작업 중이에요")
			return
		}
		switch r.URL.Path {
		case "/pause":
			p.Queued = ""
			p.Phase = "paused"
			p.Enabled = false
			p.Next = time.Time{}
			if p.cancel != nil {
				p.cancel()
			}
			p.event("자동 동기화를 일시정지했어요")
		case "/check":
			if a.state.Settings.GithubToken == "" || a.state.Settings.GiteaToken == "" || !allowedGiteaBase(a.state.Settings.GiteaBase) {
				fail(w, 400, "계정 연결을 먼저 완료해 주세요")
				return
			}
			a.begin(p, false)
		case "/start", "/sync":
			if !p.Checked {
				fail(w, 409, "연결 확인을 먼저 완료해 주세요")
				return
			}
			if r.URL.Path == "/start" {
				p.Enabled = true
				if a.save() != nil {
					p.Enabled = false
					fail(w, 500, "자동 실행 설정을 저장하지 못했어요")
					return
				}
			}
			a.begin(p, true)
		case "/update":
			if !validDirection(req.Direction) || req.Interval < 1 || req.Interval > 1440 {
				fail(w, 400, "방향과 실행 간격을 확인해 주세요")
				return
			}
			p.Direction = req.Direction
			p.Interval = req.Interval
			p.Enabled = false
			p.Checked = false
			p.Next = time.Time{}
			p.Plan = []RefPlan{}
			p.event("설정을 변경했어요. 연결을 다시 확인한 후 자동 실행을 켜 주세요")
		case "/archive":
			p.Archived = true
			p.Enabled = false
			p.Next = time.Time{}
			p.event("연결을 보관했어요. 원격 저장소와 코드에는 영향이 없어요")
		case "/restore":
			for _, other := range a.state.Pairs {
				if other != p && !other.Archived && (strings.EqualFold(other.GithubRepo, p.GithubRepo) || strings.EqualFold(other.GiteaRepo, p.GiteaRepo)) {
					fail(w, 409, "같은 저장소가 다른 연결에서 사용 중이에요")
					return
				}
			}
			p.Archived = false
			p.Enabled = false
			p.Checked = false
			p.event("연결을 복원했어요")
		}
		if a.save() != nil {
			p.Enabled = false
			if p.cancel != nil {
				p.cancel()
			}
			fail(w, 500, "상태를 저장하지 못해 자동 실행을 중지했어요")
			return
		}
	default:
		fail(w, 404, "없는 작업이에요")
		return
	}
	sendJSON(w, map[string]bool{"ok": true})
}
func (a *App) begin(p *Pair, apply bool) {
	a.beginJob(p, apply, false)
}

// Scheduled checks after startup or recovery may immediately apply safe changes.
// Explicit user checks remain read-only, even when automatic execution is enabled.
func (a *App) beginJob(p *Pair, apply, syncAfterCheck bool) {
	active := 0
	for _, other := range a.state.Pairs {
		if other.Busy {
			active++
		}
	}
	if active >= 2 {
		p.Queued = "check"
		if apply {
			p.Queued = "sync"
		}
		p.Phase = "queued"
		return
	}
	p.Queued = ""
	p.Busy = true
	p.Error = ""
	p.Phase = "checking"
	if apply {
		p.Phase = "syncing"
	}
	p.event(map[bool]string{true: "동기화를 시작했어요", false: "저장소를 변경하지 않고 확인 중이에요"}[apply])
	s := a.state.Settings
	s.Direction = p.Direction
	dir := filepath.Join(a.dir, "pairs", p.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		p.Busy = false
		p.Error = "작업 공간을 만들지 못했어요"
		p.Phase = "error"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	p.cancel = cancel
	go func() {
		defer cancel()
		result, e := a.run(ctx, s, map[string]bool{}, apply, dir, "https://github.com/"+p.GithubRepo+".git", giteaBase(s)+"/"+p.GiteaRepo+".git")
		a.mu.Lock()
		defer a.mu.Unlock()
		p.Busy = false
		p.cancel = nil
		p.Last = time.Now().Format(time.RFC3339)
		p.Next = time.Time{}
		if p.Enabled {
			p.Next = time.Now().Add(time.Duration(p.Interval) * time.Minute)
		}
		if e != nil {
			p.Checked = false
			p.Error = safeError(e)
			p.Phase = "error"
			if errors.Is(e, context.Canceled) && !p.Enabled {
				p.Phase = "paused"
				p.event(p.Error)
				p.Error = ""
			} else {
				p.event(p.Error)
			}
		} else {
			p.Plan = result.Plan
			p.Checked = true
			p.Phase = "healthy"
			if result.Blocked > 0 {
				p.Phase = "conflict"
			} else if !apply && result.Pending > 0 {
				p.Phase = "pending"
			}
			if apply {
				p.event(fmt.Sprintf("완료 · 반영 %d개 · 확인 필요 %d개", result.Applied, result.Blocked))
			} else {
				p.event(fmt.Sprintf("연결 확인 · 변경 예정 %d개 · 확인 필요 %d개", result.Pending, result.Blocked))
			}
		}
		if a.save() != nil {
			p.Enabled = false
			p.Checked = false
			p.Error = "상태 저장 실패로 자동 실행을 중지했어요"
			p.Phase = "error"
			return
		}
		if syncAfterCheck && !apply && e == nil && result.Pending > 0 && p.Enabled && !p.Archived {
			// The completed check released its worker slot. begin enforces the same
			// two-worker limit and syncRepos rechecks remote refs before pushing.
			a.begin(p, true)
		}
	}()
}
func (a *App) tick() {
	a.mu.Lock()
	defer a.mu.Unlock()
	active := 0
	for _, p := range a.state.Pairs {
		if p.Busy {
			active++
		}
	}
	for _, p := range a.state.Pairs {
		if active >= 2 {
			return
		}
		if !p.Archived && !p.Busy && p.Queued != "" {
			a.begin(p, p.Queued == "sync")
			active++
		}
	}
	for _, p := range a.state.Pairs {
		if active >= 2 {
			return
		}
		if !p.Archived && p.Enabled && !p.Busy && (p.Next.IsZero() || time.Now().After(p.Next)) {
			a.beginJob(p, p.Checked, !p.Checked)
			active++
		}
	}
}
