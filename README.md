# agent-skills-tui

Browse and sync **skills** + **MCP** configs across coding agents (Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor) from one Ratatui TUI / CLI.

Inspired by [hearth](https://github.com/ngosangns/hearth)’s TUI stack (Ratatui + crossterm), focused on skills/MCP sync — not Hearth’s daemon/services.

## Install (from source)

```bash
git clone https://github.com/ngosangns/agent-skills-tui.git
cd agent-skills-tui
cargo build --release
# binary: target/release/agent-skills-tui
cp target/release/agent-skills-tui ~/.local/bin/   # optional
```

Self-update (after a GitHub Release exists):

```bash
agent-skills-tui update --check
agent-skills-tui update
```

## Usage

```bash
agent-skills-tui                 # TUI (project scope = cwd → git root)
agent-skills-tui tui

agent-skills-tui skills list --scope user
agent-skills-tui skills list --scope project --json
agent-skills-tui mcp list --scope user

agent-skills-tui sync skills --scope user --dry-run
agent-skills-tui sync mcp --scope user --dry-run
agent-skills-tui sync skills --scope user            # apply (skips conflicts)
```

### TUI keys

| Key | Action |
|-----|--------|
| `Tab` | User ↔ Project |
| `[` / `]` | Skills / MCPs section |
| `j` / `k` | Move |
| `s` | Dry-run sync focused → `y`/`n` |
| `S` | Dry-run sync missing → `y`/`n` |
| `u` | Update check hint |
| `r` | Reload |
| `?` | Help |
| `q` | Quit |

## Canonical paths

- Skills user: `~/.agents/skills/<name>/`
- Skills project: `<git-root>/.agents/skills/<name>/`
- MCP user hub: `~/.agents/mcp.json`
- MCP project: `<git-root>/.mcp.json`

Sync prefers **symlinks** into per-agent skill dirs pointing at canonical `.agents/skills`. Content conflicts never overwrite silently.

## Agents (v1)

Grok, Devin, Cognition, OMP, Pi, Kiro, OpenCode, Claude, Cursor (+ canonical `agents`).

## License

MIT
