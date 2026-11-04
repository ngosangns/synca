package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func catalogTestHome(t *testing.T) string {
	t.Helper()
	home := realTempHome(t)
	mustWrite(t, filepath.Join(home, ".synca", "local", "mcp.json"), `{
		"mcpServers": {
			"demo": {"type": "stdio", "command": "echo", "args": ["hi"], "env": {"TOKEN": "example"}},
			"docs": {"type": "http", "url": "https://example.com/mcp", "headers": {"Authorization": "Bearer example"}}
		}
	}`)
	return home
}

func TestMcpCatalogRoundTrip(t *testing.T) {
	home := catalogTestHome(t)
	if err := RecordMcpOrigin(McpLockPath(ScopeUser, home), "demo", McpLockEntry{
		Source: "io.test/demo", SourceType: "registry", Version: "1.0.0", Variant: "npm",
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := BuildMcpCatalog(ScopeUser, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Servers) != 2 {
		t.Fatalf("servers %v", catalog.Servers)
	}
	demo := catalog.Servers["demo"]
	if demo.Config.Transport != "stdio" || len(demo.Config.Command) != 1 || demo.Config.Command[0] != "echo" {
		t.Fatalf("demo config %+v", demo.Config)
	}
	if demo.Origin == nil || demo.Origin.Source != "io.test/demo" || demo.Origin.SourceType != "registry" {
		t.Fatalf("demo origin %+v", demo.Origin)
	}
	if docs := catalog.Servers["docs"]; docs.Config.URL == nil || *docs.Config.URL != "https://example.com/mcp" || docs.Origin != nil {
		t.Fatalf("docs %+v", docs)
	}

	path := filepath.Join(t.TempDir(), "mcp-catalog.json")
	written, err := WriteMcpCatalogFile(path, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if written != path {
		t.Fatalf("written %q", written)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("catalog mode %v, want 0600", st.Mode().Perm())
	}
	loaded, err := LoadMcpCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Servers) != 2 || loaded.Version != McpCatalogVersion {
		t.Fatalf("loaded %+v", loaded)
	}
	got := loaded.Servers["demo"]
	if got.Config.Command[0] != "echo" || got.Config.Env["TOKEN"] != "example" {
		t.Fatalf("loaded demo %+v", got.Config)
	}
	if got.Origin == nil || got.Origin.Version != "1.0.0" {
		t.Fatalf("loaded demo origin %+v", got.Origin)
	}
	if _, ok := loaded.Servers["docs"]; !ok {
		t.Fatal("docs missing after round trip")
	}
}

func TestMcpCatalogExportEmptyHub(t *testing.T) {
	home := realTempHome(t)
	catalog, err := BuildMcpCatalog(ScopeUser, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Servers) != 0 {
		t.Fatalf("servers %v", catalog.Servers)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"servers":{}`) {
		t.Fatalf("marshal %s", data)
	}
}

func TestLoadMcpCatalogURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		_, _ = w.Write([]byte(`{"version":1,"servers":{"a":{"type":"stdio","command":"echo"}}}`))
	}))
	defer srv.Close()
	catalog, err := LoadMcpCatalog(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Servers) != 1 || catalog.Servers["a"].Config.Command[0] != "echo" {
		t.Fatalf("catalog %+v", catalog)
	}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	if _, err := LoadMcpCatalog(srv.URL + "/missing.json"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadMcpCatalogRejects(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"newer version":  `{"version":2,"servers":{"a":{"type":"stdio","command":"echo"}}}`,
		"no servers":     `{"version":1,"servers":{}}`,
		"missing server": `{"version":1,"servers":{"a":{}}}`,
		"bad stdio":      `{"version":1,"servers":{"a":{"type":"stdio","args":["x"]}}}`,
		"bad http":       `{"version":1,"servers":{"a":{"type":"http"}}}`,
		"bad transport":  `{"version":1,"servers":{"a":{"type":"carrier-pigeon","command":"echo"}}}`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, "catalog.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadMcpCatalog(path); err == nil {
			t.Fatalf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), "catalog") && !strings.Contains(err.Error(), "server a") {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestPlanMcpCatalogImport(t *testing.T) {
	home := realTempHome(t)
	catalog := &McpCatalog{
		Version: 1,
		Servers: map[string]McpCatalogServer{
			"demo": {Config: McpNormalized{
				Transport: "stdio", Command: []string{"echo"}, Args: []string{"hi"},
				EnvKeys: []string{"TOKEN"}, Env: map[string]string{"TOKEN": "example"},
			}, Origin: &McpCatalogOrigin{Source: "io.test/demo", SourceType: "registry", Version: "1.0.0"}},
			"docs": {Config: McpNormalized{
				Transport: "http", URL: strPtr("https://example.com/mcp"),
				HeaderKeys: []string{"Authorization"}, Headers: map[string]string{"Authorization": "Bearer example"},
			}},
		},
	}
	plan, diffs, err := PlanMcpCatalogImport(ScopeUser, home, "catalog.json", catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 2 {
		t.Fatalf("diffs %v", diffs)
	}
	for _, d := range diffs {
		if d.Action != "add" {
			t.Fatalf("diff %v", d)
		}
	}
	kinds := map[string]int{}
	for _, a := range plan.Actions {
		kinds[a.Kind]++
		if a.Mcp == nil && a.Kind != "record_mcp_origin" {
			t.Fatalf("action %v has no payload", a.Kind)
		}
	}
	if kinds["upsert_mcp_hub"] != 2 || kinds["write_mcp_agent"] == 0 || kinds["record_mcp_origin"] != 1 {
		t.Fatalf("actions %v", kinds)
	}
	if !notesContain(plan.Notes, "2 server(s): 2 add, 0 update, 0 unchanged") {
		t.Fatalf("notes %v", plan.Notes)
	}
	if notesContain(plan.Notes, "example") {
		t.Fatalf("notes leak values: %v", plan.Notes)
	}

	if _, err := ApplyManagePlan(plan, nil); err != nil {
		t.Fatal(err)
	}
	hub := filepath.Join(home, ".agents", "mcp.json")
	demo := readJSON(t, hub)["mcpServers"].(map[string]any)["demo"].(map[string]any)
	if demo["env"].(map[string]any)["TOKEN"] != "example" {
		t.Fatalf("hub demo %v", demo)
	}
	docs := readJSON(t, filepath.Join(home, ".synca", "local", "mcp.json"))["mcpServers"].(map[string]any)["docs"].(map[string]any)
	if docs["url"] != "https://example.com/mcp" {
		t.Fatalf("local docs %v", docs)
	}
	lock, err := ReadMcpLock(McpLockPath(ScopeUser, home))
	if err != nil {
		t.Fatal(err)
	}
	if lock["demo"].Source != "io.test/demo" {
		t.Fatalf("lock %+v", lock)
	}

	// A second import of the same catalog is a no-op.
	plan, diffs, err = PlanMcpCatalogImport(ScopeUser, home, "catalog.json", catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || !notesContain(plan.Notes, "nothing to import") {
		t.Fatalf("plan %v notes %v", plan.Actions, plan.Notes)
	}
	for _, d := range diffs {
		if d.Action != "same" {
			t.Fatalf("diff %v", d)
		}
	}

	// Changing a command is an update; changing only an env or header value is
	// skipped, because the fingerprint covers names only — a re-import must
	// never clobber a machine's own secrets.
	rebuilt, err := BuildMcpCatalog(ScopeUser, home)
	if err != nil {
		t.Fatal(err)
	}
	changed := rebuilt.Servers["demo"]
	changed.Config.Command = []string{"echo", "there"}
	if _, err := ApplyManagePlan(mustImportPlan(t, home, changed, "demo"), nil); err != nil {
		t.Fatal(err)
	}
	demoAfter := readJSON(t, hub)["mcpServers"].(map[string]any)["demo"].(map[string]any)
	if demoAfter["command"] != "echo" || demoAfter["args"].([]any)[0] != "there" {
		t.Fatalf("demo after update %v", demoAfter)
	}
	valueOnly := rebuilt.Servers["docs"]
	valueOnly.Config.Headers["Authorization"] = "Bearer rotated"
	skipPlan := mustImportPlan(t, home, valueOnly, "docs")
	if len(skipPlan.Actions) != 0 {
		t.Fatalf("value-only change planned %v", skipPlan.Actions)
	}
	docsAgain := readJSON(t, filepath.Join(home, ".synca", "local", "mcp.json"))["mcpServers"].(map[string]any)["docs"].(map[string]any)
	if docsAgain["headers"].(map[string]any)["Authorization"] != "Bearer example" {
		t.Fatalf("docs headers %v", docs["headers"])
	}
}

func mustImportPlan(t *testing.T, home string, server McpCatalogServer, key string) *ManagePlan {
	t.Helper()
	catalog := &McpCatalog{Version: 1, Servers: map[string]McpCatalogServer{key: server}}
	plan, _, err := PlanMcpCatalogImport(ScopeUser, home, "catalog.json", catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlanMcpCatalogImportAgentsFilter(t *testing.T) {
	home := realTempHome(t)
	catalog := &McpCatalog{
		Version: 1,
		Servers: map[string]McpCatalogServer{
			"demo": {Config: McpNormalized{Transport: "stdio", Command: []string{"echo"}}},
		},
	}
	plan, diffs, err := PlanMcpCatalogImport(ScopeUser, home, "catalog.json", catalog, []AgentKind{AgentGrok})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Action != "add" {
		t.Fatalf("diffs %v", diffs)
	}
	agents := map[string]int{}
	for _, a := range plan.Actions {
		if a.Kind == "write_mcp_agent" {
			agents[a.Agent]++
		}
	}
	if len(agents) != 1 || agents["grok"] != 1 {
		t.Fatalf("agents %v", agents)
	}
}
