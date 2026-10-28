# synca-gpui

Synca lists skills and MCP servers for the user scope or a project scope. The detail surface groups identity, sync state, scope, agent availability, and description before the file explorer and markdown preview. It keeps the Swift app's project list and activity log. One Install… button opens one dialog with a Skill tab, an MCP server tab, and a Registry tab (⌘1, ⌘2 and ⌘3 switch; each tab keeps its fields; Escape closes). Install, add MCP, sync, unlink, purge, remove, provider delete, and update go to the synca CLI. A dry-run is shown before apply, and conflict lines can be skipped or kept.

The window links the shared chrome crate `ns-ui` from the `ns-gpui` repo and calls the installed `synca` CLI. Build and install the Dev app with `../ns-gpui/scripts/install-dev.sh synca`; run it from cargo with `cargo run --release` in this directory. Dev control (`ns-dev synca …`) is described in `ns-gpui/docs/dev-channel.md`.

![Synca: skill list, skill detail with sync state and file preview](../../docs/screenshots/synca-gpui.png)

Synca: User and Workspace scopes, a search box over skills and MCP servers, Install… and sync actions pinned at the bottom of the sidebar, and the selected entry's scope, sync state, agents, files and markdown preview. More in the [synca README](https://github.com/ngosangns/synca#readme).

## Origin badge

Each skill row and the skill detail pane show `remote` or `local`. `remote` means the skills lock file records the skill as installed from skills.sh or another remote source; the tooltip names the source and its type. `local` means it came from a local folder or was never recorded. A binary that does not report origin shows every skill as `local`.

## Registry tab

The Registry tab searches skills.sh through `synca skills search`, and lists Trending, Hot, All time (200 per page, Load more) and Official (needs `SKILLS_API_TOKEN`; without one the panel shows the CLI's message and hint). Typing `owner/repo` and pressing Enter lists that repository's skills (`skills discover`). Picking a result shows its SKILL.md and files from `skills info --contents`, and its audits from `skills audit` (pass, warn, fail). Actions: Install… previews the dry-run on the plan route and applies it from there; Copy prompt runs `skills use` and copies the prompt; Open on skills.sh opens the page. Sites listed as well-known cannot be installed yet, so Install… is disabled with the reason in its tooltip.

## All skills.sh providers

Settings lists the 79 agents the `skills` CLI can install to, read from `synca providers list --json` when Settings opens and after a scope or workspace change. The six agents Synca syncs to keep their toggle and delete rows, each with a status chip, folder and skill count. The rest are read-only rows with `installed` or `not installed`, a `universal` chip for agents that read the shared `.agents/skills` folder, the folder path (selectable) and the skill count. A filter box matches name or id. A CLI without the command shows "Update the synca CLI to list all providers", and the six toggles keep working.

## Updates and restore

Check updates (sidebar) runs `skills update --dry-run` and opens the plan with its notes (`update:`, `up to date:`, `skipped:`); Apply runs `skills update`. A remote skill's detail pane has Update for that skill alone. Restore from lock does the same with `skills restore`, reinstalling every remote skill the lock file records.

## Dev commands

`ns-dev synca registry view=trending|hot|all-time|official|search query=… select=N repo=owner/repo more=true` opens the Registry tab; `state` reports it under `registry` (view, status, count, selected, detail) and the open plan under `plan`; `ns-dev synca providers query=… reload=true` opens Settings on that list and sets the filter; `state` reports it under `providers` (status, count, managed, present, shown, query). `ns-dev synca updates name=check|restore` opens the dry-run plan. Neither applies anything.
