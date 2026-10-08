package core

import "testing"

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

func TestEnsureVisibleParity(t *testing.T) {
	// ported from state.rs tests
	if got := ensureVisibleForTest(7, 0, 5, 20); got != 3 {
		t.Errorf("got %d", got)
	}
	if got := ensureVisibleForTest(2, 5, 5, 20); got != 2 {
		t.Errorf("got %d", got)
	}
	if got := ensureVisibleForTest(6, 4, 5, 20); got != 4 {
		t.Errorf("got %d", got)
	}
	if got := ensureVisibleForTest(0, 15, 5, 20); got != 0 {
		t.Errorf("got %d", got)
	}
	if got := ensureVisibleForTest(9, 0, 4, 10); got != 6 {
		t.Errorf("got %d", got)
	}
}

// ensureVisibleForTest mirrors tui.ensureVisible (kept here to pin the math
// against the Rust tests; the tui package has its own copy).
func ensureVisibleForTest(selected, offset, visible, length int) int {
	if length == 0 || visible == 0 {
		return 0
	}
	if selected > length-1 {
		selected = length - 1
	}
	maxOffset := length - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if selected < offset {
		offset = selected
	} else if selected >= offset+visible {
		offset = selected + 1 - visible
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	return offset
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
