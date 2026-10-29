package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMcpLockRecordForget(t *testing.T) {
	home := realTempHome(t)
	path := McpLockPath(ScopeUser, home)
	entry := McpLockEntry{
		Source:     "io.test/x",
		SourceType: "registry",
		Version:    "1.0.0",
		Variant:    "npm",
		EnvKeys:    []string{"API_KEY"},
	}
	if err := RecordMcpOrigin(path, "My_Server", entry); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadMcpLock(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := entries["my-server"]
	if !ok || got.Source != "io.test/x" || got.Variant != "npm" {
		t.Fatalf("entry %+v", got)
	}
	if got.InstalledAt == "" || got.UpdatedAt == "" {
		t.Fatalf("timestamps missing: %+v", got)
	}

	// A re-record keeps the first install time.
	entry.Version = "1.1.0"
	if err := RecordMcpOrigin(path, "my-server", entry); err != nil {
		t.Fatal(err)
	}
	entries, err = ReadMcpLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if entries["my-server"].Version != "1.1.0" || entries["my-server"].InstalledAt != got.InstalledAt {
		t.Fatalf("re-record %+v", entries["my-server"])
	}

	if removed, err := ForgetMcpOrigin(path, "nope"); err != nil || removed {
		t.Fatalf("forget unknown = %v, %v", removed, err)
	}
	if removed, err := ForgetMcpOrigin(path, "My_Server"); err != nil || !removed {
		t.Fatalf("forget = %v, %v", removed, err)
	}
	if entries, _ = ReadMcpLock(path); len(entries) != 0 {
		t.Fatalf("lock not empty: %v", entries)
	}

	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMcpLock(path); err == nil {
		t.Fatal("expected a corrupt lock to fail")
	}
}

func TestScanMcpAttachesProvenance(t *testing.T) {
	home := realTempHome(t)
	mustWrite(t, filepath.Join(home, ".agents", "mcp.json"),
		`{"mcpServers":{"my-server":{"type":"http","url":"https://x/mcp"}}}`)
	entries := ScanMcp(ScopeUser, home)
	if len(entries) != 1 {
		t.Fatalf("entries %v", entries)
	}
	if entries[0].Source != nil || entries[0].SourceType != nil || entries[0].SourceVersion != nil {
		t.Fatalf("unexpected provenance %+v", entries[0])
	}

	if err := RecordMcpOrigin(McpLockPath(ScopeUser, home), "my-server", McpLockEntry{
		Source: "io.test/x", SourceType: "registry", Version: "1.0.0", Variant: "streamable-http",
	}); err != nil {
		t.Fatal(err)
	}
	entries = ScanMcp(ScopeUser, home)
	e := entries[0]
	if e.Source == nil || *e.Source != "io.test/x" {
		t.Fatalf("source %+v", e)
	}
	if e.SourceType == nil || *e.SourceType != "registry" {
		t.Fatalf("source type %+v", e)
	}
	if e.SourceVersion == nil || *e.SourceVersion != "1.0.0" {
		t.Fatalf("source version %+v", e)
	}
}

func TestPlanUpdateMcpRewritesChangedServers(t *testing.T) {
	home := realTempHome(t)
	hub := filepath.Join(home, ".agents", "mcp.json")
	mustWrite(t, hub, `{"mcpServers":{"probe":{"type":"http","url":"https://old.example/mcp","env":{"API_KEY":"sk-existing"}}}}`)
	if err := RecordMcpOrigin(McpLockPath(ScopeUser, home), "probe", McpLockEntry{
		Source: "io.test/probe", SourceType: "registry", Version: "1.0.0", Variant: "streamable-http",
	}); err != nil {
		t.Fatal(err)
	}

	detail := `{"server":{"name":"io.test/probe","description":"d","version":"1.1.0","remotes":[
	 {"type":"streamable-http","url":"https://new.example/mcp",
	  "headers":[{"name":"Authorization","isRequired":true}]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(detail))
	}))
	defer srv.Close()
	t.Setenv("MCP_REGISTRY_URL", srv.URL)
	t.Setenv("MCP_REGISTRY_TOKEN", "")

	// The registry requires a header the hub does not have yet: noted, no plan.
	plan, err := PlanUpdateMcp(ScopeUser, home, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || !notesContain(plan.Notes, "failed: probe") {
		t.Fatalf("plan %+v notes %v", plan.Actions, plan.Notes)
	}

	// The header arrives with the config; the update rewrites the URL and keeps
	// the env value. The registry now also declares its literal header value.
	detail = `{"server":{"name":"io.test/probe","description":"d","version":"1.1.0","remotes":[
	 {"type":"streamable-http","url":"https://new.example/mcp",
	  "headers":[{"name":"Authorization","isRequired":true,"value":"Bearer hidden"}]}]}}`
	mustWrite(t, hub, `{"mcpServers":{"probe":{"type":"http","url":"https://old.example/mcp","env":{"API_KEY":"sk-existing"},"headers":{"Authorization":"Bearer keep"}}}}`)
	plan, err = PlanUpdateMcp(ScopeUser, home, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !notesContain(plan.Notes, "update: probe 1.0.0 -> 1.1.0") {
		t.Fatalf("notes %v", plan.Notes)
	}
	kinds := map[string]int{}
	for _, a := range plan.Actions {
		kinds[a.Kind]++
	}
	if kinds["upsert_mcp_hub"] != 1 || kinds["record_mcp_origin"] != 1 || kinds["write_mcp_agent"] == 0 {
		t.Fatalf("actions %v", plan.Actions)
	}
	if _, err := ApplyManagePlan(plan, nil); err != nil {
		t.Fatal(err)
	}
	probe := readJSON(t, hub)["mcpServers"].(map[string]any)["probe"].(map[string]any)
	if probe["url"] != "https://new.example/mcp" {
		t.Fatalf("probe %v", probe)
	}
	if probe["env"].(map[string]any)["API_KEY"] != "sk-existing" {
		t.Fatalf("env %v", probe["env"])
	}
	if probe["headers"].(map[string]any)["Authorization"] != "Bearer keep" {
		t.Fatalf("headers %v", probe["headers"])
	}
	entries, err := ReadMcpLock(McpLockPath(ScopeUser, home))
	if err != nil {
		t.Fatal(err)
	}
	if entries["probe"].Version != "1.1.0" {
		t.Fatalf("lock %+v", entries["probe"])
	}

	// Same version and content again: up to date.
	plan, err = PlanUpdateMcp(ScopeUser, home, []string{"probe"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || !notesContain(plan.Notes, "up to date: probe (1.1.0)") {
		t.Fatalf("notes %v actions %v", plan.Notes, plan.Actions)
	}

	// A key without a recorded source is a hard error.
	if _, err := PlanUpdateMcp(ScopeUser, home, []string{"nope"}, nil, ""); err == nil ||
		!strings.Contains(err.Error(), "no recorded registry source") {
		t.Fatalf("err %v", err)
	}
}
