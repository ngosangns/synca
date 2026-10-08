package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
)

// flags is a minimal clap-style flag parser: --long value, --long=value, --bool.
type flags struct {
	args    []string
	vals    map[string]string
	bools   map[string]bool
	pos     []string
	unknown []string
}

func parseFlags(args []string, boolFlags map[string]bool) *flags {
	f := &flags{vals: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") || a == "--" {
			f.pos = append(f.pos, a)
			continue
		}
		name := a[2:]
		var val string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val = name[eq+1:]
			name = name[:eq]
		}
		if boolFlags[name] {
			f.bools[name] = val == "" || val == "true"
			continue
		}
		if val == "" && i+1 < len(args) {
			val = args[i+1]
			i++
		}
		f.vals[name] = val
	}
	return f
}

func (f *flags) str(name, def string) string {
	if v, ok := f.vals[name]; ok && v != "" {
		return v
	}
	return def
}

func (f *flags) strPtr(name string) *string {
	if v, ok := f.vals[name]; ok {
		return &v
	}
	return nil
}

func (f *flags) boolVal(name string) *bool {
	if v, ok := f.vals[name]; ok {
		b := v == "true" || v == "yes" || v == "1"
		return &b
	}
	return nil
}

var commonBool = map[string]bool{"json": true, "dry-run": true, "purge": true, "yes": true, "check": true, "force": true, "help": true, "version": true}

func agentsFilter(f *flags) ([]core.AgentKind, error) {
	s := f.str("agents", "")
	if s == "" {
		return nil, nil
	}
	return core.ParseAgentList(s), nil
}

func cwdOf(f *flags) string {
	return f.str("cwd", ".")
}

func scopeOf(f *flags) (core.Scope, error) {
	switch f.str("scope", "user") {
	case "user":
		return core.ScopeUser, nil
	case "project":
		return core.ScopeProject, nil
	default:
		return core.ScopeUser, fmt.Errorf("invalid --scope '%s' (use user|project)", f.str("scope", "user"))
	}
}

func jsonPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "null"
	}
	return string(b)
}

func jsonCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func Run(args []string, runTUI func(cwd string) error) error {
	if len(args) == 0 {
		cwd, _ := os.Getwd()
		return runTUI(cwd)
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "--version", "-V", "version":
		fmt.Println("synca", core.Version)
		return nil
	case "tui":
		f := parseFlags(rest, commonBool)
		cwd := cwdOf(f)
		if f.str("cwd", "") == "" {
			if w, err := os.Getwd(); err == nil {
				cwd = w
			}
		}
		return runTUI(cwd)
	case "skills":
		return runSkills(rest)
	case "mcp":
		return runMcp(rest)
	case "sync":
		return runSync(rest)
	case "update":
		f := parseFlags(rest, commonBool)
		return runUpdate(f.bools["check"], f.bools["json"], f.bools["force"])
	default:
		return fmt.Errorf("unknown command '%s' (see: synca help)", cmd)
	}
}

func printHelp() {
	fmt.Print(`synca — Browse and sync skills + MCP across coding agents

USAGE:
  synca                        # open TUI (default)
  synca tui [--cwd <dir>]
  synca skills <list|install|remove> [flags]
  synca mcp <list|add|remove> [flags]
  synca sync <skills|mcp|all> [flags]
  synca update [--check] [--json] [--force]

COMMON FLAGS:
  --scope <user|project>   default: user
  --cwd <dir>              project root hint (default: .)
  --json                   machine-readable output
  --agents <a,b,c>         restrict to agents
  --dry-run                plan only
  --on-conflict <policy>   skip|keep-source|keep-target
`)
}

func runSkills(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: synca skills <list|install|remove>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	scope, err := scopeOf(f)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		inv := core.ScanAll(scope, cwdOf(f))
		if f.bools["json"] {
			fmt.Println(jsonPretty(inv.Skills))
		} else {
			fmt.Printf("Skills (%s)\n", scope)
			for _, s := range inv.Skills {
				var agents []string
				for _, p := range s.Presence {
					agents = append(agents, p.Agent)
				}
				flag := ""
				if s.Mismatch {
					flag = " MISMATCH"
				}
				fmt.Printf("  %s  [%s]%s\n", s.DisplayName, strings.Join(agents, ","), flag)
			}
			fmt.Printf("%d skill(s)\n", len(inv.Skills))
		}
		return nil
	case "install":
		if len(f.pos) < 1 {
			return fmt.Errorf("usage: synca skills install <source> [--scope --agents --dry-run --cwd]")
		}
		filter, err := agentsFilter(f)
		if err != nil {
			return err
		}
		plan, log, err := core.InstallSkill(scope, cwdOf(f), f.pos[0], filter, f.bools["dry-run"])
		if err != nil {
			return err
		}
		printManagePlan(plan)
		return printPlanResult(log, f.bools["dry-run"], "")
	case "remove":
		if len(f.pos) < 1 {
			return fmt.Errorf("usage: synca skills remove <name> [--scope --agents --purge --yes --dry-run --cwd]")
		}
		name := f.pos[0]
		purge, yes, dry := f.bools["purge"], f.bools["yes"], f.bools["dry-run"]
		if purge && !yes && !dry {
			ok, err := confirmPurge(name)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("purge cancelled")
				return nil
			}
		}
		filter, err := agentsFilter(f)
		if err != nil {
			return err
		}
		plan, log, err := core.RemoveSkill(scope, cwdOf(f), name, filter, purge, dry)
		if err != nil {
			return err
		}
		printManagePlan(plan)
		return printPlanResult(log, dry, "")
	default:
		return fmt.Errorf("unknown skills command '%s'", sub)
	}
}

func runMcp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: synca mcp <list|add|remove>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	scope, err := scopeOf(f)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		inv := core.ScanAll(scope, cwdOf(f))
		if f.bools["json"] {
			fmt.Println(jsonPretty(inv.Mcps))
		} else {
			fmt.Printf("MCPs (%s)\n", scope)
			for _, m := range inv.Mcps {
				var agents []string
				for _, p := range m.Presence {
					agents = append(agents, p.Agent)
				}
				flag := ""
				if m.Mismatch {
					flag = " MISMATCH"
				}
				fmt.Printf("  %s  [%s]%s\n", m.Key, strings.Join(agents, ","), flag)
			}
			fmt.Printf("%d mcp server(s)\n", len(inv.Mcps))
		}
		return nil
	case "add":
		if len(f.pos) < 1 {
			return fmt.Errorf("usage: synca mcp add <name> [--transport --command --url --enabled --scope --agents --dry-run --cwd]")
		}
		norm, err := core.McpFromCLI(
			f.str("transport", "stdio"),
			f.strPtr("command"),
			f.strPtr("url"),
			f.boolVal("enabled"),
		)
		if err != nil {
			return err
		}
		filter, err := agentsFilter(f)
		if err != nil {
			return err
		}
		plan, log, err := core.AddMcp(scope, cwdOf(f), f.pos[0], norm, filter, f.bools["dry-run"])
		if err != nil {
			return err
		}
		printManagePlan(plan)
		return printPlanResult(log, f.bools["dry-run"], "")
	case "remove":
		if len(f.pos) < 1 {
			return fmt.Errorf("usage: synca mcp remove <name> [--scope --agents --dry-run --cwd]")
		}
		filter, err := agentsFilter(f)
		if err != nil {
			return err
		}
		plan, log, err := core.RemoveMcp(scope, cwdOf(f), f.pos[0], filter, f.bools["dry-run"])
		if err != nil {
			return err
		}
		printManagePlan(plan)
		return printPlanResult(log, f.bools["dry-run"], "")
	default:
		return fmt.Errorf("unknown mcp command '%s'", sub)
	}
}

func runSync(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: synca sync <skills|mcp|all>")
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	scope, err := scopeOf(f)
	if err != nil {
		return err
	}
	cwd := cwdOf(f)
	dry := f.bools["dry-run"]
	filter, err := agentsFilter(f)
	if err != nil {
		return err
	}

	var plan *core.SyncPlan
	switch sub {
	case "skills":
		plan, err = core.PlanSyncSkills(scope, cwd, filter, f.str("key", ""))
	case "mcp":
		plan, err = core.PlanSyncMcp(scope, cwd, filter, f.str("key", ""))
	case "all":
		var sk, mc *core.SyncPlan
		sk, err = core.PlanSyncSkills(scope, cwd, filter, "")
		if err == nil {
			mc, err = core.PlanSyncMcp(scope, cwd, filter, "")
		}
		if err == nil {
			sk.DryRun = dry
			mc.DryRun = dry
			plan = core.MergePlans(sk, mc)
		}
	default:
		return fmt.Errorf("unknown sync command '%s'", sub)
	}
	if err != nil {
		return err
	}
	plan.DryRun = dry
	printPlan(plan)

	decisions, err := resolveCLIConflicts(plan, f.str("on-conflict", "skip"), dry)
	if err != nil {
		return err
	}
	if dry {
		fmt.Printf("(dry-run; no changes) on-conflict=%s\n", decisions.Default.String())
		return nil
	}
	log, err := core.ApplyPlan(plan, cwd, scope, decisions)
	if err != nil {
		return err
	}
	for _, line := range log {
		fmt.Println(" ", line)
	}
	return nil
}

func printPlan(plan *core.SyncPlan) {
	fmt.Printf("Sync plan (scope=%s, actions=%d):\n", plan.Scope, len(plan.Actions))
	for i, a := range plan.Actions {
		fmt.Printf("  %d. %s\n", i+1, jsonCompact(a))
	}
}

func printManagePlan(plan *core.ManagePlan) {
	fmt.Printf("Manage plan (scope=%s, dry_run=%t, actions=%d):\n", plan.Scope, plan.DryRun, len(plan.Actions))
	for _, n := range plan.Notes {
		fmt.Println("  note:", n)
	}
	for i, a := range plan.Actions {
		fmt.Printf("  %d. %s\n", i+1, jsonCompact(a))
	}
}

func printPlanResult(log []string, dry bool, extra string) error {
	if dry {
		if extra != "" {
			fmt.Println(extra)
		}
		fmt.Println("(dry-run; no changes)")
		return nil
	}
	for _, line := range log {
		fmt.Println(" ", line)
	}
	return nil
}

func resolveCLIConflicts(plan *core.SyncPlan, onConflict string, dry bool) (*core.ConflictDecisions, error) {
	policy, ok := core.ParseConflictPolicy(onConflict)
	if !ok {
		return nil, fmt.Errorf("invalid --on-conflict '%s' (use skip|keep-source|keep-target)", onConflict)
	}
	hasConflict := false
	for _, a := range plan.Actions {
		if a.Kind == "conflict_skill" || a.Kind == "conflict_mcp" {
			hasConflict = true
			break
		}
	}
	if hasConflict && !dry && onConflict == "skip" && isTTY(os.Stdin) {
		fmt.Fprintln(os.Stderr, "Conflicts detected. Choose policy:")
		fmt.Fprintln(os.Stderr, "  [s] skip (default)")
		fmt.Fprintln(os.Stderr, "  [a] keep-source")
		fmt.Fprintln(os.Stderr, "  [b] keep-target")
		fmt.Fprint(os.Stderr, "> ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		choice := strings.TrimSpace(line)
		if choice == "" {
			policy = core.ConflictSkip
		} else if p, ok := core.ParseConflictPolicy(choice); ok {
			policy = p
		} else {
			return nil, fmt.Errorf("unknown conflict choice '%s'", choice)
		}
	}
	return core.NewDecisions(policy), nil
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func confirmPurge(name string) (bool, error) {
	fmt.Fprintf(os.Stderr, "PURGE skill '%s' will DELETE the canonical tree and unlink all agents.\n", name)
	fmt.Fprintln(os.Stderr, "Type the skill name again to confirm, or press Enter to cancel:")
	fmt.Fprint(os.Stderr, "> ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	t := strings.TrimSpace(line)
	return t == name || t == core.NormalizeKey(name), nil
}

func runUpdate(checkOnly, jsonOut, force bool) error {
	if checkOnly {
		info, err := core.CheckUpdate()
		if err != nil {
			return err
		}
		if jsonOut {
			fmt.Println(jsonCompact(struct {
				OK              bool    `json:"ok"`
				Current         string  `json:"current"`
				Latest          *string `json:"latest"`
				UpdateAvailable bool    `json:"update_available"`
				URL             *string `json:"url"`
				AssetName       *string `json:"asset_name"`
				Message         string  `json:"message"`
			}{true, info.Current, info.Latest, info.UpdateAvailable, info.ReleaseURL, info.AssetName, info.Message}))
		} else {
			fmt.Println("Current:", info.Current)
			if info.Latest != nil {
				fmt.Println("Latest: ", *info.Latest)
			}
			if info.ReleaseURL != nil {
				fmt.Println("Release:", *info.ReleaseURL)
			}
			fmt.Println(info.Message)
			if info.UpdateAvailable {
				fmt.Println("Run: synca update")
			}
		}
		return nil
	}

	var info *core.UpdateInfo
	var err error
	if force {
		info, err = core.InstallUpdate(true)
	} else {
		var chk *core.UpdateInfo
		chk, err = core.CheckUpdate()
		if err == nil && chk.UpdateAvailable {
			info, err = core.InstallUpdate(false)
		} else {
			info = chk
		}
	}
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(jsonCompact(struct {
			OK              bool    `json:"ok"`
			Current         string  `json:"current"`
			Latest          *string `json:"latest"`
			UpdateAvailable bool    `json:"update_available"`
			Message         string  `json:"message"`
			Path            string  `json:"path"`
		}{true, info.Current, info.Latest, info.UpdateAvailable, info.Message, core.SymlinkPath()}))
	} else {
		fmt.Println("Current:", info.Current)
		if info.Latest != nil {
			fmt.Println("Latest: ", *info.Latest)
		}
		fmt.Println(info.Message)
	}
	return nil
}
