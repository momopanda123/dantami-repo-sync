package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	ghttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

type RefPlan struct {
	Ref       string `json:"ref"`
	Direction string `json:"direction"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	Old       string `json:"-"`
	New       string `json:"-"`
	Target    string `json:"-"`
}
type SyncResult struct {
	Plan                      []RefPlan
	Seen                      map[string]bool
	Applied, Blocked, Pending int
}
type leaseKey struct{}
type lease struct {
	Ref      plumbing.ReferenceName
	Old, New plumbing.Hash
}
type guardedTransport struct{ transport.Transport }
type guardedReceive struct{ transport.ReceivePackSession }

func (t guardedTransport) NewReceivePackSession(e *transport.Endpoint, a transport.AuthMethod) (transport.ReceivePackSession, error) {
	s, err := t.Transport.NewReceivePackSession(e, a)
	if err != nil {
		return nil, err
	}
	return guardedReceive{s}, nil
}
func (s guardedReceive) ReceivePack(ctx context.Context, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error) {
	l, ok := ctx.Value(leaseKey{}).(lease)
	if !ok || len(req.Commands) != 1 {
		return nil, errors.New("push lease missing")
	}
	c := req.Commands[0]
	if c.Name != l.Ref || c.Old != l.Old || c.New != l.New || c.New.IsZero() {
		return nil, errors.New("target changed; no update sent")
	}
	return s.ReceivePackSession.ReceivePack(ctx, req)
}
func init() {
	hc := &http.Client{Timeout: 9 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client.InstallProtocol("https", guardedTransport{ghttp.NewClient(hc)})
	client.InstallProtocol("http", guardedTransport{ghttp.NewClient(hc)})
	// File transport is used solely by isolated local integration tests, never a UI endpoint.
	client.InstallProtocol("file", guardedTransport{client.Protocols["file"]})
}
func credentials(side, endpoint string, s Settings) transport.AuthMethod {
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		return nil
	}
	if side == "github" {
		return &ghttp.BasicAuth{Username: "x-access-token", Password: s.GithubToken}
	}
	user := s.GiteaUser
	if user == "" {
		user = "git"
	}
	return &ghttp.BasicAuth{Username: user, Password: s.GiteaToken}
}
func syncRepos(ctx context.Context, s Settings, seen map[string]bool, apply bool, dir, gh, gt string) (SyncResult, error) {
	result := SyncResult{Seen: seen, Plan: []RefPlan{}}
	ledgerPath := filepath.Join(dir, "seen.json")
	if raw, err := os.ReadFile(ledgerPath); err == nil {
		var saved map[string]bool
		if json.Unmarshal(raw, &saved) != nil {
			return result, errors.New("invalid ledger")
		}
		for k, v := range saved {
			seen[k] = v
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}
	cache := filepath.Join(dir, "cache.git")
	r, e := git.PlainOpen(cache)
	if errors.Is(e, git.ErrRepositoryNotExists) {
		r, e = git.PlainInit(cache, true)
	}
	if e != nil {
		return result, e
	}
	endpoints := map[string]string{"github": gh, "gitea": gt}
	snap := map[string]map[string]plumbing.Hash{}
	for _, side := range []string{"github", "gitea"} {
		if e = ctx.Err(); e != nil {
			return result, e
		}
		prefix := "refs/snapshots/" + side + "/"
		it, err := r.References()
		if err != nil {
			return result, err
		}
		var clear []plumbing.ReferenceName
		err = it.ForEach(func(ref *plumbing.Reference) error {
			if strings.HasPrefix(string(ref.Name()), prefix) {
				clear = append(clear, ref.Name())
			}
			return nil
		})
		if err != nil {
			return result, err
		}
		for _, n := range clear {
			if e = r.Storer.RemoveReference(n); e != nil {
				return result, e
			}
		}
		remote := git.NewRemote(r.Storer, &config.RemoteConfig{Name: side, URLs: []string{endpoints[side]}})
		e = remote.FetchContext(ctx, &git.FetchOptions{RemoteName: side, Auth: credentials(side, endpoints[side], s), RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/*:" + prefix + "heads/*"), config.RefSpec("+refs/tags/*:" + prefix + "tags/*")}, Tags: git.NoTags, Force: true})
		if e != nil && !errors.Is(e, git.NoErrAlreadyUpToDate) && !errors.Is(e, transport.ErrEmptyRemoteRepository) {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, errors.New(side)
		}
		shallow, e := r.Storer.Shallow()
		if e != nil || len(shallow) > 0 {
			return result, errors.New("shallow history")
		}
		snap[side] = map[string]plumbing.Hash{}
		it, e = r.References()
		if e != nil {
			return result, e
		}
		e = it.ForEach(func(ref *plumbing.Reference) error {
			n := string(ref.Name())
			if strings.HasPrefix(n, prefix) && ref.Type() == plumbing.HashReference {
				snap[side]["refs/"+strings.TrimPrefix(n, prefix)] = ref.Hash()
			}
			return nil
		})
		if e != nil {
			return result, e
		}
	}
	names := map[string]bool{}
	for _, side := range []string{"github", "gitea"} {
		for n := range snap[side] {
			names[n] = true
		}
	}
	sorted := []string{}
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		a, b := snap["github"][n], snap["gitea"][n]
		p := RefPlan{Ref: strings.TrimPrefix(n, "refs/"), Status: "same", Detail: "같은 상태예요"}
		switch {
		case a == b:
		case a.IsZero() || b.IsZero():
			from, to := "github", "gitea"
			new, old := a, b
			if a.IsZero() {
				from, to = "gitea", "github"
				new, old = b, a
			}
			if seen[to+":"+n] {
				p.Status = "blocked"
				p.Detail = "한쪽에서 삭제된 항목이에요. 삭제하거나 다시 만들지 않았어요"
			} else {
				p.Status = "pending"
				p.Direction = from + " → " + to
				p.Detail = "새 항목을 복사해요"
				p.Target = to
				p.Old = old.String()
				p.New = new.String()
			}
		case strings.HasPrefix(n, "refs/tags/"):
			p.Status = "blocked"
			p.Detail = "같은 이름의 태그 내용이 달라요. 덮어쓰지 않았어요"
		default:
			ca, ea := r.CommitObject(a)
			cb, eb := r.CommitObject(b)
			if ea != nil || eb != nil {
				return result, errors.New("missing commit")
			}
			ab, e := ca.IsAncestor(cb)
			if e != nil {
				return result, e
			}
			ba, e := cb.IsAncestor(ca)
			if e != nil {
				return result, e
			}
			if ab {
				p.Status = "pending"
				p.Target = "github"
				p.Direction = "gitea → github"
				p.Old = a.String()
				p.New = b.String()
				p.Detail = "NAS의 새 커밋을 반영해요"
			} else if ba {
				p.Status = "pending"
				p.Target = "gitea"
				p.Direction = "github → gitea"
				p.Old = b.String()
				p.New = a.String()
				p.Detail = "GitHub의 새 커밋을 반영해요"
			} else {
				p.Status = "blocked"
				p.Detail = "양쪽에 서로 다른 새 커밋이 있어요. 병합이 필요해요"
			}
		}
		if p.Status == "pending" && ((s.Direction == "github_to_gitea" && p.Target != "gitea") || (s.Direction == "gitea_to_github" && p.Target != "github")) {
			p.Status = "skipped"
			p.Detail = "선택한 동기화 방향의 반대쪽 변경이라 반영하지 않아요"
			p.Target = ""
		}
		if p.Status == "blocked" {
			result.Blocked++
		}
		if p.Status == "pending" {
			result.Pending++
		}
		result.Plan = append(result.Plan, p)
	}
	// Fail closed before any remote write if the objects require Git LFS transfer.
	if result.Pending > 0 {
		it, e := r.BlobObjects()
		if e != nil {
			return result, e
		}
		e = it.ForEach(func(b *object.Blob) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if b.Size > 2048 {
				return nil
			}
			reader, e := b.Reader()
			if e != nil {
				return e
			}
			raw, e := io.ReadAll(io.LimitReader(reader, 2048))
			reader.Close()
			if e != nil {
				return e
			}
			if strings.HasPrefix(string(raw), "version https://git-lfs.github.com/spec/") {
				return errors.New("lfs")
			}
			return nil
		})
		if e != nil {
			return result, e
		}
	}
	// Persist observed presence before writes: deletion on a later run must not be recreated.
	for _, side := range []string{"github", "gitea"} {
		for n := range snap[side] {
			result.Seen[side+":"+n] = true
		}
	}
	if err := writeJSON(ledgerPath, result.Seen); err != nil {
		return result, err
	}
	if apply {
		for i := range result.Plan {
			p := &result.Plan[i]
			if p.Status != "pending" {
				continue
			}
			ref := plumbing.ReferenceName("refs/" + p.Ref)
			old, new := plumbing.NewHash(p.Old), plumbing.NewHash(p.New)
			to := p.Target
			remote := git.NewRemote(r.Storer, &config.RemoteConfig{Name: to, URLs: []string{endpoints[to]}})
			pushCtx := context.WithValue(ctx, leaseKey{}, lease{ref, old, new})
			options := &git.PushOptions{RemoteName: to, Auth: credentials(to, endpoints[to], s), RefSpecs: []config.RefSpec{config.RefSpec(new.String() + ":" + string(ref))}}
			if !old.IsZero() {
				options.RequireRemoteRefs = []config.RefSpec{config.RefSpec(old.String() + ":" + string(ref))}
			}
			e = remote.PushContext(pushCtx, options)
			if e != nil && !errors.Is(e, git.NoErrAlreadyUpToDate) {
				p.Status = "blocked"
				p.Detail = "반영 확인을 못 했어요. 다음 확인에서 실제 상태를 다시 비교해요. 쓰기 권한·보호 규칙도 확인해 주세요"
				result.Blocked++
				continue
			}
			p.Status = "applied"
			p.Detail = "반영했어요"
			result.Applied++
			result.Seen[to+":"+string(ref)] = true
			if err := writeJSON(ledgerPath, result.Seen); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
