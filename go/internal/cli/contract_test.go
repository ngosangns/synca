package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ngosangns/synca/go/internal/core"
	"github.com/ngosangns/synca/go/internal/schema"
)

func TestMain(m *testing.M) {
	stdinIsTerminal = func() bool { return false }
	os.Exit(m.Run())
}

// stdoutMu serializes tests that swap os.Stdout. Run writes the whole
// document before returning, so the pipe is drained at the same time.
var stdoutMu sync.Mutex

func runCLI(t *testing.T, args ...string) (out string, runErr error) {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	defer func() {
		os.Stdout = old
		_ = w.Close()
		<-done
		_ = r.Close()
		out = buf.String()
	}()
	runErr = Run(args)
	return out, runErr
}

func decodeOne(t *testing.T, out string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("trailing JSON: %v", err)
	}
	return v
}

func decodeFile(t *testing.T, path string) any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func loadSchemaDoc(t *testing.T, name string) any {
	t.Helper()
	b, err := schema.Read(name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func mustValidate(t *testing.T, schemaName string, instance any) {
	t.Helper()
	if err := validateSchema(loadSchemaDoc(t, schemaName), instance); err != nil {
		t.Fatalf("%s: %v", schemaName, err)
	}
}

func TestEmbeddedSchemasAreValidJSON(t *testing.T) {
	names, err := schema.Names()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"doctor.json",
		"error.json",
		"hooks-change.json",
		"hooks-list.json",
		"hooks-plan.json",
		"hooks-trust.json",
		"manage-plan.json",
		"mcp-list.json",
		"provider-clear.json",
		"schema.json",
		"skill-check.json",
		"skill-install.json",
		"skills-list.json",
		"sync-plan.json",
		"update.json",
		"version.json",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("schemas %v", names)
	}
	for _, name := range names {
		doc := loadSchemaDoc(t, name).(map[string]any)
		if doc["$schema"] != schema.Draft {
			t.Errorf("%s $schema = %v", name, doc["$schema"])
		}
	}
}

func TestGoldenFilesMatchSchemas(t *testing.T) {
	cases := []struct {
		file   string
		schema string
	}{
		{"skills-list.json", "skills-list.json"},
		{"mcp-list.json", "mcp-list.json"},
		{"version.json", "version.json"},
		{"update.json", "update.json"},
		{"update-error.json", "update.json"},
		{"error.json", "error.json"},
		{"sync-plan.json", "sync-plan.json"},
		{"manage-plan.json", "manage-plan.json"},
		{"doctor.json", "doctor.json"},
		{"skill-check.json", "skill-check.json"},
		{"skill-install.json", "skill-install.json"},
		{"provider-clear.json", "provider-clear.json"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			mustValidate(t, tc.schema, decodeFile(t, filepath.Join("testdata", "contract", tc.file)))
		})
	}
}

func TestSchemaRejectsRemovedOrRetypedFields(t *testing.T) {
	sch := loadSchemaDoc(t, "skills-list.json")
	broken := decodeFile(t, filepath.Join("testdata", "contract", "skills-list.json"))
	obj := broken.([]any)[0].(map[string]any)
	delete(obj, "key")
	if err := validateSchema(sch, broken); err == nil {
		t.Fatal("missing key was accepted")
	}

	retyped := decodeFile(t, filepath.Join("testdata", "contract", "skills-list.json"))
	retyped.([]any)[0].(map[string]any)["mismatch"] = "yes"
	if err := validateSchema(sch, retyped); err == nil {
		t.Fatal("string mismatch was accepted")
	}

	added := decodeFile(t, filepath.Join("testdata", "contract", "skills-list.json"))
	added.([]any)[0].(map[string]any)["note"] = "additive"
	if err := validateSchema(sch, added); err != nil {
		t.Fatal(err)
	}

	versionDoc := decodeFile(t, filepath.Join("testdata", "contract", "version.json")).(map[string]any)
	versionDoc["commit"] = "abc123"
	mustValidate(t, "version.json", versionDoc)

	noHint := map[string]any{"error": map[string]any{"code": "usage", "exit": float64(2), "message": "bad"}}
	mustValidate(t, "error.json", noHint)
}

func TestSchemaFileListsEveryCommand(t *testing.T) {
	root := loadSchemaDoc(t, "schema.json").(map[string]any)
	commands := root["properties"].(map[string]any)["commands"].(map[string]any)
	var required []string
	for _, item := range commands["required"].([]any) {
		required = append(required, item.(string))
	}
	var paths []string
	for _, spec := range commandSpecs() {
		paths = append(paths, spec.path)
	}
	sort.Strings(required)
	sort.Strings(paths)
	if !reflect.DeepEqual(required, paths) {
		t.Fatalf("schema required %v\nspecs %v", required, paths)
	}
}

func TestHelpExitsZero(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "synca version"},
		{[]string{"help"}, "synca schema"},
		{[]string{"--help"}, "synca version"},
		{[]string{"-h"}, "synca schema"},
		{[]string{"version", "--help"}, "synca version [--json]"},
		{[]string{"version", "-h"}, "USAGE:"},
		{[]string{"schema", "--help"}, "synca schema [--json]"},
		{[]string{"schema", "-h"}, "USAGE:"},
		{[]string{"skills", "--help"}, "synca skills list"},
		{[]string{"skills", "-h"}, "synca skills remove"},
		{[]string{"mcp", "--help"}, "synca mcp add"},
		{[]string{"mcp", "-h"}, "synca mcp remove"},
		{[]string{"sync", "--help"}, "synca sync"},
		{[]string{"sync", "-h"}, "--on-conflict"},
		{[]string{"provider", "--help"}, "provider clear"},
		{[]string{"provider", "-h"}, "--dry-run"},
		{[]string{"skills", "list", "--help"}, "synca skills list"},
		{[]string{"skills", "install", "--help"}, "synca skills install"},
		{[]string{"skills", "remove", "-h"}, "synca skills remove"},
		{[]string{"mcp", "list", "--help"}, "synca mcp list"},
		{[]string{"mcp", "add", "--help"}, "synca mcp add"},
		{[]string{"mcp", "remove", "--help"}, "synca mcp remove"},
		{[]string{"sync", "skills", "--help"}, "synca sync"},
		{[]string{"sync", "mcp", "--help"}, "--on-conflict"},
		{[]string{"sync", "all", "-h"}, "--json"},
		{[]string{"sync", "skills", "--help", "--bogus"}, "synca sync"},
		{[]string{"provider", "clear", "--help"}, "provider clear"},
		{[]string{"doctor", "--help"}, "synca doctor"},
		{[]string{"doctor", "-h"}, "Exit 0 when clean"},
		{[]string{"skill", "--help"}, "synca skill install"},
		{[]string{"skill", "install", "--help"}, "synca skill check"},
		{[]string{"skill", "check", "-h"}, "--dest"},
		{[]string{"update", "--help"}, "update"},
		{[]string{"update", "-h"}, "update"},
	}
	for _, tc := range cases {
		out, err := runCLI(t, tc.args...)
		var stderr bytes.Buffer
		if code := Finish(err, &stderr); code != 0 || stderr.Len() != 0 {
			t.Errorf("%v: code %d err %v stderr %q", tc.args, code, err, stderr.String())
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v: stdout missing %q\n%s", tc.args, tc.want, out)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("%v: empty usage", tc.args)
		}
	}
}

func TestVersionAndSchema(t *testing.T) {
	textVersion := "synca " + core.Version + "\n"
	for _, args := range [][]string{{"--version"}, {"-V"}, {"version"}, {"--version", "--json"}} {
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out != textVersion {
			t.Fatalf("%v: got %q", args, out)
		}
	}

	out, err := runCLI(t, "version", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "version.json", doc)
	if doc["name"] != "synca" || doc["version"] != core.Version || doc["schemaVersion"] != float64(schema.Version) {
		t.Fatalf("version doc %#v", doc)
	}
	switch commit := doc["commit"].(type) {
	case nil:
	case string:
		if commit == "" {
			t.Fatal("empty commit")
		}
	default:
		t.Fatalf("commit %T", doc["commit"])
	}
	if _, ok := doc["dirty"].(bool); !ok {
		t.Fatalf("dirty %T", doc["dirty"])
	}

	help, err := runCLI(t, "version", "--help", "--json")
	if err != nil || strings.HasPrefix(strings.TrimSpace(help), "{") {
		t.Fatalf("help err=%v out=%s", err, help)
	}

	out, err = runCLI(t, "schema")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	specs := commandSpecs()
	if len(lines) != len(specs) {
		t.Fatalf("lines %d\n%s", len(lines), out)
	}
	for i, spec := range specs {
		want := spec.path + "  " + spec.letter() + "  " + spec.summary
		if lines[i] != want {
			t.Errorf("line %d\n got %q\nwant %q", i, lines[i], want)
		}
		if spec.schemaFile == "" {
			t.Errorf("%s has no schema", spec.path)
		}
	}

	one, err := runCLI(t, "schema", "skills", "list")
	if err != nil {
		t.Fatal(err)
	}
	if one != lines[0]+"\n" {
		t.Fatalf("single text %q", one)
	}

	out, err = runCLI(t, "schema", "--json")
	if err != nil {
		t.Fatal(err)
	}
	catalog := decodeOne(t, out)
	mustValidate(t, "schema.json", catalog)
	cat := catalog.(map[string]any)
	if cat["name"] != "synca" || cat["schemaVersion"] != float64(1) {
		t.Fatalf("catalog header %#v", cat)
	}
	commands := cat["commands"].(map[string]any)
	if !reflect.DeepEqual(cat["error"], loadSchemaDoc(t, "error.json")) {
		t.Fatal("catalog error schema drifted from error.json")
	}
	commandSchema := loadSchemaDoc(t, "schema.json").(map[string]any)["$defs"].(map[string]any)["command"]
	for _, spec := range specs {
		entry, ok := commands[spec.path].(map[string]any)
		if !ok {
			t.Fatalf("missing %s", spec.path)
		}
		if err := validateSchema(commandSchema, entry); err != nil {
			t.Fatalf("%s: %v", spec.path, err)
		}
		if entry["summary"] != spec.summary || entry["mutates"] != spec.mutates || entry["destructive"] != spec.destructive {
			t.Fatalf("%s metadata %#v", spec.path, entry)
		}
		var flags []string
		for _, flag := range entry["flags"].([]any) {
			flags = append(flags, flag.(string))
		}
		if !reflect.DeepEqual(flags, spec.flags) {
			t.Fatalf("%s flags %v", spec.path, flags)
		}
		if spec.schemaFile == "" {
			if entry["output"] != nil {
				t.Fatalf("%s output = %#v", spec.path, entry["output"])
			}
			continue
		}
		if !reflect.DeepEqual(entry["output"], loadSchemaDoc(t, spec.schemaFile)) {
			t.Fatalf("%s output drifted from %s", spec.path, spec.schemaFile)
		}
	}

	entryOut, err := runCLI(t, "schema", "--json", "version")
	if err != nil {
		t.Fatal(err)
	}
	entry := decodeOne(t, entryOut).(map[string]any)
	if err := validateSchema(commandSchema, entry); err != nil {
		t.Fatal(err)
	}
	if _, isCatalog := entry["commands"]; isCatalog {
		t.Fatal("single entry included the catalog")
	}
	if entry["summary"] != commands["version"].(map[string]any)["summary"] {
		t.Fatal("version entry drifted")
	}

	badOut, badErr := runCLI(t, "schema", "no-such", "--json")
	assertUsageJSON(t, badOut, badErr)
	if !strings.Contains(badErr.Error(), "no-such") {
		t.Fatal(badErr)
	}
	textOut, textErr := runCLI(t, "schema", "no-such")
	assertUsageText(t, textOut, textErr, `unknown command "no-such"`)

	flagOut, flagErr := runCLI(t, "version", "--json", "--nope")
	assertUsageJSON(t, flagOut, flagErr)
	plainOut, plainErr := runCLI(t, "version", "--nope")
	assertUsageText(t, plainOut, plainErr, `unknown argument "--nope"`)

	schemaFlagOut, schemaFlagErr := runCLI(t, "schema", "--bogus")
	assertUsageText(t, schemaFlagOut, schemaFlagErr, `unknown flag "--bogus"`)
}

func assertUsageJSON(t *testing.T, out string, err error) {
	t.Helper()
	if ExitCode(err) != 2 {
		t.Fatalf("exit %d (%v)", ExitCode(err), err)
	}
	var stderr bytes.Buffer
	if code := Finish(err, &stderr); code != 2 || stderr.Len() != 0 {
		t.Fatalf("finish %d stderr %q", code, stderr.String())
	}
	doc := decodeOne(t, out)
	mustValidate(t, "error.json", doc)
	obj := doc.(map[string]any)["error"].(map[string]any)
	if obj["code"] != "usage" || obj["exit"] != float64(2) {
		t.Fatalf("%#v", obj)
	}
	if _, ok := obj["hint"].(string); !ok {
		t.Fatalf("hint %#v", obj["hint"])
	}
	if strings.Contains(out, "synca failed:") {
		t.Fatalf("json error leaked the text prefix\n%s", out)
	}
}

func assertUsageText(t *testing.T, out string, err error, message string) {
	t.Helper()
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout %q", out)
	}
	if ExitCode(err) != 2 {
		t.Fatalf("exit %d (%v)", ExitCode(err), err)
	}
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("exit type %T", err)
	}
	var stderr bytes.Buffer
	if Finish(err, &stderr) != 2 {
		t.Fatal(err)
	}
	want := "synca failed: " + message + "\n"
	if exit.Hint != "" {
		want += "hint: " + exit.Hint + "\n"
	}
	if stderr.String() != want {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestTypedExitsForCommandErrors(t *testing.T) {
	out, err := runCLI(t, "skills")
	assertUsageText(t, out, err, "usage: synca skills <list|install|remove>")

	out, err = runCLI(t, "nope")
	assertUsageText(t, out, err, "unknown command 'nope' (see: synca help)")

	out, err = runCLI(t, "skills", "list", "--scope", "nope", "--json")
	assertUsageJSON(t, out, err)
	if !strings.Contains(out, "invalid --scope") {
		t.Fatal(out)
	}
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Fatalf("error JSON is not one line:\n%s", out)
	}
}

func TestLiveListsMatchSchemas(t *testing.T) {
	home := contractHome(t)

	text, err := runCLI(t, "skills", "list")
	if err != nil {
		t.Fatal(err)
	}
	wantText := "Skills (user)\n  alpha  [agents,grok]\n  beta  [agents]\n2 skill(s)\n"
	if text != wantText {
		t.Fatalf("skills text:\n%q", text)
	}
	mcpText, err := runCLI(t, "mcp", "list")
	if err != nil {
		t.Fatal(err)
	}
	wantMCP := "MCPs (user)\n  demo  [agents]\n  remote  [agents]\n2 mcp server(s)\n"
	if mcpText != wantMCP {
		t.Fatalf("mcp text:\n%q", mcpText)
	}

	skillsOut, err := runCLI(t, "skills", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	skills := decodeOne(t, skillsOut)
	mustValidate(t, "skills-list.json", skills)
	alpha := entryByKey(t, skills, "alpha")
	if alpha["description"] != "Alpha skill" || alpha["scope"] != "user" || alpha["mismatch"] != false {
		t.Fatalf("alpha %#v", alpha)
	}
	presence := alpha["presence"].([]any)
	if len(presence) != 2 {
		t.Fatalf("alpha presence %d", len(presence))
	}
	var sawLink bool
	for _, item := range presence {
		row := item.(map[string]any)
		if row["agent"] == "grok" {
			sawLink = row["is_symlink"] == true && row["symlink_target"] != nil
			if !strings.HasPrefix(row["path"].(string), home) {
				t.Fatalf("path %v", row["path"])
			}
		}
	}
	if !sawLink {
		t.Fatal("grok symlink presence missing")
	}
	beta := entryByKey(t, skills, "beta")
	if beta["description"] != nil {
		t.Fatalf("beta description %#v", beta["description"])
	}

	mcpOut, err := runCLI(t, "mcp", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mcps := decodeOne(t, mcpOut)
	mustValidate(t, "mcp-list.json", mcps)
	demo := entryByKey(t, mcps, "demo")
	norm := demo["presence"].([]any)[0].(map[string]any)["normalized"].(map[string]any)
	if norm["transport"] != "stdio" || norm["url"] != nil {
		t.Fatalf("demo %#v", norm)
	}
	if _, ok := norm["env"].(map[string]any); !ok {
		t.Fatalf("demo env %#v", norm["env"])
	}
	remote := entryByKey(t, mcps, "remote")
	remoteNorm := remote["presence"].([]any)[0].(map[string]any)["normalized"].(map[string]any)
	if remoteNorm["command"] != nil || remoteNorm["url"] != "https://example.com/mcp" {
		t.Fatalf("remote %#v", remoteNorm)
	}
}

func TestEmptyListsMatchSchemas(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, args := range [][]string{
		{"skills", "list", "--json"},
		{"mcp", "list", "--json"},
	} {
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		doc := decodeOne(t, out)
		if _, ok := doc.([]any); !ok {
			t.Fatalf("%v: %#v", args, doc)
		}
		name := "skills-list.json"
		if args[0] == "mcp" {
			name = "mcp-list.json"
		}
		mustValidate(t, name, doc)
	}
}

func contractHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\n# Alpha\n")
	writeFile(t, filepath.Join(home, ".agents", "skills", "beta", "SKILL.md"), "---\nname: beta\n---\n# Beta\n")
	if err := os.MkdirAll(filepath.Join(home, ".grok", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(home, ".agents", "skills", "alpha"),
		filepath.Join(home, ".grok", "skills", "alpha"),
	); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".agents", "mcp.json"), `{
  "mcpServers": {
    "demo": {"type": "stdio", "command": "echo", "args": ["hi"], "env": {"TOKEN": "example"}},
    "remote": {"type": "http", "url": "https://example.com/mcp"}
  }
}
`)
	return home
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryByKey(t *testing.T, doc any, key string) map[string]any {
	t.Helper()
	arr, ok := doc.([]any)
	if !ok {
		t.Fatalf("%T", doc)
	}
	for _, item := range arr {
		obj := item.(map[string]any)
		if obj["key"] == key {
			return obj
		}
	}
	t.Fatalf("missing %s", key)
	return nil
}
