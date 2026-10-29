# Changelog

## 0.5.0

- **mcp probe:** `synca mcp probe [names...] [--scope --cwd --timeout SECONDS --json]` connects to each server (initialize, then tools/list, resources/list, prompts/list — streamable HTTP, legacy SSE and stdio) and estimates its context cost: every definition serialized as JSON, bytes / 4. Results are cached in `.mcp-probe.json` (user: `$XDG_STATE_HOME/synca/`, else `~/.agents/`; project: beside `.mcp.json`) keyed by a config fingerprint, so `mcp list` serves them until the config changes; `mcp list --json` gains `tokens`, `probed_at`, `probe_ok` and `probe_error`. A failed server is a row in the output, not a command failure; disabled servers are skipped; four servers run at once; each gets `--timeout` seconds (default 15). The cache holds measurements only — no env, header or URL secret values.
- **macOS window:** the MCP Tokens column shows the probe measurement when one exists (tooltip says "Measured from the server" with how long ago), falls back to the config estimate with the probe error when a probe failed, and a row menu action "Probe tokens" (one or all) runs the probe from the app.
- **Sidebar tables:** the skills and MCP tables sort independently — click a column head (Name, Origin, Sync, Tokens) to sort ascending, again for descending, a third time restores load order; Origin orders local before remote. MCP servers without a measured token count are probed once after each load, so every enabled server shows a real number.
- **Provider display:** Settings has one merged Providers list where every agent has an on/off toggle — no read-only rows and no skills.sh branding. The six agents the CLI syncs to are on by default (saved in `synca.disabledProviders`) and keep Delete data; the other rows are off by default and save their choice in `synca.enabledProviders` until the CLI learns to sync to them. The built-in rows render even before the list loads or when the CLI fails. Saved defaults arrays with bare items (agent ids) load back — `defaults read` quotes only values that need it.

## 0.4.0

- **MCP transports:** a `remote` entry whose URL ends in `/sse` normalizes to `sse` everywhere (before it became `http`, so a server installed with `--transport sse` read back as `http` from OpenCode, mismatched forever, and `sync mcp` skipped it as a permanent conflict).
- **MCP headers:** `--header NAME=VAL` on `mcp add` and `mcp install` (repeatable). Headers round-trip through the hub, Grok, OpenCode and generic JSON configs; `mcp list --json` gains `header_keys` and masked `headers` (`--reveal` shows them); fingerprints include header names; a registry remote's required headers are satisfied with `--header` instead of refusing the install, and optional ones are saved when given.
- **MCP provenance:** a registry install records its source in the MCP lock file (`~/.agents/.mcp-lock.json`, or `<project>/.mcp-lock.json` — names and versions only, never values). `mcp list --json` gains `source`, `source_type` and `source_version`; `mcp remove` forgets the origin and `--keep-hub` keeps it.
- **mcp update:** reinstalls a server from its recorded registry source, keeping the env and header values already in the config. `--dry-run`, `--json` and `--version` behave like `mcp add`; an unchanged fingerprint reports `up to date:`, a server without a recorded source or a vanished variant errors with its name, and a newly required env var without a value asks for `--env` and leaves that server out of the plan.
- **macOS window:** MCP rows carry the same `remote`/`local` badge as skills — decided by transport (http/sse remote, stdio local) — with provenance in the tooltip, and the detail pane gains a Server section (transport, endpoint or command, enabled, env and header names, recorded source). Remote servers gain Update (a dry-run `mcp update` plan), Unlink… (agent configs only, hub kept) and Purge… actions. The install form accepts Environment and Headers lines, and the JSON import reads command arrays, `serverUrl`/`httpUrl`, `env`, `headers`, and decides http vs sse from a `remote` type's URL. A fourth Install tab (⌘4), MCP Registry, searches and browses the Official MCP Registry and prefills the install form from a server's remote or package.
- **Sidebar:** drag the edge between the sidebar and the editor to resize it (240–560pt); the width is saved and restored on launch.
- **MCP registry:** `synca mcp search|browse|info|install` use the official MCP Registry (`registry.modelcontextprotocol.io`, API v0.1, read-only). `install` maps an npm, pypi or oci package, or a streamable-http or sse remote, onto the existing manage plan; `--package`, `--transport`, `--env`, `--arg`, `--name` and `--force` pick the variant and handle an existing key. Required env vars, variables and arguments exit 2 with names only; a remote with required headers is refused and optional headers are skipped with a note. `MCP_REGISTRY_URL` and `MCP_REGISTRY_TOKEN` change the registry. New schemas: `mcp-registry-search|browse|info`.
- **skills.sh registry:** `synca skills search|browse|official|info|audit|use|discover|update|restore|init` use skills.sh as the skills backend. `search` and `browse` use the public endpoints, `info` reads the public snapshot (and the v1 detail with a token), `audit` reads the public audit endpoint, and `official` and `browse --source` need `SKILLS_API_TOKEN` and exit 7 without one. `SKILLS_API_URL` and `SKILLS_DOWNLOAD_URL` move the host. New JSON schemas: `registry-search|browse|official|info|audit`, `skills-use`, `skills-discover`, `skills-init`.
- **Origin:** `skills list --json` gains `origin` (`remote` or `local`), `source` and `source_type`, taken from the lock files `npx skills` keeps (`~/.agents/.skill-lock.json`, or `$XDG_STATE_HOME/skills/.skill-lock.json`, and `<project>/skills-lock.json`). A remote install writes the entry (`record_origin`); installing a local folder over it or `remove --purge` drops it (`forget_origin`). Unknown fields in a lock file are kept and an unparseable file is never modified.
- **Install sources:** `owner/repo@skill` picks one skill from a repository, `skills/` and `.agents/skills/` are searched for a single skill, and a `https://skills.sh/p/...` pack URL is rejected instead of cloning a wrong repository. Git URLs and refs that start with `-` are refused.
- **skills.sh as the fetch backend:** `skills install`, `skills update`, `skills restore` and `sync` get GitHub skills from skills.sh (tree id from the GitHub API, files from `/api/download`) and fall back to `git clone` when the snapshot is missing, incomplete or unreachable. `sync skills` and `sync all` first restore remote skills that are in the lock file but missing on disk (new `fetch_skill` plan action); `--update` also refreshes changed ones; `--no-registry` skips the step; `SYNCA_NO_REGISTRY=1` forces Git.
- **Providers:** `synca providers list [--scope --cwd --json]` lists all 79 agents the skills.sh CLI supports with the directory each reads in the scope, whether it exists, its skill count, `universal` and `managed` (synca syncs to six of them). The table is embedded from `skills@1.7.4`; no network. New schema `providers-list.json`.
- **Update and restore:** `skills update [name...]` compares each remote skill with its source and reinstalls the ones that changed; `--dry-run` only checks. `skills restore` reinstalls remote skills that are in the lock file but missing on disk.
- **macOS window:** each skill in the sidebar and in the detail pane has a `remote` or `local` badge, and the Install dialog has a Registry tab (search, trending, hot, all-time, official, audits, install, copy prompt) plus update and restore actions.
- **macOS window:** a skill with no description (`"description": null` in `skills list --json`) no longer makes the whole list fail with "Failed to load skills". Rows are parsed one by one, and a row that does not fit is skipped with a toast that names it.
- **Fault tolerance:** a sync or manage apply no longer stops at the first failed action. It runs the rest, skips later steps for a skill whose copy failed, and exits 5 with every failure listed (the log of what was done goes to stderr). Before, one bad agent config left later agents unsynced and hid what had already been written.
- **Config safety:** MCP writers refuse a config that does not parse (JSON with comments, truncated JSON, a `mcpServers`/`mcp`/`mcp_servers` member of the wrong type, broken TOML) and leave it byte-identical. Before, they replaced it with a file holding only the new server. Writes are atomic and keep a symlinked config (dotfile managers) as a symlink.
- **Skill copies:** copying a skill stages it beside the destination and swaps it in, so a failed copy keeps the old directory. A copy error is no longer ignored (unreadable files were skipped silently). Replacing a directory with a symlink moves the old one aside until the link exists and restores it on failure. Leftovers of an interrupted write (`.synca-*`, `*.synca-old-*`) are not listed as skills.
- **macOS window:** the add-workspace control is a raised block. The workspace menu has no card padding; each row has its own padding.
- **macOS window:** a wheel over the file tree, the file preview, or the workspace menu moves that scroller first. At its end, the same wheel moves the parent list.
- **Docs:** new `docs/hooks.md` reference (hub schema, event and tool support per agent, trust, sync actions, lossy codes, JSON shapes, backup/lock paths). README now documents the full hooks flag set, JSON documents, per-agent hook files, and the `--strict`/`--no-hooks` rules; `ARCHITECTURE.md` and `REQUIREMENTS.md` were refreshed for the v0.3.0 command surface.
- **Skill:** `skills/synca/SKILL.md` covers hook ids, add flags, trust hashes, per-agent support, conflict reasons, and accurate untrust semantics. The embedded copy is unchanged in kind and still byte-identical.
- **Skill scripts:** the bundled skill now ships `scripts/status.sh` (read-only version/doctor/skill/MCP/hook summary), `scripts/plan.sh` (read-only dry-run summary) and `scripts/hook-scaffold.sh` (creates a hook script and prints the add/trust commands; never edits the hub or trusts).
- **CLI:** the `hooks untrust` message now says that existing agent entries are left in place until `hooks remove` plus `hooks sync`, matching the implementation.

## 0.3.0

- **Hooks:** a third resource next to skills and MCP. `synca hooks list|add|remove|trust|untrust|sync` and `synca sync hooks` manage `~/.agents/hooks.json` (version 1) and write approved hooks to Claude (`~/.claude/settings.json`), Grok (own file `~/.grok/hooks/synca.json`) and Devin (`~/.config/devin/config.json`). Events use Claude's PascalCase names, tool classes (`shell`, `read`, `write`, `search`, `web`, `agent`, `mcp`) are translated per agent, and anything an agent cannot express is reported (`event_unsupported`, `tool_unmapped`, `failure_mode_weakened`) instead of dropped. Kiro, OpenCode and Cursor are designed in `docs/plans/hooks-sync.md` and not implemented yet. User scope only.
- **Trust:** a hook runs code, so a new or changed hook (definition plus its script files) is written only after `synca hooks trust <id>` is run by a person at a terminal. There is no `--yes` for it and an agent cannot approve its own hook. Approvals live in `~/.local/state/synca/hooks/trust.json` (0600).
- **Safe edits:** only the top-level `hooks` member of a shared settings file is rewritten, one matcher group per hook, other tools' hooks untouched; a lock file lets synca update or remove only its own groups; an unparseable file is never modified; every write is atomic with a backup under `~/.local/state/synca/hooks/backups/`.
- **sync all:** includes hooks when `~/.agents/hooks.json` exists (user scope), adding an optional `hooks` object to the JSON plan. Unapproved hooks are skipped and listed with exit 0; `--strict` exits 7. `--no-hooks` leaves them out. Without a hooks hub the output is unchanged.
- **Schema:** `hooks-list.json`, `hooks-plan.json`, `hooks-change.json`, `hooks-trust.json`; `sync-plan.json` gains the optional `hooks` property. New exit use: 7 is `--strict` skipped-unapproved hooks, 8 is also `hooks add` on an existing id.

## 0.2.11

- **Secrets:** `mcp list` masks every env value, a URL userinfo password, and a secret-looking arg (`--token=…`, an `Authorization` header) as `••••••`. `--reveal` prints the real values. Fingerprints and mismatch still use the real config. `mcp add --env KEY=VAL` stores the variable; the plan shows the key with a masked value.
- **CLI:** `synca version [--json]` prints the version. With `--json` it prints `name`, `version`, `schemaVersion`, `commit`, and `dirty` (`dirty` is true when the VCS working tree was modified at build time). `synca schema [--json] [<command>]` lists commands (`R`/`M`/`D`) and the embedded v1 JSON Schemas.
- **JSON plans:** `sync skills|mcp|all --json` prints one plan: `dryRun`, `scope`, `onConflict`, `actions`, `conflicts`. Apply adds `applied` and sets `dryRun` to false. Without `--json` the text plan is unchanged. `skills install`, `skills remove`, `mcp add`, and `mcp remove` accept `--json`, and `mcp add` / `mcp remove` accept `--dry-run`.
- **Exit codes:** usage is 2, a missing path or project root is 3, a failed operation is 5, a destructive command without `--yes` off a TTY is 6, and `doctor` or `skill check` drift is 8. With `--json`, every command prints one `{"error":...}` line on stdout. Without `--json`, stderr stays `synca failed: ...`. `sync --on-conflict skip` still exits 0 when conflicts remain. `update` keeps its own codes (1 failed, 2 usage) and its own JSON report.
- **Confirm:** `skills remove --purge`, `mcp remove`, and `sync` with `--on-conflict keep-source` or `keep-target` require `--yes` when stdin is not a TTY. `provider clear` requires `--yes` even on a TTY. A dry-run does not. Every command's `--help` exits 0.
- **doctor:** read-only report of skill drift (a real agent directory whose content differs from canonical), dangling symlinks, missing links, and MCP entries that differ between the hub and an agent.
- **skill:** `synca skill install [--dest DIR]` writes the embedded `skills/synca` skill. The default dest is `~/.agents/skills/synca`. `synca skill check` exits 8 on drift and 3 when the directory is missing.
- **Agents:** Pi and OMP are no longer targets. Existing `~/.pi` and `~/.omp` directories are ignored and never deleted. Pi frontmatter repair and alias collapse are gone.

## 0.2.10 — 2026-10-22

- **Internals:** skill and Pi tree hashing share one walker; duplicate test helpers removed. No behavior change.
- **Docs:** README rewritten against the current CLI, with screenshots of the Synca window (install dialog, MCP tab, narrow layout).

## 0.2.9 — 2026-10-22

- Maintenance release: remove obsolete test scaffolding.

## 0.2.8 — 2026-10-22

- **Remote skills:** `synca skills install` accepts `owner/repo` shorthand and skills.sh repository URLs, resolves requested skills inside cloned repositories, and preserves local/Git source behavior.
- **MCP aliases:** existing `local`/`remote` transport aliases continue to normalize to stdio/HTTP while SSE remains supported.
## 0.2.7 — 2026-10-21

- **Language:** the repo is English only. The bundled `synca` skill no longer lists a Vietnamese trigger phrase.

## 0.2.6 — 2026-10-20

- **Self-update:** `synca update` now requires the GitHub asset digest, compares versions as X.Y.Z, and installs by renaming a new file into `~/.local/share/synca/bin/`. It refuses a downgrade, a binary that is not the installed symlink, and a binary inside an app bundle. A sidecar `.sha256` file is no longer accepted as proof.
- **Window:** the SwiftUI app is gone from this repo. The window is `synca-gpui` in `ns-gpui`. CI tests the Go CLI only.

## 0.2.5 — 2026-10-19

- **macOS window:** chrome, tokens, and confirmation come from the shared `ns-shell` package. Layering is in `ARCHITECTURE.md`.
- **CI:** the macOS job clones `ns-shell` beside the workspace before `swift test`.

## 0.2.4 — 2026-10-17

- **macOS app, right column:** Activity and Settings share one column. A persistent rail keeps the Activity trigger at the top and the Settings gear at the bottom, so a hidden panel can be opened again. Opening one closes the other. Settings is also in the View menu (⌘,).
- **Providers:** each coding agent can be turned off. The choice is remembered, that agent is left out of sync, install, add, and remove, and skills or MCP servers that exist only there disappear from the library. The canonical hub is not a provider.
- **Delete provider data:** `synca provider clear <name>` removes that agent's skill copies and MCP server entries in the current scope. It refuses without `--yes` (unless `--dry-run`) and never deletes the hub or other keys in the agent's config. The app asks before it runs the command.
- **Library:** column headers sort skills and MCP servers, and a row that matches across agents shows an in-sync mark.
- **Skill detail:** the file outline width fits its contents.
- **Dev:** `task dev` builds the debug app and launches it.

## 0.2.3 — 2026-10-09

- **Install name:** `make install` writes `~/.local/share/synca/bin/synca-$(VERSION)`, the same path `synca update` uses. The `-go` suffix existed only so a Rust binary of the same version could stay beside it.
- **Docs:** `docs/icon.png` renders the AppIcon artwork and shows in the README.

## 0.2.2 — 2026-10-08

- **Remove Cognition:** dropped the `cognition` agent from skills entirely — the `AgentCognition` enum value, its user (`~/.config/cognition/skills`) and project (`.cognition/skills`) skill roots, and the docs. `--agents cognition` now parses to an empty filter (all agents), same as any other unknown name.

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
