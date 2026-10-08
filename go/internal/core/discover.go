package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nameRe = regexp.MustCompile(`(?m)^name:\s*["']?([^"'\n]+)["']?\s*$`)

// frontmatterBlock returns the YAML slice between `---` and `\n---`.
func frontmatterBlock(text string) (string, bool) {
	if !strings.HasPrefix(text, "---") {
		return "", false
	}
	rest := text[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func skillFrontmatterName(md string) (string, bool) {
	text, err := os.ReadFile(md)
	if err != nil {
		return "", false
	}
	fm, ok := frontmatterBlock(string(text))
	if !ok {
		return "", false
	}
	m := nameRe.FindStringSubmatch(fm)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func skillFrontmatterDescription(md string) (string, bool) {
	text, err := os.ReadFile(md)
	if err != nil {
		return "", false
	}
	fm, ok := frontmatterBlock(string(text))
	if !ok {
		return "", false
	}
	return parseDescriptionField(fm)
}

func parseDescriptionField(fm string) (string, bool) {
	lines := strings.Split(fm, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		rest, found := strings.CutPrefix(trimmed, "description:")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || rest == "|" || rest == ">" || rest == "|-" || rest == ">-" || rest == "|+" || rest == ">+" {
			var parts []string
			for i+1 < len(lines) {
				cont := lines[i+1]
				if strings.HasPrefix(cont, " ") || strings.HasPrefix(cont, "\t") {
					parts = append(parts, strings.TrimSpace(cont))
					i++
				} else if strings.TrimSpace(cont) == "" {
					i++
					break
				} else {
					break
				}
			}
			joined := strings.TrimSpace(strings.Join(parts, " "))
			if joined == "" {
				return "", false
			}
			return joined, true
		}
		v := strings.TrimSpace(strings.Trim(rest, "\"'"))
		if v == "" {
			return "", false
		}
		return v, true
	}
	return "", false
}

func skillDescription(dir string) *string {
	md := filepath.Join(dir, "SKILL.md")
	if st, err := os.Stat(md); err == nil && !st.IsDir() {
		if d, ok := skillFrontmatterDescription(md); ok {
			return &d
		}
	}
	return nil
}

func SkillDisplayName(dir string) string {
	folder := filepath.Base(dir)
	if folder == "" {
		folder = "unknown"
	}
	md := filepath.Join(dir, "SKILL.md")
	if st, err := os.Stat(md); err == nil && !st.IsDir() {
		if n, ok := skillFrontmatterName(md); ok {
			return n
		}
	}
	return folder
}
