# agent-skills-tui

Browse and sync **skills** + **MCP** configs across coding agents (Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor) from one Ratatui TUI / CLI.

Inspired by [hearth](https://github.com/ngosangns/hearth)’s TUI stack (Ratatui + crossterm), focused on skills/MCP sync — not Hearth’s daemon/services.

## Install

### From GitHub Release (recommended)

Asset name: `agent-skills-tui-vX.Y.Z-darwin-arm64` (+ `.sha256`).

```bash
TAG=v0.1.0
ASSET=agent-skills-tui-${TAG}-darwin-arm64
curl -fsSL -o /tmp/$ASSET \
  "https://github.com/ngosangns/agent-skills-tui/releases/download/${TAG}/${ASSET}"
chmod +x /tmp/$ASSET
mkdir -p ~/.local/share/agent-skills-tui/bin ~/.local/bin
VER=${TAG#v}
cp /tmp/$ASSET ~/.local/share/agent-skills-tui/bin/agent-skills-tui-$VER
ln -sf ~/.local/share/agent-skills-tui/bin/agent-skills-tui-$VER ~/.local/bin/agent-skills-tui
```

Then self-update later:

```bash
agent-skills-tui update --check
agent-skills-tui update
```

### From source

```bash
git clone https://github.com/ngosangns/agent-skills-tui.git
cd agent-skills-tui
cargo build --release
cp target/release/agent-skills-tui ~/.local/bin/
```

Local release packaging:

```bash
./scripts/release-local.sh            # build dist/ asset + sha256
./scripts/release-local.sh --publish  # also gh release create/upload
```

CI builds the same asset on tag push (`v*`) via `.github/workflows/release.yml`.

## Usage

```bash
agent-skills-tui                 # TUI (project scope = cwd → git root)
agent-skills-tui tui

agent-skills-tui skills list --scope user
agent-skills-tui skills list --scope project --json
agent-skills-tui mcp list --scope user

agent-skills-tui sync skills --scope user --dry-run
agent-skills-tui sync mcp --scope user --dry-run
agent-skills-tui sync skills --scope user --on-conflict skip
agent-skills-tui sync skills --scope user --on-conflict keep-source
agent-skills-tui sync mcp --scope user --on-conflict keep-target
```

### Conflict flags

| `--on-conflict` | Behavior |
|-----------------|----------|
| `skip` (default) | Leave conflicting entries untouched (on a TTY, CLI prompts once if conflicts exist) |
| `keep-source` | Prefer canonical / Agents (or first) content; overwrite others + symlink/write |
| `keep-target` | Prefer non-canonical agent copy; promote it to hub and sync out |

Never silent overwrite.

### TUI keys

| Key | Action |
|-----|--------|
| `Tab` | User ↔ Project |
| `[` / `]` / `Space` | Skills / MCPs section |
| `j` / `k` | Move |
| `s` | Dry-run sync focused → `y`/`n`; conflicts → `a`/`b`/`s` |
| `S` | Dry-run sync missing → same confirm / conflict flow |
| `u` | Check GitHub Releases; `y` installs to `~/.local/share/agent-skills-tui/bin` + symlink `~/.local/bin/agent-skills-tui` |
| `r` | Reload |
| `?` | Help |
| `q` | Quit |

Conflict keys when prompted: **`a`** keep-source · **`b`** keep-target · **`s`** skip · **`n`** cancel.

## Canonical paths

- Skills user: `~/.agents/skills/<name>/`
- Skills project: `<git-root>/.agents/skills/<name>/`
- MCP user hub: `~/.agents/mcp.json`
- MCP project: `<git-root>/.mcp.json`

Sync prefers **symlinks** into per-agent skill dirs pointing at canonical `.agents/skills`. MCP writers preserve existing **env secrets** on the target when the source lacks a key.

## Agents (v1)

Grok, Devin, Cognition, OMP, Pi, Kiro, OpenCode, Claude, Cursor (+ canonical `agents`).

## License

MIT
