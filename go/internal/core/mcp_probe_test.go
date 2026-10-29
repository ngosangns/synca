package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mcpProbeFixtureServer answers initialize and tools/list like a tiny
// streamable-HTTP MCP server.
func mcpProbeFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	tool := `{"name":"a","description":"Tool a","inputSchema":{"type":"object"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *uint64 `json:"id"`
			Method string  `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fixture","version":"9.9.9"}}`
		if req.Method != "initialize" {
			result = fmt.Sprintf(`{"tools":[%s]}`, tool)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":%s}`, *req.ID, result)
	}))
	t.Cleanup(server.Close)
	return server
}

func mcpProbeHome(t *testing.T, extraConfig string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	config := fmt.Sprintf(`{
  "mcpServers": {
    "live": {"type": "http", "url": %q},
    "dead": {"type": "http", "url": "http://127.0.0.1:1/mcp"},
    "off": {"type": "stdio", "command": "nope", "args": ["x"], "enabled": false}
  }
}`, mcpProbeFixtureServer(t).URL)
	if extraConfig != "" {
		config = extraConfig
	}
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "mcp.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestProbeMcpsReportsAndCaches(t *testing.T) {
	mcpProbeHome(t, "")
	cachePath := McpProbeCachePath(ScopeUser, "")
	if cachePath == "" || !strings.HasSuffix(cachePath, filepath.Join("synca", "mcp-probe.json")) {
		t.Fatalf("cache path %q", cachePath)
	}

	outcomes, err := ProbeMcps(ScopeUser, "", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]McpProbeOutcome{}
	for _, outcome := range outcomes {
		byKey[outcome.Key] = outcome
	}
	live := byKey["live"]
	if !live.OK || live.Tools != 1 || live.Tokens == 0 || live.Server != "fixture" || live.Transport != "http" {
		t.Fatalf("live %#v", live)
	}
	dead := byKey["dead"]
	if dead.OK || dead.Error == "" || dead.Skipped {
		t.Fatalf("dead %#v", dead)
	}
	off := byKey["off"]
	if off.OK || !off.Skipped || off.Error != "server is disabled" {
		t.Fatalf("off %#v", off)
	}

	cache, err := ReadMcpProbeCache(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache["off"]; ok {
		t.Fatal("disabled server was cached")
	}
	cachedLive, ok := cache["live"]
	if !ok || !cachedLive.OK || cachedLive.Fingerprint == "" || cachedLive.ProbedAt == "" {
		t.Fatalf("cached live %#v", cachedLive)
	}

	entries := ScanAll(ScopeUser, "").Mcps
	MergeMcpProbe(entries, cache)
	merged := map[string]McpEntry{}
	for _, entry := range entries {
		merged[entry.Key] = entry
	}
	got := merged["live"]
	if got.Tokens == nil || *got.Tokens != cachedLive.Tokens || got.ProbeOK == nil || !*got.ProbeOK {
		t.Fatalf("merged live %#v", got)
	}
	if got.ProbeError != nil {
		t.Fatalf("live error %#v", *got.ProbeError)
	}
	failed := merged["dead"]
	if failed.ProbeOK == nil || *failed.ProbeOK || failed.ProbeError == nil {
		t.Fatalf("merged dead %#v", failed)
	}

	// Changing the config invalidates the cached number.
	broken := []McpEntry{merged["live"]}
	broken[0].Tokens, broken[0].ProbedAt, broken[0].ProbeOK, broken[0].ProbeError = nil, nil, nil, nil
	stale := map[string]McpProbeEntry{"live": cachedLive}
	moved := "http://example.com/moved"
	broken[0].Presence[0].Normalized.URL = &moved
	MergeMcpProbe(broken, stale)
	if broken[0].Tokens != nil {
		t.Fatalf("stale tokens survived %#v", *broken[0].Tokens)
	}
}

func TestProbeMcpsUnknownName(t *testing.T) {
	mcpProbeHome(t, "")
	outcomes, err := ProbeMcps(ScopeUser, "", []string{"ghost"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes %#v", outcomes)
	}
	if outcomes[0].Key != "ghost" || !outcomes[0].Skipped || outcomes[0].Error == "" {
		t.Fatalf("outcome %#v", outcomes[0])
	}
}

func TestProbeMcpsSelectedNamesOnly(t *testing.T) {
	mcpProbeHome(t, "")
	outcomes, err := ProbeMcps(ScopeUser, "", []string{"live"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Key != "live" || !outcomes[0].OK {
		t.Fatalf("outcomes %#v", outcomes)
	}
}
