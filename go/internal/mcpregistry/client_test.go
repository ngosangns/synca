package mcpregistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: "synca/test"}
}

func kindOf(err error) Kind {
	var re *Error
	if errors.As(err, &re) {
		return re.Kind
	}
	return 0
}

const listEnvelope = `{"servers":[{"server":{"name":"io.x/y","title":"Y","description":"d","version":"1.0.0",
"repository":{"url":"https://github.com/x/y","source":"github"},
"packages":[{"registryType":"npm","identifier":"@x/y","version":"1.0.0","transport":{"type":"stdio"}}]},
"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true,"statusChangedAt":"2025-01-01T00:00:00Z","publishedAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z"}}}],
"metadata":{"count":1,"nextCursor":"io.x/y:1.0.0"}}`

func TestSearchSendsQueryAndParsesMetadata(t *testing.T) {
	var got url.Values
	var path string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		got = r.URL.Query()
		_, _ = w.Write([]byte(listEnvelope))
	})
	res, err := c.Search(context.Background(), " filesystem ", ListOptions{Cursor: "abc", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v0.1/servers" {
		t.Errorf("path %s", path)
	}
	if got.Get("search") != "filesystem" || got.Get("limit") != "5" || got.Get("cursor") != "abc" || got.Get("version") != "latest" {
		t.Errorf("query %v", got)
	}
	if res.Metadata.NextCursor != "io.x/y:1.0.0" || len(res.Servers) != 1 {
		t.Fatalf("response %+v", res)
	}
	rec := res.Servers[0]
	if rec.Server.Name != "io.x/y" || rec.Server.Repository == nil || rec.Server.Repository.URL != "https://github.com/x/y" {
		t.Errorf("server %+v", rec.Server)
	}
	if rec.Meta.Official == nil || !rec.Meta.Official.IsLatest || rec.Meta.Official.Status != "active" {
		t.Errorf("meta %+v", rec.Meta)
	}
}

func TestListOmitsSearchAndDefaultsLimit(t *testing.T) {
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"servers":[],"metadata":{}}`))
	})
	res, err := c.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["search"]; ok {
		t.Errorf("browse sent search: %v", got)
	}
	if got.Get("limit") != "30" || got.Get("version") != "latest" {
		t.Errorf("query %v", got)
	}
	if res.Servers == nil || len(res.Servers) != 0 {
		t.Fatalf("servers %#v", res.Servers)
	}
}

func TestInfoEscapesNameAndDefaultsToLatest(t *testing.T) {
	var escaped, raw string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		escaped, raw = r.URL.EscapedPath(), r.URL.Path
		_, _ = w.Write([]byte(`{"server":{"name":"ai.2kw/mcp-server","version":"8.0.3","description":"d"}}`))
	})
	rec, err := c.Info(context.Background(), "ai.2kw/mcp-server", "")
	if err != nil {
		t.Fatal(err)
	}
	if escaped != "/v0.1/servers/ai.2kw%2Fmcp-server/versions/latest" {
		t.Errorf("escaped path %s", escaped)
	}
	if raw != "/v0.1/servers/ai.2kw/mcp-server/versions/latest" {
		t.Errorf("path %s", raw)
	}
	if rec.Server.Version != "8.0.3" {
		t.Errorf("server %+v", rec.Server)
	}
}

func TestVersionPathIsEscaped(t *testing.T) {
	var escaped string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		escaped = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"server":{"name":"io.x/y","version":"1.0 beta","description":"d"}}`))
	})
	if _, err := c.Info(context.Background(), "io.x/y", "1.0 beta"); err != nil {
		t.Fatal(err)
	}
	if escaped != "/v0.1/servers/io.x%2Fy/versions/1.0%20beta" {
		t.Errorf("escaped path %s", escaped)
	}
}

func TestTokenComesFromEnvAndURLTrimsSlash(t *testing.T) {
	var base, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base = r.URL.Path
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"servers":[],"metadata":{}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MCP_REGISTRY_URL", srv.URL+"/")
	t.Setenv("MCP_REGISTRY_TOKEN", "  secret-token  ")
	c := New("9.9.9")
	if c.BaseURL != srv.URL {
		t.Fatalf("base %q", c.BaseURL)
	}
	if c.UserAgent != "synca/9.9.9" {
		t.Fatalf("ua %q", c.UserAgent)
	}
	c.HTTP = srv.Client()
	if _, err := c.List(context.Background(), ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if base != "/v0.1/servers" {
		t.Errorf("path %s", base)
	}
	if auth != "Bearer secret-token" {
		t.Errorf("auth %q", auth)
	}
}

func TestDefaultBaseAndNoToken(t *testing.T) {
	t.Setenv("MCP_REGISTRY_URL", "")
	t.Setenv("MCP_REGISTRY_TOKEN", "")
	c := New("1")
	if c.BaseURL != DefaultBase || c.Token != "" {
		t.Fatalf("client %+v", c)
	}
}

func TestNoAuthorizationHeaderWithoutToken(t *testing.T) {
	var auth string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"servers":[],"metadata":{}}`))
	})
	if _, err := c.List(context.Background(), ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Errorf("auth %q", auth)
	}
}

func TestStatusesMapToKinds(t *testing.T) {
	cases := []struct {
		status int
		kind   Kind
	}{
		{http.StatusBadRequest, KindUsage},
		{http.StatusUnauthorized, KindUnauthorized},
		{http.StatusForbidden, KindUnauthorized},
		{http.StatusNotFound, KindNotFound},
		{http.StatusTooManyRequests, KindUnavailable},
		{http.StatusInternalServerError, KindUnavailable},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		})
		_, err := c.List(context.Background(), ListOptions{})
		if kindOf(err) != tc.kind {
			t.Errorf("%d: kind %v err %v", tc.status, kindOf(err), err)
			continue
		}
		var re *Error
		if errors.As(err, &re) && tc.status == http.StatusTooManyRequests {
			if !strings.Contains(re.Hint, "7") {
				t.Errorf("retry hint %q", re.Hint)
			}
		}
	}
}

func TestMalformedJSONIsUnavailable(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":`))
	})
	if _, err := c.List(context.Background(), ListOptions{}); kindOf(err) != KindUnavailable {
		t.Fatalf("kind %v err %v", kindOf(err), err)
	}
}

func TestValidationHappensBeforeTheNetwork(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	cases := map[string]func() error{
		"empty search":  func() error { _, err := c.Search(context.Background(), "  ", ListOptions{}); return err },
		"limit zero":    func() error { _, err := c.List(context.Background(), ListOptions{Limit: -1}); return err },
		"limit too big": func() error { _, err := c.List(context.Background(), ListOptions{Limit: MaxLimit + 1}); return err },
		"bad name":      func() error { _, err := c.Info(context.Background(), "x", ""); return err },
		"bad version":   func() error { _, err := c.Info(context.Background(), "io.x/y", "1\n2"); return err },
		"long version":  func() error { _, err := c.Info(context.Background(), "io.x/y", strings.Repeat("v", 256)); return err },
	}
	for name, call := range cases {
		if err := call(); kindOf(err) != KindUsage {
			t.Errorf("%s: kind %v err %v", name, kindOf(err), err)
		}
	}
}

func TestRegistryURLMustBeHTTPWithoutCredentials(t *testing.T) {
	for _, raw := range []string{
		"ftp://example.com",
		"https://user:pass@example.com",
		"https://example.com/?x=1",
		"https://example.com/#frag",
		"://bad",
	} {
		c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
		c.BaseURL = raw
		if _, err := c.List(context.Background(), ListOptions{}); kindOf(err) != KindUsage {
			t.Errorf("%q: kind %v err %v", raw, kindOf(err), err)
		}
	}
}

func TestCrossHostRedirectIsRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"servers":[],"metadata":{}}`))
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	t.Setenv("MCP_REGISTRY_URL", origin.URL)
	t.Setenv("MCP_REGISTRY_TOKEN", "")
	c := New("test")
	if _, err := c.List(context.Background(), ListOptions{}); kindOf(err) != KindUnavailable {
		t.Fatalf("kind %v err %v", kindOf(err), err)
	}
}
