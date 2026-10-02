# Changelog

## 0.1.0 — 2026-10-02

Initial production release (darwin-arm64).

- TUI + CLI to browse/sync skills & MCP across Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor
- Canonical hubs: `~/.agents/skills`, `~/.agents/mcp.json`, project `.agents/skills` / `.mcp.json`
- Conflict policies: `skip` | `keep-source` | `keep-target` (CLI `--on-conflict`, TUI `a`/`b`/`s`)
- Self-update from GitHub Releases (`agent-skills-tui-vX.Y.Z-darwin-arm64` + `.sha256`)
- Unit tests for normalize, MCP adapters, sync plan, conflicts, hub env merge

### Asset naming (do not rename without updating `ast_core::update`)

- `agent-skills-tui-vX.Y.Z-darwin-arm64`
- `agent-skills-tui-vX.Y.Z-darwin-arm64.sha256`
