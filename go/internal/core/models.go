// Package core scans, syncs and manages skills + MCP configs across coding agents:
// models, paths, agents, discover, scan, sync, pi skills, manage, update.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------- Scope ----------

type Scope int

const (
	ScopeUser Scope = iota
	ScopeProject
)

func (s Scope) String() string {
	if s == ScopeProject {
		return "project"
	}
	return "user"
}

func (s Scope) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// ---------- AgentKind ----------

type AgentKind int

const (
	AgentAgents AgentKind = iota
	AgentGrok
	AgentDevin
	AgentOmp
	AgentPi
	AgentKiro
	AgentOpenCode
	AgentClaude
	AgentCursor
)

var agentNames = [...]string{
	"agents", "grok", "devin", "omp",
	"pi", "kiro", "opencode", "claude", "cursor",
}

func (a AgentKind) String() string { return agentNames[a] }

// AllAgents in the canonical enum order (used for presence sort).
func AllAgents() []AgentKind {
	return []AgentKind{
		AgentAgents, AgentGrok, AgentDevin, AgentOmp,
		AgentPi, AgentKiro, AgentOpenCode, AgentClaude, AgentCursor,
	}
}

func ParseAgentList(s string) []AgentKind {
	var out []AgentKind
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		for _, a := range AllAgents() {
			if a.String() == p {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

func agentIn(a AgentKind, filter []AgentKind) bool {
	for _, f := range filter {
		if f == a {
			return true
		}
	}
	return false
}

// ---------- Models (JSON shapes must match serde output) ----------

type SkillPresence struct {
	Agent         string  `json:"agent"`
	Path          string  `json:"path"`
	IsSymlink     bool    `json:"is_symlink"`
	SymlinkTarget *string `json:"symlink_target"`
	ContentHash   string  `json:"content_hash"`
}

type SkillEntry struct {
	Key         string          `json:"key"`
	DisplayName string          `json:"display_name"`
	Description *string         `json:"description"`
	Scope       string          `json:"scope"`
	Presence    []SkillPresence `json:"presence"`
	Mismatch    bool            `json:"mismatch"`
}

type McpNormalized struct {
	Transport string            `json:"transport"`
	Command   []string          `json:"command"`
	URL       *string           `json:"url"`
	Args      []string          `json:"args"`
	Enabled   *bool             `json:"enabled"`
	EnvKeys   []string          `json:"env_keys"`
	Env       map[string]string `json:"env,omitempty"`
}

type McpPresence struct {
	Agent       string         `json:"agent"`
	Path        string         `json:"path"`
	Normalized  *McpNormalized `json:"normalized"`
	Fingerprint string         `json:"fingerprint"`
}

type McpEntry struct {
	Key      string        `json:"key"`
	Scope    string        `json:"scope"`
	Presence []McpPresence `json:"presence"`
	Mismatch bool          `json:"mismatch"`
}

type Inventory struct {
	Scope       string       `json:"scope"`
	ProjectRoot *string      `json:"project_root"`
	Skills      []SkillEntry `json:"skills"`
	Mcps        []McpEntry   `json:"mcps"`
}

func NormalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	return strings.ReplaceAll(s, " ", "-")
}

// ---------- ConflictPolicy ----------

type ConflictPolicy int

const (
	ConflictSkip ConflictPolicy = iota
	ConflictKeepSource
	ConflictKeepTarget
)

func (p ConflictPolicy) String() string {
	switch p {
	case ConflictKeepSource:
		return "keep-source"
	case ConflictKeepTarget:
		return "keep-target"
	}
	return "skip"
}

func ParseConflictPolicy(s string) (ConflictPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skip":
		return ConflictSkip, true
	case "keep-source", "keep_source", "source", "a":
		return ConflictKeepSource, true
	case "keep-target", "keep_target", "target", "b":
		return ConflictKeepTarget, true
	}
	return 0, false
}

type ConflictDecisions struct {
	Default ConflictPolicy
	Skills  map[string]ConflictPolicy
	Mcps    map[string]ConflictPolicy
}

func NewDecisions(def ConflictPolicy) *ConflictDecisions {
	return &ConflictDecisions{
		Default: def,
		Skills:  map[string]ConflictPolicy{},
		Mcps:    map[string]ConflictPolicy{},
	}
}

func (d *ConflictDecisions) ForSkill(key string) ConflictPolicy {
	if p, ok := d.Skills[key]; ok {
		return p
	}
	return d.Default
}

func (d *ConflictDecisions) ForMcp(key string) ConflictPolicy {
	if p, ok := d.Mcps[key]; ok {
		return p
	}
	return d.Default
}

// ---------- paths ----------

func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "/"
	}
	return h
}

// ProjectRoot walks up from cwd looking for `.git`.
func ProjectRoot(cwd string) string {
	cur, err := filepath.Abs(cwd)
	if err != nil {
		cur = cwd
	}
	if r, err := filepath.EvalSymlinks(cur); err == nil {
		cur = r
	}
	for {
		if st, err := os.Stat(filepath.Join(cur, ".git")); err == nil && st != nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return HashBytes(data), nil
}

func HashSkillDir(dir string) (string, error) {
	md := filepath.Join(dir, "SKILL.md")
	if st, err := os.Stat(md); err == nil && !st.IsDir() {
		return HashFile(md)
	}
	var parts [][2]any // [rel, bytes]
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			rel, e := filepath.Rel(dir, p)
			if e != nil {
				rel = p
			}
			if data, err := os.ReadFile(p); err == nil {
				parts = append(parts, [2]any{filepath.ToSlash(rel), data})
			}
			return nil
		})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i][0].(string) < parts[j][0].(string) })
	var buf []byte
	for _, p := range parts {
		buf = append(buf, p[0].(string)...)
		buf = append(buf, 0)
		buf = append(buf, p[1].([]byte)...)
		buf = append(buf, 0)
	}
	return HashBytes(buf), nil
}
