package core

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// providers.json is the agent table of the skills CLI (npx skills, version in
// its "source" field): where each of the agents it supports keeps skills.
//
//go:embed providers.json
var providersJSON []byte

type providerRow struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ProjectDir *string `json:"project_dir"`
	GlobalDir  *string `json:"global_dir"`
	Universal  bool    `json:"universal"`
	SyncaAgent *string `json:"synca_agent"`
}

type providerTable struct {
	Source string        `json:"source"`
	Agents []providerRow `json:"agents"`
}

// ProviderInfo is one skills.sh agent in one scope.
type ProviderInfo struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ProjectDir *string `json:"project_dir"`
	GlobalDir  *string `json:"global_dir"`
	Path       *string `json:"path"`
	Universal  bool    `json:"universal"`
	Managed    bool    `json:"managed"`
	SyncaAgent *string `json:"synca_agent"`
	Present    bool    `json:"present"`
	Skills     int     `json:"skills"`
}

// ProvidersDoc is the `providers list` document.
type ProvidersDoc struct {
	Source    string         `json:"source"`
	Scope     string         `json:"scope"`
	Total     int            `json:"total"`
	Providers []ProviderInfo `json:"providers"`
}

// ListProviders lists every agent the skills CLI supports with the directory
// it reads in this scope (for agents synca manages, the directory synca writes). managed marks the agents synca syncs to.
func ListProviders(scope Scope, cwd string) (*ProvidersDoc, error) {
	var table providerTable
	if err := json.Unmarshal(providersJSON, &table); err != nil {
		return nil, err
	}
	root := ""
	if scope == ScopeProject {
		root = ProjectRoot(cwd)
	}
	home := HomeDir()
	// For an agent synca syncs to, path is the directory synca writes, which can
	// differ from the skills CLI's: in a project the skills CLI shares
	// .agents/skills with OpenCode and Cursor, synca links into their own dirs.
	syncaDir := map[string]string{}
	for _, r := range SkillRoots(scope, cwd) {
		syncaDir[r.Agent.String()] = r.Path
	}
	doc := &ProvidersDoc{Source: table.Source, Scope: scope.String(), Providers: []ProviderInfo{}}
	for _, row := range table.Agents {
		info := ProviderInfo{
			ID: row.ID, Name: row.Name, ProjectDir: row.ProjectDir, GlobalDir: row.GlobalDir,
			Universal: row.Universal, SyncaAgent: row.SyncaAgent, Managed: row.SyncaAgent != nil,
		}
		var dir string
		switch scope {
		case ScopeUser:
			if row.GlobalDir != nil {
				dir = expandHome(*row.GlobalDir, home)
			}
		default:
			if row.ProjectDir != nil && root != "" {
				dir = filepath.Join(root, filepath.FromSlash(*row.ProjectDir))
			}
		}
		if row.SyncaAgent != nil {
			if d, ok := syncaDir[*row.SyncaAgent]; ok {
				dir = d
			}
		}
		if dir != "" {
			info.Path = &dir
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				info.Present = true
				info.Skills = countSkillDirs(dir)
			}
		}
		doc.Providers = append(doc.Providers, info)
	}
	sort.SliceStable(doc.Providers, func(i, j int) bool {
		return strings.ToLower(doc.Providers[i].Name) < strings.ToLower(doc.Providers[j].Name)
	})
	doc.Total = len(doc.Providers)
	return doc, nil
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// countSkillDirs counts entries of dir that are, or link to, a folder with a
// SKILL.md. Synca's own staging leftovers are not skills.
func countSkillDirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if isSyncaTemp(e.Name()) {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil && !st.IsDir() {
			n++
		}
	}
	return n
}
