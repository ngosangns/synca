package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func catalogHubHome(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "mcp.json"), `{
  "mcpServers": {
    "demo": {"type": "stdio", "command": "echo", "args": ["hi"], "env": {"TOKEN": "example"}},
    "docs": {"type": "http", "url": "https://example.com/mcp"}
  }
}
`)
	return home
}

func TestMcpExportJSONMatchesSchema(t *testing.T) {
	catalogHubHome(t)
	out, err := runCLI(t, "mcp", "export", "--json")
	if err != nil {
		t.Fatal(err)
	}
	catalog := decodeOne(t, out).(map[string]any)
	mustValidate(t, "mcp-catalog.json", catalog)
	servers := catalog["servers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("servers %v", servers)
	}
	demo := servers["demo"].(map[string]any)
	if demo["command"] != "echo" || demo["env"].(map[string]any)["TOKEN"] != "example" {
		t.Fatalf("demo %v", demo)
	}
	if catalog["version"] != float64(1) {
		t.Fatalf("version %v", catalog["version"])
	}
}

func TestMcpExportOutWritesPrivateFile(t *testing.T) {
	home := catalogHubHome(t)
	path := filepath.Join(t.TempDir(), "mcp-catalog.json")
	out, err := runCLI(t, "mcp", "export", "--out", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote "+path) || !strings.Contains(out, "keep it private") {
		t.Fatalf("text %q", out)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	mustValidate(t, "mcp-catalog.json", decodeFile(t, path))

	// --json with --out still prints the catalog document.
	out, err = runCLI(t, "mcp", "export", "--out", filepath.Join(home, "again.json"), "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "mcp-catalog.json", decodeOne(t, out))
}

func TestMcpImportDryRunJSON(t *testing.T) {
	home := setHome(t)
	catalog := filepath.Join(t.TempDir(), "mcp-catalog.json")
	writeFile(t, catalog, `{
  "version": 1,
  "servers": {"demo": {"type": "stdio", "command": "echo", "args": ["hi"], "env": {"TOKEN": "example"}}}
}
`)
	text, err := runCLI(t, "mcp", "import", catalog, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "mcp", "import", catalog, "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != true || !reflect.DeepEqual(doc["actions"], jsonObjectsWithKind(t, text)) {
		t.Fatalf("import plan\n%s\n%s", text, out)
	}
	diffs := doc["diffs"].([]any)
	if len(diffs) != 1 || diffs[0].(map[string]any)["action"] != "add" {
		t.Fatalf("diffs %v", diffs)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the hub: %v", err)
	}
}

func TestMcpImportApplyNeedsYesThenWrites(t *testing.T) {
	home := setHome(t)
	catalog := filepath.Join(t.TempDir(), "mcp-catalog.json")
	writeFile(t, catalog, `{
  "version": 1,
  "servers": {
    "demo": {"type": "stdio", "command": "echo", "args": ["hi"], "env": {"TOKEN": "example"}},
    "docs": {"type": "http", "url": "https://example.com/mcp", "headers": {"Authorization": "Bearer example"},
             "origin": {"source": "io.test/docs", "sourceType": "registry", "version": "2.0.0"}}
  }
}
`)
	out, err := runCLI(t, "mcp", "import", catalog)
	code, stderr := statusOf(t, err)
	if code != 6 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "pass --yes") {
		t.Fatalf("gate code %d stderr %q out %q", code, stderr, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("gated import wrote the hub: %v", err)
	}

	out, err = runCLI(t, "mcp", "import", catalog, "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != false || len(doc["applied"].([]any)) == 0 {
		t.Fatalf("%v", doc)
	}
	hub := decodeFile(t, filepath.Join(home, ".agents", "mcp.json")).(map[string]any)
	demo := hub["mcpServers"].(map[string]any)["demo"].(map[string]any)
	if demo["command"] != "echo" || demo["env"].(map[string]any)["TOKEN"] != "example" {
		t.Fatalf("hub demo %v", demo)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("agent config: %v", err)
	}
	lock := decodeFile(t, filepath.Join(home, ".agents", ".mcp-lock.json")).(map[string]any)
	docs := lock["mcps"].(map[string]any)["docs"].(map[string]any)
	if docs["source"] != "io.test/docs" || docs["version"] != "2.0.0" {
		t.Fatalf("lock docs %v", docs)
	}

	// Re-import is a no-op with every diff marked same.
	out, err = runCLI(t, "mcp", "import", catalog, "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if len(doc["actions"].([]any)) != 0 {
		t.Fatalf("re-import actions %v", doc["actions"])
	}
	for _, d := range doc["diffs"].([]any) {
		if d.(map[string]any)["action"] != "same" {
			t.Fatalf("diff %v", d)
		}
	}
}

func TestMcpImportURLAndAgentsFilter(t *testing.T) {
	home := setHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":1,"servers":{"demo":{"type":"stdio","command":"echo"}}}`))
	}))
	defer srv.Close()
	out, err := runCLI(t, "mcp", "import", srv.URL, "--agents", "claude", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	agents := map[string]bool{}
	for _, action := range doc["actions"].([]any) {
		a := action.(map[string]any)
		if a["kind"] == "write_mcp_agent" {
			agents[a["agent"].(string)] = true
		}
	}
	if len(agents) != 1 || !agents["claude"] {
		t.Fatalf("agents %v", agents)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("agent config: %v", err)
	}
}
