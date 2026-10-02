Implement the full MVP of this repo per REQUIREMENTS.md. Do everything in this cwd.

Priority order:
1. Cargo workspace: crates ast-core, ast-cli, ast-tui + bin agent-skills-tui. Version 0.1.0.
2. ast-core: models; discover git root; scan skills (frontmatter name + folder) and MCP configs for all agents in REQUIREMENTS; normalize/dedupe; presence per agent; content hash; dry-run sync plan for skills (canonical ~/.agents/skills or <repo>/.agents/skills + symlinks) and MCP (hub ~/.agents/mcp.json / .mcp.json + per-agent writers: JSON mcpServers, Grok TOML, OpenCode mcp).
3. ast-cli: clap subcommands — tui (default), skills list, mcp list, sync skills|mcp with --scope --agents --dry-run, update --check|--json|--force (GitHub Releases ngosangns/agent-skills-tui; graceful if none).
4. ast-tui: Ratatui 0.30 + crossterm 0.29; pages User|Project (Tab); Skills+MCPs lists; detail pane (agents, paths, mismatch); keys j/k, s sync focused dry-run first then confirm, S sync missing with two-key confirm, u update check, q quit, footer help. Pattern like hearth-tui desk/shell/state/actions (read-only reference at ~/Github/ngosangns/hearth/rust/crates/hearth-tui — do not modify hearth).
5. README.md: install, usage, scope. .gitignore for Rust.
6. cargo build && cargo test as much as practical; fix until build succeeds.
7. git add, commit with a clear message, push to origin main (repo already exists; remote origin set).

Constraints:
- No silent overwrite on skill/MCP content conflicts.
- Prefer symlinks into .agents/skills for skill sync.
- Preserve target env secrets when writing MCP if source lacks keys.
- macOS arm64 first.
- Do not invent fake skill/MCP data in runtime code; scan real paths.

When done, print: build status, binary path, commit SHA, remaining gaps.
