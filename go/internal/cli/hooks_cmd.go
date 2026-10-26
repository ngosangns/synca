package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
)

func printHooksHelp() {
	fmt.Print(`USAGE:
  synca hooks list [--agents a,b] [--json]
  synca hooks add <id> --event <Event> --command <cmd> [--tools shell,write] [--timeout SEC]
                  [--description TEXT] [--agents a,b] [--exclude-agents a,b] [--on-failure continue|block]
                  [--force] [--dry-run] [--json]
  synca hooks remove <id> [--dry-run] [--json]
  synca hooks trust <id>        (a person at a terminal only)
  synca hooks untrust <id> [--json]
  synca hooks sync [--agents a,b] [--dry-run] [--on-conflict skip|keep-source] [--strict] [--yes] [--json]

Hooks are shell commands an agent runs on events. The hub is ~/.agents/hooks.json (version 1);
scripts live in ~/.agents/hooks/<id>/ and {{hook_dir}} in a command expands to that directory.
Events: SessionStart UserPromptSubmit PreToolUse PostToolUse Stop SessionEnd PreCompact PostCompact
        Notification PermissionRequest SubagentStart SubagentStop PostToolUseFailure
Tool classes (tool events only): shell read write search web agent mcp
Agents written: claude, grok, devin. Kiro, OpenCode and Cursor are not implemented yet.

A hook runs code, so a new or changed hook is never written until a person approves it with
"synca hooks trust <id>" at a terminal. There is no --yes for that and agents cannot approve their own
hooks. "sync hooks" and "sync all" skip unapproved hooks and list them; the exit code stays 0, or 7
with --strict. Only --scope user is supported. Files synca edits are backed up under
~/.local/state/synca/hooks/backups/.
`)
}

func hooksAllow(sub string) map[string]bool {
	switch sub {
	case "list":
		return allow("agents", "json", "help", "scope", "cwd")
	case "add":
		return allow("event", "command", "tools", "timeout", "description", "agents", "exclude-agents", "on-failure", "force", "dry-run", "json", "help", "scope", "cwd")
	case "remove":
		return allow("dry-run", "json", "help", "scope", "cwd")
	case "trust":
		return allow("help", "scope", "cwd")
	case "untrust":
		return allow("json", "help", "scope", "cwd")
	case "sync":
		return allow("agents", "dry-run", "on-conflict", "strict", "yes", "json", "help", "scope", "cwd")
	}
	return allow()
}

var hooksBool = map[string]bool{"json": true, "dry-run": true, "force": true, "help": true, "strict": true, "yes": true}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type hooksListDocument struct {
	Scope string          `json:"scope"`
	Hub   string          `json:"hub"`
	Hooks []core.HookInfo `json:"hooks"`
}

type hooksChangeDocument struct {
	DryRun bool   `json:"dryRun"`
	Hook   string `json:"hook"`
	Change string `json:"change"`
	Hub    string `json:"hub"`
}

type hooksTrustDocument struct {
	Hook    string `json:"hook"`
	Trusted bool   `json:"trusted"`
}

type hooksPlanDocument struct {
	DryRun     bool              `json:"dryRun"`
	Scope      string            `json:"scope"`
	OnConflict string            `json:"onConflict"`
	Untrusted  int               `json:"untrusted"`
	Actions    []core.HookAction `json:"actions"`
	Applied    []string          `json:"applied,omitempty"`
}

func hooksPlanDoc(plan *core.HooksPlan, policy string, dry bool, applied []string) hooksPlanDocument {
	actions := plan.Actions
	if actions == nil {
		actions = []core.HookAction{}
	}
	doc := hooksPlanDocument{DryRun: dry, Scope: plan.Scope, OnConflict: policy, Untrusted: plan.Untrusted, Actions: actions}
	if !dry {
		if applied == nil {
			applied = []string{}
		}
		doc.Applied = applied
	}
	return doc
}

func asHooksErr(jsonOut bool, err error) error {
	var missing *core.HookMissingError
	var exists *core.HookExistsError
	switch {
	case errors.As(err, &missing):
		return failCode(jsonOut, ExitUnavailable, err.Error(), "see: synca hooks list")
	case errors.As(err, &exists):
		return failCode(jsonOut, ExitConflict, err.Error(), "")
	case strings.HasPrefix(err.Error(), "invalid hook"):
		return failUsage(jsonOut, err.Error(), "see: synca hooks --help")
	}
	return asCommandErr(jsonOut, err)
}

func runHooks(args []string) error {
	if len(args) == 0 {
		return failUsage(jsonIn(args), "usage: synca hooks <list|add|remove|trust|untrust|sync>", "see: synca hooks --help")
	}
	if isHelp(args[0]) || (len(args) > 1 && hasHelp(args[1:])) {
		printHooksHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest, hooksBool)
	jsonOut := f.bools["json"]
	switch sub {
	case "list", "add", "remove", "trust", "untrust", "sync":
	default:
		return failUsage(jsonOut, fmt.Sprintf("unknown hooks command '%s'", sub), "see: synca hooks --help")
	}
	if bad := f.unknownFlag(hooksAllow(sub)); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca hooks --help")
	}
	if s := f.str("scope", "user"); s != "user" {
		return failUsage(jsonOut, "hooks support --scope user only in this version", "see: synca hooks --help")
	}
	switch sub {
	case "list":
		filter, err := agentsFilter(f)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		hooks, err := core.ListHooks(filter)
		if err != nil {
			return asHooksErr(jsonOut, err)
		}
		if jsonOut {
			return writeJSON(hooksListDocument{Scope: "user", Hub: core.HubHooksPath(), Hooks: hooks})
		}
		fmt.Printf("Hooks (user) — %s\n", core.HubHooksPath())
		for _, h := range hooks {
			trusted := "TRUSTED"
			if !h.Trusted {
				trusted = "NOT TRUSTED"
			}
			off := ""
			if !h.Enabled {
				off = " (disabled)"
			}
			fmt.Printf("  %s  %s%s  [%s]%s\n", h.ID, h.Event, toolsSuffix(h.Tools), trusted, off)
			fmt.Printf("      %s\n", h.Command)
			var states []string
			for _, a := range h.Agents {
				states = append(states, a.Agent+"="+a.State)
			}
			fmt.Printf("      %s\n", strings.Join(states, "  "))
		}
		fmt.Printf("%d hook(s)\n", len(hooks))
		return nil
	case "add":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca hooks add <id> --event <Event> --command <cmd> [--tools --timeout --description --agents --exclude-agents --on-failure --force --dry-run]", "see: synca hooks --help")
		}
		timeout := 0
		if v := f.str("timeout", ""); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return failUsage(jsonOut, "invalid --timeout '"+v+"' (seconds)", "see: synca hooks --help")
			}
			timeout = n
		}
		h := core.Hook{
			Description:   f.str("description", ""),
			Event:         f.str("event", ""),
			Tools:         splitList(f.str("tools", "")),
			Handler:       core.HookHandler{Command: f.str("command", ""), TimeoutSec: timeout},
			OnFailure:     f.str("on-failure", ""),
			Agents:        splitList(f.str("agents", "")),
			ExcludeAgents: splitList(f.str("exclude-agents", "")),
		}
		dry := f.bools["dry-run"]
		change, err := core.AddHook(core.NormalizeKey(f.pos[0]), h, f.bools["force"], dry)
		if err != nil {
			return asHooksErr(jsonOut, err)
		}
		id := core.NormalizeKey(f.pos[0])
		if jsonOut {
			return writeJSON(hooksChangeDocument{DryRun: dry, Hook: id, Change: change, Hub: core.HubHooksPath()})
		}
		fmt.Printf("%s hook %s in %s\n", change, id, core.HubHooksPath())
		if !dry {
			fmt.Printf("It is not written to any agent until you approve it: synca hooks trust %s\n", id)
		}
		return nil
	case "remove":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca hooks remove <id> [--dry-run]", "see: synca hooks --help")
		}
		id := core.NormalizeKey(f.pos[0])
		dry := f.bools["dry-run"]
		if err := core.RemoveHook(id, dry); err != nil {
			return asHooksErr(jsonOut, err)
		}
		if jsonOut {
			return writeJSON(hooksChangeDocument{DryRun: dry, Hook: id, Change: "removed", Hub: core.HubHooksPath()})
		}
		fmt.Printf("removed hook %s from the hub. Run \"synca hooks sync\" to remove it from the agents.\n", id)
		return nil
	case "trust":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca hooks trust <id>", "see: synca hooks --help")
		}
		id := core.NormalizeKey(f.pos[0])
		lines, err := core.DescribeHook(id)
		if err != nil {
			return asHooksErr(false, err)
		}
		if !stdinIsTerminal() {
			return failCode(false, ExitNeedsConfirm, "approving a hook needs a person at a terminal; there is no flag to skip this", "run \"synca hooks trust "+id+"\" yourself in a terminal")
		}
		fmt.Fprintln(os.Stderr, "This hook will run on every matching agent event with your permissions:")
		for _, l := range lines {
			fmt.Fprintln(os.Stderr, "  "+l)
		}
		fmt.Fprint(os.Stderr, "Type \"yes\" to approve it: ")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != "yes" {
			return failCode(false, ExitNeedsConfirm, "not approved", "")
		}
		if _, err := core.TrustHook(id); err != nil {
			return asHooksErr(false, err)
		}
		fmt.Printf("approved %s. Run \"synca hooks sync\" to write it to the agents.\n", id)
		return nil
	case "untrust":
		if len(f.pos) < 1 {
			return failUsage(jsonOut, "usage: synca hooks untrust <id>", "see: synca hooks --help")
		}
		id := core.NormalizeKey(f.pos[0])
		if _, err := core.UntrustHook(id); err != nil {
			return asHooksErr(jsonOut, err)
		}
		if jsonOut {
			return writeJSON(hooksTrustDocument{Hook: id, Trusted: false})
		}
		fmt.Printf("withdrew the approval for %s. Existing agent entries stay in place and are reported as untrusted; run \"synca hooks remove %s\" then \"synca hooks sync\" to delete them.\n", id, id)
		return nil
	default:
		return runHooksSync(f, jsonOut)
	}
}

func toolsSuffix(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	return " on " + strings.Join(tools, ",")
}

// runHooksSync backs `synca hooks sync` and `synca sync hooks`.
func runHooksSync(f *flags, jsonOut bool) error {
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	onConflict := f.str("on-conflict", "skip")
	policy, ok := core.ParseConflictPolicy(onConflict)
	if !ok || policy == core.ConflictKeepTarget {
		return failUsage(jsonOut, fmt.Sprintf("invalid --on-conflict '%s' for hooks (use skip|keep-source)", onConflict), "see: synca hooks --help")
	}
	dry := f.bools["dry-run"]
	if !dry && policy == core.ConflictKeepSource && !f.bools["yes"] && !stdinIsTerminal() {
		return failNeedsConfirm(jsonOut, "sync hooks --on-conflict keep-source overwrites hand-edited hook entries", "pass --yes")
	}
	plan, applied, err := syncHooksNow(filter, policy, dry)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if jsonOut {
		if err := writeJSON(hooksPlanDoc(plan, policy.String(), dry, applied)); err != nil {
			return err
		}
	} else {
		printHooksPlan(plan, applied, dry)
	}
	if f.bools["strict"] && plan.Untrusted > 0 {
		return statusOnlyAfter(jsonOut, ExitUnauthorized, fmt.Sprintf("%d hook(s) need approval (synca hooks trust <id>)", plan.Untrusted))
	}
	return nil
}

func syncHooksNow(filter []core.AgentKind, policy core.ConflictPolicy, dry bool) (*core.HooksPlan, []string, error) {
	plan, err := core.PlanSyncHooks(filter, policy)
	if err != nil {
		return nil, nil, err
	}
	plan.DryRun = dry
	if dry {
		return plan, nil, nil
	}
	log, err := core.ApplyHooksPlan(plan)
	return plan, log, err
}

// statusOnlyAfter reports --strict failures. With --json the plan was already
// printed, so only the status is returned; in text mode a line explains it.
func statusOnlyAfter(jsonOut bool, code int, msg string) error {
	if jsonOut {
		return statusOnly(code)
	}
	return &ExitError{Code: code, Msg: msg}
}

func printHooksPlan(plan *core.HooksPlan, applied []string, dry bool) {
	if len(plan.Actions) == 0 {
		fmt.Println("hooks: nothing to do")
	}
	for _, a := range plan.Actions {
		line := "  " + a.Kind
		if a.Hook != "" {
			line += "  " + a.Hook
		}
		if a.Agent != "" {
			line += " -> " + a.Agent
		}
		if a.Event != "" {
			line += " (" + a.Event + ")"
		}
		if a.Reason != "" {
			line += "  " + a.Reason
		}
		fmt.Println(line)
		for _, l := range a.Lossy {
			fmt.Printf("      %s [%s] %s\n", l.Code, l.Severity, l.Detail)
		}
	}
	if plan.Untrusted > 0 {
		fmt.Printf("%d hook(s) skipped: not approved. Review and run: synca hooks trust <id>\n", plan.Untrusted)
	}
	if dry {
		fmt.Println("(dry-run; no changes)")
		return
	}
	for _, l := range applied {
		fmt.Println(" ", l)
	}
}
