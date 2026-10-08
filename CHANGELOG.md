# Changelog

## Unreleased

- **Fix (purge/unlink):** `skills remove` looked for `<agent root>/<key>`, but keys come from the SKILL.md `name`, not the folder. A skill in folder `gitbutler` with `name: but` reported "nothing to remove" with exit 0, so Purge/Unlink changed nothing on disk and the skill stayed in the app. It now removes the paths the scan found for that key (Go and Rust). The macOS app also reports "Nothing was removed" instead of success when the CLI removes nothing.
- **macOS app:** rewritten in SwiftUI with a layered layout: `SyncaKit` (CLI client, models, log store, file tree; unit-tested) and `synca-app` (`Model`, `Design`, `Features`). Three headed columns: Library, Details, Activity.
- **Library pane:** the User/Project switcher, project folder picker and action buttons (Sync all, Install, Add MCP, Reload, Updates) sit at the top of the skills pane. Search filters skills and MCP servers.
- **Skill detail:** shows a file-tree explorer with a file preview, plus a presence table with column headings.
- **Loading and errors:** every CLI call is async, cancellable and has a timeout. Skeleton rows, per-operation status bar, busy-disabled buttons, toasts and error states with retry. The activity log pages from the file tail instead of decoding the whole file.
- **Branding:** new app logo and `AppIcon.icns` (`make icon` regenerates it). `make test` runs the Swift tests.

## 0.1.12 — 2026-10-04

- **Pi skills:** sync repairs frontmatter Pi rejects. Invalid `name` values become the folder slug (`Make Bot UI` → `make-bot-ui`). Plain description values that contain `:` are quoted so Pi's YAML parser accepts them.
- **Pi collisions:** two directories with the same skill name and the same file tree become one directory plus a symlink. Pi dedupes that by real path. An identical project copy of a user skill is relinked the same way when sync runs with that project as cwd. Different trees are left in place.

## 0.1.11 — 2026-10-04

- **Fix:** JSON and TOML MCP writers dropped `args` when `command` was a single token and `args` were stored separately, which wrote a bare `uvx` and broke the server. Both writers now keep `command` and `args` together.
- **Tests:** writer regression for single-token command with separate args.

## 0.1.10 — 2026-10-03

- **Transport normalization:** `local` -> `stdio`; `remote`, `streamable-http`, legacy `sse` and bare URLs -> `http`. A URL ending in `/sse` stays `sse`. Clients such as Pi reject legacy SSE, and the aliases no longer show up as false conflicts across agents.
- **Command arrays:** `command: ["uvx", "a"]` (OpenCode style) splits into `command` + `args`, so one server has one fingerprint in every agent format.
- **OpenCode writer:** writes OpenCode's own schema (`local`/`remote`, array `command`, `environment`) and preserves existing env secrets. It previously wrote `mcpServers`-style entries.
- **CLI:** `mcp add --transport` accepts `local` and `remote`.
- **Tests:** transport aliases, command split, `mcp_from_cli` aliases, OpenCode writer.

## 0.1.9 — 2026-10-02

- **Install / remove:** `i` install skill (path or git URL) or add MCP; `d` delete (skill: unlink default, purge needs double-confirm; MCP: remove from hub + agents). Scope = current User|Project page only.
- **CLI:** `synca skills install|remove`, `synca mcp add|remove` with `--dry-run` / `--purge` / `--yes`.
- **Tests:** install→unlink→purge skill; MCP add/remove round-trip.

## 0.1.8 — 2026-10-02

- **Bounded lists:** Skills/MCPs panes no longer behave like an endless/wrapping list. j/k and mouse wheel **stop at the first and last row** (no rem_euclid wrap).
- **Fixed viewport:** Each pane only renders the visible window (`scroll..scroll+height`); title shows `n/N` plus `↑`/`↓` when more rows exist above/below.
- **Paging:** `PgUp`/`PgDn` jump by one pane-height on the focused list.


## 0.1.7 — 2026-10-02

- **Names:** Skill and MCP names are bold + colored in lists and detail (skills cyan, MCPs magenta); mismatch `!` is red.
- **Diff UI:** When content differs, detail pane shows source (a) vs target (b) with paths, short hashes/fingerprints, variants-by-hash, SKILL.md unified diff, and MCP field-level diff.
- **Conflict overlay:** Resolving conflicts (`a`/`b`/`s`) opens a modal with the same rich diff context.

## 0.1.6 — 2026-10-02

- **Scope lock:** Confirm/status text always names the active scope (`[user]` / `[project]`). Apply uses the plan's recorded scope (not the live tab). Help + footer clarify sync is **this page only**.
- **Safety:** `merge_plans` refuses to combine User + Project plans. Regression tests ensure skill/MCP plans never cross scopes.


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
