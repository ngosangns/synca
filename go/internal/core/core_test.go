package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"  My Skill_Name ": "my-skill-name",
		"Foo":              "foo",
		"a_b c":            "a-b-c",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPiSkillNameOK(t *testing.T) {
	ok := []string{"abc", "a-b-c", "a1", "x" + string(make([]byte, 0))}
	_ = ok
	for _, n := range []string{"skill", "my-skill-2", "a"} {
		if !PiSkillNameOK(n) {
			t.Errorf("expected ok: %q", n)
		}
	}
	for _, n := range []string{"", "-lead", "trail-", "double--dash", "UPPER", "has space", "under_score"} {
		if PiSkillNameOK(n) {
			t.Errorf("expected reject: %q", n)
		}
	}
}

func TestSlugify(t *testing.T) {
	if got := SlugifySkillName("My Skill!"); got != "my-skill" {
		t.Errorf("got %q", got)
	}
	if got := SlugifySkillName("!!!"); got != "skill" {
		t.Errorf("fallback got %q", got)
	}
}

func TestCanonicalTransport(t *testing.T) {
	s := func(v string) *string { return &v }
	url := func(v string) *string { return &v }
	if got := CanonicalTransport(s("sse"), url("https://x.com/mcp"), false); got != "http" {
		t.Errorf("sse non-/sse endpoint → http, got %q", got)
	}
	if got := CanonicalTransport(s("sse"), url("https://x.com/sse"), false); got != "sse" {
		t.Errorf("sse /sse endpoint → sse, got %q", got)
	}
	if got := CanonicalTransport(s("local"), nil, true); got != "stdio" {
		t.Errorf("local → stdio, got %q", got)
	}
	if got := CanonicalTransport(s(""), url("https://x.com"), false); got != "http" {
		t.Errorf("empty+url → http, got %q", got)
	}
	if got := CanonicalTransport(s(""), nil, true); got != "stdio" {
		t.Errorf("empty+cmd → stdio, got %q", got)
	}
}

func TestParseCommandLine(t *testing.T) {
	got := ParseCommandLine(`npx -y "@scope/pkg" 'a b'`)
	want := []string{"npx", "-y", "@scope/pkg", "a b"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMcpFromCLI(t *testing.T) {
	cmd := "npx -y @pkg/server"
	n, err := McpFromCLI("stdio", &cmd, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Command == nil || n.Command[0] != "npx" {
		t.Fatalf("command %v", n.Command)
	}
	if len(n.Args) != 2 || n.Args[1] != "@pkg/server" {
		t.Fatalf("args %v", n.Args)
	}
	if _, err := McpFromCLI("stdio", nil, nil, nil); err == nil {
		t.Error("expected error for missing command")
	}
	u := "https://x.com/mcp"
	n2, err := McpFromCLI("http", nil, &u, nil)
	if err != nil || n2.URL == nil || *n2.URL != u {
		t.Errorf("http: %v %v", n2, err)
	}
}

func TestFrontmatterRepair(t *testing.T) {
	text := "---\nname: Bad Name\ndescription: a: b\ncustom: fine\n---\n# body\n"
	got := RepairSkillFrontmatter(text, "bad-name")
	if got == "" {
		t.Fatal("expected repair")
	}
	if !contains(got, "name: bad-name") {
		t.Errorf("name not repaired: %q", got)
	}
	if !contains(got, `description: "a: b"`) {
		t.Errorf("description not quoted: %q", got)
	}
	if RepairSkillFrontmatter("no frontmatter", "x") != "" {
		t.Error("expected no-op")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// Skill keys come from SKILL.md frontmatter; the folder name may differ.
func TestRemoveSkillWhenFolderNameDiffersFromKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	canon := filepath.Join(home, ".agents/skills/gitbutler")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canon, "SKILL.md"), []byte("---\nname: but\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cursorRoot := filepath.Join(home, ".cursor/skills")
	if err := os.MkdirAll(cursorRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cursorRoot, "gitbutler")
	if err := os.Symlink("../../.agents/skills/gitbutler", link); err != nil {
		t.Fatal(err)
	}

	// unlink keeps the canonical copy
	if _, _, err := RemoveSkill(ScopeUser, home, "but", nil, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("unlink left the agent link behind")
	}
	if _, err := os.Stat(canon); err != nil {
		t.Errorf("unlink must keep canonical: %v", err)
	}

	// purge removes it, and the skill disappears from the scan
	if err := os.Symlink("../../.agents/skills/gitbutler", link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RemoveSkill(ScopeUser, home, "but", nil, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(canon); err == nil {
		t.Error("purge left the canonical directory behind")
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("purge left the agent link behind")
	}
	for _, e := range ScanSkills(ScopeUser, home) {
		if e.Key == "but" {
			t.Error("purged skill still listed by scan")
		}
	}
}

func TestRemoveSkillDryRunTouchesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	canon := filepath.Join(home, ".agents/skills/gitbutler")
	_ = os.MkdirAll(canon, 0o755)
	_ = os.WriteFile(filepath.Join(canon, "SKILL.md"), []byte("---\nname: but\n---\n"), 0o644)
	plan, _, err := RemoveSkill(ScopeUser, home, "but", nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "purge_canonical_skill" {
		t.Errorf("unexpected plan: %+v", plan.Actions)
	}
	if _, err := os.Stat(canon); err != nil {
		t.Error("dry run deleted the skill")
	}
}

// keep-target must keep the copy that differs from the canonical one, even when
// an earlier agent merely symlinks to the canonical copy.
func TestKeepTargetSkipsSymlinkToCanonical(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write := func(dir, body string) {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: alpha\n---\n"+body+"\n"), 0o644)
	}
	canon := filepath.Join(home, ".agents/skills/alpha")
	write(canon, "CANON")
	_ = os.MkdirAll(filepath.Join(home, ".grok/skills"), 0o755)
	_ = os.Symlink("../../.agents/skills/alpha", filepath.Join(home, ".grok/skills/alpha"))
	write(filepath.Join(home, ".cursor/skills/alpha"), "CURSOR")

	plan, err := PlanSyncSkills(ScopeUser, home, nil, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	dec := NewDecisions(ConflictKeepTarget)
	if _, err := ApplyPlan(plan, home, ScopeUser, dec); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(canon, "SKILL.md"))
	if !strings.Contains(string(got), "CURSOR") {
		t.Errorf("keep-target kept the source instead: %q", got)
	}
}
