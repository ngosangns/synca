# synca

Browse and sync **skills** + **MCP** configs across coding agents (Grok, Devin, OMP, Pi, Kiro, OpenCode, Claude, Cursor) from one CLI, with a native macOS app in [`macos/`](macos/) on top of it.

The CLI is the source of truth; the app only runs it. Skills/MCP sync only — no daemon or services.

## Manage skills / MCP

```bash
synca skills install ./my-skill --scope user
synca skills install https://github.com/org/skill-repo.git --dry-run
synca skills remove my-skill            # unlink agents (canonical kept)
synca skills remove my-skill --purge --yes
synca mcp add my-server --transport stdio --command "npx -y @pkg/server"
synca mcp remove my-server --scope user
```

## Install

### From GitHub Release (recommended)

Asset name: `synca-vX.Y.Z-darwin-arm64` (+ `.sha256`).

```bash
TAG=v0.2.0
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
cd synca/go
make install    # builds, copies to ~/.local/share/synca/bin, links ~/.local/bin/synca
```

Needs Go (version in `go/go.mod`).

Local release packaging:

```bash
./scripts/release-local.sh            # test + build dist/ asset + sha256
./scripts/release-local.sh --publish  # also gh release create/upload
```

CI builds the same asset on tag push (`v*`) via `.github/workflows/release.yml`.

## Usage

```bash
synca                 # print help

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

## macOS app

```bash
cd macos
make            # build synca.app (needs the synca CLI on PATH or SYNCA_BIN)
make install    # copy to /Applications
make test       # SyncaKit + end-to-end app tests (sandboxed HOME)
```

Three columns: Library (user/project switcher, project manager, actions, skills + MCP lists), Details (file tree, presence, conflict diffs) and Activity (command log).

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
