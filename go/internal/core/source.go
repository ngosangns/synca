package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const cloneTimeout = 3 * time.Minute

// SourceSpec is a parsed skill source.
type SourceSpec struct {
	// Local is true for a folder on disk. Path is then set.
	Local bool
	Path  string
	// URL is the Git URL to clone. Source is the identifier the lock file keeps
	// ("owner/repo" for GitHub, the URL otherwise). Type is "github" or "git".
	URL    string
	Source string
	Type   string
	Ref    string
	// Skill selects one skill inside the repository. It is a name or a folder.
	Skill string
}

var ownerRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)
var segRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ParseSource accepts a local folder, owner/repo, owner/repo@skill, a Git URL,
// or a https://skills.sh/<owner>/<repo>[/<skill>] URL.
func ParseSource(src string) (SourceSpec, error) {
	s := strings.TrimSpace(src)
	if s == "" {
		return SourceSpec{}, fmt.Errorf("empty skill source")
	}
	if st, err := os.Stat(s); err == nil && st.IsDir() {
		return SourceSpec{Local: true, Path: s}, nil
	}
	if strings.HasPrefix(s, "-") {
		return SourceSpec{}, fmt.Errorf("invalid skill source %q", s)
	}
	if strings.HasPrefix(s, "https://skills.sh/") || strings.HasPrefix(s, "http://skills.sh/") ||
		strings.HasPrefix(s, "https://www.skills.sh/") {
		path := s[strings.Index(s, "skills.sh/")+len("skills.sh/"):]
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) > 0 && parts[0] == "p" {
			return SourceSpec{}, fmt.Errorf("invalid skill source: skills.sh packs are not supported")
		}
		if len(parts) < 2 || len(parts) > 3 || !segRe.MatchString(parts[0]) || !segRe.MatchString(parts[1]) {
			return SourceSpec{}, fmt.Errorf("invalid skill source %q (use https://skills.sh/owner/repo/skill)", s)
		}
		spec := githubSpec(parts[0], parts[1])
		if len(parts) == 3 {
			if !segRe.MatchString(parts[2]) {
				return SourceSpec{}, fmt.Errorf("invalid skill source %q", s)
			}
			spec.Skill = parts[2]
		}
		return spec, nil
	}
	if !strings.Contains(s, "://") && !strings.HasPrefix(s, "git@") {
		repo, skill, hasSkill := strings.Cut(s, "@")
		if hasSkill && ownerRepoRe.MatchString(repo) && segRe.MatchString(skill) {
			parts := strings.SplitN(repo, "/", 2)
			spec := githubSpec(parts[0], parts[1])
			spec.Skill = skill
			return spec, nil
		}
		if !hasSkill && ownerRepoRe.MatchString(s) && !strings.HasSuffix(s, ".git") {
			parts := strings.SplitN(s, "/", 2)
			return githubSpec(parts[0], parts[1]), nil
		}
	}
	if strings.HasPrefix(s, "git@") || strings.HasSuffix(s, ".git") || strings.Contains(s, "://") {
		if owner, repo, ok := githubURL(s); ok {
			return githubSpec(owner, repo), nil
		}
		return SourceSpec{URL: s, Source: s, Type: "git"}, nil
	}
	return SourceSpec{Local: true, Path: s}, nil
}

func githubSpec(owner, repo string) SourceSpec {
	repo = strings.TrimSuffix(repo, ".git")
	return SourceSpec{
		URL:    "https://github.com/" + owner + "/" + repo + ".git",
		Source: owner + "/" + repo,
		Type:   "github",
	}
}

// githubURL reports owner and repo for https://github.com/o/r[.git] and
// git@github.com:o/r[.git].
func githubURL(s string) (string, string, bool) {
	var path string
	switch {
	case strings.HasPrefix(s, "git@github.com:"):
		path = strings.TrimPrefix(s, "git@github.com:")
	case strings.HasPrefix(s, "https://github.com/"):
		path = strings.TrimPrefix(s, "https://github.com/")
	default:
		return "", "", false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || !segRe.MatchString(parts[0]) || !segRe.MatchString(strings.TrimSuffix(parts[1], ".git")) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Resolved is a skill directory ready to copy, with where it came from.
type Resolved struct {
	Dir     string
	Cleanup func()
	// Origin is nil for a local folder.
	Origin *LockEntry
}

// ResolveSkillSource resolves a source to one skill directory. A remote
// source is cloned into a temporary directory that Cleanup removes.
func ResolveSkillSource(src string) (*Resolved, error) {
	spec, err := ParseSource(src)
	if err != nil {
		return nil, err
	}
	if spec.Local {
		path := spec.Path
		if !filepath.IsAbs(path) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			path = filepath.Join(cwd, path)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("skill source not found: %s", path)
		}
		dir, err := findSkillDir(path)
		if err != nil {
			return nil, err
		}
		return &Resolved{Dir: dir, Cleanup: func() {}}, nil
	}
	if spec.Type == "github" && spec.Ref == "" && !registryOff() {
		if owner, repo, ok := githubOwnerRepo(spec.Source); ok {
			if res, err := fetchGitHub(owner, repo, SkillSlug(spec.Skill)); err == nil {
				return res, nil
			}
		}
	}
	repo, err := CloneRepo(spec.URL, spec.Ref)
	if err != nil {
		return nil, fmt.Errorf("git clone failed for %s", src)
	}
	dir, err := repo.pick(spec.Skill)
	if err != nil {
		repo.Cleanup()
		return nil, err
	}
	return &Resolved{Dir: dir, Cleanup: repo.Cleanup, Origin: repo.originOf(spec, dir)}, nil
}

// Repo is a shallow clone in a temporary directory.
type Repo struct {
	Root    string
	Cleanup func()
}

// CloneRepo shallow-clones url. A non-empty ref is a branch or tag.
func CloneRepo(url, ref string) (*Repo, error) {
	if strings.HasPrefix(url, "-") {
		return nil, fmt.Errorf("invalid Git URL")
	}
	tmp, err := os.MkdirTemp("", "synca-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	dest := filepath.Join(tmp, "repo")
	args := []string{"clone", "--depth", "1", "--quiet"}
	if ref != "" {
		if strings.HasPrefix(ref, "-") {
			cleanup()
			return nil, fmt.Errorf("invalid ref")
		}
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dest)
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		cleanup()
		return nil, err
	}
	return &Repo{Root: dest, Cleanup: cleanup}, nil
}

// DiscoveredSkill is one skill found in a tree.
type DiscoveredSkill struct {
	Name        string
	Description string
	// Path is the folder relative to the root, slash separated. "" is the root.
	Path string
	Slug string
	dir  string
}

// SkillSlug lowercases a name and keeps letters, digits and dashes.
func SkillSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || r == ' ':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

const discoverDepth = 6

// DiscoverSkills lists every folder under root that holds a SKILL.md. It does
// not descend into a skill folder, .git or node_modules.
func DiscoverSkills(root string) ([]DiscoveredSkill, error) {
	var out []DiscoveredSkill
	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		md := filepath.Join(dir, "SKILL.md")
		if st, err := os.Stat(md); err == nil && !st.IsDir() {
			rel, _ := filepath.Rel(root, dir)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				rel = ""
			}
			name := SkillDisplayName(dir)
			desc := ""
			if d := skillDescription(dir); d != nil {
				desc = *d
			}
			out = append(out, DiscoveredSkill{Name: name, Description: desc, Path: rel, Slug: SkillSlug(name), dir: dir})
			return nil
		}
		if depth >= discoverDepth {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == ".git" || e.Name() == "node_modules" {
				continue
			}
			if err := walk(filepath.Join(dir, e.Name()), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, 0); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Find returns the skill whose slug or folder matches want.
func findDiscovered(skills []DiscoveredSkill, want string) *DiscoveredSkill {
	slug := SkillSlug(want)
	var byFolder, bySlug []*DiscoveredSkill
	for i := range skills {
		s := &skills[i]
		if SkillSlug(filepath.Base(s.Path)) == slug && s.Path != "" {
			byFolder = append(byFolder, s)
		}
		if s.Slug == slug {
			bySlug = append(bySlug, s)
		}
	}
	for _, list := range [][]*DiscoveredSkill{bySlug, byFolder} {
		if len(list) > 0 {
			best := list[0]
			for _, s := range list[1:] {
				if len(s.Path) < len(best.Path) {
					best = s
				}
			}
			return best
		}
	}
	return nil
}

// pick returns the skill directory a spec names, or the only skill in the repo.
func (r *Repo) pick(want string) (string, error) {
	if want == "" {
		return findSkillDir(r.Root)
	}
	skills, err := DiscoverSkills(r.Root)
	if err != nil {
		return "", err
	}
	if s := findDiscovered(skills, want); s != nil {
		return s.dir, nil
	}
	return "", fmt.Errorf("skill %s not found in remote repository", want)
}

// originOf builds the lock entry for a skill directory inside this clone.
func (r *Repo) originOf(spec SourceSpec, dir string) *LockEntry {
	rel, err := filepath.Rel(r.Root, dir)
	if err != nil || rel == "." {
		rel = ""
	}
	rel = filepath.ToSlash(rel)
	e := &LockEntry{
		Source:     spec.Source,
		SourceType: spec.Type,
		SourceURL:  spec.URL,
		Ref:        spec.Ref,
		SkillPath:  strings.TrimPrefix(rel+"/SKILL.md", "/"),
	}
	e.FolderHash = r.treeHash(rel)
	return e
}

// treeHash is the Git tree id of folder at HEAD, which is the value the skills
// CLI keeps as skillFolderHash.
func (r *Repo) treeHash(folder string) string {
	rev := "HEAD^{tree}"
	if folder != "" {
		rev = "HEAD:" + folder
	}
	out, err := exec.Command("git", "-C", r.Root, "rev-parse", "--verify", "--end-of-options", rev).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
			var nestedErr error
			for _, nested := range []string{".agents/skills", "skills"} {
				dir := filepath.Join(path, nested)
				if st, err := os.Stat(dir); err == nil && st.IsDir() {
					found, err := findSkillDir(dir)
					if err == nil {
						return found, nil
					}
					if nestedErr == nil && strings.HasPrefix(err.Error(), "multiple skills") {
						nestedErr = err
					}
				}
			}
			if nestedErr != nil {
				return "", nestedErr
			}
			return "", fmt.Errorf("no SKILL.md under %s", path)
		}
		return "", fmt.Errorf("multiple skills under %s; point at a specific skill folder or use owner/repo@skill", path)
	}
	return "", fmt.Errorf("not a skill directory: %s", path)
}
