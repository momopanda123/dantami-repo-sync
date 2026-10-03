package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLanguagePreferenceAndDynamicMessages(t *testing.T) {
	r := httptest.NewRequest("GET", "https://nas.example/", nil)
	if requestLanguage(r) != "ko" {
		t.Fatal("default")
	}
	r.AddCookie(&http.Cookie{Name: "DantamiLanguage", Value: "en"})
	if requestLanguage(r) != "en" {
		t.Fatal("preference")
	}
	if v := translateText("완료 · 반영 3개 · 확인 필요 2개"); v != "Finished · 3 applied · 2 need review" {
		t.Fatal(v)
	}
	if v := translateText("Git 접속 확인 완료 · GitHub 1개, Gitea 2개 · 권한 미확인/보관 항목 5개 제외"); strings.Contains(v, "개") {
		t.Fatal(v)
	}
}
func TestLocalizationPreservesUserData(t *testing.T) {
	r := httptest.NewRequest("GET", "https://nas.example/", nil)
	r.AddCookie(&http.Cookie{Name: "DantamiLanguage", Value: "en"})
	w := httptest.NewRecorder()
	sendJSON(withLocale(w, r), map[string]any{"error": "로그인이 필요해요", "username": "정상", "full_name": "사용자/정상", "events": []map[string]string{{"message": "반영했어요"}}})
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["username"] != "정상" || v["full_name"] != "사용자/정상" || v["error"] != "Sign in to continue" {
		t.Fatal(v)
	}
}
func TestEnglishLoginAndErrors(t *testing.T) {
	x := authFixture(t)
	r := httptest.NewRequest("GET", "https://nas.example/", nil)
	r.AddCookie(&http.Cookie{Name: "DantamiLanguage", Value: "en"})
	w := httptest.NewRecorder()
	x.handler(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `lang="en"`) || !strings.Contains(w.Body.String(), "Sign in with your app account") {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "https://nas.example/status", nil)
	r.AddCookie(&http.Cookie{Name: "DantamiLanguage", Value: "en"})
	w = httptest.NewRecorder()
	x.handler(w, r)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Sign in to continue") {
		t.Fatal(w.Code, w.Body)
	}
	for _, asset := range []string{"login.js", "language.js"} {
		r = httptest.NewRequest("GET", "https://nas.example/asset?name="+asset, nil)
		r.AddCookie(&http.Cookie{Name: "DantamiLanguage", Value: "en"})
		w = httptest.NewRecorder()
		x.handler(w, r)
		if w.Code != 200 {
			t.Fatal(asset, w.Code)
		}
	}
}
