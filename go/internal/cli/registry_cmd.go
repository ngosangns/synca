package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
	"github.com/ngosangns/synca/go/internal/registry"
)

func printRegistryHelp() {
	fmt.Print(`USAGE:
  synca skills search <query> [--owner O] [--limit N] [--json]
  synca skills browse [--view all-time|trending|hot] [--page N] [--source owner/repo] [--json]
  synca skills official [--json]
  synca skills info <owner/repo/skill> [--contents] [--json]
  synca skills audit <owner/repo/skill> [--json]
  synca skills use <source[@skill]> [--skill S] [--json]
  synca skills discover <source> [--json]
  synca skills update [name...] [--scope --cwd --agents --dry-run --json]
  synca skills restore [--scope --cwd --agents --dry-run --json]
  synca skills init <name> [--dir D] [--json]

The registry is skills.sh. search and browse use its public endpoints.
audit is public too. official and browse --source use the v1 API and need
SKILLS_API_TOKEN (a Vercel OIDC token); without it they exit 7. info uses the
v1 detail endpoint when a token is set (adds the install count), else the public snapshot.
SKILLS_API_URL and SKILLS_DOWNLOAD_URL change the host. Page numbers start at 0.
install accepts owner/repo, owner/repo@skill, a Git URL, a skills.sh URL, or a folder.
update compares each remote skill with its source and reinstalls the ones that changed.
restore reinstalls the remote skills in the lock file that are missing on disk.
`)
}

func registryCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// registryErr maps a registry failure onto the exit-code contract.
func registryErr(jsonOut bool, err error) error {
	var re *registry.Error
	if !errors.As(err, &re) {
		return asCommandErr(jsonOut, err)
	}
	switch re.Kind {
	case registry.KindUsage:
		return failUsage(jsonOut, re.Msg, nonEmpty(re.Hint, "see: synca skills --help"))
	case registry.KindUnauthorized:
		return failCode(jsonOut, ExitUnauthorized, re.Msg, re.Hint)
	default:
		return failCode(jsonOut, ExitUnavailable, re.Msg, re.Hint)
	}
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func intFlag(f *flags, name string, def int) (int, error) {
	raw := f.str(name, "")
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("--%s must be a whole number", name)
	}
	return n, nil
}

func fmtInstalls(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "K"
	default:
		return strconv.Itoa(n)
	}
}

func printRegistrySkills(skills []registry.Skill) {
	for _, s := range skills {
		mark := ""
		if s.Official {
			mark = "  official"
		}
		fmt.Printf("  %-48s %7s installs%s\n", s.ID, fmtInstalls(s.Installs), mark)
	}
}

// runRegistry handles the registry-backed skills subcommands. It reports
// handled=false for the others.
func runRegistry(sub string, rest []string) (handled bool, err error) {
	switch sub {
	case "search", "browse", "official", "info", "audit", "use", "discover", "update", "restore", "init":
	default:
		return false, nil
	}
	f := parseFlags(rest, commonBool)
	jsonOut := f.bools["json"]
	allowed := map[string][]string{
		"search":   {"owner", "limit", "json", "help"},
		"browse":   {"view", "page", "source", "json", "help"},
		"official": {"json", "help"},
		"info":     {"contents", "json", "help"},
		"audit":    {"json", "help"},
		"use":      {"skill", "json", "help"},
		"discover": {"json", "help"},
		"update":   {"scope", "cwd", "agents", "dry-run", "json", "help"},
		"restore":  {"scope", "cwd", "agents", "dry-run", "json", "help"},
		"init":     {"dir", "json", "help"},
	}
	if bad := f.unknownFlag(allow(allowed[sub]...)); bad != "" {
		return true, failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca skills --help")
	}
	switch sub {
	case "search":
		return true, runSearch(f, jsonOut)
	case "browse":
		return true, runBrowse(f, jsonOut)
	case "official":
		return true, runOfficial(f, jsonOut)
	case "info":
		return true, runInfo(f, jsonOut)
	case "audit":
		return true, runAudit(f, jsonOut)
	case "use":
		return true, runUse(f, jsonOut)
	case "discover":
		return true, runDiscover(f, jsonOut)
	case "update":
		return true, runUpdate(f, jsonOut)
	case "restore":
		return true, runRestore(f, jsonOut)
	default:
		return true, runInit(f, jsonOut)
	}
}

func runSearch(f *flags, jsonOut bool) error {
	if len(f.pos) < 1 {
		return failUsage(jsonOut, "usage: synca skills search <query> [--owner O] [--limit N]", "see: synca skills --help")
	}
	limit, err := intFlag(f, "limit", 0)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca skills --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	res, err := registry.New(core.Version).Search(ctx, strings.Join(f.pos, " "), f.str("owner", ""), limit)
	if err != nil {
		return registryErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(res)
	}
	fmt.Printf("Search %q (%s)\n", res.Query, res.SearchType)
	printRegistrySkills(res.Skills)
	fmt.Printf("%d skill(s)\n", res.Count)
	return nil
}

func runBrowse(f *flags, jsonOut bool) error {
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca skills browse [--view --page --source]", "see: synca skills --help")
	}
	page, err := intFlag(f, "page", 0)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca skills --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	res, err := registry.New(core.Version).Browse(ctx, f.str("view", "all-time"), page, f.str("source", ""))
	if err != nil {
		return registryErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(res)
	}
	fmt.Printf("%s, page %d (%d of %d)\n", res.View, res.Page, len(res.Skills), res.Total)
	printRegistrySkills(res.Skills)
	if res.HasMore {
		fmt.Printf("more: synca skills browse --view %s --page %d\n", res.View, res.Page+1)
	}
	return nil
}

func runOfficial(f *flags, jsonOut bool) error {
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca skills official", "see: synca skills --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	res, err := registry.New(core.Version).Official(ctx)
	if err != nil {
		return registryErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(res)
	}
	fmt.Printf("Official skills: %d owner(s), %d skill(s)\n", res.TotalOwners, res.TotalSkills)
	for _, o := range res.Owners {
		fmt.Printf("%s (%s installs)\n", o.Owner, fmtInstalls(o.TotalInstalls))
		printRegistrySkills(o.Skills)
	}
	return nil
}

func runInfo(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca skills info <owner/repo/skill> [--contents]", "see: synca skills --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	res, err := registry.New(core.Version).Info(ctx, f.pos[0], f.bools["contents"])
	if err != nil {
		return registryErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(res)
	}
	fmt.Println(res.ID)
	if res.Installs != nil {
		fmt.Printf("  installs %s\n", fmtInstalls(*res.Installs))
	}
	if res.Hash != nil {
		fmt.Printf("  hash %s\n", *res.Hash)
	}
	for _, file := range res.Files {
		fmt.Printf("  %-40s %d bytes\n", file.Path, file.Size)
		if file.Contents != nil {
			fmt.Println(*file.Contents)
		}
	}
	return nil
}

func runAudit(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca skills audit <owner/repo/skill>", "see: synca skills --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	res, err := registry.New(core.Version).Audits(ctx, f.pos[0])
	if err != nil {
		return registryErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(res)
	}
	fmt.Println(res.ID)
	if len(res.Audits) == 0 {
		fmt.Println("  no audits yet")
	}
	for _, a := range res.Audits {
		risk := ""
		if a.RiskLevel != "" {
			risk = " " + a.RiskLevel
		}
		fmt.Printf("  %-22s %-4s%s  %s\n", a.Provider, a.Status, risk, a.Summary)
	}
	return nil
}

type useDocument struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

func runUse(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca skills use <source[@skill]> [--skill S]", "see: synca skills --help")
	}
	src := f.pos[0]
	if want := f.str("skill", ""); want != "" {
		src += "@" + want
	}
	res, err := core.ResolveSkillSource(src)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	defer res.Cleanup()
	body, err := os.ReadFile(filepath.Join(res.Dir, "SKILL.md"))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	prompt := "You are being given a Skill to execute for the user's next request.\n\n" +
		"Use the following SKILL.md as your instructions:\n\n<SKILL.md>\n" + string(body) + "\n</SKILL.md>\n"
	name := core.SkillDisplayName(res.Dir)
	if jsonOut {
		return writeJSON(useDocument{Name: name, Prompt: prompt})
	}
	fmt.Print(prompt)
	return nil
}

type discoveredJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Slug        string `json:"slug"`
}

type discoverDocument struct {
	Source string           `json:"source"`
	Skills []discoveredJSON `json:"skills"`
}

func runDiscover(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca skills discover <source>", "see: synca skills --help")
	}
	spec, err := core.ParseSource(f.pos[0])
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	root := spec.Path
	if !spec.Local {
		repo, err := core.CloneRepo(spec.URL, spec.Ref)
		if err != nil {
			return failCode(jsonOut, ExitUnavailable, "git clone failed for "+f.pos[0], "check the source and the network")
		}
		defer repo.Cleanup()
		root = repo.Root
	} else if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return failCode(jsonOut, ExitUnavailable, "skill source not found: "+root, "")
	}
	found, err := core.DiscoverSkills(root)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	doc := discoverDocument{Source: f.pos[0], Skills: []discoveredJSON{}}
	for _, s := range found {
		doc.Skills = append(doc.Skills, discoveredJSON{Name: s.Name, Description: s.Description, Path: s.Path, Slug: s.Slug})
	}
	if jsonOut {
		return writeJSON(doc)
	}
	for _, s := range doc.Skills {
		fmt.Printf("  %-32s %s\n", s.Slug, s.Description)
	}
	fmt.Printf("%d skill(s)\n", len(doc.Skills))
	return nil
}

func runUpdate(f *flags, jsonOut bool) error {
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca skills --help")
	}
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	dry := f.bools["dry-run"]
	plan, log, err := core.PlanUpdateSkills(scope, cwdOf(f), f.pos, filter, !dry)
	if err != nil {
		if plan != nil && log != nil {
			return applyFailed(jsonOut, log, err)
		}
		return asCommandErr(jsonOut, err)
	}
	return finishManage(plan, log, dry, jsonOut)
}

func runRestore(f *flags, jsonOut bool) error {
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca skills restore [--scope --cwd --agents --dry-run]", "see: synca skills --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca skills --help")
	}
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	dry := f.bools["dry-run"]
	plan, log, err := core.PlanRestoreSkills(scope, cwdOf(f), nil, filter, !dry)
	if err != nil {
		if plan != nil && log != nil {
			return applyFailed(jsonOut, log, err)
		}
		return asCommandErr(jsonOut, err)
	}
	return finishManage(plan, log, dry, jsonOut)
}

type initDocument struct {
	Dir     string   `json:"dir"`
	Created bool     `json:"created"`
	Files   []string `json:"files"`
}

func runInit(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca skills init <name> [--dir D]", "see: synca skills --help")
	}
	dir, err := core.InitSkill(f.pos[0], f.str("dir", "."))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(initDocument{Dir: dir, Created: true, Files: []string{"SKILL.md"}})
	}
	fmt.Println("created " + filepath.Join(dir, "SKILL.md"))
	return nil
}
