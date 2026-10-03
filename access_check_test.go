package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func gitAdvert(service string) *http.Response {
	p := "# service=" + service + "\n"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-" + service + "-advertisement"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf("%04x%s0000", len(p)+4, p)))}
}
func TestDiscoveryFiltersUnselectedAndReadOnlyEvenWithPushMetadata(t *testing.T) {
	h := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Body != nil {
			t.Error("probe attempted mutation")
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != "sample-user" || p != "fake-scoped-token" || strings.Contains(r.URL.String(), p) {
			t.Error("wrong token handling")
		}
		service := r.URL.Query().Get("service")
		if strings.Contains(r.URL.Path, "unselected") || strings.Contains(r.URL.Path, "read-only") && service == "git-receive-pack" {
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("denied"))}, nil
		}
		return gitAdvert(service), nil
	})}
	candidates := []Repo{{FullName: "sample/selected"}, {FullName: "sample/unselected"}, {FullName: "sample/read-only"}, {FullName: "sample/archived", Archived: true}}
	for i := range candidates {
		candidates[i].Permissions.Push = true
	}
	got, hidden, e := filterSyncRepos(context.Background(), h, "https://git.example.com", "sample-user", "fake-scoped-token", candidates)
	if e != nil || len(got) != 1 || got[0].FullName != "sample/selected" || !got[0].AccessChecked || hidden != 3 {
		t.Fatal(got, hidden, e)
	}
}
func TestAccessCheckRejectsHTMLRedirectAndNetworkErrors(t *testing.T) {
	for _, status := range []int{200, 302, 429, 500} {
		h := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("login page"))}, nil
		})}
		ok, _ := verifyGitAccess(context.Background(), h, "https://git.example.com/sample/repo.git", "sample", "fake")
		if ok {
			t.Fatal(status)
		}
	}
}
func TestAddRejectsStaleUnverifiedAndRevokedSelection(t *testing.T) {
	a := seeded(t)
	body := map[string]string{"github_repo": "me/alpha", "gitea_repo": "nas/alpha", "direction": "both"}
	a.repoVerifiedAt = time.Now().Add(-6 * time.Minute)
	if w := post(t, a, "/add", body); w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.repoVerifiedAt = time.Now()
	a.repos["github"][0].AccessChecked = false
	if w := post(t, a, "/add", body); w.Code != 400 {
		t.Fatal(w.Code)
	}
	a.repos["github"][0].AccessChecked = true
	a.verifyPair = func(context.Context, Settings, string, string) error { return fmt.Errorf("revoked") }
	if w := post(t, a, "/add", body); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if len(a.state.Pairs) != 1 {
		t.Fatal("invalid pair persisted")
	}
}
