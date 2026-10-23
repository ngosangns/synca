package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
	"github.com/ngosangns/synca/go/internal/updater"
)

// flags is a minimal clap-style flag parser: --long value, --long=value, --bool.
type flags struct {
	args      []string
	vals      map[string]string
	bools     map[string]bool
	lists     map[string][]string
	pos       []string
	flagOrder []string
}

func parseFlags(args []string, boolFlags map[string]bool) *flags {
	f := &flags{
		vals:  map[string]string{},
		bools: map[string]bool{},
		lists: map[string][]string{},
	}
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
		f.flagOrder = append(f.flagOrder, name)
		if boolFlags[name] {
			f.bools[name] = val == "" || val == "true"
			continue
		}
		if val == "" && i+1 < len(args) {
			val = args[i+1]
			i++
		}
		f.vals[name] = val
		f.lists[name] = append(f.lists[name], val)
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

func (f *flags) unknownFlag(allowed map[string]bool) string {
	for _, name := range f.flagOrder {
		if !allowed[name] {
			return name
		}
	}
	return ""
}

func allow(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

func (f *flags) boolVal(name string) *bool {
	if v, ok := f.vals[name]; ok {
		b := v == "true" || v == "yes" || v == "1"
		return &b
	}
	return nil
}

var commonBool = map[string]bool{"json": true, "dry-run": true, "purge": true, "yes": true, "check": true, "force": true, "help": true, "version": true, "reveal": true}

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

func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "--version", "-V":
		fmt.Println("synca", core.Version)
		return nil
	case "version":
		return runVersion(rest)
	case "schema":
		return runSchema(rest)
	case "skills":
		return runSkills(rest)
	case "mcp":
		return runMcp(rest)
	case "sync":
		return runSync(rest)
	case "provider":
		return runProvider(rest)
	case "doctor":
		return runDoctor(rest)
	case "skill":
		return runSkill(rest)
	case "update":
		err := updater.Run(context.Background(), updater.Options{
			Args:    rest,
			Version: core.Version,
			Stdout:  os.Stdout,
			Stderr:  os.Stderr,
		})
		if err == nil {
			return nil
		}
		if _, ok := err.(*updater.ExitError); ok {
			os.Exit(updater.ExitCode(err))
		}
		return err
	default:
		return failUsage(jsonIn(args), fmt.Sprintf("unknown command '%s' (see: synca help)", cmd), "see: synca help")
	}
}

func printHelp() {
	fmt.Print(`synca — Browse and sync skills + MCP across coding agents

USAGE:
  synca skills <list|install|remove> [flags]
  synca mcp <list|add|remove> [flags]
  synca sync <skills|mcp|all> [flags]
  synca provider clear <name> [--scope --cwd --yes --dry-run]
  synca doctor [--scope --cwd --json]
  synca skill <install|check> [--dest DIR]
  synca update [--check] [--json] [--force]
  synca version [--json]
  synca schema [--json] [<command>]

COMMON FLAGS:
  --scope <user|project>   default: user
  --cwd <dir>              project root hint (default: .)
  --json                   machine-readable output
  --agents <a,b,c>         restrict to agents (grok, devin, kiro, opencode, claude, cursor)
  --dry-run                plan only
  --yes                    confirm a destructive command when stdin is not a TTY
  --on-conflict <policy>   skip|keep-source|keep-target
`)
}

func isHelp(s string) bool {
	return s == "--help" || s == "-h"
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if isHelp(arg) {
			return true
		}
	}
	return false
}

func jsonIn(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

// stdinIsTerminal reports whether stdin is a real terminal (see isTTY).
// Tests replace it so a developer terminal cannot turn a hermetic run into a prompt.
var stdinIsTerminal = func() bool { return isTTY(os.Stdin) }

func printSkillsHelp() {
	fmt.Print(`USAGE:
  synca skills list [--scope user|project] [--cwd <dir>] [--json]
  synca skills install <source> [--scope --cwd --agents --dry-run --json]
  synca skills remove <name> [--scope --cwd --agents --purge --yes --dry-run --json]
`)
}

func printMcpHelp() {
	fmt.Print(`USAGE:
  synca mcp list [--scope user|project] [--cwd <dir>] [--json] [--reveal]
  synca mcp add <name> [--transport --command --url --env KEY=VAL --enabled --scope --cwd --agents --dry-run --json]
  synca mcp remove <name> [--scope --cwd --agents --yes --dry-run --json]

JSON masks every env value as •••••• unless --reveal. A URL password and a
secret-looking arg (--token=…, an Authorization header) are masked the same way.
--reveal prints the real values. The text list is a summary and does not print
them. --env may be repeated.
`)
}

func printSyncHelp() {
	fmt.Print(`USAGE:
  synca sync <skills|mcp|all> [--scope --cwd --agents --dry-run --on-conflict --key --yes --json]

--json prints one plan: dryRun, scope, onConflict, actions, conflicts.
An apply adds applied and sets dryRun to false. Without --json the text plan is unchanged.
keep-source and keep-target overwrite data and need --yes when stdin is not a TTY.
--on-conflict skip leaves conflicts in place and exits 0.
`)
}

func printProviderHelp() {
	fmt.Print(`USAGE:
  synca provider clear <name> [--scope --cwd --yes --dry-run --json]
`)
}

func runSkills(args []string) error {
	if len(args) == 0 {
		return failUsage(jsonIn(args), "usage: synca skills <list|install|remove>", "see: synca skills --help")
	}
	if isHelp(args[0]) || (len(args) > 1 && hasHelp(args[1:])) {
		printSkillsHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	jsonOut := f.bools["json"]
	if bad := f.unknownFlag(allow("scope", "cwd", "agents", "dry-run", "purge", "yes", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca skills --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca skills --help")
	}
	switch sub {
	case "list":
		inv := core.ScanAll(scope, cwdOf(f))
		if jsonOut {
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
			return failUsage(jsonOut, "usage: synca skills install <source> [--scope --agents --dry-run --cwd]", "see: synca skills --help")
		}
		filter, err := agentsFilter(f)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		plan, log, err := core.InstallSkill(scope, cwdOf(f), f.pos[0], filter, f.bools["dry-run"])
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		return finishManage(plan, log, f.bools["dry-run"], jsonOut)
	case "remove":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca skills remove <name> [--scope --agents --purge --yes --dry-run --cwd]", "see: synca skills --help")
		}
		name := f.pos[0]
		purge, yes, dry := f.bools["purge"], f.bools["yes"], f.bools["dry-run"]
		filter, err := agentsFilter(f)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		plan, err := core.PlanRemoveSkill(scope, cwdOf(f), name, filter, purge)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		if purge && !yes && !dry && len(plan.Actions) > 0 {
			if !stdinIsTerminal() {
				return failNeedsConfirm(jsonOut, "purge deletes the canonical skill "+quoteArg(name), "pass --yes")
			}
			ok, err := confirmPurge(name)
			if err != nil {
				return asCommandErr(jsonOut, err)
			}
			if !ok {
				fmt.Println("purge cancelled")
				return nil
			}
		}
		plan.DryRun = dry
		var log []string
		if !dry {
			log, err = core.ApplyManagePlan(plan, nil)
			if err != nil {
				return asCommandErr(jsonOut, err)
			}
		}
		return finishManage(plan, log, dry, jsonOut)
	default:
		return failUsage(jsonOut, fmt.Sprintf("unknown skills command '%s'", sub), "see: synca skills --help")
	}
}

func runMcp(args []string) error {
	if len(args) == 0 {
		return failUsage(jsonIn(args), "usage: synca mcp <list|add|remove>", "see: synca mcp --help")
	}
	if isHelp(args[0]) || (len(args) > 1 && hasHelp(args[1:])) {
		printMcpHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	jsonOut := f.bools["json"]
	switch sub {
	case "list", "add", "remove":
	default:
		return failUsage(jsonOut, fmt.Sprintf("unknown mcp command '%s'", sub), "see: synca mcp --help")
	}
	if bad := f.unknownFlag(mcpAllow(sub)); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca mcp --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	switch sub {
	case "list":
		inv := core.ScanAll(scope, cwdOf(f))
		entries := inv.Mcps
		if !f.bools["reveal"] {
			entries = core.MaskMcpEntries(entries)
		}
		if jsonOut {
			fmt.Println(jsonPretty(entries))
		} else {
			fmt.Printf("MCPs (%s)\n", scope)
			for _, m := range entries {
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
			fmt.Printf("%d mcp server(s)\n", len(entries))
		}
		return nil
	case "add":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca mcp add <name> [--transport --command --url --env KEY=VAL --enabled --scope --agents --dry-run --cwd]", "see: synca mcp --help")
		}
		norm, err := core.McpFromCLI(
			f.str("transport", "stdio"),
			f.strPtr("command"),
			f.strPtr("url"),
			f.boolVal("enabled"),
		)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		env, keys, err := parseEnvPairs(f.lists["env"])
		if err != nil {
			return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
		}
		norm.Env = env
		norm.EnvKeys = keys
		filter, err := agentsFilter(f)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		plan, log, err := core.AddMcp(scope, cwdOf(f), f.pos[0], norm, filter, f.bools["dry-run"])
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		return finishManage(plan, log, f.bools["dry-run"], jsonOut)
	case "remove":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca mcp remove <name> [--scope --agents --yes --dry-run --cwd]", "see: synca mcp --help")
		}
		dry := f.bools["dry-run"]
		filter, err := agentsFilter(f)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		plan, err := core.PlanRemoveMcp(scope, cwdOf(f), f.pos[0], filter)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		if !dry && !f.bools["yes"] && len(plan.Actions) > 0 && !stdinIsTerminal() {
			return failNeedsConfirm(jsonOut, "mcp remove deletes the server from the hub and from agent configs", "pass --yes")
		}
		plan.DryRun = dry
		var log []string
		if !dry {
			log, err = core.ApplyManagePlan(plan, nil)
			if err != nil {
				return asCommandErr(jsonOut, err)
			}
		}
		return finishManage(plan, log, dry, jsonOut)
	default:
		return failUsage(jsonOut, fmt.Sprintf("unknown mcp command '%s'", sub), "see: synca mcp --help")
	}
}

func mcpAllow(sub string) map[string]bool {
	switch sub {
	case "list":
		return allow("scope", "cwd", "json", "reveal", "help")
	case "add":
		return allow("scope", "cwd", "agents", "transport", "command", "url", "env", "enabled", "dry-run", "json", "help")
	case "remove":
		return allow("scope", "cwd", "agents", "yes", "dry-run", "json", "help")
	default:
		return allow("help")
	}
}

// parseEnvPairs reads repeated --env KEY=VALUE flags. Keys are sorted.
func parseEnvPairs(pairs []string) (map[string]string, []string, error) {
	env := map[string]string{}
	var keys []string
	for _, pair := range pairs {
		key, val, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, nil, fmt.Errorf("invalid --env %s (use KEY=VALUE)", quoteArg(pair))
		}
		if _, exists := env[key]; !exists {
			keys = append(keys, key)
		}
		env[key] = val
	}
	sort.Strings(keys)
	if keys == nil {
		keys = []string{}
	}
	return env, keys, nil
}

func runSync(args []string) error {
	if len(args) == 0 {
		return failUsage(jsonIn(args), "usage: synca sync <skills|mcp|all>", "see: synca sync --help")
	}
	if isHelp(args[0]) || (len(args) > 1 && hasHelp(args[1:])) {
		printSyncHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, commonBool)
	jsonOut := f.bools["json"]
	if bad := f.unknownFlag(allow("scope", "cwd", "agents", "dry-run", "on-conflict", "key", "yes", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca sync --help")
	}
	if sub == "all" && f.str("key", "") != "" {
		return failUsage(jsonOut, "sync all does not take --key", "see: synca sync --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca sync --help")
	}
	cwd := cwdOf(f)
	dry := f.bools["dry-run"]
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	onConflict := f.str("on-conflict", "skip")
	policy, ok := core.ParseConflictPolicy(onConflict)
	if !ok {
		return failUsage(jsonOut, fmt.Sprintf("invalid --on-conflict '%s' (use skip|keep-source|keep-target)", onConflict), "see: synca sync --help")
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
		return failUsage(jsonOut, fmt.Sprintf("unknown sync command '%s'", sub), "see: synca sync --help")
	}
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	plan.DryRun = dry
	if !dry && (policy == core.ConflictKeepSource || policy == core.ConflictKeepTarget) && !f.bools["yes"] && !stdinIsTerminal() {
		return failNeedsConfirm(jsonOut, "sync --on-conflict "+policy.String()+" overwrites skill or MCP data", "pass --yes")
	}
	if jsonOut {
		if dry {
			return writeSyncJSON(plan, policy.String(), true, nil)
		}
		decisions := core.NewDecisions(policy)
		log, err := core.ApplyPlan(plan, cwd, scope, decisions)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		return writeSyncJSON(plan, policy.String(), false, log)
	}

	printPlan(plan)
	decisions, err := resolveCLIConflicts(plan, onConflict, dry)
	if err != nil {
		return asCommandErr(false, err)
	}
	if dry {
		fmt.Printf("(dry-run; no changes) on-conflict=%s\n", decisions.Default.String())
		return nil
	}
	log, err := core.ApplyPlan(plan, cwd, scope, decisions)
	if err != nil {
		return asCommandErr(false, err)
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
	if hasConflict && !dry && onConflict == "skip" && stdinIsTerminal() {
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

func runProvider(args []string) error {
	if hasHelp(args) {
		printProviderHelp()
		return nil
	}
	jsonOut := jsonIn(args)
	if len(args) == 0 || args[0] != "clear" {
		return failUsage(jsonOut, "usage: synca provider clear <name> [--scope --cwd --yes --dry-run]", "see: synca provider --help")
	}
	f := parseFlags(args[1:], commonBool)
	jsonOut = f.bools["json"]
	if bad := f.unknownFlag(allow("scope", "cwd", "yes", "dry-run", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca provider --help")
	}
	if len(f.pos) < 1 {
		return failUsage(jsonOut, "usage: synca provider clear <name> [--scope --cwd --yes --dry-run]", "see: synca provider --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca provider --help")
	}
	dry, yes := f.bools["dry-run"], f.bools["yes"]
	if !dry && !yes {
		return failNeedsConfirm(jsonOut, "refusing to delete provider data without --yes", "pass --yes")
	}
	log, err := core.ClearProvider(scope, cwdOf(f), f.pos[0], dry)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if log == nil {
		log = []string{}
	}
	if jsonOut {
		return writeJSON(providerDocument{
			DryRun:   dry,
			Scope:    scope.String(),
			Provider: f.pos[0],
			Lines:    log,
		})
	}
	for _, line := range log {
		fmt.Println(line)
	}
	return nil
}
