package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ManageAction — same serde shape: {"kind":...,variant fields}.
type ManageAction struct {
	Kind     string `json:"kind"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Link     string `json:"link,omitempty"`
	Target   string `json:"target,omitempty"`
	SkillKey string `json:"skill_key,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Path     string `json:"path,omitempty"`
	Hub      string `json:"hub,omitempty"`
	Server   string `json:"server,omitempty"`
}

// MarshalJSON matches the serde enum field order per variant.
func (a ManageAction) MarshalJSON() ([]byte, error) {
	type j struct {
		Kind     string `json:"kind"`
		From     string `json:"from,omitempty"`
		To       string `json:"to,omitempty"`
		Link     string `json:"link,omitempty"`
		Target   string `json:"target,omitempty"`
		Path     string `json:"path,omitempty"`
		SkillKey string `json:"skill_key,omitempty"`
		Agent    string `json:"agent,omitempty"`
		Hub      string `json:"hub,omitempty"`
		Server   string `json:"server,omitempty"`
	}
	switch a.Kind {
	case "unlink_skill":
		return json.Marshal(struct {
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			SkillKey string `json:"skill_key"`
			Agent    string `json:"agent"`
		}{a.Kind, a.Path, a.SkillKey, a.Agent})
	case "purge_canonical_skill":
		return json.Marshal(struct {
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			SkillKey string `json:"skill_key"`
		}{a.Kind, a.Path, a.SkillKey})
	case "write_mcp_agent", "remove_mcp_agent":
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			Agent  string `json:"agent"`
			Server string `json:"server"`
		}{a.Kind, a.Path, a.Agent, a.Server})
	default:
		return json.Marshal(j{
			Kind: a.Kind, From: a.From, To: a.To, Link: a.Link, Target: a.Target,
			Path: a.Path, SkillKey: a.SkillKey, Agent: a.Agent, Hub: a.Hub, Server: a.Server,
		})
	}
}

type ManagePlan struct {
	Scope   string         `json:"scope"`
	DryRun  bool           `json:"dry_run"`
	Actions []ManageAction `json:"actions"`
	Notes   []string       `json:"notes"`
}

// ResolveSkillSource resolves a local path or git URL to a skill dir.
// Returns (skillDir, cleanup func or nil).
func ResolveSkillSource(src string) (string, func(), error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return "", nil, fmt.Errorf("empty skill source")
	}
	looksGit := strings.HasPrefix(trimmed, "git@") ||
		strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://") ||
		strings.HasSuffix(trimmed, ".git") ||
		strings.Contains(trimmed, "github.com/") ||
		strings.Contains(trimmed, "gitlab.com/")

	st, statErr := os.Stat(trimmed)
	isDir := statErr == nil && st.IsDir()

	if looksGit && !isDir {
		tmp, err := os.MkdirTemp("", "synca-*")
		if err != nil {
			return "", nil, err
		}
		cleanup := func() { os.RemoveAll(tmp) }
		dest := filepath.Join(tmp, "repo")
		cmd := exec.Command("git", "clone", "--depth", "1", trimmed, dest)
		if err := cmd.Run(); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("git clone failed for %s", trimmed)
		}
		dir, err := findSkillDir(dest)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		return dir, cleanup, nil
	}

	path := trimmed
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		path = filepath.Join(cwd, path)
	}
	if _, err := os.Stat(path); err != nil {
		return "", nil, fmt.Errorf("skill source not found: %s", path)
	}
	dir, err := findSkillDir(path)
	if err != nil {
		return "", nil, err
	}
	return dir, nil, nil
}

func findSkillDir(path string) (string, error) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		path = r
	} else {
		path, _ = filepath.Abs(path)
	}
	if st, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil && !st.IsDir() {
		return path, nil
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		var candidates []string
		for _, e := range entries {
			p := filepath.Join(path, e.Name())
			if st, err := os.Stat(filepath.Join(p, "SKILL.md")); err == nil && !st.IsDir() {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		if len(candidates) == 0 {
			nested := filepath.Join(path, ".agents/skills")
			if st, err := os.Stat(nested); err == nil && st.IsDir() {
				return findSkillDir(nested)
			}
			return "", fmt.Errorf("no SKILL.md under %s", path)
		}
		return "", fmt.Errorf("multiple skills under %s; point at a specific skill folder", path)
	}
	return "", fmt.Errorf("not a skill directory: %s", path)
}

func skillKeyFromDir(dir string) string {
	var name string
	if n, ok := skillFrontmatterName(filepath.Join(dir, "SKILL.md")); ok {
		name = n
	} else {
		name = SkillDisplayName(dir)
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	return NormalizeKey(name)
}

func PlanInstallSkill(scope Scope, cwd, sourceDir string, agentsFilter []AgentKind) (*ManagePlan, error) {
	canonical := CanonicalSkillsDir(scope, cwd)
	if canonical == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	key := skillKeyFromDir(sourceDir)
	canonSkill := filepath.Join(canonical, key)
	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{fmt.Sprintf("install skill '%s' from %s", key, sourceDir)},
	}
	plan.Actions = append(plan.Actions, ManageAction{
		Kind: "copy_skill", From: sourceDir, To: canonSkill, SkillKey: key,
	})
	for _, r := range SkillRoots(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		if rootAliasesCanonical(r.Path, canonical) {
			continue
		}
		plan.Actions = append(plan.Actions, ManageAction{
			Kind: "symlink_skill", Link: filepath.Join(r.Path, key), Target: canonSkill,
			SkillKey: key, Agent: r.Agent.String(),
		})
	}
	return plan, nil
}

func presenceAgentIn(name string, filter []AgentKind) bool {
	for _, a := range filter {
		if a.String() == name {
			return true
		}
	}
	return false
}

func PlanRemoveSkill(scope Scope, cwd, key string, agentsFilter []AgentKind, purge bool) (*ManagePlan, error) {
	key = NormalizeKey(key)
	canonical := CanonicalSkillsDir(scope, cwd)
	if canonical == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	note := fmt.Sprintf("unlink skill '%s' from agents (canonical kept)", key)
	if purge {
		note = fmt.Sprintf("PURGE skill '%s' (canonical + all agent links)", key)
	}
	plan := &ManagePlan{Scope: scope.String(), DryRun: true, Notes: []string{note}}
	// Skill keys come from SKILL.md frontmatter, so the folder name can differ
	// from the key (folder "gitbutler", name "but"). Act on the paths the scan
	// actually found for this key instead of guessing <root>/<key>.
	for _, e := range ScanSkills(scope, cwd) {
		if e.Key != key {
			continue
		}
		for _, p := range e.Presence {
			if p.Agent == AgentAgents.String() {
				if purge {
					plan.Actions = append(plan.Actions, ManageAction{
						Kind: "purge_canonical_skill", Path: p.Path, SkillKey: key,
					})
				}
				continue
			}
			if agentsFilter != nil && !presenceAgentIn(p.Agent, agentsFilter) {
				continue
			}
			// This agent reads the canonical dir through a symlinked root, so the path
			// is the canonical copy itself; unlinking it would delete the skill.
			if rootAliasesCanonical(filepath.Dir(p.Path), canonical) {
				continue
			}
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "unlink_skill", Path: p.Path, SkillKey: key, Agent: p.Agent,
			})
		}
	}
	if len(plan.Actions) == 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("nothing to remove for skill '%s'", key))
	}
	return plan, nil
}

func PlanAddMcp(scope Scope, cwd, name string, norm *McpNormalized, agentsFilter []AgentKind) (*ManagePlan, error) {
	key := NormalizeKey(name)
	hub := CanonicalMcpPath(scope, cwd)
	if hub == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{fmt.Sprintf("add mcp '%s' to hub + agents", key)},
	}
	plan.Actions = append(plan.Actions, ManageAction{
		Kind: "upsert_mcp_hub", Hub: hub, Server: key,
	})
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		plan.Actions = append(plan.Actions, ManageAction{
			Kind: "write_mcp_agent", Path: r.Path, Agent: r.Agent.String(), Server: key,
		})
	}
	return plan, nil
}

func PlanRemoveMcp(scope Scope, cwd, name string, agentsFilter []AgentKind) (*ManagePlan, error) {
	key := NormalizeKey(name)
	hub := CanonicalMcpPath(scope, cwd)
	if hub == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{fmt.Sprintf("remove mcp '%s' from hub + agents", key)},
	}
	plan.Actions = append(plan.Actions, ManageAction{
		Kind: "remove_mcp_hub", Hub: hub, Server: key,
	})
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		plan.Actions = append(plan.Actions, ManageAction{
			Kind: "remove_mcp_agent", Path: r.Path, Agent: r.Agent.String(), Server: key,
		})
	}
	return plan, nil
}

// ApplyManagePlan.
func ApplyManagePlan(plan *ManagePlan, mcpPayload *McpNormalized) ([]string, error) {
	var log []string
	for _, a := range plan.Actions {
		switch a.Kind {
		case "copy_skill":
			if parent := filepath.Dir(a.To); parent != "" {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return log, err
				}
			}
			if _, err := os.Lstat(a.To); err == nil {
				if err := RemovePath(a.To); err != nil {
					return log, err
				}
			}
			if err := CopyDirRecursive(a.From, a.To); err != nil {
				return log, err
			}
			log = append(log, fmt.Sprintf("copied skill %s: %s -> %s", a.SkillKey, a.From, a.To))
		case "symlink_skill":
			if err := ForceSymlink(a.Link, a.Target, a.Agent, &log); err != nil {
				return log, err
			}
		case "unlink_skill":
			if _, err := os.Lstat(a.Path); err == nil {
				if err := RemovePath(a.Path); err != nil {
					return log, err
				}
				log = append(log, fmt.Sprintf("unlinked skill %s from %s: %s", a.SkillKey, a.Agent, a.Path))
			}
		case "purge_canonical_skill":
			if _, err := os.Lstat(a.Path); err == nil {
				if err := RemovePath(a.Path); err != nil {
					return log, err
				}
				log = append(log, fmt.Sprintf("purged canonical skill %s: %s", a.SkillKey, a.Path))
			}
		case "upsert_mcp_hub":
			if mcpPayload == nil {
				return log, fmt.Errorf("mcp payload required for UpsertMcpHub")
			}
			if err := UpsertMcpJSONHub(a.Hub, a.Server, mcpPayload); err != nil {
				return log, err
			}
			log = append(log, fmt.Sprintf("hub upsert %s in %s", a.Server, a.Hub))
		case "write_mcp_agent":
			if mcpPayload == nil {
				return log, fmt.Errorf("mcp payload required for WriteMcpAgent")
			}
			if err := WriteMcpToAgent(a.Path, agentByName(a.Agent), a.Server, mcpPayload); err != nil {
				return log, err
			}
			log = append(log, fmt.Sprintf("wrote mcp %s -> %s (%s)", a.Server, a.Path, a.Agent))
		case "remove_mcp_hub":
			if err := removeMcpFromJSONHub(a.Hub, a.Server); err != nil {
				return log, err
			}
			log = append(log, fmt.Sprintf("hub remove %s from %s", a.Server, a.Hub))
		case "remove_mcp_agent":
			if err := removeMcpFromAgent(a.Path, a.Agent, a.Server); err != nil {
				return log, err
			}
			log = append(log, fmt.Sprintf("removed mcp %s from %s (%s)", a.Server, a.Path, a.Agent))
		}
	}
	return log, nil
}

func removeMcpFromJSONHub(hub, server string) error {
	if st, err := os.Stat(hub); err != nil || st.IsDir() {
		return nil
	}
	text, err := os.ReadFile(hub)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(text, &root); err != nil {
		return err
	}
	if obj, ok := root["mcpServers"].(map[string]any); ok {
		delete(obj, server)
	}
	return writeJSONPretty(hub, root)
}

func removeMcpFromAgent(path, agent, server string) error {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return nil
	}
	switch agent {
	case "grok":
		return removeMcpFromGrokToml(path, server)
	case "opencode":
		root := readJSONFile(path)
		if obj, ok := root["mcp"].(map[string]any); ok {
			delete(obj, server)
		}
		return writeJSONPretty(path, root)
	default:
		root := readJSONFile(path)
		if obj, ok := root["mcpServers"].(map[string]any); ok {
			delete(obj, server)
		}
		return writeJSONPretty(path, root)
	}
}

func removeMcpFromGrokToml(path, server string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(text)) == "" {
		return nil
	}
	var root map[string]any
	if _, err := toml.Decode(string(text), &root); err != nil {
		return err
	}
	if servers, ok := root["mcp_servers"].(map[string]any); ok {
		delete(servers, server)
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(root); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// ---------- convenience wrappers ----------

func InstallSkill(scope Scope, cwd, source string, agentsFilter []AgentKind, dryRun bool) (*ManagePlan, []string, error) {
	dir, cleanup, err := ResolveSkillSource(source)
	if err != nil {
		return nil, nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	plan, err := PlanInstallSkill(scope, cwd, dir, agentsFilter)
	if err != nil {
		return nil, nil, err
	}
	plan.DryRun = dryRun
	if dryRun {
		return plan, nil, nil
	}
	log, err := ApplyManagePlan(plan, nil)
	return plan, log, err
}

func RemoveSkill(scope Scope, cwd, key string, agentsFilter []AgentKind, purge, dryRun bool) (*ManagePlan, []string, error) {
	plan, err := PlanRemoveSkill(scope, cwd, key, agentsFilter, purge)
	if err != nil {
		return nil, nil, err
	}
	plan.DryRun = dryRun
	if dryRun {
		return plan, nil, nil
	}
	log, err := ApplyManagePlan(plan, nil)
	return plan, log, err
}

func AddMcp(scope Scope, cwd, name string, norm *McpNormalized, agentsFilter []AgentKind, dryRun bool) (*ManagePlan, []string, error) {
	plan, err := PlanAddMcp(scope, cwd, name, norm, agentsFilter)
	if err != nil {
		return nil, nil, err
	}
	plan.DryRun = dryRun
	if dryRun {
		return plan, nil, nil
	}
	log, err := ApplyManagePlan(plan, norm)
	return plan, log, err
}

func RemoveMcp(scope Scope, cwd, name string, agentsFilter []AgentKind, dryRun bool) (*ManagePlan, []string, error) {
	plan, err := PlanRemoveMcp(scope, cwd, name, agentsFilter)
	if err != nil {
		return nil, nil, err
	}
	plan.DryRun = dryRun
	if dryRun {
		return plan, nil, nil
	}
	log, err := ApplyManagePlan(plan, nil)
	return plan, log, err
}

// ParseCommandLine — whitespace split with light quote support.
func ParseCommandLine(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, ch := range s {
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				cur.WriteRune(ch)
			}
		} else if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		} else {
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func McpFromCLI(transport string, command, url *string, enabled *bool) (*McpNormalized, error) {
	requested := strings.ToLower(strings.TrimSpace(transport))
	canonical := CanonicalTransport(&requested, url, command != nil)
	norm := &McpNormalized{
		Transport: canonical,
		Enabled:   enabled,
		EnvKeys:   []string{},
		Env:       map[string]string{},
	}
	switch canonical {
	case "stdio":
		if command == nil {
			return nil, fmt.Errorf("--command required for stdio")
		}
		parts := ParseCommandLine(*command)
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty --command")
		}
		norm.Command = parts[:1]
		if len(parts) > 1 {
			norm.Args = parts[1:]
		}
	case "http", "sse":
		if url == nil {
			return nil, fmt.Errorf("--url required for %s", requested)
		}
		norm.URL = url
	default:
		return nil, fmt.Errorf("unknown transport '%s' (use stdio|http|sse|remote|local)", requested)
	}
	return norm, nil
}
