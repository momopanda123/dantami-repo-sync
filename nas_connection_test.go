package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEndpointAllowlist(t *testing.T) {
	for _, v := range []string{"https://git.example.com", "http://127.0.0.1:3000", "http://127.0.0.1:32123"} {
		if !allowedGiteaBase(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"http://evil.example", "https://user:pass@git.example.com", "http://127.0.0.1:22", "http://127.0.0.1:3000/a", "http://user:pw@127.0.0.1:3000", "http://127.0.0.1:3000?x=y", "http://localhost:3000", "http://192.168.1.1:3000"} {
		if allowedGiteaBase(v) {
			t.Fatal(v)
		}
	}
}
func TestLocalListenerParser(t *testing.T) {
	raw := "sl local_address rem_address st\n0: 00000000:0BB8 00000000:0000 0A\n1: 0100007F:2328 00000000:0000 0A\n2: 00000000:0016 00000000:0000 0A\n3: 00000000:3039 00000000:0000 01\n"
	v := localListenerPorts(raw)
	if len(v) != 2 || v[0] != 3000 || v[1] != 9000 {
		t.Fatal(v)
	}
}
func TestDetectSendsNoCredentials(t *testing.T) {
	n := 0
	h := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		n++
		if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Fatal("credential sent")
		}
		body := `{"version":"1.23.1"}`
		if r.URL.Path == "/swagger.v1.json" {
			body = `{"info":{"title":"Gitea API"},"paths":{"/user/repos":{}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	c, e := identifyGitea(context.Background(), h, "http://127.0.0.1:3000")
	if e != nil || c.Version != "1.23.1" || n != 2 {
		t.Fatal(c, e, n)
	}
}
func TestDetectRejectsGenericVersionService(t *testing.T) {
	h := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"1.0","info":{"title":"Other server"}}`))}, nil
	})}
	if _, e := identifyGitea(context.Background(), h, "http://127.0.0.1:3000"); e == nil {
		t.Fatal("false identity")
	}
}
func TestUnconfirmedEndpointCannotReceiveSavedToken(t *testing.T) {
	a := seeded(t)
	w := post(t, a, "/save", map[string]any{"github_token": "fake-gh", "gitea_token": "fake-gt", "gitea_base": "http://127.0.0.1:9999", "consent": true})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	if a.state.Settings.GiteaToken != "" {
		t.Fatal("credential saved to unknown endpoint")
	}
}
func TestGiteaUsernameConfig(t *testing.T) {
	s := Settings{GiteaUser: "configured-user", GiteaToken: "fake"}
	a := credentials("gitea", "http://127.0.0.1:3000/repo.git", s)
	if a == nil || a.Name() != "http-basic-auth" {
		t.Fatal(a)
	}
}
func TestDiagnosticsDoesNotClaimAbsentService(t *testing.T) {
	v := runtimeDiagnostics(t.TempDir())
	if len(v) != 5 {
		t.Fatal(v)
	}
	if v[3].OK || v[4].OK {
		t.Fatal("absent service marked working")
	}
}

func TestFreshInstallHasNoPersonalConfiguration(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if len(a.state.Pairs) != 0 || a.state.Settings.GiteaBase != "" || a.state.Settings.GiteaUser != "" || a.state.Settings.GithubToken != "" || a.state.Settings.GiteaToken != "" {
		t.Fatal("fresh install must be empty")
	}
}
func TestServerChangeCannotReuseSavedCredential(t *testing.T) {
	a := seeded(t)
	a.state.Settings.GiteaToken = "old-server-token"
	w := post(t, a, "/save", map[string]any{"gitea_base": "https://new.example.com", "github_token": "fake", "consent": true})
	if w.Code != 400 || a.state.Settings.GiteaBase != "https://git.example.com" {
		t.Fatal("credential destination changed without replacement token")
	}
}
func TestConfiguredServerSurvivesUpgrade(t *testing.T) {
	a := seeded(t)
	a.state.Settings.GiteaBase = "https://custom.example.net:8443"
	a.state.Settings.GiteaUser = "sample"
	a.state.Settings.GiteaToken = "private-test-value"
	if e := a.save(); e != nil {
		t.Fatal(e)
	}
	b, e := newApp(a.dir)
	if e != nil {
		t.Fatal(e)
	}
	if b.state.Settings.GiteaBase != a.state.Settings.GiteaBase || b.state.Settings.GiteaToken != a.state.Settings.GiteaToken || len(b.state.Pairs) != 1 {
		t.Fatal("existing private settings lost")
	}
}
