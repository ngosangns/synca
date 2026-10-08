package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func sp(v string) *string { return &v }
func bp(v bool) *bool     { return &v }

func TestSkillFrontmatterAndDisplayName(t *testing.T) {
	tmp := t.TempDir()
	skill := filepath.Join(tmp, "my-skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(skill, "SKILL.md")
	if err := os.WriteFile(md, []byte("---\nname: Fancy Name\ndescription: x\n---\n# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, ok := skillFrontmatterName(md); !ok || n != "Fancy Name" {
		t.Errorf("name = %q, %v", n, ok)
	}
	if d, ok := skillFrontmatterDescription(md); !ok || d != "x" {
		t.Errorf("description = %q, %v", d, ok)
	}
	if got := SkillDisplayName(skill); got != "Fancy Name" {
		t.Errorf("display name = %q", got)
	}
	if d := skillDescription(skill); d == nil || *d != "x" {
		t.Errorf("skillDescription = %v", d)
	}

	skill2 := filepath.Join(tmp, "folder-only")
	if err := os.MkdirAll(skill2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill2, "SKILL.md"), []byte("# no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SkillDisplayName(skill2); got != "folder-only" {
		t.Errorf("display name fallback = %q", got)
	}
	if d := skillDescription(skill2); d != nil {
		t.Errorf("expected nil description, got %q", *d)
	}
}

func TestDescriptionSingleLineAndBlock(t *testing.T) {
	md := filepath.Join(t.TempDir(), "SKILL.md")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(md, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("---\nname: n\ndescription: Hello world skill\n---\n# body\n")
	if d, ok := skillFrontmatterDescription(md); !ok || d != "Hello world skill" {
		t.Errorf("single line = %q, %v", d, ok)
	}
	write("---\nname: n\ndescription: |\n  line one\n  line two\n---\n")
	if d, ok := skillFrontmatterDescription(md); !ok || d != "line one line two" {
		t.Errorf("block = %q, %v", d, ok)
	}
	write("---\nname: n\n---\n# no desc\n")
	if d, ok := skillFrontmatterDescription(md); ok {
		t.Errorf("expected none, got %q", d)
	}
}

func TestMcpNormalizeJSONCommandStringAndArray(t *testing.T) {
	s := jsonToNormalized(map[string]any{
		"type":    "stdio",
		"command": "npx",
		"args":    []any{"-y", "foo"},
		"env":     map[string]any{"TOKEN": "secret"},
	})
	if s.Transport != "stdio" {
		t.Errorf("transport %q", s.Transport)
	}
	if !reflect.DeepEqual(s.Command, []string{"npx"}) {
		t.Errorf("command %v", s.Command)
	}
	if !reflect.DeepEqual(s.Args, []string{"-y", "foo"}) {
		t.Errorf("args %v", s.Args)
	}
	if s.Env["TOKEN"] != "secret" {
		t.Errorf("env %v", s.Env)
	}
	if !reflect.DeepEqual(s.EnvKeys, []string{"TOKEN"}) {
		t.Errorf("env keys %v", s.EnvKeys)
	}

	a := jsonToNormalized(map[string]any{
		"command": []any{"python", "-m", "server"},
		"url":     nil,
	})
	if !reflect.DeepEqual(a.Command, []string{"python"}) {
		t.Errorf("array command %v", a.Command)
	}
	if !reflect.DeepEqual(a.Args, []string{"-m", "server"}) {
		t.Errorf("array args %v", a.Args)
	}
	if a.Transport != "stdio" {
		t.Errorf("transport %q", a.Transport)
	}

	u := jsonToNormalized(map[string]any{"url": "https://example.com/sse"})
	if u.Transport != "sse" {
		t.Errorf("sse transport %q", u.Transport)
	}
	if u.URL == nil || *u.URL != "https://example.com/sse" {
		t.Errorf("url %v", u.URL)
	}
	bare := jsonToNormalized(map[string]any{"url": "https://example.com/mcp"})
	if bare.Transport != "http" {
		t.Errorf("bare url transport %q", bare.Transport)
	}
}

func TestMcpNormalizeTomlAndOpencode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := "[mcp_servers.svc]\ntype = \"stdio\"\ncommand = \"uvx\"\nargs = [\"mcp-server\"]\n[mcp_servers.svc.env]\nKEY = \"val\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	servers, err := ReadMcpServers(AgentGrok, path)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := servers["svc"]
	if !ok {
		t.Fatalf("svc missing: %v", servers)
	}
	if !reflect.DeepEqual(n.Command, []string{"uvx"}) {
		t.Errorf("command %v", n.Command)
	}
	if !reflect.DeepEqual(n.Args, []string{"mcp-server"}) {
		t.Errorf("args %v", n.Args)
	}
	if n.Env["KEY"] != "val" {
		t.Errorf("env %v", n.Env)
	}

	oc := jsonToNormalized(map[string]any{
		"type":    "remote",
		"url":     "https://mcp.example",
		"enabled": true,
	})
	if oc.Transport != "http" {
		t.Errorf("remote transport %q", oc.Transport)
	}
	if oc.Enabled == nil || !*oc.Enabled {
		t.Errorf("enabled %v", oc.Enabled)
	}
}

func TestMcpTransportAliasesCollapse(t *testing.T) {
	cases := []struct {
		raw    *string
		url    *string
		hasCmd bool
		want   string
	}{
		{sp("local"), nil, true, "stdio"},
		{sp("remote"), sp("https://x/mcp"), false, "http"},
		{sp("streamable-http"), sp("https://x/mcp"), false, "http"},
		{sp("sse"), sp("https://x/mcp"), false, "http"},
		{sp("sse"), sp("https://x/sse"), false, "sse"},
		{nil, nil, false, "stdio"},
	}
	for _, c := range cases {
		if got := CanonicalTransport(c.raw, c.url, c.hasCmd); got != c.want {
			t.Errorf("CanonicalTransport(%v, %v, %v) = %q want %q", c.raw, c.url, c.hasCmd, got, c.want)
		}
	}
}

func TestMcpCommandArraySplitsForStableFingerprint(t *testing.T) {
	oc := jsonToNormalized(map[string]any{"type": "local", "command": []any{"uvx", "--from", "pkg", "serve"}})
	std := jsonToNormalized(map[string]any{"type": "stdio", "command": "uvx", "args": []any{"--from", "pkg", "serve"}})
	if !reflect.DeepEqual(oc.Command, []string{"uvx"}) {
		t.Errorf("command %v", oc.Command)
	}
	if !reflect.DeepEqual(oc.Args, std.Args) {
		t.Errorf("args %v vs %v", oc.Args, std.Args)
	}
	if McpFingerprint(oc) != McpFingerprint(std) {
		t.Error("fingerprints differ for equivalent configs")
	}
}

func TestMcpFingerprintIgnoresEnvValues(t *testing.T) {
	a := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"npx"},
		Enabled:   bp(true),
		EnvKeys:   []string{"TOKEN"},
		Env:       map[string]string{"TOKEN": "aaa"},
	}
	b := &McpNormalized{
		Transport: "stdio",
		Command:   []string{"npx"},
		Enabled:   bp(false), // enabled is ignored by the fingerprint
		EnvKeys:   []string{"TOKEN"},
		Env:       map[string]string{"TOKEN": "bbb"},
	}
	if McpFingerprint(a) != McpFingerprint(b) {
		t.Error("fingerprint must ignore env values and enabled")
	}
	c := *a
	c.Command = []string{"other"}
	if McpFingerprint(a) == McpFingerprint(&c) {
		t.Error("fingerprint must change with command")
	}
}

func TestConflictPolicyParse(t *testing.T) {
	cases := map[string]ConflictPolicy{
		"keep-source": ConflictKeepSource,
		"keep-target": ConflictKeepTarget,
		"skip":        ConflictSkip,
	}
	for in, want := range cases {
		got, ok := ParseConflictPolicy(in)
		if !ok || got != want {
			t.Errorf("ParseConflictPolicy(%q) = %v, %v", in, got, ok)
		}
	}
	if _, ok := ParseConflictPolicy("nope"); ok {
		t.Error("expected nope to be rejected")
	}
}
