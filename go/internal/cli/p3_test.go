package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func statusOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	return Finish(err, &stderr), stderr.String()
}

func setHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

func jsonObjectsWithKind(t *testing.T, text string) []any {
	t.Helper()
	var out []any
	for _, line := range strings.Split(text, "\n") {
		start := strings.Index(line, "{")
		end := strings.LastIndex(line, "}")
		if start < 0 || end <= start {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line[start:end+1]), &v); err != nil {
			continue
		}
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := obj["kind"]; !ok {
			continue
		}
		out = append(out, v)
	}
	if out == nil {
		return []any{}
	}
	return out
}

func loggedLines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "  note:") {
			continue
		}
		rest := strings.TrimPrefix(line, "  ")
		if rest != "" && rest[0] >= '0' && rest[0] <= '9' && strings.Contains(rest, ". {") {
			continue
		}
		out = append(out, rest)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func normHome(home string, lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.ReplaceAll(line, home, "HOME")
	}
	return out
}

func asStrings(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("%T", v)
	}
	out := make([]string, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("[%d] %T", i, item)
		}
		out[i] = s
	}
	return out
}

func TestRootHelpOmitsPiAndOmp(t *testing.T) {
	out, err := runCLI(t)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "omp") || strings.Contains(out, " pi") || strings.Contains(out, "Pi") {
		t.Fatalf("help still names Pi or OMP:\n%s", out)
	}
	if !strings.Contains(out, "doctor") || !strings.Contains(out, "skill") {
		t.Fatalf("help missing doctor or skill:\n%s", out)
	}
}

func TestSyncJSONMatchesTextPlan(t *testing.T) {
	contractHome(t)
	for _, sub := range []string{"skills", "mcp", "all"} {
		t.Run(sub, func(t *testing.T) {
			text, err := runCLI(t, "sync", sub, "--dry-run")
			if err != nil {
				t.Fatal(err)
			}
			out, err := runCLI(t, "sync", sub, "--dry-run", "--json")
			if err != nil {
				t.Fatal(err)
			}
			doc := decodeOne(t, out).(map[string]any)
			mustValidate(t, "sync-plan.json", doc)
			if doc["dryRun"] != true || doc["scope"] != "user" || doc["onConflict"] != "skip" {
				t.Fatalf("%#v", doc)
			}
			if _, ok := doc["applied"]; ok {
				t.Fatal("dry-run included applied")
			}
			actions := doc["actions"].([]any)
			if !reflect.DeepEqual(actions, jsonObjectsWithKind(t, text)) {
				t.Fatalf("text actions differ from JSON\ntext:\n%s\njson:\n%s", text, out)
			}
			head := "Sync plan (scope=user, actions="
			if !strings.HasPrefix(text, head) || !strings.HasSuffix(text, "(dry-run; no changes) on-conflict=skip\n") {
				t.Fatalf("text plan changed:\n%s", text)
			}
			if strings.Contains(text, "repair_skill_frontmatter") || strings.Contains(text, "collapse_skill_alias") {
				t.Fatalf("pi action still planned:\n%s", text)
			}
		})
	}
}

func TestSyncApplyJSONAndSkipConflicts(t *testing.T) {
	homeText := contractHome(t)
	text, err := runCLI(t, "sync", "skills")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "Sync plan (scope=user, actions=") {
		t.Fatalf("apply text:\n%s", text)
	}
	homeJSON := contractHome(t)
	out, err := runCLI(t, "sync", "skills", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "sync-plan.json", doc)
	if doc["dryRun"] != false {
		t.Fatal(doc["dryRun"])
	}
	applied := normHome(homeJSON, asStrings(t, doc["applied"]))
	if !reflect.DeepEqual(applied, normHome(homeText, loggedLines(text))) {
		t.Fatalf("applied %#v\ntext %q", applied, text)
	}

	home := contractHome(t)
	writeFile(t, filepath.Join(home, ".agents", "skills", "clash", "SKILL.md"), "---\nname: clash\n---\n# One\n")
	grok := filepath.Join(home, ".grok", "skills", "clash", "SKILL.md")
	writeFile(t, grok, "---\nname: clash\n---\n# Two\n")
	before := mustRead(t, grok)

	text, err = runCLI(t, "sync", "skills", "--key", "clash", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "sync", "skills", "--key", "clash", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "sync-plan.json", doc)
	conflicts := doc["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts %#v", conflicts)
	}
	row := conflicts[0].(map[string]any)
	if row["skill"] != true || row["key"] != "clash" || row["policy"] != "skip" {
		t.Fatalf("conflict %#v", row)
	}
	if !reflect.DeepEqual(doc["actions"], jsonObjectsWithKind(t, text)) {
		t.Fatalf("conflict plan\n%s\n%s", text, out)
	}

	out, err = runCLI(t, "sync", "skills", "--key", "clash", "--json")
	code, stderr := statusOf(t, err)
	if code != 0 || stderr != "" {
		t.Fatalf("skip apply code %d stderr %q err %v", code, stderr, err)
	}
	appliedDoc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "sync-plan.json", appliedDoc)
	if appliedDoc["dryRun"] != false || len(appliedDoc["conflicts"].([]any)) != 1 {
		t.Fatalf("%#v", appliedDoc)
	}
	if mustRead(t, grok) != before {
		t.Fatal("skip overwrote the agent copy")
	}
}

func TestDestructiveSyncNeedsYes(t *testing.T) {
	home := contractHome(t)
	canon := filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md")
	before := mustRead(t, canon)
	outside := t.TempDir()

	out, err := runCLI(t, "sync", "skills", "--scope", "project", "--cwd", outside, "--on-conflict", "keep-source")
	code, stderr := statusOf(t, err)
	if code != 3 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "no project root") || !strings.Contains(stderr, "hint:") {
		t.Fatalf("code %d stderr %q out %q", code, stderr, out)
	}

	for _, policy := range []string{"keep-source", "keep-target"} {
		out, err = runCLI(t, "sync", "skills", "--on-conflict", policy)
		code, stderr = statusOf(t, err)
		if code != 6 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "pass --yes") || !strings.Contains(stderr, "synca failed:") {
			t.Fatalf("%s code %d stderr %q out %q", policy, code, stderr, out)
		}
		if strings.Contains(out, "Sync plan") {
			t.Fatal("refused sync still printed the plan")
		}
		out, err = runCLI(t, "sync", "skills", "--on-conflict", policy, "--json")
		code, stderr = statusOf(t, err)
		if code != 6 || stderr != "" || strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
			t.Fatalf("%s json code %d stderr %q out %s", policy, code, stderr, out)
		}
		obj := decodeOne(t, out).(map[string]any)["error"].(map[string]any)
		if obj["code"] != "needs-confirm" || obj["exit"] != float64(6) || obj["hint"] != "pass --yes" {
			t.Fatalf("%#v", obj)
		}
		mustValidate(t, "error.json", decodeOne(t, out))

		out, err = runCLI(t, "sync", "skills", "--on-conflict", policy, "--dry-run")
		if err != nil || !strings.Contains(out, "on-conflict="+policy) {
			t.Fatalf("dry-run %s err %v\n%s", policy, err, out)
		}
	}
	if mustRead(t, canon) != before {
		t.Fatal("gated sync wrote the canonical skill")
	}

	out, err = runCLI(t, "sync", "skills", "--on-conflict", "keep-source", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "sync-plan.json", doc)
	if doc["dryRun"] != false || doc["onConflict"] != "keep-source" {
		t.Fatalf("%#v", doc)
	}
}

func TestSyncAllRejectsKeyAndUnknownAgentsMeanAll(t *testing.T) {
	out, err := runCLI(t, "sync", "all", "--key", "alpha", "--dry-run")
	assertUsageText(t, out, err, "sync all does not take --key")

	out, err = runCLI(t, "sync", "skills", "--bogus")
	assertUsageText(t, out, err, `unknown flag "--bogus"`)

	contractHome(t)
	all, err := runCLI(t, "sync", "skills", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	pi, err := runCLI(t, "sync", "skills", "--agents", "pi", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if all != pi {
		t.Fatalf("--agents pi changed the plan\nall:\n%s\npi:\n%s", all, pi)
	}
}

func TestManageJSONAndDestructiveGate(t *testing.T) {
	home := contractHome(t)
	demo := filepath.Join(home, ".agents", "mcp.json")
	before := mustRead(t, demo)
	alpha := filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md")
	alphaBefore := mustRead(t, alpha)

	text, err := runCLI(t, "mcp", "add", "added", "--transport", "stdio", "--command", "echo", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "mcp", "add", "added", "--transport", "stdio", "--command", "echo", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != true || !reflect.DeepEqual(doc["actions"], jsonObjectsWithKind(t, text)) {
		t.Fatalf("add plan\n%s\n%s", text, out)
	}
	if !strings.HasPrefix(text, "Manage plan (scope=user, dry_run=true, actions=") || !strings.HasSuffix(text, "(dry-run; no changes)\n") {
		t.Fatalf("manage text:\n%s", text)
	}
	if _, ok := doc["applied"]; ok {
		t.Fatal("dry-run manage included applied")
	}

	text, err = runCLI(t, "mcp", "remove", "demo", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "mcp", "remove", "demo", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if !reflect.DeepEqual(doc["actions"], jsonObjectsWithKind(t, text)) {
		t.Fatalf("remove plan\n%s\n%s", text, out)
	}
	if mustRead(t, demo) != before {
		t.Fatal("dry-run remove wrote mcp.json")
	}

	out, err = runCLI(t, "mcp", "remove", "demo")
	code, stderr := statusOf(t, err)
	if code != 6 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "pass --yes") {
		t.Fatalf("remove code %d stderr %q out %q", code, stderr, out)
	}
	out, err = runCLI(t, "skills", "remove", "alpha", "--purge")
	code, stderr = statusOf(t, err)
	if code != 6 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "pass --yes") {
		t.Fatalf("purge code %d stderr %q out %q", code, stderr, out)
	}
	if mustRead(t, alpha) != alphaBefore || mustRead(t, demo) != before {
		t.Fatal("gated remove wrote files")
	}

	text, err = runCLI(t, "skills", "remove", "alpha", "--purge", "--dry-run")
	if err != nil || !strings.Contains(text, "purge_canonical_skill") {
		t.Fatalf("purge dry-run %v\n%s", err, text)
	}

	freshText := setHome(t)
	text, err = runCLI(t, "mcp", "add", "added", "--transport", "stdio", "--command", "echo")
	if err != nil {
		t.Fatal(err)
	}
	freshJSON := setHome(t)
	out, err = runCLI(t, "mcp", "add", "added", "--transport", "stdio", "--command", "echo", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != false {
		t.Fatal(doc["dryRun"])
	}
	if !reflect.DeepEqual(normHome(freshJSON, asStrings(t, doc["applied"])), normHome(freshText, loggedLines(text))) {
		t.Fatalf("applied %#v\ntext %q", doc["applied"], text)
	}

	out, err = runCLI(t, "mcp", "remove", "added", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != false {
		t.Fatal("remove apply dryRun")
	}
}

func TestProviderClearJSONAndGate(t *testing.T) {
	home := contractHome(t)
	grokSkill := filepath.Join(home, ".grok", "skills", "alpha")
	if _, err := os.Lstat(grokSkill); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "provider", "clear", "kiro")
	code, stderr := statusOf(t, err)
	if code != 6 || strings.TrimSpace(out) != "" || !strings.Contains(stderr, "pass --yes") {
		t.Fatalf("code %d stderr %q out %q", code, stderr, out)
	}

	text, err := runCLI(t, "provider", "clear", "kiro", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "provider", "clear", "kiro", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "provider-clear.json", doc)
	if doc["dryRun"] != true || doc["provider"] != "kiro" {
		t.Fatalf("%#v", doc)
	}
	lines := asStrings(t, doc["lines"])
	if strings.Join(lines, "\n")+"\n" != text {
		t.Fatalf("lines %#v\ntext %q", lines, text)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "skills: nothing at ") || !strings.Contains(joined, "mcp: nothing at ") {
		t.Fatalf("empty provider text changed: %s", joined)
	}

	text, err = runCLI(t, "provider", "clear", "grok", "--dry-run")
	if err != nil || !strings.Contains(text, "would remove") {
		t.Fatalf("grok dry-run %v\n%s", err, text)
	}
	if _, err := os.Lstat(grokSkill); err != nil {
		t.Fatal("dry-run removed the grok skill")
	}

	out, err = runCLI(t, "provider", "clear", "grok", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "provider-clear.json", doc)
	if doc["dryRun"] != false || !strings.Contains(strings.Join(asStrings(t, doc["lines"]), "\n"), "removed") {
		t.Fatalf("%#v", doc)
	}

	out, err = runCLI(t, "provider", "clear", "nope", "--yes")
	assertUsageText(t, out, err, `unknown provider "nope"`)
}

func TestDoctorReports(t *testing.T) {
	home := setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Alpha\n")
	writeFile(t, filepath.Join(home, ".pi", "agent", "skills", "orphan", "SKILL.md"), "---\nname: orphan\n---\npi\n")
	writeFile(t, filepath.Join(home, ".omp", "agent", "skills", "orphan", "SKILL.md"), "---\nname: orphan\n---\nomp\n")
	writeFile(t, filepath.Join(home, ".pi", "agent", "mcp.json"), `{"mcpServers":{"pi-only":{"command":"echo"}}}`)

	text, err := runCLI(t, "doctor")
	code, stderr := statusOf(t, err)
	if code != 0 || stderr != "" || text != "doctor user: clean\n" {
		t.Fatalf("clean code %d stderr %q text %q err %v", code, stderr, text, err)
	}
	out, err := runCLI(t, "doctor", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "doctor.json", doc)
	if doc["ok"] != true || len(doc["drift"].([]any)) != 0 || len(doc["mcp"].([]any)) != 0 {
		t.Fatalf("%s", out)
	}

	writeFile(t, filepath.Join(home, ".grok", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Same\n")
	writeFile(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Same\n")
	text, err = runCLI(t, "doctor")
	code, stderr = statusOf(t, err)
	if code != 0 || strings.Contains(text, "drift ") {
		t.Fatalf("same content code %d text %q", code, text)
	}

	writeFile(t, filepath.Join(home, ".grok", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Different\n")
	before := mustRead(t, filepath.Join(home, ".grok", "skills", "alpha", "SKILL.md"))
	text, err = runCLI(t, "doctor")
	code, stderr = statusOf(t, err)
	if code != 8 || stderr != "" || !strings.Contains(text, "drift grok alpha ") || strings.Contains(text, "synca failed:") {
		t.Fatalf("drift code %d stderr %q text %q", code, stderr, text)
	}
	out, err = runCLI(t, "doctor", "--json")
	code, stderr = statusOf(t, err)
	if code != 8 || stderr != "" {
		t.Fatalf("drift json code %d stderr %q", code, stderr)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "doctor.json", doc)
	if doc["ok"] != false || len(doc["drift"].([]any)) == 0 {
		t.Fatalf("%s", out)
	}
	if mustRead(t, filepath.Join(home, ".grok", "skills", "alpha", "SKILL.md")) != before {
		t.Fatal("doctor wrote a skill")
	}

	home = setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Alpha\n")
	if err := os.MkdirAll(filepath.Join(home, ".grok", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/no/such/alpha", filepath.Join(home, ".grok", "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	text, err = runCLI(t, "doctor")
	code, _ = statusOf(t, err)
	if code != 8 || !strings.Contains(text, "dangling grok alpha ") || !strings.Contains(text, " -> ") {
		t.Fatalf("dangling code %d text %q err %v", code, text, err)
	}

	home = setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"), "---\nname: alpha\n---\n# Alpha\n")
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	text, err = runCLI(t, "doctor")
	code, _ = statusOf(t, err)
	if code != 8 || !strings.Contains(text, "missing cursor alpha ") {
		t.Fatalf("missing code %d text %q err %v", code, text, err)
	}

	home = setHome(t)
	writeFile(t, filepath.Join(home, ".agents", "mcp.json"), `{"mcpServers":{"demo":{"command":"echo"}}}`)
	writeFile(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers":{"demo":{"command":"other"},"extra":{"command":"echo"}}}`)
	out, err = runCLI(t, "doctor", "--json")
	code, stderr = statusOf(t, err)
	if code != 8 || stderr != "" {
		t.Fatalf("mcp code %d stderr %q err %v\n%s", code, stderr, err, out)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "doctor.json", doc)
	reasons := map[string]bool{}
	for _, item := range doc["mcp"].([]any) {
		reasons[item.(map[string]any)["reason"].(string)] = true
	}
	if !reasons["differs"] || !reasons["only in agent"] {
		t.Fatalf("mcp reasons %#v\n%s", reasons, out)
	}

	out, err = runCLI(t, "doctor", "--scope", "project", "--cwd", t.TempDir(), "--json")
	code, stderr = statusOf(t, err)
	if code != 3 || stderr != "" {
		t.Fatalf("project code %d stderr %q out %s", code, stderr, out)
	}
	obj := decodeOne(t, out).(map[string]any)["error"].(map[string]any)
	if obj["code"] != "unavailable" || !strings.Contains(obj["message"].(string), "no project root") {
		t.Fatalf("%#v", obj)
	}
}

func TestSkillInstallAndCheck(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "synca")
	out, err := runCLI(t, "skill", "install", "--dest", dest)
	if err != nil {
		t.Fatal(err)
	}
	if out != "installed synca skill at "+dest+"\n" {
		t.Fatalf("install text %q", out)
	}
	info, err := os.Stat(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 != 0 {
		t.Fatalf("mode %o", info.Mode())
	}
	out, err = runCLI(t, "skill", "check", "--dest", dest)
	if err != nil || out != "in sync "+dest+"\n" {
		t.Fatalf("check %v %q", err, out)
	}
	out, err = runCLI(t, "skill", "check", "--dest", dest, "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "skill-check.json", doc)
	if doc["inSync"] != true || doc["dest"] != dest {
		t.Fatalf("%#v", doc)
	}

	out, err = runCLI(t, "skill", "install", "--dest", dest, "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "skill-install.json", doc)
	if doc["installed"] != true || doc["dest"] != dest {
		t.Fatalf("%#v", doc)
	}

	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "skill", "check", "--dest", dest, "--json")
	code, stderr := statusOf(t, err)
	if code != 8 || stderr != "" {
		t.Fatalf("drift code %d stderr %q out %s", code, stderr, out)
	}
	doc = decodeOne(t, out).(map[string]any)
	mustValidate(t, "skill-check.json", doc)
	if doc["inSync"] != false || len(doc["different"].([]any)) != 1 {
		t.Fatalf("%#v", doc)
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "skill", "check", "--dest", empty)
	code, stderr = statusOf(t, err)
	if code != 8 || stderr != "" || !strings.Contains(out, "missing SKILL.md") {
		t.Fatalf("empty code %d stderr %q out %q", code, stderr, out)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	out, err = runCLI(t, "skill", "check", "--dest", missing, "--json")
	code, stderr = statusOf(t, err)
	if code != 3 || stderr != "" {
		t.Fatalf("missing code %d stderr %q out %s", code, stderr, out)
	}
	obj := decodeOne(t, out).(map[string]any)["error"].(map[string]any)
	if obj["code"] != "unavailable" || obj["hint"] != "run: synca skill install" {
		t.Fatalf("%#v", obj)
	}

	home := setHome(t)
	out, err = runCLI(t, "skill", "install")
	if err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(home, ".agents", "skills", "synca")
	if out != "installed synca skill at "+def+"\n" {
		t.Fatalf("default dest %q", out)
	}
	if _, err := os.Stat(filepath.Join(def, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", "")
	out, err = runCLI(t, "skill", "check")
	code, stderr = statusOf(t, err)
	if code != 3 || !strings.Contains(stderr, "HOME is not set") {
		t.Fatalf("home code %d stderr %q out %q err %v", code, stderr, out, err)
	}
}

func TestEmbeddedSkillMatchesRepo(t *testing.T) {
	repo := filepath.Join("..", "..", "..", "skills", "synca")
	want := map[string]string{}
	err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := embeddedSkillFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("embedded %d repo %d", len(got), len(want))
	}
	for name, body := range want {
		file, ok := got[name]
		if !ok || string(file.body) != body {
			t.Fatalf("embedded %s drifted from skills/synca", name)
		}
	}
}

func TestSkillsInstallDryRunJSON(t *testing.T) {
	setHome(t)
	src := filepath.Join("..", "..", "..", "skills", "synca")
	text, err := runCLI(t, "skills", "install", src, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "skills", "install", src, "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "manage-plan.json", doc)
	if doc["dryRun"] != true || !reflect.DeepEqual(doc["actions"], jsonObjectsWithKind(t, text)) {
		t.Fatalf("install plan\n%s\n%s", text, out)
	}
	if !reflect.DeepEqual(asStrings(t, doc["notes"]), noteLines(text)) {
		t.Fatalf("notes %#v\n%s", doc["notes"], text)
	}
	if !strings.HasPrefix(text, "Manage plan (scope=user, dry_run=true, actions=") || !strings.Contains(text, "copy_skill") {
		t.Fatalf("install text:\n%s", text)
	}
}

func noteLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		const prefix = "  note: "
		if strings.HasPrefix(line, prefix) {
			out = append(out, strings.TrimPrefix(line, prefix))
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
