package cli

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The embedded tree is a mirror of the repo's skills/synca directory.
// skills/synca is the source of truth. TestEmbeddedSkillMatchesRepo fails
// when the two copies differ.

//go:embed skill
var skillFS embed.FS

func printSkillHelp() {
	fmt.Print(`USAGE:
  synca skill install [--dest DIR]
  synca skill check [--dest DIR] [--json]

install writes the embedded synca skill. The default dest is ~/.agents/skills/synca.
check compares that directory with the embedded copy. Exit 0 when they match,
8 on drift, 3 when the directory is missing.
`)
}

func runSkill(args []string) error {
	if len(args) == 0 {
		return failUsage(jsonIn(args), "usage: synca skill <install|check>", "see: synca skill --help")
	}
	if isHelp(args[0]) {
		printSkillHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	if hasHelp(rest) {
		printSkillHelp()
		return nil
	}
	switch sub {
	case "install":
		return runSkillInstall(rest)
	case "check":
		return runSkillCheck(rest)
	default:
		return failUsage(jsonIn(rest), "unknown skill command "+quoteArg(sub), "see: synca skill --help")
	}
}

func runSkillInstall(args []string) error {
	f := parseFlags(args, commonBool)
	jsonOut := f.bools["json"]
	if bad := f.unknownFlag(allow("dest", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca skill --help")
	}
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca skill install [--dest DIR]", "see: synca skill --help")
	}
	dest, err := skillDest(f.str("dest", ""))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if err := writeEmbeddedSkill(dest); err != nil {
		return failCode(jsonOut, ExitFailed, err.Error(), "")
	}
	if jsonOut {
		return writeJSON(skillInstallDocument{Dest: dest, Installed: true})
	}
	fmt.Println("installed synca skill at " + dest)
	return nil
}

func runSkillCheck(args []string) error {
	f := parseFlags(args, commonBool)
	jsonOut := f.bools["json"]
	if bad := f.unknownFlag(allow("dest", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca skill --help")
	}
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca skill check [--dest DIR] [--json]", "see: synca skill --help")
	}
	dest, err := skillDest(f.str("dest", ""))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	info, statErr := os.Stat(dest)
	if statErr != nil || !info.IsDir() {
		return failCode(jsonOut, ExitUnavailable, "skill directory is missing: "+dest, "run: synca skill install")
	}
	diff := diffSkill(dest)
	if jsonOut {
		if err := writeJSON(skillCheckDocument{
			Dest:      dest,
			InSync:    diff.inSync(),
			Missing:   stringOrEmpty(diff.missing),
			Different: stringOrEmpty(diff.different),
			Extra:     stringOrEmpty(diff.extra),
		}); err != nil {
			return err
		}
	} else if diff.inSync() {
		fmt.Println("in sync " + dest)
	} else {
		fmt.Println("drift " + dest)
		for _, name := range diff.missing {
			fmt.Println("missing " + name)
		}
		for _, name := range diff.different {
			fmt.Println("different " + name)
		}
		for _, name := range diff.extra {
			fmt.Println("extra " + name)
		}
	}
	if !diff.inSync() {
		return statusOnly(ExitConflict)
	}
	return nil
}

type skillInstallDocument struct {
	Dest      string `json:"dest"`
	Installed bool   `json:"installed"`
}

type skillCheckDocument struct {
	Dest      string   `json:"dest"`
	InSync    bool     `json:"inSync"`
	Missing   []string `json:"missing"`
	Different []string `json:"different"`
	Extra     []string `json:"extra"`
}

func skillDest(flag string) (string, error) {
	dest := flag
	if dest == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("HOME is not set")
		}
		dest = filepath.Join(home, ".agents", "skills", "synca")
	}
	if !filepath.IsAbs(dest) {
		abs, err := filepath.Abs(dest)
		if err != nil {
			return "", err
		}
		dest = abs
	}
	if filepath.Base(dest) == "SKILL.md" {
		dest = filepath.Dir(dest)
	}
	return dest, nil
}

type skillFile struct {
	body []byte
	exec bool
}

func embeddedSkillFiles() (map[string]skillFile, error) {
	out := map[string]skillFile{}
	err := fs.WalkDir(skillFS, "skill", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel("skill", path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		body, err := fs.ReadFile(skillFS, path)
		if err != nil {
			return err
		}
		out[rel] = skillFile{body: body, exec: strings.HasSuffix(rel, ".sh")}
		return nil
	})
	return out, err
}

func writeEmbeddedSkill(dest string) error {
	files, err := embeddedSkillFiles()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dest, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := files[name]
		path := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("could not create %s: %w", filepath.Dir(path), err)
		}
		mode := os.FileMode(0o644)
		if file.exec {
			mode = 0o755
		}
		if err := os.WriteFile(path, file.body, mode); err != nil {
			return fmt.Errorf("could not write %s: %w", path, err)
		}
	}
	return nil
}

type skillDiff struct {
	missing   []string
	different []string
	extra     []string
}

func (d skillDiff) inSync() bool {
	return len(d.missing) == 0 && len(d.different) == 0 && len(d.extra) == 0
}

func diffSkill(dest string) skillDiff {
	expected, err := embeddedSkillFiles()
	if err != nil {
		return skillDiff{missing: []string{"SKILL.md"}}
	}
	found := map[string]bool{}
	var extra []string
	_ = filepath.WalkDir(dest, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(dest, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			return nil
		}
		if _, ok := expected[rel]; ok {
			found[rel] = true
			return nil
		}
		extra = append(extra, rel)
		return nil
	})
	var missing, different []string
	for name, want := range expected {
		if !found[name] {
			missing = append(missing, name)
			continue
		}
		path := filepath.Join(dest, filepath.FromSlash(name))
		body, readErr := os.ReadFile(path)
		info, statErr := os.Stat(path)
		exec := statErr == nil && info.Mode()&0o111 != 0
		if readErr != nil || string(body) != string(want.body) || exec != want.exec {
			different = append(different, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(different)
	sort.Strings(extra)
	return skillDiff{missing: missing, different: different, extra: extra}
}

func stringOrEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
