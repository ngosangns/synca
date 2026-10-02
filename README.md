# synca

Browse and sync **skills** + **MCP** configs across coding agents (Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor) from one Ratatui TUI / CLI.

Inspired by [hearth](https://github.com/ngosangns/hearth)’s TUI stack (Ratatui + crossterm), focused on skills/MCP sync — not Hearth’s daemon/services.

## Install

### From GitHub Release (recommended)

Asset name: `synca-vX.Y.Z-darwin-arm64` (+ `.sha256`).

```bash
TAG=v0.1.5
ASSET=synca-${TAG}-darwin-arm64
curl -fsSL -o /tmp/$ASSET \
  "https://github.com/ngosangns/synca/releases/download/${TAG}/${ASSET}"
chmod +x /tmp/$ASSET
mkdir -p ~/.local/share/synca/bin ~/.local/bin
VER=${TAG#v}
cp /tmp/$ASSET ~/.local/share/synca/bin/synca-$VER
ln -sf ~/.local/share/synca/bin/synca-$VER ~/.local/bin/synca
```

Then self-update later:

```bash
synca update --check
synca update
```

### From source

```bash
git clone https://github.com/ngosangns/synca.git
cd synca
cargo build --release
cp target/release/synca ~/.local/bin/
```

Local release packaging:

```bash
./scripts/release-local.sh            # build dist/ asset + sha256
./scripts/release-local.sh --publish  # also gh release create/upload
```

CI builds the same asset on tag push (`v*`) via `.github/workflows/release.yml`.

## Usage

```bash
synca                 # TUI (project scope = cwd → git root)
synca tui

synca skills list --scope user
synca skills list --scope project --json
synca mcp list --scope user

synca sync skills --scope user --dry-run
synca sync mcp --scope user --dry-run
synca sync all --scope user --dry-run
synca sync skills --scope user --on-conflict skip
synca sync skills --scope user --on-conflict keep-source
synca sync mcp --scope user --on-conflict keep-target
synca sync all --scope user --on-conflict keep-source
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
| `s` | Sync focused item → `y`/`n`; conflicts → `a`/`b`/`s` |
| `S` | Sync **ALL skills** (current User\|Project page) |
| `M` | Sync **ALL MCPs** (current page) |
| `A` | Sync **ALL skills + MCPs** (current page) |
| `u` | Check GitHub Releases; `y` installs to `~/.local/share/synca/bin` + symlink `~/.local/bin/synca` |
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
