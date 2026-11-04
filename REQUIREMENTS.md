# synca — Requirements (locked 2026-10-02)

The agent list below was amended when Pi and OMP were dropped. Existing Pi and OMP directories are ignored and never deleted. The CLI list was amended for hooks and the v0.3.0 command surface.

## Goal
CLI (macOS arm64 first) to **browse and sync skills + MCP configs + hooks** across coding agents. The window is `synca-gpui` in the sibling repo `ns-gpui`.

## Stack
- CLI: Go (`go/`: `internal/core` scan/sync/manage/update, `internal/cli` commands, `cmd/synca`)
- Binary name: `synca`

## Agents in scope (v1)
Grok Build, Devin, Kiro CLI, OpenCode, Claude Code (compat paths), Cursor (compat paths), plus the canonical `agents` hub.
Out of scope: crush, goose, omp, pi.

## Canonical paths
Each skill and MCP server has one home, chosen by origin. A remote skill (lock `sourceType` other than `local`) and a registry MCP server live in `.agents`. A folder install, `mcp add`, and the bundled `synca` skill live in `.synca/local`. Sync writes the other managed agents from that home. It does not copy a local item into `.agents`, or an installed item into `.synca/local`. An agent that only reads `.agents/skills` therefore does not see local skills. Project `.agents/` is added to `.git/info/exclude`. `.synca/local` stays committable. A tracked `.gitignore` is not edited.

- Local skills: `~/.synca/local/skills/<name>/` and `<git-root>/.synca/local/skills/<name>/`
- Installed skills: `~/.agents/skills/<name>/` and `<git-root>/.agents/skills/<name>/` (real directories)
- Local MCP: `~/.synca/local/mcp.json` and `<git-root>/.synca/local/mcp.json` (`{ "mcpServers": { ... } }`)
- Installed MCP: `~/.agents/mcp.json` and `<git-root>/.agents/mcp.json`
- Claude project writer: `<git-root>/.mcp.json` (translated copy, not a home)
- Hooks user hub: `~/.agents/hooks.json` (`{ "version": 1, "hooks": { ... } }`), scripts in `~/.agents/hooks/<id>/`; user scope only; written to Claude, Grok and Devin (design for Kiro, OpenCode, Cursor and project scope: `docs/plans/hooks-sync.md`). Hooks are not stored under `.synca/local`.

### Per-agent skill roots (read; write via symlink to the owning home when syncing)
**User:** `~/.grok/skills`, `~/.config/devin/skills`, `~/.kiro/skills`, `~/.config/opencode/skills`, `~/.claude/skills`, `~/.cursor/skills`
**Project:** `.grok/skills`, `.devin/skills`, `.kiro/skills`, `.opencode/skills`, `.claude/skills`, `.cursor/skills`

### Per-agent MCP configs (read/write adapters)
**User:** `~/.grok/config.toml` `[mcp_servers.<name>]`; `~/.config/devin/mcp_config.json`; `~/.kiro/settings/mcp.json`; `~/.config/opencode/opencode.json` key `mcp`; `~/.claude.json` `mcpServers`; `~/.cursor/mcp.json`
**Project:** `.mcp.json` (Claude); `.grok/config.toml`; `.kiro/settings/mcp.json`; project `opencode.json`; `.cursor/mcp.json`

## Inventory
Two scopes: **User** and **Project** (cwd → git root; if not a git repo, show a clear empty/error state).
Each scope lists Skills and MCP servers.

**Dedupe**
- Skill key: normalize(frontmatter `name` || folder name)
- MCP key: normalize(server name)
- Deep equality for conflict: skill = hash of SKILL.md (or dir); MCP = normalized `{transport, command|url}` ignoring secret env values and enable flags

**Detail:** which agents have it, real path (file/symlink→target), content mismatch flag, enabled if known.

## Sync behavior
### Skills
1. Copy a missing skill into the home its origin selects (`.agents/skills` when remote, `.synca/local/skills` otherwise).
2. Symlink each managed agent to that home. A real directory whose full tree matches the home is replaced with that symlink. Skip when the trees differ. A matching copy in the other home is removed. It is not replaced with a symlink.
3. Conflict (same name, different hash): stop; show summary; user chooses keep-source / keep-target / skip. Never silent overwrite.

### MCP
1. Upsert into the home the origin selects (`.agents/mcp.json` for a registry server, `.synca/local/mcp.json` otherwise), then write each managed agent, including project `.mcp.json` for Claude. A copy in the other home is removed.
2. Writers per agent format (TOML↔JSON↔OpenCode `mcp`; command string↔argv; type/transport; preserve existing env secrets on target if source lacks key).
3. Support sync one / sync all missing / selected agents; dry-run preview.

### Hooks
1. Hub to agents only; never import an agent's hooks into the hub.
2. A hook runs code: a new or changed hook is written only after a person approves it (`synca hooks trust`, terminal only). Unapproved hooks are skipped and listed.
3. Edit shared settings files in place (only the `hooks` member), keep foreign hooks, back up, write atomically, never touch an unparseable file.

## CLI
```
synca                                         # print help
synca skills list|install|remove [--scope user|project] [--agents a,b] [--dry-run] [--json]
synca mcp list|add|remove [--scope user|project] [--agents a,b] [--dry-run] [--json]
synca hooks list|add|remove|trust|untrust|sync [--agents a,b] [--dry-run] [--json]
synca sync skills|mcp|hooks|all [--scope user|project] [--agents a,b] [--dry-run]
              [--on-conflict skip|keep-source|keep-target] [--key name] [--yes] [--strict] [--no-hooks] [--json]
synca provider clear <name> [--scope user|project] [--yes] [--dry-run] [--json]
synca doctor [--scope user|project] [--json]
synca skill install|check [--dest DIR] [--json]
synca version [--json]
synca schema [--json] [<command>]
synca update [--check] [--json] [--force]
```

The bundled skill (`skills/synca`, embedded in the binary) ships read-only helpers `status.sh` and `plan.sh` and the side-effect-limited `hook-scaffold.sh`.

## Update
GitHub Releases `ngosangns/synca`, install under `~/.local/share/synca/bin/synca-X.Y.Z` + symlink `~/.local/bin/synca`. Asset: `synca-vX.Y.Z-darwin-arm64`. The updater requires the GitHub sha256 digest, does not downgrade, and does not replace any other path.

## Non-goals v1
In-app skill editor; running MCP servers; session tokens; background auto-sync; Windows/Linux (phase 2); crush/goose.

## Success criteria
1. `make build` in `go/` succeeds on darwin-arm64
2. `skills list` / `mcp list` show the real user skills and MCP servers on this machine
3. `sync skills --scope user --dry-run` prints planned symlink/copy actions
4. `update --check` hits GitHub API (graceful if no release yet)
5. README with install + usage
