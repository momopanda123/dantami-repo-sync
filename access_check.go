package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Probe only Git service advertisements. No pack, ref update, or POST is sent.
// A successful advertisement is not a guarantee against branch protection or
// workflow-specific restrictions at the eventual push.
func verifyGitAccess(ctx context.Context, h *http.Client, endpoint, username, token string) (bool, error) {
	client := *h
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, service := range []string{"git-upload-pack", "git-receive-pack"} {
		req, e := http.NewRequestWithContext(ctx, "GET", endpoint+"/info/refs?service="+service, nil)
		if e != nil {
			return false, e
		}
		req.SetBasicAuth(username, token)
		req.Header.Set("Accept", "application/x-"+service+"-advertisement")
		res, e := client.Do(req)
		if e != nil {
			return false, fmt.Errorf("access check failed")
		}
		status := res.StatusCode
		typ := strings.ToLower(strings.TrimSpace(strings.Split(res.Header.Get("Content-Type"), ";")[0]))
		b, readErr := io.ReadAll(io.LimitReader(res.Body, 128))
		res.Body.Close()
		if status == 401 || status == 403 || status == 404 {
			return false, nil
		}
		if status != 200 || readErr != nil {
			return false, fmt.Errorf("access check incomplete")
		}
		payload := "# service=" + service + "\n"
		prefix := fmt.Sprintf("%04x%s0000", len(payload)+4, payload)
		if typ != "application/x-"+service+"-advertisement" || !strings.HasPrefix(string(b), prefix) {
			return false, nil
		}
	}
	return true, nil
}
func filterSyncRepos(ctx context.Context, h *http.Client, base, user, token string, candidates []Repo) ([]Repo, int, error) {
	if len(candidates) > 1000 {
		return nil, 0, fmt.Errorf("too many candidates")
	}
	result := make([]Repo, len(candidates))
	valid := make([]bool, len(candidates))
	var mu sync.Mutex
	var firstErr error
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				r := candidates[i]
				if r.Archived || !repoName.MatchString(r.FullName) {
					continue
				}
				ok, e := verifyGitAccess(ctx, h, strings.TrimRight(base, "/")+"/"+r.FullName+".git", user, token)
				if e != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = e
					}
					mu.Unlock()
					continue
				}
				if ok {
					r.AccessChecked = true
					result[i] = r
					valid[i] = true
				}
			}
		}()
	}
	for i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	if firstErr != nil {
		return nil, 0, firstErr
	}
	out := []Repo{}
	for i, r := range result {
		if valid[i] {
			out = append(out, r)
		}
	}
	return out, len(candidates) - len(out), nil
}

func verifyPairAccess(ctx context.Context, s Settings, gh, gt string) error {
	h := &http.Client{Timeout: 7 * time.Second}
	user := s.GiteaUser
	if user == "" {
		user = "git"
	}
	ok, e := verifyGitAccess(ctx, h, "https://github.com/"+gh+".git", "x-access-token", s.GithubToken)
	if e != nil || !ok {
		return fmt.Errorf("github")
	}
	ok, e = verifyGitAccess(ctx, h, giteaBase(s)+"/"+gt+".git", user, s.GiteaToken)
	if e != nil || !ok {
		return fmt.Errorf("gitea")
	}
	return nil
}
