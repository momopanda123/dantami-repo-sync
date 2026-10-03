package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

var localeOnce sync.Once
var englishMessages map[string]string

type messagePattern struct {
	expression *regexp.Regexp
	english    string
}

var messagePatterns []messagePattern

func loadMessages() {
	b, e := webFiles.ReadFile("web/i18n.json")
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(b, &englishMessages); e != nil {
		panic(e)
	}
	for k, v := range englishMessages {
		if strings.Contains(k, "%d") {
			messagePatterns = append(messagePatterns, messagePattern{regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(k), "%d", `([0-9]+)`) + "$"), v})
		}
	}
}
func translateText(s string) string {
	localeOnce.Do(loadMessages)
	if v, ok := englishMessages[s]; ok {
		return v
	}
	for _, p := range messagePatterns {
		if m := p.expression.FindStringSubmatch(s); m != nil {
			out := p.english
			for _, n := range m[1:] {
				out = strings.Replace(out, "%d", n, 1)
			}
			return out
		}
	}
	return s
}
func requestLanguage(r *http.Request) string {
	if c, e := r.Cookie("DantamiLanguage"); e == nil && c.Value == "en" {
		return "en"
	}
	return "ko"
}

type localeWriter struct {
	http.ResponseWriter
	language string
}

func withLocale(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	return &localeWriter{w, requestLanguage(r)}
}
func localizedJSON(w http.ResponseWriter, v any) any {
	lw, ok := w.(*localeWriter)
	if !ok || lw.language != "en" {
		return v
	}
	b, e := json.Marshal(v)
	if e != nil {
		return v
	}
	var value any
	if json.Unmarshal(b, &value) != nil {
		return v
	}
	var walk func(any, string) any
	keys := map[string]bool{"error": true, "warning": true, "message": true, "detail": true, "title": true, "kind": true, "discovery_error": true, "discovery_notice": true, "detect_message": true}
	walk = func(x any, key string) any {
		switch t := x.(type) {
		case string:
			if keys[key] {
				return translateText(t)
			}
			return t
		case []any:
			for i := range t {
				t[i] = walk(t[i], key)
			}
		case map[string]any:
			for k, v := range t {
				t[k] = walk(v, k)
			}
		}
		return x
	}
	return walk(value, "")
}
