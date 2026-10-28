package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngosangns/synca/go/internal/core"
)

// githubSkillServer serves repository o/r with one skill "alpha" on both the
// fake GitHub API and the fake skills.sh download endpoint.
func githubSkillServer(t *testing.T, body *string) {
	t.Helper()
	tree := strings.Repeat("c", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/git/trees/HEAD":
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("a", 40), "tree": []map[string]string{
				{"path": "skills/alpha", "type": "tree", "sha": tree},
				{"path": "skills/alpha/SKILL.md", "type": "blob", "sha": "b"},
			}})
		case r.URL.Path == "/api/download/o/r/alpha":
			_ = json.NewEncoder(w).Encode(map[string]any{"hash": "h", "files": []map[string]string{{"path": "SKILL.md", "contents": *body}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("SKILLS_API_URL", srv.URL)
	t.Setenv("SKILLS_DOWNLOAD_URL", srv.URL)
	t.Setenv("SKILLS_API_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
}

func fakeHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	return home
}

func skillMD(desc string) string {
	return "---\nname: alpha\ndescription: " + desc + "\n---\nbody\n"
}

func TestInstallFromRegistryThenSyncRestoresAndUpdates(t *testing.T) {
	home := fakeHome(t)
	body := skillMD("one")
	githubSkillServer(t, &body)

	out, err := runCLI(t, "skills", "install", "o/r@alpha", "--agents", "claude", "--json")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "one") {
		t.Fatalf("installed %q", got)
	}
	if e := core.ReadLock(core.ScopeUser, home)["alpha"]; e.FolderHash != strings.Repeat("c", 40) || e.Source != "o/r" {
		t.Fatalf("lock %+v", e)
	}

	// Lose the canonical copy. sync skills must bring it back from skills.sh.
	if err := os.RemoveAll(filepath.Join(home, ".agents/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "sync", "skills", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("dry sync: %v\n%s", err, out)
	}
	plan := decodeOne(t, out).(map[string]any)
	mustValidate(t, "sync-plan.json", plan)
	first := plan["actions"].([]any)[0].(map[string]any)
	if first["kind"] != "fetch_skill" || first["skill_key"] != "alpha" || first["reason"] != "restore" {
		t.Fatalf("first action %v", first)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/alpha")); err == nil {
		t.Fatal("dry-run wrote")
	}

	if out, err = runCLI(t, "sync", "all", "--no-hooks", "--json"); err != nil {
		t.Fatalf("sync all: %v\n%s", err, out)
	}
	mustValidate(t, "sync-plan.json", decodeOne(t, out))
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/alpha/SKILL.md")); err != nil {
		t.Fatalf("not restored: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude/skills/alpha")); err != nil {
		// A claude skills root only exists when its directory does; creating it is sync's job.
		t.Logf("claude link: %v", err)
	}

	// The source moves on. Plain sync keeps the installed copy; --update takes the new one.
	body = skillMD("two")
	githubSkillServer(t, &body)
	treeMoved := func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repos/o/r/git/trees/HEAD" {
				_ = json.NewEncoder(w).Encode(map[string]any{"sha": "x", "tree": []map[string]string{
					{"path": "skills/alpha", "type": "tree", "sha": strings.Repeat("e", 40)},
					{"path": "skills/alpha/SKILL.md", "type": "blob", "sha": "b"},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hash": "h", "files": []map[string]string{{"path": "SKILL.md", "contents": body}}})
		}))
		t.Cleanup(srv.Close)
		t.Setenv("GITHUB_API_URL", srv.URL)
		t.Setenv("SKILLS_DOWNLOAD_URL", srv.URL)
	}
	treeMoved()
	if _, err := runCLI(t, "sync", "skills", "--json"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "one") {
		t.Errorf("sync without --update changed the skill: %q", got)
	}
	if _, err := runCLI(t, "sync", "skills", "--update", "--json"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "two") {
		t.Errorf("--update kept the old copy: %q", got)
	}
}

func TestSyncNoRegistryUsesNoNetwork(t *testing.T) {
	home := fakeHome(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(404) }))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("SKILLS_DOWNLOAD_URL", srv.URL)
	if err := core.RecordOrigin(core.LockPath(core.ScopeUser, home), "alpha", core.LockEntry{Source: "o/r", SourceType: "github", SkillPath: "skills/alpha/SKILL.md", FolderHash: strings.Repeat("c", 40)}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"sync", "skills", "--no-registry", "--json"},
		{"sync", "all", "--no-registry", "--no-hooks", "--dry-run", "--json"},
	} {
		if _, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if hits != 0 {
		t.Errorf("network used %d times", hits)
	}
	// With the registry step on, the same sync asks the server and survives its 404.
	if _, err := runCLI(t, "sync", "skills", "--json"); err != nil {
		t.Fatalf("sync with a failing registry: %v", err)
	}
	if hits == 0 {
		t.Error("registry step never asked the server")
	}
}

func TestRegistrySyncFlagsOnlyApplyToSkillsAndAll(t *testing.T) {
	fakeHome(t)
	for _, args := range [][]string{
		{"sync", "mcp", "--update", "--json"},
		{"sync", "mcp", "--no-registry", "--json"},
	} {
		out, err := runCLI(t, args...)
		if ExitCode(err) != ExitUsage {
			t.Errorf("%v: exit %d", args, ExitCode(err))
		}
		assertErrorCode(t, out, "usage")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
