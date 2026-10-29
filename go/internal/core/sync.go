package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// SyncAction carries the union of all serde variants; Kind is the snake_case
// tag emitted as `"kind"` and only the fields of that variant are populated.
type SyncAction struct {
	Kind         string   `json:"kind"`
	From         string   `json:"from,omitempty"`
	To           string   `json:"to,omitempty"`
	Link         string   `json:"link,omitempty"`
	Target       string   `json:"target,omitempty"`
	SkillKey     string   `json:"skill_key,omitempty"`
	Agent        string   `json:"agent,omitempty"`
	Path         string   `json:"path,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	Hashes       []string `json:"hashes,omitempty"`
	Hub          string   `json:"hub,omitempty"`
	Server       string   `json:"server,omitempty"`
	SourceAgent  string   `json:"source_agent,omitempty"`
	Fingerprints []string `json:"fingerprints,omitempty"`
}

// MarshalJSON emits per-variant field order identical to the serde enum
// (tagged "kind"), so dry-run text output is stable.
func (a SyncAction) MarshalJSON() ([]byte, error) {
	type j struct {
		Kind         string   `json:"kind"`
		From         string   `json:"from,omitempty"`
		To           string   `json:"to,omitempty"`
		Link         string   `json:"link,omitempty"`
		Target       string   `json:"target,omitempty"`
		Path         string   `json:"path,omitempty"`
		SkillKey     string   `json:"skill_key,omitempty"`
		Agent        string   `json:"agent,omitempty"`
		Reason       string   `json:"reason,omitempty"`
		Paths        []string `json:"paths,omitempty"`
		Hashes       []string `json:"hashes,omitempty"`
		Hub          string   `json:"hub,omitempty"`
		Server       string   `json:"server,omitempty"`
		SourceAgent  string   `json:"source_agent,omitempty"`
		Fingerprints []string `json:"fingerprints,omitempty"`
	}
	switch a.Kind {
	case "skip_same", "repair_skill_frontmatter":
		return json.Marshal(j{Kind: a.Kind, Path: a.Path, SkillKey: a.SkillKey, Reason: a.Reason})
	case "write_mcp_server":
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			Agent  string `json:"agent"`
			Server string `json:"server"`
		}{a.Kind, a.Path, a.Agent, a.Server})
	case "skip_mcp_same":
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Path   string `json:"path"`
			Server string `json:"server"`
			Reason string `json:"reason"`
		}{a.Kind, a.Path, a.Server, a.Reason})
	case "ensure_mcp_hub":
		return json.Marshal(struct {
			Kind        string `json:"kind"`
			Hub         string `json:"hub"`
			Server      string `json:"server"`
			SourceAgent string `json:"source_agent"`
		}{a.Kind, a.Hub, a.Server, a.SourceAgent})
	default:
		return json.Marshal(j{
			Kind: a.Kind, From: a.From, To: a.To, Link: a.Link, Target: a.Target,
			Path: a.Path, SkillKey: a.SkillKey, Agent: a.Agent, Reason: a.Reason,
			Paths: a.Paths, Hashes: a.Hashes, Hub: a.Hub, Server: a.Server,
			SourceAgent: a.SourceAgent, Fingerprints: a.Fingerprints,
		})
	}
}

type SyncPlan struct {
	Scope   string       `json:"scope"`
	DryRun  bool         `json:"dry_run"`
	Actions []SyncAction `json:"actions"`
}

func PlanSyncSkills(scope Scope, cwd string, agentsFilter []AgentKind, onlyKey string) (*SyncPlan, error) {
	skills := ScanSkills(scope, cwd)
	canonical := CanonicalSkillsDir(scope, cwd)
	if canonical == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	var targets []agentPath
	for _, r := range SkillRoots(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		// A root that is itself the canonical dir (e.g. .cursor/skills -> ../.agents/skills)
		// already sees every canonical skill; "linking" into it would replace the
		// canonical copy with a link to itself.
		if rootAliasesCanonical(r.Path, canonical) {
			continue
		}
		targets = append(targets, r)
	}

	plan := &SyncPlan{Scope: scope.String(), DryRun: true, Actions: []SyncAction{}}

	for _, skill := range skills {
		if onlyKey != "" && skill.Key != NormalizeKey(onlyKey) && skill.DisplayName != onlyKey {
			continue
		}
		if skill.Mismatch {
			var paths, hashes []string
			for _, p := range skill.Presence {
				paths = append(paths, p.Path)
				hashes = append(hashes, p.ContentHash)
			}
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "conflict_skill", SkillKey: skill.Key, Paths: paths, Hashes: hashes,
			})
			continue
		}

		source := pickPresence(skill.Presence)
		if source == nil {
			// Only dangling links exist: there is no content to copy. Copying from
			// them would create an empty canonical skill.
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "skip_same", Path: skill.Presence[0].Path, SkillKey: skill.Key,
				Reason: "only dangling links exist; restore the skill or delete the dead links",
			})
			continue
		}
		canonSkill := filepath.Join(canonical, skill.Key)
		sourceReal := source.Path
		if source.IsSymlink {
			if r, err := filepath.EvalSymlinks(source.Path); err == nil {
				sourceReal = r
			}
		}

		if _, err := os.Lstat(canonSkill); os.IsNotExist(err) || isDanglingLink(canonSkill) {
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "ensure_canonical_copy", From: sourceReal, To: canonSkill, SkillKey: skill.Key,
			})
		} else {
			same := canonSame(sourceReal, canonSkill)
			if !same {
				plan.Actions = append(plan.Actions, SyncAction{
					Kind: "skip_same", Path: canonSkill, SkillKey: skill.Key,
					Reason: "canonical already present",
				})
			}
		}

		for _, t := range targets {
			link := filepath.Join(t.Path, skill.Key)
			if _, err := os.Lstat(link); err == nil {
				if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
					if tgt, err := os.Readlink(link); err == nil {
						resolved := tgt
						if !filepath.IsAbs(resolved) {
							resolved = filepath.Join(t.Path, tgt)
						}
						if canonSame(resolved, canonSkill) || tgt == canonSkill {
							plan.Actions = append(plan.Actions, SyncAction{
								Kind: "skip_same", Path: link, SkillKey: skill.Key,
								Reason: fmt.Sprintf("already linked for %s", t.Agent),
							})
							continue
						}
						// A dead link holds no content, so replacing it loses nothing. If it
						// already points at the canonical path, that path is restored above.
						if isDanglingLink(link) {
							if filepath.Clean(resolved) == filepath.Clean(canonSkill) {
								plan.Actions = append(plan.Actions, SyncAction{
									Kind: "skip_same", Path: link, SkillKey: skill.Key,
									Reason: fmt.Sprintf("already linked for %s", t.Agent),
								})
							} else {
								plan.Actions = append(plan.Actions, SyncAction{
									Kind: "symlink_skill", Link: link, Target: canonSkill,
									SkillKey: skill.Key, Agent: t.Agent.String(),
								})
							}
							continue
						}
					}
				}
				plan.Actions = append(plan.Actions, SyncAction{
					Kind: "skip_same", Path: link, SkillKey: skill.Key,
					Reason: fmt.Sprintf("path exists for %s (not overwriting; resolve manually)", t.Agent),
				})
				continue
			}
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "symlink_skill", Link: link, Target: canonSkill,
				SkillKey: skill.Key, Agent: t.Agent.String(),
			})
		}
	}
	return plan, nil
}

// HashDangling marks a symlink whose target cannot be resolved. It carries no
// content, so it never counts towards a content mismatch.
const HashDangling = "dangling"

// isDanglingLink reports whether path is a symlink that cannot be resolved
// (missing target or a symlink loop).
func isDanglingLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = os.Stat(path)
	return err != nil
}

// physicalPath resolves symlinks in the parent directory only, so a path can be
// compared even when the final component is a dangling or looping link.
func physicalPath(p string) string {
	dir, base := filepath.Split(filepath.Clean(p))
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	return filepath.Join(dir, base)
}

// rootAliasesCanonical reports whether an agent skills root is the canonical
// skills dir reached through a symlink (e.g. .cursor/skills -> ../.agents/skills).
func rootAliasesCanonical(root, canonical string) bool {
	a, ea := filepath.EvalSymlinks(root)
	b, eb := filepath.EvalSymlinks(canonical)
	return ea == nil && eb == nil && a == b && filepath.Clean(root) != filepath.Clean(canonical)
}

func canonSame(a, b string) bool {
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}

// pickPresence returns the copy to sync from: the canonical one when it has
// content, else the first live copy. Dangling links never qualify.
func pickPresence(p []SkillPresence) *SkillPresence {
	for i := range p {
		if p[i].Agent == "agents" && p[i].ContentHash != HashDangling {
			return &p[i]
		}
	}
	for i := range p {
		if p[i].ContentHash != HashDangling {
			return &p[i]
		}
	}
	return nil
}

func pickMcpPresence(p []McpPresence) *McpPresence {
	for i := range p {
		if p[i].Agent == "agents" {
			return &p[i]
		}
	}
	if len(p) > 0 {
		return &p[0]
	}
	return nil
}

func PlanSyncMcp(scope Scope, cwd string, agentsFilter []AgentKind, onlyKey string) (*SyncPlan, error) {
	mcps := ScanMcp(scope, cwd)
	hub := CanonicalMcpPath(scope, cwd)
	if hub == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	var writers []agentPath
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		writers = append(writers, r)
	}

	plan := &SyncPlan{Scope: scope.String(), DryRun: true, Actions: []SyncAction{}}
	for _, entry := range mcps {
		if onlyKey != "" && entry.Key != NormalizeKey(onlyKey) {
			continue
		}
		if entry.Mismatch {
			var fps []string
			for _, p := range entry.Presence {
				fps = append(fps, p.Fingerprint)
			}
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "conflict_mcp", Server: entry.Key, Fingerprints: fps,
			})
			continue
		}
		source := pickMcpPresence(entry.Presence)
		if source == nil {
			continue
		}
		plan.Actions = append(plan.Actions, SyncAction{
			Kind: "ensure_mcp_hub", Hub: hub, Server: entry.Key, SourceAgent: source.Agent,
		})
		for _, w := range writers {
			if p := findMcpAgent(entry.Presence, w.Agent); p != nil && p.Fingerprint == source.Fingerprint {
				plan.Actions = append(plan.Actions, SyncAction{
					Kind: "skip_mcp_same", Path: w.Path, Server: entry.Key,
					Reason: fmt.Sprintf("already present for %s", w.Agent),
				})
				continue
			}
			plan.Actions = append(plan.Actions, SyncAction{
				Kind: "write_mcp_server", Path: w.Path, Agent: w.Agent.String(), Server: entry.Key,
			})
		}
	}
	return plan, nil
}

func findMcpAgent(p []McpPresence, a AgentKind) *McpPresence {
	for i := range p {
		if p[i].Agent == a.String() {
			return &p[i]
		}
	}
	return nil
}

// ApplyPlan runs every action. A failed action does not stop the others: it is
// logged, later steps for the same skill are skipped (a link to a missing copy
// would replace the agent's real directory), and all failures are returned
// together with the log of what did happen.
func ApplyPlan(plan *SyncPlan, cwd string, scope Scope, decisions *ConflictDecisions) ([]string, error) {
	var log []string
	var failures []error
	failedSkills := map[string]bool{}
	skills := ScanSkills(scope, cwd)
	skillByKey := map[string]*SkillEntry{}
	for i := range skills {
		skillByKey[skills[i].Key] = &skills[i]
	}
	mcps := ScanMcp(scope, cwd)
	mcpByKey := map[string]*McpEntry{}
	for i := range mcps {
		mcpByKey[mcps[i].Key] = &mcps[i]
	}

	skillSource := map[string]string{}
	for _, s := range skills {
		p := pickPresence(s.Presence)
		if p == nil {
			continue
		}
		real := p.Path
		if p.IsSymlink {
			if r, err := filepath.EvalSymlinks(p.Path); err == nil {
				real = r
			}
		}
		skillSource[s.Key] = real
	}
	mcpSource := map[string]*McpNormalized{}
	for _, m := range mcps {
		if p := pickMcpPresence(m.Presence); p != nil {
			mcpSource[m.Key] = p.Normalized
		}
	}

	apply := func(a SyncAction) ([]string, error) {
		var out []string
		switch a.Kind {
		case "conflict_skill":
			if decisions.ForSkill(a.SkillKey) == ConflictSkip {
				return append(out, fmt.Sprintf("conflict skipped (skill): %s", a.SkillKey)), nil
			}
			entry := skillByKey[a.SkillKey]
			if entry == nil {
				return append(out, fmt.Sprintf("conflict skill missing from inventory: %s", a.SkillKey)), nil
			}
			return resolveSkillConflict(scope, cwd, entry, decisions.ForSkill(a.SkillKey))
		case "conflict_mcp":
			if decisions.ForMcp(a.Server) == ConflictSkip {
				return append(out, fmt.Sprintf("conflict skipped (mcp): %s", a.Server)), nil
			}
			entry := mcpByKey[a.Server]
			if entry == nil {
				return append(out, fmt.Sprintf("conflict mcp missing from inventory: %s", a.Server)), nil
			}
			return resolveMcpConflict(scope, cwd, entry, decisions.ForMcp(a.Server))
		case "skip_same", "skip_mcp_same":
			out = append(out, fmt.Sprintf("skip %s: %s", a.Path, a.Reason))
		case "ensure_canonical_copy":
			src := a.From
			if s, ok := skillSource[a.SkillKey]; ok {
				src = s
			}
			if err := replaceDirWithCopy(src, a.To); err != nil {
				return out, err
			}
			out = append(out, fmt.Sprintf("copied %s -> %s", src, a.To))
		case "symlink_skill":
			err := ForceSymlink(a.Link, a.Target, a.Agent, &out)
			return out, err
		case "ensure_mcp_hub":
			norm := mcpSource[a.Server]
			if norm == nil {
				return append(out, fmt.Sprintf("no source mcp for %s", a.Server)), nil
			}
			if err := UpsertMcpJSONHub(a.Hub, a.Server, norm); err != nil {
				return out, err
			}
			out = append(out, fmt.Sprintf("hub upsert %s in %s", a.Server, a.Hub))
		case "write_mcp_server":
			norm := mcpSource[a.Server]
			if norm == nil {
				return out, nil
			}
			if err := WriteMcpToAgent(a.Path, agentByName(a.Agent), a.Server, norm); err != nil {
				return out, err
			}
			out = append(out, fmt.Sprintf("wrote mcp %s -> %s (%s)", a.Server, a.Path, a.Agent))
		}
		return out, nil
	}

	for _, a := range plan.Actions {
		if a.SkillKey != "" && failedSkills[a.SkillKey] {
			log = append(log, fmt.Sprintf("skipped %s for skill %s: an earlier step failed", a.Kind, a.SkillKey))
			continue
		}
		lines, err := apply(a)
		log = append(log, lines...)
		if err != nil {
			subject := a.SkillKey
			if subject == "" {
				subject = a.Server
			}
			if a.SkillKey != "" {
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

func agentByName(name string) AgentKind {
	for _, a := range AllAgents() {
		if a.String() == name {
			return a
		}
	}
	return AgentCursor
}

func pickSkillWinner(entry *SkillEntry, policy ConflictPolicy) *SkillPresence {
	source := &entry.Presence[0]
	for i := range entry.Presence {
		if entry.Presence[i].Agent == "agents" {
			source = &entry.Presence[i]
			break
		}
	}
	switch policy {
	case ConflictKeepSource:
		return source
	case ConflictKeepTarget:
		// First copy whose content differs from the source. A symlink to the
		// canonical copy has the same hash and would silently keep the source.
		for i := range entry.Presence {
			if entry.Presence[i].ContentHash != source.ContentHash {
				return &entry.Presence[i]
			}
		}
		return source
	}
	return nil
}

func pickMcpWinner(entry *McpEntry, policy ConflictPolicy) *McpPresence {
	source := &entry.Presence[0]
	for i := range entry.Presence {
		if entry.Presence[i].Agent == "agents" {
			source = &entry.Presence[i]
			break
		}
	}
	switch policy {
	case ConflictKeepSource:
		return source
	case ConflictKeepTarget:
		for i := range entry.Presence {
			if entry.Presence[i].Fingerprint != source.Fingerprint {
				return &entry.Presence[i]
			}
		}
		return source
	}
	return nil
}

func resolveSkillConflict(scope Scope, cwd string, entry *SkillEntry, policy ConflictPolicy) ([]string, error) {
	var log []string
	winner := pickSkillWinner(entry, policy)
	winnerReal := winner.Path
	if winner.IsSymlink {
		if r, err := filepath.EvalSymlinks(winner.Path); err == nil {
			winnerReal = r
		}
	}
	canonical := CanonicalSkillsDir(scope, cwd)
	if canonical == "" {
		return log, fmt.Errorf("no canonical skills dir")
	}
	canonSkill := filepath.Join(canonical, entry.Key)
	if err := os.MkdirAll(filepath.Dir(canonSkill), 0o755); err != nil {
		return log, err
	}
	winnerIsCanon := canonSame(winnerReal, canonSkill) || winnerReal == canonSkill
	if !winnerIsCanon {
		if err := replaceDirWithCopy(winnerReal, canonSkill); err != nil {
			return log, err
		}
		log = append(log, fmt.Sprintf("conflict skill %s: keep %s → canonical %s", entry.Key, winner.Agent, canonSkill))
	} else {
		log = append(log, fmt.Sprintf("conflict skill %s: keep %s (already canonical)", entry.Key, winner.Agent))
	}
	for _, r := range SkillRoots(scope, cwd) {
		if r.Agent == AgentAgents || rootAliasesCanonical(r.Path, canonical) {
			continue
		}
		link := filepath.Join(r.Path, entry.Key)
		if err := ForceSymlink(link, canonSkill, r.Agent.String(), &log); err != nil {
			return log, err
		}
	}
	return log, nil
}

func resolveMcpConflict(scope Scope, cwd string, entry *McpEntry, policy ConflictPolicy) ([]string, error) {
	var log []string
	winner := pickMcpWinner(entry, policy)
	hub := CanonicalMcpPath(scope, cwd)
	if hub == "" {
		return log, fmt.Errorf("no mcp hub")
	}
	if err := UpsertMcpJSONHub(hub, entry.Key, winner.Normalized); err != nil {
		return log, err
	}
	log = append(log, fmt.Sprintf("conflict mcp %s: keep %s → hub %s", entry.Key, winner.Agent, hub))
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if err := WriteMcpToAgent(r.Path, r.Agent, entry.Key, winner.Normalized); err != nil {
			return log, err
		}
		log = append(log, fmt.Sprintf("conflict mcp %s: wrote %s (%s)", entry.Key, r.Path, r.Agent))
	}
	return log, nil
}

func RemovePath(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || fi.Mode().IsRegular() {
		return os.Remove(path)
	}
	if fi.IsDir() {
		return os.RemoveAll(path)
	}
	return nil
}

func ForceSymlink(link, target, agent string, log *[]string) error {
	if parent := filepath.Dir(link); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	// Never replace the target with a link to itself. This happens when the link's
	// directory is an alias of the target's directory; removing the "old" link would
	// delete the real data and leave a self-referencing symlink.
	if physicalPath(link) == physicalPath(target) {
		*log = append(*log, fmt.Sprintf("skip symlink for %s: %s is the canonical path itself", agent, link))
		return nil
	}
	linkTarget := target
	if rel, ok := pathdiffRelative(link, target); ok {
		linkTarget = rel
	}
	// The old entry is moved aside, not deleted, until the new link exists.
	aside := ""
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if tgt, err := os.Readlink(link); err == nil {
				resolved := tgt
				if !filepath.IsAbs(resolved) {
					resolved = filepath.Join(filepath.Dir(link), tgt)
				}
				if canonSame(resolved, target) || tgt == target {
					*log = append(*log, fmt.Sprintf("skip symlink for %s: %s already linked", agent, link))
					return nil
				}
			}
		}
		aside = fmt.Sprintf("%s.synca-old-%d", link, os.Getpid())
		_ = RemovePath(aside)
		if err := os.Rename(link, aside); err != nil {
			return err
		}
	}
	if err := os.Symlink(linkTarget, link); err != nil {
		if aside != "" {
			_ = os.Rename(aside, link)
		}
		return err
	}
	if aside != "" {
		if err := RemovePath(aside); err != nil {
			*log = append(*log, fmt.Sprintf("warning: could not remove %s: %v", aside, err))
		}
	}
	*log = append(*log, fmt.Sprintf("symlink %s -> %s (%s)", link, linkTarget, agent))
	return nil
}

func pathdiffRelative(link, target string) (string, bool) {
	linkParent := filepath.Dir(link)
	targetC, err := filepath.EvalSymlinks(target)
	if err != nil {
		targetC = target
	}
	linkC, err := filepath.EvalSymlinks(linkParent)
	if err != nil {
		linkC = linkParent
	}
	return pathdiff(linkC, targetC), true
}

func pathdiff(fromDir, to string) string {
	from := splitComps(fromDir)
	toc := splitComps(to)
	i := 0
	for i < len(from) && i < len(toc) && from[i] == toc[i] {
		i++
	}
	var rel []string
	for range from[i:] {
		rel = append(rel, "..")
	}
	rel = append(rel, toc[i:]...)
	if len(rel) == 0 {
		return "."
	}
	return filepath.Join(rel...)
}

func splitComps(p string) []string {
	vol := filepath.VolumeName(p)
	p = strings.TrimPrefix(p, vol)
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

// CopyDirRecursive copies regular files and directories. Any walk or IO error
// is returned; a partial copy is never reported as success.
func CopyDirRecursive(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			rel = p
		}
		dest := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if info.Mode().IsRegular() {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			perm := info.Mode().Perm()
			return os.WriteFile(dest, data, perm)
		}
		return nil
	})
}

// replaceDirWithCopy copies src into a staging directory beside dst, then swaps
// it in. If the copy fails, dst is untouched. The old dst is removed only after
// the swap.
func replaceDirWithCopy(src, dst string) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".synca-stage-*")
	if err != nil {
		return err
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := CopyDirRecursive(src, stage); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	aside := ""
	if _, err := os.Lstat(dst); err == nil {
		aside = stage + ".old"
		if err := os.Rename(dst, aside); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
	}
	if err := os.Rename(stage, dst); err != nil {
		if aside != "" {
			_ = os.Rename(aside, dst)
		}
		_ = os.RemoveAll(stage)
		return err
	}
	if aside != "" {
		_ = RemovePath(aside)
	}
	return nil
}

// ---------- MCP writers ----------

func UpsertMcpJSONHub(hub, server string, norm *McpNormalized) error {
	root, err := readJSONObject(hub)
	if err != nil {
		return err
	}
	servers, err := objectMember(root, "mcpServers", hub)
	if err != nil {
		return err
	}
	obj := NormalizedToJSON(norm)
	mergeEnv(obj, servers[server])
	mergeHeaders(obj, servers[server])
	servers[server] = obj
	return writeJSONPretty(hub, root)
}

// mergeEnv preserves existing env secrets into obj when source lacks them.
func mergeEnv(obj map[string]any, existing any) {
	old, ok := existing.(map[string]any)
	if !ok {
		return
	}
	oldEnv, ok := old["env"].(map[string]any)
	if !ok {
		return
	}
	// Source env may arrive as map[string]string (from McpNormalized) or map[string]any
	// (read back from JSON); accept both so source values are never dropped.
	env := map[string]any{}
	switch cur := obj["env"].(type) {
	case map[string]string:
		for k, v := range cur {
			env[k] = v
		}
	case map[string]any:
		for k, v := range cur {
			env[k] = v
		}
	}
	for k, v := range oldEnv {
		if _, present := env[k]; !present {
			env[k] = v
		}
	}
	if len(env) > 0 {
		obj["env"] = env
	}
}

// mergeHeaders preserves header values already in a config, so a sync that
// only knows header names from another agent never drops a secret.
func mergeHeaders(obj map[string]any, existing any) {
	old, ok := existing.(map[string]any)
	if !ok {
		return
	}
	oldHeaders, ok := old["headers"].(map[string]any)
	if !ok {
		return
	}
	headers := map[string]any{}
	switch cur := obj["headers"].(type) {
	case map[string]string:
		for k, v := range cur {
			headers[k] = v
		}
	case map[string]any:
		for k, v := range cur {
			headers[k] = v
		}
	}
	for k, v := range oldHeaders {
		if _, present := headers[k]; !present {
			headers[k] = v
		}
	}
	if len(headers) > 0 {
		obj["headers"] = headers
	}
}

func WriteMcpToAgent(path string, agent AgentKind, server string, norm *McpNormalized) error {
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	switch agent {
	case AgentGrok:
		return writeGrokToml(path, server, norm)
	case AgentOpenCode:
		return writeOpencode(path, server, norm)
	default:
		return writeMcpServersJSON(path, server, norm)
	}
}

func NormalizedToJSON(norm *McpNormalized) map[string]any {
	obj := map[string]any{}
	obj["type"] = norm.Transport
	if norm.Command != nil && len(norm.Command) > 0 {
		obj["command"] = norm.Command[0]
		args := append([]string{}, norm.Command[1:]...)
		if norm.Args != nil {
			args = append(args, norm.Args...)
		}
		if len(args) > 0 {
			obj["args"] = args
		}
	} else if norm.Args != nil {
		obj["args"] = norm.Args
	}
	if norm.URL != nil {
		obj["url"] = *norm.URL
	}
	if norm.Enabled != nil {
		obj["enabled"] = *norm.Enabled
	}
	if len(norm.Env) > 0 {
		obj["env"] = norm.Env
	}
	if len(norm.Headers) > 0 {
		obj["headers"] = norm.Headers
	}
	return obj
}

// readJSONObject reads a JSON object file. A missing or empty file is an empty
// object. Any other failure is returned, so a caller never overwrites a config
// it could not parse.
func readJSONObject(path string) (map[string]any, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(text)) == "" {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(text, &root); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (comments are not supported); fix it or move it aside: %w", path, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

// objectMember returns root[key] as an object, creating it when absent. A
// member of another type is an error, not something to replace.
func objectMember(root map[string]any, key, path string) (map[string]any, error) {
	v, present := root[key]
	if !present || v == nil {
		m := map[string]any{}
		root[key] = m
		return m, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: %q is not an object", path, key)
	}
	return m, nil
}

func writeJSONPretty(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o644)
}

func writeMcpServersJSON(path, server string, norm *McpNormalized) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers, err := objectMember(root, "mcpServers", path)
	if err != nil {
		return err
	}
	obj := NormalizedToJSON(norm)
	mergeEnv(obj, servers[server])
	mergeHeaders(obj, servers[server])
	servers[server] = obj
	return writeJSONPretty(path, root)
}

func opencodeEntry(norm *McpNormalized) map[string]any {
	obj := map[string]any{}
	if norm.Transport == "stdio" {
		obj["type"] = "local"
		var cmd []string
		if norm.Command != nil {
			cmd = append(cmd, norm.Command...)
		}
		if norm.Args != nil {
			cmd = append(cmd, norm.Args...)
		}
		obj["command"] = cmd
	} else {
		obj["type"] = "remote"
		if norm.URL != nil {
			obj["url"] = *norm.URL
		}
	}
	if norm.Enabled != nil {
		obj["enabled"] = *norm.Enabled
	}
	if len(norm.Env) > 0 {
		obj["environment"] = norm.Env
	}
	if len(norm.Headers) > 0 {
		obj["headers"] = norm.Headers
	}
	return obj
}

func writeOpencode(path, server string, norm *McpNormalized) error {
	root, err := readJSONObject(path)
	if err != nil {
		return err
	}
	mcp, err := objectMember(root, "mcp", path)
	if err != nil {
		return err
	}
	obj := opencodeEntry(norm)
	// preserve env (either key spelling) and headers from the existing entry
	if old, ok := mcp[server].(map[string]any); ok {
		var oldEnv map[string]any
		if e, ok := old["environment"].(map[string]any); ok {
			oldEnv = e
		} else if e, ok := old["env"].(map[string]any); ok {
			oldEnv = e
		}
		if oldEnv != nil {
			env, _ := obj["environment"].(map[string]any)
			if env == nil {
				env = map[string]any{}
				obj["environment"] = env
			}
			for k, v := range oldEnv {
				if _, present := env[k]; !present {
					env[k] = v
				}
			}
		}
		mergeHeaders(obj, old)
	}
	mcp[server] = obj
	return writeJSONPretty(path, root)
}

func writeGrokToml(path, server string, norm *McpNormalized) error {
	root := map[string]any{}
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(text)) != "" {
			if _, err := toml.Decode(string(text), &root); err != nil {
				return fmt.Errorf("%s is not valid TOML; fix it or move it aside: %w", path, err)
			}
		}
	}
	servers, err := objectMember(root, "mcp_servers", path)
	if err != nil {
		return err
	}

	existingEnv := map[string]string{}
	existingHeaders := map[string]string{}
	if old, ok := servers[server].(map[string]any); ok {
		if env, ok := old["env"].(map[string]any); ok {
			for k, v := range env {
				if s, ok := v.(string); ok {
					existingEnv[k] = s
				}
			}
		}
		if headers, ok := old["headers"].(map[string]any); ok {
			for k, v := range headers {
				if s, ok := v.(string); ok {
					existingHeaders[k] = s
				}
			}
		}
	}

	tbl := map[string]any{"type": norm.Transport}
	if norm.Command != nil && len(norm.Command) > 0 {
		tbl["command"] = norm.Command[0]
		args := append([]string{}, norm.Command[1:]...)
		if norm.Args != nil {
			args = append(args, norm.Args...)
		}
		if len(args) > 0 {
			tbl["args"] = args
		}
	} else if norm.Args != nil {
		tbl["args"] = norm.Args
	}
	if norm.URL != nil {
		tbl["url"] = *norm.URL
	}
	if norm.Enabled != nil {
		tbl["enabled"] = *norm.Enabled
	}
	envTbl := map[string]any{}
	for k, v := range existingEnv {
		envTbl[k] = v
	}
	for k, v := range norm.Env {
		if _, present := envTbl[k]; !present {
			envTbl[k] = v
		}
	}
	if len(envTbl) > 0 {
		tbl["env"] = envTbl
	}
	headerTbl := map[string]any{}
	for k, v := range existingHeaders {
		headerTbl[k] = v
	}
	for k, v := range norm.Headers {
		if _, present := headerTbl[k]; !present {
			headerTbl[k] = v
		}
	}
	if len(headerTbl) > 0 {
		tbl["headers"] = headerTbl
	}
	servers[server] = tbl

	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(root); err != nil {
		return err
	}
	return atomicWriteFile(path, []byte(sb.String()), 0o644)
}

func MergePlans(a, b *SyncPlan) *SyncPlan {
	if a.Scope != b.Scope {
		return a
	}
	a.Actions = append(a.Actions, b.Actions...)
	a.DryRun = a.DryRun && b.DryRun
	return a
}
