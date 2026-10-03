package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

func fetchRepos(ctx context.Context, h *http.Client, base, token, side string) ([]Repo, error) {
	repos := []Repo{}
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		url := fmt.Sprintf("%s/user/repos?page=%d&per_page=100&limit=100&sort=full_name&direction=asc", base, page)
		req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
		if e != nil {
			return nil, e
		}
		scheme := "Bearer "
		if side == "gitea" {
			scheme = "token "
		}
		req.Header.Set("Authorization", scheme+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Dantami-Repo-Sync/1.4.1")
		res, e := h.Do(req)
		if e != nil {
			return nil, errors.New(side)
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return nil, errors.New(side)
		}
		var batch []Repo
		e = json.NewDecoder(io.LimitReader(res.Body, 8*1024*1024)).Decode(&batch)
		res.Body.Close()
		if e != nil {
			return nil, errors.New(side)
		}
		if len(batch) == 0 {
			sort.Slice(repos, func(i, j int) bool { return repos[i].FullName < repos[j].FullName })
			return repos, nil
		}
		added := 0
		for _, r := range batch {
			if !repoName.MatchString(r.FullName) {
				continue
			}
			if !seen[r.FullName] {
				repos = append(repos, r)
				seen[r.FullName] = true
				added++
			}
		}
		if added == 0 {
			return nil, errors.New(side)
		}
	}
	return nil, errors.New("repository list too large")
}
func (a *App) discover() {
	a.discovering = true
	a.discoveryError = ""
	a.discoveryNotice = "현재 토큰의 Git 읽기·쓰기 접속을 확인하고 있어요"
	a.repos = map[string][]Repo{"github": {}, "gitea": {}}
	a.repoVerifiedAt = time.Time{}
	a.discoveryVersion++
	s := a.state.Settings
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		h := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		gh, e1 := fetchRepos(ctx, h, "https://api.github.com", s.GithubToken, "github")
		hiddenGH, hiddenGT := 0, 0
		if e1 == nil {
			gh, hiddenGH, e1 = filterSyncRepos(ctx, h, "https://github.com", "x-access-token", s.GithubToken, gh)
		}
		gt, e2 := fetchRepos(ctx, h, giteaBase(s)+"/api/v1", s.GiteaToken, "gitea")
		user := s.GiteaUser
		if user == "" {
			user = "git"
		}
		if e2 == nil {
			gt, hiddenGT, e2 = filterSyncRepos(ctx, h, giteaBase(s), user, s.GiteaToken, gt)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.discovering = false
		a.discoveryVersion++
		a.discoveryNotice = fmt.Sprintf("Git 접속 확인 완료 · GitHub %d개, Gitea %d개 · 권한 미확인/보관 항목 %d개 제외", len(gh), len(gt), hiddenGH+hiddenGT)
		if e1 == nil && e2 == nil {
			a.repoVerifiedAt = time.Now()
		} else {
			a.discoveryNotice = "접근 확인을 마치지 못했어요. 목록 새로고침으로 다시 확인해 주세요"
		}
		if e1 == nil {
			a.repos["github"] = gh
		}
		if e2 == nil {
			a.repos["gitea"] = gt
		}
		if e1 != nil {
			a.discoveryError = safeError(e1)
		}
		if e2 != nil {
			if a.discoveryError != "" {
				a.discoveryError += " / "
			}
			a.discoveryError += safeError(e2)
		}
	}()
}
