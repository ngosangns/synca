# agent-skills-tui — Requirements (locked 2026-10-02)

## Goal
CLI + TUI (macOS arm64 first) to **browse and sync skills + MCP configs** across coding agents.
UX/stack inspired by [ngosangns/hearth](https://github.com/ngosangns/hearth) (`hearth tui`: Ratatui + crossterm), **not** Hearth's daemon/services domain.

## Stack
- Rust edition 2021+
- Ratatui 0.30 + crossterm 0.29
- Binary name: `agent-skills-tui` (short alias optional later)
- Layout suggestion: crates `ast-core` (scan/sync/models), `ast-cli` (clap + update), `ast-tui` (desk/shell/state/actions), bin `agent-skills-tui`

## Agents in scope (v1)
Grok Build, Devin, OMP, Pi, Kiro CLI, OpenCode, Claude Code (compat paths), Cursor (compat paths).
Out of scope v1: crush, goose, Hearth domain.

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

## TUI
Two pages (Tab): **User** | **Project** (cwd → git root; if not a git repo, show clear empty/error state).
Each page: Skills list + MCPs list (sections or sub-panes).

**Dedupe**
- Skill key: normalize(frontmatter `name` || folder name)
- MCP key: normalize(server name)
- Deep equality for conflict: skill = hash of SKILL.md (or dir); MCP = normalized `{transport, command|url}` ignoring secret env values and enable flags

**Focus detail pane:** which agents have it, real path (file/symlink→target), content mismatch flag, enabled if known.

Keys (Hearth-like): Tab pages; j/k navigate; Enter/detail; `s` sync focused; `S` sync missing (with confirm); `u` update check; `q` quit; footer help; **two-keypress confirm** for destructive sync overwrite choices.

## Sync behavior
### Skills
1. Ensure present in canonical `.agents/skills` (copy if missing).
2. For each target agent path: symlink to canonical (create parent dirs). Skip if already same target/inode/hash.
3. Conflict (same name, different hash): stop; show summary; user chooses keep-source / keep-target / skip. Never silent overwrite.

### MCP
1. Promote/merge into hub (`~/.agents/mcp.json` or `.mcp.json`).
2. Writers per agent format (TOML↔JSON↔OpenCode `mcp`; command string↔argv; type/transport; preserve existing env secrets on target if source lacks key).
3. Support sync one / sync all missing / selected agents; dry-run + TUI preview.

## CLI
```
agent-skills-tui                 # open TUI (project scope = cwd)
agent-skills-tui tui
agent-skills-tui skills list [--scope user|project] [--json]
agent-skills-tui mcp list [--scope user|project] [--json]
agent-skills-tui sync skills|mcp [--scope user|project] [--agents a,b] [--dry-run]
agent-skills-tui update [--check] [--json] [--force]
```

## Update
Like Hearth: GitHub Releases `ngosangns/agent-skills-tui`, install under `~/.local/share/agent-skills-tui/bin/` + symlink `~/.local/bin/agent-skills-tui`. Asset naming TBD (`agent-skills-tui-vX.Y.Z-darwin-arm64` + sha256).

## Non-goals v1
In-TUI skill editor; running MCP servers; session tokens; background auto-sync; Windows/Linux (phase 2); crush/goose.

## Success criteria MVP
1. `cargo build --release` succeeds on darwin-arm64
2. TUI opens, lists real user skills/MCPs from this machine, Tab switches scopes
3. Focus shows agent presence
4. `sync skills --scope user --dry-run` prints planned symlink/copy actions
5. `update --check` hits GitHub API (graceful if no release yet)
6. README with install + usage
