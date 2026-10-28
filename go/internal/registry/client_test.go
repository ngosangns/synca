package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func client(t *testing.T, h http.HandlerFunc, token string) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, DownloadURL: srv.URL, Token: token, HTTP: srv.Client(), UserAgent: "test"}
}

func kindOf(err error) Kind {
	var re *Error
	if errors.As(err, &re) {
		return re.Kind
	}
	return 0
}

func TestSearchMapsPublicResponseAndBuildsQuery(t *testing.T) {
	var got string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path + "?" + r.URL.RawQuery
		if r.Header.Get("Authorization") != "" {
			t.Error("public search sent a token")
		}
		_, _ = w.Write([]byte(`{"query":"pdf","searchType":"fuzzy","skills":[
			{"id":"anthropics/skills/pdf","source":"anthropics/skills","skillId":"pdf","name":"pdf","installs":5},
			{"id":"mintlify.com/mintlify","source":"mintlify.com","skillId":"mintlify","name":"","installs":2},
			{"id":"broken","source":"","skillId":"","name":"x","installs":1}]}`))
	}, "tok")
	res, err := c.Search(context.Background(), " pdf ", "Anthropics", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/api/search?limit=7&owner=anthropics&q=pdf" {
		t.Errorf("request %s", got)
	}
	if res.Count != 2 || len(res.Skills) != 2 {
		t.Fatalf("skills %+v", res.Skills)
	}
	a, b := res.Skills[0], res.Skills[1]
	if a.ID != "anthropics/skills/pdf" || a.SourceType != "github" || *a.InstallURL != "https://github.com/anthropics/skills" || a.URL != "https://skills.sh/anthropics/skills/pdf" {
		t.Errorf("github skill %+v", a)
	}
	if b.SourceType != "well-known" || b.Name != "mintlify" || *b.InstallURL != "https://mintlify.com" {
		t.Errorf("well-known skill %+v", b)
	}
}

func TestSearchRejectsBadInputBeforeTheNetwork(t *testing.T) {
	c := client(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") }, "")
	for name, call := range map[string]func() error{
		"short query":  func() error { _, err := c.Search(context.Background(), "a", "", 0); return err },
		"big limit":    func() error { _, err := c.Search(context.Background(), "ab", "", 201); return err },
		"bad owner":    func() error { _, err := c.Search(context.Background(), "ab", "-x/../y", 0); return err },
		"bad view":     func() error { _, err := c.Browse(context.Background(), "weekly", 0, ""); return err },
		"negative pg":  func() error { _, err := c.Browse(context.Background(), "hot", -1, ""); return err },
		"bad id":       func() error { _, err := c.Info(context.Background(), "a/../b", false); return err },
		"bad id audit": func() error { _, err := c.Audits(context.Background(), "a b/c"); return err },
	} {
		if err := call(); kindOf(err) != KindUsage {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBrowseReadsLeaderboardPages(t *testing.T) {
	var path string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"skills":[{"source":"vercel-labs/skills","skillId":"find-skills","name":"find-skills","installs":9,"weeklyInstalls":[1,2],"isOfficial":true,"installsYesterday":3,"change":6}],"total":42,"hasMore":true,"page":2}`))
	}, "")
	res, err := c.Browse(context.Background(), "hot", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/skills/hot/2" {
		t.Errorf("path %s", path)
	}
	s := res.Skills[0]
	if !res.HasMore || res.Total != 42 || res.PerPage != 1 || !s.Official || s.WeeklyInstalls[1] != 2 || *s.Change != 6 || *s.InstallsYesterday != 3 {
		t.Errorf("result %+v skill %+v", res, s)
	}
}

func TestTokenOnlyEndpointsFailWithoutATokenAndSendItWithOne(t *testing.T) {
	calls := 0
	h := func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.URL.Path == "/api/v1/skills/curated":
			_, _ = w.Write([]byte(`{"data":[{"owner":"vercel-labs","totalInstalls":5,"featuredRepo":"skills","featuredSkill":"find-skills","skills":[{"id":"vercel-labs/skills/find-skills","slug":"find-skills","name":"find-skills","source":"vercel-labs/skills","installs":4,"sourceType":"github","installUrl":"https://github.com/vercel-labs/skills","url":"https://skills.sh/vercel-labs/skills/find-skills"}]}],"totalOwners":1,"totalSkills":1,"generatedAt":"2026-03-31T08:00:00.000Z"}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/skills/audit/"):
			_, _ = w.Write([]byte(`{"id":"a/b/c","source":"a/b","slug":"c","audits":[{"provider":"Socket","slug":"socket","status":"pass","summary":"ok","auditedAt":"x"}]}`))
		case r.URL.Path == "/api/v1/skills/a/b/c":
			_, _ = w.Write([]byte(`{"id":"a/b/c","source":"a/b","slug":"c","installs":12,"hash":"h","files":[{"path":"SKILL.md","contents":"hello"}]}`))
		case r.URL.Path == "/api/v1/skills":
			if r.URL.Query().Get("source") != "a/b" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"a/b/c","slug":"c","name":"c","source":"a/b","installs":1,"sourceType":"github","url":"u","isDuplicate":true}],"pagination":{"page":0,"perPage":100,"total":1,"hasMore":false}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}
	anon := client(t, func(http.ResponseWriter, *http.Request) { t.Error("anonymous request sent") }, "")
	if _, err := anon.Official(context.Background()); kindOf(err) != KindUnauthorized {
		t.Errorf("official: %v", err)
	}
	if _, err := anon.Browse(context.Background(), "all-time", 0, "a/b"); kindOf(err) != KindUnauthorized {
		t.Errorf("browse source: %v", err)
	}
	if _, err := anon.Info(context.Background(), "mintlify.com/mintlify", false); kindOf(err) != KindUnauthorized {
		t.Errorf("well-known info: %v", err)
	}

	c := client(t, h, "tok")
	off, err := c.Official(context.Background())
	if err != nil || !off.Owners[0].Skills[0].Official || off.TotalSkills != 1 {
		t.Fatalf("official %+v %v", off, err)
	}
	au, err := c.Audits(context.Background(), "a/b/c")
	if err != nil || au.Audits[0].Status != "pass" {
		t.Fatalf("audits %+v %v", au, err)
	}
	info, err := c.Info(context.Background(), "a/b/c", true)
	if err != nil || *info.Installs != 12 || *info.Files[0].Contents != "hello" || info.Files[0].Size != 5 {
		t.Fatalf("info %+v %v", info, err)
	}
	br, err := c.Browse(context.Background(), "all-time", 0, "a/b")
	if err != nil || !br.Skills[0].Duplicate {
		t.Fatalf("browse source %+v %v", br, err)
	}
	if calls != 4 {
		t.Errorf("calls %d", calls)
	}
}

func TestInfoWithoutTokenUsesPublicDownloadAndHidesContentsByDefault(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/download/a/b/c" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"files":[{"path":"SKILL.md","contents":"hello"}],"hash":"h"}`))
	}, "")
	info, err := c.Info(context.Background(), "a/b/c", false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Installs != nil || info.Source != "a/b" || info.Slug != "c" || info.Files[0].Contents != nil || info.Files[0].Size != 5 {
		t.Errorf("info %+v", info)
	}
}

func TestAuditNotFoundIsAnEmptyList(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found","message":"no audits"}`))
	}, "tok")
	res, err := c.Audits(context.Background(), "a/b/c")
	if err != nil || res.Audits == nil || len(res.Audits) != 0 || res.Source != "a/b" || res.Slug != "c" {
		t.Errorf("res %+v err %v", res, err)
	}
}

func TestStatusCodesMapToKinds(t *testing.T) {
	for status, want := range map[int]Kind{
		http.StatusBadRequest:          KindUsage,
		http.StatusUnauthorized:        KindUnauthorized,
		http.StatusNotFound:            KindNotFound,
		http.StatusTooManyRequests:     KindUnavailable,
		http.StatusServiceUnavailable:  KindUnavailable,
		http.StatusInternalServerError: KindUnavailable,
	} {
		c := client(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "9")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"e","message":"m"}`))
		}, "tok")
		_, err := c.Info(context.Background(), "a/b/c", false)
		if kindOf(err) != want {
			t.Errorf("%d: %v", status, err)
		}
		if status == http.StatusTooManyRequests {
			var re *Error
			errors.As(err, &re)
			if !strings.Contains(re.Hint, "9") {
				t.Errorf("hint %q", re.Hint)
			}
		}
	}
}

func TestGarbageBodyIsUnavailable(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) }, "")
	if _, err := c.Browse(context.Background(), "hot", 0, ""); kindOf(err) != KindUnavailable {
		t.Errorf("err %v", err)
	}
}

func TestUnreachableHostIsUnavailable(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", DownloadURL: "http://127.0.0.1:1", HTTP: &http.Client{}}
	if _, err := c.Search(context.Background(), "ab", "", 0); kindOf(err) != KindUnavailable {
		t.Errorf("err %v", err)
	}
}

func TestNewReadsEnvironment(t *testing.T) {
	t.Setenv("SKILLS_API_URL", "http://example.test/")
	t.Setenv("SKILLS_DOWNLOAD_URL", "")
	t.Setenv("SKILLS_API_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN", " oidc ")
	c := New("1.2.3")
	if c.BaseURL != "http://example.test" || c.DownloadURL != "http://example.test" || c.Token != "oidc" || c.UserAgent != "synca/1.2.3" {
		t.Errorf("client %+v", c)
	}
	t.Setenv("SKILLS_API_TOKEN", "own")
	if New("x").Token != "own" {
		t.Error("SKILLS_API_TOKEN does not win")
	}
}

func TestSplitID(t *testing.T) {
	for _, ok := range []string{"a/b/c", "mintlify.com/mintlify", "/a/b/c/"} {
		if _, err := SplitID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a", "a/b/c/d", "a/../c", "a/./c", "a/b c/d", "a//c", "a/b/c?x=1"} {
		if _, err := SplitID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
