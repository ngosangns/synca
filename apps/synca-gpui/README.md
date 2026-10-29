# synca-gpui

Synca lists skills and MCP servers for the user scope or a project scope. The detail surface groups identity, sync state, scope, agent availability, and description before the file explorer and markdown preview. It keeps the Swift app's project list and activity log. One Install… button opens one dialog with a Skill tab, an MCP server tab, a Registry tab and an MCP Registry tab (⌘1, ⌘2, ⌘3 and ⌘4 switch; each tab keeps its fields; Escape closes). Install, add MCP, sync, unlink, purge, remove, provider delete, and update go to the synca CLI. A dry-run is shown before apply, and conflict lines can be skipped or kept.

The window links the shared chrome crate `ns-ui` from the `ns-gpui` repo and calls the installed `synca` CLI. Build and install the Dev app with `../ns-gpui/scripts/install-dev.sh synca`; run it from cargo with `cargo run --release` in this directory. Dev control (`ns-dev synca …`) is described in `ns-gpui/docs/dev-channel.md`.

![Synca: skill list, skill detail with sync state and file preview](../../docs/screenshots/synca-gpui.png)

Synca: User and Workspace scopes, a search box over skills and MCP servers, Install… and sync actions pinned at the bottom of the sidebar, and the selected entry's scope, sync state, agents, files and markdown preview. More in the [synca README](https://github.com/ngosangns/synca#readme).

## Origin badge

Each skill row and the skill detail pane show `remote` or `local`. `remote` means the skills lock file records the skill as installed from skills.sh or another remote source; the tooltip names the source and its type. `local` means it came from a local folder or was never recorded. A binary that does not report origin shows every skill as `local`.

MCP rows use the same badge, decided by the server's transport: `remote` for http and SSE (the server is reached over the network), `local` for stdio (a command runs on this machine). The tooltip adds provenance when the MCP lock file records it — `Installed from <registry>@<version>` — and the detail pane's Server section shows the transport (HTTP, SSE or stdio), the endpoint or command, enabled state, env and header key names, and the recorded registry source. Env and header values are masked by the CLI; only names are shown.

## MCP servers

The MCP tab installs a server into every agent: a name, a transport (stdio command, http URL, SSE URL), optional Environment and Headers fields (`KEY=VALUE` per line; values never appear in plans, logs or the detail pane), and JSON paste that accepts `mcpServers` blocks — a command array with args, `url`/`serverUrl`/`httpUrl`, `env`/`environment`, `headers`, and a `remote` type whose transport the URL decides (`/sse` is SSE). A remote MCP entry's actions are Sync this, Update (re-reads the registry, needs a recorded source), Unlink… (removes it from chosen agents but keeps the hub entry) and Purge… (removes the hub entry too). `synca mcp update` backs the Update action with a dry-run plan first.

The Tokens column shows a live measurement when `synca mcp probe` has one cached — the tooltip says "Measured from the server" with how long ago — and otherwise the config estimate; a failed probe keeps the estimate and the tooltip names the error. The row menu's "Probe tokens" action probes the selection or every MCP in scope and refreshes the list when it finishes.

## Registry tab

The Registry tab searches skills.sh through `synca skills search`, and lists Trending, Hot, All time (200 per page, Load more) and Official (needs `SKILLS_API_TOKEN`; without one the panel shows the CLI's message and hint). Typing `owner/repo` and pressing Enter lists that repository's skills (`skills discover`). Picking a result shows its SKILL.md and files from `skills info --contents`, and its audits from `skills audit` (pass, warn, fail). Actions: Install… previews the dry-run on the plan route and applies it from there; Copy prompt runs `skills use` and copies the prompt; Open on skills.sh opens the page. Sites listed as well-known cannot be installed yet, so Install… is disabled with the reason in its tooltip.

## MCP Registry tab

The MCP Registry tab (⌘4) searches and browses the Official MCP Registry through `synca mcp search` and `synca mcp browse` (50 per page, Load more by cursor), and shows a server's packages (npm, PyPI, OCI with their env keys) and remotes (type, URL, header names) from `synca mcp info`. Install… prefills the MCP tab from the first remote or package — the transport, URL or a runner command (`npx -y`, `uvx`, `docker run -i --rm`), and `KEY=` lines for the known env keys and header names (values left empty for the user, secrets especially) — then previews the dry-run plan. Open website only follows https links.

## Providers

Settings shows one Providers list: every agent the skills backend can install to, read from `synca providers list --json` when Settings opens and after a scope or workspace change. Every row has an on/off toggle. The six agents Synca syncs to (marked `managed`) are on by default and save their choice in `synca.disabledProviders` — toggling one off removes it from sync; they also have a Delete data button. The other rows are off by default and save their choice in `synca.enabledProviders` (the CLI cannot sync or clear them yet — the tooltip says so). Each row shows `installed` or `not installed`, a `universal` chip for agents that read the shared `.agents/skills` folder, the folder path (selectable) and the skill count, and a filter box matches name or id across the whole list. The six built-in rows render even before the list loads or when the CLI fails, so their toggles keep working; a CLI without the command shows "Update the synca CLI to list all providers".

## Updates and restore

Check updates (sidebar) runs `skills update --dry-run` and opens the plan with its notes (`update:`, `up to date:`, `skipped:`); Apply runs `skills update`. A remote skill's detail pane has Update for that skill alone. Restore from lock does the same with `skills restore`, reinstalling every remote skill the lock file records.

## Dev commands

`ns-dev synca registry view=trending|hot|all-time|official|search query=… select=N repo=owner/repo more=true` opens the Registry tab; `state` reports it under `registry` (view, status, count, selected, detail) and the open plan under `plan`; `ns-dev synca mcpreg view=search|browse query=… select=N more=true` opens the MCP Registry tab, reported under `mcpRegistry` (view, status, count, selected, detail); `ns-dev synca mcp name=… command=… url=… env=…;… headers=…;…` adds an MCP row to the list; `ns-dev synca mcp-update name=<key|all>` opens the `mcp update` dry-run plan; `ns-dev synca providers query=… reload=true` opens Settings on that list and sets the filter or reloads it; `state` reports it under `providers` (status, count, managed, present, shown, query). `ns-dev synca updates name=check|restore` opens the dry-run plan. Neither applies anything. `ns-dev synca sidebar width=420` sets the sidebar width the drag edge controls and saves it; `state` reports it as `sidebarWidth`. `state`'s `selection` carries `origin`, `transport`, `source` and `source_version` for MCP entries too.

## Sidebar tables

The skills and MCP tables sort independently. Click a column head — Name, Origin, Sync, Tokens — to sort ascending, again for descending, and a third time to restore load order. Origin orders local before remote; for a skill `remote` means installed from skills.sh or another remote source, for an MCP server it means reached over HTTP(S). MCP servers without a measured token count are probed once after each load (`synca mcp probe`, results cached per config fingerprint), so every enabled server shows a real number; the token tooltip says whether a number was measured or estimated.

## Sidebar width

Drag the edge between the sidebar and the editor to resize it (240–560pt; the editor keeps at least 280pt, so the edge gives way sooner in a narrow window or with a side column open). The width is saved to `com.ngosangns.synca` as `synca.sidebarWidth` and restored on launch.
