package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo makes a repository with one commit and returns its path.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), body)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitCommit(t, dir, "first")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
}

func repoURL(dir string) string { return "file://" + dir }

func md(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
}

func TestLockOriginRoundTripKeepsUnknownFields(t *testing.T) {
	home := realTempHome(t)
	path := filepath.Join(home, ".agents", ".skill-lock.json")
	mustWrite(t, path, `{
  "version": 3,
  "skills": {
    "Alpha": {"source":"o/r","sourceType":"github","skillPath":"skills/alpha/SKILL.md","skillFolderHash":"abc","installedAt":"2026-01-01T00:00:00.000Z","extra":{"keep":true}}
  },
  "dismissed": {"x": true},
  "lastSelectedAgents": ["claude-code"]
}`)
	e := ReadLock(ScopeUser, home)["alpha"]
	if !e.Remote() || e.Source != "o/r" {
		t.Fatalf("entry %+v", e)
	}
	if err := RecordOrigin(path, "alpha", LockEntry{Source: "o/r", SourceType: "github", SkillPath: "skills/alpha/SKILL.md", FolderHash: "def"}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"dismissed", "lastSelectedAgents"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("%s dropped", k)
		}
	}
	var skills map[string]map[string]any
	_ = json.Unmarshal(doc["skills"], &skills)
	got, ok := skills["Alpha"]
	if !ok {
		t.Fatalf("entry renamed: %v", skills)
	}
	if got["skillFolderHash"] != "def" || got["installedAt"] != "2026-01-01T00:00:00.000Z" || got["extra"] == nil {
		t.Errorf("fields %v", got)
	}
	if got["updatedAt"] == nil {
		t.Error("updatedAt not set")
	}
	removed, err := ForgetOrigin(path, "alpha")
	if err != nil || !removed {
		t.Fatalf("forget %v %v", removed, err)
	}
	if _, ok := ReadLock(ScopeUser, home)["alpha"]; ok {
		t.Error("entry still there")
	}
	removed, err = ForgetOrigin(path, "alpha")
	if err != nil || removed {
		t.Errorf("second forget %v %v", removed, err)
	}
}

func TestLockUnparseableIsLeftAlone(t *testing.T) {
	home := realTempHome(t)
	path := filepath.Join(home, ".agents", ".skill-lock.json")
	mustWrite(t, path, "{not json")
	if err := RecordOrigin(path, "a", LockEntry{Source: "o/r", SourceType: "github"}); err == nil {
		t.Fatal("wrote into a broken lock file")
	}
	if data, _ := os.ReadFile(path); string(data) != "{not json" {
		t.Errorf("file changed: %q", data)
	}
	if len(ReadLock(ScopeUser, home)) != 0 {
		t.Error("broken lock gave entries")
	}
}

func TestLockPathHonoursXDGStateHome(t *testing.T) {
	home := realTempHome(t)
	if got := LockPath(ScopeUser, home); got != filepath.Join(home, ".agents", ".skill-lock.json") {
		t.Errorf("default %s", got)
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	if got := LockPath(ScopeUser, home); got != filepath.Join(state, "skills", ".skill-lock.json") {
		t.Errorf("xdg %s", got)
	}
}

func TestInstallFromGitRecordsOriginAndScanShowsRemote(t *testing.T) {
	home := realTempHome(t)
	repo := gitRepo(t, map[string]string{
		"skills/alpha/SKILL.md": md("alpha", "Alpha"),
		"skills/beta/SKILL.md":  md("beta", "Beta"),
	})
	if _, _, err := InstallSkill(ScopeUser, home, repoURL(repo), nil, false); err == nil || !strings.Contains(err.Error(), "multiple skills") {
		t.Fatalf("expected a multiple-skills error, got %v", err)
	}
	spec, err := ParseSource(repoURL(repo))
	if err != nil || spec.Type != "git" {
		t.Fatalf("spec %+v %v", spec, err)
	}
	r, err := CloneRepo(spec.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()
	dir, err := r.pick("beta")
	if err != nil {
		t.Fatal(err)
	}
	origin := r.originOf(spec, dir)
	if origin.SkillPath != "skills/beta/SKILL.md" || len(origin.FolderHash) != 40 {
		t.Fatalf("origin %+v", origin)
	}
	plan, err := PlanInstallSkill(ScopeUser, home, dir, nil, origin)
	if err != nil {
		t.Fatal(err)
	}
	last := plan.Actions[len(plan.Actions)-1]
	if last.Kind != "record_origin" || last.Source != spec.Source {
		t.Fatalf("last action %+v", last)
	}
	if _, err := ApplyManagePlan(plan, nil); err != nil {
		t.Fatal(err)
	}
	var found *SkillEntry
	for _, e := range ScanSkills(ScopeUser, home) {
		if e.Key == "beta" {
			e := e
			found = &e
		}
	}
	if found == nil || found.Origin != "remote" || found.Source == nil || *found.Source != spec.Source || *found.SourceType != "git" {
		t.Fatalf("scan entry %+v", found)
	}
}

func TestReinstallFromLocalFolderTurnsRemoteIntoLocal(t *testing.T) {
	home := realTempHome(t)
	canonical := filepath.Join(home, ".agents", "skills")
	mustWrite(t, filepath.Join(canonical, "alpha", "SKILL.md"), md("alpha", "A"))
	lock := LockPath(ScopeUser, home)
	if err := RecordOrigin(lock, "alpha", LockEntry{Source: "o/r", SourceType: "github", FolderHash: "x"}); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "alpha")
	mustWrite(t, filepath.Join(local, "SKILL.md"), md("alpha", "mine"))
	if _, _, err := InstallSkill(ScopeUser, home, local, nil, false); err != nil {
		t.Fatal(err)
	}
	for _, e := range ScanSkills(ScopeUser, home) {
		if e.Key == "alpha" && (e.Origin != "local" || e.Source != nil) {
			t.Errorf("origin %q source %v", e.Origin, e.Source)
		}
	}
}

func TestPurgeForgetsOriginAndUnlinkKeepsIt(t *testing.T) {
	home := realTempHome(t)
	src := filepath.Join(home, "src", "alpha")
	mustWrite(t, filepath.Join(src, "SKILL.md"), md("alpha", "A"))
	if _, _, err := InstallSkill(ScopeUser, home, src, nil, false); err != nil {
		t.Fatal(err)
	}
	lock := LockPath(ScopeUser, home)
	if err := RecordOrigin(lock, "alpha", LockEntry{Source: "o/r", SourceType: "github"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RemoveSkill(ScopeUser, home, "alpha", nil, false, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadLock(ScopeUser, home)["alpha"]; !ok {
		t.Error("unlink forgot the origin")
	}
	if _, _, err := RemoveSkill(ScopeUser, home, "alpha", nil, true, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadLock(ScopeUser, home)["alpha"]; ok {
		t.Error("purge kept the origin")
	}
}

func TestUpdateReinstallsOnlyChangedSkills(t *testing.T) {
	home := realTempHome(t)
	t.Setenv("SYNCA_NO_REGISTRY", "1")
	repo := gitRepo(t, map[string]string{
		"skills/alpha/SKILL.md": md("alpha", "one"),
		"skills/beta/SKILL.md":  md("beta", "one"),
	})
	spec := SourceSpec{URL: repoURL(repo), Source: "o/r", Type: "github"}
	r, err := CloneRepo(spec.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(r.Root, "skills", name)
		plan, err := PlanInstallSkill(ScopeUser, home, dir, nil, r.originOf(spec, dir))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyManagePlan(plan, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.Cleanup()
	// The lock URL is a file:// URL so the update can clone it.
	lock := LockPath(ScopeUser, home)
	for _, name := range []string{"alpha", "beta"} {
		e := ReadLock(ScopeUser, home)[name]
		e.SourceURL = repoURL(repo)
		if err := RecordOrigin(lock, name, e); err != nil {
			t.Fatal(err)
		}
	}

	plan, _, err := PlanUpdateSkills(ScopeUser, home, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 || !notesHave(plan.Notes, "up to date: alpha") || !notesHave(plan.Notes, "up to date: beta") {
		t.Fatalf("clean check: %+v", plan)
	}

	mustWrite(t, filepath.Join(repo, "skills/alpha/SKILL.md"), md("alpha", "two"))
	gitCommit(t, repo, "change alpha")

	plan, _, err = PlanUpdateSkills(ScopeUser, home, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.DryRun || !notesHave(plan.Notes, "update: alpha") || !notesHave(plan.Notes, "up to date: beta") {
		t.Fatalf("dry check: %+v", plan.Notes)
	}
	kinds := map[string]int{}
	for _, a := range plan.Actions {
		kinds[a.Kind]++
		if a.SkillKey != "alpha" {
			t.Errorf("action for %s", a.SkillKey)
		}
	}
	if kinds["copy_skill"] != 1 || kinds["record_origin"] != 1 {
		t.Fatalf("kinds %v", kinds)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "one") {
		t.Fatal("dry run changed the skill")
	}

	plan, log, err := PlanUpdateSkills(ScopeUser, home, []string{"alpha"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.DryRun || len(log) == 0 {
		t.Fatalf("apply: %+v %v", plan, log)
	}
	if got := readFile(t, filepath.Join(home, ".agents/skills/alpha/SKILL.md")); !strings.Contains(got, "two") {
		t.Errorf("not updated: %s", got)
	}
	plan, _, _ = PlanUpdateSkills(ScopeUser, home, nil, nil, false)
	if len(plan.Actions) != 0 {
		t.Errorf("still outdated after update: %v", plan.Notes)
	}
	if _, _, err := PlanUpdateSkills(ScopeUser, home, []string{"nope"}, nil, false); err == nil {
		t.Error("unknown name accepted")
	}
}

func TestRestoreReinstallsMissingRemoteSkills(t *testing.T) {
	home := realTempHome(t)
	t.Setenv("SYNCA_NO_REGISTRY", "1")
	repo := gitRepo(t, map[string]string{"skills/alpha/SKILL.md": md("alpha", "one")})
	lock := LockPath(ScopeUser, home)
	if err := RecordOrigin(lock, "alpha", LockEntry{
		Source: "o/r", SourceType: "git", SourceURL: repoURL(repo), SkillPath: "skills/alpha/SKILL.md", FolderHash: "x",
	}); err != nil {
		t.Fatal(err)
	}
	if err := RecordOrigin(lock, "gone", LockEntry{Source: "o/r", SourceType: "local"}); err != nil {
		t.Fatal(err)
	}
	plan, _, err := PlanRestoreSkills(ScopeUser, home, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !notesHave(plan.Notes, "restore: alpha") || len(plan.Actions) == 0 {
		t.Fatalf("plan %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/alpha")); err == nil {
		t.Fatal("dry run wrote")
	}
	if _, _, err := PlanRestoreSkills(ScopeUser, home, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents/skills/alpha/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	plan, _, _ = PlanRestoreSkills(ScopeUser, home, nil, nil, false)
	if len(plan.Actions) != 0 || !notesHave(plan.Notes, "present: alpha") {
		t.Errorf("second restore %+v", plan)
	}
}

func TestDiscoverSkillsFindsNestedAndNamedSkills(t *testing.T) {
	repo := gitRepo(t, map[string]string{
		"skills/alpha/SKILL.md":          md("Alpha Tool", "A"),
		"skills/.curated/beta/SKILL.md":  md("beta", "B"),
		"node_modules/x/SKILL.md":        md("ignored", "I"),
		"skills/alpha/examples/SKILL.md": md("inner", "never reached"),
		"README.md":                      "x",
	})
	skills, err := DiscoverSkills(repo)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, s := range skills {
		slugs = append(slugs, s.Slug)
	}
	if strings.Join(slugs, ",") != "beta,alpha-tool" && strings.Join(slugs, ",") != "alpha-tool,beta" {
		t.Fatalf("slugs %v", slugs)
	}
	if s := findDiscovered(skills, "alpha"); s == nil || s.Name != "Alpha Tool" {
		t.Errorf("folder match %+v", s)
	}
	if s := findDiscovered(skills, "Alpha Tool"); s == nil {
		t.Error("name match missed")
	}
	if findDiscovered(skills, "missing") != nil {
		t.Error("phantom match")
	}
}

func TestInitSkill(t *testing.T) {
	dir := t.TempDir()
	out, err := InitSkill("My Skill", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "my-skill" {
		t.Errorf("dir %s", out)
	}
	if n, ok := skillFrontmatterName(filepath.Join(out, "SKILL.md")); !ok || n != "my-skill" {
		t.Errorf("name %q %v", n, ok)
	}
	if _, err := InitSkill("My Skill", dir); err == nil {
		t.Error("overwrote an existing skill")
	}
	if _, err := InitSkill("!!!", dir); err == nil {
		t.Error("accepted an empty slug")
	}
}

func notesHave(notes []string, want string) bool {
	for _, n := range notes {
		if n == want {
			return true
		}
	}
	return false
}
