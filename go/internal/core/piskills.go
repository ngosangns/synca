package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxNameLen = 64

func PiSkillNameOK(name string) bool {
	if name == "" || len(name) > maxNameLen {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, b := range []byte(name) {
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-') {
			return false
		}
	}
	return true
}

func SlugifySkillName(name string) string {
	var out []rune
	prevHyphen := false
	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			out = append(out, c)
			prevHyphen = false
		} else if !prevHyphen && len(out) > 0 {
			out = append(out, '-')
			prevHyphen = true
		}
	}
	s := strings.TrimRight(string(out), "-")
	if len(s) > maxNameLen {
		s = s[:maxNameLen]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		return "skill"
	}
	return s
}

func yamlDoubleQuote(v string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range v {
		switch c {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			sb.WriteRune(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func splitTopKey(line string) (string, string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", "", false
	}
	key, value, found := strings.Cut(line, ":")
	if !found || key == "" {
		return "", "", false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", "", false
		}
	}
	return key, strings.TrimSpace(value), true
}

func plainScalarNeedsQuote(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '"', '\'', '|', '>', '[', '{', '*', '&', '!':
		return false
	}
	return strings.Contains(v, ":")
}

func unquoteScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// RepairSkillFrontmatter rewrites frontmatter so Pi accepts it; "" when safe.
func RepairSkillFrontmatter(text, folderName string) string {
	fm, ok := frontmatterBlock(text)
	if !ok {
		return ""
	}
	fmBody := strings.TrimLeft(fm, "\n")
	if fmBody == "" {
		return ""
	}
	lines := strings.Split(fmBody, "\n")
	changed := false
	folderOK := PiSkillNameOK(folderName)

	for i, line := range lines {
		key, value, ok := splitTopKey(line)
		if !ok || key != "name" {
			continue
		}
		current := unquoteScalar(value)
		if PiSkillNameOK(current) {
			continue
		}
		replacement := current
		if folderOK {
			replacement = folderName
		} else {
			replacement = SlugifySkillName(current)
		}
		if !PiSkillNameOK(replacement) || replacement == current {
			continue
		}
		lines[i] = "name: " + replacement
		changed = true
	}

	for i, line := range lines {
		key, value, ok := splitTopKey(line)
		if !ok || key == "name" || !plainScalarNeedsQuote(value) {
			continue
		}
		lines[i] = key + ": " + yamlDoubleQuote(value)
		changed = true
	}

	if !changed {
		return ""
	}
	rest := text[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	after := rest[end:]
	return "---\n" + strings.Join(lines, "\n") + after
}

func effectiveName(text, folderName string) string {
	rendered := text
	if r := RepairSkillFrontmatter(text, folderName); r != "" {
		rendered = r
	}
	fm, _ := frontmatterBlock(rendered)
	for _, line := range strings.Split(fm, "\n") {
		if key, value, ok := splitTopKey(line); ok && key == "name" {
			if n := unquoteScalar(value); n != "" {
				return n
			}
		}
	}
	return folderName
}

// TreeHash hashes a skill tree (files + symlink targets), sorted by rel path.
func TreeHash(dir string) (string, error) {
	var parts [][2]any
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			rel, e := filepath.Rel(dir, p)
			if e != nil {
				rel = p
			}
			rel = filepath.ToSlash(rel)
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(p)
				parts = append(parts, [2]any{rel, []byte(target)})
			} else if info.Mode().IsRegular() {
				if data, err := os.ReadFile(p); err == nil {
					parts = append(parts, [2]any{rel, data})
				}
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

type skillDir struct {
	path   string
	folder string
	name   string
	hash   string
}

func readSkillDir(path string) *skillDir {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return nil
	}
	md := filepath.Join(path, "SKILL.md")
	if st, err := os.Stat(md); err != nil || st.IsDir() {
		return nil
	}
	folder := filepath.Base(path)
	text, err := os.ReadFile(md)
	if err != nil {
		return nil
	}
	hash, err := TreeHash(path)
	if err != nil {
		return nil
	}
	return &skillDir{path: path, folder: folder, name: effectiveName(string(text), folder), hash: hash}
}

func listRealSkillDirs(root string) []*skillDir {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []*skillDir
	for _, e := range entries {
		if info := readSkillDir(filepath.Join(root, e.Name())); info != nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].folder < out[j].folder })
	return out
}

func samePath(a, b string) bool {
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	if ea == nil && eb == nil {
		return ra == rb
	}
	return a == b
}

func pickKeeper(dirs []*skillDir) *skillDir {
	for _, d := range dirs {
		if d.folder == d.name {
			return d
		}
	}
	key := NormalizeKey(dirs[0].name)
	for _, d := range dirs {
		if d.folder == key {
			return d
		}
	}
	return dirs[0]
}

// PlanPiCompat mirrors pi_skills.rs::plan_pi_compat.
func PlanPiCompat(scope Scope, cwd, canonical, onlyKey string) []SyncAction {
	if st, err := os.Stat(canonical); err != nil || !st.IsDir() {
		return nil
	}
	var actions []SyncAction
	seenMd := map[string]bool{}

	for _, info := range listRealSkillDirs(canonical) {
		md := filepath.Join(info.path, "SKILL.md")
		realMd, err := filepath.EvalSymlinks(md)
		if err != nil {
			continue
		}
		if seenMd[realMd] {
			continue
		}
		seenMd[realMd] = true
		text, err := os.ReadFile(md)
		if err != nil {
			continue
		}
		if RepairSkillFrontmatter(string(text), info.folder) == "" {
			continue
		}
		if onlyKey != "" && NormalizeKey(onlyKey) != NormalizeKey(info.name) &&
			onlyKey != info.folder && onlyKey != info.name {
			continue
		}
		actions = append(actions, SyncAction{
			Kind: "repair_skill_frontmatter", Path: md, SkillKey: info.name,
		})
	}

	dirs := listRealSkillDirs(canonical)
	byName := map[string][]*skillDir{}
	for _, info := range dirs {
		byName[info.name] = append(byName[info.name], info)
	}
	for _, name := range sortedKeys(byName) {
		group := byName[name]
		if onlyKey != "" && NormalizeKey(onlyKey) != NormalizeKey(name) && onlyKey != name {
			continue
		}
		byHash := map[string][]*skillDir{}
		for _, info := range group {
			byHash[info.hash] = append(byHash[info.hash], info)
		}
		for _, same := range byHash {
			if len(same) < 2 {
				continue
			}
			keeper := pickKeeper(same)
			for _, extra := range same {
				if samePath(extra.path, keeper.path) {
					continue
				}
				actions = append(actions, SyncAction{
					Kind: "collapse_skill_alias", From: extra.path, To: keeper.path, SkillKey: name,
				})
			}
		}
	}

	if scope == ScopeUser {
		actions = append(actions, planProjectShadows(cwd, canonical, onlyKey)...)
	}
	return actions
}

func planProjectShadows(cwd, userRoot, onlyKey string) []SyncAction {
	proj := ProjectRoot(cwd)
	if proj == "" {
		return nil
	}
	projSkills := filepath.Join(proj, ".agents/skills")
	if st, err := os.Stat(projSkills); err != nil || !st.IsDir() || samePath(projSkills, userRoot) {
		return nil
	}
	userByName := map[string][]*skillDir{}
	for _, info := range listRealSkillDirs(userRoot) {
		userByName[info.name] = append(userByName[info.name], info)
	}
	var actions []SyncAction
	for _, pd := range listRealSkillDirs(projSkills) {
		if onlyKey != "" && NormalizeKey(onlyKey) != NormalizeKey(pd.name) && onlyKey != pd.folder {
			continue
		}
		group, ok := userByName[pd.name]
		if !ok {
			continue
		}
		var userDir *skillDir
		for _, u := range group {
			if u.hash == pd.hash {
				userDir = u
				break
			}
		}
		if userDir == nil {
			continue
		}
		keeper := pickKeeper(group)
		target := userDir.path
		if keeper.hash == pd.hash {
			target = keeper.path
		}
		if samePath(pd.path, target) {
			continue
		}
		actions = append(actions, SyncAction{
			Kind: "collapse_skill_alias", From: pd.path, To: target, SkillKey: pd.name,
		})
	}
	return actions
}

func RepairInstalledSkills(root string) []string {
	var log []string
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return log
	}
	for _, info := range listRealSkillDirs(root) {
		md := filepath.Join(info.path, "SKILL.md")
		ok, err := ApplyFrontmatterRepair(md)
		switch {
		case err != nil:
			log = append(log, fmt.Sprintf("repair failed %s: %v", md, err))
		case ok:
			log = append(log, fmt.Sprintf("repaired frontmatter %s: %s", info.folder, md))
		}
	}
	return log
}

// ApplyFrontmatterRepair rewrites one SKILL.md. false = nothing changed.
func ApplyFrontmatterRepair(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	folder := filepath.Base(filepath.Dir(path))
	repaired := RepairSkillFrontmatter(string(data), folder)
	if repaired == "" {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(repaired), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
