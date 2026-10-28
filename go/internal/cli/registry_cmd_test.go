package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func registryServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/download/"):
			_, _ = w.Write([]byte(`{"files":[{"path":"SKILL.md","contents":"hello"},{"path":"a/b.md","contents":"xy"}],"hash":"h"}`))
		case r.URL.Path == "/api/search":
			_, _ = w.Write([]byte(`{"query":"pdf","searchType":"fuzzy","skills":[{"source":"a/b","skillId":"pdf","name":"pdf","installs":3}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SKILLS_API_URL", srv.URL)
	t.Setenv("SKILLS_DOWNLOAD_URL", srv.URL)
	t.Setenv("SKILLS_API_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
}

func TestSkillsInfoContentsFlagKeepsJSONOutput(t *testing.T) {
	registryServer(t)
	for _, args := range [][]string{
		{"skills", "info", "a/b/c", "--contents", "--json"},
		{"skills", "info", "a/b/c", "--json", "--contents"},
	} {
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		doc := decodeOne(t, out).(map[string]any)
		mustValidate(t, "registry-info.json", doc)
		files := doc["files"].([]any)
		if len(files) != 2 || files[0].(map[string]any)["contents"] != "hello" {
			t.Errorf("%v: files %v", args, files)
		}
	}
	out, err := runCLI(t, "skills", "info", "a/b/c", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range decodeOne(t, out).(map[string]any)["files"].([]any) {
		if _, has := f.(map[string]any)["contents"]; has {
			t.Error("contents present without --contents")
		}
	}
}

func TestSkillsSearchJSONMatchesSchema(t *testing.T) {
	registryServer(t)
	out, err := runCLI(t, "skills", "search", "pdf", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "registry-search.json", decodeOne(t, out))
}

func TestOfficialWithoutTokenIsExit7(t *testing.T) {
	registryServer(t)
	out, err := runCLI(t, "skills", "official", "--json")
	if ExitCode(err) != ExitUnauthorized {
		t.Fatalf("exit %d err %v", ExitCode(err), err)
	}
	assertErrorCode(t, out, "unauthorized")
}

func assertErrorCode(t *testing.T, out, code string) {
	t.Helper()
	doc := decodeOne(t, out).(map[string]any)
	mustValidate(t, "error.json", doc)
	if got := doc["error"].(map[string]any)["code"]; got != code {
		t.Errorf("code %v, want %s", got, code)
	}
}

func TestRegistryUsageErrorsAreExit2(t *testing.T) {
	registryServer(t)
	for _, args := range [][]string{
		{"skills", "search", "a", "--json"},
		{"skills", "search", "--json"},
		{"skills", "browse", "--view", "weekly", "--json"},
		{"skills", "info", "../x", "--json"},
		{"skills", "search", "ab", "--nope=1", "--json"},
	} {
		out, err := runCLI(t, args...)
		if ExitCode(err) != ExitUsage {
			t.Errorf("%v: exit %d", args, ExitCode(err))
		}
		assertErrorCode(t, out, "usage")
	}
}

func TestProvidersListJSONMatchesSchema(t *testing.T) {
	home := fakeHome(t)
	if err := os.MkdirAll(home+"/.claude/skills", 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "providers", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeOne(t, out)
	mustValidate(t, "providers-list.json", doc)
	if n := len(doc.(map[string]any)["providers"].([]any)); n < 70 {
		t.Errorf("only %d providers", n)
	}
	out, err = runCLI(t, "providers", "list", "--scope", "project", "--json", "--cwd", home)
	if err != nil {
		t.Fatal(err)
	}
	mustValidate(t, "providers-list.json", decodeOne(t, out))
	badOut, badErr := runCLI(t, "providers", "nope", "--json")
	if ExitCode(badErr) != ExitUsage {
		t.Errorf("exit %d", ExitCode(badErr))
	}
	assertErrorCode(t, badOut, "usage")
}
