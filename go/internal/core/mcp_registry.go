package core

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ngosangns/synca/go/internal/mcpregistry"
)

// McpRegistryOptions turns registry metadata into a server config.
type McpRegistryOptions struct {
	Key         string
	PackageType string
	Transport   string
	Env         map[string]string
	Headers     map[string]string
	Args        map[string]string
	Enabled     *bool
}

var (
	registryEnvNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	registryArgKeyRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	registryHintRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	registryVarToken  = regexp.MustCompile(`\{([A-Za-z0-9_.-]+)\}`)

	registryPackageBases = map[string]string{
		"npm":  "https://registry.npmjs.org",
		"pypi": "https://pypi.org",
		"oci":  "https://docker.io",
	}
)

// DefaultMcpRegistryKey derives the installed key from the last segment of a
// reverse-DNS registry name.
func DefaultMcpRegistryKey(name string) (string, error) {
	name = strings.TrimSpace(name)
	seg := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		seg = name[i+1:]
	}
	seg = NormalizeKey(seg)
	if seg == "" {
		return "", fmt.Errorf("cannot derive a key from %q; pass --name", name)
	}
	return seg, nil
}

// SelectedMcpRegistryVariant reports the variant label McpFromRegistry would
// pick for these options, so a caller can record it without re-normalizing.
func SelectedMcpRegistryVariant(server *mcpregistry.Server, packageType, transport string) (string, error) {
	variant, err := selectMcpRegistryVariant(server, packageType, transport)
	if err != nil {
		return "", err
	}
	return variant.label, nil
}

// McpFromRegistry maps one registry server onto synca's normalized MCP model.
// Values from --env and --arg override registry literals; secrets never appear
// in the returned notes.
func McpFromRegistry(server *mcpregistry.Server, opts McpRegistryOptions) (string, *McpNormalized, []string, error) {
	if server == nil {
		return "", nil, nil, fmt.Errorf("missing registry server")
	}
	key := strings.TrimSpace(opts.Key)
	if key == "" {
		derived, err := DefaultMcpRegistryKey(server.Name)
		if err != nil {
			return "", nil, nil, err
		}
		key = derived
	} else {
		key = NormalizeKey(key)
	}
	if key == "" {
		return "", nil, nil, fmt.Errorf("invalid --name")
	}
	variant, err := selectMcpRegistryVariant(server, opts.PackageType, opts.Transport)
	if err != nil {
		return "", nil, nil, err
	}
	if variant.pkg != nil && len(opts.Headers) > 0 {
		return "", nil, nil, fmt.Errorf("--header needs a remote registry server")
	}
	resolver, err := newRegistryResolver(opts)
	if err != nil {
		return "", nil, nil, err
	}
	var norm *McpNormalized
	var notes []string
	if variant.pkg != nil {
		norm, notes, err = normalizeRegistryPackage(server, variant.pkg, opts, resolver)
	} else {
		norm, notes, err = normalizeRegistryRemote(server, variant.remote, opts, resolver)
	}
	if err != nil {
		return "", nil, nil, err
	}
	if err := resolver.checkUnusedArgs(); err != nil {
		return "", nil, nil, err
	}
	version := strings.TrimSpace(server.Version)
	if version == "" {
		version = "unknown"
	}
	notes = append([]string{fmt.Sprintf("registry %s@%s -> %s", server.Name, version, variant.label)}, notes...)
	return key, norm, notes, nil
}

type mcpRegistryVariant struct {
	pkg    *mcpregistry.Package
	remote *mcpregistry.Remote
	label  string
}

func selectMcpRegistryVariant(server *mcpregistry.Server, packageType, transport string) (*mcpRegistryVariant, error) {
	pkgType := strings.ToLower(strings.TrimSpace(packageType))
	tr := strings.ToLower(strings.TrimSpace(transport))
	switch tr {
	case "", "stdio", "local", "http", "remote", "url", "sse", "streamable-http", "streamable_http":
	default:
		return nil, fmt.Errorf("unsupported --transport %q (use stdio|http|sse)", transport)
	}
	if pkgType != "" {
		switch pkgType {
		case "npm", "pypi", "oci":
		default:
			return nil, fmt.Errorf("unsupported --package %q (use npm|pypi|oci)", packageType)
		}
		if tr != "" && tr != "stdio" && tr != "local" {
			return nil, fmt.Errorf("--package cannot be combined with --transport %s", tr)
		}
		if pkg := firstRegistryPackage(server, pkgType); pkg != nil {
			return &mcpRegistryVariant{pkg: pkg, label: pkgType}, nil
		}
		return nil, noRegistryVariantErr(server, "no "+pkgType+" package")
	}
	if tr == "" || tr == "stdio" || tr == "local" {
		for _, t := range []string{"npm", "pypi", "oci"} {
			if pkg := firstRegistryPackage(server, t); pkg != nil {
				return &mcpRegistryVariant{pkg: pkg, label: t}, nil
			}
		}
		if tr != "" {
			return nil, noRegistryVariantErr(server, "no npm, pypi or oci package")
		}
	}
	remoteType := "streamable-http"
	if tr == "sse" {
		remoteType = "sse"
	}
	if remote := firstRegistryRemote(server, remoteType); remote != nil {
		return &mcpRegistryVariant{remote: remote, label: remoteType}, nil
	}
	if tr == "" {
		if remote := firstRegistryRemote(server, "sse"); remote != nil {
			return &mcpRegistryVariant{remote: remote, label: "sse"}, nil
		}
		return nil, noRegistryVariantErr(server, "no streamable-http or sse remote")
	}
	return nil, noRegistryVariantErr(server, "no "+remoteType+" remote")
}

func firstRegistryPackage(server *mcpregistry.Server, typ string) *mcpregistry.Package {
	for i := range server.Packages {
		if strings.EqualFold(strings.TrimSpace(server.Packages[i].RegistryType), typ) {
			return &server.Packages[i]
		}
	}
	return nil
}

func firstRegistryRemote(server *mcpregistry.Server, typ string) *mcpregistry.Remote {
	for i := range server.Remotes {
		if strings.EqualFold(strings.TrimSpace(server.Remotes[i].Type), typ) {
			return &server.Remotes[i]
		}
	}
	return nil
}

func noRegistryVariantErr(server *mcpregistry.Server, what string) error {
	var packages, remotes []string
	seen := map[string]bool{}
	for _, p := range server.Packages {
		t := strings.ToLower(strings.TrimSpace(p.RegistryType))
		if t != "" && !seen[t] {
			seen[t] = true
			packages = append(packages, t)
		}
	}
	seen = map[string]bool{}
	for _, r := range server.Remotes {
		t := strings.ToLower(strings.TrimSpace(r.Type))
		if t != "" && !seen[t] {
			seen[t] = true
			remotes = append(remotes, t)
		}
	}
	sort.Strings(packages)
	sort.Strings(remotes)
	return fmt.Errorf("registry server %q has %s; packages: %s; remotes: %s",
		server.Name, what, joinOrNone(packages), joinOrNone(remotes))
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func normalizeRegistryPackage(server *mcpregistry.Server, pkg *mcpregistry.Package, opts McpRegistryOptions, r *registryResolver) (*McpNormalized, []string, error) {
	typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
	switch typ {
	case "npm", "pypi", "oci":
	default:
		return nil, nil, fmt.Errorf("unsupported package type %q (use npm|pypi|oci)", pkg.RegistryType)
	}
	switch t := strings.ToLower(strings.TrimSpace(pkg.Transport.Type)); t {
	case "", "stdio":
	default:
		return nil, nil, fmt.Errorf("%s package uses transport %q; synca only installs stdio packages", typ, pkg.Transport.Type)
	}
	identifier := strings.TrimSpace(pkg.Identifier)
	if err := validateRegistryIdentifier(identifier); err != nil {
		return nil, nil, err
	}
	version := strings.TrimSpace(pkg.Version)
	if version == "" {
		version = strings.TrimSpace(server.Version)
	}
	if err := mcpregistry.ValidateVersion(version); err != nil {
		return nil, nil, err
	}
	if err := validateRegistryBaseURL(typ, pkg.RegistryBaseURL); err != nil {
		return nil, nil, err
	}
	env, err := r.resolveEnv(pkg.EnvironmentVariables)
	if err != nil {
		return nil, nil, err
	}
	runtimeArgs, err := r.resolveArguments(pkg.RuntimeArguments)
	if err != nil {
		return nil, nil, err
	}
	packageArgs, err := r.resolveArguments(pkg.PackageArguments)
	if err != nil {
		return nil, nil, err
	}
	argv, note := registryPackageArgv(typ, pkg.RuntimeHint, identifier, version, runtimeArgs, packageArgs)
	norm, err := McpFromParts("stdio", argv, nil, opts.Enabled)
	if err != nil {
		return nil, nil, err
	}
	norm.Env = env
	norm.EnvKeys = sortedKeys(env)
	if norm.EnvKeys == nil {
		norm.EnvKeys = []string{}
	}
	var notes []string
	if note != "" {
		notes = append(notes, note)
	}
	return norm, notes, nil
}

func validateRegistryIdentifier(identifier string) error {
	if identifier == "" {
		return fmt.Errorf("package identifier is empty")
	}
	if strings.HasPrefix(identifier, "-") {
		return fmt.Errorf("package identifier %q must not start with '-'", identifier)
	}
	for _, ch := range identifier {
		if unicode.IsSpace(ch) || unicode.IsControl(ch) {
			return fmt.Errorf("package identifier contains whitespace or a control character")
		}
	}
	return nil
}

func validateRegistryBaseURL(typ, raw string) error {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return nil
	}
	if typ == "oci" {
		// The image identifier carries its own registry host, so a well-formed
		// https base cannot redirect the pull.
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("package declares an invalid registryBaseUrl %q", raw)
		}
		return nil
	}
	if strings.EqualFold(raw, registryPackageBases[typ]) {
		return nil
	}
	return fmt.Errorf("package declares registryBaseUrl %q; synca installs %s packages from %s",
		raw, typ, registryPackageBases[typ])
}

func registryPackageArgv(typ, hint, identifier, version string, runtimeArgs, packageArgs []string) ([]string, string) {
	var argv []string
	var note string
	command := func(fallback string) string {
		if hint == "" {
			return fallback
		}
		if registryHintRe.MatchString(hint) {
			return hint
		}
		note = fmt.Sprintf("ignored invalid runtimeHint %q; using %s", hint, fallback)
		return fallback
	}
	switch typ {
	case "npm":
		cmd := command("npx")
		argv = []string{cmd}
		if cmd == "npx" {
			argv = append(argv, "-y")
		}
		argv = append(argv, runtimeArgs...)
		argv = append(argv, identifier+"@"+version)
		argv = append(argv, packageArgs...)
	case "pypi":
		argv = []string{command("uvx")}
		argv = append(argv, runtimeArgs...)
		argv = append(argv, identifier+"=="+version)
		argv = append(argv, packageArgs...)
	case "oci":
		argv = []string{command("docker"), "run", "-i", "--rm"}
		argv = append(argv, runtimeArgs...)
		argv = append(argv, identifier+":"+version)
		argv = append(argv, packageArgs...)
	}
	return argv, note
}

func normalizeRegistryRemote(server *mcpregistry.Server, remote *mcpregistry.Remote, opts McpRegistryOptions, r *registryResolver) (*McpNormalized, []string, error) {
	typ := strings.ToLower(strings.TrimSpace(remote.Type))
	switch typ {
	case "streamable-http", "sse":
	default:
		return nil, nil, fmt.Errorf("unsupported remote type %q", remote.Type)
	}
	var notes []string
	if _, err := r.resolveRemoteVariables(remote.Variables); err != nil {
		return nil, nil, err
	}
	var headers []string
	var headerValues = map[string]string{}
	var missing []string
	var skipped []string
	for _, h := range remote.Headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		value, ok := opts.Headers[name]
		if !ok {
			switch {
			case h.Value != "":
				value, ok = h.Value, true
			case h.Default != "":
				value, ok = h.Default, true
			}
		}
		if ok {
			resolved, unknown := substituteRegistryVars(value, r.vars)
			if len(unknown) > 0 {
				missing = append(missing, fmt.Sprintf("%s (needs %s)", name, strings.Join(unknown, ", ")))
				continue
			}
			headers = append(headers, name)
			headerValues[name] = resolved
			if h.IsSecret || secretName(name) {
				notes = append(notes, fmt.Sprintf("header %s=%s", name, SecretMask))
			}
			continue
		}
		if h.IsRequired {
			missing = append(missing, name)
			continue
		}
		skipped = append(skipped, name)
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("registry requires header(s) %s; pass --header NAME=VALUE", strings.Join(missing, ", "))
	}
	sort.Strings(headers)
	if len(skipped) > 0 {
		notes = append(notes, "skipped optional header(s): "+strings.Join(skipped, ", "))
	}
	rawURL := strings.TrimSpace(remote.URL)
	if rawURL == "" {
		return nil, nil, fmt.Errorf("remote %s has no URL", typ)
	}
	resolved, missing := substituteRegistryVars(rawURL, r.vars)
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("remote URL has unresolved variable(s): %s (pass --env NAME=VALUE)", strings.Join(missing, ", "))
	}
	if err := validateRegistryRemoteURL(resolved); err != nil {
		return nil, nil, err
	}
	transport := "http"
	if typ == "sse" {
		transport = "sse"
	}
	norm, err := McpFromParts(transport, nil, &resolved, opts.Enabled)
	if err != nil {
		return nil, nil, err
	}
	if len(headers) > 0 {
		norm.HeaderKeys = headers
		norm.Headers = headerValues
	}
	if typ == "sse" && norm.Transport != "sse" {
		notes = append(notes, fmt.Sprintf("registry lists %s as sse but its URL does not end in /sse; stored as http", MaskURL(resolved)))
	}
	return norm, notes, nil
}

func validateRegistryRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("remote URL %q must be an http(s) URL with a host", MaskURL(raw))
	}
	return nil
}

type registryResolver struct {
	envOverrides map[string]string
	argOverrides map[string]string
	vars         map[string]string
	usedArgs     map[string]bool
	posIndex     int
}

func newRegistryResolver(opts McpRegistryOptions) (*registryResolver, error) {
	r := &registryResolver{
		envOverrides: map[string]string{},
		argOverrides: map[string]string{},
		vars:         map[string]string{},
		usedArgs:     map[string]bool{},
	}
	for k, v := range opts.Env {
		name := strings.TrimSpace(k)
		if !registryEnvNameRe.MatchString(name) {
			return nil, fmt.Errorf("invalid --env name %q (use letters, digits and underscore, not starting with a digit)", name)
		}
		r.envOverrides[name] = v
	}
	for k, v := range opts.Args {
		name := strings.TrimLeft(strings.TrimSpace(k), "-")
		if !registryArgKeyRe.MatchString(name) {
			return nil, fmt.Errorf("invalid --arg name %q (use NAME=VALUE)", k)
		}
		r.argOverrides[name] = v
	}
	for k := range opts.Headers {
		name := strings.TrimSpace(k)
		if name == "" || strings.ContainsAny(name, " \t:") {
			return nil, fmt.Errorf("invalid --header name %q (use NAME=VALUE)", k)
		}
	}
	for k, v := range r.envOverrides {
		r.vars[k] = v
	}
	for k, v := range r.argOverrides {
		r.vars[k] = v
	}
	return r, nil
}

// resolveEnv applies --env over --env, declared value then default. Only names
// appear in errors, never values.
func (r *registryResolver) resolveEnv(decls []mcpregistry.EnvVar) (map[string]string, error) {
	env := map[string]string{}
	var missing []string
	for _, d := range decls {
		name := strings.TrimSpace(d.Name)
		if !registryEnvNameRe.MatchString(name) {
			return nil, fmt.Errorf("registry declares an invalid env name %q", name)
		}
		val, ok := r.envOverrides[name]
		if !ok {
			switch {
			case d.Value != "":
				val, ok = d.Value, true
			case d.Default != "":
				val, ok = d.Default, true
			}
		}
		if !ok {
			if d.IsRequired {
				missing = append(missing, name)
			}
			continue
		}
		val, _ = substituteRegistryVars(val, r.vars)
		if len(d.Choices) > 0 && !registryChoice(d.Choices, val) {
			return nil, fmt.Errorf("env %s must be one of: %s", name, strings.Join(d.Choices, ", "))
		}
		r.vars[name] = val
		env[name] = val
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env: %s (pass --env NAME=VALUE)", strings.Join(missing, ", "))
	}
	for k, v := range r.envOverrides {
		if _, ok := env[k]; !ok {
			env[k] = v
		}
	}
	return env, nil
}

func (r *registryResolver) resolveArguments(decls []mcpregistry.Argument) ([]string, error) {
	var out []string
	var missing []string
	for _, d := range decls {
		typ := strings.ToLower(strings.TrimSpace(d.Type))
		named := typ == "named"
		var key, flag string
		switch typ {
		case "named":
			flag = strings.TrimLeft(strings.TrimSpace(d.Name), "-")
			if flag == "" {
				return nil, fmt.Errorf("registry declares a named argument without a name")
			}
			if !registryArgKeyRe.MatchString(flag) {
				return nil, fmt.Errorf("registry declares an invalid argument name %q", d.Name)
			}
			key = flag
		case "positional", "":
			r.posIndex++
			key = strings.TrimSpace(d.ValueHint)
			if !registryArgKeyRe.MatchString(key) {
				key = fmt.Sprintf("arg%d", r.posIndex)
			}
		default:
			return nil, fmt.Errorf("registry declares an unsupported argument type %q", d.Type)
		}
		val, ok := r.argOverrides[key]
		if ok {
			r.usedArgs[key] = true
		}
		if !ok {
			switch {
			case d.Value != "":
				val, ok = d.Value, true
			case d.Default != "":
				val, ok = d.Default, true
			}
		}
		if !ok {
			if d.IsRequired {
				missing = append(missing, key)
			}
			continue
		}
		resolved, unresolved := substituteRegistryVars(val, r.vars)
		if len(unresolved) > 0 {
			return nil, fmt.Errorf("argument %s has unresolved variable(s): %s", key, strings.Join(unresolved, ", "))
		}
		r.vars[key] = resolved
		if named {
			if resolved == "" {
				out = append(out, "--"+flag)
			} else {
				out = append(out, "--"+flag+"="+resolved)
			}
			continue
		}
		out = append(out, resolved)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required argument value(s): %s (pass --arg NAME=VALUE)", strings.Join(missing, ", "))
	}
	return out, nil
}

func (r *registryResolver) resolveRemoteVariables(in map[string]mcpregistry.Input) (map[string]string, error) {
	out := map[string]string{}
	if len(in) == 0 {
		return out, nil
	}
	var missing []string
	for _, key := range sortedKeys(in) {
		spec := in[key]
		val, ok := r.envOverrides[key]
		if !ok {
			if v, fromArg := r.argOverrides[key]; fromArg {
				val, ok = v, true
				r.usedArgs[key] = true
			}
		}
		if !ok {
			switch {
			case spec.Value != "":
				val, ok = spec.Value, true
			case spec.Default != "":
				val, ok = spec.Default, true
			}
		}
		if !ok {
			if spec.IsRequired {
				missing = append(missing, key)
			}
			continue
		}
		val, _ = substituteRegistryVars(val, r.vars)
		if len(spec.Choices) > 0 && !registryChoice(spec.Choices, val) {
			return nil, fmt.Errorf("variable %s must be one of: %s", key, strings.Join(spec.Choices, ", "))
		}
		r.vars[key] = val
		out[key] = val
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required variable(s): %s (pass --env NAME=VALUE)", strings.Join(missing, ", "))
	}
	return out, nil
}

func (r *registryResolver) checkUnusedArgs() error {
	var unused []string
	for k := range r.argOverrides {
		if !r.usedArgs[k] {
			unused = append(unused, k)
		}
	}
	if len(unused) == 0 {
		return nil
	}
	sort.Strings(unused)
	return fmt.Errorf("unused --arg %s (this registry server has no such argument)", strings.Join(unused, ", "))
}

func registryChoice(choices []string, value string) bool {
	for _, c := range choices {
		if c == value {
			return true
		}
	}
	return false
}

// substituteRegistryVars replaces {name} tokens and returns the unknown ones.
// The caller decides whether leaving a token in place is acceptable.
func substituteRegistryVars(s string, vars map[string]string) (string, []string) {
	var missing []string
	seen := map[string]bool{}
	out := registryVarToken.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if v, ok := vars[name]; ok {
			return v
		}
		if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
		return m
	})
	return out, missing
}
