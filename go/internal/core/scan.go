package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

func ScanAll(scope Scope, cwd string) Inventory {
	inv := Inventory{
		Scope:  scope.String(),
		Skills: ScanSkills(scope, cwd),
		Mcps:   ScanMcp(scope, cwd),
	}
	if scope == ScopeProject {
		if r := ProjectRoot(cwd); r != "" {
			inv.ProjectRoot = &r
		}
	}
	return inv
}

func ScanSkills(scope Scope, cwd string) []SkillEntry {
	byKey := map[string]*SkillEntry{}
	var order []string

	for _, root := range SkillRoots(scope, cwd) {
		st, err := os.Stat(root.Path)
		if err != nil || !st.IsDir() {
			continue
		}
		dh, err := os.Open(root.Path)
		if err != nil {
			continue
		}
		names, err := dh.Readdirnames(-1)
		dh.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			path := filepath.Join(root.Path, name)
			linfo, lerr := os.Lstat(path)
			isSymlink := lerr == nil && linfo.Mode()&os.ModeSymlink != 0
			isDir := false
			if isSymlink {
				if st, err := os.Stat(path); err == nil {
					isDir = st.IsDir()
				}
			} else if lerr == nil {
				isDir = linfo.IsDir()
			}
			if !isDir && !isSymlink {
				continue
			}

			var symlinkTarget *string
			if isSymlink {
				if t, err := os.Readlink(path); err == nil {
					symlinkTarget = &t
				}
			}
			resolveForHash := path
			if isSymlink {
				if r, err := filepath.EvalSymlinks(path); err == nil {
					resolveForHash = r
				} else if symlinkTarget != nil {
					if filepath.IsAbs(*symlinkTarget) {
						resolveForHash = *symlinkTarget
					} else {
						resolveForHash = filepath.Join(root.Path, *symlinkTarget)
					}
				}
			}
			hash, err := HashSkillDir(resolveForHash)
			if err != nil {
				hash = "missing"
			}
			display := SkillDisplayName(resolveForHash)
			description := skillDescription(resolveForHash)
			key := NormalizeKey(display)

			presence := SkillPresence{
				Agent:         root.Agent.String(),
				Path:          path,
				IsSymlink:     isSymlink,
				SymlinkTarget: symlinkTarget,
				ContentHash:   hash,
			}
			if e, ok := byKey[key]; ok {
				if e.Description == nil && description != nil {
					e.Description = description
				}
				e.Presence = append(e.Presence, presence)
			} else {
				byKey[key] = &SkillEntry{
					Key:         key,
					DisplayName: display,
					Description: description,
					Scope:       scope.String(),
					Presence:    []SkillPresence{presence},
				}
				order = append(order, key)
			}
		}
	}

	out := make([]SkillEntry, 0, len(byKey))
	for _, k := range order {
		e := *byKey[k]
		hashes := map[string]bool{}
		for _, p := range e.Presence {
			hashes[p.ContentHash] = true
		}
		e.Mismatch = len(hashes) > 1
		sort.SliceStable(e.Presence, func(i, j int) bool {
			return agentOrder(e.Presence[i].Agent) < agentOrder(e.Presence[j].Agent)
		})
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func agentOrder(name string) int {
	for i, n := range agentNames {
		if n == name {
			return i
		}
	}
	return len(agentNames)
}

func ScanMcp(scope Scope, cwd string) []McpEntry {
	byKey := map[string]*McpEntry{}
	var order []string

	for _, ap := range McpConfigPaths(scope, cwd) {
		st, err := os.Stat(ap.Path)
		if err != nil || st.IsDir() {
			continue
		}
		servers, err := ReadMcpServers(ap.Agent, ap.Path)
		if err != nil {
			continue
		}
		for _, name := range sortedKeys(servers) {
			norm := servers[name]
			key := NormalizeKey(name)
			fp := McpFingerprint(&norm)
			presence := McpPresence{
				Agent:       ap.Agent.String(),
				Path:        ap.Path,
				Normalized:  &norm,
				Fingerprint: fp,
			}
			if e, ok := byKey[key]; ok {
				e.Presence = append(e.Presence, presence)
			} else {
				byKey[key] = &McpEntry{
					Key:      key,
					Scope:    scope.String(),
					Presence: []McpPresence{presence},
				}
				order = append(order, key)
			}
		}
	}

	out := make([]McpEntry, 0, len(byKey))
	for _, k := range order {
		e := *byKey[k]
		fps := map[string]bool{}
		for _, p := range e.Presence {
			fps[p.Fingerprint] = true
		}
		e.Mismatch = len(fps) > 1
		sort.SliceStable(e.Presence, func(i, j int) bool {
			return agentOrder(e.Presence[i].Agent) < agentOrder(e.Presence[j].Agent)
		})
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func McpFingerprint(n *McpNormalized) string {
	var parts []string
	parts = append(parts, n.Transport)
	if n.Command != nil {
		parts = append(parts, "cmd:"+strings.Join(n.Command, "\x1f"))
	}
	if n.Args != nil {
		parts = append(parts, "args:"+strings.Join(n.Args, "\x1f"))
	}
	if n.URL != nil {
		parts = append(parts, "url:"+*n.URL)
	}
	keys := append([]string{}, n.EnvKeys...)
	sort.Strings(keys)
	parts = append(parts, "env:"+strings.Join(keys, ","))
	return HashBytes([]byte(strings.Join(parts, "|")))
}

func ReadMcpServers(agent AgentKind, path string) (map[string]McpNormalized, error) {
	switch agent {
	case AgentGrok:
		return readGrokToml(path)
	case AgentOpenCode:
		return readOpencodeJSON(path)
	default:
		return readMcpServersJSON(path)
	}
}

func readMcpServersJSON(path string) (map[string]McpNormalized, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(text, &root); err != nil {
		return nil, err
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers, _ = root["mcp"].(map[string]any)
	}
	out := map[string]McpNormalized{}
	for name, cfg := range servers {
		if obj, ok := cfg.(map[string]any); ok {
			out[name] = *jsonToNormalized(obj)
		}
	}
	return out, nil
}

func readOpencodeJSON(path string) (map[string]McpNormalized, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(text, &root); err != nil {
		return nil, err
	}
	mcp, _ := root["mcp"].(map[string]any)
	out := map[string]McpNormalized{}
	for name, cfg := range mcp {
		if obj, ok := cfg.(map[string]any); ok {
			out[name] = *jsonToNormalized(obj)
		}
	}
	return out, nil
}

func readGrokToml(path string) (map[string]McpNormalized, error) {
	var value map[string]any
	if _, err := toml.DecodeFile(path, &value); err != nil {
		return nil, err
	}
	out := map[string]McpNormalized{}
	servers, _ := value["mcp_servers"].(map[string]any)
	for name, cfg := range servers {
		if tbl, ok := cfg.(map[string]any); ok {
			out[name] = *tomlToNormalized(tbl)
		}
	}
	return out, nil
}

// CanonicalTransport mirrors scan.rs::canonical_transport.
func CanonicalTransport(raw, url *string, hasCommand bool) string {
	var r string
	if raw != nil {
		r = strings.ToLower(strings.TrimSpace(*raw))
	}
	sseEndpoint := false
	if url != nil {
		sseEndpoint = strings.HasSuffix(strings.TrimRight(*url, "/"), "/sse")
	}
	switch r {
	case "stdio", "local":
		return "stdio"
	case "sse":
		if sseEndpoint {
			return "sse"
		}
		return "http"
	case "http", "remote", "streamable-http", "streamable_http", "url":
		return "http"
	case "":
		if url != nil && !hasCommand {
			if sseEndpoint {
				return "sse"
			}
			return "http"
		}
		return "stdio"
	default:
		return r
	}
}

// splitCommandArray: command ["uvx","a","b"] → command ["uvx"] + args ["a","b",...]
func splitCommandArray(command, args *[]string) {
	if command == nil || *command == nil || len(*command) <= 1 {
		return
	}
	merged := append([]string{}, (*command)[1:]...)
	if args != nil && *args != nil {
		merged = append(merged, (*args)...)
	}
	*command = (*command)[:1]
	*args = merged
}

func strPtr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

func strSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func jsonToNormalized(cfg map[string]any) *McpNormalized {
	var rawType *string
	if t, ok := cfg["type"]; ok {
		rawType = strPtr(t)
	} else if t, ok := cfg["transport"]; ok {
		rawType = strPtr(t)
	}
	url := strPtr(cfg["url"])
	_, hasCommand := cfg["command"]
	transport := CanonicalTransport(rawType, url, hasCommand)

	var command []string
	switch c := cfg["command"].(type) {
	case string:
		command = []string{c}
	case []any:
		command = strSlice(c)
	}
	args := strSlice(cfg["args"])
	splitCommandArray(&command, &args)

	var enabled *bool
	if b, ok := cfg["enabled"].(bool); ok {
		enabled = &b
	} else if d, ok := cfg["disabled"].(bool); ok {
		v := !d
		enabled = &v
	}

	env := map[string]string{}
	envKeys := []string{}
	var envObj map[string]any
	if e, ok := cfg["env"].(map[string]any); ok {
		envObj = e
	} else if e, ok := cfg["environment"].(map[string]any); ok {
		envObj = e
	}
	for k, v := range envObj {
		envKeys = append(envKeys, k)
		if s, ok := v.(string); ok {
			env[k] = s
		} else {
			b, _ := json.Marshal(v)
			env[k] = string(b)
		}
	}
	sort.Strings(envKeys)

	return &McpNormalized{
		Transport: transport,
		Command:   command,
		URL:       url,
		Args:      args,
		Enabled:   enabled,
		EnvKeys:   envKeys,
		Env:       env,
	}
}

func tomlToNormalized(cfg map[string]any) *McpNormalized {
	var rawType *string
	if t, ok := cfg["type"]; ok {
		rawType = strPtr(t)
	} else if t, ok := cfg["transport"]; ok {
		rawType = strPtr(t)
	}
	url := strPtr(cfg["url"])
	_, hasCommand := cfg["command"]
	transport := CanonicalTransport(rawType, url, hasCommand)

	var command []string
	switch c := cfg["command"].(type) {
	case string:
		command = []string{c}
	default:
		command = strSlice(c)
	}
	args := strSlice(cfg["args"])
	splitCommandArray(&command, &args)

	var enabled *bool
	if b, ok := cfg["enabled"].(bool); ok {
		enabled = &b
	}

	env := map[string]string{}
	envKeys := []string{}
	if tbl, ok := cfg["env"].(map[string]any); ok {
		for k, v := range tbl {
			envKeys = append(envKeys, k)
			if s, ok := v.(string); ok {
				env[k] = s
			} else {
				b, _ := json.Marshal(v)
				env[k] = string(b)
			}
		}
	}
	sort.Strings(envKeys)

	return &McpNormalized{
		Transport: transport,
		Command:   command,
		URL:       url,
		Args:      args,
		Enabled:   enabled,
		EnvKeys:   envKeys,
		Env:       env,
	}
}
