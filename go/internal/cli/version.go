package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/ngosangns/synca/go/internal/core"
	"github.com/ngosangns/synca/go/internal/schema"
)

type versionDocument struct {
	Name          string  `json:"name"`
	Version       string  `json:"version"`
	SchemaVersion int     `json:"schemaVersion"`
	Commit        *string `json:"commit"`
	Dirty         bool    `json:"dirty"`
}

func runVersion(args []string) error {
	for _, arg := range args {
		if isHelp(arg) {
			printVersionHelp()
			return nil
		}
	}
	jsonOut := false
	var bad string
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			if bad == "" {
				bad = arg
			}
		}
	}
	if bad != "" {
		return failUsage(jsonOut, "unknown argument "+quoteArg(bad), "see: synca version --help")
	}
	if !jsonOut {
		fmt.Println("synca", core.Version)
		return nil
	}
	commit, dirty := vcsRevision()
	return writeJSON(versionDocument{
		Name:          "synca",
		Version:       core.Version,
		SchemaVersion: schema.Version,
		Commit:        commit,
		Dirty:         dirty,
	})
}

func printVersionHelp() {
	fmt.Print(`USAGE:
  synca version [--json]

Prints "synca X.Y.Z". With --json, prints name, version, schemaVersion, commit and dirty.
`)
}

func vcsRevision() (commit *string, dirty bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				revision := setting.Value
				commit = &revision
			}
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	return commit, dirty
}

func quoteArg(s string) string {
	return fmt.Sprintf("%q", s)
}
