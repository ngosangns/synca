//! Per-agent path helpers for skills roots and MCP config files.

use crate::models::{AgentKind, Scope};
use crate::paths::{home_dir, project_root};
use std::path::PathBuf;

pub fn skill_roots(scope: Scope, cwd: &std::path::Path) -> Vec<(AgentKind, PathBuf)> {
    match scope {
        Scope::User => {
            let home = home_dir();
            vec![
                (AgentKind::Agents, home.join(".agents/skills")),
                (AgentKind::Grok, home.join(".grok/skills")),
                (AgentKind::Devin, home.join(".config/devin/skills")),
                (AgentKind::Cognition, home.join(".config/cognition/skills")),
                (AgentKind::Pi, home.join(".pi/agent/skills")),
                (AgentKind::Omp, home.join(".omp/agent/skills")),
                (AgentKind::Kiro, home.join(".kiro/skills")),
                (AgentKind::OpenCode, home.join(".config/opencode/skills")),
                (AgentKind::Claude, home.join(".claude/skills")),
                (AgentKind::Cursor, home.join(".cursor/skills")),
            ]
        }
        Scope::Project => {
            let Some(root) = project_root(cwd) else {
                return vec![];
            };
            vec![
                (AgentKind::Agents, root.join(".agents/skills")),
                (AgentKind::Grok, root.join(".grok/skills")),
                (AgentKind::Devin, root.join(".devin/skills")),
                (AgentKind::Cognition, root.join(".cognition/skills")),
                (AgentKind::Omp, root.join(".omp/skills")),
                (AgentKind::Pi, root.join(".pi/skills")),
                (AgentKind::Kiro, root.join(".kiro/skills")),
                (AgentKind::OpenCode, root.join(".opencode/skills")),
                (AgentKind::Claude, root.join(".claude/skills")),
                (AgentKind::Cursor, root.join(".cursor/skills")),
            ]
        }
    }
}

pub fn mcp_config_paths(scope: Scope, cwd: &std::path::Path) -> Vec<(AgentKind, PathBuf)> {
    match scope {
        Scope::User => {
            let home = home_dir();
            vec![
                (AgentKind::Agents, home.join(".agents/mcp.json")),
                (AgentKind::Grok, home.join(".grok/config.toml")),
                (AgentKind::Devin, home.join(".config/devin/mcp_config.json")),
                (AgentKind::Omp, home.join(".omp/agent/mcp.json")),
                (AgentKind::Pi, home.join(".pi/agent/mcp.json")),
                (AgentKind::Kiro, home.join(".kiro/settings/mcp.json")),
                (AgentKind::OpenCode, home.join(".config/opencode/opencode.json")),
                (AgentKind::Claude, home.join(".claude.json")),
                (AgentKind::Cursor, home.join(".cursor/mcp.json")),
            ]
        }
        Scope::Project => {
            let Some(root) = project_root(cwd) else {
                return vec![];
            };
            vec![
                (AgentKind::Agents, root.join(".mcp.json")),
                (AgentKind::Grok, root.join(".grok/config.toml")),
                (AgentKind::Omp, root.join(".omp/mcp.json")),
                (AgentKind::Pi, root.join(".pi/mcp.json")),
                (AgentKind::Kiro, root.join(".kiro/settings/mcp.json")),
                (AgentKind::OpenCode, root.join("opencode.json")),
                (AgentKind::Cursor, root.join(".cursor/mcp.json")),
            ]
        }
    }
}

pub fn canonical_skills_dir(scope: Scope, cwd: &std::path::Path) -> Option<PathBuf> {
    match scope {
        Scope::User => Some(home_dir().join(".agents/skills")),
        Scope::Project => project_root(cwd).map(|r| r.join(".agents/skills")),
    }
}

pub fn canonical_mcp_path(scope: Scope, cwd: &std::path::Path) -> Option<PathBuf> {
    match scope {
        Scope::User => Some(home_dir().join(".agents/mcp.json")),
        Scope::Project => project_root(cwd).map(|r| r.join(".mcp.json")),
    }
}
