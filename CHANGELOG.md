# Changelog

## 0.1.1 — 2026-10-02

Rename project to **synca** (repo, crates, binary, install paths, release assets).

- Binary / CLI: `synca`
- Crates: `synca-core`, `synca-cli`, `synca-tui`
- Repo: `ngosangns/synca` (old `ngosangns/agent-skills-tui` redirects)
- Install: `~/.local/share/synca/bin/` + symlink `~/.local/bin/synca`
- Assets: `synca-vX.Y.Z-darwin-arm64` (+ `.sha256`)

## 0.1.0 — 2026-10-02

Initial production release (darwin-arm64), published under the previous name `agent-skills-tui`.

- TUI + CLI to browse/sync skills & MCP across Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor
- Canonical hubs: `~/.agents/skills`, `~/.agents/mcp.json`, project `.agents/skills` / `.mcp.json`
- Conflict policies: `skip` | `keep-source` | `keep-target` (CLI `--on-conflict`, TUI `a`/`b`/`s`)
- Self-update from GitHub Releases
- Unit tests for normalize, MCP adapters, sync plan, conflicts, hub env merge

### Asset naming (current — do not rename without updating `synca_core::update`)

- `synca-vX.Y.Z-darwin-arm64`
- `synca-vX.Y.Z-darwin-arm64.sha256`
