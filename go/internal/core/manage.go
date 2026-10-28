package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	Source   string `json:"source,omitempty"`
	// Origin is the lock entry a record_origin action writes. It is not part of
	// the JSON plan.
	Origin *LockEntry `json:"-"`
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
		Source   string `json:"source,omitempty"`
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
			Path: a.Path, SkillKey: a.SkillKey, Agent: a.Agent, Hub: a.Hub, Server: a.Server, Source: a.Source,
		})
	}
}

type ManagePlan struct {
	Scope   string         `json:"scope"`
	DryRun  bool           `json:"dry_run"`
	Actions []ManageAction `json:"actions"`
	Notes   []string       `json:"notes"`
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

// PlanInstallSkill plans a copy of sourceDir into the canonical directory and
// links to agents. A non-nil origin records where the skill came from. A nil
// origin on a skill the lock file lists as remote removes that record, since
// the installed copy is now a local one.
func PlanInstallSkill(scope Scope, cwd, sourceDir string, agentsFilter []AgentKind, origin *LockEntry) (*ManagePlan, error) {
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
	if lock := LockPath(scope, cwd); lock != "" {
		if origin != nil {
			entry := *origin
			if filepath.Base(lock) == projectLockName {
				hash, err := ComputeFolderHash(sourceDir)
				if err != nil {
					return nil, err
				}
				entry.ComputedHash = hash
				entry.FolderHash = ""
			}
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "record_origin", Path: lock, SkillKey: key, Source: entry.Source, Origin: &entry,
			})
		} else if _, ok := ReadLock(scope, cwd)[key]; ok {
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "forget_origin", Path: lock, SkillKey: key,
			})
		}
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
	requested := key
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
	found := false
	for _, e := range ScanSkills(scope, cwd) {
		if e.Key != key {
			continue
		}
		found = true
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
	if !found {
		return nil, fmt.Errorf("no skill named %s", requested)
	}
	if purge {
		if lock := LockPath(scope, cwd); lock != "" {
			if _, ok := ReadLock(scope, cwd)[key]; ok {
				plan.Actions = append(plan.Actions, ManageAction{
					Kind: "forget_origin", Path: lock, SkillKey: key,
				})
			}
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
	// Notes are printed. The real values stay on norm and are written on apply.
	if norm != nil {
		for _, envKey := range norm.EnvKeys {
			plan.Notes = append(plan.Notes, fmt.Sprintf("env %s=%s", envKey, SecretMask))
		}
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

func PlanRemoveMcp(scope Scope, cwd, name string, agentsFilter []AgentKind, keepHub bool) (*ManagePlan, error) {
	key := NormalizeKey(name)
	if CanonicalMcpPath(scope, cwd) == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	if keepHub && len(agentsFilter) == 0 {
		return nil, fmt.Errorf("--keep-hub requires --agents")
	}
	note := fmt.Sprintf("remove mcp '%s' from hub + agents", key)
	if keepHub {
		note = fmt.Sprintf("remove mcp '%s' from agents (hub kept)", key)
	}
	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{note},
	}
	found := false
	for _, e := range ScanMcp(scope, cwd) {
		if e.Key != key {
			continue
		}
		found = true
		for _, p := range e.Presence {
			// The hub is removed whenever the server is in it, including when
			// --agents limits which agent configs are edited. --keep-hub keeps
			// the hub so the next sync can restore the agent links.
			if p.Agent == AgentAgents.String() {
				if !keepHub {
					plan.Actions = append(plan.Actions, ManageAction{
						Kind: "remove_mcp_hub", Hub: p.Path, Server: key,
					})
				}
				continue
			}
			if agentsFilter != nil && !presenceAgentIn(p.Agent, agentsFilter) {
				continue
			}
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "remove_mcp_agent", Path: p.Path, Agent: p.Agent, Server: key,
			})
		}
	}
	if !found {
		return nil, fmt.Errorf("no MCP server named %s", name)
	}
	if len(plan.Actions) == 0 {
		plan.Notes = append(plan.Notes, fmt.Sprintf("nothing to remove for mcp '%s'", key))
	}
	return plan, nil
}

// ApplyManagePlan runs every action. A failed action is logged and the rest
// still run, except that a skill whose copy failed gets no link. All failures
// are returned together with the log.
func ApplyManagePlan(plan *ManagePlan, mcpPayload *McpNormalized) ([]string, error) {
	var log []string
	var failures []error
	failedSkills := map[string]bool{}
	for _, a := range plan.Actions {
		if a.SkillKey != "" && failedSkills[a.SkillKey] && (a.Kind == "symlink_skill" || a.Kind == "record_origin") {
			log = append(log, fmt.Sprintf("skipped %s for skill %s: the copy failed", a.Kind, a.SkillKey))
			continue
		}
		lines, err := applyManageAction(a, mcpPayload)
		log = append(log, lines...)
		if err != nil {
			subject := a.SkillKey
			if subject == "" {
				subject = a.Server
			}
			if a.Kind == "copy_skill" {
				failedSkills[a.SkillKey] = true
			}
			failures = append(failures, fmt.Errorf("%s %s: %w", a.Kind, subject, err))
		}
	}
	if len(failures) > 0 {
		return log, fmt.Errorf("%d of %d actions failed:\n%w", len(failures), len(plan.Actions), errors.Join(failures...))
	}
	return log, nil
}

func applyManageAction(a ManageAction, mcpPayload *McpNormalized) ([]string, error) {
	var log []string
	switch a.Kind {
	case "copy_skill":
		if err := replaceDirWithCopy(a.From, a.To); err != nil {
			return log, err
		}
		log = append(log, fmt.Sprintf("copied skill %s: %s -> %s", a.SkillKey, a.From, a.To))
	case "symlink_skill":
		err := ForceSymlink(a.Link, a.Target, a.Agent, &log)
		return log, err
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
	case "record_origin":
		if a.Origin == nil {
			return log, fmt.Errorf("origin required for record_origin")
		}
		if err := RecordOrigin(a.Path, a.SkillKey, *a.Origin); err != nil {
			return log, err
		}
		log = append(log, fmt.Sprintf("recorded origin of %s: %s", a.SkillKey, a.Source))
	case "forget_origin":
		removed, err := ForgetOrigin(a.Path, a.SkillKey)
		if err != nil {
			return log, err
		}
		if removed {
			log = append(log, fmt.Sprintf("forgot origin of %s", a.SkillKey))
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
	return log, nil
}

// deleteNormalized removes every entry whose name is the same MCP key.
// It reports whether a key was present so a missing server is not a rewrite.
func deleteNormalized(obj map[string]any, server string) bool {
	if len(obj) == 0 {
		return false
	}
	want := NormalizeKey(server)
	removed := false
	for name := range obj {
		if NormalizeKey(name) == want {
			delete(obj, name)
			removed = true
		}
	}
	return removed
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
	obj, ok := root["mcpServers"].(map[string]any)
	if !ok || !deleteNormalized(obj, server) {
		return nil
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
		root, err := readJSONObject(path)
		if err != nil {
			return err
		}
		obj, ok := root["mcp"].(map[string]any)
		if !ok || !deleteNormalized(obj, server) {
			return nil
		}
		return writeJSONPretty(path, root)
	default:
		root, err := readJSONObject(path)
		if err != nil {
			return err
		}
		obj, ok := root["mcpServers"].(map[string]any)
		if !ok || !deleteNormalized(obj, server) {
			return nil
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
	servers, ok := root["mcp_servers"].(map[string]any)
	if !ok || !deleteNormalized(servers, server) {
		return nil
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(root); err != nil {
		return err
	}
	return atomicWriteFile(path, []byte(sb.String()), 0o644)
}

// ---------- convenience wrappers ----------

func InstallSkill(scope Scope, cwd, source string, agentsFilter []AgentKind, dryRun bool) (*ManagePlan, []string, error) {
	res, err := ResolveSkillSource(source)
	if err != nil {
		return nil, nil, err
	}
	defer res.Cleanup()
	plan, err := PlanInstallSkill(scope, cwd, res.Dir, agentsFilter, res.Origin)
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

func RemoveMcp(scope Scope, cwd, name string, agentsFilter []AgentKind, keepHub, dryRun bool) (*ManagePlan, []string, error) {
	plan, err := PlanRemoveMcp(scope, cwd, name, agentsFilter, keepHub)
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
	var parts []string
	if command != nil {
		parts = ParseCommandLine(*command)
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty --command")
		}
	}
	return McpFromParts(transport, parts, url, enabled)
}

// McpFromParts builds a normalized server from an argv that is already split.
// Registry installs use it so a package spec is never re-parsed as a shell line.
func McpFromParts(transport string, command []string, url *string, enabled *bool) (*McpNormalized, error) {
	requested := strings.ToLower(strings.TrimSpace(transport))
	canonical := CanonicalTransport(&requested, url, len(command) > 0)
	norm := &McpNormalized{
		Transport: canonical,
		Enabled:   enabled,
		EnvKeys:   []string{},
		Env:       map[string]string{},
	}
	switch canonical {
	case "stdio":
		if len(command) == 0 {
			return nil, fmt.Errorf("--command required for stdio")
		}
		norm.Command = []string{command[0]}
		if len(command) > 1 {
			norm.Args = append([]string{}, command[1:]...)
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

// lookupProvider resolves a coding-agent provider. The canonical hub
// ("agents") is not a provider and is rejected.
func lookupProvider(name string) (AgentKind, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == AgentAgents.String() {
		return 0, false
	}
	for _, a := range AllAgents() {
		if a != AgentAgents && a.String() == name {
			return a, true
		}
	}
	return 0, false
}

// ClearProvider deletes one provider's skill copies and MCP entries for a scope.
// The canonical hub is never touched. A skills root that is a symlink is removed
// as a link only. MCP files keep every key that is not the server table, so a
// Grok config or Claude settings file is not wiped.
func ClearProvider(scope Scope, cwd, name string, dryRun bool) ([]string, error) {
	agent, ok := lookupProvider(name)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	canonical := CanonicalSkillsDir(scope, cwd)
	if scope == ScopeProject && canonical == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	var log []string
	for _, r := range SkillRoots(scope, cwd) {
		if r.Agent != agent {
			continue
		}
		line, err := clearSkillsRoot(r.Path, canonical, dryRun)
		if err != nil {
			return log, err
		}
		log = append(log, line)
	}
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent != agent {
			continue
		}
		line, err := clearAgentMcp(r.Path, agent, dryRun)
		if err != nil {
			return log, err
		}
		log = append(log, line)
	}
	if len(log) == 0 {
		log = append(log, "nothing to remove")
	}
	return log, nil
}

func clearSkillsRoot(root, canonical string, dryRun bool) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "skills: nothing at " + root, nil
	}
	if rootAliasesCanonical(root, canonical) && info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("refusing to delete %s: it is the canonical skills directory", root)
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	kind := "skills root"
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "skills symlink"
	}
	if !dryRun {
		if err := RemovePath(root); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%s %s %s", verb, kind, root), nil
}

func clearAgentMcp(path string, agent AgentKind, dryRun bool) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		return "mcp: nothing at " + path, nil
	}
	switch agent {
	case AgentGrok:
		return clearGrokMcpTable(path, dryRun)
	case AgentOpenCode:
		return clearJSONKey(path, "mcp", dryRun)
	default:
		return clearJSONKey(path, "mcpServers", dryRun)
	}
}

func clearJSONKey(path, key string, dryRun bool) (string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	root := map[string]any{}
	if strings.TrimSpace(string(text)) != "" {
		if err := json.Unmarshal(text, &root); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
	}
	servers, _ := root[key].(map[string]any)
	if len(servers) == 0 {
		return "mcp: no servers in " + path, nil
	}
	if !dryRun {
		delete(root, key)
		if err := writeJSONPretty(path, root); err != nil {
			return "", err
		}
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	return fmt.Sprintf("%s %d mcp server(s) from %s", verb, len(servers), path), nil
}

func clearGrokMcpTable(path string, dryRun bool) (string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(text)) == "" {
		return "mcp: no servers in " + path, nil
	}
	var root map[string]any
	if _, err := toml.Decode(string(text), &root); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	servers, _ := root["mcp_servers"].(map[string]any)
	if len(servers) == 0 {
		return "mcp: no servers in " + path, nil
	}
	if !dryRun {
		delete(root, "mcp_servers")
		var sb strings.Builder
		if err := toml.NewEncoder(&sb).Encode(root); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			return "", err
		}
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	return fmt.Sprintf("%s %d mcp server(s) from %s", verb, len(servers), path), nil
}
