# Changelog

## 0.2.1 — 2026-10-08

- **Fix (skill reports "mismatch" even after sync):** when an agent's skills dir is a symlink to the canonical one (`.pi/skills -> ../.agents/skills`), sync linked the canonical skill into that root, which replaced the canonical copy with a symlink to itself. The skill then resolved to nothing, every agent link dangled, and the app kept showing "Agent copies differ". Sync now skips roots that alias the canonical dir, refuses to link a path to itself, and repairs the self-referencing link on the next run.
- **Fix:** dangling symlinks are no longer counted as differing content, so they cannot cause a false mismatch. Dead links are repaired to point at the canonical copy, and the sync source is never a dead link.
- **Fix:** `skills remove` and `skills install` no longer act on a skills root that aliases the canonical dir, so unlinking from such an agent cannot delete the canonical skill.
- **Fix (macOS app):** CLI output written just before the process exited could be lost (about 1 in 600 calls under load), so a fast failure or a "nothing to remove" message could look like a silent success. Each pipe is now drained to EOF with `DispatchIO`, which holds no thread while waiting, so a timeout still fires on time on a loaded 3-core runner.
- **Taskfile:** `task install` builds and installs the CLI and the macOS app; `task --list` shows the rest.

## 0.2.0 — 2026-10-08

- **Cleanup:** removed the PHP/NativePHP desktop app (`desktop/`) and its workflow, both terminal UIs (Ratatui in Rust, Bubble Tea in Go), the unused `filter_missing` / `agents_present_in_plan` helpers, unused Cargo dependencies, `GROK_TASK.md`, and stale TUI docs. `synca` with no arguments now prints help, and `synca tui` is gone.
- **Go is the only CLI:** the Rust workspace (`crates/`, `bin/`, `Cargo.*`) is removed. CI and the release workflow now `go vet` / `go test` / build the Go binary, and the release job refuses a tag that does not match `Version` in `go/internal/core/update.go`. The macOS app tests run in CI against the freshly built CLI. The Rust test suite was ported to Go (10 -> 35 tests).
- **Fix (MCP env merge):** the Go JSON writers dropped the source `env` values when the target already had an `env`, because of a `map[string]string` vs `map[string]any` type assertion. Source values now win and target-only secrets are kept (the Rust behaviour).
- **macOS app, conflict resolution:** a sync conflict now shows both copies (agent, path, hash, file count) and the diff between them before you choose Skip / Keep source / Keep target. Skills list every changed file (modified, only in source, only in target) with unified or side-by-side line diffs and collapsed unchanged runs; binary and oversized files are not diffed. MCP conflicts diff the normalized config. Each side is tagged "will be kept" / "will be replaced" for the chosen resolution, and "Resolve all" applies one choice to every conflict.
- **Fix (keep-target):** `--on-conflict keep-target` kept the first non-canonical copy, which could be a symlink to the canonical copy, so "keep target" silently kept the source. It now keeps the first copy whose content differs from the source (Go and Rust).
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
