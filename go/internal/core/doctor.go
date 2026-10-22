package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DriftIssue is an agent skill copy that is a real directory and whose
// content differs from the canonical skill.
type DriftIssue struct {
	Agent     string `json:"agent"`
	Skill     string `json:"skill"`
	Path      string `json:"path"`
	Canonical string `json:"canonical"`
	Differs   bool   `json:"differs"`
}

// LinkIssue is a dangling symlink or a canonical skill with no link in an
// agent directory that exists.
type LinkIssue struct {
	Agent  string `json:"agent"`
	Skill  string `json:"skill"`
	Path   string `json:"path"`
	Target string `json:"target,omitempty"`
}

// McpIssue is an MCP server that differs between the hub and an agent config.
type McpIssue struct {
	Agent  string `json:"agent"`
	Server string `json:"server"`
	Path   string `json:"path"`
	Hub    string `json:"hub,omitempty"`
	Reason string `json:"reason"`
}

// DoctorReport is the read-only result of Doctor. Empty slices are empty, not null.
type DoctorReport struct {
	OK       bool         `json:"ok"`
	Scope    string       `json:"scope"`
	Drift    []DriftIssue `json:"drift"`
	Dangling []LinkIssue  `json:"dangling"`
	Missing  []LinkIssue  `json:"missing"`
	MCP      []McpIssue   `json:"mcp"`
}

// Doctor compares canonical skills and the MCP hub with each agent.
// It does not write. Pi and OMP directories are not inspected.
func Doctor(scope Scope, cwd string) (*DoctorReport, error) {
	if scope == ScopeProject && ProjectRoot(cwd) == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	report := &DoctorReport{
		Scope:    scope.String(),
		Drift:    []DriftIssue{},
		Dangling: []LinkIssue{},
		Missing:  []LinkIssue{},
		MCP:      []McpIssue{},
	}
	canonical := CanonicalSkillsDir(scope, cwd)
	skills := ScanSkills(scope, cwd)
	canonByKey := map[string]SkillPresence{}
	for _, skill := range skills {
		for _, p := range skill.Presence {
			if p.Agent == AgentAgents.String() {
				canonByKey[skill.Key] = p
			}
		}
	}
	for _, skill := range skills {
		canon, hasCanon := canonByKey[skill.Key]
		for _, p := range skill.Presence {
			if p.ContentHash == HashDangling || (p.IsSymlink && isDanglingLink(p.Path)) {
				report.Dangling = append(report.Dangling, LinkIssue{
					Agent:  p.Agent,
					Skill:  skill.Key,
					Path:   p.Path,
					Target: linkTarget(p),
				})
				continue
			}
			if p.Agent == AgentAgents.String() || p.IsSymlink || !hasCanon {
				continue
			}
			if canon.ContentHash == HashDangling || canon.ContentHash == "" || canon.ContentHash == "missing" {
				continue
			}
			if p.ContentHash != canon.ContentHash {
				report.Drift = append(report.Drift, DriftIssue{
					Agent:     p.Agent,
					Skill:     skill.Key,
					Path:      p.Path,
					Canonical: canon.Path,
					Differs:   true,
				})
			}
		}
	}
	for _, skill := range skills {
		canon, ok := canonByKey[skill.Key]
		if !ok || canon.ContentHash == HashDangling || canon.ContentHash == "missing" {
			continue
		}
		present := map[string]bool{}
		for _, p := range skill.Presence {
			present[p.Agent] = true
		}
		for _, root := range SkillRoots(scope, cwd) {
			if root.Agent == AgentAgents || present[root.Agent.String()] {
				continue
			}
			if rootAliasesCanonical(root.Path, canonical) {
				continue
			}
			st, err := os.Stat(root.Path)
			if err != nil || !st.IsDir() {
				continue
			}
			report.Missing = append(report.Missing, LinkIssue{
				Agent: root.Agent.String(),
				Skill: skill.Key,
				Path:  filepath.Join(root.Path, skill.Key),
			})
		}
	}
	report.MCP = doctorMCP(scope, cwd)
	sort.Slice(report.Drift, func(i, j int) bool {
		if report.Drift[i].Agent != report.Drift[j].Agent {
			return report.Drift[i].Agent < report.Drift[j].Agent
		}
		return report.Drift[i].Skill < report.Drift[j].Skill
	})
	sort.Slice(report.Dangling, func(i, j int) bool {
		if report.Dangling[i].Agent != report.Dangling[j].Agent {
			return report.Dangling[i].Agent < report.Dangling[j].Agent
		}
		return report.Dangling[i].Path < report.Dangling[j].Path
	})
	sort.Slice(report.Missing, func(i, j int) bool {
		if report.Missing[i].Agent != report.Missing[j].Agent {
			return report.Missing[i].Agent < report.Missing[j].Agent
		}
		return report.Missing[i].Skill < report.Missing[j].Skill
	})
	sort.Slice(report.MCP, func(i, j int) bool {
		if report.MCP[i].Agent != report.MCP[j].Agent {
			return report.MCP[i].Agent < report.MCP[j].Agent
		}
		if report.MCP[i].Server != report.MCP[j].Server {
			return report.MCP[i].Server < report.MCP[j].Server
		}
		return report.MCP[i].Reason < report.MCP[j].Reason
	})
	report.OK = len(report.Drift) == 0 && len(report.Dangling) == 0 && len(report.Missing) == 0 && len(report.MCP) == 0
	return report, nil
}

func linkTarget(p SkillPresence) string {
	if p.SymlinkTarget == nil {
		return ""
	}
	return *p.SymlinkTarget
}

func doctorMCP(scope Scope, cwd string) []McpIssue {
	var issues []McpIssue
	hubPath := CanonicalMcpPath(scope, cwd)
	hub := map[string]McpNormalized{}
	if st, err := os.Stat(hubPath); err == nil && !st.IsDir() {
		if servers, err := ReadMcpServers(AgentAgents, hubPath); err == nil {
			hub = servers
		} else {
			issues = append(issues, McpIssue{
				Agent: AgentAgents.String(), Server: "", Path: hubPath, Reason: "unreadable",
			})
		}
	}
	for _, root := range McpConfigPaths(scope, cwd) {
		if root.Agent == AgentAgents {
			continue
		}
		st, err := os.Stat(root.Path)
		if err != nil || st.IsDir() {
			continue
		}
		servers, err := ReadMcpServers(root.Agent, root.Path)
		if err != nil {
			issues = append(issues, McpIssue{
				Agent: root.Agent.String(), Path: root.Path, Hub: hubPath, Reason: "unreadable",
			})
			continue
		}
		seen := map[string]bool{}
		for name, norm := range servers {
			seen[name] = true
			h, ok := hub[name]
			if !ok {
				issues = append(issues, McpIssue{
					Agent: root.Agent.String(), Server: name, Path: root.Path, Hub: hubPath, Reason: "only in agent",
				})
				continue
			}
			if McpFingerprint(&norm) != McpFingerprint(&h) {
				issues = append(issues, McpIssue{
					Agent: root.Agent.String(), Server: name, Path: root.Path, Hub: hubPath, Reason: "differs",
				})
			}
		}
		for name := range hub {
			if seen[name] {
				continue
			}
			issues = append(issues, McpIssue{
				Agent: root.Agent.String(), Server: name, Path: root.Path, Hub: hubPath, Reason: "missing from agent",
			})
		}
	}
	if issues == nil {
		return []McpIssue{}
	}
	return issues
}
