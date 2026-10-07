---
name: synca
description: "Browse, install, and sync skills + MCP configs across coding agents (Grok, Devin, Cognition, OMP, Pi, Kiro, OpenCode, Claude, Cursor) via the `synca` CLI/TUI. Use when installing a skill or MCP server so every agent sees it, checking drift between canonical ~/.agents and per-agent dirs, resolving skill/MCP conflicts, or updating synca itself. Triggers: synca, sync skills, sync mcp, install skill for all agents, đồng bộ skills."
---

# synca

`synca` (binary at `~/.local/bin/synca`, releases `ngosangns/synca`) keeps one canonical copy of
each skill/MCP and fans it out to every agent. Prefer `synca` over hand-editing per-agent dirs —
hand-edited copies drift and synca cannot update them later.

## Mental model

- **Canonical skills**: `~/.agents/skills/<name>/` (user) · `<git-root>/.agents/skills/<name>/` (project)
- **Canonical MCP hub**: `~/.agents/mcp.json` (user) · `<git-root>/.mcp.json` (project)
- Agent dirs get **relative symlinks** to canonical; MCP writers translate per-agent format
  (TOML/JSON/OpenCode `mcp`) and preserve existing env secrets on the target.
- Agents: grok, devin, cognition, omp, pi, kiro, opencode, claude, cursor (+ canonical `agents`).

## Commands

```bash
synca                          # TUI (project scope = cwd → git root)
synca skills list [--scope user|project] [--json]
synca mcp list [--scope user|project] [--json]

synca skills install <path|git-url> [--scope user] [--dry-run]
synca skills remove <name>                # unlink agents, keep canonical
synca skills remove <name> --purge --yes  # also delete canonical

synca mcp list --scope user
synca mcp add <name> --transport stdio --command "npx -y @pkg/server"
synca mcp remove <name> --scope user

synca sync skills|mcp|all [--scope user|project] [--dry-run] [--on-conflict skip|keep-source|keep-target]
synca update [--check]
```

## Sync semantics

- **Always `--dry-run` first** when checking state — output lists every planned action
  (`ensure_canonical_copy`, `symlink_skill`, `ensure_mcp_hub`, `skip_same`, `skip_mcp_same`).
- `ensure_mcp_hub` is an idempotent upsert — it appears in every dry-run even when the hub is
  already correct.
- `--on-conflict` default `skip`: entries whose content differs are left untouched.
  `keep-source` = canonical wins; `keep-target` = agent copy promoted to hub then fanned out.
  Never silent overwrite.
- `"path exists ... resolve manually"` means the agent has a **real directory** (not a symlink).
  If `diff -rq <path> ~/.agents/skills/<name>` is clean, replace it with a relative symlink to
  canonical so it tracks updates; if it differs, that's a real conflict — ask before overwriting.
- A **dangling symlink** in an agent dir means canonical was removed — either restore canonical
  or delete the dead link; don't copy from it.

## Scope

- `user` (default): `~/.agents/...` + per-agent home dirs.
- `project`: `<git-root>/.agents/...` + repo-local `.devin/`, `.claude/`, `.grok/`… — only works
  inside a git repo.
- `--agents a,b` restricts targets.

## Updating synca

`synca update` checks GitHub Releases and installs to `~/.local/share/synca/bin/` with a symlink
at `~/.local/bin/synca` (TUI: `u`).

## TUI quick keys

`Tab` user↔project · `j/k` move · `s` sync focused · `S`/`M`/`A` sync all skills/MCPs/both ·
`i` install · `d` delete (`y` unlink / `p` purge) · `r` reload · `u` update · `q` quit.
Conflict prompt: `a` keep-source · `b` keep-target · `s` skip · `n` cancel.
