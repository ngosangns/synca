<p align="center"><img src="docs/icon.png" alt="synca" width="128"></p>

# synca

Browse and sync **skills**, **MCP** configs, and **hooks** across coding agents (Grok, Devin, Kiro, OpenCode, Claude, Cursor) from one CLI. Pi (`~/.pi`) and OMP (`~/.omp`) are not targets. Directories that already exist for them are ignored and never deleted.

The CLI is the source of truth and the only writer. The window is `synca-gpui` in [`apps/synca-gpui`](#window). There is no daemon or background service.

The full hooks reference is in [`docs/hooks.md`](docs/hooks.md).

## Screenshots

The window, driven by the CLI (`synca-gpui`, dark theme).

![Library and a skill detail in the main window](docs/screenshots/synca-main-1440.png)

Main view (1440×900): User and Workspace scopes, search, and the skill and MCP server list on the left, with Install… and the sync actions pinned at the bottom; the selected entry's scope, sync state, agents, files and markdown preview on the right.

![Install dialog, Skill tab](docs/screenshots/synca-install-dialog.png)

The Install dialog, Skill tab: pick a local folder, remote Git source, skills.sh link or JSON, then preview the dry-run before anything is written.

![Install dialog, MCP server tab](docs/screenshots/synca-install-mcp.png)

The MCP server tab of the same dialog: name the server, choose a local process, HTTP, SSE or a JSON config, then preview the normalized config.

<img src="docs/screenshots/synca-mobile-390.png" alt="A skill detail in a 390 pt wide window" width="320">

A skill detail in a narrow (390 pt) window: the list and the detail become separate screens (the back arrow returns to the list), and the Install dialog fills the window.

## Install

### From GitHub Release (recommended)

macOS arm64 only. Asset name: `synca-vX.Y.Z-darwin-arm64`. `SHA256SUMS` is for a manual check. `synca update` verifies the GitHub asset digest instead.

```bash
TAG=v0.4.0
ASSET=synca-${TAG}-darwin-arm64
VER=${TAG#v}
mkdir -p ~/.local/share/synca/bin ~/.local/bin
curl -fsSL -o ~/.local/share/synca/bin/synca-$VER \
  "https://github.com/ngosangns/synca/releases/download/${TAG}/${ASSET}"
chmod 755 ~/.local/share/synca/bin/synca-$VER
ln -sfn ../share/synca/bin/synca-$VER ~/.local/bin/synca
```

Make sure `~/.local/bin` is on `PATH`. Then self-update later:

```bash
synca update --check
synca update
```

### From source

Needs Go (version in `go/go.mod`) and, for the `task` commands, [go-task](https://taskfile.dev).

```bash
git clone https://github.com/ngosangns/synca.git
cd synca
task install        # builds, copies to ~/.local/share/synca/bin/synca-<VERSION>, links ~/.local/bin/synca
```

`task install` runs `make install` in `go/`, so `cd go && make install` does the same without `task`.

| Task | What it does |
| --- | --- |
| `task install` | Same as `task cli:install` |
| `task cli:build` | Build `go/bin/synca` only |
| `task cli:install` | Build, install to `~/.local/share/synca/bin`, link `~/.local/bin/synca` |
| `task test` / `task cli:test` | `go vet ./...` and `go test ./...` |
| `task release:build` | Test and build `dist/synca-vX.Y.Z-darwin-arm64` plus `SHA256SUMS`. No publish |
| `task release:publish` | Same, then `gh release create` or upload for the `VERSION` file. Asks first |
| `task dev:install` | Build the window in `apps/synca-gpui` and install `Synca Dev.app` into `~/Applications` |
| `task dev:install:all` | `task install`, then `task dev:install` |

The version lives in the root `VERSION` file and must match `Version` in `go/internal/core/models.go`; the release script and CI refuse a tag that does not match. CI (`.github/workflows/ci.yml`) vets, tests and builds the CLI. `release.yml` builds the asset on a `v*` tag push.

## Usage

```bash
synca                 # print help (same as synca help)
synca --version       # synca 0.4.0
```

```text
synca skills <list|install|remove> [flags]
synca mcp <list|add|remove|search|browse|info|install> [flags]
synca hooks <list|add|remove|trust|untrust|sync> [flags]
synca sync <skills|mcp|hooks|all> [flags]
synca provider clear <name> [--scope --cwd --yes --dry-run --json]
synca doctor [--scope --cwd --json]
synca skill <install|check> [--dest DIR] [--json]
synca update [--check] [--json] [--force]
synca version [--json]
synca schema [--json] [<command>]
```

### Common flags

| Flag | Meaning |
| --- | --- |
| `--scope user\|project` | Default `user`. `project` needs a git repo (see [Scope](#scope)) |
| `--cwd <dir>` | Where to look for the project root. Default `.` |
| `--agents a,b,c` | Restrict to these agents. Unknown names are ignored for skills and MCP (an empty result means all agents); `hooks add` rejects an unknown agent |
| `--dry-run` | Print the plan and change nothing |
| `--json` | One JSON document on stdout. Every command accepts it except `hooks trust` |
| `--yes` | Confirm a destructive command. Required off a TTY for `skills remove --purge`, `mcp remove`, `hooks sync --on-conflict keep-source`, and `sync` with `keep-source` or `keep-target`. `provider clear` requires it even on a TTY |
| `--on-conflict skip\|keep-source\|keep-target` | `sync` only. Default `skip`. `skip` exits 0 and reports conflicts. `keep-source` and `keep-target` overwrite and need `--yes` off a TTY. Hooks accept only `skip` and `keep-source` |
| `--key <name>` | `sync skills` and `sync mcp` only. `sync all` and `sync hooks` reject it |
| `--strict` | `sync hooks` and `sync all`. Exit 7 when a hook was skipped because no person approved it |
| `--no-hooks` | `sync all` only. Leave hooks out of the run |

Flags take `--flag value` or `--flag=value`.

### List

```bash
synca skills list --scope user
synca skills list --scope project --json
synca mcp list --scope user
synca mcp list --json
synca mcp list --json --reveal
```

Text output is one line per entry with the agents that have it, and `MISMATCH` when copies differ. It does not print env values. `mcp list --json` does: every env value and header value is `••••••` unless you pass `--reveal`. A URL password and a secret-looking arg are masked the same way. The fingerprint still comes from the real config. Each row also carries `source`, `source_type` and `source_version` when the MCP lock file (`~/.agents/.mcp-lock.json`, or `<project>/.mcp-lock.json`) records a registry install; the file holds names and versions only, never values.

### Skills

```bash
synca skills install ./my-skill --scope user
synca skills install vercel-labs/agent-skills@find-skills --dry-run
synca skills install https://skills.sh/vercel-labs/agent-skills/find-skills --dry-run
synca skills install https://github.com/org/skill-repo.git@my-skill --dry-run
synca skills remove my-skill                  # unlink agents, keep the canonical copy
synca skills remove my-skill --purge          # also delete canonical; asks you to retype the name
synca skills remove my-skill --purge --yes
```

`install` takes a source and copies the skill into the canonical dir, then links it into each agent dir:

- a local folder with `SKILL.md`, or a folder that holds exactly one skill folder (it also looks in `.agents/skills` and `skills`);
- `owner/repo` shorthand (GitHub), or `owner/repo@skill` to pick one skill from a repository that holds many;
- a Git URL (`https://github.com/...`, `gitlab.com`, `git@...`, anything ending in `.git`);
- a `https://skills.sh/<owner>/<repo>/<skill>` URL.

Remote sources are cloned with `git clone --depth 1` into a temp dir that is removed afterwards. `git` must be installed. A skill's key is its `SKILL.md` `name`, not the folder name; `remove` works on that key.

#### Origin: remote or local

Every skill in `skills list --json` has `origin`, `source` and `source_type`. `origin` is `remote` when the skills lock file records a GitHub or Git source for the skill, and `local` for everything else. The window shows the same value as a badge.

synca reads and writes the lock files that `npx skills` keeps, so both tools share one history:

| Scope | Lock file |
| --- | --- |
| user | `$XDG_STATE_HOME/skills/.skill-lock.json`, else `~/.agents/.skill-lock.json` |
| project | `<project root>/skills-lock.json` |

A remote `install` adds a `record_origin` action. Installing a local folder over a remote skill adds `forget_origin`, so the skill becomes `local`. `remove --purge` forgets the origin; `remove` without `--purge` keeps it. Fields synca does not know are kept when it rewrites a lock file, and a lock file that does not parse is never modified.

#### skills.sh registry

```bash
synca skills search "react native" --limit 10
synca skills browse --view trending --page 0
synca skills info vercel-labs/skills/find-skills --contents
synca skills audit vercel-labs/skills/find-skills
synca skills use vercel-labs/skills@find-skills       # prompt text, nothing installed
synca skills discover anthropics/skills               # list the skills in a repo
synca skills update --dry-run                         # check; no change
synca sync skills --update --dry-run                  # same fetch, then the cross-agent sync
synca skills update find-skills
synca skills restore --dry-run                        # remote skills in the lock file but missing on disk
synca skills init my-skill --dir .
```

| Command | Endpoint | Token |
| --- | --- | --- |
| `search` | `/api/search` | no |
| `browse` (`--view all-time\|trending\|hot`, `--page N`, 200 per page, pages start at 0) | `/api/skills/<view>/<page>` | no |
| `info` | `/api/download/<owner>/<repo>/<skill>`, or `/api/v1/skills/<id>` when a token is set (adds `installs`) | no |
| `audit` | `/api/v1/skills/audit/<id>` | no |
| `official`, `browse --source owner/repo` | `/api/v1/skills/curated`, `/api/v1/skills` | yes |

A token is a Vercel OIDC token in `SKILLS_API_TOKEN` (or `VERCEL_OIDC_TOKEN`). Without one, the token commands exit 7. `SKILLS_API_URL` and `SKILLS_DOWNLOAD_URL` change the host, as in the `skills` CLI. `search` needs a query of two characters or more. An `audit` of a skill nobody has audited gives an empty `audits` list.

#### How skills are fetched

A GitHub skill (`owner/repo`, `owner/repo@skill`, a skills.sh URL, or a GitHub URL) is fetched from skills.sh first. synca reads the repository tree from the GitHub API (`GITHUB_TOKEN` or `GH_TOKEN` raises the 60 requests an hour limit; `GITHUB_API_URL` moves the host) to find the skill folder and its Git tree id, then downloads the files from `skills.sh/api/download/<owner>/<repo>/<skill>`. It uses the snapshot only when it holds every installable file the tree lists for that folder. Otherwise, and for non-GitHub URLs, an explicit ref, or a network error, it runs `git clone --depth 1` as before. The tree id goes in the lock file as `skillFolderHash`, the same value `npx skills` writes. `SYNCA_NO_REGISTRY=1` forces `git clone` everywhere.

`sync skills` and `sync all` run the same fetch first. A remote skill that the lock file lists but that is missing on disk is restored (plan action `fetch_skill`, reason `restore`), then the usual cross-agent sync links it. `--update` also reinstalls a remote skill whose source changed (`fetch_skill`, reason `update`). `--no-registry` skips the step and uses no network. If the registry or a source cannot be reached, synca prints `registry: skipped: ...` on stderr and syncs the rest. Local skills are never touched by this step.

`update` clones each source once and compares the skill folder with the version in the lock file. It plans a reinstall (`copy_skill`, links, `record_origin`) for the skills that changed, and writes `up to date:`, `update:` and `skipped:` lines to `notes`. Well-known (domain) sources and skills without a recorded version are skipped with a reason. `update` without `--dry-run` applies the plan. `restore` does the same for skills that are in the lock file but not on disk.

### Providers

```bash
synca providers list                       # every agent skills.sh supports
synca providers list --scope project --json
```

`providers list` shows the 79 agents the `skills` CLI can install to, not only the six synca syncs to. Each row has the skills directory the agent reads in this scope (`path`), whether it exists (`present`), how many skills it holds, `universal` (it reads the shared `.agents/skills`), and `managed` (synca syncs to it). For a managed agent `path` is the directory synca writes, which in a project can differ from the one the skills CLI uses (OpenCode and Cursor use `.opencode/skills` and `.cursor/skills`). The table is embedded from `skills@1.7.4` and needs no network. synca still syncs only to its six agents; the rest are listed for information.

### MCP servers

```bash
synca mcp search filesystem --json
synca mcp browse --limit 20
synca mcp info ai.2kw/mcp-server --json
synca mcp install ai.2kw/mcp-server --env AI_2KW_API_KEY=... --dry-run
synca mcp add my-server --transport stdio --command "npx -y @pkg/server"
synca mcp add hosted-server --transport http --url https://example.com/mcp
synca mcp add stream-server --transport sse --url https://example.com/sse
synca mcp add gated-server --transport http --url https://example.com/mcp --header "Authorization=Bearer ..."
synca mcp update --dry-run
synca mcp remove my-server --scope user --dry-run
synca mcp remove my-server --scope user --yes
synca mcp remove my-server --agents cursor --keep-hub
```

`mcp search`, `browse`, `info` and `install` read the official MCP Registry (`registry.modelcontextprotocol.io`) and need no token. `search` matches the server name only (the registry's limit), `browse` follows registry name order rather than popularity, and `info` lists the packages and remotes of one version. `--version` defaults to `latest`. `MCP_REGISTRY_URL` changes the registry and `MCP_REGISTRY_TOKEN` adds a Bearer token for a subregistry. JSON output is `mcp-registry-search.json` for `search`, `mcp-registry-browse.json` for `browse` and `mcp-registry-info.json` for `info`; it prints env, header and variable names but never their values.

`mcp install <name>` fetches the metadata and turns one variant into a server config; its plan and `--json` document are the same `manage-plan.json` as `mcp add`. The variant is chosen in this order: `--package npm|pypi|oci`; `--transport stdio` (same package order); `--transport http|sse` (the matching remote); otherwise npm, pypi, oci, then `streamable-http`, then `sse`. `--package` cannot be combined with `--transport http` or `sse`. npm becomes `npx -y <id>@<version>`, pypi `uvx <id>==<version>`, and oci `docker run -i --rm <id>:<version>`; registry runtime arguments go before the package, package arguments after it.

The installed key is the last segment of the registry name (`ai.2kw/mcp-server` is `mcp-server`); `--name` overrides it. Applying over an existing key needs `--force`, while a dry-run adds a note instead of failing. `--env KEY=VAL` fills a declared variable before its literal value and default; `--arg NAME=VAL` fills an argument (a named flag uses its name without dashes, a positional one uses its `valueHint` or `argN`). Required env vars, variables and arguments without a value exit 2 and list only names. `--header NAME=VAL` fills a remote header: it overrides a registry header value and satisfies a required header (missing ones exit 2 with names only); an optional header without a value is skipped with a note, and a secret-looking one is shown masked in the plan. A successful install records its registry source in the MCP lock file. `--dry-run` still fetches the registry, it only writes nothing. sse keeps the same rule as `mcp add`: it becomes `http` unless the URL ends in `/sse`, and the plan says so.

`mcp update [names...] [--version V]` re-reads the recorded registry source of each server and reinstalls the current version: env and header values already in the config are kept, a changed version is planned as an update (`upsert_mcp_hub`, `write_mcp_agent`, `record_mcp_origin`), an identical fingerprint reports `up to date:` and a server without a recorded source errors with its name. A newly required env var without a value asks for `--env KEY=VAL` and that server is left out of the plan. `--dry-run` and `--json` behave like `mcp add`.

`mcp add` and `mcp remove` both accept `--dry-run`. `mcp remove` deletes the server from the hub and from agent configs, so off a TTY it exits 6 unless you pass `--yes`. With `--keep-hub --agents a,b` it removes the server from those agent configs only and keeps it in the hub; that unlink is reversible (the next `sync mcp` adds it back) and needs no `--yes`. `--keep-hub` without `--agents` is a usage error.

`mcp add` flags: `--transport stdio|http|sse` (default `stdio`; `local` means stdio, `remote` follows the URL — `http`, or `sse` when it ends in `/sse`), `--command` (required for stdio, split like a shell line), `--url` (required for http and sse), `--env KEY=VAL` (repeatable; the plan shows the key with the value masked), `--header NAME=VAL` (repeatable; masked like env), `--enabled true|false`. `sse` is kept only when the URL ends in `/sse`; otherwise the server is stored as `http`.

### Sync

```bash
synca sync skills --scope user --dry-run
synca sync mcp --scope user --dry-run
synca sync all --scope user --dry-run
synca sync skills --scope user --on-conflict skip
synca sync all --scope user --on-conflict keep-source --yes
synca sync mcp --scope user --on-conflict keep-target --yes
synca sync hooks --scope user --dry-run
synca sync all --scope user --dry-run --no-hooks
synca sync skills --dry-run --json
```

Run with `--dry-run` first. The text plan is one compact JSON object per line. `--json` prints one document instead: `dryRun`, `scope`, `onConflict`, `actions` (those same objects), and `conflicts` (`skill`, `key`, `policy`). An apply adds `applied` and sets `dryRun` to false. Without `--json` the text is unchanged.

Action kinds for skills and MCP: `ensure_canonical_copy`, `symlink_skill`, `ensure_mcp_hub`, `write_mcp_server`, `skip_same`, `skip_mcp_same`, `conflict_skill`, `conflict_mcp`. `ensure_mcp_hub` is an idempotent upsert and shows up even when the hub is already correct. Hooks use their own kinds (`write_hook`, `update_hook`, …); see [Hooks](#hooks).

`sync all` also runs the hooks phase when the scope is user and `~/.agents/hooks.json` exists. The JSON plan then gains a `hooks` object with the same shape as `hooks sync --json`; `--no-hooks` leaves it out. Unapproved hooks are skipped and listed (exit 0, or 7 with `--strict`). `--on-conflict keep-target` applies to skills and MCP; the hooks phase falls back to `skip`.

| `--on-conflict` | Behavior |
|-----------------|----------|
| `skip` (default) | Leave conflicting entries untouched and exit 0. On a TTY, a non-dry run with conflicts asks once for `skip`, `keep-source` or `keep-target` |
| `keep-source` | Prefer canonical / Agents (or first) content; overwrite others + symlink/write. Needs `--yes` off a TTY |
| `keep-target` | Prefer the first agent copy whose content differs from the source; promote it to the hub and sync out. Needs `--yes` off a TTY |

Never silent overwrite. A dry-run does not need `--yes`.

### Hooks

Hooks are shell commands an agent runs on an event (before or after a tool, on prompt submit, on stop). Skills and MCP only copy data; a hook **runs code with your permissions**, so it is handled more strictly. The complete event/tool tables, JSON shapes and edit rules are in [`docs/hooks.md`](docs/hooks.md).

```bash
~/.agents/skills/synca/scripts/hook-scaffold.sh block-rm --event PreToolUse --tools shell
synca hooks add block-rm --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh' --timeout 10 --dry-run
synca hooks add block-rm --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh' --timeout 10
synca hooks list
synca hooks trust block-rm      # a person at a terminal: shows the command, you type "yes"
synca hooks sync --dry-run
synca hooks sync
```

`synca hooks sync` is the same as `synca sync hooks`.

- **Hub:** `~/.agents/hooks.json` (`{"version":1,"hooks":{"<id>":{...}}}`). Scripts live in `~/.agents/hooks/<id>/`; `{{hook_dir}}` in a command expands to that directory. An id is lowercase letters, digits and `-`; `hooks add` normalizes the argument. Unknown fields are rejected, and so is a command that embeds a token, key, password or `Authorization` header.
- **Add flags:** `--event` and `--command` are required. `--tools shell,read` is allowed only on `PreToolUse`, `PostToolUse`, `PostToolUseFailure` and `PermissionRequest`. `--timeout SEC` is `0..3600`. `--on-failure continue|block`. `--agents` / `--exclude-agents` restrict where it is written. `--force` replaces an existing id (a changed definition needs a new approval); without it the id exits 8. `--dry-run` validates and reports `added` or `replaced` without writing.
- **Events:** `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop` (portable) and `SessionEnd`, `PreCompact`, `PostCompact`, `Notification`, `PermissionRequest`, `SubagentStart`, `SubagentStop`, `PostToolUseFailure`. Not every agent has each one: the plan reports `skip_hook_unsupported` / `event_unsupported`.
- **Tools:** `shell`, `read`, `write`, `search`, `web`, `agent`, `mcp` are translated to each agent's tool names. If no class maps for an agent the hook is skipped for it, never widened to every tool. Unmapped classes are reported as `tool_unmapped`.
- **Agents written:** Claude (`~/.claude/settings.json`), Grok (its own file `~/.grok/hooks/synca.json`), Devin (`~/.config/devin/config.json`). Kiro, OpenCode and Cursor are designed (`docs/plans/hooks-sync.md`) but not implemented. An agent is written only when its config directory exists.
- **Trust:** a new or changed hook (its definition and every file under `~/.agents/hooks/<id>/`) is never written until `synca hooks trust <id>` is run by a person at a terminal and they type `yes`. There is no flag to skip it, and an agent running synca cannot approve its own hook. Approvals are stored per machine in `~/.local/state/synca/hooks/trust.json` (mode 0600). Off a terminal, `hooks trust` exits 6.
- **Untrust and remove:** `hooks untrust <id>` withdraws approval. Existing agent entries stay and are reported as `untrusted`; they are not deleted. To delete them, run `hooks remove <id>`, then `hooks sync`. `hooks remove` deletes the hub entry and approval; the next sync removes synca's groups.
- **Sync:** hub to agents only; synca never imports an agent's hooks into the hub. `sync hooks` and `sync all` write only approved hooks. Unapproved ones appear as `conflict_hook` with reason `untrusted` and the exit stays 0 (7 with `--strict`). `sync all` includes hooks only when `~/.agents/hooks.json` exists; `--no-hooks` leaves them out. `sync hooks` rejects `--on-conflict keep-target`. Re-running changes nothing.
- **Plan kinds:** `write_hook`, `update_hook`, `remove_hook`, `adopt_hook`, `skip_hook_same`, `skip_hook_unsupported`, `conflict_hook`. Reasons include `untrusted`, `modified_externally`, `parse_error: …`, `agent_not_installed`, `event_unsupported`, `tool_unmapped`, `scope_unsupported`.
- **Editing:** for Claude and Devin only the top-level `"hooks"` member of the settings file is rewritten (every other byte is kept; inside `hooks` whitespace is normalized), one matcher group per hook, and other tools' hooks are left alone. synca remembers what it wrote in `~/.local/state/synca/hooks/lock.json`, so it can update or remove only its own groups. A file that is not valid JSON is never touched. Each write is atomic and the previous file is saved under `~/.local/state/synca/hooks/backups/` (last 20 per file).
- **Lossy:** `onFailure: "block"` has no verified equivalent in these agents, so it is reported as `failure_mode_weakened` (a script that exits with code 2 still blocks where the agent supports it).
- **Conflicts:** a hand-edited Grok file or Claude/Devin group is reported as `modified_externally` and skipped; `--on-conflict keep-source --yes` restores the hub version. `untrusted` is never resolved by a conflict policy.
- **JSON:** `hooks list --json` prints `{scope, hub, hooks}`; `hooks add|remove --json` prints `{dryRun, hook, change, hub}`; `hooks untrust --json` prints `{hook, trusted:false}`; `hooks sync --json` prints `{dryRun, scope, onConflict, untrusted, actions}` and an apply adds `applied`. `sync all --json` adds an optional `hooks` object with the same shape.
- Only `--scope user` is supported for hooks.

### Providers

```bash
synca provider clear kiro --dry-run
synca provider clear kiro --scope user --yes
```

Deletes one agent's skill copies and MCP server entries in the scope. It refuses without `--yes` (unless `--dry-run`) and exits 6, including on a TTY. It never touches the canonical hub, and only removes the MCP server table or key from the agent's config, not the rest of the file. `agents` is not a valid provider. `--json` prints `{dryRun, scope, provider, lines}` where `lines` are the same strings as the text output.

### Update

```bash
synca update --check          # report only
synca update --check --json
synca update                  # install the latest release
synca update --force          # reinstall the current version
```

Details:

- darwin-arm64 only.
- Replaces only a binary installed as `~/.local/share/synca/bin/synca-<ver>` (or the regular file `~/.local/bin/synca`). It refuses other binaries and anything inside an `.app` bundle.
- The release asset must carry a GitHub SHA-256 digest; a sidecar checksum file is not accepted. It refuses a downgrade.
- Uses `GITHUB_TOKEN`, `GH_TOKEN` or `gh auth token` when available, to avoid API rate limits.
- Exit status: 1 for a failed update, 2 for bad arguments. Other commands use the contract codes below. `update` does not.

### Doctor

```bash
synca doctor
synca doctor --json
```

Read-only. Reports a real agent skill directory whose content differs from canonical, dangling symlinks, missing links in agent skill directories that exist, and MCP entries that differ between the hub and an agent config. Exit 0 when clean, 8 when anything differs. An agent with no skills directory or no MCP file is not reported. Pi and OMP are not inspected.

### Bundled skill

```bash
synca skill install
synca skill check
synca skill check --json
```

`skill install` writes the embedded copy of `skills/synca` (the source of truth in this repo), including `scripts/status.sh`, `scripts/plan.sh` and `scripts/hook-scaffold.sh`. The default destination is `~/.agents/skills/synca`; `--dest DIR` writes elsewhere. `skill check` exits 0 when that directory matches, 8 when it has drifted, and 3 when it is missing. The helper scripts need `jq` and the `synca` binary; `status.sh` and `plan.sh` are read-only, while `hook-scaffold.sh` only creates `~/.agents/hooks/<id>/check.sh` and never adds or trusts a hook.

### Exit codes

| Exit | When |
| --- | --- |
| 2 | Usage: unknown flag, bad argument, unknown command, unknown event or tool class, secret in a hook command |
| 3 | Unavailable: missing path or source, no git root for `--scope project`, or an unknown hook id |
| 5 | The operation failed |
| 6 | A destructive command had no `--yes` off a TTY, or `hooks trust` was not run by a person at a terminal or was not answered `yes` |
| 7 | `--strict`: `sync hooks` or `sync all` skipped a hook that was not approved |
| 8 | `doctor` or `skill check` found drift, or `hooks add` found the id already exists |

With `--json`, a failure prints one line on stdout: `{"error":{"code","exit","message","hint"}}`. Without `--json`, stderr stays `synca failed: ...` plus an optional `hint:` line. `sync --on-conflict skip` exits 0 and still lists conflicts. Every `--help` / `-h` exits 0.

## Where things live

### Scope

- `user` (default): `~/.agents/...` plus per-agent home dirs.
- `project`: `<git-root>/.agents/...` plus repo-local agent dirs. The git root is the nearest parent of `--cwd` that contains `.git`. Outside a repo, `list` shows nothing and `install`, `add` and `remove` fail with "no project root".
- Hooks are user scope only. `--scope project` is rejected for `hooks` and `sync hooks`.

### Canonical paths

| | User | Project |
| --- | --- | --- |
| Skills | `~/.agents/skills/<name>/` | `<git-root>/.agents/skills/<name>/` |
| MCP hub | `~/.agents/mcp.json` | `<git-root>/.mcp.json` |
| Hooks hub | `~/.agents/hooks.json` | not supported |

Hook scripts live in `~/.agents/hooks/<id>/`. Hook state (trust, lock, backups) lives in `~/.local/state/synca/hooks/`.

Sync prefers **symlinks** from each agent's skills dir to the canonical dir. An agent dir that is itself a symlink to the canonical dir is left alone. MCP writers translate to each agent's format (JSON, Grok TOML, OpenCode `mcp` key) and preserve existing **env secrets** on the target when the source lacks a key. Hook writers translate event and tool names and edit shared settings files in place.

### Agent paths

Skills dirs (`<root>` is `~` for user scope, `<git-root>` for project scope):

| Agent | User | Project |
| --- | --- | --- |
| Grok | `~/.grok/skills` | `.grok/skills` |
| Devin | `~/.config/devin/skills` | `.devin/skills` |
| Kiro | `~/.kiro/skills` | `.kiro/skills` |
| OpenCode | `~/.config/opencode/skills` | `.opencode/skills` |
| Claude | `~/.claude/skills` | `.claude/skills` |
| Cursor | `~/.cursor/skills` | `.cursor/skills` |

MCP config files:

| Agent | User | Project |
| --- | --- | --- |
| Grok | `~/.grok/config.toml` | `.grok/config.toml` |
| Devin | `~/.config/devin/mcp_config.json` | none |
| Kiro | `~/.kiro/settings/mcp.json` | `.kiro/settings/mcp.json` |
| OpenCode | `~/.config/opencode/opencode.json` | `opencode.json` |
| Claude | `~/.claude.json` | none |
| Cursor | `~/.cursor/mcp.json` | `.cursor/mcp.json` |

Hook files (user scope only):

| Agent | File | Ownership |
| --- | --- | --- |
| Claude | `~/.claude/settings.json` | Shared; only the top-level `hooks` member is edited |
| Grok | `~/.grok/hooks/synca.json` | Owned by synca |
| Devin | `~/.config/devin/config.json` | Shared; only the top-level `hooks` member is edited |

## Window

The window is the `synca-gpui` crate in `apps/synca-gpui`. It has its own Cargo workspace and links the shared chrome crate `ns-ui` from the `ns-gpui` repo. It shells out to `~/.local/bin/synca` (or `synca` on `PATH`), so install the CLI first.

```bash
task dev:install      # builds apps/synca-gpui and installs ~/Applications/Synca Dev.app
# or, from apps/synca-gpui:
cargo run --release
```

What it does, all through the CLI:

- User scope or a project (workspace) scope; a project folder list is remembered.
- Library of skills and MCP servers with search, sync state per entry, and agent chips.
- Skill detail with a file tree and Markdown preview; Sync this, Unlink, Purge.
- One **Install…** button opens one dialog with two tabs: **Skill** (local folder, remote Git source, skills.sh, JSON) and **MCP server** (local process, HTTP, SSE, JSON). Each tab keeps what you typed when you switch; ⌘1 and ⌘2 switch tabs, Left and Right move between them when the tab row has focus, and Escape closes the dialog.
- Sync runs a `--dry-run` first and shows the plan; conflicts can be skipped or resolved per entry.
- Activity panel (command log) and Settings panel: turn agents on or off, delete one agent's data (`provider clear`), check for updates.

Window state is stored in the macOS defaults domain `com.ngosangns.synca` (`synca.projects`, `synca.projectDir`, `synca.disabledProviders`). The activity log is `~/Library/Application Support/synca/command-log.jsonl`. A disabled agent is excluded by passing `--agents` to every command. The CLI itself reads no config file.

## Agent skill

`skills/synca/SKILL.md` is a skill that teaches coding agents to use this CLI instead of hand-editing agent dirs. It ships with `scripts/status.sh` (read-only health summary), `scripts/plan.sh` (read-only dry-run summary) and `scripts/hook-scaffold.sh` (creates a hook script and prints the add/trust commands). Install the skill and its scripts with:

```bash
synca skill install
```

The hooks reference for people is [`docs/hooks.md`](docs/hooks.md).

## Agents

Grok, Devin, Kiro, OpenCode, Claude, Cursor (+ canonical `agents`). Pi and OMP are ignored.

## License

MIT
