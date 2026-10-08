# synca — Requirements (locked 2026-10-02)

## Goal
CLI + native macOS app (macOS arm64 first) to **browse and sync skills + MCP configs** across coding agents.

## Stack
- CLI: Go (`go/`: `internal/core` scan/sync/manage/update, `internal/cli` commands, `cmd/synca`)
- macOS app: SwiftUI (`macos/`), shells out to the CLI
- Binary name: `synca`

## Agents in scope (v1)
Grok Build, Devin, OMP, Pi, Kiro CLI, OpenCode, Claude Code (compat paths), Cursor (compat paths).
Out of scope v1: crush, goose.

## Canonical paths
- Skills user: `~/.agents/skills/<name>/SKILL.md`
- Skills project: `<git-root>/.agents/skills/<name>/SKILL.md`
- MCP user hub: `~/.agents/mcp.json` (`{ "mcpServers": { ... } }`)
- MCP project canonical: `<git-root>/.mcp.json`

### Per-agent skill roots (read; write via symlink to canonical when syncing)
**User:** `~/.grok/skills`, `~/.config/devin/skills`, `~/.config/cognition/skills`, `~/.pi/agent/skills`, `~/.kiro/skills`, `~/.config/opencode/skills`, `~/.claude/skills`, `~/.cursor/skills`, `~/.agents/skills`
**Project:** `.grok/skills`, `.devin/skills`, `.cognition/skills`, `.omp/skills`, `.pi/skills`, `.kiro/skills`, `.opencode/skills`, `.claude/skills`, `.cursor/skills`, `.agents/skills`

### Per-agent MCP configs (read/write adapters)
**User:** `~/.grok/config.toml` `[mcp_servers.<name>]`; `~/.config/devin/mcp_config.json`; `~/.omp/agent/mcp.json`; `~/.pi/agent/mcp.json`; `~/.kiro/settings/mcp.json`; `~/.config/opencode/opencode.json` key `mcp`; `~/.claude.json` `mcpServers`; `~/.cursor/mcp.json`; hub `~/.agents/mcp.json`
**Project:** `.mcp.json`; `.grok/config.toml`; `.omp/mcp.json`; `.pi/mcp.json`; `.kiro/settings/mcp.json`; project `opencode.json`; `.cursor/mcp.json`

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
1. Ensure present in canonical `.agents/skills` (copy if missing).
2. For each target agent path: symlink to canonical (create parent dirs). Skip if already same target/inode/hash.
3. Conflict (same name, different hash): stop; show summary; user chooses keep-source / keep-target / skip. Never silent overwrite.

### MCP
1. Promote/merge into hub (`~/.agents/mcp.json` or `.mcp.json`).
2. Writers per agent format (TOML↔JSON↔OpenCode `mcp`; command string↔argv; type/transport; preserve existing env secrets on target if source lacks key).
3. Support sync one / sync all missing / selected agents; dry-run preview.

## CLI
```
synca                 # print help
synca skills list [--scope user|project] [--json]
synca mcp list [--scope user|project] [--json]
synca sync skills|mcp [--scope user|project] [--agents a,b] [--dry-run]
synca update [--check] [--json] [--force]
```

## Update
GitHub Releases `ngosangns/synca`, install under `~/.local/share/synca/bin/` + symlink `~/.local/bin/synca`. Asset naming TBD (`synca-vX.Y.Z-darwin-arm64` + sha256).

## Non-goals v1
In-app skill editor; running MCP servers; session tokens; background auto-sync; Windows/Linux (phase 2); crush/goose.

## Success criteria
1. `make build` in `go/` succeeds on darwin-arm64
2. `skills list` / `mcp list` show the real user skills and MCP servers on this machine
3. `sync skills --scope user --dry-run` prints planned symlink/copy actions
4. `update --check` hits GitHub API (graceful if no release yet)
5. README with install + usage
