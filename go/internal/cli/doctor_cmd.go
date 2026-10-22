package cli

import (
	"fmt"

	"github.com/ngosangns/synca/go/internal/core"
)

func printDoctorHelp() {
	fmt.Print(`USAGE:
  synca doctor [--scope user|project] [--cwd <dir>] [--json]

Read-only. Reports skill drift (a real agent directory whose content differs
from canonical), dangling symlinks, missing agent links, and MCP entries that
differ between the hub and an agent. Exit 0 when clean, 8 when anything differs.
`)
}

func runDoctor(args []string) error {
	if hasHelp(args) {
		printDoctorHelp()
		return nil
	}
	f := parseFlags(args, commonBool)
	jsonOut := f.bools["json"]
	if bad := f.unknownFlag(allow("scope", "cwd", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca doctor --help")
	}
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca doctor [--scope --cwd --json]", "see: synca doctor --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca doctor --help")
	}
	report, err := core.Doctor(scope, cwdOf(f))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if jsonOut {
		if err := writeJSON(report); err != nil {
			return err
		}
	} else {
		printDoctor(report)
	}
	if !report.OK {
		return statusOnly(ExitConflict)
	}
	return nil
}

func printDoctor(report *core.DoctorReport) {
	n := len(report.Drift) + len(report.Dangling) + len(report.Missing) + len(report.MCP)
	if n == 0 {
		fmt.Printf("doctor %s: clean\n", report.Scope)
		return
	}
	fmt.Printf("doctor %s: %d issue(s)\n", report.Scope, n)
	for _, item := range report.Drift {
		fmt.Printf("drift %s %s %s differs\n", item.Agent, item.Skill, item.Path)
	}
	for _, item := range report.Dangling {
		target := item.Target
		if target == "" {
			target = "?"
		}
		fmt.Printf("dangling %s %s %s -> %s\n", item.Agent, item.Skill, item.Path, target)
	}
	for _, item := range report.Missing {
		fmt.Printf("missing %s %s %s\n", item.Agent, item.Skill, item.Path)
	}
	for _, item := range report.MCP {
		name := item.Server
		if name == "" {
			name = "-"
		}
		fmt.Printf("mcp %s %s %s %s\n", item.Agent, name, item.Path, item.Reason)
	}
}
