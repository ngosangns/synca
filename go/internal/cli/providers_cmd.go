package cli

import (
	"fmt"

	"github.com/ngosangns/synca/go/internal/core"
)

func printProvidersHelp() {
	fmt.Print(`USAGE:
  synca providers list [--scope user|project] [--cwd <dir>] [--json]

Lists every agent the skills.sh CLI supports, with the skills directory it reads
in this scope, whether that directory exists, and how many skills it holds.
managed is true for the agents synca syncs to. The table comes from the skills
CLI and is not fetched from the network.
`)
}

func runProviders(args []string) error {
	jsonOut := jsonIn(args)
	if len(args) == 0 {
		return failUsage(jsonOut, "usage: synca providers list [--scope --cwd --json]", "see: synca providers --help")
	}
	if isHelp(args[0]) || hasHelp(args[1:]) {
		printProvidersHelp()
		return nil
	}
	if args[0] != "list" {
		return failUsage(jsonOut, fmt.Sprintf("unknown providers command '%s'", args[0]), "see: synca providers --help")
	}
	f := parseFlags(args[1:], commonBool)
	if bad := f.unknownFlag(allow("scope", "cwd", "json", "help")); bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg("--"+bad), "see: synca providers --help")
	}
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca providers list [--scope --cwd --json]", "see: synca providers --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca providers --help")
	}
	doc, err := core.ListProviders(scope, cwdOf(f))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if jsonOut {
		return writeJSON(doc)
	}
	present, managed := 0, 0
	for _, p := range doc.Providers {
		state := "not installed"
		switch {
		case p.Managed:
			state = "managed"
			managed++
		case p.Present:
			state = "installed"
		}
		if p.Present {
			present++
		}
		where := "-"
		if p.Path != nil {
			where = *p.Path
		}
		fmt.Printf("  %-26s %-14s %3d skills  %s\n", p.Name, state, p.Skills, where)
	}
	fmt.Printf("%d provider(s), %d present, %d managed by synca\n", doc.Total, present, managed)
	return nil
}
