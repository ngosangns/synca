package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRegistry serves one GitHub repository o/r with the skills in files
// (folder -> {file -> text}), through the two hosts synca asks: the GitHub tree
// API and the skills.sh snapshot download.
type fakeRegistry struct {
	t        *testing.T
	repoSHA  string
	folders  map[string]string // folder -> tree sha
	files    map[string]map[string]string
	slugs    map[string]string // slug -> folder
	treeHits atomic.Int32
	dlHits   atomic.Int32
	failTree bool
	failDl   bool
	extra    []string // extra blob paths in the tree that the snapshot lacks
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	f := &fakeRegistry{t: t, repoSHA: strings.Repeat("a", 40), folders: map[string]string{}, files: map[string]map[string]string{}, slugs: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("SKILLS_API_URL", srv.URL)
	t.Setenv("SKILLS_DOWNLOAD_URL", srv.URL)
	t.Setenv("SKILLS_API_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	return f
}

func (f *fakeRegistry) add(slug, folder, sha string, files map[string]string) {
	f.folders[folder] = sha
	f.files[folder] = files
	f.slugs[slug] = folder
}

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/repos/o/r/git/trees/HEAD":
		f.treeHits.Add(1)
		if f.failTree {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var tree []map[string]string
		for folder, sha := range f.folders {
			if folder != "" {
				tree = append(tree, map[string]string{"path": folder, "type": "tree", "sha": sha})
			}
			for name := range f.files[folder] {
				p := name
				if folder != "" {
					p = folder + "/" + name
				}
				tree = append(tree, map[string]string{"path": p, "type": "blob", "sha": "b"})
			}
		}
		for _, p := range f.extra {
			tree = append(tree, map[string]string{"path": p, "type": "blob", "sha": "b"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": f.repoSHA, "truncated": false, "tree": tree})
	case strings.HasPrefix(r.URL.Path, "/api/download/o/r/"):
		f.dlHits.Add(1)
		slug := strings.TrimPrefix(r.URL.Path, "/api/download/o/r/")
		folder, ok := f.slugs[slug]
		if !ok || f.failDl {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var files []map[string]string
		for name, body := range f.files[folder] {
			files = append(files, map[string]string{"path": name, "contents": body})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"files": files, "hash": "h"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestInstallOwnerRepoAtSkillUsesRegistryAndRecordsTreeSHA(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	sha := strings.Repeat("c", 40)
	reg.add("alpha", "skills/alpha", sha, map[string]string{
		"SKILL.md":       md("alpha", "A"),
		"scripts/run.sh": "echo hi",
	})
	reg.add("beta", "skills/beta", strings.Repeat("d", 40), map[string]string{"SKILL.md": md("beta", "B")})

	plan, _, err := InstallSkill(ScopeUser, home, "o/r@alpha", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if reg.dlHits.Load() != 1 {
		t.Errorf("snapshot downloads: %d", reg.dlHits.Load())
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/scripts/run.sh")); got != "echo hi" {
		t.Errorf("supporting file: %q", got)
	}
	var origin *LockEntry
	for _, a := range plan.Actions {
		if a.Kind == "record_origin" {
			origin = a.Origin
		}
	}
	if origin == nil || origin.FolderHash != sha || origin.SkillPath != "skills/alpha/SKILL.md" || origin.Source != "o/r" || origin.SourceType != "github" {
		t.Fatalf("origin %+v", origin)
	}
	if e := ReadLock(ScopeUser, home)["alpha"]; e.FolderHash != sha {
		t.Errorf("lock %+v", e)
	}
	// No clone happened: git was never asked for o/r.
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/beta")); err == nil {
		t.Error("installed an unrequested skill")
	}
}

func TestInstallFallsBackToGitWhenRegistryCannotServe(t *testing.T) {
	cases := map[string]func(*fakeRegistry){
		"tree unavailable":  func(f *fakeRegistry) { f.failTree = true },
		"download missing":  func(f *fakeRegistry) { f.failDl = true },
		"incomplete files":  func(f *fakeRegistry) { f.extra = []string{"skills/alpha/references/big.md"} },
		"skill not listed":  func(f *fakeRegistry) { delete(f.slugs, "alpha") },
		"unknown repo path": func(f *fakeRegistry) { f.folders = map[string]string{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			reg := newFakeRegistry(t)
			reg.add("alpha", "skills/alpha", strings.Repeat("c", 40), map[string]string{"SKILL.md": md("alpha", "A")})
			mutate(reg)
			if _, err := fetchGitHub("o", "r", "alpha"); err != errNoSnapshot {
				t.Fatalf("got %v, want errNoSnapshot", err)
			}
		})
	}
}

func TestSnapshotRejectsUnsafePaths(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.add("alpha", "skills/alpha", strings.Repeat("c", 40), map[string]string{"SKILL.md": md("alpha", "A"), "../evil": "x"})
	if _, err := fetchGitHub("o", "r", "alpha"); err == nil || err == errNoSnapshot {
		t.Fatalf("unsafe path accepted: %v", err)
	}
}

func TestSnapshotSkipsMetadataAndGitDirs(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.add("alpha", "skills/alpha", strings.Repeat("c", 40), map[string]string{
		"SKILL.md":      md("alpha", "A"),
		"metadata.json": "{}",
		".git/config":   "x",
	})
	res, err := fetchGitHub("o", "r", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Cleanup()
	for _, skipped := range []string{"metadata.json", ".git/config"} {
		if _, err := os.Stat(filepath.Join(res.Dir, filepath.FromSlash(skipped))); err == nil {
			t.Errorf("%s was written", skipped)
		}
	}
}

func TestUpdateUsesOneTreeRequestAndNoCloneWhenUnchanged(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	shaA, shaB := strings.Repeat("1", 40), strings.Repeat("2", 40)
	reg.add("alpha", "skills/alpha", shaA, map[string]string{"SKILL.md": md("alpha", "A")})
	reg.add("beta", "skills/beta", shaB, map[string]string{"SKILL.md": md("beta", "B")})
	for _, n := range []string{"alpha", "beta"} {
		if _, _, err := InstallSkill(ScopeUser, home, "o/r@"+n, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	reg.treeHits.Store(0)
	reg.dlHits.Store(0)
	t.Setenv("PATH", t.TempDir()) // git is not reachable: any clone fails

	plan, _, err := PlanUpdateSkills(ScopeUser, home, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || !notesHave(plan.Notes, "up to date: alpha") || !notesHave(plan.Notes, "up to date: beta") {
		t.Fatalf("plan %+v", plan)
	}
	if reg.treeHits.Load() != 1 || reg.dlHits.Load() != 0 {
		t.Errorf("tree %d downloads %d", reg.treeHits.Load(), reg.dlHits.Load())
	}

	reg.folders["skills/alpha"] = strings.Repeat("9", 40)
	reg.files["skills/alpha"]["SKILL.md"] = md("alpha", "changed")
	plan, log, err := PlanUpdateSkills(ScopeUser, home, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !notesHave(plan.Notes, "update: alpha") || !notesHave(plan.Notes, "up to date: beta") || len(log) == 0 {
		t.Fatalf("plan %+v log %v", plan.Notes, log)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "changed") {
		t.Errorf("not updated: %s", got)
	}
	if e := ReadLock(ScopeUser, home)["alpha"]; e.FolderHash != strings.Repeat("9", 40) {
		t.Errorf("lock hash %s", e.FolderHash)
	}
	if reg.dlHits.Load() != 1 {
		t.Errorf("only the changed skill should download, got %d", reg.dlHits.Load())
	}
}

func TestRestoreUsesRegistryWithoutGit(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	reg.add("alpha", "skills/alpha", strings.Repeat("1", 40), map[string]string{"SKILL.md": md("alpha", "A")})
	if _, _, err := InstallSkill(ScopeUser, home, "o/r@alpha", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(home, ".agents/skills/alpha")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	plan, _, err := PlanRestoreSkills(ScopeUser, home, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !notesHave(plan.Notes, "restore: alpha") {
		t.Fatalf("notes %v", plan.Notes)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/alpha/SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncFromRegistryRestoresThenUpdatesOnlyWhenAsked(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	reg.add("alpha", "skills/alpha", strings.Repeat("1", 40), map[string]string{"SKILL.md": md("alpha", "A")})
	reg.add("beta", "skills/beta", strings.Repeat("2", 40), map[string]string{"SKILL.md": md("beta", "B")})
	for _, n := range []string{"alpha", "beta"} {
		if _, _, err := InstallSkill(ScopeUser, home, "o/r@"+n, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(home, ".agents/skills/beta")); err != nil {
		t.Fatal(err)
	}
	reg.folders["skills/alpha"] = strings.Repeat("9", 40)
	reg.files["skills/alpha"]["SKILL.md"] = md("alpha", "changed")

	res, err := SyncFromRegistry(ScopeUser, home, nil, "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Kind != "fetch_skill" || res.Actions[0].SkillKey != "beta" || res.Actions[0].Reason != "restore" {
		t.Fatalf("dry plan %+v", res.Actions)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/beta")); err == nil {
		t.Fatal("dry run wrote")
	}

	if _, err := SyncFromRegistry(ScopeUser, home, nil, "", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/beta/SKILL.md")); err != nil {
		t.Error("beta not restored")
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); strings.Contains(got, "changed") {
		t.Error("sync updated without --update")
	}

	res, err = SyncFromRegistry(ScopeUser, home, nil, "", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "changed") {
		t.Errorf("--update did not update: %v", res.Actions)
	}
}

func TestSyncFromRegistryLeavesLocalSkillsAndSurvivesOutage(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	reg.failTree = true
	reg.failDl = true
	t.Setenv("PATH", t.TempDir())
	mustWrite(t, filepath.Join(home, ".agents/skills/mine/SKILL.md"), md("mine", "local"))
	if err := RecordOrigin(LockPath(ScopeUser, home), "gone", LockEntry{Source: "o/r", SourceType: "github", SkillPath: "skills/gone/SKILL.md", FolderHash: strings.Repeat("1", 40)}); err != nil {
		t.Fatal(err)
	}
	res, err := SyncFromRegistry(ScopeUser, home, nil, "", true, true)
	if err != nil {
		t.Fatalf("outage must not fail the sync: %v", err)
	}
	if len(res.Actions) != 0 {
		t.Errorf("actions %+v", res.Actions)
	}
	var warned bool
	for _, w := range res.Warnings {
		warned = warned || strings.Contains(w, "gone")
	}
	if !warned {
		t.Errorf("no warning for the skill that could not be fetched: %v", res.Warnings)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/mine/SKILL.md")); !strings.Contains(got, "local") {
		t.Error("local skill touched")
	}
	if reg.dlHits.Load() != 0 {
		t.Errorf("downloads %d", reg.dlHits.Load())
	}
}

func TestRegistryOffSkipsTheNetwork(t *testing.T) {
	home := realTempHome(t)
	reg := newFakeRegistry(t)
	reg.add("alpha", "skills/alpha", strings.Repeat("1", 40), map[string]string{"SKILL.md": md("alpha", "A")})
	t.Setenv("SYNCA_NO_REGISTRY", "1")
	t.Setenv("PATH", t.TempDir())
	if _, _, err := InstallSkill(ScopeUser, home, "o/r@alpha", nil, false); err == nil {
		t.Fatal("install succeeded without registry and git")
	}
	if reg.treeHits.Load() != 0 || reg.dlHits.Load() != 0 {
		t.Errorf("network used: tree %d dl %d", reg.treeHits.Load(), reg.dlHits.Load())
	}
}
