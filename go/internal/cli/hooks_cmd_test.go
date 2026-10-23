package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withStdin(t *testing.T, input string, tty bool) {
	t.Helper()
	oldTTY, oldStdin := stdinIsTerminal, os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(input)
	_ = w.Close()
	os.Stdin = r
	stdinIsTerminal = func() bool { return tty }
	t.Cleanup(func() {
		os.Stdin = oldStdin
		stdinIsTerminal = oldTTY
		_ = r.Close()
	})
}

func exitOf(err error) int {
	var stderr strings.Builder
	return Finish(err, &stderrWriter{&stderr})
}

type stderrWriter struct{ b *strings.Builder }

func (w *stderrWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

func hooksSetup(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), "{\n  \"model\": \"x\"\n}\n")
	return home
}

func addGuard(t *testing.T) {
	t.Helper()
	out, err := runCLI(t, "hooks", "add", "guard", "--event", "PreToolUse", "--tools", "shell", "--command", "{{hook_dir}}/check.sh", "--timeout", "10", "--json")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	mustValidate(t, "hooks-change.json", decodeOne(t, out))
}

func TestHooksAddListValidate(t *testing.T) {
	hooksSetup(t)
	addGuard(t)

	out, err := runCLI(t, "hooks", "add", "guard", "--event", "Stop", "--command", "echo", "--json")
	if code := exitOf(err); code != ExitConflict {
		t.Fatalf("duplicate add exit %d\n%s", code, out)
	}
	out, err = runCLI(t, "hooks", "add", "bad", "--event", "Nope", "--command", "echo", "--json")
	if code := exitOf(err); code != ExitUsage {
		t.Fatalf("invalid add exit %d\n%s", code, out)
	}
	mustValidate(t, "error.json", decodeOne(t, out))

	out, err = runCLI(t, "hooks", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "hooks-list.json", doc)
	hooks := doc.(map[string]any)["hooks"].([]any)
	if len(hooks) != 1 || hooks[0].(map[string]any)["trusted"] != false {
		t.Fatalf("a new hook must start untrusted: %v", hooks)
	}
	if out, err = runCLI(t, "hooks", "list"); err != nil || !strings.Contains(out, "NOT TRUSTED") {
		t.Fatalf("text list: %v\n%s", err, out)
	}
	if _, err = runCLI(t, "hooks", "list", "--scope", "project"); exitOf(err) != ExitUsage {
		t.Fatal("project scope must be refused")
	}
}

func TestHooksTrustNeedsATerminalAndTypedApproval(t *testing.T) {
	hooksSetup(t)
	addGuard(t)

	withStdin(t, "yes\n", false)
	_, err := runCLI(t, "hooks", "trust", "guard")
	if exitOf(err) != ExitNeedsConfirm {
		t.Fatalf("off-TTY trust must exit %d, got %d", ExitNeedsConfirm, exitOf(err))
	}
	if _, err := runCLI(t, "hooks", "trust", "guard", "--yes"); exitOf(err) != ExitUsage {
		t.Fatal("trust must not accept --yes")
	}

	withStdin(t, "no\n", true)
	if _, err := runCLI(t, "hooks", "trust", "guard"); exitOf(err) != ExitNeedsConfirm {
		t.Fatal("anything but yes must not approve")
	}
	out, _ := runCLI(t, "hooks", "list", "--json")
	if !strings.Contains(out, `"trusted": false`) {
		t.Fatalf("hook trusted without approval:\n%s", out)
	}

	withStdin(t, "yes\n", true)
	if _, err := runCLI(t, "hooks", "trust", "guard"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, "hooks", "list", "--json")
	if !strings.Contains(out, `"trusted": true`) {
		t.Fatalf("approval not recorded:\n%s", out)
	}
	if _, err := runCLI(t, "hooks", "trust", "ghost"); exitOf(err) != ExitUnavailable {
		t.Fatal("unknown hook must be unavailable")
	}
}

func TestHooksSyncUntrustedStrictAndLifecycle(t *testing.T) {
	home := hooksSetup(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	orig, _ := os.ReadFile(settings)
	addGuard(t)

	out, err := runCLI(t, "hooks", "sync", "--agents", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "hooks-plan.json", doc)
	if doc.(map[string]any)["untrusted"].(float64) != 1 {
		t.Fatalf("expected 1 untrusted: %s", out)
	}
	now, _ := os.ReadFile(settings)
	if string(now) != string(orig) {
		t.Fatal("an untrusted hook was written")
	}
	if _, err := runCLI(t, "sync", "hooks", "--agents", "claude", "--strict", "--json"); exitOf(err) != ExitUnauthorized {
		t.Fatalf("--strict must exit 7, got %d", exitOf(err))
	}

	withStdin(t, "yes\n", true)
	if _, err := runCLI(t, "hooks", "trust", "guard"); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "sync", "hooks", "--agents", "claude", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "hooks-plan.json", decodeOne(t, out))
	if now, _ := os.ReadFile(settings); string(now) != string(orig) {
		t.Fatal("dry-run wrote the file")
	}
	out, err = runCLI(t, "sync", "hooks", "--agents", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "hooks-plan.json", decodeOne(t, out))
	if now, _ := os.ReadFile(settings); !strings.Contains(string(now), "check.sh") {
		t.Fatalf("approved hook not written:\n%s", now)
	}

	if _, err := runCLI(t, "hooks", "remove", "guard", "--json"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "hooks", "sync", "--agents", "claude", "--json"); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(settings); strings.Contains(string(now), "check.sh") || !strings.Contains(string(now), `"model": "x"`) {
		t.Fatalf("removal damaged the file:\n%s", now)
	}
}

func TestSyncAllIncludesHooksOnlyWithAHub(t *testing.T) {
	home := hooksSetup(t)

	out, err := runCLI(t, "sync", "all", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "sync-plan.json", doc)
	if _, ok := doc.(map[string]any)["hooks"]; ok {
		t.Fatal("sync all without a hooks hub must not add a hooks key")
	}

	writeFile(t, filepath.Join(home, ".agents", "hooks.json"), `{"version":1,"hooks":{"done":{"event":"Stop","handler":{"command":"echo done"}}}}`)
	out, err = runCLI(t, "sync", "all", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc = decodeOne(t, out)
	mustValidate(t, "sync-plan.json", doc)
	hooks, ok := doc.(map[string]any)["hooks"].(map[string]any)
	if !ok || hooks["untrusted"].(float64) != 1 {
		t.Fatalf("expected a hooks section with 1 untrusted: %s", out)
	}
	out, err = runCLI(t, "sync", "all", "--dry-run", "--no-hooks", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeOne(t, out).(map[string]any)["hooks"]; ok {
		t.Fatal("--no-hooks must leave hooks out")
	}
	if _, err := runCLI(t, "sync", "skills", "--strict", "--json"); exitOf(err) != ExitUsage {
		t.Fatal("--strict only applies to sync all and sync hooks")
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	before, _ := os.ReadFile(settings)
	if _, err := runCLI(t, "sync", "all", "--json"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(settings); string(after) != string(before) {
		t.Fatal("sync all wrote an unapproved hook")
	}
}
