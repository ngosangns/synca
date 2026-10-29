package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mcpRegistryListBody = `{"servers":[
 {"server":{"name":"io.test/server","title":"Test Server","description":"d","version":"1.0.0",
   "repository":{"url":"https://github.com/x/y","source":"github"},
   "packages":[{"registryType":"npm","registryBaseUrl":"https://registry.npmjs.org","identifier":"@x/y","version":"1.0.0","transport":{"type":"stdio"}}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true,"statusChangedAt":"2025-01-01T00:00:00Z","publishedAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z"}}},
 {"server":{"name":"ac.test/remote","title":"Remote","description":"r","version":"2.0.0","remotes":[{"type":"streamable-http","url":"https://api.test/mcp"}]}}
],"metadata":{"count":2,"nextCursor":"ac.test/remote:2.0.0"}}`

const mcpRegistryDetailBody = `{"server":{"name":"io.test/server","title":"Test Server","description":"d","version":"1.0.0",
"repository":{"url":"https://github.com/x/y","source":"github"},
"packages":[{"registryType":"npm","registryBaseUrl":"https://registry.npmjs.org","identifier":"@x/y","version":"1.0.0","transport":{"type":"stdio"},
 "environmentVariables":[{"name":"API_KEY","isRequired":true},{"name":"REGION","default":"eu-west-1"}]}]},
"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","isLatest":true,"statusChangedAt":"2025-01-01T00:00:00Z","publishedAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z"}}}`

func mcpRegistry(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("MCP_REGISTRY_URL", srv.URL)
	t.Setenv("MCP_REGISTRY_TOKEN", "")
	return srv
}

func mcpRegistryStub(t *testing.T, detail string) *httptest.Server {
	t.Helper()
	return mcpRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v0.1/servers/") {
			_, _ = w.Write([]byte(detail))
			return
		}
		_, _ = w.Write([]byte(mcpRegistryListBody))
	})
}

func TestMcpSearchJSON(t *testing.T) {
	var query string
	mcpRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(mcpRegistryListBody))
	})
	out, err := runCLI(t, "mcp", "search", "filesystem", "--limit", "5", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "search=filesystem") || !strings.Contains(query, "limit=5") || !strings.Contains(query, "version=latest") {
		t.Fatalf("query %s", query)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "mcp-registry-search.json", doc)
	obj := doc.(map[string]any)
	if obj["query"] != "filesystem" || obj["count"] != float64(2) || obj["nextCursor"] == "" {
		t.Fatalf("doc %#v", obj)
	}
	first := obj["servers"].([]any)[0].(map[string]any)
	if first["name"] != "io.test/server" || first["status"] != "active" || first["repositoryUrl"] != "https://github.com/x/y" {
		t.Fatalf("server %#v", first)
	}
}

func TestMcpBrowseJSONUsesCursor(t *testing.T) {
	var query string
	mcpRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(mcpRegistryListBody))
	})
	out, err := runCLI(t, "mcp", "browse", "--cursor", "abc", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "cursor=abc") || strings.Contains(query, "search=") {
		t.Fatalf("query %s", query)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "mcp-registry-browse.json", doc)
	if doc.(map[string]any)["cursor"] != "abc" {
		t.Fatalf("doc %#v", doc)
	}
}

func TestMcpInfoJSON(t *testing.T) {
	var path string
	mcpRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = w.Write([]byte(mcpRegistryDetailBody))
	})
	out, err := runCLI(t, "mcp", "info", "io.test/server", "--version", "1.0.0", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v0.1/servers/io.test%2Fserver/versions/1.0.0" {
		t.Fatalf("path %s", path)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "mcp-registry-info.json", doc)
	server := doc.(map[string]any)["server"].(map[string]any)
	pkg := server["packages"].([]any)[0].(map[string]any)
	if pkg["identifier"] != "@x/y" || strings.Join(anyStrings(pkg["envKeys"]), ",") != "API_KEY,REGION" {
		t.Fatalf("package %#v", pkg)
	}
	if server["official"] == nil {
		t.Fatalf("missing official: %#v", server)
	}
}

func TestMcpRegistryErrorsMapToExitCodes(t *testing.T) {
	cases := []struct {
		status int
		code   int
	}{
		{http.StatusNotFound, ExitUnavailable},
		{http.StatusUnauthorized, ExitUnauthorized},
		{http.StatusTooManyRequests, ExitUnavailable},
	}
	for _, tc := range cases {
		mcpRegistry(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		})
		_, err := runCLI(t, "mcp", "info", "io.test/server", "--json")
		if code, _ := statusOf(t, err); code != tc.code {
			t.Errorf("%d: code %d err %v", tc.status, code, err)
		}
	}
	_, err := runCLI(t, "mcp", "search", "--json")
	if code, _ := statusOf(t, err); code != ExitUsage {
		t.Errorf("empty query: %d %v", code, err)
	}
	_, err = runCLI(t, "mcp", "browse", "--limit", "500", "--json")
	if code, _ := statusOf(t, err); code != ExitUsage {
		t.Errorf("bad limit: %d %v", code, err)
	}
}

func TestMcpInstallDryRunJSON(t *testing.T) {
	home := setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	out, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=super-secret", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "manage-plan.json", doc)
	plan := doc.(map[string]any)
	notes := anyStrings(plan["notes"])
	if !containsLine(notes, "registry io.test/server@1.0.0 -> npm") {
		t.Fatalf("notes %v", notes)
	}
	if strings.Contains(out, "super-secret") {
		t.Fatal("dry-run leaked the env value")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the hub: %v", err)
	}
}

func TestMcpInstallApplyWritesHubAndAgent(t *testing.T) {
	home := setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	out, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=secret", "--agents", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "manage-plan.json", doc)
	if strings.Contains(out, "secret") {
		t.Fatal("apply leaked the env value")
	}
	agents := 0
	for _, action := range doc.(map[string]any)["actions"].([]any) {
		a := action.(map[string]any)
		if a["kind"] == "write_mcp_agent" {
			agents++
			if a["agent"] != "claude" {
				t.Fatalf("unexpected agent %#v", a)
			}
		}
	}
	if agents != 1 {
		t.Fatalf("agent actions %d", agents)
	}

	hub := decodeFile(t, filepath.Join(home, ".agents", "mcp.json")).(map[string]any)
	server := hub["mcpServers"].(map[string]any)["server"].(map[string]any)
	if server["type"] != "stdio" || server["command"] != "npx" {
		t.Fatalf("hub server %#v", server)
	}
	if got := strings.Join(anyStrings(server["args"]), " "); got != "-y @x/y@1.0.0" {
		t.Fatalf("args %q", got)
	}
	if env := server["env"].(map[string]any); env["API_KEY"] != "secret" || env["REGION"] != "eu-west-1" {
		t.Fatalf("env %#v", env)
	}
	agent := decodeFile(t, filepath.Join(home, ".claude.json")).(map[string]any)
	if _, ok := agent["mcpServers"].(map[string]any)["server"]; !ok {
		t.Fatalf("agent config %#v", agent)
	}

	listOut, err := runCLI(t, "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "mcp-list.json", decodeOne(t, listOut))
}

func TestMcpInstallMissingRequiredEnvListsNamesOnly(t *testing.T) {
	setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	out, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "OTHER=top-secret", "--dry-run", "--json")
	code, _ := statusOf(t, err)
	if code != ExitUsage {
		t.Fatalf("code %d err %v", code, err)
	}
	if !strings.Contains(out, "API_KEY") || strings.Contains(out, "top-secret") {
		t.Fatalf("message %s", out)
	}
}

func TestMcpInstallHeaders(t *testing.T) {
	setHome(t)
	required := `{"server":{"name":"io.test/h","description":"d","version":"1.0.0","remotes":[
	 {"type":"streamable-http","url":"https://api.test/mcp","headers":[{"name":"Authorization","isRequired":true,"value":"Bearer hidden"}]}]}}`
	mcpRegistryStub(t, required)
	out, err := runCLI(t, "mcp", "install", "io.test/h", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	notes := anyStrings(decodeOne(t, out).(map[string]any)["notes"])
	if !containsLine(notes, "header Authorization=••••••") || strings.Contains(out, "hidden") {
		t.Fatalf("notes %v out %s", notes, out)
	}

	noValue := `{"server":{"name":"io.test/h","description":"d","version":"1.0.0","remotes":[
	 {"type":"streamable-http","url":"https://api.test/mcp","headers":[{"name":"Authorization","isRequired":true}]}]}}`
	mcpRegistryStub(t, noValue)
	out, err = runCLI(t, "mcp", "install", "io.test/h", "--dry-run", "--json")
	if code, _ := statusOf(t, err); code != ExitUsage {
		t.Fatalf("code %d err %v", code, err)
	}
	if !strings.Contains(out, "Authorization") || !strings.Contains(out, "--header") {
		t.Fatalf("message %s", out)
	}

	out, err = runCLI(t, "mcp", "install", "io.test/h", "--header", "Authorization=Bearer from-flag", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "from-flag") {
		t.Fatalf("header value leaked: %s", out)
	}

	optional := `{"server":{"name":"io.test/h","description":"d","version":"1.0.0","remotes":[
	 {"type":"streamable-http","url":"https://api.test/mcp","headers":[{"name":"X-Trace"}]}]}}`
	mcpRegistryStub(t, optional)
	out, err = runCLI(t, "mcp", "install", "io.test/h", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	notes = anyStrings(decodeOne(t, out).(map[string]any)["notes"])
	if !containsLine(notes, "skipped optional header(s): X-Trace") {
		t.Fatalf("notes %v", notes)
	}
}

func TestMcpInstallNameEnabledAndForce(t *testing.T) {
	home := setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)

	out, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--name", "Other_Server", "--enabled", "false", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	first := decodeOne(t, out).(map[string]any)["actions"].([]any)[0].(map[string]any)
	if first["server"] != "other-server" {
		t.Fatalf("key %#v", first)
	}

	if _, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--agents", "claude"); err != nil {
		t.Fatal(err)
	}
	_, err = runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--agents", "claude", "--json")
	if code, _ := statusOf(t, err); code != ExitUsage {
		t.Fatalf("collision code %d err %v", code, err)
	}

	out, err = runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--force", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	notes := anyStrings(decodeOne(t, out).(map[string]any)["notes"])
	if containsLine(notes, "already exists") {
		t.Fatalf("--force still warned: %v", notes)
	}

	out, err = runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--name", "disabled", "--enabled", "false", "--agents", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	hub := decodeFile(t, filepath.Join(home, ".agents", "mcp.json")).(map[string]any)
	entry := hub["mcpServers"].(map[string]any)["disabled"].(map[string]any)
	if entry["enabled"] != false {
		t.Fatalf("enabled %#v", entry["enabled"])
	}
}

func TestMcpInstallRejectsUnsupportedPackage(t *testing.T) {
	setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	_, err := runCLI(t, "mcp", "install", "io.test/server", "--package", "cargo", "--dry-run", "--json")
	if code, _ := statusOf(t, err); code != ExitUsage {
		t.Fatalf("code %d err %v", code, err)
	}
}

func anyStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestMcpInstallRecordsOriginAndListReportsIt(t *testing.T) {
	home := setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	out, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--json")
	if err != nil {
		t.Fatal(err)
	}
	applied := asStrings(t, decodeOne(t, out).(map[string]any)["applied"])
	if !containsLine(applied, "recorded origin of mcp server: io.test/server") {
		t.Fatalf("applied %v", applied)
	}
	lock, err := os.ReadFile(filepath.Join(home, ".agents", ".mcp-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"source": "io.test/server"`, `"variant": "npm"`, `"envKeys"`} {
		if !strings.Contains(string(lock), want) {
			t.Fatalf("lock missing %s: %s", want, lock)
		}
	}

	list, err := runCLI(t, "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	entry := entryByKey(t, decodeOne(t, list), "server")
	if entry["source"] != "io.test/server" || entry["source_type"] != "registry" || entry["source_version"] != "1.0.0" {
		t.Fatalf("provenance %v %v %v", entry["source"], entry["source_type"], entry["source_version"])
	}

	// Removing the server forgets the origin.
	if _, err := runCLI(t, "mcp", "remove", "server", "--yes", "--json"); err != nil {
		t.Fatal(err)
	}
	lock, err = os.ReadFile(filepath.Join(home, ".agents", ".mcp-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lock), "io.test/server") {
		t.Fatalf("origin survived remove: %s", lock)
	}
}

func TestMcpUpdateCommandDryRunAndUnknownKey(t *testing.T) {
	setHome(t)
	mcpRegistryStub(t, mcpRegistryDetailBody)
	if _, err := runCLI(t, "mcp", "install", "io.test/server", "--env", "API_KEY=x", "--json"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "mcp", "update", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	notes := asStrings(t, doc["notes"])
	if !containsLine(notes, "up to date: server (1.0.0)") {
		t.Fatalf("notes %v", notes)
	}

	if _, err := runCLI(t, "mcp", "update", "nope", "--dry-run", "--json"); err == nil {
		t.Fatal("updated a server with no recorded source")
	} else if code, _ := statusOf(t, err); code != ExitUsage {
		t.Fatalf("code %d", code)
	}
}
