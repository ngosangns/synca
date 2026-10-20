# How this app is built

Synca copies skills and MCP config. That work is file IO. The product in this repo is the Go module. The macOS window is `synca-gpui` in the sibling repo `ns-gpui`. It calls this CLI. This repo has no UI.

## What the Go module is

| Package | Owns |
| --- | --- |
| `cmd/synca` | Process entry. Exits non-zero and prints the error. |
| `internal/cli` | Flag parsing and stdout. No copy of the sync rules. |
| `internal/core` | Discover agents, read skills and MCP, plan a sync, write it. |
| `internal/updater` | `synca update`. GitHub release into the versioned bin dir. |

`internal/core` stays one package until a second caller needs a seam.

The CLI is the product. The window does not reimplement a sync. There is no daemon.

## Scope

In scope: skills and MCP for the agents listed in the README, user scope and project scope, and a dry-run plan before a write.

Out of scope: a UI in this repo, a command palette, an extension host, collaboration, editing file contents in the window, and a second sync engine.

## GitHub

`ci.yml` vets, tests, and builds the Go module. `release.yml` publishes the one darwin-arm64 asset.
