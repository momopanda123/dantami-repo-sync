package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCGIForwardsOnlyBrowserAuthenticationFields(t *testing.T) {
	old := localServiceTransport
	t.Cleanup(func() { localServiceTransport = old })
	localServiceTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Host != "nas.example" || r.URL.Path != "/auth/login" || r.Header.Get("X-Session-ID") != "" || r.Header.Get("X-App-User") != "" || r.Header.Get("Cookie") != "test=value" {
			t.Fatal("untrusted identity forwarded", r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": []string{"test=session; Secure; HttpOnly"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})
	r := httptest.NewRequest("POST", "https://nas.example/index.cgi?action=auth/login", strings.NewReader(`{}`))
	r.Header.Set("Cookie", "test=value")
	r.Header.Set("X-App-User", "admin")
	r.Header.Set("X-Session-ID", "spoof")
	w := httptest.NewRecorder()
	cgiHandler(w, r)
	if w.Code != 200 || w.Header().Get("Set-Cookie") == "" {
		t.Fatal(w.Code)
	}
}
