package main

import (
	"context"
	"encoding/json"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitcmd(t *testing.T, d string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = d
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	d := t.TempDir()
	w := filepath.Join(d, "work")
	os.Mkdir(w, 0700)
	gitcmd(t, w, "init", "-b", "master")
	gitcmd(t, w, "config", "user.name", "Test")
	gitcmd(t, w, "config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(w, "a"), []byte("first"), 0600)
	gitcmd(t, w, "add", ".")
	gitcmd(t, w, "commit", "-m", "first")
	a, b := filepath.Join(d, "a.git"), filepath.Join(d, "b.git")
	gitcmd(t, d, "clone", "--bare", w, a)
	gitcmd(t, d, "clone", "--bare", w, b)
	return d, w, a, b
}
func change(t *testing.T, w, r, text string) {
	os.WriteFile(filepath.Join(w, "a"), []byte(text), 0600)
	gitcmd(t, w, "add", ".")
	gitcmd(t, w, "commit", "-m", text)
	gitcmd(t, w, "push", r, "master")
}
func runSync(t *testing.T, d, a, b string, seen map[string]bool, apply bool) SyncResult {
	t.Helper()
	v, e := syncRepos(context.Background(), Settings{}, seen, apply, d, a, b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestEqualAndPreview(t *testing.T) {
	d, w, a, b := fixture(t)
	s := map[string]bool{}
	v := runSync(t, d, a, b, s, false)
	if v.Pending != 0 || v.Blocked != 0 {
		t.Fatal(v)
	}
	old := gitcmd(t, b, "rev-parse", "master")
	change(t, w, a, "next")
	v = runSync(t, d, a, b, s, false)
	if v.Pending != 1 || gitcmd(t, b, "rev-parse", "master") != old {
		t.Fatal(v)
	}
}
func TestBothDirections(t *testing.T) {
	d, w, a, b := fixture(t)
	s := map[string]bool{}
	change(t, w, a, "github")
	v := runSync(t, d, a, b, s, true)
	if v.Applied != 1 || gitcmd(t, a, "rev-parse", "master") != gitcmd(t, b, "rev-parse", "master") {
		t.Fatal(v)
	}
	change(t, w, b, "gitea")
	v = runSync(t, d, a, b, v.Seen, true)
	if v.Applied != 1 || gitcmd(t, a, "rev-parse", "master") != gitcmd(t, b, "rev-parse", "master") {
		t.Fatal(v)
	}
}
func TestDivergedNeverOverwrites(t *testing.T) {
	d, w, a, b := fixture(t)
	base := gitcmd(t, w, "rev-parse", "HEAD")
	change(t, w, a, "left")
	gitcmd(t, w, "reset", "--hard", base)
	change(t, w, b, "right")
	left, right := gitcmd(t, a, "rev-parse", "master"), gitcmd(t, b, "rev-parse", "master")
	v := runSync(t, d, a, b, map[string]bool{}, true)
	if v.Blocked != 1 || v.Applied != 0 || gitcmd(t, a, "rev-parse", "master") != left || gitcmd(t, b, "rev-parse", "master") != right {
		t.Fatal(v)
	}
}
func TestNewBranchesTagsAndDeletion(t *testing.T) {
	d, w, a, b := fixture(t)
	v := runSync(t, d, a, b, map[string]bool{}, false)
	gitcmd(t, w, "branch", "feature/x")
	gitcmd(t, w, "tag", "v1")
	gitcmd(t, w, "push", a, "feature/x", "refs/tags/v1")
	v = runSync(t, d, a, b, v.Seen, true)
	if v.Applied != 2 {
		t.Fatal(v)
	}
	gitcmd(t, b, "update-ref", "-d", "refs/heads/feature/x")
	v = runSync(t, d, a, b, v.Seen, true)
	if v.Blocked != 1 || v.Applied != 0 {
		t.Fatal(v)
	}
}
func TestTagConflict(t *testing.T) {
	d, w, a, b := fixture(t)
	gitcmd(t, w, "tag", "v1")
	gitcmd(t, w, "push", a, "refs/tags/v1")
	change(t, w, b, "next")
	gitcmd(t, w, "tag", "-f", "v1")
	gitcmd(t, w, "push", b, "refs/tags/v1")
	v := runSync(t, d, a, b, map[string]bool{}, true)
	if v.Blocked != 1 {
		t.Fatal(v)
	}
}
func TestLFSFailsBeforeWrites(t *testing.T) {
	d, w, a, b := fixture(t)
	change(t, w, a, "version https://git-lfs.github.com/spec/v1\noid sha256:123\nsize 12\n")
	before := gitcmd(t, b, "rev-parse", "master")
	_, e := syncRepos(context.Background(), Settings{}, map[string]bool{}, true, d, a, b)
	if e == nil || e.Error() != "lfs" || gitcmd(t, b, "rev-parse", "master") != before {
		t.Fatal(e)
	}
}
func TestCanceled(t *testing.T) {
	d, _, a, b := fixture(t)
	ctx, c := context.WithCancel(context.Background())
	c()
	_, e := syncRepos(ctx, Settings{}, map[string]bool{}, true, d, a, b)
	if e == nil {
		t.Fatal("expected cancellation")
	}
}
func TestLeaseGuardsMissingRefRace(t *testing.T) {
	s := guardedReceive{}
	n := plumbing.ReferenceName("refs/heads/new")
	old := plumbing.NewHash(strings.Repeat("1", 40))
	new := plumbing.NewHash(strings.Repeat("2", 40))
	ctx := context.WithValue(context.Background(), leaseKey{}, lease{n, plumbing.ZeroHash, new})
	_, e := s.ReceivePack(ctx, &packp.ReferenceUpdateRequest{Commands: []*packp.Command{{Name: n, Old: old, New: new}}})
	if e == nil {
		t.Fatal("race not rejected")
	}
}
func TestLeaseGuardsDeletion(t *testing.T) {
	s := guardedReceive{}
	n := plumbing.ReferenceName("refs/heads/master")
	old := plumbing.NewHash(strings.Repeat("1", 40))
	ctx := context.WithValue(context.Background(), leaseKey{}, lease{n, old, plumbing.ZeroHash})
	_, e := s.ReceivePack(ctx, &packp.ReferenceUpdateRequest{Commands: []*packp.Command{{Name: n, Old: old, New: plumbing.ZeroHash}}})
	if e == nil {
		t.Fatal("delete allowed")
	}
}
func TestAPIAuthCSRFAndSecrets(t *testing.T) {
	dataDir = t.TempDir()
	a, e := newApp(dataDir)
	if e != nil {
		t.Fatal(e)
	}
	a.state.Pairs = []*Pair{newPair("default", "sample/repo", "sample/repo", "both")}
	a.state.Settings = Settings{GithubToken: "NEVER-RETURN-GH", GiteaToken: "NEVER-RETURN-GT"}
	r := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	a.handler(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r.Header.Set("X-Session-ID", "session")
	w = httptest.NewRecorder()
	a.handler(w, r)
	if strings.Contains(w.Body.String(), "NEVER-RETURN") {
		t.Fatal("secret leak")
	}
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["csrf"] == "" {
		t.Fatal(v)
	}
	r = httptest.NewRequest("POST", "/start", strings.NewReader(`{"id":"default"}`))
	r.Header.Set("X-Session-ID", "session")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.handler(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r.Header.Set("X-CSRF-Token", a.csrf("session"))
	w = httptest.NewRecorder()
	a.handler(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestPauseAndPersist(t *testing.T) {
	dataDir = t.TempDir()
	a, _ := newApp(dataDir)
	a.state.Pairs = []*Pair{newPair("default", "sample/repo", "sample/repo", "both")}
	a.state.Pairs[0].Enabled = true
	r := httptest.NewRequest("POST", "/pause", strings.NewReader(`{"id":"default"}`))
	r.Header.Set("X-Session-ID", "s")
	r.Header.Set("X-CSRF-Token", a.csrf("s"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handler(w, r)
	if w.Code != http.StatusOK || a.state.Pairs[0].Enabled {
		t.Fatal(w.Code)
	}
	b, e := newApp(dataDir)
	if e != nil || b.state.Pairs[0].Enabled {
		t.Fatal(e)
	}
	st, _ := os.Stat(filepath.Join(dataDir, "state.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}
func TestCorruptConfigFailsClosed(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "state.json"), []byte("broken"), 0600)
	if _, e := newApp(d); e == nil {
		t.Fatal("corrupt config accepted")
	}
}
func TestSafeErrorNoCredentials(t *testing.T) {
	s := safeError(os.ErrPermission)
	if strings.Contains(s, "permission") {
		t.Fatal(s)
	}
}
func TestEmptyRemote(t *testing.T) {
	d, _, a, _ := fixture(t)
	b := filepath.Join(d, "empty.git")
	gitcmd(t, d, "init", "--bare", b)
	v := runSync(t, d, a, b, map[string]bool{}, true)
	if v.Applied != 1 || gitcmd(t, a, "rev-parse", "master") != gitcmd(t, b, "rev-parse", "master") {
		t.Fatal(v)
	}
}
func TestDeletionLedgerSurvivesRestart(t *testing.T) {
	d, w, a, b := fixture(t)
	gitcmd(t, w, "branch", "extra")
	gitcmd(t, w, "push", a, "extra")
	v := runSync(t, d, a, b, map[string]bool{}, true)
	if v.Applied != 1 {
		t.Fatal(v)
	}
	gitcmd(t, b, "update-ref", "-d", "refs/heads/extra")
	v = runSync(t, d, a, b, map[string]bool{}, true)
	if v.Blocked != 1 || v.Applied != 0 {
		t.Fatal(v)
	}
}
