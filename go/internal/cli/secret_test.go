package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ngosangns/synca/go/internal/core"
)

const (
	secretToken = "fake-token-123"
	otherToken  = "fake-token-456"
)

func assertNoSecret(t *testing.T, label, text string) {
	t.Helper()
	if strings.Contains(text, secretToken) || strings.Contains(text, otherToken) {
		t.Fatalf("%s printed a secret value", label)
	}
}

func writeSecretHome(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "mcp.json"), `{
  "mcpServers": {
    "demo": {
      "type": "stdio",
      "command": "echo --token=`+secretToken+`",
      "args": ["--token=`+secretToken+`", "--api-key", "`+secretToken+`", "Authorization: Bearer `+secretToken+`"],
      "env": {"DISCORD_TOKEN": "`+secretToken+`", "LOG_LEVEL": "info"}
    },
    "remote": {"type": "http", "url": "https://bot:`+secretToken+`@example.com/mcp"}
  }
}
`)
	writeFile(t, filepath.Join(home, ".cursor", "mcp.json"), `{
  "mcpServers": {
    "remote": {"type": "http", "url": "https://bot:`+otherToken+`@example.com/mcp"}
  }
}
`)
	return home
}

func TestMcpListMasksSecrets(t *testing.T) {
	writeSecretHome(t)

	text, err := runCLI(t, "mcp", "list")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp list text", text)
	if !strings.Contains(text, "MISMATCH") {
		t.Fatal("text list dropped the mismatch flag")
	}

	out, err := runCLI(t, "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp list json", out)
	doc := decodeOne(t, out)
	mustValidate(t, "mcp-list.json", doc)
	demo := entryByKey(t, doc, "demo")
	norm := demo["presence"].([]any)[0].(map[string]any)["normalized"].(map[string]any)
	env := norm["env"].(map[string]any)
	if env["DISCORD_TOKEN"] != core.SecretMask || env["LOG_LEVEL"] != core.SecretMask {
		t.Fatal("env values were not masked")
	}
	keys := norm["env_keys"].([]any)
	if len(keys) != 2 || keys[0] != "DISCORD_TOKEN" || keys[1] != "LOG_LEVEL" {
		t.Fatal("env_keys changed")
	}
	if norm["command"].([]any)[0] != "echo --token="+core.SecretMask {
		t.Fatal("command was not masked")
	}
	args := norm["args"].([]any)
	if args[0] != "--token="+core.SecretMask || args[2] != core.SecretMask || args[3] != "Authorization: "+core.SecretMask {
		t.Fatal("args were not masked")
	}
	remote := entryByKey(t, doc, "remote")
	if remote["mismatch"] != true {
		t.Fatal("masking cleared a real mismatch")
	}
	maskedFP := fingerprintsOf(t, doc)

	revealed, err := runCLI(t, "mcp", "list", "--json", "--reveal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(revealed, secretToken) || !strings.Contains(revealed, otherToken) {
		t.Fatal("reveal hid a secret value")
	}
	revealDoc := decodeOne(t, revealed)
	mustValidate(t, "mcp-list.json", revealDoc)
	revealNorm := entryByKey(t, revealDoc, "demo")["presence"].([]any)[0].(map[string]any)["normalized"].(map[string]any)
	revealEnv := revealNorm["env"].(map[string]any)
	if revealEnv["LOG_LEVEL"] != "info" {
		t.Fatal("reveal changed a non-secret env value")
	}
	if !reflect.DeepEqual(fingerprintsOf(t, revealDoc), maskedFP) {
		t.Fatal("fingerprint changed when values were masked")
	}
	scanned := map[string]string{}
	for _, entry := range core.ScanMcp(core.ScopeUser, ".") {
		for _, presence := range entry.Presence {
			scanned[entry.Key+"/"+presence.Agent] = presence.Fingerprint
		}
	}
	if !reflect.DeepEqual(scanned, maskedFP) {
		t.Fatal("printed fingerprint is not the scan fingerprint")
	}

	revealText, err := runCLI(t, "mcp", "list", "--reveal")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp list text reveal", revealText)
}

func fingerprintsOf(t *testing.T, doc any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, item := range doc.([]any) {
		entry := item.(map[string]any)
		for _, raw := range entry["presence"].([]any) {
			presence := raw.(map[string]any)
			out[entry["key"].(string)+"/"+presence["agent"].(string)] = presence["fingerprint"].(string)
		}
	}
	return out
}

func TestMcpAddDryRunMasksEnv(t *testing.T) {
	home := setHome(t)
	text, err := runCLI(t, "mcp", "add", "added",
		"--transport", "stdio", "--command", "echo",
		"--env", "DISCORD_TOKEN="+secretToken,
		"--env", "LOG_LEVEL=info",
		"--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp add text", text)
	if !strings.Contains(text, "env DISCORD_TOKEN="+core.SecretMask) || !strings.Contains(text, "env LOG_LEVEL="+core.SecretMask) {
		t.Fatal("dry-run text hid an env key")
	}
	out, err := runCLI(t, "mcp", "add", "added",
		"--transport", "stdio", "--command", "echo",
		"--env", "LOG_LEVEL=info",
		"--env", "DISCORD_TOKEN="+secretToken,
		"--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp add json", out)
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != true {
		t.Fatal("dry-run flag was dropped")
	}
	notes := asStrings(t, doc["notes"])
	if !containsAll(notes, "env DISCORD_TOKEN="+core.SecretMask, "env LOG_LEVEL="+core.SecretMask) {
		t.Fatal("dry-run notes hid an env key")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote the hub")
	}
}

func containsAll(lines []string, wants ...string) bool {
	for _, want := range wants {
		found := false
		for _, line := range lines {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestOtherCommandsDoNotPrintMcpSecrets(t *testing.T) {
	writeSecretHome(t)

	cases := [][]string{
		{"mcp", "remove", "demo", "--dry-run"},
		{"mcp", "remove", "demo", "--dry-run", "--json"},
		{"sync", "mcp", "--dry-run"},
		{"sync", "mcp", "--dry-run", "--json"},
		{"sync", "all", "--dry-run", "--json"},
		{"doctor"},
		{"doctor", "--json"},
		{"skills", "list"},
		{"skills", "list", "--json"},
		{"provider", "clear", "cursor", "--dry-run"},
		{"provider", "clear", "cursor", "--dry-run", "--json"},
	}
	for _, args := range cases {
		out, err := runCLI(t, args...)
		label := strings.Join(args, " ")
		assertNoSecret(t, label, out)
		code, stderr := statusOf(t, err)
		assertNoSecret(t, label+" stderr", stderr)
		want := 0
		if args[0] == "doctor" {
			want = 8
		}
		if code != want {
			t.Fatalf("%s exited %d", label, code)
		}
	}

	out, err := runCLI(t, "schema", "mcp list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--reveal") {
		t.Fatal("mcp list schema is missing --reveal")
	}
}

func TestMcpAddApplyStoresValueWithoutPrintingIt(t *testing.T) {
	home := setHome(t)
	out, err := runCLI(t, "mcp", "add", "added", "--transport", "stdio", "--command", "echo", "--env", "DISCORD_TOKEN="+secretToken, "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp add apply", out)
	if !strings.Contains(out, "DISCORD_TOKEN="+core.SecretMask) {
		t.Fatal("apply plan hid the env key")
	}
	body, err := os.ReadFile(filepath.Join(home, ".agents", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), secretToken) {
		t.Fatal("apply did not store the env value")
	}
}

func TestMcpAddHeadersStoresValuesWithoutPrintingThem(t *testing.T) {
	home := setHome(t)
	out, err := runCLI(t, "mcp", "add", "remote",
		"--transport", "sse", "--url", "https://api.example/sse",
		"--header", "Authorization=Bearer "+secretToken,
		"--header", "X-Trace=abc",
		"--json")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp add headers", out)
	for _, want := range []string{
		"header Authorization=" + core.SecretMask,
		"header X-Trace=" + core.SecretMask,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan hid %q: %s", want, out)
		}
	}
	body, err := os.ReadFile(filepath.Join(home, ".agents", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), secretToken) || !strings.Contains(string(body), `"X-Trace": "abc"`) {
		t.Fatal("apply did not store the header values")
	}

	list, err := runCLI(t, "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "mcp list headers", list)
	norm := entryByKey(t, decodeOne(t, list), "remote")["presence"].([]any)[0].(map[string]any)["normalized"].(map[string]any)
	if norm["transport"] != "sse" {
		t.Fatalf("transport %v", norm["transport"])
	}
	headers := norm["headers"].(map[string]any)
	if headers["Authorization"] != core.SecretMask || headers["X-Trace"] != core.SecretMask {
		t.Fatal("header values were not masked")
	}

	if _, err := runCLI(t, "mcp", "add", "bad", "--transport", "sse", "--url", "https://x/sse", "--header", "nope"); err == nil {
		t.Fatal("accepted a header without a value")
	} else if code, _ := statusOf(t, err); code != ExitUsage {
		t.Fatalf("code %d", code)
	}
	if _, err := runCLI(t, "mcp", "add", "bad", "--transport", "stdio", "--command", "echo", "--header", "A=b"); err == nil {
		t.Fatal("accepted a header on a stdio server")
	}
}
