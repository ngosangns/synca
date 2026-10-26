# Hooks

Hooks are shell commands an agent runs on an event (before or after a tool, on prompt submit, when a session starts or stops). They execute code with your permissions, so synca treats them more strictly than skills or MCP: the flow is hub to agents only, every hook is hash-pinned, and a person must approve it at a terminal.

Hooks are implemented for **user scope only**. Claude, Grok and Devin are written. Kiro, OpenCode and Cursor are designed in [`docs/plans/hooks-sync.md`](plans/hooks-sync.md) but not implemented yet.

The short version lives in the [README](../README.md#hooks). This page is the reference.

## Where hooks live

| Path | What |
| --- | --- |
| `~/.agents/hooks.json` | The hub: canonical hook definitions, `version: 1` |
| `~/.agents/hooks/<id>/` | Scripts for hook `<id>`; `{{hook_dir}}` expands here |
| `~/.local/state/synca/hooks/trust.json` | Human approvals (mode 0600), machine-local |
| `~/.local/state/synca/hooks/lock.json` | What synca wrote into each agent file |
| `~/.local/state/synca/hooks/backups/` | Previous agent files (last 20 per file) |

Hub example:

```json
{
  "version": 1,
  "hooks": {
    "block-rm": {
      "description": "Refuse recursive deletes",
      "event": "PreToolUse",
      "tools": ["shell"],
      "handler": {
        "command": "{{hook_dir}}/check.sh",
        "timeoutSec": 10
      },
      "onFailure": "block",
      "agents": ["claude", "grok"],
      "excludeAgents": ["devin"]
    }
  }
}
```

An id is lowercase letters, digits and `-`, up to 64 characters. `synca hooks add` normalizes the argument (lowercase, `_` and spaces become `-`). Unknown JSON fields are rejected, so a typo cannot silently drop a safety property.

| Field | Rules |
| --- | --- |
| `description` | Optional text |
| `enabled` | Optional; default `true`. Disabling never invalidates an approval |
| `event` | Required; see [Events](#events) |
| `tools` | Only on tool events; see [Tools](#tools) |
| `handler.type` | Optional; only `command` |
| `handler.command` | Required, non-empty. A command that embeds a token, key, password or `Authorization` header is rejected |
| `handler.timeoutSec` | `0..3600`; `0` means the agent default |
| `onFailure` | `continue` or `block`; see [Lossy translation](#lossy-translation) |
| `agents` | Optional allow-list of agent names |
| `excludeAgents` | Optional deny-list of agent names |

`{{hook_dir}}` expands to the absolute hook script directory. `{{project_dir}}` is rejected (`scope_unsupported`) because project hooks are not implemented. Read secrets from the environment inside the script, never in the command string.

The approval hash covers the canonical definition plus every file under `~/.agents/hooks/<id>/`. Editing a script, adding a helper file, or changing any hook field invalidates the approval. Toggling `enabled` does not.

## Events

Canonical names use Claude Code's PascalCase spelling.

| Event | Claude | Grok | Devin |
| --- | --- | --- | --- |
| `SessionStart` | yes | yes | yes |
| `UserPromptSubmit` | yes | yes | yes |
| `PreToolUse` | yes | yes | yes |
| `PostToolUse` | yes | yes | yes |
| `Stop` | yes | yes | yes |
| `SessionEnd` | yes | yes | yes |
| `PreCompact` | yes | yes | no |
| `PostCompact` | yes | yes | yes (`PostCompaction`) |
| `Notification` | yes | yes | no |
| `PermissionRequest` | yes | no | yes |
| `SubagentStart` | yes | yes | no |
| `SubagentStop` | yes | yes | no |
| `PostToolUseFailure` | yes | yes | no |

An event an agent does not support produces `skip_hook_unsupported` with reason `event_unsupported` (or `scope_unsupported`), never a silently altered hook.

## Tools

Tool classes are translated to each agent's native tool names. They are valid only on `PreToolUse`, `PostToolUse`, `PostToolUseFailure` and `PermissionRequest`.

| Class | Claude | Grok | Devin |
| --- | --- | --- | --- |
| `shell` | `Bash` | `run_terminal_command` | `exec` |
| `read` | `Read` | `read_file` | — |
| `write` | `Edit`, `Write`, `MultiEdit` | `search_replace`, `hashline_edit`, `write` | `edit` |
| `search` | `Grep`, `Glob` | `grep`, `list_dir` | — |
| `web` | `WebFetch`, `WebSearch` | — | — |
| `agent` | `Task` | `spawn_subagent` | — |
| `mcp` | `mcp__.*` | — | `mcp__.*` |

A class an agent cannot map is reported as `tool_unmapped` (warning). If **none** of the requested classes map for an agent, the hook is skipped for that agent. synca never drops the matcher to widen a hook to every tool.

## Trust

A new or changed hook is written to no agent until `synca hooks trust <id>` is run by a person:

```text
$ synca hooks trust block-rm
This hook will run on every matching agent event with your permissions:
  hook:    block-rm
  event:   PreToolUse
  tools:   shell
  command: {{hook_dir}}/check.sh
  scripts: /Users/you/.agents/hooks/block-rm (their content is part of the approval)
Type "yes" to approve it:
```

- It requires stdin to be a terminal. There is no `--yes`, environment variable, or non-interactive bypass; off a terminal it exits 6.
- Typing anything other than `yes` cancels with exit 6. A dry-run never asks.
- Approvals are per machine, per hook and per hash. They are not committed and do not travel to another machine.
- An agent (including a coding agent using the bundled skill) must not run `synca hooks trust`. A human runs it.
- `synca hooks untrust <id>` withdraws the approval. Existing agent entries are **not** deleted; the next sync reports them as `untrusted`. To remove them, use `synca hooks remove <id>` then `synca hooks sync`.
- `synca hooks remove <id>` deletes the hub entry and its approval; the next sync removes synca's groups from the agents.

## Commands

```text
synca hooks list [--agents a,b] [--json]
synca hooks add <id> --event <Event> --command <cmd> [--tools shell,write] [--timeout SEC]
                [--description TEXT] [--agents a,b] [--exclude-agents a,b]
                [--on-failure continue|block] [--force] [--dry-run] [--json]
synca hooks remove <id> [--dry-run] [--json]
synca hooks trust <id>
synca hooks untrust <id> [--json]
synca hooks sync [--agents a,b] [--dry-run] [--on-conflict skip|keep-source] [--strict] [--yes] [--json]
```

`synca sync hooks` is the same as `synca hooks sync`. It rejects `--key`, `--no-hooks`, `--on-conflict keep-target`, and `--scope project`. `--on-conflict keep-source` needs `--yes` off a TTY.

### list

```bash
synca hooks list
synca hooks list --agents claude,devin --json
```

Text shows the id, event, tools, trust state, command and each agent's state. JSON returns `{scope, hub, hooks}`; each hook has `id`, optional `description`, `event`, `tools`, `command`, `timeoutSec`, `onFailure`, `enabled`, `trusted`, and `agents: [{agent, state}]`.

Per-agent state is one of:

| State | Meaning |
| --- | --- |
| `synced` | The agent file matches the approved render |
| `pending` | Approved, but not written yet |
| `pending_removal` | Removed from the hub; the next sync deletes it |
| `disabled` | The hook has `enabled: false` |
| `excluded` | `agents` / `excludeAgents` excludes this agent |
| `not_synced` | Not present in the agent file |
| `unsupported: <reason>` | The agent cannot express the hook |
| `conflict: <reason>` | `untrusted`, `modified_externally`, or a parse error |

### add

```bash
synca hooks add block-rm --event PreToolUse --tools shell \
  --command '{{hook_dir}}/check.sh' --timeout 10 --on-failure block --dry-run
synca hooks add block-rm --event PreToolUse --tools shell \
  --command '{{hook_dir}}/check.sh' --timeout 10 --on-failure block
```

- `--event` and `--command` are required.
- `--tools` is allowed only on tool events; it is a comma-separated list of classes.
- `--timeout` is seconds, `0..3600`.
- `--agents` / `--exclude-agents` restrict where the hook is written. Unknown names are rejected.
- An existing id exits 8. `--force` replaces it; a changed definition then needs a new approval.
- `--dry-run` validates and reports `added` or `replaced` without writing the hub.
- JSON: `{dryRun, hook, change: added|replaced, hub}`.

The command does not create script files. Put them in `~/.agents/hooks/<id>/` yourself, or use the bundled scaffold:

```bash
~/.agents/skills/synca/scripts/hook-scaffold.sh block-rm --event PreToolUse --tools shell
```

`hook-scaffold.sh` writes only `check.sh` (mode 0755), refuses to overwrite without `--force`, and prints the exact `hooks add` and `hooks trust` commands. It never edits the hub and never approves anything.

### remove, trust, untrust

```bash
synca hooks remove block-rm --dry-run
synca hooks remove block-rm
synca hooks trust block-rm        # a person at a terminal
synca hooks untrust block-rm
```

- `remove` deletes the hub entry and approval. Run `synca hooks sync` to delete it from agent files. JSON: `{dryRun, hook, change: "removed", hub}`.
- `untrust` removes the approval only. JSON: `{hook, trusted: false}`.
- `trust` has no JSON mode and prints its review to stderr.

### sync

```bash
synca hooks sync --dry-run
synca hooks sync --json
synca hooks sync --agents claude,grok
synca hooks sync --on-conflict keep-source --yes
```

The plan has these action kinds:

| Kind | Meaning |
| --- | --- |
| `write_hook` | Missing in the agent file |
| `update_hook` | synca wrote it before; the render changed |
| `remove_hook` | synca wrote it before; it is no longer desired |
| `adopt_hook` | The exact render is already present; record it in the lock without editing |
| `skip_hook_same` | Already correct |
| `skip_hook_unsupported` | Event, tool class, or agent unsupported |
| `conflict_hook` | `untrusted`, `modified_externally`, or `parse_error` |

- Unapproved hooks are `conflict_hook` with reason `untrusted`; they are skipped. Exit stays 0, or 7 with `--strict`.
- `--on-conflict skip` (default) reports conflicts and writes nothing for them.
- `--on-conflict keep-source` restores the hub version over a hand-edited entry; off a TTY it needs `--yes`. It never overrides `untrusted`.
- `--on-conflict keep-target` is not supported for hooks.
- `sync all` includes the hooks phase only when `~/.agents/hooks.json` exists and the scope is user. `--no-hooks` leaves it out. With `keep-target`, the skills/MCP phase uses that policy and the hooks phase uses `skip`.

Agent files:

- Claude `~/.claude/settings.json` and Devin `~/.config/devin/config.json` are shared. synca rewrites only the top-level `hooks` member, preserves every other byte, and normalizes whitespace inside `hooks`. An unparseable file is reported as `parse_error` and left untouched.
- Grok `~/.grok/hooks/synca.json` is owned by synca. A hand edit is `modified_externally`; `keep-source` restores it.
- An agent is written only when its config directory exists. Otherwise the hook is `skip_hook_unsupported` with reason `agent_not_installed`.
- Each write is atomic, preceded by a backup under `~/.local/state/synca/hooks/backups/` (last 20 per file). Files are re-hashed before writing; a file that changed during planning is skipped with `file_changed_during_apply`.

Plan JSON:

```json
{
  "dryRun": true,
  "scope": "user",
  "onConflict": "skip",
  "untrusted": 1,
  "actions": [
    {
      "kind": "conflict_hook",
      "hook": "block-rm",
      "agent": "claude",
      "path": "/Users/you/.claude/settings.json",
      "event": "PreToolUse",
      "reason": "untrusted"
    }
  ]
}
```

An apply adds `applied` with one line per written or skipped file. `sync all --json` adds an optional `hooks` object with the same shape.

### Lossy translation

| Code | Meaning |
| --- | --- |
| `failure_mode_weakened` | `onFailure: block` cannot be enforced by the agent; a script that exits 2 still blocks where supported |
| `tool_unmapped` | A requested tool class has no native name for that agent |

Lossy items appear under the action in text output and in `actions[].lossy` in JSON. They are warnings, not silent changes.

## Exit codes

| Exit | When |
| --- | --- |
| 2 | Usage: bad flag, unknown event or tool class, secret in a command, unknown agent |
| 3 | Unknown hook id |
| 5 | The operation failed (for example an unreadable hub) |
| 6 | `hooks trust` off a terminal, or not answered `yes` |
| 7 | `--strict` and a hook was skipped as unapproved |
| 8 | `hooks add` found an existing id |

## Inspect the contract

```bash
synca schema "hooks list" --json
synca schema "hooks add" --json
synca schema "hooks sync" --json
```

The embedded v1 schemas are in `go/internal/schema/v1/hooks-*.json`.
