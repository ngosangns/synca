---
name: synca
description: "Browse, install, and sync skills, MCP configs and hooks across coding agents (Grok, Devin, Kiro, OpenCode, Claude, Cursor) with the synca CLI. Use when installing a skill or MCP server for every agent, adding a hook for several agents, checking whether a skill is in sync, resolving a conflict, running doctor, or updating synca. Triggers: synca, sync skills, sync mcp, sync hooks, install skill for all agents, doctor, synca version, synca schema."
---

# synca

`synca` keeps one canonical copy of each skill, MCP server and hook and links or writes it to each agent. Use the CLI. Do not hand-edit per-agent directories.

Do not edit `~/.agents/skills/synca` by hand. Edit this repo's `skills/synca/SKILL.md`, then run `synca skill install`.

## Mutate rule

Read-only: `skills list`, `mcp list`, `hooks list`, `version`, `schema`, `doctor`, `skill check`, and any command with `--dry-run`.

Run a mutating command only when the user asked for that action in this task: `skills install`, `skills remove`, `mcp add`, `mcp remove`, `hooks add`, `hooks remove`, `hooks untrust`, `sync` without `--dry-run`, `provider clear`, `skill install`, `update`. Pass `--dry-run` first on the plan commands.

These are destructive and need `--yes` when stdin is not a TTY: `skills remove --purge`, `mcp remove`, `provider clear`, and `sync` with `--on-conflict keep-source` or `keep-target`. `provider clear` needs `--yes` even on a TTY. A dry-run does not need `--yes`.

## Layout

- Skills: `~/.agents/skills/<name>/` (user), `<git-root>/.agents/skills/<name>/` (project).
- MCP hub: `~/.agents/mcp.json` (user), `<git-root>/.mcp.json` (project).
- Hooks hub: `~/.agents/hooks.json` (user scope only), scripts in `~/.agents/hooks/<id>/`. Written to claude, grok and devin.
- Agents: grok, devin, kiro, opencode, claude, cursor, plus canonical `agents`. Pi (`~/.pi`) and OMP (`~/.omp`) are ignored and never deleted.
- `--scope user` is the default. `--scope project` needs a git repository, or `--cwd`.

## Recipes

Is skill X in sync for every agent:

```bash
synca doctor --json
synca sync skills --key X --dry-run --json
```

`doctor` exits 0 when clean and 8 when it finds drift, a dangling symlink, a missing link, or an MCP difference. `conflict_skill` in the sync plan means copies of X differ.

Install a skill for every agent:

```bash
synca skills install <path|owner/repo|git-url> --dry-run
synca skills install <path|owner/repo|git-url>
```

Register an MCP server for every agent:

```bash
synca mcp add <name> --transport stdio --command "npx -y @pkg/server" --dry-run
synca mcp add <name> --transport stdio --command "npx -y @pkg/server"
```

HTTP uses `--transport http --url https://example.com/mcp`.

Resolve a conflict:

```bash
synca sync skills --key X --dry-run --on-conflict keep-source
synca sync skills --key X --on-conflict keep-source --yes
```

`skip` (the default) leaves conflicts in place and exits 0. `keep-source` makes canonical win. `keep-target` promotes the differing agent copy. Both need `--yes` off a TTY. Use `sync mcp --key <server>` for an MCP conflict.

Doctor and the bundled skill:

```bash
synca doctor
synca skill check
synca skill install
```

## Hooks

A hook is a shell command an agent runs on an event. It executes code, so approval is a person's job.

```bash
synca hooks add <id> --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh' --dry-run
synca hooks add <id> --event PreToolUse --tools shell --command '{{hook_dir}}/check.sh'
synca hooks list --json
synca hooks sync --dry-run --json
```

- **Never run `synca hooks trust`.** It needs a person at a terminal, has no `--yes`, and fails off a TTY with exit 6. After `hooks add`, tell the user to run `synca hooks trust <id>` themselves. Do not try to write `~/.local/state/synca/hooks/trust.json` or edit the agents' hook files by hand.
- `sync hooks` and `sync all` write only approved hooks. Unapproved ones appear as `conflict_hook` with reason `untrusted` and the exit stays 0 (7 with `--strict`).
- A hook with `onFailure: block` is reported `failure_mode_weakened`: the agents have no verified way to make a failing hook block; the script must exit 2.
- Events: SessionStart UserPromptSubmit PreToolUse PostToolUse Stop SessionEnd PreCompact PostCompact Notification PermissionRequest SubagentStart SubagentStop PostToolUseFailure. Tools (tool events only): shell read write search web agent mcp.
- To remove a hook: `synca hooks remove <id>`, then `synca hooks sync`.

## Commands

```bash
synca skills list|install|remove
synca mcp list|add|remove
synca hooks list|add|remove|trust|untrust|sync
synca sync skills|mcp|hooks|all [--dry-run] [--on-conflict skip|keep-source|keep-target] [--key name] [--yes] [--strict] [--no-hooks] [--json]
synca provider clear <name> [--yes] [--dry-run] [--json]
synca doctor [--json]
synca skill install|check [--dest DIR] [--json]
synca version [--json]
synca schema [--json] [<command>]
synca update [--check] [--json]
```

`version --json` prints `name`, `version`, `schemaVersion`, `commit`, and `dirty`.
`sync --json` prints `dryRun`, `scope`, `onConflict`, `actions`, and `conflicts` (`skill`, `key`, `policy`). Each action is the same object the text plan prints on its own line. An apply adds `applied` and sets `dryRun` to false. Without `--json` the text plan is unchanged.
`schema` marks each command R (read), M (mutate), or D (destructive).

## Secrets

`mcp list` masks every env value as `••••••`. A URL password and a secret-looking arg (`--token=…`, an `Authorization` header) are masked the same way. `env_keys` stay visible. Do not pass `--reveal` unless the user asked for the value.

`mcp add --env KEY=VAL --dry-run` prints the key with a masked value.

## Failures

| Exit | Name | When |
| --- | --- | --- |
| 2 | usage | Bad flags or arguments |
| 3 | unavailable | Missing path, source, or project root |
| 5 | failed | The operation failed |
| 6 | needs-confirm | Destructive command without `--yes` off a TTY, or `hooks trust` off a terminal |
| 7 | unauthorized | `--strict`: a hook was skipped because no person approved it |
| 8 | conflict | `doctor` or `skill check` found drift, or `hooks add` on an existing id |

With `--json`, a failure prints one line on stdout: `{"error":{"code","exit","message","hint"}}`. Without `--json`, stderr is `synca failed: ...` and an optional `hint:` line. `sync --on-conflict skip` exits 0 and still lists conflicts. `update` keeps its own codes: 1 failed, 2 usage.
