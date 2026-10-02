# Changelog

## 0.1.5 — 2026-10-02

- **Bulk sync:** TUI hotkeys `S` = sync ALL skills, `M` = sync ALL MCPs, `A` = sync ALL skills+MCPs (scope = current User|Project page). Still two-key confirm (`y`/`n`) and conflict `a`/`b`/`s`. Focused sync remains `s`.
- **Footer:** 3-line bar — status, nav hotkeys, sync hotkeys (`s`/`S`/`M`/`A`) always visible.
- **CLI:** `synca sync all` (same `--scope` / `--agents` / `--dry-run` / `--on-conflict` flags as skills|mcp).

## 0.1.4 — 2026-10-02

- **TUI footer:** always show a persistent hotkey bar (Tab, [/]/Space, j/k, click/wheel, s/S, u, r, ?, q). Status / confirm / update messages render on the line above and never wipe the hotkeys. Pending modes append y/n or a/b/s to the hotkey bar.

## 0.1.3 — 2026-10-02

- **Mouse:** enable crossterm mouse capture in the TUI; click Skills/MCPs rows to select, click User/Project tabs, scroll wheel on lists (moves selection) and detail pane (scrolls). Mouse disabled on exit (hearth-style).
- **Detail:** skill pane shows frontmatter `description` (and name) prominently; shows a clear note when missing. MCP pane adds a Summary (transport / command / url) above per-agent rows.

## 0.1.2 — 2026-10-02

- Fix TUI list panes not auto-scrolling on j/k: Skills and MCPs now use ratatui `ListState` + `render_stateful_widget` so the focused row stays in the viewport.
- Reset list selection / detail scroll on Tab (User↔Project) and section switch (`[`/`]`/Space) so scroll offsets are not stale across contexts.
- Detail pane supports PageUp/PageDown scroll; scroll resets when the focused skill/MCP changes.


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
