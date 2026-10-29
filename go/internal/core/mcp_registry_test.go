package core

import (
	"strings"
	"testing"

	"github.com/ngosangns/synca/go/internal/mcpregistry"
)

func registryServer(packages []mcpregistry.Package, remotes []mcpregistry.Remote) *mcpregistry.Server {
	return &mcpregistry.Server{
		Name:     "io.test/server",
		Version:  "1.2.3",
		Packages: packages,
		Remotes:  remotes,
	}
}

func npmPackage() mcpregistry.Package {
	return mcpregistry.Package{
		RegistryType:    "npm",
		RegistryBaseURL: "https://registry.npmjs.org",
		Identifier:      "@x/y",
		Version:         "1.0.0",
		Transport:       mcpregistry.PackageTransport{Type: "stdio"},
	}
}

func mustRegistry(t *testing.T, server *mcpregistry.Server, opts McpRegistryOptions) (string, *McpNormalized, []string) {
	t.Helper()
	key, norm, notes, err := McpFromRegistry(server, opts)
	if err != nil {
		t.Fatalf("McpFromRegistry: %v", err)
	}
	return key, norm, notes
}

func argvOf(norm *McpNormalized) []string {
	return append(append([]string{}, norm.Command...), norm.Args...)
}

func TestRegistryPackageCommands(t *testing.T) {
	cases := []struct {
		name string
		pkg  mcpregistry.Package
		argv []string
	}{
		{
			name: "npm",
			pkg:  npmPackage(),
			argv: []string{"npx", "-y", "@x/y@1.0.0"},
		},
		{
			name: "pypi",
			pkg: mcpregistry.Package{
				RegistryType: "pypi", RegistryBaseURL: "https://pypi.org",
				Identifier: "acme-server", Version: "2.0.0",
				Transport: mcpregistry.PackageTransport{Type: "stdio"},
			},
			argv: []string{"uvx", "acme-server==2.0.0"},
		},
		{
			name: "oci",
			pkg: mcpregistry.Package{
				RegistryType: "oci", RegistryBaseURL: "https://docker.io",
				Identifier: "ghcr.io/acme/server", Version: "3.1.0",
				Transport: mcpregistry.PackageTransport{Type: "stdio"},
			},
			argv: []string{"docker", "run", "-i", "--rm", "ghcr.io/acme/server:3.1.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, norm, notes := mustRegistry(t, registryServer([]mcpregistry.Package{tc.pkg}, nil), McpRegistryOptions{})
			if norm.Transport != "stdio" {
				t.Fatalf("transport %q", norm.Transport)
			}
			if got := argvOf(norm); strings.Join(got, " ") != strings.Join(tc.argv, " ") {
				t.Fatalf("argv %v want %v", got, tc.argv)
			}
			if len(notes) == 0 || !strings.Contains(notes[0], "-> "+tc.name) {
				t.Errorf("notes %v", notes)
			}
		})
	}
}

func TestRegistryRuntimeAndPackageArgumentsKeepOrder(t *testing.T) {
	pkg := npmPackage()
	pkg.RuntimeArguments = []mcpregistry.Argument{
		{Type: "named", Name: "--registry", Value: "https://registry.example"},
	}
	pkg.PackageArguments = []mcpregistry.Argument{
		{Type: "named", Name: "--root", Default: "/tmp"},
	}
	_, norm, _ := mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{})
	want := []string{"npx", "-y", "--registry=https://registry.example", "@x/y@1.0.0", "--root=/tmp"}
	if got := argvOf(norm); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %v want %v", got, want)
	}
}

func TestRegistryVersionFallsBackToServerVersion(t *testing.T) {
	pkg := npmPackage()
	pkg.Version = ""
	_, norm, _ := mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{})
	if norm.Args[1] != "@x/y@1.2.3" {
		t.Fatalf("args %v", norm.Args)
	}

	pkg.Version = ""
	srv := registryServer([]mcpregistry.Package{pkg}, nil)
	srv.Version = ""
	if _, _, _, err := McpFromRegistry(srv, McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("empty version err %v", err)
	}
}

func TestRegistrySelectionOrder(t *testing.T) {
	npm := npmPackage()
	pypi := mcpregistry.Package{RegistryType: "pypi", Identifier: "acme", Version: "1.0.0", Transport: mcpregistry.PackageTransport{Type: "stdio"}}
	http := mcpregistry.Remote{Type: "streamable-http", URL: "https://api.example/mcp"}
	sse := mcpregistry.Remote{Type: "sse", URL: "https://api.example/sse"}

	srv := registryServer([]mcpregistry.Package{pypi, npm}, []mcpregistry.Remote{http, sse})
	_, norm, _ := mustRegistry(t, srv, McpRegistryOptions{})
	if norm.Command[0] != "npx" {
		t.Errorf("default should pick npm, got %v", norm.Command)
	}

	_, norm, _ = mustRegistry(t, srv, McpRegistryOptions{PackageType: "pypi"})
	if norm.Command[0] != "uvx" {
		t.Errorf("--package pypi picked %v", norm.Command)
	}

	_, norm, _ = mustRegistry(t, srv, McpRegistryOptions{Transport: "http"})
	if norm.Transport != "http" || *norm.URL != http.URL {
		t.Errorf("--transport http picked %+v", norm)
	}

	_, norm, _ = mustRegistry(t, srv, McpRegistryOptions{Transport: "sse"})
	if norm.Transport != "sse" || *norm.URL != sse.URL {
		t.Errorf("--transport sse picked %+v", norm)
	}

	_, norm, _ = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{http}), McpRegistryOptions{})
	if norm.Transport != "http" {
		t.Errorf("remote fallback %+v", norm)
	}

	if _, _, _, err := McpFromRegistry(srv, McpRegistryOptions{PackageType: "npm", Transport: "http"}); err == nil {
		t.Fatal("--package with --transport http was accepted")
	}
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{sse}), McpRegistryOptions{Transport: "http"}); err == nil {
		t.Fatal("http transport matched an sse remote")
	}
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{http}), McpRegistryOptions{Transport: "stdio"}); err == nil {
		t.Fatal("stdio transport matched a remote")
	}
	if _, _, _, err := McpFromRegistry(registryServer(nil, nil), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "packages: none") {
		t.Fatalf("no variants err %v", err)
	}
	if _, _, _, err := McpFromRegistry(srv, McpRegistryOptions{PackageType: "cargo"}); err == nil || !strings.Contains(err.Error(), "npm|pypi|oci") {
		t.Fatalf("unsupported package err %v", err)
	}
}

func TestRegistryPackageRejectsUnsafeMetadata(t *testing.T) {
	cases := map[string]func(*mcpregistry.Package){
		"empty identifier": func(p *mcpregistry.Package) { p.Identifier = "" },
		"leading dash":     func(p *mcpregistry.Package) { p.Identifier = "-x" },
		"whitespace":       func(p *mcpregistry.Package) { p.Identifier = "a b" },
		"foreign registry": func(p *mcpregistry.Package) { p.RegistryBaseURL = "https://evil.example" },
		"remote transport": func(p *mcpregistry.Package) { p.Transport.Type = "streamable-http" },
	}
	for name, mutate := range cases {
		pkg := npmPackage()
		mutate(&pkg)
		if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	oci := mcpregistry.Package{
		RegistryType: "oci", RegistryBaseURL: "https://ghcr.io",
		Identifier: "ghcr.io/acme/server", Version: "1.0.0",
		Transport: mcpregistry.PackageTransport{Type: "stdio"},
	}
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{oci}, nil), McpRegistryOptions{}); err != nil {
		t.Errorf("oci custom registry rejected: %v", err)
	}
}

func TestRegistryRuntimeHint(t *testing.T) {
	pkg := npmPackage()
	pkg.RuntimeHint = "bunx"
	_, norm, notes := mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{})
	if norm.Command[0] != "bunx" || len(notes) != 1 {
		t.Fatalf("custom hint %v notes %v", norm.Command, notes)
	}

	pkg.RuntimeHint = "bad hint"
	_, norm, notes = mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{})
	if norm.Command[0] != "npx" {
		t.Fatalf("invalid hint used %v", norm.Command)
	}
	if len(notes) != 2 || !strings.Contains(notes[1], "runtimeHint") {
		t.Fatalf("notes %v", notes)
	}
}

func TestRegistryEnvPrecedenceAndValidation(t *testing.T) {
	pkg := npmPackage()
	pkg.EnvironmentVariables = []mcpregistry.EnvVar{
		{Name: "API_KEY", IsRequired: true, Default: "default-key"},
		{Name: "MODE", Value: "literal", Default: "fallback"},
		{Name: "EMPTY_OK", Default: "d"},
	}
	_, norm, _ := mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{
		Env: map[string]string{"API_KEY": "real-key"},
	})
	if norm.Env["API_KEY"] != "real-key" || norm.Env["MODE"] != "literal" || norm.Env["EMPTY_OK"] != "d" {
		t.Fatalf("env %v", norm.Env)
	}
	if strings.Join(norm.EnvKeys, ",") != "API_KEY,EMPTY_OK,MODE" {
		t.Fatalf("keys %v", norm.EnvKeys)
	}

	pkg.EnvironmentVariables = append(pkg.EnvironmentVariables, mcpregistry.EnvVar{Name: "OTHER", IsRequired: true})
	_, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{
		Env: map[string]string{"API_KEY": "secret-value"},
	})
	if err == nil || !strings.Contains(err.Error(), "OTHER") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("required env err %v", err)
	}

	pkg.EnvironmentVariables = []mcpregistry.EnvVar{
		{Name: "LOG", Default: "debug", Choices: []string{"info", "warn"}},
	}
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "info, warn") {
		t.Fatalf("choices err %v", err)
	}

	pkg.EnvironmentVariables = []mcpregistry.EnvVar{{Name: "1BAD"}}
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil {
		t.Fatal("invalid env name accepted")
	}

	pkg.EnvironmentVariables = []mcpregistry.EnvVar{{Name: "URL", Default: "{BASE}/v1"}}
	_, norm, _ = mustRegistry(t, registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{
		Env: map[string]string{"BASE": "https://api.example"},
	})
	if norm.Env["URL"] != "https://api.example/v1" {
		t.Fatalf("substitution %v", norm.Env)
	}
}

func TestRegistryArguments(t *testing.T) {
	build := func(args []mcpregistry.Argument) mcpregistry.Package {
		pkg := npmPackage()
		pkg.PackageArguments = args
		return pkg
	}

	_, norm, _ := mustRegistry(t, registryServer([]mcpregistry.Package{build([]mcpregistry.Argument{
		{Type: "named", Name: "--port", Value: "8080"},
		{Type: "named", Name: "--flag", Value: "x"},
		{Type: "positional", ValueHint: "path", Value: "/tmp"},
	})}, nil), McpRegistryOptions{Args: map[string]string{"port": "9090", "flag": "", "path": "/data"}})
	want := []string{"npx", "-y", "@x/y@1.0.0", "--port=9090", "--flag", "/data"}
	if got := argvOf(norm); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv %v want %v", got, want)
	}

	_, norm, _ = mustRegistry(t, registryServer([]mcpregistry.Package{build([]mcpregistry.Argument{
		{Type: "positional", Value: "one"},
		{Type: "positional", Value: "two"},
	})}, nil), McpRegistryOptions{Args: map[string]string{"arg2": "second"}})
	want = []string{"npx", "-y", "@x/y@1.0.0", "one", "second"}
	if got := argvOf(norm); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("positional argv %v want %v", got, want)
	}

	pkg := build([]mcpregistry.Argument{{Type: "positional", ValueHint: "path", IsRequired: true}})
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("missing arg err %v", err)
	}

	pkg = build([]mcpregistry.Argument{{Type: "named", Name: "--url", Default: "{MISSING}/x"}})
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("unresolved arg err %v", err)
	}

	pkg = build(nil)
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{Args: map[string]string{"bogus": "1"}}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unused arg err %v", err)
	}

	pkg = build([]mcpregistry.Argument{{Type: "weird", Name: "x", Value: "1"}})
	if _, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{}); err == nil {
		t.Fatal("unknown argument type accepted")
	}
}

func TestRegistryRemoteMapping(t *testing.T) {
	_, norm, _ := mustRegistry(t, registryServer(nil, []mcpregistry.Remote{
		{Type: "streamable-http", URL: "https://api.example/mcp"},
	}), McpRegistryOptions{})
	if norm.Transport != "http" || norm.URL == nil || *norm.URL != "https://api.example/mcp" {
		t.Fatalf("remote %+v", norm)
	}
	if len(norm.EnvKeys) != 0 || len(norm.Env) != 0 {
		t.Fatalf("remote env %+v", norm.Env)
	}

	_, norm, notes := mustRegistry(t, registryServer(nil, []mcpregistry.Remote{
		{Type: "sse", URL: "https://api.example/sse"},
	}), McpRegistryOptions{})
	if norm.Transport != "sse" {
		t.Fatalf("sse transport %q", norm.Transport)
	}
	_ = notes

	_, norm, notes = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{
		{Type: "sse", URL: "https://api.example/mcp"},
	}), McpRegistryOptions{})
	if norm.Transport != "http" {
		t.Fatalf("sse fallback transport %q", norm.Transport)
	}
	if !notesContain(notes, "stored as http") {
		t.Fatalf("no canonicalization note: %v", notes)
	}

	for _, raw := range []string{"", "example.com/mcp", "ftp://x/mcp"} {
		if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{
			{Type: "streamable-http", URL: raw},
		}), McpRegistryOptions{}); err == nil {
			t.Errorf("url %q accepted", raw)
		}
	}
}

func TestRegistryRemoteHeadersAndVariables(t *testing.T) {
	remote := mcpregistry.Remote{
		Type: "streamable-http",
		URL:  "{baseUrl}/mcp",
		Headers: []mcpregistry.Header{
			{Name: "Authorization", IsRequired: true, Value: "Bearer top-secret"},
		},
		Variables: map[string]mcpregistry.Input{
			"baseUrl": {IsRequired: true},
		},
	}
	env := map[string]string{"baseUrl": "https://api.example"}

	// A declared header value is stored, and its value never reaches the notes.
	_, norm, notes := mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{Env: env})
	if norm.Transport != "http" || *norm.URL != "https://api.example/mcp" {
		t.Fatalf("remote %+v", norm)
	}
	if norm.Headers["Authorization"] != "Bearer top-secret" || len(norm.HeaderKeys) != 1 || norm.HeaderKeys[0] != "Authorization" {
		t.Fatalf("headers %v %+v", norm.HeaderKeys, norm.Headers)
	}
	if !notesContain(notes, "Authorization") || notesContain(notes, "top-secret") {
		t.Fatalf("header note %v", notes)
	}

	// A required header without a value needs --header.
	remote.Headers = []mcpregistry.Header{{Name: "Authorization", IsRequired: true}}
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{Env: env}); err == nil ||
		!strings.Contains(err.Error(), "Authorization") || !strings.Contains(err.Error(), "--header") {
		t.Fatalf("required header err %v", err)
	}
	_, norm, notes = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{
		Env:     env,
		Headers: map[string]string{"Authorization": "Bearer from-flag"},
	})
	if norm.Headers["Authorization"] != "Bearer from-flag" || notesContain(notes, "from-flag") {
		t.Fatalf("header override %+v notes %v", norm.Headers, notes)
	}

	// An optional header with a value is stored; one without is skipped.
	remote.Headers = []mcpregistry.Header{{Name: "X-Trace", Value: "internal"}}
	_, norm, notes = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{Env: env})
	if norm.Headers["X-Trace"] != "internal" || notesContain(notes, "internal") || notesContain(notes, "skipped") {
		t.Fatalf("optional header %+v notes %v", norm.Headers, notes)
	}
	remote.Headers = []mcpregistry.Header{{Name: "X-Trace"}}
	_, norm, notes = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{Env: env})
	if len(norm.Headers) != 0 || !notesContain(notes, "skipped optional header(s): X-Trace") {
		t.Fatalf("optional header skip %+v notes %v", norm.Headers, notes)
	}

	remote.Headers = nil
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "baseUrl") {
		t.Fatalf("required variable err %v", err)
	}

	_, norm, _ = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{
		Args: map[string]string{"baseUrl": "https://api.arg"},
	})
	if *norm.URL != "https://api.arg/mcp" {
		t.Fatalf("arg variable %v", *norm.URL)
	}

	remote.Variables = map[string]mcpregistry.Input{"baseUrl": {Value: "https://api.literal"}}
	_, norm, _ = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{})
	if *norm.URL != "https://api.literal/mcp" {
		t.Fatalf("literal variable %v", *norm.URL)
	}

	remote.Variables = map[string]mcpregistry.Input{"baseUrl": {Default: "https://api.default"}}
	_, norm, _ = mustRegistry(t, registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{})
	if *norm.URL != "https://api.default/mcp" {
		t.Fatalf("default variable %v", *norm.URL)
	}

	remote.Variables = map[string]mcpregistry.Input{"baseUrl": {Choices: []string{"https://a", "https://b"}, Default: "https://c"}}
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "https://a") {
		t.Fatalf("variable choices err %v", err)
	}

	remote.Variables = nil
	remote.URL = "{baseUrl}/mcp"
	if _, _, _, err := McpFromRegistry(registryServer(nil, []mcpregistry.Remote{remote}), McpRegistryOptions{}); err == nil || !strings.Contains(err.Error(), "baseUrl") {
		t.Fatalf("unresolved url err %v", err)
	}
}

func TestRegistryRemoteSSENoteMasksPassword(t *testing.T) {
	_, norm, notes := mustRegistry(t, registryServer(nil, []mcpregistry.Remote{
		{Type: "sse", URL: "https://user:hunter2@api.example/mcp"},
	}), McpRegistryOptions{})
	if norm.Transport != "http" {
		t.Fatalf("transport %q", norm.Transport)
	}
	joined := strings.Join(notes, "\n")
	if strings.Contains(joined, "hunter2") || !strings.Contains(joined, "user:") {
		t.Fatalf("notes leaked a password: %v", notes)
	}
}

func TestRegistryKeyDerivation(t *testing.T) {
	if key, err := DefaultMcpRegistryKey("io.test/My_Server"); err != nil || key != "my-server" {
		t.Fatalf("key %q err %v", key, err)
	}
	if _, err := DefaultMcpRegistryKey("/"); err == nil {
		t.Fatal("empty segment was accepted")
	}
	key, _, _ := mustRegistry(t, registryServer([]mcpregistry.Package{npmPackage()}, nil), McpRegistryOptions{})
	if key != "server" {
		t.Fatalf("default key %q", key)
	}
	key, _, _ = mustRegistry(t, registryServer([]mcpregistry.Package{npmPackage()}, nil), McpRegistryOptions{Key: "Custom Key"})
	if key != "custom-key" {
		t.Fatalf("override key %q", key)
	}
}

func TestRegistryNeverPrintsSecrets(t *testing.T) {
	pkg := npmPackage()
	pkg.EnvironmentVariables = []mcpregistry.EnvVar{{Name: "TOKEN", IsRequired: true}}
	_, _, _, err := McpFromRegistry(registryServer([]mcpregistry.Package{pkg}, nil), McpRegistryOptions{
		Env: map[string]string{"OTHER": "super-secret"},
	})
	if err != nil && strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked an env value: %v", err)
	}
}

func notesContain(notes []string, want string) bool {
	for _, n := range notes {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}
