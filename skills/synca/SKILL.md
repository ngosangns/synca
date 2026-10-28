---
name: synca
description: "Browse, install, and sync skills, MCP configs and hooks across coding agents (Grok, Devin, Kiro, OpenCode, Claude, Cursor) with the synca CLI. Use when installing a skill or MCP server for every agent, adding a hook for several agents, checking whether a skill is in sync, resolving a conflict, running doctor, or updating synca. Triggers: synca, sync skills, sync mcp, sync hooks, install skill for all agents, doctor, synca version, synca schema."
---

# synca

`synca` keeps one canonical copy of each skill, MCP server and hook and links or writes it to each agent. Use the CLI. Do not hand-edit per-agent directories.

Do not edit `~/.agents/skills/synca` by hand. Edit this repo's `skills/synca/SKILL.md`, then run `synca skill install`.

## Mutate rule

Read-only: `skills list`, `skills search`, `skills browse`, `skills official`, `skills info`, `skills audit`, `skills use`, `skills discover`, `providers list`, `mcp list`, `mcp search`, `mcp browse`, `mcp info`, `hooks list`, `version`, `schema`, `doctor`, `skill check`, and any command with `--dry-run`. The bundled helpers `scripts/status.sh` and `scripts/plan.sh` are also read-only.

Run a mutating command only when the user asked for that action in this task: `skills install`, `skills remove`, `skills update`, `skills restore`, `skills init`, `mcp add`, `mcp remove`, `mcp install`, `hooks add`, `hooks remove`, `hooks untrust`, `hooks sync`, `sync` without `--dry-run`, `provider clear`, `skill install`, `update`. Pass `--dry-run` first on the plan commands.

These are destructive and need `--yes` when stdin is not a TTY: `skills remove --purge`, `mcp remove`, `provider clear`, `sync` with `--on-conflict keep-source` or `keep-target`, and `hooks sync --on-conflict keep-source`. `provider clear` needs `--yes` even on a TTY. A dry-run does not need `--yes`.

`skills remove <key> --agents a,b` and `mcp remove <name> --agents a,b --keep-hub` unlink one or more agents and keep the canonical copy; the next sync adds the links back. `--keep-hub` without `--agents` is a usage error.

**Never run `synca hooks trust`.** It needs a person at a terminal, has no `--yes`, and fails off a TTY with exit 6. After `hooks add`, tell the user to run it themselves. Do not write the trust store or the agents' hook files by hand.

## Layout

- Skills: `~/.agents/skills/<name>/` (user), `<git-root>/.agents/skills/<name>/` (project).
- MCP hub: `~/.agents/mcp.json` (user), `<git-root>/.mcp.json` (project).
- Hooks hub: `~/.agents/hooks.json` (user scope only), scripts in `~/.agents/hooks/<id>/`. Written to Claude, Grok and Devin.
- Hook state: trust `~/.local/state/synca/hooks/trust.json` (0600), ownership `lock.json`, backups in `backups/` (last 20 per file).
- Agents: grok, devin, kiro, opencode, claude, cursor, plus canonical `agents`. Pi (`~/.pi`) and OMP (`~/.omp`) are ignored and never deleted.
- `--scope user` is the default. `--scope project` needs a git repository, or `--cwd`.
- Helpers installed with this skill: `scripts/status.sh`, `scripts/plan.sh`, `scripts/hook-scaffold.sh`.

## Recipes

Is everything in sync for every agent:

```bash
synca doctor --json
synca sync skills --key X --dry-run --json
~/.agents/skills/synca/scripts/status.sh
```

`doctor` exits 0 when clean and 8 when it finds drift, a dangling symlink, a missing link, or an MCP difference. `conflict_skill` in the sync plan means copies of X differ. `status.sh` also reports hook trust and pending state and exits 8 on doctor drift.

Review a plan without writing:

```bash
~/.agents/skills/synca/scripts/plan.sh all
~/.agents/skills/synca/scripts/plan.sh skills --key X
~/.agents/skills/synca/scripts/plan.sh all --json
```

Install a skill for every agent:

```bash
synca skills install <path|owner/repo|owner/repo@skill|git-url> --dry-run
synca skills install <path|owner/repo|owner/repo@skill|git-url>
```

Find a skill on skills.sh, check it, and keep remote skills current:

```bash
synca skills search "<query>" --json
synca skills audit <owner/repo/skill> --json
synca skills update --dry-run --json
```

GitHub skills come from skills.sh with a `git clone` fallback. `sync skills` and `sync all` first restore remote skills missing on disk (`fetch_skill` actions); `--update` also refreshes changed ones and `--no-registry` skips the step. `skills list --json` marks each skill `origin: remote|local`. `skills official` and `skills browse --source` need `SKILLS_API_TOKEN` and exit 7 without it.

Find an MCP server in the official registry and install it for every agent:

```bash
synca mcp search "<query>" --json
synca mcp info <namespace/name> --json
synca mcp install <namespace/name> --env API_KEY=... --dry-run --json
synca mcp install <namespace/name> --env API_KEY=...
```

`install` picks npm, pypi or oci first, then a streamable-http or sse remote; `--package` and `--transport stdio|http|sse` override that. `--arg NAME=VAL` fills arguments, `--name` overrides the key, and an existing key needs `--force`. Required env vars, variables and arguments and remotes with required headers exit 2 with names only. The registry is read over the network even with `--dry-run`. `MCP_REGISTRY_URL` and `MCP_REGISTRY_TOKEN` change it.

Register an MCP server without the registry:

```bash
synca mcp add <name> --transport stdio --command "npx -y @pkg/server" --dry-run
synca mcp add <name> --transport stdio --command "npx -y @pkg/server"
```

HTTP uses `--transport http --url https://example.com/mcp`; SSE uses `--transport sse` and is kept only when the URL ends in `/sse`.

Resolve a conflict:

```bash
synca sync skills --key X --dry-run --on-conflict keep-source
synca sync skills --key X --on-conflict keep-source --yes
```

`skip` (the default) leaves conflicts in place and exits 0. `keep-source` makes canonical win. `keep-target` promotes the differing agent copy. Both need `--yes` off a TTY. Use `sync mcp --key <server>` for an MCP conflict. Hooks accept only `skip` and `keep-source`.

Doctor and the bundled skill:

```bash
synca doctor
synca skill check
synca skill install
```

## Hooks

A hook is a shell command an agent runs on an event. It executes code with the user's permissions, so approval is a person's job. Only `--scope user` is supported.

Create a script, then add and approve the hook:

```bash
~/.agents/skills/synca/scripts/hook-scaffold.sh my-check --event PreToolUse --tools shell
synca hooks add my-check --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh' --dry-run
synca hooks add my-check --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh'
# now tell the user to run, in their terminal:
#   synca hooks trust my-check
#   synca hooks sync
```

The scaffold writes only `~/.agents/hooks/my-check/check.sh`; it never adds or trusts a hook.

`hooks add` flags: `--event <Event>` and `--command <cmd>` are required. `--tools shell,read` only on tool events. `--timeout SEC` is 0..3600. `--on-failure continue|block`. `--agents a,b` / `--exclude-agents a,b`. `--description TEXT`. `--force` replaces an existing id. `--dry-run` and `--json`.

- The id is normalized (lowercase, `_` and spaces to `-`). An existing id exits 8 unless `--force` is passed; a changed definition needs a new approval.
- `{{hook_dir}}` expands to `~/.agents/hooks/<id>/`. The approval hash covers the definition and every file in that directory, so editing a script needs a new approval.
- A command that embeds a token, key, password or `Authorization` header is rejected. Read secrets from the environment inside the script.
- Events: `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `Stop` `SessionEnd` `PreCompact` `PostCompact` `Notification` `PermissionRequest` `SubagentStart` `SubagentStop` `PostToolUseFailure`.
- Tool classes (only on `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`): `shell` `read` `write` `search` `web` `agent` `mcp`.
- Written agents: Claude (all events), Grok (all except `PermissionRequest`, own file `~/.grok/hooks/synca.json`), Devin (a subset; `PostCompact` becomes `PostCompaction`). Kiro, OpenCode and Cursor are not implemented. An agent is written only when its config directory exists.
- If no requested tool class maps for an agent, the hook is skipped (`tool_unmapped`) and never widened to every tool. Some classes map only for some agents (`web` Claude only; `mcp` Claude and Devin; `read`/`search` Claude and Grok; `agent` Claude and Grok).
- `onFailure: block` is reported `failure_mode_weakened`: the agents have no verified way to make a failing hook block; a script that exits 2 still blocks where the agent supports it.

Trust and sync:

- A new or changed hook is never written until a person runs `synca hooks trust <id>` and types `yes`. Approvals are per machine, not per agent.
- `synca hooks untrust <id>` withdraws approval. Existing agent entries stay in place and are reported as `untrusted`; they are not deleted. To delete them, run `synca hooks remove <id>`, then `synca hooks sync`.
- `hooks remove` deletes the hub entry and its approval. The next `hooks sync` removes synca's groups from the agents.
- `sync hooks` and `sync all` write only approved hooks. Unapproved ones appear as `conflict_hook` with reason `untrusted`; exit stays 0, or 7 with `--strict`.
- `sync all` includes hooks only when `~/.agents/hooks.json` exists and the scope is user; `--no-hooks` leaves them out. `sync hooks` rejects `--on-conflict keep-target`; `sync all` accepts it for skills/MCP but uses `skip` for the hooks phase.
- Plan kinds: `write_hook`, `update_hook`, `remove_hook`, `adopt_hook`, `skip_hook_same`, `skip_hook_unsupported`, `conflict_hook`. Unsupported reasons include `agent_not_installed`, `event_unsupported`, `tool_unmapped`, `scope_unsupported`; conflict reasons include `untrusted`, `modified_externally`, `parse_error: ...`.
- Claude and Devin settings files: only the top-level `hooks` member is rewritten, every other byte is kept, and an unparseable file is never touched. Grok's `synca.json` is owned by synca. Each write is atomic with a backup.
- `hooks list` states per agent: `synced`, `pending`, `pending_removal`, `disabled`, `excluded`, `not_synced`, `unsupported: <reason>`, `conflict: <reason>`.

Hook JSON shapes:

- `hooks list --json`: `{scope, hub, hooks:[{id, event, tools, command, timeoutSec, onFailure, enabled, trusted, agents:[{agent,state}]}]}`.
- `hooks add|remove --json`: `{dryRun, hook, change: added|replaced|removed, hub}`.
- `hooks untrust --json`: `{hook, trusted:false}`.
- `hooks sync --json`: `{dryRun, scope, onConflict, untrusted, actions:[...]}`; an apply adds `applied`.
- `sync all --json` adds an optional `hooks` object with that same shape.

## Commands

```bash
synca skills list|install|remove
synca mcp list|add|remove|search|browse|info|install
synca hooks list|add|remove|trust|untrust|sync
synca sync skills|mcp|hooks|all [--dry-run] [--on-conflict skip|keep-source|keep-target] [--key name] [--agents a,b] [--yes] [--strict] [--no-hooks] [--json]
synca provider clear <name> [--yes] [--dry-run] [--json]
synca doctor [--scope --cwd --json]
synca skill install|check [--dest DIR] [--json]
synca version [--json]
synca schema [--json] [<command>]
synca update [--check] [--force] [--json]
```

- `--key` applies to `sync skills` and `sync mcp` only. `sync all` and `sync hooks` reject it.
- `--strict` applies to `sync hooks` and `sync all`; `--no-hooks` applies only to `sync all`.
- `--agents` restricts agents; for skills and MCP unknown names are ignored, and an empty result means all agents. `hooks add` validates agent names and rejects an unknown one. Hooks accept `--agents` on `list` and `sync`.
- `--json` prints one document on stdout; every command accepts it except `hooks trust`.

`version --json` prints `name`, `version`, `schemaVersion`, `commit`, and `dirty`.
`sync --json` prints `dryRun`, `scope`, `onConflict`, `actions`, and `conflicts` (`skill`, `key`, `policy`). Each action is the same object the text plan prints on its own line. An apply adds `applied` and sets `dryRun` to false. Without `--json` the text plan is unchanged.
`schema` marks each command R (read), M (mutate), or D (destructive).

## Helper scripts

Installed at `~/.agents/skills/synca/scripts/` (or `<skill-dir>/scripts/` in the repo). They need `jq` and the `synca` binary (`SYNCA_BIN` overrides the lookup).

- `status.sh [--scope user|project] [--cwd DIR] [--json]` — read-only version, counts, doctor findings, and hook trust/state summary. Exit 0 clean, 8 doctor drift, 3 unavailable.
- `plan.sh [skills|mcp|hooks|all] [--scope] [--cwd] [--key] [--agents] [--json]` — read-only `sync --dry-run --json`, summarized by action kind, conflicts, untrusted hooks and lossy warnings. `--json` prints the raw plan.
- `hook-scaffold.sh <id> [--event Event] [--tools shell,read] [--force]` — creates `~/.agents/hooks/<id>/check.sh` (0755) and prints the exact `hooks add` and `hooks trust` commands. It does not edit the hub, sync, or approve anything.

## Secrets

`mcp list` masks every env value as `••••••`. A URL password and a secret-looking arg (`--token=…`, an `Authorization` header) are masked the same way. `env_keys` stay visible. Do not pass `--reveal` unless the user asked for the value.

`mcp add --env KEY=VAL --dry-run` prints the key with a masked value; `mcp install` never prints env, header or variable values, even in errors. A hook command that embeds a secret is rejected at `hooks add`.

## Failures

| Exit | Name | When |
| --- | --- | --- |
| 2 | usage | Bad flags or arguments |
| 3 | unavailable | Missing path, source, project root, or hook |
| 5 | failed | The operation failed |
| 6 | needs-confirm | Destructive command without `--yes` off a TTY, or `hooks trust` off a terminal or not answered `yes` |
| 7 | unauthorized | `--strict`: a hook was skipped because no person approved it |
| 8 | conflict | `doctor` or `skill check` found drift, or `hooks add` on an existing id |

With `--json`, a failure prints one line on stdout: `{"error":{"code","exit","message","hint"}}`. Without `--json`, stderr is `synca failed: ...` and an optional `hint:` line. `sync --on-conflict skip` exits 0 and still lists conflicts. `update` keeps its own codes: 1 failed, 2 usage.
