package main

import (
	"context"

	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"net/url"
	"os"
	"path/filepath"

	"syscall"
	"time"
)

//go:embed web/*
var webFiles embed.FS

const packageName = "DantamiRepoSync"

var dataDir = "/var/packages/" + packageName + "/var/private"

func writeJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".save-")
	if e != nil {
		return e
	}
	n := f.Name()
	defer os.Remove(n)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(n, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		defer d.Close()
		e = d.Sync()
	}
	return e
}
func sendJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, s string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	sendJSON(w, map[string]string{"error": s})
}
func safeError(e error) string {
	if errors.Is(e, context.Canceled) {
		return "작업을 중지했어요. 이미 반영된 커밋은 유지돼요"
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return "연결 시간이 초과됐어요. 다음 확인 때 다시 시도해요"
	}
	// Never reflect transport errors, which can contain credentials or URLs.
	switch e.Error() {
	case "github":
		return "GitHub 연결 실패 · 토큰의 저장소 권한, 만료일, NAS 인터넷 연결을 확인해 주세요"
	case "gitea":
		return "Gitea 연결 실패 · 토큰 권한, 인증서 또는 NAS에서 도메인에 접속 가능한지 확인해 주세요"
	case "lfs":
		return "Git LFS 파일이 있어 자동 반영을 멈췄어요. LFS 데이터 전송이 별도로 필요해요"
	default:
		return "저장소 확인 중 오류가 발생했어요. 데이터는 강제로 덮어쓰지 않아요"
	}
}
func serveDaemon() error {
	if e := os.MkdirAll(dataDir, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(dataDir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("already running")
	}
	a, e := newApp(dataDir)
	if e != nil {
		return e
	}
	auth, e := newAuthApp(a)
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(dataDir, "service.pid"), os.Getpid()); e != nil {
		return e
	}
	defer os.Remove(filepath.Join(dataDir, "service.pid"))
	socket := filepath.Join(dataDir, "service.sock")
	os.Remove(socket)
	ln, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer ln.Close()
	defer os.Remove(socket)
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	go func() {
		tick := time.NewTicker(time.Second * 5)
		defer tick.Stop()
		for range tick.C {
			a.tick()
		}
	}()
	return (&http.Server{Handler: http.HandlerFunc(auth.handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}).Serve(ln)
}

var localServiceTransport http.RoundTripper = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dataDir, "service.sock"))
}}

func cgiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	route := "/"
	q := r.URL.Query()
	if q.Get("action") != "" {
		route = "/" + q.Get("action")
	} else if q.Get("asset") != "" {
		route = "/asset?name=" + url.QueryEscape(q.Get("asset"))
	}
	req, e := http.NewRequestWithContext(r.Context(), r.Method, "http://unix"+route, io.LimitReader(r.Body, 16384))
	if e != nil {
		fail(w, 400, "요청 오류")
		return
	}
	req.Host = r.Host
	for _, name := range []string{"Cookie", "Origin", "Content-Type", "X-CSRF-Token"} {
		req.Header.Set(name, r.Header.Get(name))
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: localServiceTransport}
	res, e := client.Do(req)
	if e != nil {
		fail(w, 503, "앱 서비스가 시작되지 않았어요. 패키지 센터에서 설치 시 관리자 계정 설정과 실행 상태를 확인해 주세요")
		return
	}
	defer res.Body.Close()
	for _, name := range []string{"Content-Type", "Cache-Control", "Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Set-Cookie", "Retry-After"} {
		for _, v := range res.Header.Values(name) {
			w.Header().Add(name, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	io.Copy(w, io.LimitReader(res.Body, 2*1024*1024))
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--init-admin" {
		if e := initializeAccounts(dataDir, os.Getenv("app_admin_user"), os.Getenv("app_admin_password"), os.Getenv("app_admin_confirm")); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}

	if os.Getenv("GATEWAY_INTERFACE") != "" {
		if e := cgi.Serve(http.HandlerFunc(cgiHandler)); e != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--data" {
		dataDir = os.Args[2]
	}
	if e := serveDaemon(); e != nil {
		fmt.Fprintln(os.Stderr, "Dantami Repo Sync service could not start:", e)
		os.Exit(1)
	}
}
