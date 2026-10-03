package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, a *App, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
	r.Header.Set("X-Session-ID", "test-session")
	r.Header.Set("X-CSRF-Token", a.csrf("test-session"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handler(w, r)
	return w
}
func seeded(t *testing.T) *App {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.state.Pairs = []*Pair{newPair("default", "sample/legacy", "sample/legacy", "both")}
	a.state.Settings.GiteaBase = "https://git.example.com"
	a.verifyPair = func(context.Context, Settings, string, string) error { return nil }
	a.repoVerifiedAt = time.Now()
	a.repos["github"] = []Repo{{FullName: "me/alpha", AccessChecked: true}, {FullName: "me/beta", AccessChecked: true}}
	a.repos["gitea"] = []Repo{{FullName: "nas/alpha", AccessChecked: true}, {FullName: "nas/beta", AccessChecked: true}}
	return a
}
func TestManagerAddDuplicateArchiveRestore(t *testing.T) {
	a := seeded(t)
	body := map[string]any{"github_repo": "me/alpha", "gitea_repo": "nas/alpha", "direction": "both"}
	if w := post(t, a, "/add", body); w.Code != 200 {
		t.Fatal(w.Body)
	}
	id := a.state.Pairs[1].ID
	if w := post(t, a, "/add", body); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := post(t, a, "/archive", map[string]string{"id": id}); w.Code != 200 || !a.state.Pairs[1].Archived {
		t.Fatal(w.Code)
	}
	if w := post(t, a, "/restore", map[string]string{"id": id}); w.Code != 200 || a.state.Pairs[1].Archived {
		t.Fatal(w.Code)
	}
	b, e := newApp(a.dir)
	if e != nil || len(b.state.Pairs) != 2 {
		t.Fatal(e)
	}
}
func TestManagerRejectUnknownRepoAndTraversal(t *testing.T) {
	a := seeded(t)
	for _, name := range []string{"me/missing", "../../etc", "https://evil.example/repo"} {
		w := post(t, a, "/add", map[string]string{"github_repo": name, "gitea_repo": "nas/alpha", "direction": "both"})
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if w := post(t, a, "/update", map[string]any{"id": "default", "direction": "both", "interval": 0}); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestManagerUpdatePausesAndRechecks(t *testing.T) {
	a := seeded(t)
	p := a.state.Pairs[0]
	p.Enabled = true
	p.Checked = true
	w := post(t, a, "/update", map[string]any{"id": "default", "direction": "github_to_gitea", "interval": 15})
	if w.Code != 200 || p.Enabled || p.Checked || p.Direction != "github_to_gitea" || p.Interval != 15 {
		t.Fatal(w.Code, p)
	}
	if w := post(t, a, "/sync", map[string]string{"id": "default"}); w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestManagerNoCredentialLeak(t *testing.T) {
	a := seeded(t)
	a.state.Settings.GithubToken = "secret-gh-value"
	a.state.Settings.GiteaToken = "secret-gt-value"
	r := httptest.NewRequest("GET", "/status", nil)
	r.Header.Set("X-Session-ID", "test")
	w := httptest.NewRecorder()
	a.handler(w, r)
	if strings.Contains(w.Body.String(), "secret-") {
		t.Fatal("credential leak")
	}
}
func TestLegacyMigrationPausesWithoutGuessingRepository(t *testing.T) {
	d := t.TempDir()
	writeJSON(filepath.Join(d, "state.json"), map[string]any{"settings": map[string]any{"github_token": "g", "gitea_token": "t", "enabled": true}, "seen": map[string]bool{"gitea:refs/heads/feature": true}})
	writeJSON(filepath.Join(d, "seen.json"), map[string]bool{"gitea:refs/heads/deleted": true})
	a, e := newApp(d)
	if e != nil {
		t.Fatal(e)
	}
	if a.state.Version != 3 || len(a.state.Pairs) != 0 || a.state.Settings.Enabled {
		t.Fatal(a.state)
	}
	b, e := os.ReadFile(filepath.Join(d, "seen.json"))
	if e != nil || !strings.Contains(string(b), "deleted") {
		t.Fatal(e, string(b))
	}
	if _, e = newApp(d); e != nil {
		t.Fatal(e)
	}
}
func TestOneWayDoesNotPushBack(t *testing.T) {
	d, w, a, b := fixture(t)
	change(t, w, b, "nas change")
	before := gitcmd(t, a, "rev-parse", "master")
	v, e := syncRepos(context.Background(), Settings{Direction: "github_to_gitea"}, map[string]bool{}, true, d, a, b)
	if e != nil || v.Applied != 0 || v.Plan[0].Status != "skipped" || gitcmd(t, a, "rev-parse", "master") != before {
		t.Fatal(e, v)
	}
}
func TestOneWayForward(t *testing.T) {
	for _, direction := range []string{"github_to_gitea", "gitea_to_github"} {
		t.Run(direction, func(t *testing.T) {
			d, w, a, b := fixture(t)
			source := a
			if direction == "gitea_to_github" {
				source = b
			}
			change(t, w, source, "new")
			v, e := syncRepos(context.Background(), Settings{Direction: direction}, map[string]bool{}, true, d, a, b)
			if e != nil || v.Applied != 1 {
				t.Fatal(e, v)
			}
		})
	}
}
func TestPairIsolation(t *testing.T) {
	d1, w1, a1, b1 := fixture(t)
	d2, w2, a2, b2 := fixture(t)
	change(t, w1, a1, "one")
	change(t, w2, b2, "two")
	v1 := runSync(t, d1, a1, b1, map[string]bool{}, true)
	v2 := runSync(t, d2, a2, b2, map[string]bool{}, true)
	if v1.Applied != 1 || v2.Applied != 1 || gitcmd(t, a1, "rev-parse", "master") == gitcmd(t, a2, "rev-parse", "master") {
		t.Fatal(v1, v2)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDiscoveryPaginationShortServerCap(t *testing.T) {
	pages := 0
	h := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		pages++
		if r.Header.Get("Authorization") != "token private-test" || strings.Contains(r.URL.String(), "private-test") {
			t.Fatal("token handling")
		}
		body := `[]`
		if pages == 1 {
			body = `[{"full_name":"nas/one"}]`
		}
		if pages == 2 {
			body = `[{"full_name":"nas/two"},{"full_name":"../../bad"}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	v, e := fetchRepos(context.Background(), h, "https://example.invalid/api/v1", "private-test", "gitea")
	if e != nil || len(v) != 2 || pages != 3 {
		t.Fatal(e, v, pages)
	}
}
func TestDiscoveryFailureIsSanitized(t *testing.T) {
	h := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret-token"))}, nil
	})}
	_, e := fetchRepos(context.Background(), h, "https://example.invalid", "private-token", "github")
	if e == nil || e.Error() != "github" {
		t.Fatal(e)
	}
}
func TestQueueLimitAndPause(t *testing.T) {
	a := seeded(t)
	a.state.Pairs[0].Busy = true
	a.state.Pairs = append(a.state.Pairs, newPair("111111111111111111111111", "me/a", "nas/a", "both"), newPair("222222222222222222222222", "me/b", "nas/b", "both"))
	a.state.Pairs[1].Busy = true
	p := a.state.Pairs[2]
	p.Checked = true
	w := post(t, a, "/sync", map[string]string{"id": p.ID})
	if w.Code != 200 || p.Busy || p.Queued != "sync" {
		t.Fatal(w.Code, p)
	}
	w = post(t, a, "/pause", map[string]string{"id": p.ID})
	if w.Code != 200 || p.Queued != "" {
		t.Fatal(w.Code, p)
	}
}
func TestWorkerUpdatesOnlyOwnPair(t *testing.T) {
	a := seeded(t)
	a.run = func(ctx context.Context, s Settings, m map[string]bool, apply bool, d, gh, gt string) (SyncResult, error) {
		return SyncResult{Plan: []RefPlan{{Ref: "heads/master", Status: "same"}}, Seen: m}, nil
	}
	p := a.state.Pairs[0]
	a.state.Pairs = append(a.state.Pairs, newPair("111111111111111111111111", "me/a", "nas/a", "both"))
	a.mu.Lock()
	a.begin(p, false)
	a.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		done := !p.Busy
		if done {
			if !p.Checked || p.Phase != "healthy" || a.state.Pairs[1].Checked {
				t.Fatal("cross-pair state")
			}
		}
		a.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestUpgradeRemovesLegacySeedButKeepsUserPairs(t *testing.T) {
	d := t.TempDir()
	custom := newPair("0123456789abcdef01234567", "sample/custom", "sample/custom", "both")
	old := State{Version: 2, Settings: Settings{GiteaBase: "https://git.example.com"}, Pairs: []*Pair{newPair("default", "sample/old-seed", "sample/old-seed", "both"), custom}}
	if e := writeJSON(filepath.Join(d, "state.json"), old); e != nil {
		t.Fatal(e)
	}
	a, e := newApp(d)
	if e != nil {
		t.Fatal(e)
	}
	if a.state.Version != 3 || len(a.state.Pairs) != 1 || a.state.Pairs[0].ID != custom.ID {
		t.Fatal("seed retained or user pair lost")
	}
	b, e := os.ReadFile(filepath.Join(d, "legacy-default-backup.json"))
	if e != nil || !strings.Contains(string(b), "old-seed") {
		t.Fatal("legacy backup missing", e)
	}
	again, e := newApp(d)
	if e != nil || len(again.state.Pairs) != 1 {
		t.Fatal("migration not idempotent", e)
	}
}
func TestRemoveRequiresConfirmationAndCleansOnlyOwnCache(t *testing.T) {
	a := seeded(t)
	cache := filepath.Join(a.dir, "pairs", "default")
	other := filepath.Join(a.dir, "pairs", "another")
	os.MkdirAll(cache, 0700)
	os.MkdirAll(other, 0700)
	os.WriteFile(filepath.Join(cache, "seen.json"), []byte("{}"), 0600)
	if w := post(t, a, "/delete", map[string]any{"id": "default"}); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if len(a.state.Pairs) != 1 {
		t.Fatal("deleted before consent")
	}
	a.state.Pairs[0].Busy = true
	if w := post(t, a, "/delete", map[string]any{"id": "default", "consent": true}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.state.Pairs[0].Busy = false
	if w := post(t, a, "/delete", map[string]any{"id": "default", "consent": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if len(a.state.Pairs) != 0 {
		t.Fatal("pair retained")
	}
	if _, e := os.Stat(cache); !os.IsNotExist(e) {
		t.Fatal("cache retained")
	}
	if _, e := os.Stat(other); e != nil {
		t.Fatal("unrelated cache removed")
	}
	b, e := newApp(a.dir)
	if e != nil || len(b.state.Pairs) != 0 {
		t.Fatal("removed pair returned", e)
	}
	if w := post(t, a, "/delete", map[string]any{"id": "../../other", "consent": true}); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestNewConnectionIntervalPreference(t *testing.T) {
	for _, interval := range []int{0, 1, 3, 5, 15, 60, 1440, -1, 1441} {
		t.Run(fmt.Sprint(interval), func(t *testing.T) {
			a := seeded(t)
			old := a.state.Pairs[0].Interval
			w := post(t, a, "/add", map[string]any{"github_repo": "me/alpha", "gitea_repo": "nas/alpha", "direction": "github_to_gitea", "interval": interval})
			if interval < 0 || interval > 1440 {
				if w.Code != 400 || len(a.state.Pairs) != 1 {
					t.Fatal(w.Code, w.Body)
				}
				return
			}
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			want := interval
			if want == 0 {
				want = 3
			}
			p := a.state.Pairs[1]
			if p.Interval != want || p.Enabled || p.Checked || p.Direction != "github_to_gitea" || a.state.Pairs[0].Interval != old {
				t.Fatal(p)
			}
			loaded, err := newApp(a.dir)
			if err != nil || loaded.state.Pairs[1].Interval != want {
				t.Fatal(err)
			}
		})
	}
}
