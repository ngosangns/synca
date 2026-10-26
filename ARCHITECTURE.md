# How this app is built

Synca copies skills and MCP config. That work is file IO. The product in this repo is the Go module. The macOS window is `synca-gpui` in the sibling repo `ns-gpui`. It calls this CLI. This repo has no UI.

## What the Go module is

| Package | Owns |
| --- | --- |
| `cmd/synca` | Process entry. Exits non-zero and prints the error. |
| `internal/cli` | Flag parsing and stdout. No copy of the sync rules. |
| `internal/core` | Discover agents, read skills, MCP and hooks, plan a sync, write it. Hook trust, lock, backups and format-preserving edits live in `hooks_*`. |
| `internal/updater` | `synca update`. GitHub release into the versioned bin dir. |
| `skills/synca` | The bundled skill and its helper scripts. `go/internal/cli/skill` embeds a byte-identical copy; a test fails on drift. |

`internal/core` stays one package until a second caller needs a seam.

The CLI is the product. The window does not reimplement a sync. There is no daemon.
The skill scripts (`skills/synca/scripts/`) are thin read-only wrappers around the CLI (`status.sh`, `plan.sh`) plus a hook scaffold. They carry no sync rules; `hook-scaffold.sh` only writes a script file and never trusts a hook.

## Docs

`README.md` is the user guide. `docs/hooks.md` is the hooks reference. `docs/plans/hooks-sync.md` is the design and remaining work for the agents that are not implemented. `REQUIREMENTS.md` records the locked scope.

## Scope

In scope: skills, MCP and hooks for the agents listed in the README, user scope and project scope, and a dry-run plan before a write.

Out of scope: a UI in this repo, a command palette, an extension host, collaboration, editing file contents in the window, and a second sync engine.

## GitHub

`ci.yml` vets, tests, and builds the Go module. `release.yml` publishes the one darwin-arm64 asset.
