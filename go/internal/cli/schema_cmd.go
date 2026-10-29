package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ngosangns/synca/go/internal/schema"
)

// commandSpec is one row of `synca schema`.
// schemaFile is empty when that command has no JSON output yet.
type commandSpec struct {
	path        string
	summary     string
	mutates     bool
	destructive bool
	flags       []string
	schemaFile  string
}

func (s commandSpec) letter() string {
	if s.destructive {
		return "D"
	}
	if s.mutates {
		return "M"
	}
	return "R"
}

type catalogDocument struct {
	Name          string                  `json:"name"`
	SchemaVersion int                     `json:"schemaVersion"`
	Commands      map[string]commandEntry `json:"commands"`
	Error         json.RawMessage         `json:"error"`
}

type commandEntry struct {
	Summary     string          `json:"summary"`
	Mutates     bool            `json:"mutates"`
	Destructive bool            `json:"destructive"`
	Flags       []string        `json:"flags"`
	Output      json.RawMessage `json:"output"`
}

func commandSpecs() []commandSpec {
	return []commandSpec{
		{
			path:       "skills list",
			summary:    "List skills and which agents have each one",
			flags:      []string{"--scope", "--cwd", "--json"},
			schemaFile: "skills-list.json",
		},
		{
			path:       "skills install",
			summary:    "Copy a skill into the canonical directory and link it to agents",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:       "skills remove",
			summary:    "Unlink a skill from agent dirs. --purge also deletes the canonical copy and is destructive",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--purge", "--yes", "--dry-run", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:       "skills search",
			summary:    "Search the skills.sh registry by name, source or description",
			flags:      []string{"--owner", "--limit", "--json"},
			schemaFile: "registry-search.json",
		},
		{
			path:       "skills browse",
			summary:    "List one page of the skills.sh leaderboard (all-time, trending or hot). Pages start at 0",
			flags:      []string{"--view", "--page", "--source", "--json"},
			schemaFile: "registry-browse.json",
		},
		{
			path:       "skills official",
			summary:    "List the curated first-party skills. Needs SKILLS_API_TOKEN (exit 7 without it)",
			flags:      []string{"--json"},
			schemaFile: "registry-official.json",
		},
		{
			path:       "skills info",
			summary:    "Show the files of a registry skill. --contents adds the text of each file",
			flags:      []string{"--contents", "--json"},
			schemaFile: "registry-info.json",
		},
		{
			path:       "skills audit",
			summary:    "Show the security audits of a registry skill. An empty list means nobody audited it yet",
			flags:      []string{"--json"},
			schemaFile: "registry-audit.json",
		},
		{
			path:       "skills use",
			summary:    "Print a prompt that carries one skill's SKILL.md, without installing it",
			flags:      []string{"--skill", "--json"},
			schemaFile: "skills-use.json",
		},
		{
			path:       "skills discover",
			summary:    "List the skills inside a repository or folder without installing",
			flags:      []string{"--json"},
			schemaFile: "skills-discover.json",
		},
		{
			path:       "skills update",
			summary:    "Reinstall the remote skills whose source changed. --dry-run only checks",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:       "skills restore",
			summary:    "Reinstall the remote skills in the lock file that are missing on disk",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:       "skills init",
			summary:    "Create a new skill folder with a SKILL.md stub",
			mutates:    true,
			flags:      []string{"--dir", "--json"},
			schemaFile: "skills-init.json",
		},
		{
			path:       "mcp list",
			summary:    "List MCP servers. Env values, URL passwords, and secret-looking args are masked unless --reveal",
			flags:      []string{"--scope", "--cwd", "--json", "--reveal"},
			schemaFile: "mcp-list.json",
		},
		{
			path:       "mcp add",
			summary:    "Add an MCP server to the hub and agent configs. --env KEY=VAL is masked in the plan",
			mutates:    true,
			flags:      []string{"--transport", "--command", "--url", "--env", "--enabled", "--scope", "--cwd", "--agents", "--dry-run", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:        "mcp remove",
			summary:     "Remove an MCP server from the hub and from agent configs. --keep-hub with --agents removes it from those agents only",
			mutates:     true,
			destructive: true,
			flags:       []string{"--scope", "--cwd", "--agents", "--keep-hub", "--yes", "--dry-run", "--json"},
			schemaFile:  "manage-plan.json",
		},
		{
			path:       "mcp probe",
			summary:    "Connect to each MCP server and estimate its context cost from the live tool definitions. Runs stdio commands locally; results are cached for mcp list",
			flags:      []string{"--scope", "--cwd", "--timeout", "--json"},
			schemaFile: "mcp-probe.json",
		},
		{
			path:       "mcp search",
			summary:    "Search the official MCP registry by server name",
			flags:      []string{"--limit", "--cursor", "--json"},
			schemaFile: "mcp-registry-search.json",
		},
		{
			path:       "mcp browse",
			summary:    "List one page of the official MCP registry in registry name order",
			flags:      []string{"--limit", "--cursor", "--json"},
			schemaFile: "mcp-registry-browse.json",
		},
		{
			path:       "mcp info",
			summary:    "Show a registry MCP server, its packages and remotes. Env, header and variable values are never printed",
			flags:      []string{"--version", "--json"},
			schemaFile: "mcp-registry-info.json",
		},
		{
			path:       "mcp install",
			summary:    "Install an MCP server from the official registry. --dry-run still fetches the registry and writes nothing; an existing key needs --force",
			mutates:    true,
			flags:      []string{"--version", "--name", "--transport", "--package", "--env", "--arg", "--enabled", "--scope", "--cwd", "--agents", "--dry-run", "--force", "--json"},
			schemaFile: "manage-plan.json",
		},
		{
			path:       "sync skills",
			summary:    "Sync skills to each agent. First fetches missing remote skills from skills.sh (--update also refreshes changed ones; --no-registry skips this). --json prints the plan. keep-source and keep-target are destructive and need --yes off a TTY",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--on-conflict", "--key", "--yes", "--update", "--no-registry", "--json"},
			schemaFile: "sync-plan.json",
		},
		{
			path:       "sync mcp",
			summary:    "Sync MCP servers to each agent. --json prints the plan. keep-source and keep-target are destructive and need --yes off a TTY",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--on-conflict", "--key", "--yes", "--json"},
			schemaFile: "sync-plan.json",
		},
		{
			path:       "sync all",
			summary:    "Sync skills, MCP servers and, when ~/.agents/hooks.json exists, approved hooks. Skills are first fetched from skills.sh when missing (--update also refreshes changed ones; --no-registry skips this). --json prints the plan. keep-source and keep-target are destructive and need --yes off a TTY",
			mutates:    true,
			flags:      []string{"--scope", "--cwd", "--agents", "--dry-run", "--on-conflict", "--yes", "--strict", "--no-hooks", "--update", "--no-registry", "--json"},
			schemaFile: "sync-plan.json",
		},
		{
			path:       "sync hooks",
			summary:    "Write approved hub hooks to claude, grok and devin. Unapproved hooks are skipped and listed (exit 7 with --strict)",
			mutates:    true,
			flags:      []string{"--agents", "--dry-run", "--on-conflict", "--strict", "--yes", "--json"},
			schemaFile: "hooks-plan.json",
		},
		{
			path:       "hooks list",
			summary:    "List hub hooks with approval state and per-agent sync state",
			flags:      []string{"--agents", "--json"},
			schemaFile: "hooks-list.json",
		},
		{
			path:       "hooks add",
			summary:    "Add a hook to the hub. It is not written to agents until a person runs hooks trust",
			mutates:    true,
			flags:      []string{"--event", "--command", "--tools", "--timeout", "--description", "--agents", "--exclude-agents", "--on-failure", "--force", "--dry-run", "--json"},
			schemaFile: "hooks-change.json",
		},
		{
			path:       "hooks remove",
			summary:    "Remove a hook from the hub and withdraw its approval. hooks sync removes it from agents",
			mutates:    true,
			flags:      []string{"--dry-run", "--json"},
			schemaFile: "hooks-change.json",
		},
		{
			path:       "hooks untrust",
			summary:    "Withdraw the approval of a hook. hooks sync removes it from agents",
			mutates:    true,
			flags:      []string{"--json"},
			schemaFile: "hooks-trust.json",
		},
		{
			path:       "hooks sync",
			summary:    "Same as sync hooks",
			mutates:    true,
			flags:      []string{"--agents", "--dry-run", "--on-conflict", "--strict", "--yes", "--json"},
			schemaFile: "hooks-plan.json",
		},
		{
			path:        "provider clear",
			summary:     "Delete one agent's skill copies and MCP entries in this scope",
			mutates:     true,
			destructive: true,
			flags:       []string{"--scope", "--cwd", "--yes", "--dry-run", "--json"},
			schemaFile:  "provider-clear.json",
		},
		{
			path:       "providers list",
			summary:    "List every agent the skills.sh CLI supports, with its skills directory in this scope and how many skills it holds",
			flags:      []string{"--scope", "--cwd", "--json"},
			schemaFile: "providers-list.json",
		},
		{
			path:       "doctor",
			summary:    "Report skill drift, dangling links, missing links, and MCP differences",
			flags:      []string{"--scope", "--cwd", "--json"},
			schemaFile: "doctor.json",
		},
		{
			path:       "skill install",
			summary:    "Install the embedded synca skill into a directory",
			mutates:    true,
			flags:      []string{"--dest", "--json"},
			schemaFile: "skill-install.json",
		},
		{
			path:       "skill check",
			summary:    "Compare the installed synca skill with the embedded copy",
			flags:      []string{"--dest", "--json"},
			schemaFile: "skill-check.json",
		},
		{
			path:       "update",
			summary:    "Download and install the latest release. --check only reports",
			mutates:    true,
			flags:      []string{"--check", "--json", "--force"},
			schemaFile: "update.json",
		},
		{
			path:       "version",
			summary:    "Print the synca version",
			flags:      []string{"--json"},
			schemaFile: "version.json",
		},
		{
			path:       "schema",
			summary:    "Print the v1 command catalog and output schemas",
			flags:      []string{"--json"},
			schemaFile: "schema.json",
		},
	}
}

func findSpec(specs []commandSpec, path string) (commandSpec, bool) {
	for _, spec := range specs {
		if spec.path == path {
			return spec, true
		}
	}
	return commandSpec{}, false
}

func (s commandSpec) entry() (commandEntry, error) {
	entry := commandEntry{
		Summary:     s.summary,
		Mutates:     s.mutates,
		Destructive: s.destructive,
		Flags:       append([]string{}, s.flags...),
		Output:      json.RawMessage("null"),
	}
	if s.schemaFile == "" {
		return entry, nil
	}
	raw, err := schema.Read(s.schemaFile)
	if err != nil {
		return commandEntry{}, err
	}
	entry.Output = json.RawMessage(bytes.TrimSpace(raw))
	return entry, nil
}

func buildCatalog() (catalogDocument, error) {
	specs := commandSpecs()
	doc := catalogDocument{
		Name:          "synca",
		SchemaVersion: schema.Version,
		Commands:      make(map[string]commandEntry, len(specs)),
	}
	raw, err := schema.Read("error.json")
	if err != nil {
		return catalogDocument{}, err
	}
	doc.Error = json.RawMessage(bytes.TrimSpace(raw))
	for _, spec := range specs {
		entry, err := spec.entry()
		if err != nil {
			return catalogDocument{}, err
		}
		doc.Commands[spec.path] = entry
	}
	return doc, nil
}

func runSchema(args []string) error {
	for _, arg := range args {
		if isHelp(arg) {
			printSchemaHelp()
			return nil
		}
	}
	jsonOut := false
	var pos []string
	var bad string
	for _, arg := range args {
		switch {
		case arg == "--json":
			jsonOut = true
		case strings.HasPrefix(arg, "-"):
			if bad == "" {
				bad = arg
			}
		default:
			pos = append(pos, arg)
		}
	}
	if bad != "" {
		return failUsage(jsonOut, "unknown flag "+quoteArg(bad), "see: synca schema --help")
	}
	specs := commandSpecs()
	path := strings.Join(pos, " ")
	if path == "" {
		if jsonOut {
			doc, err := buildCatalog()
			if err != nil {
				return err
			}
			return writeJSON(doc)
		}
		for _, spec := range specs {
			fmt.Printf("%s  %s  %s\n", spec.path, spec.letter(), spec.summary)
		}
		return nil
	}
	spec, ok := findSpec(specs, path)
	if !ok {
		return failUsage(jsonOut, "unknown command "+quoteArg(path), "see: synca schema")
	}
	if jsonOut {
		entry, err := spec.entry()
		if err != nil {
			return err
		}
		return writeJSON(entry)
	}
	fmt.Printf("%s  %s  %s\n", spec.path, spec.letter(), spec.summary)
	return nil
}

func printSchemaHelp() {
	fmt.Print(`USAGE:
  synca schema [--json] [<command>]

Lists commands as "path  R|M|D  summary". With --json, prints the v1 schema catalog.
With a command, prints that one entry. An unknown command exits 2.
`)
}
