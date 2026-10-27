# Hooks sync across agents in Synca — analysis, design, plan

> Status: **partially implemented in synca 0.3.0** (see "Implementation status" below). Multica issue: GN-62 (t447u). Written 2026-10-23; revised twice the same day after the user's decisions (Q1, Q3, Q5, Q6, Q7, Q14, then Q17–Q20 — see "Decisions log").
> Scope: analysis + design + plan only. No code changes, nothing committed.
> Language: **English** (the repo is English-only, CHANGELOG 0.2.7; decision Q14). Identifiers are English. Text intended to ship in the repo (SKILL.md, MCP tool descriptions, help text) is drafted here in English as well.
> Synca version when written: 0.2.11. Agent versions installed on this Mac: grok 1.0.67, devin 3000.11.92, kiro-cli 2.33.0, opencode 1.18.34. Claude Code and Cursor CLI are not installed on this Mac (Cursor app has `~/.cursor`; Claude only has `~/.claude/skills`).

---

## 0. Summary

**Problem.** Skills and MCP already sync. Hooks (shell commands an agent runs on events: before/after a tool, on prompt submit, on stop…) are one file, one schema, one event-name set and one stdin/stdout contract per agent. Users copy them by hand, and several third-party tools (NS Adeck, Orca, axi-sdk-js, kirocrew) already write hooks into those same files.

**Key design decisions (details in §3–§5):**

1. **Hooks are a third resource type next to skills/MCP, but different in kind: they execute arbitrary code.** So the defaults are *safer* than for the other two: hub → agent is one-way, nothing is auto-imported from agents into the hub, and every hook is hash-pinned and must be approved by a human. **`sync all` includes hooks (decision Q3), but trust gating still applies**: untrusted or changed hooks are skipped and reported, never written; non-interactive runs never auto-trust (§3.5.1).
2. **Canonical store** = `~/.agents/hooks.json` (user) and `<git-root>/.agents/hooks.json` (project), `version: 1`, keyed by `id` like `mcpServers`. Scripts live in `~/.agents/hooks/<id>/` and agents call the absolute path directly (no copy → no drift; unlike skills, which use symlinks).
3. **Event taxonomy uses Claude Code's PascalCase names** (already the lingua franca: Grok, Devin, Cursor and Copilot read Claude files). 5 portable core events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`; 8 second-tier events with uneven support. Every (hook × agent) pair yields an explicit **lossy** report (`event_unsupported`, `blocking_weakened`, `contract_mismatch`…) — never silently dropped.
4. **Abstract tool classes** (`shell`, `read`, `write`, `search`, `web`, `agent`, `mcp`) are translated to each agent's tool names/regex. Raw `matcher` regex is allowed but must pin `agents` and is flagged non-portable.
5. **Ownership & merge:** prefer a *Synca-owned file* (Grok `~/.grok/hooks/synca.json`, Kiro V3 `~/.kiro/hooks/synca.json`, OpenCode generated plugin); for shared files (Claude `settings.json`, Devin `config.json`, Cursor `hooks.json`) add **one dedicated matcher-group per hook**, never merge into a user's group, tracked by an external lock file + content match → idempotent, stale hooks removable, "foreign" hooks (NS Adeck/Orca in the real `~/.config/devin/config.json`) preserved byte-for-byte.
6. **Format-preserving edit + backup + atomic write + compare-and-swap.** The current MCP writer re-marshals the whole file (loses key order, loses TOML comments) and keeps no backup — not good enough for `settings.json`.
7. **Trust model:** a new/changed hook must be approved by a human (TTY) via `synca hooks trust`; trust is stored locally (`~/.local/state/synca/hooks/trust.json`, 0600), never in a repo. **An agent (via MCP/skill) can never approve its own hook** — prompt injection → persistent code execution. Project hooks are untrusted by default.
8. **Compat graph:** Grok reads `~/.claude/settings.json` + `~/.cursor/hooks.json`, Devin reads `~/.claude/settings.json` + `~/.claude.json`, Cursor reads Claude settings → double-fire risk. The planner knows the graph; `doctor` warns `compat_double_fire`; default is to write each agent's native file and rely on dedupe where verified (Grok dedupes identical handlers; Devin/Cursor to verify in M0).
9. **Only registration is portable, not the script's I/O contract** (each agent reads/writes different JSON; only `exit 2 + stderr` is common). A hook declares `contract: portable|claude|shim`; the optional shim `synca hooks run` normalizes I/O (later).
10. **Project scope has two targets (decision Q5):** `local` (uncommitted, git-ignored; **default**) and `shared` (committed; opt-in by `--target shared` or per-hook `target` field). Synca verifies the local file is really ignored (adds a `.git/info/exclude` entry as a plan action) and **refuses** to write when it cannot prove that (Q19, no escape hatch; §3.14). Q18 adds a third visibility, `personal`: a definition that lives only in an uncommitted overlay `.agents/hooks.local.json` (§3.17).
11. **Kiro 2.x and V3 are both supported (decision Q6)** by two adapters behind one `kiro` agent; the mode is detected, never guessed, and a hook is never written to both layouts (§3.15).
12. **OpenCode gets a Synca-generated fixed JS plugin (decision Q7)** with an ownership header, template hash, hooks embedded as data (the plugin never reads the hub at runtime), no network, no `eval` (§3.16).
13. **MCP:** Synca has no MCP server today. Decision Q1 adds **`synca serve --mcp`** (stdio, thin, same `internal/core`, same schema v1; built on the official Go SDK pinned at v1.8.0 with an isolation layer and a fallback plan, §4.2.1) with a registry structured by tool group so skills / MCP-config / sync tools can be added later. v1 ships `synca_hooks_*` + `synca_doctor`; **there is no trust tool**; apply is two-step `plan → apply(planHash)` (§4).

**Estimate:** 34–44 working days sequential (±40%; +2 if the optional shim is built), split into M0–M5. Release 0.3.0 (CLI hooks, incl. `sync all` and the personal overlay) then 0.4.0 (MCP server + skill). The estimate grew from the first draft's 22–30 because of decisions Q1, Q3, Q5, Q6, Q7 and, in the second revision, Q18 (overlay, +2.5 days across M1/M3/M4) and Q20 (SDK isolation + notices, +1) (see §7).

---

## Decisions log (2026-10-23)

| # | Decision (user) | Where applied | Notes / interpretation |
| --- | --- | --- | --- |
| Q1 | **YES** — add `synca serve --mcp` as a real MCP server; design transport, hooks tools, a structure for future skills/MCP-config/sync tools, packaging, client config snippets. | §0.13, §4 (rewritten), §5, §6, M4 (3–4 d → 6–8 d), §7 risks, §3.13 | Supersedes the earlier "decide whether to do it" framing. `ARCHITECTURE.md` must be edited in M4 (MCP server is a second *caller* of `internal/core`, not a second sync engine). |
| Q3 | **YES** — `sync all` includes hooks. Trust gating still applies: untrusted/changed hooks are skipped and reported unless approved with `synca hooks trust` on a TTY; non-interactive runs never auto-trust. Define exit codes and output. | §0.1, §3.5 (new §3.5.1), §3.7 S1/S3, §3.9, §3.10 (additive `hooks` key), §3.12, §5 skill text, M3 | Supersedes the earlier proposal "`sync all` excludes hooks". Chosen exit semantics: skip ⇒ exit 0 by default (matches `--on-conflict skip` precedent and the GUI window's `sync all --on-conflict skip --json` call), exit 7 only with `--strict`. This is a design choice inside the decision — see Q17. |
| Q5 | **"yes"** (a yes/no-style answer to an either/or question: local *or* shared) — interpreted as **support BOTH** project scopes. **Default = local/uncommitted** (git-ignored location, ensure it is really ignored, warn otherwise). Shared is opt-in via flag/field. | §0.10, §3.5 scope paragraph, new §3.14, §3.9 (`--target`, per-hook `target`), §3.12, §3.7 | **Interpretation flagged:** a bare "yes" to "local or shared?" is ambiguous; implemented as both-with-local-default. It is isolated in one constant (`defaultProjectTarget = "local"`) plus the `--target`/`target` inputs, so flipping the default or dropping a target is a one-line change. |
| Q6 | **Support BOTH Kiro 2.x (agent-profile hooks) and V3 (file-based `~/.kiro/hooks/`).** Detect version, pick adapter, define behavior when both present. | §0.11, §2.1 (Kiro column), §3.4 table, new §3.15, §3.12, M0 item, M2 (2.5 d → 4 d) | The installed `kiro-cli 2.33.0` already has `~/.kiro/settings/permissions.yaml` (a V3-era artifact) while reporting 2.x, so detection is capability/probe-based with an explicit override, not just a version-number check. |
| Q7 | **YES** — Synca generates a fixed JS plugin for OpenCode. | §0.12, §3.4 table, new §3.16, §3.7 S12, §3.12, M2 (3 d → 3.5 d), §7 risks | Hooks are embedded in the plugin as data at sync time; plugin does not read hub config at runtime (keeps trust gating at sync time). |
| Q14 | **Plan must be in ENGLISH**; translate the whole document, keep Vietnamese out of the file. | Whole document | Done in this revision. |
| Q17 | **Skipped-untrusted hooks exit 0 by default; exit 7 only with `--strict`** (as proposed). | §3.5.1 (table and rationale), §3.9, §7 risks | No change from the proposal; the exit-code table was already written this way. |
| Q18 | **YES — add the personal, uncommitted overlay `<git-root>/.agents/hooks.local.json`.** Define location, merge/precedence, id collisions, trust, never-committed guarantee, `list`/`add --target`, MCP, schema, doctor, tests. | new §3.17, §0.10, §3.1, §3.2, §3.3, §3.5, §3.6, §3.7 (S4, S13), §3.9, §3.10, §3.11, §3.12, §4.4/§4.7/§4.8, §5, §6, M1 (4–5 → 5–6 d), M3 (4–5 → 5–6 d), M4 (+0.5 d) | Decisions inside the decision: overlay is project-scope only; no override of hub hooks (ids unique across layers, collision invalidates the overlay entry); always rendered to local files; trust key gains `layer`; the file is only created after `git check-ignore` proves it ignored, and re-verified on every run (a tracked/unignored overlay is disabled for that run); `--target` gets a third value `personal`. |
| Q19 | **REFUSE the write when git-ignore of a local file cannot be proven.** Keep diagnostics and any escape hatch minimal and explicit. | §3.14 step 5 + new "Refusal diagnostics", S13, §3.9 (flag removed), §3.12, §4.8, §6 | **No escape hatch in v1:** the earlier draft's `--allow-unignored` is **removed** (CLI, MCP and docs). Non-git directories cannot be project scope anyway; the remaining failures (a tracked file, a `!` negation in a tracked `.gitignore`, an unwritable exclude file) each have a user-side fix that the diagnostic prints, and `--target shared` is the explicit way to get a committed file. If you want a hatch later it is one per-invocation CLI flag, never persisted, never in MCP. |
| Q20 | **Use the official Go MCP SDK; pin the version; note spec compatibility, dependency/licensing/binary-size impact, fallback plan.** | new §4.2.1, §4.2, §4.4 (`sdk.go`), §6, M0 item 10, M4 (+0.5 d), M5 (+0.5 d: third-party notices), §7 risks | Pinned `go-sdk v1.8.0`. Measured in a scratch copy of the repo (discarded): stripped darwin-arm64 binary 7.35 MB → 9.09 MB (+1.74 MB), +1 direct and +8 indirect modules, MIT→Apache-2.0 licensing transition in the SDK, other modules MIT/BSD-style. Fallback: stay pinned; port the one adapter file; or hand-written stdio server (≈4–6 d). |

All open questions were decided on 2026-10-24 (user delegated them: pick the sensible default and record it) — see §8.1 and the "Implementation status" section.

---

## 1. Current state of Synca (read from the repo)

### 1.1 Architecture

| Package | Role |
| --- | --- |
| `cmd/synca` | entry |
| `internal/cli` | flag parsing, output, **no** sync rules. Commands: `skills`, `mcp`, `sync`, `provider clear`, `doctor`, `skill`, `update`, `version`, `schema` |
| `internal/core` | one package: models, agents (paths), discover, scan, sync, manage, doctor, redact |
| `internal/schema/v1` | JSON Schema 2020-12 for every command's output (`schema.Version = 1`), golden files in `internal/cli/testdata/contract/*.json` |
| `internal/updater` | `synca update` (darwin-arm64) |

`ARCHITECTURE.md`: "the CLI is the product; the `synca-gpui` window (repo `ns-gpui`) only calls the CLI; no daemon; no config file" — the CLI reads no config file; window state lives in macOS defaults (`synca.disabledProviders` → passed as `--agents`). Explicit out-of-scope: UI in this repo, command palette, extension host, a "second sync engine". Decision Q1 changes the "no MCP server" part: `ARCHITECTURE.md` gets edited in M4.

Current agents (`agents.go`): `agents` (hub), `grok`, `devin`, `kiro`, `opencode`, `claude`, `cursor`. Pi/OMP were dropped. Dependencies today: only `github.com/BurntSushi/toml` (plus indirect `uuid`, `x/mod`).

### 1.2 How skills/MCP are modeled

| | Skills | MCP |
| --- | --- | --- |
| User hub | `~/.agents/skills/<name>/` | `~/.agents/mcp.json` (`{"mcpServers":{…}}`) |
| Project hub | `<root>/.agents/skills/<name>/` | `<root>/.mcp.json` |
| How written to agents | symlink from the agent's skills dir to the hub (`symlink_skill`) | **rewrite the entry** in the agent's own config (JSON `mcpServers`, Grok TOML `[mcp_servers.x]`, OpenCode `mcp` key with `type: local/remote`) |
| Key | frontmatter `name` (`NormalizeKey`) | server name (`NormalizeKey`) |
| Compare | `HashSkillDir` | `McpFingerprint` over `McpNormalized` |
| Conflict | `conflict_skill` + `--on-conflict skip|keep-source|keep-target` | `conflict_mcp`, same policies |
| Direction | hub ⇄ agent: `ScanSkills` reads every agent, `pickPresence` picks the source, **auto-promotes an agent's copy into the hub** (`ensure_canonical_copy`/`ensure_mcp_hub`) | same |
| Secrets | none | `redact.go` masks env / URL password / secret-looking args; `--reveal` shows; the writer keeps an old secret env when the source lacks it (`mergeEnv`) |
| Instructions (AGENTS.md/CLAUDE.md…) | **not covered** by Synca today | — |

Machine output: `--json` one document; plan = `{dryRun, scope, onConflict, actions[], conflicts[]}`; `actions[].kind` is a **hard enum** in `schema/v1/sync-plan.json`; `conflicts[].skill` is a bool (cannot represent a hook). Exit codes: 2 usage, 3 unavailable, 5 failed, 6 needs-confirm, 8 conflict/drift; `ExitUnauthorized = 7` is declared with `codeName` "unauthorized" but **unused anywhere**. `--yes` is required off-TTY for destructive operations; dry-run does not need it. `skill install|check` ships an embedded SKILL.md (`//go:embed skill`); there are **two copies**, `skills/synca/SKILL.md` and `go/internal/cli/skill/SKILL.md`, currently identical (must be kept in sync).

How `sync all` works today (`runSync` in `internal/cli`): `PlanSyncSkills` + `PlanSyncMcp` → `MergePlans` → on a TTY with conflicts and `--on-conflict skip` a policy prompt (`s/a/b`) → `ApplyPlan`. The `synca-gpui` window calls `sync all --dry-run --json` for the plan and `sync all --on-conflict skip --json` to apply (`engine.rs` `sync_plan`/`sync_apply`); it parses JSON into `serde_json::Value`, so additive top-level keys are expected to be harmless (to be confirmed in M3).

### 1.3 Why hooks must differ from skills/MCP

| # | Difference | Design consequence |
| --- | --- | --- |
| 1 | Hooks **execute code** on agent events | trust, hash-pin, human confirmation, no unattended trust, no silent import |
| 2 | There is no "canonical" source to import from: agent hooks usually belong to third-party tools (Orca, NS Adeck) with their own env vars (`ORCA_PANE_KEY`, `NSADECK_SESSION`) | hub → agent one-way; `hooks import` only when a human asks |
| 3 | Target files are shared with other user config (`settings.json`; Devin `config.json` has `permissions`, `agent`, `shell`…) and **other tools also write there** | ownership tracking, foreign-preserving merge, format-preserving edit |
| 4 | Current writer: `json.MarshalIndent(map)` → **key order lost**, TOML encode → **comments lost**, `os.WriteFile` not atomic, **no backup** | hooks need a new editor + backup + atomic + CAS (do not reuse `writeJSONPretty`) |
| 5 | Agents read each other's files (compat) | planner must know it to avoid double execution |
| 6 | Event/tool names and I/O contracts differ; one agent has no hooks (OpenCode: JS plugins only) | canonical taxonomy + lossy report + generated plugin |
| 7 | Repo "English only" rule | skill/MCP text and this plan are in English |
| 8 | Project hooks can be committed into a repo someone else controls | local-by-default target, project hooks untrusted, ignore verification (§3.14) |

---

## 2. Research: hooks per agent

Sources: official docs (Claude Code `code.claude.com/docs/en/hooks`, Devin `docs.devin.ai/cli/extensibility/hooks`, Kiro `kiro.dev/docs/hooks` + `/docs/cli/v3/hooks-migration`, Cursor `cursor.com/docs/hooks` + `/docs/reference/third-party-hooks`, OpenCode `opencode.ai/docs/plugins`, Codex `developers.openai.com/codex/hooks`, Gemini `geminicli.com/docs/hooks`, Copilot `docs.github.com/copilot/reference/hooks-reference`, Windsurf `docs.devin.ai/desktop/cascade/hooks`) + **the docs shipped with Grok** (`~/.grok/docs/user-guide/10-hooks.md`, read in full) + real config on this Mac (read-only, no secrets printed). Confidence: ✅ read in docs/confirmed on the machine, ❓ docs conflict or are missing — needs M0.

### 2.1 Overview matrix: do they have hooks, where, what schema

| | Claude Code | Grok | Devin CLI | Kiro CLI | Cursor | OpenCode |
| --- | --- | --- | --- | --- | --- | --- |
| **Synca target?** | yes | yes | yes | yes (2.x **and** V3, decision Q6) | yes | yes |
| **Declarative hooks?** | ✅ | ✅ | ✅ | ✅ (2.x: inside an agent profile; V3: separate files) | ✅ | ❌ **JS/TS plugins only** (Synca generates one, decision Q7) |
| **User** | `~/.claude/settings.json` | `~/.grok/hooks/*.json` (own files, many), `~/.grok/config.toml` `[[hooks.<Event>]]`, `managed_config.toml` | `~/.config/devin/config.json` key `hooks` ✅ (on this Mac), `~/.claude/settings.json`, `~/.claude.json` | 2.x: `~/.kiro/agents/<profile>.json` key `hooks`; V3: `~/.kiro/hooks/*.json` | `~/.cursor/hooks.json` | `~/.config/opencode/plugins/*.{js,ts}`; or `plugin` in `opencode.json` (npm) |
| **Project** | `.claude/settings.json` (committed), `.claude/settings.local.json` (personal) | `<repo>/.grok/hooks/*.json` (also sub-directories down to cwd); needs folder-trust | `.devin/hooks.v1.json` (whole file is the hooks object), `.devin/config.json`, `.devin/config.local.json`, `.claude/settings*.json` | 2.x: agent profile in `.kiro/agents/` ❓; V3: `.kiro/hooks/*.json` | `<project>/.cursor/hooks.json` | `.opencode/plugins/` |
| **Schema** | `{hooks:{Event:[{matcher, hooks:[handler]}]}}` | Claude-like (matcher group) + TOML equivalent | Claude-like; `hooks.v1.json` has no wrapping key | 2.x: `{hooks:{agentSpawn:[{command,matcher}]}}`; V3: `{version:"v1",hooks:[{name,trigger,matcher,action:{type,command\|prompt},timeout,enabled,confirm}]}` | **flat**: `{version:1,hooks:{event:[{command,type,timeout,matcher,loop_limit,failClosed}]}}` (no matcher group) | none; the module returns `{ "tool.execute.before": fn, event: fn, … }` |
| **Handler types** | `command`, `http`, `mcp_tool`, `prompt`, `agent` | `command`, `http` | `command`, `prompt` | `command`, `agent` (V3: inject prompt) | `command`, `prompt` | JS code (may use `$` shell) |
| **Notable fields** | `args` (exec form), `if`, `async`, `shell`, `onFailure: continue\|block`, `statusMessage`, `once` | `args`, `env`, `timeout`; **`if` ignored** (handler skipped + error reported); `async/shell/statusMessage/headers` dropped | `fail_closed`, `timeout` | `timeout` (default 60 s, 0 = off), `enabled`, `confirm` (Stop) | `failClosed`, `loop_limit` (default 5), `timeout` | — |
| **Default timeout** | 600 s (30 s for UserPromptSubmit) | 5 s; 600 s for Stop/SubagentStop/PostToolUse; 30 s UserPromptSubmit | unknown ❓ | 60 s | platform default (not stated) | — |
| **Per-hook enable/disable** | no (only `disableAllHooks`) | `~/.grok/disabled-hooks`, Space in `/hooks` | unknown ❓ | V3: `enabled:false` | unknown ❓ | delete the plugin file |
| **Trust** | workspace trust for project hooks | folder-trust (`trusted_folders.toml`, `/hooks-trust`); even user hooks are held in an untrusted folder | unknown ❓ | not stated | workspace trust for project | n/a |
| **Reload** | file watcher auto-reloads | `/hooks` → `r` | unknown ❓ | when a session opens | auto-reload | on startup |
| **On this Mac** | not installed | ✅ 1.0.67; `~/.grok/hooks/` has `ns-adeck.json`, `orca-status.json` | ✅ `config.json` has 8 events, 2 groups each (NS Adeck + Orca) | ✅ 2.33.0; `~/.kiro/agents/kirocrew*.json` have `hooks.postToolUse`; `~/.kiro/settings/permissions.yaml` exists | no `hooks.json` | ✅ 1.18.34; plugins `axi-*.js` (header "axi-sdk-js managed opencode plugin"), `orca-opencode-status.js` |

Extended reference (**not** Synca targets today; kept to show the model generalizes and to design extension points): Codex (`~/.codex/hooks.json` or `[[hooks.<Event>]]` in `config.toml`; project `.codex/`; Claude-style matcher-group; **mandatory per-hook review/trust by hash**, a changed hook is skipped until re-trusted), Gemini CLI (`settings.json` key `hooks`; events `BeforeTool/AfterTool/BeforeAgent/AfterAgent/SessionStart/SessionEnd/PreCompress/Notification/BeforeModel/AfterModel/BeforeToolSelection`; `hooksConfig.disabled`), Copilot CLI (`.github/hooks/*.json`, `~/.copilot/hooks/*.json`, `~/.copilot/settings.json`, **also reads `.claude/settings.json`**; camelCase events; fields `bash`/`powershell`/`cwd`/`timeoutSec`), Windsurf/Cascade (`.windsurf/hooks.json`; events `pre_run_command`, `post_write_code`…, exit 2 blocks).

### 2.2 Event mapping (canonical → each agent)

Legend: ✅ native, ≈ approximate (lossy), ❌ unsupported, ❓ needs M0.

| Canonical | Claude | Grok | Devin | Kiro (2.x → V3) | Cursor | OpenCode (plugin) |
| --- | --- | --- | --- | --- | --- | --- |
| **SessionStart** | ✅ `SessionStart` (matcher `startup\|resume\|clear\|compact\|fork`) | ✅ `SessionStart` (matcher `new\|load`; Claude values do **not** match) | ✅ | ✅ `agentSpawn` → `SessionStart` | ✅ `sessionStart` | ≈ `event` `session.created` |
| **UserPromptSubmit** | ✅ (can block) | ✅ (can block; stdout on allow is dropped → no `additionalContext`) | ✅ | ✅ but **the CLI cannot block it** (non-zero exit is only forwarded as context) | ✅ `beforeSubmitPrompt` | ❓ not seen in the events docs (maybe `chat.message`, M0) |
| **PreToolUse** | ✅ | ✅ | ✅ | ✅ (2.x: exit 2 blocks; V3: docs conflict ❓) | ✅ `preToolUse` (+ `beforeShellExecution`, `beforeMCPExecution`, `beforeReadFile`) | ✅ `tool.execute.before` (block by `throw`) |
| **PostToolUse** | ✅ | ✅ | ✅ | ✅ | ✅ `postToolUse` | ✅ `tool.execute.after` |
| **Stop** | ✅ (exit 2/`decision:block` keeps the agent going) | ✅ (+ `StopCancelled`, `StopFailure` Grok-only) | ✅ | ✅ (V3: exit 1/`decision:block` → one more turn) | ✅ `stop` (`followup_message`) | ≈ `event` `session.idle` (cannot block) |
| SessionEnd | ✅ | ✅ | ✅ | ❓ (the availability table says CLI V3 has it, the trigger list does not) | ✅ `sessionEnd` | ❓ `session.deleted` |
| PreCompact | ✅ | ✅ | ❌ | ❌ | ✅ `preCompact` | ≈ `experimental.session.compacting` (edits the compaction prompt; different meaning) |
| PostCompact | ✅ | ✅ | ✅ `PostCompaction` | ❌ | ❌ | ≈ `session.compacted` |
| Notification | ✅ | ✅ (matcher `idle_prompt\|permission_prompt\|task_complete`) | ❌ | ❌ | ❌ | ❌ |
| PermissionRequest | ✅ | ❌ (only `PermissionDenied`) | ✅ | ❌ | ❌ | ≈ `permission.asked` |
| SubagentStart / SubagentStop | ✅ / ✅ | ✅ / ✅ | ❌ / ❌ | ❌ / ❌ | ✅ / ✅ | ❌ |
| PostToolUseFailure | ✅ | ✅ | ❌ | ❌ | ✅ | ❌ |

Outside taxonomy v1 (only via the `native` escape hatch): Kiro `PostFileCreate/Save/Delete`, `PreTaskExec/PostTaskExec`; Cursor `beforeTabFileRead`, `afterTabFileEdit`, `workspaceOpen`, `afterAgentResponse`, `afterAgentThought`; Claude `Setup`, `FileChanged`, `CwdChanged`, `WorktreeCreate`, `PreModelSwitch`…; Gemini `BeforeModel`…; Grok `StopCancelled`, `StopFailure`.

Kiro 2.x vs V3 event support (decision Q6): **2.x has only five events** (`agentSpawn`, `userPromptSubmit`, `preToolUse`, `postToolUse`, `stop`), so on 2.x the second-tier events and `SessionEnd` are `event_unsupported`; 2.x entries have `command` + `matcher` only (no `timeout`, no `name`, no `enabled`) → `field_dropped` for `timeoutSec`/`enabled`. V3 supports the V3 trigger list with `timeout`/`enabled`/`name`.

### 2.3 stdin / stdout / exit-code contract

| | Claude | Grok | Devin | Kiro | Cursor | OpenCode |
| --- | --- | --- | --- | --- | --- | --- |
| **Input** | stdin JSON snake_case: `session_id`, `cwd`, `hook_event_name`, `tool_name`, `tool_input`, `transcript_path`… | stdin one JSON line; sends **both** camelCase (`hookEventName`, `toolName`, `toolInput`, `sessionId`, `workspaceRoot`) **and** Claude-style `hook_event_name` | stdin JSON: `hook_event_name`, `tool_name` (`exec`, `edit`, `mcp__srv__tool`), `tool_input`, `session_id`, `prompt_id` | stdin JSON: `hook_event_name` (e.g. `agentSpawn`), `cwd`, `session_id`, …; V3 prompt via env `USER_PROMPT` | stdin JSON: camelCase `hook_event_name`, `tool_name` (`Shell`, `Read`, `Write`, `MCP:<name>`), `tool_input`, `conversation_id`, `workspace_roots` | function args `(input, output)`; no stdin |
| **Block** | exit 2 (stderr = reason) or JSON `hookSpecificOutput.permissionDecision: deny` | exit 2 or `{"decision":"deny"}`/`permissionDecision` | exit 2 or `{"decision":"block"}` | 2.x: exit 2 (preToolUse only). V3 ❓ | exit 2 or `{"permission":"deny"}`; **also accepts Claude's nested format** | `throw new Error(...)` |
| **Other exit** | does not block (unless `onFailure:"block"`) | always fail-open (a few exceptions: oversized reply, bad `updatedInput`) | does not block (unless `fail_closed:true`) | stderr warning | fail-open (unless `failClosed:true`) | n/a |
| **Special output** | `additionalContext`, `updatedInput`, `updatedToolOutput`, `continue:false`… | like Claude + `ask`/`defer`; **`updatedInput`/`updatedToolOutput` Grok/Claude only** | `additionalContext`, `updatedInput` (merge) | stdout (exit 0) is **added to the agent context** | `updated_input`, `additional_context`, `followup_message`… | assign `output.args` |
| **Env vars** | `CLAUDE_PROJECT_DIR`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_ENV_FILE` | `GROK_WORKSPACE_ROOT`, `GROK_HOOK_EVENT`, `GROK_SESSION_ID`, `CLAUDE_PROJECT_DIR` (also) | `DEVIN_PROJECT_DIR` | — | `CURSOR_PLUGIN_ROOT` (plugins) | — |
| **Runs via** | `sh -c` (or exec form with `args`) | `sh -c`; direct exec if `args` or the command is a single path | shell | shell, cwd = project root | shell (PowerShell on Windows) | inside the OpenCode process |
| **Order/parallelism** | parallel | sequential in config order, stops at first `deny`; dedupes identical handlers across sources | ❓ | ❓ | merged: deny > ask > allow | sequential in load order |

**Contract conclusion:** the *only* thing reliably portable across Claude, Grok, Devin, Cursor (and Gemini, Windsurf) is **`exit 2` + stderr to block, exit 0 to allow**; stdin carries `tool_name`/`tool_input` but **`tool_name` values differ** (Bash / run_terminal_command / exec / Shell). Everything else (JSON decisions, `updatedInput`, `additionalContext`) is not portable. Kiro, Copilot (`toolArgs` is a JSON *string*) and OpenCode (no stdin) differ even more.

### 2.4 Tool names & matchers

| Canonical class | Claude | Grok (accepts Claude names via alias) | Devin | Kiro | Cursor | OpenCode | Gemini (reference) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `shell` | `Bash` | `run_terminal_command` (`Bash`, `Shell` aliases) | `exec` | `shell` (category), `execute_bash` (2.x) | `Shell` | `bash`/`shell` ❓ | `run_shell_command` |
| `read` | `Read` | `read_file` | ❓ | `read` | `Read` | `read` | `read_file` |
| `write` | `Edit`, `Write`, `MultiEdit` | `search_replace`, `hashline_edit`, `write` | `edit` | `write` (`fs_write` 2.x) | `Write` | `edit`/`write` | `write_file`, `replace` |
| `search` | `Grep`, `Glob` | `grep`, `list_dir` | ❓ | ❓ | `Grep` | `grep`/`glob` | ❓ |
| `agent` | `Task` | `spawn_subagent` | ❓ | `delegate` | `Task` | `task` ❓ | — |
| `mcp` | `mcp__<srv>__<tool>` | `<srv>__<tool>` (also accepts `mcp__`) | `mcp__<srv>__<tool>` | `@<srv>/<tool>`, `@mcp` | `MCP:<tool>` | ❓ | `mcp_<srv>_<tool>` |

Matcher syntax: Claude/Grok — only characters `[A-Za-z0-9_\- ,|]` means an exact-name list, anything else is an *unanchored* regex; Devin/Cursor — regex; Kiro — name/category/`@mcp`/wildcards `*` `?`/regex; Kiro file triggers — regex over the path.

### 2.5 Compat graph (agents reading other agents' files) — the double-fire source

| Agent reads | Files outside its "home" that it also loads hooks from | Off switch |
| --- | --- | --- |
| Grok | `~/.claude/settings.json` (+local), `<proj>/.claude/settings*.json`, `~/.cursor/hooks.json`, `<proj>/.cursor/hooks.json` | `[compat.claude] hooks=false`, `[compat.cursor] hooks=false` in `~/.grok/config.toml` (readable) |
| Devin | `~/.claude/settings.json` (+local), `~/.claude.json`, `<proj>/.claude/settings*.json` | `read_config_from.claude` in Devin config (readable) |
| Cursor | Claude settings ("Include Third-Party Plugins, Skills, and Other Configs") | in-app setting (**not readable** from a file) |
| Copilot CLI (reference) | `.claude/settings.json` in the repo | — |

Real consequence: **Grok reports a load error** for any Cursor-style flat entry under an event Grok knows ("this Cursor hook won't run until Grok reads Cursor hook replies") — i.e. writing a Cursor-native entry into `~/.cursor/hooks.json` makes Grok print an error at every start (the hook still runs in Cursor). Grok dedupes *identical* handlers (same command+args+env) across sources; Devin/Cursor dedupe could not be verified.

### 2.6 Findings from real config on this Mac (read-only)

- `~/.grok/hooks/ns-adeck.json` and `orca-status.json`: each tool writes its **own file** into `hooks/` — a good precedent for the "Synca-owned file" strategy. Every hook is `sh -c 'if [ -n "$ORCA_PANE_KEY" ] … else cat >/dev/null; fi'` (no-op when the env var is absent) with `timeout: 10`.
- `~/.config/devin/config.json`: key `hooks` has 8 events (PermissionRequest, PostCompaction, PostToolUse, PreToolUse, SessionEnd, SessionStart, Stop, UserPromptSubmit); **each event has 2 separate matcher-groups** (NS Adeck, Orca) next to `permissions`, `agent`, `shell`, `theme_mode`, `version`. A `config.json.bak` exists. → Synca must add a 3rd group without touching the other two and must not treat them as "stale".
- OpenCode: plugins generated by `axi-sdk-js` carry the header `// axi-sdk-js managed opencode plugin … safe to edit only if you remove the managed marker` — precedent for an **in-file marker** on a tool-generated plugin (reused in §3.16).
- Kiro 2.33.0: hooks live in profiles `~/.kiro/agents/kirocrew*.json` (managed by the kirocrew tool, shaped `hooks.postToolUse:[{matcher:"execute_bash",command:…}]`). `~/.kiro/hooks/` does not exist. `~/.kiro/settings/permissions.yaml` exists even though the CLI reports 2.33.0 — a hint that V3-era files can appear before/without a "3.x" version string. → Kiro needs a capability-based mode detection (§3.15).
- Claude (`~/.claude/` only has `skills`) and Cursor (no `hooks.json`): no hooks to learn from; adapters rely entirely on docs → higher risk, needs M0.
- Grok has `~/.grok/config.toml` with `[ui.status_line] command=…` — **not a hook** (do not confuse when scanning).

---

## 3. Design

### 3.1 Principles

1. **Declare intent, not agent syntax.** The hub says "run script X before the shell tool, may block"; the adapter handles syntax.
2. **Never silent:** anything that cannot be mapped or gets weaker appears in the plan (`lossy[]` / `skip_hook_unsupported`).
3. **Safe before convenient:** a new/changed hook that a human has not approved never touches an agent file — in *every* command, including `sync all`.
4. **Idempotent and minimal:** running twice = 0 changes; only Synca's entries are touched; foreign entries stay byte-identical.
5. **Fits the existing architecture:** the CLI stays the primary product, `core` stays one package, `--json` output has a schema v1 + golden, no daemon, no new config file besides the hub (machine-local state is separate, §3.6). The MCP server (§4) is a second caller of `core`, not a second engine.
6. **Data-driven:** each agent's capability table is data (one file, `hooks_events.go`), not `if agent ==` scattered everywhere.
7. **Local by default for project scope:** a hook Synca writes into a repo is not committed unless the user asked for `shared` (§3.14), and a hook definition that must never reach teammates lives in the uncommitted personal overlay (§3.17). If Synca cannot prove an uncommitted file is git-ignored, it does not write it.

### 3.2 Canonical model

```text
Hook {
  id            string        // key, NormalizeKey, [a-z0-9-]{1,64}
  description   string?
  enabled       bool          // default true
  event         Event         // §3.2.1
  tools         []ToolClass?  // abstract matcher; tool-scoped events only
  matcher       string?       // raw regex/name; mutually exclusive with tools; requires agents
  handler       Handler       // type=command (v1)
  onFailure     "continue"|"block"   // default continue
  critical      bool          // true: must not be weakened by mapping (see §3.4)
  contract      "portable"|"claude"|"shim"  // which protocol the script speaks (see §3.8)
  agents        []Agent?      // allowlist; empty = every supporting agent
  excludeAgents []Agent?
  target        "local"|"shared"?   // project scope only; default "local" (§3.14); overlay hooks are always local (§3.17)
  files         []string      // scripts shipped under hooks/<id>/ (for hash + verify)
  requiresEnv   []string      // env var NAMES that must exist (names only, never values)
  native        {agent: rawEntry}?  // escape hatch, not portable, lint warns
}
Handler { type:"command", command:string, args:[]string?, timeoutSec:number, shell:"sh" }

HooksFile {
  version        1
  hooks          { id: Hook }
  agentSettings  { kiro: {mode:"auto"|"v2"|"v3", profiles:[string]} }?   // per-agent knobs (§3.15)
}
// The personal overlay `.agents/hooks.local.json` has the same shape minus `agentSettings` (§3.17).
```

#### 3.2.1 Event taxonomy v1 (Claude PascalCase names)

- **Tier 1 (portable core, ≥ 5 of 6 targets):** `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`.
- **Tier 2 (uneven support):** `SessionEnd`, `PreCompact`, `PostCompact`, `Notification`, `PermissionRequest`, `SubagentStart`, `SubagentStop`, `PostToolUseFailure`.
- Each event has attributes: `canBlock` (PreToolUse, UserPromptSubmit, Stop = yes), `toolScoped` (PreToolUse/PostToolUse/PostToolUseFailure/PermissionRequest), `matcherKind` (`tool`, `sessionSource`, `none`).
- Claude names are used because Grok/Devin/Cursor/Copilot/Codex already accept or map them; an adapter only needs a rename table (Kiro `agentSpawn`, Cursor camelCase).

#### 3.2.2 Abstract tool classes

`shell`, `read`, `write` (edit + write + multiedit), `search` (grep/glob/list), `web`, `agent` (subagent/task), `mcp` (optionally `mcp:<server>` or `mcp:<server>/<tool>`). Adapters translate to each agent's names/regex per §2.4. A class with no mapped name on an agent → `tool_unmapped` (warn; error for `critical`).

### 3.3 Canonical files

Locations (parallel to the MCP hub):

| | User | Project |
| --- | --- | --- |
| Definitions | `~/.agents/hooks.json` | `<git-root>/.agents/hooks.json` |
| Scripts | `~/.agents/hooks/<id>/…` | `<git-root>/.agents/hooks/<id>/…` |
| Personal overlay (never committed) | — (user hub is already personal) | `<git-root>/.agents/hooks.local.json`, scripts in `<git-root>/.agents/hooks.local/<id>/…` (§3.17) |

(The project MCP hub is `.mcp.json` because that is Claude's standard; hooks have no shared standard file, hence `.agents/hooks.json`.)

The project hub file is **meant to be committed** (it is the team's definition). What each developer's agents actually load is decided by the *target* (§3.14): by default the rendered agent files stay local and each developer must `trust` the hooks. A personal, uncommitted definition layer (`.agents/hooks.local.json`) sits next to it (§3.17, decision Q18).

Example:

```json
{
  "version": 1,
  "hooks": {
    "block-rm-rf": {
      "description": "Block rm -rf on the shell tool",
      "event": "PreToolUse",
      "tools": ["shell"],
      "handler": {
        "type": "command",
        "command": "{{hook_dir}}/check.sh",
        "timeoutSec": 10
      },
      "onFailure": "block",
      "critical": true,
      "contract": "portable",
      "files": ["check.sh"],
      "agents": ["claude", "grok", "cursor", "devin"]
    },
    "notify-done": {
      "event": "Stop",
      "handler": { "type": "command", "command": "afplay /System/Library/Sounds/Glass.aiff", "timeoutSec": 5 },
      "target": "shared"
    }
  },
  "agentSettings": { "kiro": { "mode": "auto", "profiles": [] } }
}
```

Conventions:

- camelCase keys in the hub file (like Claude/Cursor, which users and agents already know); `actions[]` in plans stay snake_case like today's `SyncAction`.
- Templates: **only** two variables, `{{hook_dir}}` (the hook's script directory) and `{{project_dir}}`. Adapters render them to absolute paths (user scope) or to the agent's variable (project scope: `$CLAUDE_PROJECT_DIR`, `$DEVIN_PROJECT_DIR`…; **a project file never contains the user's absolute path**; an agent without a project-dir variable → `scope_unsupported` for that hook).
- **No secret values in the file.** No literal `env` values. Only `requiresEnv: ["NAME"]`. Lint rejects `--token=…` style args, `Authorization` headers and strings matching `IsSecretKey` (reuse `redact.go`).
- `version` bumps on schema change; a file with a newer `version` than supported → error 5 "upgrade synca".
- Embedded JSON Schema `schema/v1/hooks-file.json` for agents/IDEs to validate (also `synca schema hooks-file`).

### 3.4 Mapping to agents and the lossy report

**Adapter** (pure interface, golden-testable, no I/O in `Render`/`Apply`):

```go
type HookAdapter interface {
    Agent() AgentKind
    Caps() HookCaps                         // event→{status,nativeName,canBlock,matcherKind}, handlerTypes, fields, compatReads
    Target(scope Scope, cwd string, t Target) (HookFileRef, error) // path, format, ownership mode (own-file|shared-file|plugin), ignore requirement
    Render(h Hook, rc RenderCtx) (Entry, []Lossy, error)           // pure
    ReadForeign(src []byte) ([]ForeignEntry, error)                // list/import/doctor
    Apply(src []byte, ops []EntryOp) ([]byte, error)               // pure: format-preserving edit
}
```

Kiro registers **two** adapters (`kiro2`, `kiro3`) behind `AgentKind = kiro`; a `KiroModeResolver` picks one per run (§3.15). OpenCode's `Apply` regenerates the whole plugin (§3.16).

**Lossy codes** (each has severity `info|warn|error`):

| Code | Meaning | Example |
| --- | --- | --- |
| `event_unsupported` | the agent has no such event | Devin has no `PreCompact`; Kiro 2.x has no `SessionEnd` |
| `event_approx` | approximate mapping | OpenCode `Stop` → `session.idle` |
| `matcher_translated` | `tools` translated to a native regex/name | `shell` → Devin `exec` |
| `matcher_raw_unportable` | raw `matcher` used on another agent | Claude regex for Kiro |
| `tool_unmapped` | a tool class has no counterpart | Devin `search` ❓ |
| `handler_type_unsupported` | `http`/`prompt` on an agent without it | (v1 is command-only, so rare) |
| `field_dropped` | a field is dropped | `onFailure` and `if` on Grok; `timeoutSec` on Kiro 2.x |
| `blocking_weakened` | the hook must block but the agent cannot block that event | Kiro CLI `UserPromptSubmit` |
| `failure_mode_weakened` | `onFailure:"block"` has no equivalent | Grok always fails open |
| `contract_mismatch` | `contract: claude` but the agent does not speak it | Kiro, OpenCode, Copilot |
| `timeout_clamped` | the agent re-applies a timeout value | — |
| `order_not_guaranteed` | ordering/parallelism differs | Claude parallel vs Grok sequential |
| `scope_unsupported` | the agent lacks this scope | OpenCode project has no project-dir variable ❓ |
| `compat_double_fire` | the agent also reads a file Synca writes | Devin ← `~/.claude/settings.json` |
| `target_local_unavailable` | `local` requested but the only file is tracked/shared | Cursor `hooks.json` already committed |
| `target_not_ignored` | a local file could not be proven git-ignored and Synca could not make it so → **refused, nothing written** (error; reasons `negated_by_gitignore`, `exclude_unwritable`, `check_ignore_failed`; §3.14) | a tracked `.gitignore` re-includes the path |
| `overlay_id_collision` | a personal overlay hook has the same id as a hub hook → the overlay entry is invalid (§3.17) | `ding-on-stop` added to the hub by a teammate |
| `overlay_invalid` | overlay violates its schema (forbidden field, `target:"shared"`) | — |
| `overlay_not_local` | the overlay is tracked or not ignored → the personal layer is disabled for this run (§3.17) | someone ran `git add -f` |
| `kiro_mode_undetermined` | cannot tell Kiro 2.x from V3 | no binary, mixed layouts (§3.15) |
| `kiro_profile_required` | Kiro 2.x needs an explicit profile | no `--kiro-profile` given |

Rule for `critical: true`: any `blocking_weakened`, `failure_mode_weakened`, `event_unsupported`, `tool_unmapped` → **error**, and the hook is *not* written for that agent unless `--allow-lossy`; `doctor` reports error-level drift. Non-critical: warn, still written.

Render table per agent (condensed; per-adapter detail in M2). "Local" and "shared" are the project targets from §3.14:

| Agent | Ownership | User target | Project **local** (default) | Project **shared** (opt-in) | Entry / marker | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| **Claude** | shared-file (own matcher-group per hook) | `~/.claude/settings.json` | `.claude/settings.local.json` | `.claude/settings.json` | group `{matcher,hooks:[{type:"command",command,args?,timeout,onFailure?}]}`; tracking via lock + content | use exec form (`args`) when possible; no name to use as marker. Claude only git-ignores `settings.local.json` when *it* creates the file — Synca still verifies (§3.14) |
| **Grok** | **own-file** | `~/.grok/hooks/synca.json` | `<root>/.grok/hooks/synca.local.json` (ignored) | `<root>/.grok/hooks/synca.json` | Claude-style schema; the whole file belongs to Synca (a `"_synca"` header key if M0 confirms Grok ignores unknown keys; otherwise lock only) | no `config.toml` edits at all; `if`/`onFailure` dropped (lossy); needs Grok folder-trust |
| **Devin** | shared-file | `~/.config/devin/config.json` key `hooks` | `.devin/config.local.json` | `.devin/hooks.v1.json` | Claude-style group | `config.json` has other content → format-preserving edit mandatory; matcher `shell`→`exec`, `write`→`edit`; `fail_closed` ↔ `onFailure:block` |
| **Kiro V3** | own-file | `~/.kiro/hooks/synca.json` | `.kiro/hooks/synca.local.json` (ignored) | `.kiro/hooks/synca.json` | `{version:"v1",hooks:[{name:"synca:<id>",trigger,matcher,action:{type:"command",command},timeout,enabled}]}` — `name` is the marker | see §3.15 |
| **Kiro 2.x** | shared-file inside an agent profile | **only profiles named explicitly** (`--kiro-profile` / `agentSettings.kiro.profiles`) | profile in `.kiro/agents/` ❓ | same ❓ | `hooks.<event>:[{command,matcher}]`; no marker → lock + content | never touches profiles Synca was not told about (kirocrew profiles are managed by another tool) |
| **Cursor** | shared-file | `~/.cursor/hooks.json` | `.cursor/hooks.json` only if the file is untracked and ignored; otherwise `target_local_unavailable` | `.cursor/hooks.json` | flat entry `{command,timeout,matcher,failClosed}`; `version:1` | camelCase events; flat entries make Grok report a load error (§2.5) ❓ M0 checks whether Cursor accepts matcher-groups |
| **OpenCode** | own-file (generated plugin) | `~/.config/opencode/plugins/synca-hooks.js` | `.opencode/plugins/synca-hooks.local.js` (ignored) | `.opencode/plugins/synca-hooks.js` | header marker + embedded hook data (§3.16) | no declarative hooks → Synca generates one fixed plugin from a template embedded in the binary |

**Insertion position:** each canonical hook = exactly one group/entry, appended at the end of the event array (never merged into a user's/tool's group). For Grok (chain stops at the first `deny`) a `priority: first|last` option is possible (v1.1).

### 3.5 Sync semantics

**Direction:** hub → agent (declarative). Unlike MCP/skills, `sync hooks` does **not** promote an agent's hook into the hub. Foreign hooks (the user's or another tool's) are listed by `hooks list --all` and enter the hub only through `hooks import --from <agent> --key <id>` (dry-run by default, requires `--yes`, and the imported hook starts as **pending trust**).

**Plan** = diff between *desired* (hub, enabled, trusted, filtered by `agents`/`target`) and *actual* (agent file contents matched against the lock):

| `kind` | When |
| --- | --- |
| `write_hook` | desired has it, actual does not |
| `update_hook` | entry written by Synca (per lock) but content ≠ desired (hub changed) |
| `remove_hook` | in lock but no longer desired (removed from hub / `enabled:false` / agent excluded / target switched) — removed only if the current entry **matches the recorded hash** |
| `skip_hook_same` | entry already matches |
| `skip_hook_unsupported` | event/agent unsupported (with lossy code) |
| `conflict_hook` | `modified_externally` (a Synca entry was hand-edited), `id_collision` (foreign entry identical but not in lock → treated as *adopt* unless content differs), `untrusted` (needs trust), `file_changed_during_apply`, `parse_error` |
| `adopt_hook` | entry exactly matches the render but is not in the lock → record in lock, do not edit the file (idempotent after lock loss) |
| `ensure_git_ignore` | a `local` target path (`purpose:"rendered"`) or the personal overlay (`purpose:"overlay"`) is not ignored → add a managed entry to `.git/info/exclude` (§3.14, §3.17); if it cannot be proven ignored afterwards, the dependent write is **refused** |
| `release_git_ignore` | a Synca-managed ignore entry is no longer needed (target switched to `shared`, or last hook removed) |

`--on-conflict` keeps today's meaning: `skip` (default, exit 0, listed); `keep-source` = hub wins (overwrites a hand-edited entry; needs `--yes` off-TTY); `keep-target` = bring the hand-edited agent entry back into the hub (hash changes → back to pending trust; needs `--yes`). **`untrusted` is not a conflict a policy can resolve** — neither `keep-source`, `--yes`, nor `--on-conflict` ever turns `untrusted` into a write.

**Idempotency:** deterministic render (fixed key order, no timestamps in entries), compare via `entryHash` of the normalized render; running twice = only `skip_hook_same`.

**Dry-run/diff:** `--dry-run` prints the plan and (with `--json`) a per-file unified `diff` (secrets redacted via `redact.go`); writes nothing, including lock/backup/ignore entries.

**Safe apply (per target file):**
1. Take an `flock` on `~/.local/state/synca/hooks/lock` (stops two `synca` runs at once).
2. Resolve symlinks (dotfile managers) and write the real file, keeping the symlink; the plan notes "file is a symlink to …".
3. Read the file, compute `preHash`; if ≠ the plan's `preHash` → `conflict_hook: file_changed_during_apply`, retry once (Claude/Grok may rewrite `settings.json` while running).
4. **Never overwrite an unparseable file** (JSONC/trailing comma/BOM/corrupt): `parse_error` (conflict class, §3.5.1), with a hint.
5. Back up the whole file to `~/.local/state/synca/hooks/backups/<UTC-ts>/<abs-path>` (keep 20 per file).
6. Pure `Apply` → new bytes; write a temp file in the same directory, `fsync`, keep mode/owner, `rename`.
7. Update the lock after a successful rename (lock is written last; a lost lock → next run `adopt_hook`).

**Format-preserving edit:** JSON through an editor that preserves key order/whitespace/comments (candidates: `tailscale/hujson` or a hand-written token-position patcher; decided in M0 by round-trip tests on a sanitized copy of `~/.config/devin/config.json` and sample `settings.json`); the own-file strategy (Grok/Kiro V3/OpenCode) avoids the problem entirely. **Do not reuse** `writeJSONPretty`/`toml.Encoder` for hooks. No TOML edits (Grok `config.toml`, Codex `config.toml`) in v1.

**Removing stale hooks:** `hooks remove <id>` removes from the hub or the personal overlay (wherever the id lives) only; the next `sync hooks`/`sync all` issues `remove_hook` at the agents (or `hooks remove --sync`). A container (event array / `hooks` object) is removed only if Synca created it (recorded in the lock's `createdContainers`). A stale `remove_hook` also removes a Synca-managed ignore entry when no managed file needs it.

**Enable/disable per agent:** `agents`/`excludeAgents` per hook, the `--agents` flag (the window passes `--agents` from `synca.disabledProviders`), and global `enabled:false`. Disabling an agent does **not** delete already-written hooks (same as today's `--agents` semantics); only a `sync` where the agent is excluded from *desired* yields `remove_hook` — and to avoid surprises a `remove_hook` caused by excluding an agent needs an explicit `--prune`.

**Scope:** `--scope user|project`, `--cwd`; project needs a git root (like today). A **project** canonical hook (hub or personal overlay) is only ever written to **project** agent files, never to user files. `--target local|shared` selects the project render target for hub hooks; overlay hooks are always local; see §3.14 and §3.17.

#### 3.5.1 `sync all`, `sync hooks`, trust gating, output, exit codes (decision Q3)

`sync all` now = skills + MCP + hooks. New: `sync hooks` (hooks only) and `sync all --no-hooks` (opt out; also the way to get the old behavior). `sync skills` and `sync mcp` are unchanged. `sync all` with no hub `hooks.json` behaves and prints exactly as today (the hooks phase is a no-op; the `hooks` JSON key is omitted).

**Security rule (unchanged by Q3):** a hook is written only if `trust` state is `trusted` for `(scope, hook, definitionHash)`. Therefore:

- **pending** (never approved) and **changed** (hash differs from the approved one: hook edited, script bytes changed, new `files`) hooks are **skipped** by `sync all`/`sync hooks`: action `conflict_hook` with `reason: "untrusted"` and `trust.state: "pending"|"changed"`. Nothing about them is written to any agent file, lock, or ignore file.
- Trust is per `(scope, hook, hash)`, **not** per agent: an already-trusted hook may be written to a newly detected agent without a new approval (same code, same hash). If this is too loose, see Q10.
- **Non-interactive runs never trust.** No flag (`--yes`, `--on-conflict keep-source`) and no environment variable approves a hook. Off-TTY, `synca hooks trust` exits 6 `needs-confirm` even with `--yes`.
- **Interactive TTY review** (only when stdin and stderr are terminals, not `--json`, not `--dry-run`, not `--no-prompt`): after the plan is printed and if ≥ 1 hook is `pending`/`changed`, `sync all`/`sync hooks` asks once on stderr: `N hook(s) need review. Review and trust now? [y/N]` (default **N**). On `y` it runs the same flow as `synca hooks trust` for each hook (shows verbatim command, script contents, target agents, lossy items; user retypes the id), then re-plans and writes the newly trusted hooks in the same run. `--yes` skips this prompt (it never answers it).

**Ordering and partial success:** skills → MCP → hooks, applied independently. A hooks skip or hooks failure never prevents skills/MCP from being applied; the final report says what was applied and what was not (there is no cross-resource transaction, as today).

**Text output** (additive, after the existing plan lines): hook actions as compact JSON lines like other actions, then one summary line on stderr when anything was skipped:

```text
Hooks: 2 written, 1 unchanged, 3 skipped (untrusted: notify-done, block-rm-rf, log-prompts). Run `synca hooks trust <id>` in a terminal to review.
```

**`--json` output:** the existing `sync all` document is unchanged for skills/MCP (`actions[]`, `conflicts[]` carry only skill/MCP entries). A new optional top-level key `hooks` holds a `hooks-plan` object (`actions`, `lossy`, `conflicts`, `trust.required[]`, `applied`). The key is omitted when the hub has no hooks. Rationale: `conflicts[].skill` is a bool and `actions[].kind` is a hard enum, so mixing hook entries into those arrays would break strict consumers; a separate key is additive. `sync-plan.json` gets an optional `hooks` property (`$ref` to the hooks-plan definition); the golden for a hub without hooks stays byte-identical.

**Exit codes:**

| Situation | `sync all` / `sync hooks` default | with `--strict` |
| --- | --- | --- |
| everything applied or unchanged | 0 | 0 |
| ≥ 1 hook skipped as `untrusted` (pending/changed); everything else applied | **0** (reported on stderr and in JSON `trust.required`) | **7** `unauthorized` — after applying everything that *can* be applied |
| unsupported/non-critical lossy skips | 0 | 0 |
| unresolved `conflict_hook` (`modified_externally`, `id_collision`, `parse_error`, `file_changed_during_apply`) | 0 (listed, like `--on-conflict skip` today) | 8 |
| `critical` hook not covered on a required agent (lossy error) | 0, hook not written, listed | 8 |
| `--on-conflict keep-*` off-TTY without `--yes` | 6 needs-confirm (unchanged rule) | 6 |
| hub `version` too new; I/O error writing a file; another `synca` holds the lock | 5 | 5 |
| usage error, no project root/hub | 2 / 3 | 2 / 3 |

Why exit 0 by default: (a) it matches the existing precedent — unresolved conflicts under `--on-conflict skip` exit 0; (b) the `synca-gpui` window runs `sync all --on-conflict skip --json` and a non-zero code would turn a normal "needs review" state into an apparent failure for users who never opted into hooks flow; (c) skipping is safe — nothing runs. The risk is that a script ignores the report; `--strict` exists for CI/scripts that must fail loudly, and the skill tells agents to read `hooks.trust.required`. **Decided (Q17):** exit 0 by default, exit 7 only with `--strict`.

`doctor` severities for the same states: `hooks_untrusted: pending` = **warn** (not drift, exit 0); `hooks_changed` (hash differs from approved) = **error** (exit 8).

**`provider clear <agent>`:** also removes the **Synca-managed hooks** of that agent (per lock), never foreign ones; listed in the plan (Q11).

### 3.6 Ownership, lock, local state

Machine-local state directory (not in hub/repo): `~/.local/state/synca/hooks/` (`$XDG_STATE_HOME` if set).

| File | Content | Mode |
| --- | --- | --- |
| `lock.json` | `{version, entries:[{layer:"shared"\|"personal", hook, agent, path(abs), locator, entryHash, definitionHash, target, syncaVersion, writtenAt, createdContainers}], ignores:[{repo, pattern, purpose:"rendered"\|"overlay", writtenAt}]}` | 0600 |
| `trust.json` | `{version, entries:[{scope:"user"\|"<abs project root>", layer:"shared"\|"personal", hook, definitionHash, approvedAt, via:"tty"}]}` | 0600 |
| `backups/<ts>/…` | copy of the file before writing | 0600 |

What counts as "Synca's": (1) own-file → the whole file; (2) shared-file → `lock` + content match; (3) inline marker when the agent surely ignores unknown keys (Kiro V3 `name:"synca:<id>"`, OpenCode plugin header; other agents only if M0 confirms). A lost lock is harmless: matching content → `adopt_hook`, non-matching → treated as foreign (untouched).

Why the lock is outside the hub: the hub (especially project) can be committed/shared; the lock holds absolute paths and local hashes, and trust **must not** live in a repo (a malicious repo could commit a fake "trust").

---

### 3.7 Security model

Hooks = arbitrary command execution triggered by an agent/LLM → Synca's highest-risk feature (more than skills/MCP).

| # | Measure |
| --- | --- |
| S1 | **No first-time auto-run:** a new/changed hook is `pending`. Neither `sync hooks` nor `sync all` writes a `pending`/`changed` hook; it becomes `conflict_hook: untrusted`, exit **0** by default and **7** `unauthorized` (already defined in `exit.go`) under `--strict` (§3.5.1). |
| S2 | **Hash pinning:** `definitionHash` = sha256(canonical JSON of the hook ‖ sorted list of (relpath, sha256(file)) for `files`). Any changed byte → `changed`. `synca hooks verify` compares disk against the pinned hash (detects a script edited after trust — residual risk: agents run the script at its absolute path, so a changed script needs no re-sync; `doctor` must catch it). |
| S3 | **Trust only through a human:** `synca hooks trust <id>` requires a TTY, prints the **verbatim command + script + target agents + lossy items**, and asks to retype the id. Off-TTY it refuses, even with `--yes` (unlike `mcp remove`, `--yes` here does not substitute for the human). The `sync all` TTY review prompt (§3.5.1) uses the same flow and is never auto-answered. No MCP tool approves trust (§4). |
| S4 | **Project hooks are untrusted by default** and are only written to project files (never spread to user files). Trust is bound to `(scope, layer, hook, hash)` (the `layer` keeps a personal hook's approval from carrying over to a hub hook, §3.17); a freshly cloned repo → `pending`. Agent-side gates are respected as well (Grok folder-trust, Codex hash review, Cursor workspace trust); Synca does **not** try to bypass them and does **not** write `trusted_folders.toml`. |
| S5 | **Secrets:** no secret values in the hub (§3.3); lint blocks them; all output/diffs go through `redact.go`; `hooks list` masks secret-looking `command`s. Only env var names are recorded (`requiresEnv`); `doctor` checks whether they are set (never prints values). |
| S6 | **Paths/env:** commands use resolved absolute paths (user) or the agent's project variable (project); no unquoted `~`/globs; prefer exec form (`args`) where the agent supports it to avoid shell injection from string concatenation; never concatenate untrusted data into a command; scripts must be `+x` (doctor checks). |
| S7 | **Lint:** warn on `curl … \| sh`, writes outside the project dir, `sudo`, `eval`; an `http` handler (if ever supported) only HTTPS/loopback and shows the host. |
| S8 | **Managed layers:** never write managed/enterprise locations (`/etc/grok`, `/Library/Application Support/Cursor`, Claude managed policy). `doctor` reads `allow_managed_hooks_only` (Grok) / `allowManagedHooksOnly` (Claude) when present and reports "user hooks disabled by policy". |
| S9 | **Fail-open vs critical:** every agent is fail-open by default (hook crash = no block). `critical` + `onFailure:"block"` counts as "protection" only when the agent has an equivalent (Claude `onFailure`, Devin `fail_closed`, Cursor `failClosed`); otherwise `failure_mode_weakened`. |
| S10 | **File permissions:** keep an existing file's mode, never widen; new agent config files 0644 (each agent's convention), state files 0600. |
| S11 | **An agent cannot grant itself permission:** `hooks add` via MCP/skill creates a `pending` hook; there is no path from MCP to `trust.json`; the MCP server also does not use client elicitation for trust (the client UI is mediated by the same LLM session). |
| S12 | **Generated code is fixed code (OpenCode plugin, §3.16):** the plugin is rendered from a template compiled into the binary; only trusted hooks are embedded as data; no `eval`/`new Function`/dynamic import/network; the plugin never reads the hub or any other file at runtime; `doctor` verifies template and data hashes. |
| S13 | **Local targets and the personal overlay must really be local:** before writing a `local` project file, or creating `.agents/hooks.local.json`, Synca proves the path is git-ignored and untracked; if it cannot, it **refuses — there is no override flag** (Q19) — so a hook or hook definition cannot be committed by accident (§3.14, §3.17). The ignore entry goes in `.git/info/exclude` (uncommitted), never into a tracked `.gitignore`. The overlay is re-verified on every run; a tracked/unignored overlay is disabled for that run. |

### 3.8 Command portability

- **OS:** Synca only runs on darwin-arm64; the standard shell is `sh -c` (macOS/Linux). Windows/PowerShell is out of scope (already stated); field `shell` accepts only `sh`.
- **Paths:** `{{hook_dir}}`/`{{project_dir}}` (§3.3). Scripts live in the hub and are shared by every agent (no copy).
- **Script contract (`contract`):**
  - `portable` (default): the script only uses `exit 2` + stderr to block and reads stdin JSON tolerantly (`tool_name` may be `Bash|run_terminal_command|exec|Shell`). Works on every target that has the event.
  - `claude`: the script reads/writes Claude-style JSON. Works on Claude, Grok (accepts `hookSpecificOutput`), Cursor (accepts the nested format), Devin (partly). Kiro/OpenCode → `contract_mismatch`.
  - `shim` (M2b, optional): register `synca hooks run --id <id> --agent <a>`; the shim normalizes stdin to canonical, calls the real script, translates output/exit code to the agent's protocol, re-checks the hash at run time (critical → fail-closed). Costs: depends on `synca` being reachable by the hook runner (absolute path), adds a few tens of ms per call, and a missing binary makes the hook fail open → lossy warning.
- `hooks events --agent X` shows the real tool/event names so a human/agent can write the script correctly.

### 3.9 CLI UX (consistent with `skills`/`mcp`)

```text
synca hooks list    [--scope --cwd --agents --all --target all|shared|local|personal --json]   # canonical + per-agent state; --all includes foreign
synca hooks events  [--agent NAME --json]                      # taxonomy + support matrix + tool names (kiro shows the detected mode)
synca hooks add <id> --event E [--tools shell,write | --matcher RE --agents a,b]
                     --command "…" [--timeout 10] [--on-failure block] [--critical]
                     [--agents …] [--contract portable|claude] [--target local|shared|personal]
                     [--dry-run] [--json]
synca hooks remove <id> [--yes] [--dry-run] [--json]           # hub only; destructive → needs --yes off-TTY
synca hooks enable|disable <id> [--json]
synca hooks import --from <agent> --key <foreign-id> [--dry-run --yes --json]   # explicit
synca hooks trust <id>|--all [--scope --cwd]                   # TTY only
synca hooks untrust <id>
synca hooks verify  [--scope --cwd --json]                     # disk hash vs trusted hash (read-only)
synca sync hooks    [--scope --cwd --agents --target local|shared --dry-run
                     --on-conflict skip|keep-source|keep-target --key id --yes
                     --allow-lossy --prune --strict --no-prompt --no-personal
                     --kiro-mode auto|v2|v3 --kiro-profile NAME --json]
synca sync all      [existing flags] [--no-hooks] [--target …] [--strict] [--no-prompt]   # now includes hooks (§3.5.1)
synca serve --mcp   [--read-only] [--tools hooks,doctor] [--allow-cwd DIR]                # §4
synca doctor        # adds a "hooks" section (additive)
```

- `--key <id>` for hooks is like `--key` of `sync skills|mcp`; `sync all` still rejects `--key`.
- `--target`: on `hooks add` / `hooks import` it picks where the hook is **defined and rendered**: `shared`, `local` (default) or `personal` (overlay, §3.17). On `sync` it only applies to project hub hooks and means the render target: precedence CLI flag > per-hook `target` field > default (`local`); overlay hooks are always local, `--target personal` on `sync` is a usage error. In user scope there is one file per agent and `--target` is rejected. `hooks list --target` filters by visibility.
- `--strict` (`sync hooks`, `sync all`, `doctor`): exit 7 if a hook was skipped as untrusted; exit 8 on unresolved hook conflicts or an uncovered `critical` hook (table in §3.5.1).
- Exit codes: 2 usage, 3 unavailable (no project root / no hub), 5 failed (write error, hub version too new, lock held), 6 needs-confirm (includes `hooks trust` off-TTY), **7 unauthorized (needs trust; only with `--strict` for sync)**, 8 drift/conflict (`doctor`, `--strict`).
- `hooks add` defaults to **dry-run off-TTY** and always creates a `pending trust` hook; it does not touch agent files.
- Mutate/destructive in `synca schema`: `hooks list|events|verify` = R; `hooks add|enable|disable|import|trust|untrust|sync hooks|sync all` = M; `hooks remove` = D; `serve` is not a data command (listed as `S`, a long-running server, with the tool kinds taken from the same table, §4.6).

### 3.10 JSON plan/outputs (schema v1, **new files**; `sync-plan.json` only gains an optional `hooks` property)

`sync-plan.json` has a hard `kind` enum and `conflicts[].skill` is a bool → hook entries are **not** mixed into `actions[]`/`conflicts[]`. New `schema/v1/hooks-plan.json`, `hooks-list.json`, `hooks-events.json`, `hooks-verify.json`, `hooks-file.json` (+ goldens in `cli/testdata/contract/`). `doctor.json` gets a `hooks` key (additive; the existing test only forbids *removing/retyping* fields). `sync-plan.json` gains optional `hooks` (a `$ref` to the hooks-plan definition) for `sync all`.

```json
{
  "dryRun": true, "scope": "user", "onConflict": "skip",
  "planHash": "sha256:…",
  "actions": [
    {"kind":"write_hook","hook":"block-rm-rf","agent":"claude","event":"PreToolUse","native_event":"PreToolUse",
     "path":"/Users/x/.claude/settings.json","locator":"hooks.PreToolUse","entry_hash":"sha256:…","target":"user","lossy":[]},
    {"kind":"skip_hook_unsupported","hook":"on-compact","agent":"devin","reason":"event_unsupported","detail":"devin has no PreCompact"},
    {"kind":"conflict_hook","hook":"notify-done","agent":"grok","reason":"untrusted","trust":"pending"},
    {"kind":"ensure_git_ignore","repo":"/Users/x/proj","pattern":"/.grok/hooks/synca.local.json"}
  ],
  "lossy": [
    {"hook":"block-rm-rf","agent":"grok","code":"failure_mode_weakened","severity":"error","detail":"grok hooks always fail open"}
  ],
  "conflicts": [{"hook":"notify-done","agent":"grok","reason":"untrusted","policy":"skip"}],
  "trust": {"required":[{"hook":"notify-done","definitionHash":"sha256:…","state":"pending","reviewCommand":"synca hooks trust notify-done --scope user"}]},
  "kiro": {"mode":"v3","source":"cli-version","detected":"3.1.0"},
  "applied": []
}
```

`planHash` = sha256 of (normalized actions ‖ `definitionHash` of relevant hooks ‖ `preHash` of target files ‖ `preHash` of `.git/info/exclude` ‖ `preHash` of `.agents/hooks.local.json` when the personal layer is included) — used for the safe `plan → apply` (MCP, §4). Additive layer/overlay fields are listed in §3.17 (JSON schema).

### 3.11 Doctor

`synca doctor` adds (read-only; exit 8 on error-level problems):

- `hooks_drift`: a Synca entry edited/missing; a stale entry still in an agent although removed from the hub.
- `hooks_untrusted` (warn) / `hooks_changed` (error): disk hash ≠ approved hash, or never approved.
- `hooks_lossy`: a critical hook not covered.
- `hooks_double_fire`: a hook will load from >1 source of the same agent (graph in §2.5; reads Grok/Devin compat flags when present).
- `hooks_policy_blocked`: managed-only disables user hooks.
- `hooks_env_missing`: `requiresEnv` missing (names only).
- `hooks_not_executable`: script lacks `+x` or does not exist.
- `hooks_parse_error`: an agent file cannot be parsed.
- `hooks_local_not_ignored` (error: a local file exists but is not provably ignored — Synca would have refused to create it) / `hooks_local_tracked` (error) / `hooks_shared_ignored` (warn): project target problems (§3.14).
- Overlay checks (§3.17): `hooks_overlay_not_ignored` (error), `hooks_overlay_tracked` (error), `hooks_overlay_parse_error` (error), `hooks_overlay_invalid` (error), `hooks_overlay_id_collision` (error), `hooks_overlay_orphaned` (warn), `hooks_overlay_scripts_orphan` (info), `hooks_overlay_untrusted` (warn).
- `hooks_kiro_dual_layout` (warn) / `hooks_kiro_mode_undetermined` (warn) / `hooks_kiro_stale_layout` (warn): §3.15.
- `hooks_plugin_outdated` / `hooks_plugin_edited` / `hooks_plugin_foreign` (OpenCode, §3.16).

### 3.12 Failure modes

| Situation | Behavior |
| --- | --- |
| Agent file does not parse (JSONC/corrupt) | not written, `conflict_hook: parse_error`; exit 0 by default / 8 with `--strict` (it is a conflict-class skip; other resources still sync); hint to fix by hand |
| Agent rewrites the file between plan and apply | CAS fails → retry once → `file_changed_during_apply` |
| Lock lost | `adopt_hook` if content matches; otherwise treated as foreign |
| User hand-edits a hook inside the agent | `conflict_hook: modified_externally`, per `--on-conflict` |
| Script hash changes after trust | `changed`; sync skips that hook; doctor reports |
| `rename` fails midway | original intact (temp file + rename), backup remains |
| Two `synca` processes | `flock` → the later one fails with 5 "another synca is writing" |
| Target is a symlink | write the real file, keep the link |
| Agent not installed / dir missing | like skills: skipped (never create the config dir of an uninstalled agent); exception: an own-file inside the hooks dir when the agent's root dir exists |
| Grok not folder-trusted | still writes; `doctor` notes "Grok ignores it until /hooks-trust" (Synca does not edit Grok's trust store) |
| Cursor/Grok print a load error because of flat entries | `compat_double_fire`/`compat_load_error` warning in the plan before writing |
| Synca downgraded / hub `version` newer | refuse to write, exit 5 |
| Local target not ignored / tracked | no write for that hook+agent; `target_not_ignored` (refused, with the diagnostic of §3.14) / `target_local_unavailable`; sync exit 0 (8 with `--strict`), `hooks add --target personal` exit 5 |
| `.git/info/exclude` unwritable, or a tracked `.gitignore` re-includes the path with `!` | `target_not_ignored`: **refused**, no write, no flag overrides it; the diagnostic prints the fix (§3.14) |
| Personal overlay tracked / not ignored / unparseable | personal layer disabled for the run, hub hooks still sync; `overlay_not_local` / `parse_error`; `doctor` error (§3.17) |
| Overlay id equals a hub id | overlay entry invalid (`overlay_id_collision`); its stale rendered entries are removed by the next sync (§3.17) |
| Kiro mode undetermined | skip Kiro hooks with `kiro_mode_undetermined` (info); never guess |
| Kiro hooks present in both layouts | write only the resolved layout; the other layout's managed entries are `stale` (need `--prune`) (§3.15) |
| OpenCode path holds a non-Synca file | never overwritten: `conflict_hook: not_owned` |
| OpenCode plugin edited by hand | `modified_externally`; `keep-source` regenerates it |
| `sync all`: hooks skipped as untrusted | skills/MCP still applied; exit 0 (7 with `--strict`); review hint printed (§3.5.1) |

### 3.13 Migration & versioning

- Hub `version: 1`. Future changes: adding an optional field does not bump; changing semantics → `version: 2` + `synca hooks migrate` (dry-run).
- Lock/trust have their own `version`; a different version → read-only, "needs migrate".
- Users with manual/other-tool hooks: **no automatic import**; `hooks list --all` shows them, `hooks import` when wanted. Orca/NS Adeck hooks should stay managed by those tools; Synca does not touch them.
- **`sync all` behavior change** (Q3): from 0.3.0, `sync all` also processes hooks. Compatibility: no hub `hooks.json` ⇒ identical behavior and output; hooks present ⇒ only already-trusted hooks are written; `--no-hooks` restores the old scope. This is why the change ships in a minor release with a CHANGELOG note and a SKILL.md update.
- Release: 0.3.0 (CLI hooks including `sync all`) → 0.4.0 (MCP server + skill). `VERSION` and `core.Version` bump together (the release script checks).
- `synca-gpui` window: keeps working unchanged (it parses `sync all` JSON as generic values; to be confirmed in M3 with the real window build); a Hooks tab (list, trust-pending badge pointing the user to a terminal) is a separate issue in `ns-gpui` once the JSON contract settles.

### 3.14 Project targets: local vs shared (decision Q5)

**Interpretation (flagged):** Q5 asked "local (uncommitted) or shared (committed)?" and the answer was a bare "yes". It is implemented as **support both, default local**. The default lives in one constant (`defaultProjectTarget = "local"`) and in the `--target` / hook `target` inputs; changing the default or removing a target is a small change (Q5 stays in the §8 table as "decided — interpretation to confirm").

**Definitions (project scope only):**

- `local`: the rendered agent file/entry is uncommitted — it exists only on this machine and must be git-ignored.
- `shared`: the rendered agent file/entry is meant to be committed so teammates (even without Synca) load it. Agent-side trust gates still apply to them.
- The hub `.agents/hooks.json` is committed in both cases (it is the definition). `local` means "each developer renders and trusts their own copy".

**Resolution:** per hook: CLI `--target` > hook field `target` > `local`. The plan groups hooks by (agent, target).

**Targets per agent** (extends the §3.4 table). Own-file agents split into **two files** so one can be ignored and one committed:

| Agent | local file | shared file |
| --- | --- | --- |
| Claude | `.claude/settings.local.json` | `.claude/settings.json` |
| Devin | `.devin/config.local.json` | `.devin/hooks.v1.json` |
| Grok | `.grok/hooks/synca.local.json` | `.grok/hooks/synca.json` |
| Kiro V3 | `.kiro/hooks/synca.local.json` | `.kiro/hooks/synca.json` |
| OpenCode | `.opencode/plugins/synca-hooks.local.js` | `.opencode/plugins/synca-hooks.js` |
| Kiro 2.x | profile in `.kiro/agents/` ❓ (M0) | same ❓ |
| Cursor | `.cursor/hooks.json` **only if** untracked and ignored | `.cursor/hooks.json` |

Cursor has a single file: it is either local (untracked + ignored) or shared. If a project has both local and shared Cursor hooks, shared wins and the local ones report `target_local_unavailable`. If `.cursor/hooks.json` is already tracked (the team committed it), a local Cursor hook is `target_local_unavailable` as well. Grok/Kiro V3/OpenCode read every matching file in their directory, so two files work; Grok/Kiro/OpenCode filenames end in `.local.*` to make the intent obvious.

**Ensure-ignore protocol (before writing any local file):**

1. Find the repo: `git rev-parse --show-toplevel`; the exclude file: `git rev-parse --git-path info/exclude` (works for worktrees/submodules where `.git` is a file).
2. `git ls-files --error-unmatch -- <path>`: if the path is **tracked**, ignoring it has no effect → `target_local_unavailable` (conflict class, no write).
3. `git check-ignore -q -- <path>` (works for not-yet-existing paths): if already ignored (by `.gitignore`, global ignore, or exclude) → no action.
4. Otherwise plan action `ensure_git_ignore`: append the anchored pattern (e.g. `/.grok/hooks/synca.local.json`) inside a managed block in `.git/info/exclude`:

   ```text
   # >>> synca managed: local hook files (do not edit) >>>
   /.grok/hooks/synca.local.json
   # <<< synca managed <<<
   ```

   `.git/info/exclude` is per-clone and never committed; a tracked `.gitignore` is **never** edited. It is recorded in the lock (`ignores[]`).
5. Re-run `git check-ignore`; if the path is still not ignored (a tracked `.gitignore` re-includes it with a `!` rule — those outrank `info/exclude` — the exclude file is read-only, `git` failed, odd `core.excludesFile` shadowing) → `target_not_ignored`: **refuse, write nothing for that target.** Decision Q19: there is no override flag, no environment variable, and no hub field; the earlier draft's `--allow-unignored` is removed. (A non-git directory never gets here: project scope needs a git root.)
6. `--dry-run` shows `ensure_git_ignore` but does not touch the exclude file.

**Refusal diagnostics (kept short and explicit).** Text output (stderr):

```text
refused: <path> is not git-ignored and synca could not make it so (<reason>).
A local hook file must never be committed. Add `<pattern>` to .git/info/exclude (or .gitignore) and re-run,
or use --target shared if you want a committed file.
```

For a tracked file: `refused: <path> is tracked by git, so it cannot be local. Run `git rm --cached <path>` or use --target shared.` JSON: a `conflict_hook` with `reason:"target_not_ignored"` (or `target_local_unavailable`), `detail` ∈ `negated_by_gitignore|exclude_unwritable|check_ignore_failed|tracked`, and `fix[]` with the literal pattern/command. Exit codes: `sync hooks`/`sync all` treat it as a conflict-class skip (0, or 8 with `--strict`); `hooks add --target personal` fails with 5 and creates nothing; MCP returns `isError` with the same envelope. `doctor` reports `hooks_local_not_ignored` for files that exist anyway.

**Shared checks:** writing a `shared` file prints "this file is committable; review `git diff` before committing". If the shared file turns out to be ignored → `hooks_shared_ignored` warning (the teammates would never get it). A hook switched from `local` to `shared` (or back) yields `remove_hook` at the old file + `write_hook` at the new one (+ `release_git_ignore` when the managed pattern is no longer needed); for the single-file Cursor case only the ignore entry changes.

**Interaction with trust (S4):** trust is keyed by `(project root, hook, hash)` regardless of target; a teammate cloning a repo with `shared` files gets whatever the agent does with committed hooks (agent-side gate) and has no Synca trust record, so `synca doctor` there reports the hook as untrusted (hooks written by someone else) — `hooks list --all` shows it as foreign.

### 3.15 Kiro: 2.x and V3 (decision Q6)

Two adapters behind `kiro`:

| | `kiro2` (agent-profile hooks) | `kiro3` (file-based hooks) |
| --- | --- | --- |
| User | `~/.kiro/agents/<profile>.json` key `hooks` — **only profiles named explicitly** | `~/.kiro/hooks/synca.json` |
| Project | `.kiro/agents/<profile>.json` ❓ | `.kiro/hooks/synca[.local].json` |
| Entry | `hooks.<event>:[{command,matcher}]`; events `agentSpawn`, `userPromptSubmit`, `preToolUse`, `postToolUse`, `stop` | `{name:"synca:<id>",trigger,matcher,action:{type:"command",command},timeout,enabled}` |
| Ownership | lock + content (no marker field) | whole file + `name` marker |
| Lossy | no `timeout`/`enabled`/`name` (`field_dropped`); 2.x has no tier-2 events | V3 trigger list; exit-code behavior of `PreToolUse` ❓ (docs conflict) |
| Profiles | needs `--kiro-profile NAME` (repeatable) or `agentSettings.kiro.profiles`; none → `kiro_profile_required` (info), no write | n/a |

**Detection (`KiroModeResolver`)**, never guessing:

1. **Override:** `--kiro-mode auto|v2|v3` > `agentSettings.kiro.mode` > `auto`.
2. **Probe the binary** (auto): `kiro-cli --version` (PATH, then `~/.local/bin`), 2 s timeout, minimal env, no network. Major ≥ 3 → `v3`; major 2 → `v2` candidate.
3. **Cross-check the filesystem:** V3 signals = `~/.kiro/hooks/` or `.kiro/hooks/` containing files; V2 signals = a `~/.kiro/agents/*.json` with an object-form `hooks`. (`~/.kiro/settings/permissions.yaml` is recorded as a *hint* only: it exists on this Mac next to `kiro-cli 2.33.0`.)
4. **Resolve:**

   | CLI | Filesystem | Result |
   | --- | --- | --- |
   | ≥ 3 | anything | `v3` (leftover profile hooks become `stale_other_layout`) |
   | 2.x | no V3 files | `v2` |
   | 2.x | V3 files present | M0 decides whether 2.x loads `~/.kiro/hooks/`; until proven it is **ambiguous** → `kiro_mode_undetermined` (use `--kiro-mode`) |
   | not installed | only V3 signals | `v3` |
   | not installed | only V2 signals | `v2` |
   | not installed | both or none | `kiro_mode_undetermined`, skip |

**When both layouts are present (or `--kiro-mode` contradicts the files):**

- V3 loads profile hooks *and* standalone files, so writing the same hook to both would **double fire**. Synca never writes one hook to both layouts: only the resolved layout gets `write_hook`/`update_hook`.
- Synca-managed entries left in the **other** layout are `stale_other_layout`; they are removed only with `--prune` and only when the entry still matches the hash in the lock.
- Foreign entries (kirocrew profiles etc.) in either layout are never touched.
- `doctor` reports `hooks_kiro_dual_layout` (warn: both layouts hold hooks) and `hooks_kiro_stale_layout`.
- **Upgrade 2.x → V3:** Kiro's `/upgrade-agent` converts profile hooks to array form and may rename entries (`agentSpawn-0`…), which also rewrites Synca-managed entries. They no longer match the lock → they become foreign, and the new V3 file is written too, so the hook fires twice until the converted copy is removed by hand. Synca detects this by content match (`stale_converted_entry`, with a manual-removal hint); it does not delete entries it cannot prove it owns. Recommended upgrade order: `synca sync hooks --prune --kiro-mode v3` **before** running `/upgrade-agent`, or remove the converted entries afterwards.
- `hooks events --agent kiro` prints the resolved mode, its source (flag/settings/cli-version/filesystem) and both capability columns.

### 3.16 OpenCode generated plugin (decision Q7)

OpenCode has no declarative hooks; Synca generates **one fixed plugin** that executes the trusted canonical hooks.

**Files and ownership**

| | Path |
| --- | --- |
| User | `~/.config/opencode/plugins/synca-hooks.js` |
| Project local (default) | `.opencode/plugins/synca-hooks.local.js` (git-ignored per §3.14) |
| Project shared | `.opencode/plugins/synca-hooks.js` |

OpenCode auto-loads files in these plugin directories; `opencode.json` is never edited. A file at the path **without** the Synca marker is never overwritten (`conflict_hook: not_owned`). With no hooks left for OpenCode the plugin file is removed (it is own-file).

**Header (ownership marker, versioning, hashes)** — precedent: axi-sdk-js plugin header (§2.6):

```js
// synca-managed-plugin: opencode-hooks
// plugin-schema: 1
// synca-version: 0.3.0
// template-sha256: <sha256 of the fixed template text, DATA excluded>
// data-sha256: <sha256 of the DATA literal below>
// DO NOT EDIT. Regenerated by `synca sync hooks`; hand edits are detected and reported.
import { spawn } from "node:child_process";
const DATA = /* synca:data:begin */ { "hooks": [ /* rendered hooks, see below */ ] } /* synca:data:end */;
export const SyncaHooks = async ({ client, directory, worktree }) => { /* fixed template body */ };
```

**Versioning / drift:** `plugin-schema` is an integer bumped when the template contract changes. Synca regenerates the full expected file (template + current desired DATA) and compares bytes. The two header hashes let `doctor` say *why* it differs: template older than this Synca → `hooks_plugin_outdated` (plan: `update_hook`); data differs → normal hub change (`update_hook`); recomputed hashes ≠ header → `hooks_plugin_edited` (`modified_externally`); no marker → `hooks_plugin_foreign`.

**How it reads hub config:** it does **not**. Trusted hooks are rendered into the `DATA` literal at sync time: `{id, definitionHash, event, nativeEvents[], toolNames[], command, args, timeoutSec, onFailure, critical, projectDir}`. Tool classes are already resolved to OpenCode tool names by the Go adapter, so the plugin has no class tables. Reasons: (1) trust gating stays at sync time — editing the hub cannot change what runs until a sync of trusted hooks; (2) no runtime file reads, so nothing for a prompt-injected agent to edit that the plugin would honor; (3) self-contained and hash-verifiable.

**Event mapping (rendered into `nativeEvents`)** — all subject to M0 verification of the real OpenCode event/tool names:

| Canonical | OpenCode hook | Blocking |
| --- | --- | --- |
| `PreToolUse` | `tool.execute.before` | exit 2 → `throw new Error(stderr)` |
| `PostToolUse` | `tool.execute.after` | no |
| `SessionStart` | `event` `session.created` (≈) | no |
| `Stop` | `event` `session.idle` (≈) | no |
| `SessionEnd` | `event` `session.deleted` ❓ | no |
| `PostCompact` | `event` `session.compacted` (≈) | no |
| `PreCompact` | `experimental.session.compacting` (≈, different meaning) | no |
| `PermissionRequest` | `event` `permission.asked` (≈) | no |
| `UserPromptSubmit` | `chat.message` ❓ (M0: exists? can it block?) | decided by M0 |

Unmapped events → `event_unsupported`; approximations → `event_approx` with the table's note.

**Execution semantics (in the template):** spawn `/bin/sh -c <command>` (or direct exec when `args` is set) with a Claude-shaped JSON on stdin (`hook_event_name`, `session_id`, `cwd`, `tool_name`, `tool_input`, plus `synca_tool_class`); env = process env + `SYNCA_HOOK_ID`, `SYNCA_AGENT=opencode`, `CLAUDE_PROJECT_DIR`-compatible `SYNCA_PROJECT_DIR`; a timer kills the child with SIGKILL at `timeoutSec`; stdout/stderr capped at 64 KiB. Blockable events: exit 2 → throw with stderr; with `onFailure:"block"` also throw on non-zero exit/timeout/spawn error; otherwise fail-open (log via `client.app.log` when available, else silent). Output JSON is ignored (`contract: claude` → `contract_mismatch`). Hooks run sequentially in DATA order.

**Security constraints (S12):** the template is reviewed source in the repo, embedded with `//go:embed`; imports are limited to `node:child_process`; no `fetch`/`node:net`/`node:http`, no `eval`/`new Function`/dynamic `import()`/`require`; DATA is a JSON literal only; the plugin never reads the hub, trust store, or any config file; only trusted hooks are included; a CI test greps the rendered plugin for forbidden constructs and runs it under `node --test` against fake children (skipped if `node` is absent). The plugin runs inside the OpenCode process with the user's privileges — the fixed template + hash-pin + doctor check is the mitigation, and unsupported-marking remains available as a kill switch (`--agents` excluding opencode, or `agents` in a hook).

**Other notes:** OpenCode loads plugins on startup (a restart is needed; `doctor`/plan note it). Same-path user file plus project file both load; double fire is possible if the same hook is in both scopes (not for a project hook — project hooks only go to project files).

### 3.17 Personal overlay `.agents/hooks.local.json` (decision Q18)

**Purpose:** a project hook *definition* that exists only on this machine and never appears in the team's committed hub — for example a desktop notification on `Stop`, or a personal guard the team did not agree to. This is different from the `local` *target* (§3.14), which keeps the *rendered agent file* uncommitted but whose definition still lives in the committed `.agents/hooks.json`.

**Three visibility values** (the single axis used by `--target` on `hooks add` and `hooks list`):

| `--target` | Definition lives in | Rendered into | Committed? |
| --- | --- | --- | --- |
| `shared` | `.agents/hooks.json` (hub), `target:"shared"` | shared agent files | definition **yes**, rendered files **yes** |
| `local` (default) | `.agents/hooks.json` (hub), `target:"local"` | local agent files | definition **yes**, rendered files **no** |
| `personal` | `.agents/hooks.local.json` (overlay) | local agent files | definition **no**, rendered files **no** |

**Where it lives (project scope only):**

| Path | Content |
| --- | --- |
| `<git-root>/.agents/hooks.local.json` | `{ "version": 1, "hooks": { … } }` — same `Hook` shape as the hub |
| `<git-root>/.agents/hooks.local/<id>/…` | scripts of overlay hooks; `{{hook_dir}}` resolves here for an overlay hook (and to `.agents/hooks/<id>/` for a hub hook) |

There is **no user-scope overlay**: `~/.agents/hooks.json` is already personal and uncommitted.

Overlay constraints (validated by `schema/v1/hooks-file-local.json` and by the loader): only `version` and `hooks` are allowed (`agentSettings` is rejected — Kiro mode/profile stay in flags or the hub); a hook's `target` may be absent or `"local"`, `"shared"` is rejected (`overlay_invalid`) — a personal hook is by definition never shared; all other fields (`agents`, `critical`, `contract`, `files`, …) behave as in the hub.

```json
{
  "version": 1,
  "hooks": {
    "ding-on-stop": {
      "event": "Stop",
      "handler": { "type": "command", "command": "afplay /System/Library/Sounds/Glass.aiff", "timeoutSec": 5 }
    }
  }
}
```

**Layers, merge order, precedence.** Within one project the effective hook set is the union of two layers: `shared` (hub `hooks.json`) then `personal` (overlay). There is **no override**: the overlay can add hooks but can never replace, disable, or shadow a hub hook. Hooks are rendered in a deterministic order — shared layer first, then personal, each sorted by `id` — into the agent files for their resolved target (overlay hooks share the *local* files with `local` hub hooks, each hook in its own matcher-group/entry, lock keyed by `(layer, hook, agent)`). The user-scope hub is a different scope and is never merged with project layers; a project hook never goes to user files (unchanged). Cross-scope order is whatever the agent does.

**Id collisions.**
1. At load, an id present in both layers → the **overlay entry is invalid** (`overlay_id_collision`, error); the hub hook is unaffected and is the only one rendered.
2. `hooks add <id>` refuses an id that exists in any layer of that scope (exit 5, nothing written).
3. A `git pull` that introduces a hub id equal to an existing personal id triggers (1). The personal hook's previously rendered entries become stale and the next sync issues `remove_hook` for them (they match the recorded hash); the diagnostic tells the user to rename the personal hook.
4. Ids are not required to differ from user-scope ids (separate scope, separate trust and lock keys).

**Trust.** Same trust store, same human-only gate (S3), but the key becomes `(scope root, layer, hook, definitionHash)`. `layer` is part of the key so trust earned by a personal hook cannot be inherited by a hub hook with the same id and bytes (the hub copy could have come from someone else), and vice-versa. Overlay hooks start `pending` — "personal" does **not** mean "trusted": an agent can write the overlay (via `synca_hooks_add`, or by editing the file), so the same pending → human-approved path applies. `definitionHash` of an overlay hook covers its definition plus the files under `hooks.local/<id>/`. The TTY review shows a `personal (uncommitted)` label.

**Never committed — how it is guaranteed and checked.**
1. **Creation gate:** `hooks add --target personal` (CLI or MCP) first plans `ensure_git_ignore` with `purpose:"overlay"` for the two patterns `/.agents/hooks.local.json` and `/.agents/hooks.local/` (managed block in `.git/info/exclude`, protocol of §3.14 steps 1–5). The overlay file is created **only after** `git check-ignore` proves both patterns ignored and `git ls-files` shows the path untracked. Otherwise: refuse, nothing created (Q19, no escape hatch).
2. **Re-verification on every run** (`hooks list`, `sync hooks`, `sync all`, `hooks trust`, `doctor`, every MCP tool that reads the project hub): if the overlay or its script directory is tracked or not ignored, the **personal layer is disabled for that run** — its hooks are not rendered, trusted, or written, and `overlay_not_local` is reported (`hooks_overlay_tracked` / `hooks_overlay_not_ignored`). Exit: `sync` 0 by default, 8 with `--strict`; `doctor` error (8). Cost: two cached `git` calls per process.
3. **Already-rendered entries are left alone** when the layer is disabled this way (removal needs a deliberate `sync`); `doctor` lists them as `hooks_overlay_orphaned`.
4. **Stated limits:** Synca cannot stop a user from running `git add -f`, deleting the managed `info/exclude` block, or another tool from committing ignored files; it detects the result on the next run (2). It installs no git hooks and never edits a tracked `.gitignore`.
5. **Per clone by construction:** the overlay is not present in other clones or machines; it must be recreated there.
6. A malformed overlay (`parse_error`) disables only the personal layer for that run (hub hooks still sync); an overlay in a non-git directory cannot exist (project scope requires a git root).

**CLI addressing**

| Command | Behavior |
| --- | --- |
| `synca hooks add <id> --target personal …` | creates/updates the overlay (after the creation gate); `--target local|shared` writes the hub as before; default `local` |
| `synca hooks list [--target all\|shared\|local\|personal]` | default `all`; every row shows `layer` and effective `visibility`; the JSON also reports overlay health (below) |
| `synca hooks remove\|enable\|disable\|trust\|untrust\|verify <id>` | the id is resolved across layers (ids are unique across them, so no flag is needed); the output names the layer |
| `synca hooks import --from <agent> --key <id> [--target personal\|local\|shared]` | default `local` (hub). Choose `personal` to import into the overlay |
| `synca sync hooks` / `sync all` (project scope) | include the overlay by default; `--no-personal` leaves it out; `--target local\|shared` does **not** change overlay hooks (always local); `--target personal` is a usage error (it names a definition location, not a render target) |

**MCP addressing.** `synca_hooks_list` gets a `target` filter (`all|shared|local|personal`, default `all`) and returns `layer` per hook plus the overlay health object. `synca_hooks_add` accepts `target: personal`; for it `confirm:true` is required when `dryRun:false` because the call also writes the managed `.git/info/exclude` block (shown in the dry-run as `ensure_git_ignore`). `synca_hooks_plan`/`apply` include the personal layer unless `includePersonal:false`; the overlay file's hash and the exclude file's hash are part of `planHash`. `synca_hooks_remove` resolves the id across layers. There is still no trust tool. Project-cwd guard (§4.2) applies. Resource: `synca://hooks/project/personal` (read-only view of the overlay).

**JSON schema.** New `schema/v1/hooks-file-local.json` (and `synca schema hooks-file-local`):

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "synca/schema/v1/hooks-file-local.json",
  "title": "Personal hooks overlay (.agents/hooks.local.json)",
  "allOf": [{ "$ref": "hooks-file.json" }],
  "properties": {
    "version": { "const": 1 },
    "agentSettings": false,
    "hooks": {
      "additionalProperties": {
        "properties": { "target": { "enum": ["local"] } }
      }
    }
  }
}
```

Changes to existing documents (all additive): `hooks-list.json` — `hooks[].layer` (`shared|personal`), `hooks[].visibility` (`shared|local|personal`), top-level `overlay: {path, present, tracked, ignored, state}` with `state` ∈ `ok|absent|not_ignored|tracked|parse_error|invalid`; `hooks-plan.json` — `actions[].layer`, `trust.required[].layer`, `ensure_git_ignore.purpose` (`rendered|overlay`), `overlay` health; `hooks-verify.json` — `layer`; `lock.json` entries and `trust.json` entries — `layer`; `doctor.json` — the checks below.

**Doctor checks (errors exit 8, warnings exit 0):**
- `hooks_overlay_not_ignored` (error), `hooks_overlay_tracked` (error) — the guarantee in point 2 failed;
- `hooks_overlay_parse_error` (error), `hooks_overlay_invalid` (error: forbidden fields, `target:"shared"`, schema violation);
- `hooks_overlay_id_collision` (error);
- `hooks_overlay_orphaned` (warn): entries rendered from a now-disabled/removed overlay hook remain in agent files;
- `hooks_overlay_scripts_orphan` (info): `hooks.local/<id>/` directories with no matching hook;
- `hooks_overlay_untrusted` (warn, same as other pending hooks).

**Tests** are listed in §6 (row "Personal overlay (Q18)").

---

## 4. Synca's MCP surface (decision Q1: a real MCP server)

### 4.1 Decision and architectural constraints

Synca has no MCP server today (`synca mcp …` only *manages the MCP config of agents*). Decision Q1 adds **`synca serve --mcp`**: a thin MCP server so agents can call Synca with structure instead of shelling out, with hooks as the first tool group and a registry designed to take skills / MCP-config / sync tools later. This widens `ARCHITECTURE.md`, which M4 edits ("the MCP server is a second caller of `internal/core`; still no daemon, still no second sync engine").

Constraints kept:

- **No daemon:** `synca serve --mcp` is a **stdio** process spawned by the agent per session (like any local MCP server): no listening port, no state between sessions, exits on stdin EOF. Every call re-reads state from disk.
- **No second engine:** handlers call the same `internal/core` functions as the CLI; a tool's result is exactly the CLI's `--json` v1 document (as `structuredContent`), and `outputSchema` comes from `internal/schema/v1`. The JSON document builders that currently live in `internal/cli` (e.g. `writeSyncJSON`) are extracted into a small shared package in M4 so CLI and server cannot diverge.
- New package `internal/mcpserver` (transport + parameter mapping only). `core` stays one package.

### 4.2 Transport, protocol, lifecycle

| Topic | Design |
| --- | --- |
| Transport | **stdio only** (newline-delimited JSON-RPC on stdin/stdout). Streamable HTTP is out of scope (it would need a long-lived listener = a daemon). `synca serve` without `--mcp` is a usage error (reserves room for future flags). |
| stdout discipline | stdout carries protocol only. `core` never prints; server logs go to **stderr** (`SYNCA_LOG=debug` optional). A test runs the real binary and asserts every stdout line is valid JSON-RPC. |
| TTY guard | if stdin is a terminal, print a hint to stderr and exit 2 ("this is a protocol server; register it with `synca mcp add`"). |
| Protocol version / SDK | official Go SDK `github.com/modelcontextprotocol/go-sdk`, **pinned at `v1.8.0`** (decision Q20); serves MCP `2026-07-28` (stateless, `server/discover`, per-request `_meta`) and negotiates down to `2025-11-25`/`2025-06-18`/`2025-03-26`/`2024-11-05` with the legacy `initialize` handshake — older agent clients must keep working, so both eras are tested. Compatibility table, dependencies, licensing, measured binary size and the fallback plan are in §4.2.1. |
| Deprecated features | do not rely on `roots`, `sampling`, `logging` (deprecated in `2026-07-28`). No elicitation: trust never goes through the client (S11). |
| Capabilities | `tools` (static list per process, `listChanged:false`), `resources` (read-only, no subscribe). No `prompts` in v1. No tasks extension (operations are fast; revisit if skill install over network is added). |
| Identity | `serverInfo`: `name:"synca"`, `version: core.Version`; every result carries `_meta.synca = {schemaVersion:1, syncaVersion}` so clients can detect a drifted binary. |
| Cancellation / timeouts | honor request cancellation through `context`; per-call default timeout 60 s; no call blocks on a TTY. |
| Concurrency | each agent session = its own process. Writes take the same `flock` as the CLI (§3.5), so a CLI run and several servers cannot interleave writes. |
| Flags | `--read-only` (register only read tools), `--tools hooks,doctor` (register groups), `--allow-cwd DIR` (repeatable). |
| Project scope guard | project-scope tools accept `cwd` but it must resolve to the git root containing the server's launch directory or a repo given via `--allow-cwd`. Otherwise a prompt-injected agent could point Synca at any repository. |


#### 4.2.1 SDK decision (Q20): official Go SDK, pinned

| Topic | Design |
| --- | --- |
| Dependency | `github.com/modelcontextprotocol/go-sdk` **pinned to `v1.8.0`** (released 2026-09-14) in `go.mod`/`go.sum`; no `replace`, no floating version, no pre-release. Upgrades are explicit PRs that must pass the black-box protocol suite (§6). |
| Spec compatibility | SDK `v1.7.0+` supports MCP `2026-07-28` (current: stateless, per-request `_meta`, `server/discover`) and keeps `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05` with the legacy `initialize` handshake. Synca serves all of them (tested both eras); roots/sampling/logging (deprecated in `2026-07-28`) are not used. |
| Toolchain | builds with the repo's Go (`go 1.27.0`): confirmed with `go1.27.0 darwin/arm64` in a scratch copy of the repo. |
| New modules | 1 direct + 8 indirect: `go-sdk v1.8.0`; `github.com/google/jsonschema-go v0.4.3`, `github.com/segmentio/encoding v0.5.4`, `github.com/segmentio/asm v1.1.3`, `github.com/yosida95/uritemplate/v3 v3.0.2`, `golang.org/x/oauth2 v0.35.0`, `golang.org/x/sync v0.20.0`, `golang.org/x/sys v0.41.0`, `golang.org/x/time v0.15.0`. Today Synca's `go.mod` has `BurntSushi/toml`, `google/uuid`, `golang.org/x/mod`. The SDK's `auth`/`oauthex`/streamable-HTTP packages are linked transitively by `mcp` even though Synca uses stdio only. |
| Binary size (measured) | scratch copy of the repo with the SDK linked into `cmd/synca` and one stdio tool: stripped (`-ldflags="-s -w"`) darwin-arm64 binary **7.35 MB → 9.09 MB (+1.74 MB, about +24%)**. The delta is smaller than a standalone server's (a bare SDK hello-server is 7.68 MB vs 1.59 MB for a hello-world) because Synca already links `net/http` for `synca update`. The scratch copy was discarded; nothing in the repo changed. |
| Licensing | the SDK's `LICENSE` states a transition **MIT → Apache-2.0** (new contributions Apache-2.0, older ones stay MIT); transitive modules are MIT (`jsonschema-go`, `segmentio/encoding`, `segmentio/asm`) or BSD-style (`uritemplate`, `golang.org/x/*`) per the `LICENSE` files in the module cache. All permissive; the obligation is to preserve notices when distributing binaries. M5 adds `THIRD_PARTY_NOTICES.md` (generated from the module list, checked in CI to match `go.mod`) and M0 confirms SPDX identifiers with a license scanner. |
| Isolation | only `internal/mcpserver` may import the SDK, and inside it only `sdk.go` (a thin adapter: build server, register a `ToolSpec`, register a resource, run stdio). Handlers are written against Synca's own `ToolSpec` and plain structs. A test fails the build if any other package imports `go-sdk` (`go list -deps` check). |
| Fallback if the SDK API breaks or the SDK becomes unusable | (1) **stay pinned** — a published module version is immutable, so a breaking `v1.x`/`v2` never forces an upgrade; (2) if a security fix exists only in an incompatible release, port `sdk.go` (one file) to the new API; (3) if the SDK is abandoned, unfixable, or its dependency/licensing profile becomes unacceptable, replace `sdk.go` with a hand-written minimal stdio server (JSON-RPC framing, `initialize` + `server/discover`, `tools/list`, `tools/call`, `resources/read`, cancellation; no HTTP/OAuth) — estimated **4–6 days** because the adapter seam and the SDK-independent black-box protocol suite and recorded wire fixtures (`initialize`, `server/discover`, `tools/list`, representative `tools/call`) already exist; (4) tool schemas come from `internal/schema/v1`, so they survive a backend swap unchanged. |
| Supply chain | `go.sum` verified in CI; `govulncheck` recommended as an M4 CI step (not a new decision); no vendoring by default (the repo does not vendor today). |

### 4.3 Packaging and client configuration

- **Packaging:** same binary, same `synca update`, same release script. No separate package/npm wrapper (darwin-arm64 only). Register with an absolute path where possible because GUI-launched agents may have a minimal `PATH`: `~/.local/bin/synca` is where the installer puts it.
- **Self-registration (supported path):** `synca mcp add synca --transport stdio --command "/Users/<you>/.local/bin/synca serve --mcp"` (plus `--dry-run` first). It writes the right shape for every agent Synca knows (Grok TOML, OpenCode `mcp`, Kiro/Claude/Cursor/Devin JSON). End-to-end test: register into a temp HOME, then drive the server over stdio.
- **Manual snippets** (for clients Synca does not manage; shapes taken from each agent's MCP docs, not run here — Claude, Cursor and Devin are not installed on this Mac):

```json
// Claude Code (.mcp.json / ~/.claude.json), Cursor (~/.cursor/mcp.json), Kiro (~/.kiro/settings/mcp.json)
{ "mcpServers": { "synca": { "command": "/Users/<you>/.local/bin/synca", "args": ["serve", "--mcp"] } } }
```

```toml
# Grok (~/.grok/config.toml)
[mcp_servers.synca]
command = "/Users/<you>/.local/bin/synca"
args = ["serve", "--mcp"]
```

```json
// OpenCode (opencode.json)
{ "mcp": { "synca": { "type": "local", "command": ["/Users/<you>/.local/bin/synca", "serve", "--mcp"], "enabled": true } } }
```

  Devin: use `synca mcp add` (Synca already knows Devin's file). Read-only variant: add `"--read-only"` to the args.

### 4.4 Structure for future tool groups

```text
go/internal/mcpserver/
  server.go        // New(opts) *Server: options, capabilities, serverInfo, stdio run
  sdk.go           // the ONLY file that imports go-sdk: build server, register ToolSpec/resource, run stdio (§4.2.1)
  registry.go      // ToolSpec{Name, Group, Kind(R|M|D), Title, Description, Input, Output, Handler}; Register(group); filtering by --tools/--read-only
  guard.go         // project-cwd guard, planHash verification, confirm checks
  errors.go        // core error → tool error envelope {error:{code,exit,message,hint}}
  resources.go     // read-only resources
  tools_hooks.go   // group "hooks"   (v1)
  tools_doctor.go  // group "doctor"  (v1)
  tools_skills.go  // group "skills"  (future)
  tools_mcpcfg.go  // group "mcp"     (future: manage agents' MCP config)
  tools_sync.go    // group "sync"    (future: sync skills|mcp|all)
```

- **One table, one truth:** each `ToolSpec` names its CLI command; the `Kind` (R/M/D) is compared in a test against `commandSpecs()` (the same table `synca schema` prints), and annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint:false`) are derived from `Kind`. A tool cannot claim to be read-only while its CLI twin mutates.
- **Adding a group = one new file + one `Register` call;** input/output schemas come from `internal/schema/v1`.
- **Shared write protocol:** every mutating group uses the same `plan → apply(planHash)` + `confirm:true` pattern implemented once in `guard.go`.

Rollout:

| Phase | Release | Tools |
| --- | --- | --- |
| v1 | 0.4.0 | `synca_hooks_*` (below), `synca_doctor` |
| v1.1 | later | read-only `synca_skills_list`, `synca_mcp_list` (secrets masked, `--reveal` is CLI-only), `synca_sync_plan` (skills+MCP+hooks, the `sync all` plan incl. `trust.required`) |
| v2 | later | mutating `synca_skills_*`, `synca_mcp_*`, `synca_sync_apply` using the same `planHash` protocol — which needs `planHash` added (additively) to `sync-plan.json` |

Whenever a `sync` tool exists it must keep the §3.5.1 rule: untrusted hooks are skipped and listed, never trusted.

### 4.5 Naming and annotations

Tool name = `synca_<group>_<verb>` (CLI `synca hooks list` → `synca_hooks_list`). Annotations: `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint:false`. Every tool takes `scope` (`user|project`, default `user`), `cwd`, `agents[]` like the CLI flags. Results: `structuredContent` = the v1 document, `content[0].text` = the same JSON (for clients without structured output). Errors: `isError:true` with the CLI envelope `{"error":{"code","exit","message","hint"}}`. Descriptions are English, short, and state "does not run or trust anything".

### 4.6 Hooks tools (server v1)

| Tool | Kind | CLI equivalent | Safety notes |
| --- | --- | --- | --- |
| `synca_hooks_list` | read | `hooks list` | `command` secret-masked; includes `trust`, `targets[]`, `layer`, `visibility` and the overlay health object; `target` filter `all|shared|local|personal` |
| `synca_hooks_events` | read | `hooks events` | support matrix and per-agent tool names so an agent can write correct hooks; Kiro shows resolved mode |
| `synca_hooks_plan` | read | `sync hooks --dry-run --json` | returns `planHash`, `trust.required[]` with the human `reviewCommand` |
| `synca_hooks_apply` | **mutate** | `sync hooks` | requires `planHash` + `confirm:true`; rejects `stale_plan`; **skips** untrusted/changed hooks and returns them in `trust.required`; **never** trusts; includes the personal layer unless `includePersonal:false`; never accepts `allowLossy`/`prune` (CLI-only); refuses writes whose git-ignore cannot be proven (no override exists) |
| `synca_hooks_add` | **mutate (hub or overlay)** | `hooks add` | writes the hub or, with `target:"personal"`, the uncommitted overlay (plus the managed `.git/info/exclude` block); hook is `pending`; `dryRun` defaults to `true`; `confirm:true` required when `dryRun:false` |
| `synca_hooks_remove` | **destructive (hub)** | `hooks remove` | `dryRun` defaults to `true`; needs `confirm:true`; agent files are cleaned on the next apply |
| `synca_hooks_verify` | read | `hooks verify` | disk hash vs approved hash |
| `synca_doctor` | read | `doctor` | includes the hooks section (§3.11) |

**Deliberately absent:** `synca_hooks_trust`, `synca_hooks_import`, and any tool that writes `trust.json`. Hooks are code execution; letting an agent approve a hook it (or an injected prompt) wrote = persistent code execution. When `plan`/`apply` meet an untrusted hook the result contains the CLI command for a **human**: `synca hooks trust <id> --scope user`.

### 4.7 Input schemas (condensed, JSON Schema 2020-12)

```json
{
  "synca_hooks_list": {
    "type": "object", "additionalProperties": false,
    "properties": {
      "scope": {"enum": ["user","project"], "default": "user"},
      "cwd": {"type": "string"},
      "agents": {"type": "array", "items": {"enum": ["grok","devin","kiro","opencode","claude","cursor"]}},
      "includeForeign": {"type": "boolean", "default": false},
      "target": {"enum": ["all","shared","local","personal"], "default": "all"}
    }
  },
  "synca_hooks_events": {
    "type": "object", "additionalProperties": false,
    "properties": {"agent": {"enum": ["grok","devin","kiro","opencode","claude","cursor"]}}
  },
  "synca_hooks_plan": {
    "type": "object", "additionalProperties": false,
    "properties": {
      "scope": {"enum": ["user","project"], "default": "user"},
      "cwd": {"type": "string"},
      "agents": {"type": "array", "items": {"type": "string"}},
      "key": {"type": "string", "description": "hook id; omit for all"},
      "target": {"enum": ["local","shared"], "default": "local", "description": "project scope only"},
      "kiroMode": {"enum": ["auto","v2","v3"], "default": "auto"},
      "kiroProfiles": {"type": "array", "items": {"type": "string"}},
      "includePersonal": {"type": "boolean", "default": true, "description": "project scope: include the personal overlay"}
    }
  },
  "synca_hooks_apply": {
    "type": "object", "additionalProperties": false,
    "required": ["planHash", "confirm"],
    "properties": {
      "scope": {"enum": ["user","project"], "default": "user"},
      "cwd": {"type": "string"},
      "agents": {"type": "array", "items": {"type": "string"}},
      "key": {"type": "string"},
      "target": {"enum": ["local","shared"], "default": "local"},
      "kiroMode": {"enum": ["auto","v2","v3"], "default": "auto"},
      "kiroProfiles": {"type": "array", "items": {"type": "string"}},
      "includePersonal": {"type": "boolean", "default": true},
      "planHash": {"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
      "confirm": {"const": true},
      "onConflict": {"enum": ["skip"], "default": "skip", "description": "keep-source/keep-target are CLI-only"}
    }
  },
  "synca_hooks_add": {
    "type": "object", "additionalProperties": false,
    "required": ["id", "event", "command"],
    "properties": {
      "scope": {"enum": ["user","project"], "default": "user"},
      "cwd": {"type": "string"},
      "id": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,63}$"},
      "event": {"enum": ["SessionStart","UserPromptSubmit","PreToolUse","PostToolUse","Stop",
                         "SessionEnd","PreCompact","PostCompact","Notification","PermissionRequest",
                         "SubagentStart","SubagentStop","PostToolUseFailure"]},
      "tools": {"type": "array", "items": {"type": "string", "pattern": "^(shell|read|write|search|web|agent|mcp(:[^/]+(/.+)?)?)$"}},
      "matcher": {"type": "string", "description": "raw regex; requires agents; not portable"},
      "command": {"type": "string", "description": "no secrets; may use {{hook_dir}}"},
      "args": {"type": "array", "items": {"type": "string"}},
      "timeoutSec": {"type": "number", "minimum": 1, "maximum": 600, "default": 10},
      "onFailure": {"enum": ["continue","block"], "default": "continue"},
      "critical": {"type": "boolean", "default": false},
      "contract": {"enum": ["portable","claude"], "default": "portable"},
      "agents": {"type": "array", "items": {"type": "string"}},
      "target": {"enum": ["local","shared","personal"], "description": "project scope only; default local; personal = uncommitted overlay (section 3.17)"},
      "dryRun": {"type": "boolean", "default": true},
      "confirm": {"type": "boolean", "default": false, "description": "required when dryRun is false"}
    }
  },
  "synca_hooks_remove": {
    "type": "object", "additionalProperties": false,
    "required": ["id"],
    "properties": {
      "scope": {"enum": ["user","project"], "default": "user"},
      "cwd": {"type": "string"},
      "id": {"type": "string"},
      "dryRun": {"type": "boolean", "default": true},
      "confirm": {"type": "boolean", "default": false}
    }
  }
}
```

Outputs: `synca_hooks_list` → `hooks-list.json`; `synca_hooks_plan` → `hooks-plan.json` (with `planHash`); `synca_hooks_apply` → `hooks-plan.json` with `dryRun:false` + `applied[]`; `synca_hooks_events` → `hooks-events.json`; errors use `needs-confirm` (6), `unauthorized` (7, with a `hint` containing the trust command for the human; only returned when *every* requested hook was untrusted and `key` was given — otherwise untrusted hooks are skipped and reported in `trust.required` as in the CLI default), `conflict` (8), `stale_plan` (5).

`synca_hooks_list` example (condensed):

```json
{"scope":"user","hooks":[{
  "id":"block-rm-rf","event":"PreToolUse","tools":["shell"],"enabled":true,"critical":true,
  "command":"/Users/x/.agents/hooks/block-rm-rf/check.sh",
  "trust":{"state":"trusted","definitionHash":"sha256:…"},
  "targets":[
    {"agent":"claude","state":"in_sync","path":"/Users/x/.claude/settings.json"},
    {"agent":"grok","state":"lossy","lossy":[{"code":"failure_mode_weakened","severity":"error"}]},
    {"agent":"kiro","state":"unsupported","reason":"event_unsupported"}
  ]}]}
```

### 4.8 Safety of the MCP write path

1. **Two steps bound by a hash:** `plan` → human/agent reviews → `apply(planHash)`. If the hub, scripts, target files or ignore file change between the two → `stale_plan`.
2. **`confirm:true` required** for apply and for any non-dry-run add/remove; `dryRun` defaults to `true` for add/remove; `onConflict` is `skip` only through MCP (overwriting is a human act through the CLI); `allowLossy` and `prune` are CLI-only, and **no tool or flag can override a refused git-ignore check** (Q19).
3. **A hook cannot run unless a human trusted it:** `apply` skips and reports `untrusted`; `add` creates `pending`.
4. **No secrets accepted:** lint rejects secret-looking parameters; descriptions say so.
5. **No arbitrary file tools:** only hub/overlay hooks and Synca-owned entries (per lock) can change; the only writes outside those are the managed `.git/info/exclude` block (for `local` project files and the personal overlay), always shown as an `ensure_git_ignore` action the caller confirms by hash (`plan`/`apply`) or by `confirm:true` (`add --target personal`).
6. **Project guard:** project-scope calls are limited to the launch repo or `--allow-cwd` repos (§4.2).
7. **Resources (read-only):** `synca://hooks/{scope}` (hub), `synca://hooks/project/personal` (overlay, only when verified ignored), `synca://hooks/events` (taxonomy), `synca://hooks/agents/{agent}` (capabilities). No prompts in v1.
8. Tool text is English (repo rule), short, "does not run or trust anything".

---

## 5. Synca skill

Update **both copies** `skills/synca/SKILL.md` and `go/internal/cli/skill/SKILL.md` (currently identical; add `TestSkillCopiesIdentical` so they cannot drift), then the user runs `synca skill install`.

### 5.1 Draft (text to add/change in SKILL.md, English because it ships)

Frontmatter `description`: add "hooks" and triggers: `synca hooks, sync hooks, hooks for every agent, PreToolUse, SessionStart, hook doctor, hook trust, synca serve --mcp`.

New content (draft):

```markdown
## Hooks

Hooks run shell commands when an agent fires an event (before a tool, on prompt submit, on stop).
They execute arbitrary code, so they are stricter than skills and MCP.

Hard rules:
- Never edit agent hook files by hand (`~/.claude/settings.json`, `~/.grok/hooks/*`, `~/.cursor/hooks.json`, `~/.config/opencode/plugins/synca-hooks.js`, ...). Use `synca hooks` and `synca sync hooks`.
- Never run `synca hooks trust`, never answer a trust prompt, and never try to approve a hook yourself. Trust is the user's decision. When a plan reports `untrusted`, `trust.required`, or exit 7, show the user the exact command and stop.
- Show the user the full command (and script) of any hook before you add it. Do not put secrets in a hook; use `requiresEnv` with a variable name.
- `sync all` includes hooks, but only hooks the user already trusted are written; the rest are skipped and listed under `hooks.trust.required`. Always run `sync all --dry-run` first and read that list. `--no-hooks` leaves hooks out.
- Project hooks are `local` (git-ignored, not committed) by default. Use `--target shared` only when the user wants the file committed.
- Use `--target personal` for a hook only this user wants: it goes to the uncommitted `<git-root>/.agents/hooks.local.json`. Never `git add -f` it, never remove its ignore entry, and never move its content into `.agents/hooks.json` without the user asking.

Layout:
- Hub: `~/.agents/hooks.json` (user), `<git-root>/.agents/hooks.json` (project). Scripts: `~/.agents/hooks/<id>/`.
- Only the 6 synca agents are targets: grok, devin, kiro (2.x and V3, auto-detected), opencode (generated plugin), claude, cursor.

Recipes:
  # What can each agent do? (events, tool names, blocking)
  synca hooks events --agent cursor --json
  # Add a guard: dry-run first, then ask the user to trust it
  synca hooks add block-rm-rf --event PreToolUse --tools shell --command "{{hook_dir}}/check.sh" --critical --dry-run
  synca hooks add block-rm-rf --event PreToolUse --tools shell --command "{{hook_dir}}/check.sh" --critical
  # Preview what would be written to each agent
  synca sync hooks --dry-run --json
  # Apply (only trusted hooks are written)
  synca sync hooks
  # Health
  synca doctor --json
  synca hooks verify --json

Writing a portable hook script:
- Read JSON on stdin tolerantly. `tool_name` differs per agent (`Bash`, `run_terminal_command`, `exec`, `Shell`); match on the command text or use `--tools shell` and let synca translate the matcher.
- To block: print the reason to stderr and `exit 2`. Do not rely on JSON decisions unless `--contract claude`.
- Exit 0 to allow. Any other exit code does NOT block on most agents.
- Keep it fast (default timeout 10s) and idempotent. Print nothing else on stdout.

Reading the plan:
- `skip_hook_unsupported`: that agent has no such event. Not an error unless the hook is `critical`.
- `lossy[]` with `severity: error` means the guard would be weaker on that agent (for example Grok always fails open). Tell the user.
- `conflict_hook` reasons: `untrusted`, `modified_externally`, `parse_error`, `file_changed_during_apply`, `not_owned`.
- `target_not_ignored` / `target_local_unavailable` / `overlay_not_local`: a local or personal hook could not be kept out of git, so synca refused. No flag overrides this. Show the user the fix printed in the diagnostic; do not edit ignore files yourself.
- `kiro_mode_undetermined` / `kiro_profile_required`: ask the user for `--kiro-mode` / `--kiro-profile`.
Exit codes: 6 needs-confirm, 7 unauthorized (hook not trusted; with `--strict`), 8 conflict/drift.
```

"Mutate rule" additions: *read-only* = `hooks list|events|verify`, anything with `--dry-run`; *mutating* = `hooks add|remove|enable|disable|import|untrust`, `sync hooks`, `sync all`; *human-only* = `hooks trust` (and the trust prompt in `sync`). "Commands": add the `synca hooks …`, `synca sync hooks`, `synca serve --mcp` lines. "Failures": add exit 7.

When the MCP server exists (M4) add: "If the synca MCP tools are available, prefer `synca_hooks_plan` → `synca_hooks_apply`; there is deliberately no trust tool. Register the server with `synca mcp add synca --transport stdio --command \"<abs path to synca> serve --mcp\"`."

### 5.2 Skill tests

The existing `skill check` drift test plus an embed test; a test "skill text mentions every hooks command in `commandSpecs()`" (cross-checked against `synca schema`) so the skill cannot go stale.

---

## 6. Test strategy

Repo-wide rules: no real agents, no OAuth; fake HOME via `t.TempDir()`; CI macOS vet+test+build.

| Layer | Content |
| --- | --- |
| **Per-adapter golden** | `testdata/hooks/<agent>/{in.json|in.toml, hub.json, out.*}`: pure `Render` + `Apply`, byte-for-byte. Each adapter ≥ 8 cases: empty file, file with foreign hooks, two foreign tools side by side (simulating the **sanitized** Devin `config.json` with NS Adeck + Orca), add/change/remove, a container created by Synca, unsupported event, critical lossy. Kiro: both adapters, both modes. |
| **Foreign preservation** | property test: for every sample file, bytes outside Synca's entries are unchanged after `Apply` (key order, indent, comments where the format supports them). |
| **Round-trip** | `Render → ReadForeign → compare` yields the canonical (as far as describable). |
| **Idempotency** | `PlanSyncHooks` after `ApplyHooksPlan` = only `skip_hook_same`; deleting the lock → `adopt_hook`, no file write. |
| **Trust/hash** | one changed byte of script/hook → `changed`; off-TTY `trust` refuses even with `--yes`; an untrusted project hook never leaks into a user file; trust store 0600. |
| **`sync all` trust gating (Q3)** | untrusted/changed hooks are never written by `sync all`/`sync hooks` under any flag combination (`--yes`, `--on-conflict keep-source`); exit 0 default, exit 7 with `--strict`; skills/MCP still applied when hooks are skipped; TTY prompt default-N and never answered by `--yes`; JSON `hooks.trust.required`; no `hooks.json` ⇒ output byte-identical to pre-0.3.0 golden; `--no-hooks`. |
| **Safe writes** | CAS (file changed between plan/apply), `parse_error` writes nothing, symlink preserved, backup created and pruned at 20, concurrent `flock`, simulated power loss (kill after temp write). |
| **Local/shared targets (Q5, Q19)** | temp git repos: ignored path → no action; unignored → `ensure_git_ignore` writes only `.git/info/exclude` (never `.gitignore`); tracked file → `target_local_unavailable`; a tracked `.gitignore` with `!` re-including the path → `target_not_ignored` and **no write**; read-only exclude → refused; worktree and submodule (`.git` as file); switching target moves entries and releases the ignore; Cursor single-file rules; shared file ignored → warning; **assert there is no flag, env var or MCP parameter that bypasses the refusal**; diagnostic text and JSON `detail`/`fix[]` golden. |
| **Personal overlay (Q18)** | creation gate: overlay is created only after both patterns are provably ignored and untracked (otherwise exit 5, no file, no exclude change beyond the managed block); re-verification: `git add -f` of the overlay or removal of the exclude block disables the personal layer for the run (`overlay_not_local`, exit 0 / 8 `--strict`, `doctor` error) while hub hooks still sync; merge: shared then personal, sorted by id, deterministic bytes; collisions: same id in both layers → overlay entry invalid, hub unaffected, `hooks add` refuses, stale rendered entries removed next sync; trust: personal hook starts `pending`, hand-edit → `changed`, trust key includes `layer` (a personal approval does not transfer to a hub hook with identical bytes), `hooks.local/<id>/` bytes are in the hash; `--target personal`/`--target all|shared|local` on `list`, `--no-personal` on sync, `--target personal` on sync = usage error; overlay `target:"shared"` or `agentSettings` → `overlay_invalid`; schema `hooks-file-local` validation; goldens for `hooks-list` (`layer`, `visibility`, `overlay` object) and `hooks-plan` (`layer`, `purpose`); doctor checks incl. orphaned entries; MCP: `synca_hooks_add` personal needs `confirm:true`, `includePersonal:false`, resource only served when verified; overlay in a non-git directory is impossible. |
| **Kiro modes (Q6)** | fake `kiro-cli` scripts printing 2.33.0 / 3.x / garbage / timeout; filesystem-only detection; the resolution table row by row; both layouts present → one layout written, other `stale_other_layout`, `--prune` removes only hash-matching entries; converted-entry case after a simulated `/upgrade-agent`. |
| **OpenCode plugin (Q7)** | golden of the rendered plugin; header hashes verify; hand edit → `modified_externally`; foreign file at path → `not_owned`; grep test for forbidden constructs (`eval`, `Function(`, `import(`, `fetch`, `node:net`, `node:http`); run under `node --test` with fake children (block via exit 2, timeout kill, fail-open, onFailure block) — skipped when `node` is absent. |
| **JSON contract** | goldens + schemas for `hooks-plan|list|events|verify|file`; extend `TestSchemaFileListsEveryCommand`, `TestLiveListsMatchSchemas`; `doctor` has `hooks` without changing old fields; `sync-plan.json` optional `hooks` property validates. |
| **CLI** | exit codes (6/7/8), `--yes` off-TTY, `--dry-run` creates no file (including state and ignore entries), `--agents` filter, `--target`. |
| **Secrets** | a hook with `--token=…` is linted; diff/plan/list are masked. |
| **MCP (M4)** | protocol via a fake client over stdio and in-memory: tool list, annotations vs `commandSpecs()` kinds, `stale_plan`, `confirm` required, **assert no trust tool exists**, nothing outside hub/overlay/owned entries is written, stdout purity, legacy `initialize` negotiation and `2026-07-28` `server/discover`, `--read-only` hides M/D tools, project-cwd guard rejects a foreign repo. |
| **SDK pin and isolation (Q20)** | the suite above is black-box over a real stdio subprocess with recorded wire fixtures, so it validates any backend; a test fails if `go.mod` pins anything other than `go-sdk v1.8.0` without an updated fixture set; a test fails if a package other than `internal/mcpserver` imports `go-sdk`, or if any file in it other than `sdk.go` does; `THIRD_PARTY_NOTICES.md` is checked against the module list; binary-size budget check (fails if the stripped binary grows beyond +3 MB vs the 0.3.0 release). |
| **Skill** | the two SKILL.md copies are identical; the skill mentions every command. |
| **Manual M0 verification (not automated)** | for installed agents: load Synca-generated files in an isolated HOME and use the agent's own listing command (`grok inspect --json`, `/hooks` in Devin/Grok/Claude) to confirm the hook loads, without calling a model. Steps that need a real session are run and confirmed by the user. |

---

## 7. Plan

Rough estimates in working days (±40%), one person, sequential; running the M2 tracks in parallel could cut the calendar time to roughly 22–28 days. The totals grew from the first draft's 22–30 days because of decisions Q1 (real MCP server), Q3 (`sync all` integration + TTY review flow), Q5 (local/shared targets + ignore handling), Q6 (two Kiro adapters + mode detection), Q7 (plugin design and test harness) and, in this revision, Q18 (personal overlay: loader, layered lock/trust, collision rules, CLI/MCP/doctor/schema, tests; **+2.5 days**) and Q20 (SDK adapter/isolation, license notices; **+1 day**). Q17 and Q19 do not change the estimate (Q19 removes a flag, saving a little).

### M0 — Format-verification spike (3–4 days; was 2–3)

Goal: clear the ❓ cells before writing adapters.
Work: (1) does each agent tolerate an unknown key in an entry (`_synca`/`name`); (2) do Devin/Cursor/Grok dedupe when the same handler arrives from `~/.claude/settings.json` and a native file; (3) does Cursor accept matcher-groups in `hooks.json` (and how loud is Grok's error on flat entries); (4) **Kiro (Q6):** what `kiro-cli 2.33.0` loads from `~/.kiro/hooks/` (decides the "2.x + V3 files" row of §3.15), what `--version` prints for a V3 build, `/upgrade-agent` behavior on Synca-style entries, V3 `PreToolUse` exit code, whether `SessionEnd` exists, 2.x project profile layout; (5) **OpenCode (Q7):** real tool names (`bash` vs `shell`), whether `chat.message` exists/can block, `session.deleted`, plugin-directory auto-load of two files; (6) Devin: full tool names, default timeout, per-hook disable; (7) pick the format-preserving JSON editor (hujson vs hand-written) via round-trip tests on a sanitized copy of `~/.config/devin/config.json`; (8) Claude Code and Cursor are not installed: need the user to confirm versions, or install a trial build into an isolated HOME; (9) **targets (Q5):** confirm Grok/Kiro V3/OpenCode load a second `*.local.*` file in the same directory, and that `git check-ignore`/`info/exclude` behave as designed for worktrees and submodules; check Claude's `.claude/settings.local.json` and Devin's `.devin/config.local.json` ignore behavior; (10) **MCP (Q1, Q20):** confirm `go-sdk` v1.8.0 serves both `initialize` and `server/discover` over stdio against a real agent client (Grok on this Mac), record the wire fixtures, confirm SPDX licenses with a scanner (the size/dependency measurement is already done, §4.2.1); (11) **overlay and refusal (Q18, Q19):** confirm that a `!` negation in a tracked `.gitignore` really outranks `info/exclude`, and that `git check-ignore`/`ls-files` give the expected results for the overlay paths in worktrees and submodules.
**Output:** `docs/plans/hooks-sync-m0-findings.md` + sanitized fixtures + updated decision table for §2–§3.
**Acceptance:** every ❓ in §2.1–§2.4 becomes ✅/❌ or an "accepted risk" decision; JSON editor library chosen; SDK pin confirmed against a real client.

### M1 — Model + schema + state (5–6 days; was 3–4)

`hooks_model.go` (parse/validate/normalize/hash, `target`, `agentSettings`, `layer`), **`hooks_overlay.go` (overlay loader: two-layer merge, collision rule, `hooks-file-local` validation, health state, creation gate and per-run re-verification)**, `hooks_events.go` (capability table), `hooks_lock.go` and `hooks_trust.go` (both keyed by layer), `hooks_edit.go` (editor), `hooks_target.go` (local/shared resolution, git ignore verification, managed `info/exclude` block, refusal diagnostics — no override path), secret/path lint, `schema/v1/hooks-file.json` and `hooks-file-local.json`, backup/atomic/CAS/flock helpers.
**Acceptance:** sample hub parses + validates; hashes stable across runs; trust store 0600; safe-write helpers tested for CAS/atomic/symlink; ignore helper tested on plain repo, worktree, submodule, `!`-negation and read-only-exclude refusals; overlay merge/collision/trust-key rules tested; existing skills/MCP tests stay green.

### M2 — Adapters in priority order (12–16 days nominal 15; was 10–13)

| Order | Adapter | Why | Days |
| --- | --- | --- | --- |
| 1 | Claude | the "lingua franca" format, indirectly covers Grok/Devin/Cursor; local + shared files | 2.5 |
| 2 | Grok | own-file (two files for local/shared), best docs, installed here for verification | 1.5 |
| 3 | Cursor | different (flat) schema, compat handling, single-file target rules | 1.5 |
| 4 | Devin | shared-file with real foreign groups on this Mac; `config.local.json` / `hooks.v1.json` | 2 |
| 5 | Kiro | two adapters (`kiro2` profiles, `kiro3` files) + `KiroModeResolver` + dual-layout/upgrade handling | 4 |
| 6 | OpenCode | generated plugin: template, header/hashes, node test harness, event mapping — highest risk | 3.5 |
| 7 (optional) | shim `synca hooks run` | `contract: shim`, fail-closed for critical | +2 |

**Per-adapter acceptance:** ≥ 8 golden cases green; foreign bytes preserved; round-trip; idempotent; lossy complete for all 13 events; M0-style manual verification via the agent's listing command (if installed).

### M3 — CLI + doctor + sync (5–6 days; was 3–4)

`hooks_plan.go` (`PlanSyncHooks`, `ApplyHooksPlan`), `internal/cli/hooks_cmd.go`, `commandSpecs()` (R/M/D correct), **`sync all` integration** (hooks phase after skills/MCP, `--no-hooks`, additive `hooks` key and optional property in `sync-plan.json`, TTY trust-review prompt, `--strict`/`--no-prompt`), schemas/goldens, `doctor` hooks section (incl. overlay checks), `--target personal|all` on `add`/`list`, `--no-personal`, layer/overlay fields in the JSON outputs, `provider clear` removes managed hooks (if Q11 agrees), `printHelp` update; confirm with the real `synca-gpui` build that `sync all --json` with the extra key still parses.
**Acceptance:** every §3.9 command works; exit codes follow §3.5.1 (0 default / 7 strict for untrusted; 6/8 as specified); `sync hooks --dry-run --json` validates against the schema; apply twice = 0 changes; `sync all` without a hub `hooks.json` is byte-identical to 0.2.x output; no flag combination writes an untrusted hook.

### M4 — MCP server + skill (7–9 days; was 3–4)

`internal/mcpserver` (server, `sdk.go` adapter, registry, guard, errors, resources), `go.mod` pin of `go-sdk v1.8.0` + import-isolation test + size budget test, personal-overlay parameters/resource, shared JSON-document package extracted from `internal/cli`, `synca serve --mcp` with `--read-only`/`--tools`/`--allow-cwd`, 8 tools of §4.6, protocol tests (stdio + in-memory, legacy and modern negotiation), client-config snippets in README, self-registration test via `synca mcp add`; update both SKILL.md copies + tests; `ARCHITECTURE.md` scope edit; `synca schema` lists `serve`.
**Acceptance:** fake-client suite passes; no trust tool exists; `skill check` clean; `apply` rejects `stale_plan` and skips `untrusted`; stdout purity test passes; works end-to-end when registered into a real agent (Grok on this Mac) in an isolated HOME.

### M5 — Docs & release (2–3 days; was 1–2)

README (Hooks section: layout, trust, local/shared/personal, lossy, MCP snippets, examples), CHANGELOG (note the `sync all` change), REQUIREMENTS (add hooks to Goal/Agents/Canonical paths), ARCHITECTURE, `THIRD_PARTY_NOTICES.md` (SDK Apache-2.0/MIT + transitive modules), bump `VERSION`/`core.Version` → 0.3.0 (0.4.0 for MCP when split), `task release:build`; a tracking issue for `ns-gpui` (Hooks tab).
**Acceptance:** CI green; release script does not refuse; docs and `synca schema` agree.

**Total:** M0 3–4 + M1 5–6 + M2 12–16 (6 adapters, shim excluded) + M3 5–6 + M4 7–9 + M5 2–3 = **34–44 days**; with the optional shim (+2) → 36–46.

### Risks

| Risk | Level | Mitigation |
| --- | --- | --- |
| Agent docs change fast (Kiro already moved 2.x → V3; Claude keeps adding events) | high | capability table as data, per-version golden tests, `hooks events` shows the verified version, `doctor` flags unknown agent versions |
| Format-preserving edit corrupts `settings.json`/`config.json` | high | own-file preferred, parse errors never write, backup + atomic + CAS, byte-preservation property tests |
| Double fire from cross-compat | medium | compat graph + `doctor` + M0 dedupe verification |
| I/O contract differences → hook "runs" but does not block | medium | `contract`, `blocking_weakened`, critical fail-closed, recommend exit 2 |
| Hooks as an attack vector (malicious repo, prompt injection) | high | S1–S13, human-only trust, project hooks untrusted + local by default, no trust tool, no elicitation |
| **`sync all` now touches code-executing config (Q3, Q17)** | medium | writes only already-trusted hooks; skip is exit 0 by default (decided, Q17) so unattended runs do not fail but are visible (stderr/JSON; `--strict` exits 7 for CI); `--no-hooks`; no hub `hooks.json` ⇒ identical behavior |
| **Local file or personal overlay ends up committed (Q5, Q18, Q19)** | medium | `git ls-files`/`check-ignore` verification before creating anything, `info/exclude` managed block, **refuse when not provable (no override)**, overlay re-verified every run and disabled when tracked, `doctor` re-checks, Cursor single-file rule; residual: `git add -f` is detected afterwards, not prevented |
| **Personal overlay surprises (Q18)** | low–medium | no override of hub hooks (collision invalidates the overlay entry), trust keyed by layer and always human-gated, overlay absent on other clones by design, project-scope only |
| Local/shared interpretation differs from what the user meant | low | isolated constant + inputs; confirm in §8 |
| **Kiro mode misdetected / upgrade double-fire (Q6)** | medium | capability-based resolver with override, never write both layouts, `stale_*` reporting, documented `--prune` order before `/upgrade-agent`, M0 probe of what 2.33.0 actually loads |
| **OpenCode plugin executes inside the agent process (Q7)** | medium | fixed embedded template, hash-pinned data, no network/eval/dynamic import, no runtime file reads, doctor hash check, kill switch via `--agents`/`agents` |
| Claude/Cursor not verified on this machine | medium | M0 + user confirms versions |
| **MCP server widens scope and adds a dependency (Q1, Q20)** | medium | stdio only, no daemon, thin over `core`, official SDK **pinned at v1.8.0** and isolated in `sdk.go`, measured +1.74 MB (+24%) and +9 modules, hand-written fallback ≈ 4–6 days, tool kinds checked against `commandSpecs()`, `--read-only`, project-cwd guard, separate 0.4.0 release |
| SDK licensing/supply chain (Q20) | low | MIT→Apache-2.0 + MIT/BSD transitive modules, notices file in M5, `go.sum` verified, `govulncheck` suggested in CI |
| MCP spec churn (`2026-07-28` is stateless and deprecates roots/sampling/logging) | low–medium | SDK negotiates versions, legacy handshake kept, no use of deprecated features |
| An agent blocks Synca-written hooks itself (Grok folder-trust, Codex hash review) | low | Synca does not bypass; `doctor` explains |

---

## 8. Questions and decisions

### 8.1 Decided (2026-10-23)

| # | Question | Decision |
| --- | --- | --- |
| Q1 | Should Synca add an MCP server (`synca serve --mcp`, stdio)? | **YES** — designed in §4 |
| Q3 | Does `sync all` include hooks? | **YES**, with trust gating unchanged — §3.5.1 |
| Q5 | Project scope default `local` or `shared`? | **Both supported; default `local`** (a bare "yes" to an either/or, interpretation flagged in §3.14 and the decisions log) |
| Q6 | Kiro 2.x profile hooks vs V3 files? | **Both**, detected, one layout written — §3.15 |
| Q7 | OpenCode generated plugin? | **YES** — §3.16 |
| Q14 | Plan language? | **English** — this document |
| Q2 | Canonical event names = Claude-style PascalCase? | **Yes** (implemented in 0.3.0) |
| Q4 | May foreign hooks be auto-imported into the hub? | **No.** Only an explicit `hooks import` (not built yet) may do it. `sync all` includes hooks, so auto-import would silently widen what it manages |
| Q8 | Build the shim `synca hooks run`? | **Not now.** Opt-in after the remaining adapters; `contract` is not in schema v1 yet |
| Q9 | v1 `command` handlers only? | **Yes.** `handler.type` other than `command` is rejected |
| Q10 | Trust key? | **`(scope, hook id, hash)`, no TTL** (implemented). The hash covers the definition and every file under `~/.agents/hooks/<id>/`. A trusted hook is auto-written to a newly installed agent; if that proves too loose, add the agent to the key |
| Q11 | Should `provider clear <agent>` also remove synca-managed hooks? | **Yes, decided; not implemented in 0.3.0.** Until then `hooks remove` + `hooks sync`, or `--agents`/`excludeAgents` + `--on-conflict keep-source`, remove them |
| Q12 | Codex/Gemini/Copilot/Windsurf targets? | **Out of scope.** The adapter table is data, so they can be added later |
| Q13 | Flip agents' compat flags to avoid double fire? | **No.** Report in `doctor` only (the doctor hooks section is not built yet) |
| Q15 | Versions? | **0.3.0 = hooks CLI** (including the `sync all` change), **0.4.0 = MCP server** |
| Q16 | May M0 run manual checks on installed agents and install trial builds into an isolated HOME? | **Yes** (listing commands only, no model calls, isolated HOME). Not run in 0.3.0: Claude Code and Cursor are still not installed here and their versions are unconfirmed |
| Q17 | `sync all`/`sync hooks` skipped-untrusted exit code? | **Exit 0 by default; exit 7 only with `--strict`** — §3.5.1 |
| Q18 | Personal uncommitted overlay? | **YES: `<git-root>/.agents/hooks.local.json`** — §3.17 |
| Q19 | Local file whose git-ignore cannot be proven? | **REFUSE; no escape hatch in v1** (`--allow-unignored` removed) — §3.14 |
| Q20 | MCP implementation? | **Official Go SDK, pinned `v1.8.0`, isolated, with fallback** — §4.2.1 |

### 8.2 Still open

Nothing from this list is blocking. Open work is the remaining milestones in "Implementation status".

---

## Implementation status (synca 0.3.0, 2026-10-24)

**Built** (`go/internal/core/hooks_*.go`, `go/internal/cli/hooks_cmd.go`, schemas `hooks-*.json`, tests in `hooks_test.go` and `hooks_cmd_test.go`):

- Hub `~/.agents/hooks.json` (schema v1, unknown fields rejected, secret lint, `{{hook_dir}}`), events (all 13) and tool classes, per-hook `agents`/`excludeAgents`/`enabled`.
- Trust store (`~/.local/state/synca/hooks/trust.json`, 0600, keyed by hook id and hash of definition + scripts); `hooks trust` needs a TTY and a typed `yes`, no flag; agents cannot approve.
- Adapters: **Claude** (shared file, `~/.claude/settings.json`), **Grok** (own file `~/.grok/hooks/synca.json`), **Devin** (shared file `~/.config/devin/config.json`, event `PostCompact` -> `PostCompaction`, tools `shell`->`exec`, `write`->`edit`). Capability tables are data in `hooks_adapter.go`.
- Shared-file editor (`hooks_edit.go`): splices only the top-level `hooks` member, keeps the other bytes, appends one matcher group per hook, never touches an unparseable file; lock file (`lock.json`) so only synca's groups are updated/removed; atomic write + compare-before-write + backup (last 20); symlinks resolved.
- Plan kinds: `write_hook`, `update_hook`, `remove_hook`, `adopt_hook`, `skip_hook_same`, `skip_hook_unsupported`, `conflict_hook` (`untrusted`, `modified_externally`, `parse_error`). Lossy codes: `tool_unmapped`, `failure_mode_weakened`. Unmapped tool classes skip the hook for that agent instead of widening the matcher.
- `synca hooks list|add|remove|trust|untrust|sync`, `synca sync hooks`, and `sync all` integration (only when the hub exists; additive `hooks` key; exit 0, `--strict` exit 7; `--no-hooks`).

**Decisions made while building** (documented here because they deviate from or narrow the text above):

1. User scope only. `--scope project`, the `local`/`shared` targets (§3.14) and the personal overlay (§3.17) are not built.
2. `onFailure: block` is never emitted for Claude/Devin/Grok: the field names (`onFailure`, `fail_closed`) are unverified (M0), so it is reported as `failure_mode_weakened` rather than risk an invalid settings file.
3. The editor normalizes whitespace inside `hooks` (key order and structure are kept); everything outside `hooks` is byte-identical. Foreign groups are preserved semantically, not byte-for-byte.
4. A group edited by hand is detected by its command (stored in the lock), not by a marker. Default is skip + `modified_externally`; `--on-conflict keep-source --yes` restores the hub version. `keep-target` is rejected for hooks.
5. A group missing from the file (deleted by hand) is rewritten, because the hub is the source of truth.
6. An agent is written only when its config directory exists (`~/.claude`, `~/.grok`, `~/.config/devin`); otherwise the plan reports `skip_hook_unsupported` with reason `agent_not_installed`.
7. When removing the last hook of a Grok file synca deletes its own file; for shared files it leaves an empty `"hooks": {}` member (it only removes event arrays it created).
8. No `flock` across concurrent synca runs; the compare-before-write check (`file_changed_during_apply`) covers the race.

**Not built (remaining plan):** M0 verification spike (Claude/Cursor not installed here), Cursor, Kiro 2.x/V3 and OpenCode adapters, project scope and local/shared targets with the ignore proof, the personal overlay, `hooks import`, the doctor hooks section, `provider clear` for hooks, the shim, the MCP server (0.4.0), the `synca-gpui` Hooks tab. Verification done in 0.3.0: unit and CLI tests (idempotence, foreign-hook preservation, untrusted never written, JSON schema validation) and a run against copies of the real Devin and Grok hook files in a throwaway HOME (Devin config semantically identical after write+remove, everything outside `hooks` byte-identical). No real agent config on this Mac was modified and no agent was run to see the hooks fire.

---

## 9. Not verified (stated explicitly)

- **Claude Code and Cursor:** not installed on this Mac; all details come from official docs, no real file loaded.
- **No agent was run** to see a hook actually load/execute (to avoid model calls/OAuth); `grok inspect` exists but was not run because it shares `leader.sock` with other running Grok sessions.
- Devin: no page listing tool names/matchers (URL `/hooks/matcher` 404s); default timeout, per-hook disable, ordering/parallelism, dedupe unknown. Whether `.devin/config.local.json` is auto-ignored is taken from docs, untested.
- Kiro: V3 docs conflict on the `PreToolUse` blocking exit code ("Hook actions" page: any non-zero blocks; triggers/migration pages: exit 2) and on `SessionEnd`; 2.x project profile layout unknown; this Mac runs 2.33.0 yet has `permissions.yaml`, so what 2.33.0 does with `~/.kiro/hooks/` and what a V3 `--version` prints are unverified; `/upgrade-agent` behavior on Synca entries is inferred from docs.
- OpenCode: real tool names (`bash` vs `shell`) and events for user prompt/stop/session end unconfirmed (docs list only); existence of `chat.message` unconfirmed; loading of two plugin files in one directory unconfirmed.
- Cursor: unknown whether `hooks.json` accepts Claude-style matcher-groups; dedupe against Claude settings unknown.
- Unknown keys in entries (`_synca` marker): only Grok is confirmed by docs ("ignores any other key").
- Grok/Kiro V3 loading a second `*.local.*` file next to `synca.json`: inferred from "reads every `*.json`" docs, untested.
- That Claude auto-git-ignores `settings.local.json` only when it creates the file: from docs, untested — Synca verifies with `git check-ignore` regardless.
- The format-preserving JSON editor is not chosen/tried.
- MCP facts (spec `2026-07-28`, stateless model, deprecation of roots/sampling/logging, SDK version/compatibility table) come from web search results of the spec/SDK pages on 2026-10-23. The SDK itself **was** built into a scratch copy of the repo (go1.27.0 darwin/arm64) to measure size and modules and to read its `LICENSE` files, but no `synca serve --mcp` exists yet, so the server behavior, the legacy `initialize` fallback and `server/discover` against real agent clients are untested; client config snippets for Claude/Cursor/Kiro/OpenCode/Grok follow each agent's MCP docs and Synca's own writers, untested for `synca serve --mcp`.
- Licensing: read from `LICENSE` files in the module cache (SDK: MIT→Apache-2.0 transition notice + Apache text; others MIT/BSD-style); SPDX identifiers not machine-verified yet (M0).
- Overlay and refusal behavior (`!` negation outranking `info/exclude`, `check-ignore` in worktrees/submodules) comes from git's documented ignore precedence, not from a test run here (M0 item 11).
- Whether the `synca-gpui` window ignores an extra top-level key in `sync all --json`: `engine.rs` parses generic JSON values, but the JSON path was not exhaustively read — M3 checks with a real window build.
- Effort numbers are rough and not based on a prototype.
- Matcher/tool tables for Copilot/Gemini/Windsurf/Codex are reference-level from doc search results, not read in full.

---

## Appendix: sources

- Claude Code hooks reference: https://code.claude.com/docs/en/hooks
- Grok hooks: `~/.grok/docs/user-guide/10-hooks.md`, `05-configuration.md` (Harness compatibility) — shipped with grok 1.0.67
- Devin CLI hooks: https://docs.devin.ai/cli/extensibility/hooks/overview , `/lifecycle-hooks`, https://docs.devin.ai/cli/extensibility
- Kiro: https://kiro.dev/docs/hooks/ , `/docs/hooks/types/`, `/docs/hooks/actions/`, `/docs/cli/v3/hooks-migration/`, `/docs/cli/2x-reference/`
- Cursor: https://cursor.com/docs/hooks , https://cursor.com/docs/reference/third-party-hooks
- OpenCode plugins: https://opencode.ai/docs/plugins/
- Reference: Codex https://developers.openai.com/codex/hooks ; Gemini https://geminicli.com/docs/hooks/reference/ ; Copilot https://docs.github.com/en/copilot/reference/hooks-reference ; Windsurf/Cascade https://docs.devin.ai/desktop/cascade/hooks
- MCP: specification 2026-07-28 https://modelcontextprotocol.io/specification/2026-07-28 ; versioning https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning ; Go SDK https://github.com/modelcontextprotocol/go-sdk (v1.8.0; LICENSE: MIT→Apache-2.0 transition), https://go.sdk.modelcontextprotocol.io/quick_start/
- Synca repo 0.2.11: `ARCHITECTURE.md`, `README.md`, `REQUIREMENTS.md`, `go/internal/{core,cli,schema}`, `skills/synca/SKILL.md`; window client `synca/apps/synca-gpui/src/engine.rs`
