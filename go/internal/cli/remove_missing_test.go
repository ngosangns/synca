package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fileState struct {
	mode os.FileMode
	mod  int64
	body string
	dir  bool
}

func treeState(t *testing.T, root string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		st := fileState{mode: info.Mode(), mod: info.ModTime().UnixNano(), dir: info.IsDir()}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			st.body = string(b)
		}
		out[rel] = st
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func ageTree(t *testing.T, root string) {
	t.Helper()
	past := time.Now().Add(-2 * time.Hour)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, past, past)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoveUnknownLeavesFilesUntouched(t *testing.T) {
	home := setHome(t)
	files := map[string]string{
		".agents/mcp.json": `{
  "mcpServers": {
    "keep": {"command": "echo"}
  }
}
`,
		".grok/config.toml":            "theme = \"dark\"\n\n[mcp_servers.keep]\ncommand = \"echo\"\n",
		".cursor/mcp.json":             "{\"mcpServers\":{\"keep\":{\"command\":\"echo\"}}}\n",
		".claude.json":                 "{\"mcpServers\":{\"other\":{\"command\":\"echo\"}},\"numStartups\":1}\n",
		".agents/skills/keep/SKILL.md": "---\nname: keep\n---\n# Keep\n",
	}
	for rel, body := range files {
		writeFile(t, filepath.Join(home, rel), body)
	}
	ageTree(t, home)
	before := treeState(t, home)

	unchanged := func(t *testing.T) {
		t.Helper()
		after := treeState(t, home)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("files changed\nbefore %#v\nafter %#v", before, after)
		}
	}
	assertMissing := func(t *testing.T, args []string, message string) {
		t.Helper()
		out, err := runCLI(t, args...)
		code, stderr := statusOf(t, err)
		if code != ExitUnavailable || strings.TrimSpace(out) != "" || !strings.Contains(stderr, message) {
			t.Fatalf("%v code %d stderr %q out %q", args, code, stderr, out)
		}
		unchanged(t)
	}

	assertMissing(t, []string{"mcp", "remove", "missing"}, "no MCP server named missing")
	assertMissing(t, []string{"mcp", "remove", "missing", "--yes"}, "no MCP server named missing")
	assertMissing(t, []string{"mcp", "remove", "missing", "--dry-run"}, "no MCP server named missing")
	assertMissing(t, []string{"skills", "remove", "missing"}, "no skill named missing")
	assertMissing(t, []string{"skills", "remove", "missing", "--purge"}, "no skill named missing")
	assertMissing(t, []string{"skills", "remove", "missing", "--purge", "--yes"}, "no skill named missing")

	out, err := runCLI(t, "mcp", "remove", "missing", "--json")
	code, stderr := statusOf(t, err)
	if code != ExitUnavailable || stderr != "" {
		t.Fatalf("json code %d stderr %q out %q", code, stderr, out)
	}
	doc := decodeOne(t, out).(map[string]any)
	errObj := doc["error"].(map[string]any)
	if errObj["code"] != "unavailable" || errObj["exit"] != float64(3) || errObj["message"] != "no MCP server named missing" {
		t.Fatalf("%#v", doc)
	}
	unchanged(t)

	out, err = runCLI(t, "skills", "remove", "missing", "--json")
	code, stderr = statusOf(t, err)
	if code != ExitUnavailable || stderr != "" {
		t.Fatalf("json code %d stderr %q out %q", code, stderr, out)
	}
	doc = decodeOne(t, out).(map[string]any)
	errObj = doc["error"].(map[string]any)
	if errObj["code"] != "unavailable" || errObj["exit"] != float64(3) || errObj["message"] != "no skill named missing" {
		t.Fatalf("%#v", doc)
	}
	unchanged(t)
}
