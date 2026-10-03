package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultGiteaBase = ""

type NASConnection struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

func giteaBase(s Settings) string {
	if s.GiteaBase == "" {
		return defaultGiteaBase
	}
	return s.GiteaBase
}
func allowedGiteaBase(raw string) bool {

	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path != "" || u.Hostname() == "" || strings.ContainsAny(raw, " \t\r\n\\") {
		return false
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return false
		}
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		return false
	}
	p, e := strconv.Atoi(u.Port())
	return e == nil && p >= 1024 && p <= 65535 && u.Host == net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
}

// Read only the NAS's own TCP listeners. No LAN scanning or Docker socket access.
func localListenerPorts(raw string) []int {
	found := map[int]bool{3000: true}
	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		parts := strings.Split(f[1], ":")
		if len(parts) != 2 {
			continue
		}
		p, e := strconv.ParseInt(parts[1], 16, 32)
		if e == nil && p >= 1024 && p <= 65535 {
			found[int(p)] = true
		}
	}
	ports := []int{}
	for p := range found {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	if len(ports) > 64 {
		ports = ports[:64]
	}
	return ports
}
func identifyGitea(ctx context.Context, h *http.Client, base string) (NASConnection, error) {
	if !allowedGiteaBase(base) {
		return NASConnection{}, errors.New("invalid endpoint")
	}
	get := func(path string, out any) error {
		r, e := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if e != nil {
			return e
		}
		r.Header.Set("Accept", "application/json")
		resp, e := h.Do(r)
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("not gitea")
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(out)
	}
	var version struct {
		Version string `json:"version"`
	}
	if e := get("/api/v1/version", &version); e != nil || version.Version == "" || len(version.Version) > 100 {
		return NASConnection{}, errors.New("not gitea")
	}
	var spec struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if e := get("/swagger.v1.json", &spec); e != nil || !strings.Contains(strings.ToLower(spec.Info.Title), "gitea") || spec.Paths["/user/repos"] == nil {
		return NASConnection{}, errors.New("identity not confirmed")
	}
	kind := "NAS 내부 연결"
	if strings.HasPrefix(base, "https://") {
		kind = "기존 Gitea 주소"
	}
	return NASConnection{base, version.Version, kind}, nil
}
func (a *App) detectNAS() {
	a.detecting = true
	configuredBase := giteaBase(a.state.Settings)
	a.detectMessage = "NAS에서 연결 가능한 Gitea를 확인하고 있어요. 접근 토큰은 보내지 않아요"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		raw, _ := os.ReadFile("/proc/net/tcp")
		raw6, _ := os.ReadFile("/proc/net/tcp6")
		ports := localListenerPorts(string(raw) + "\n" + string(raw6))
		bases := []string{}
		if configuredBase != "" {
			bases = append(bases, configuredBase)
		}
		for _, p := range ports {
			bases = append(bases, fmt.Sprintf("http://127.0.0.1:%d", p))
		}
		h := &http.Client{Timeout: 1200 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		found := []NASConnection{}
		for _, base := range bases {
			if ctx.Err() != nil {
				break
			}
			if c, e := identifyGitea(ctx, h, base); e == nil {
				found = append(found, c)
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.detecting = false
		a.nasConnections = found
		a.detectVersion++
		if len(found) == 0 {
			a.detectMessage = "Gitea를 자동으로 확인하지 못했어요. 기존 주소는 유지했으며 토큰을 다른 주소로 보내지 않았어요. Docker가 NAS에 포트를 공개하지 않으면 내부 자동탐색이 제한될 수 있어요"
		} else {
			a.detectMessage = fmt.Sprintf("Gitea 연결 %d개를 찾았어요. 사용할 연결을 선택한 뒤 연결 저장을 눌러 주세요", len(found))
		}
	}()
}
