package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ngosangns/synca/go/internal/core"
)

const exportSecretsHint = "the catalog stores env and header values in plain text; keep it private"

func runMcpExport(scope core.Scope, f *flags, jsonOut bool) error {
	catalog, err := core.BuildMcpCatalog(scope, cwdOf(f))
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	out := f.str("out", "")
	if out != "" {
		path, err := core.WriteMcpCatalogFile(out, catalog)
		if err != nil {
			return asCommandErr(jsonOut, err)
		}
		if jsonOut {
			return writeJSON(catalog)
		}
		fmt.Printf("wrote %s (%d server(s))\n%s\n", path, len(catalog.Servers), exportSecretsHint)
		return nil
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	fmt.Println(string(data))
	if !jsonOut {
		fmt.Fprintln(os.Stderr, exportSecretsHint)
	}
	return nil
}

func runMcpImport(scope core.Scope, f *flags, jsonOut bool) error {
	if len(f.pos) < 1 {
		return failUsage(jsonOut, "usage: synca mcp import <file|url> [--scope --cwd --agents --dry-run --yes --json]", "see: synca mcp --help")
	}
	source := f.pos[0]
	catalog, err := core.LoadMcpCatalog(source)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	dry := f.bools["dry-run"]
	plan, diffs, err := core.PlanMcpCatalogImport(scope, cwdOf(f), source, catalog, filter)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	if !dry && !f.bools["yes"] && len(plan.Actions) > 0 && !stdinIsTerminal() {
		return failNeedsConfirm(jsonOut, "mcp import writes the catalog's servers into the hub and agent configs", "pass --yes")
	}
	plan.DryRun = dry
	var log []string
	if !dry {
		log, err = core.ApplyManagePlan(plan, nil)
		if err != nil {
			return applyFailed(jsonOut, log, err)
		}
	}
	if jsonOut {
		return writeManageCatalogJSON(plan, dry, log, diffs)
	}
	printManagePlan(plan)
	return printPlanResult(log, dry, "")
}
