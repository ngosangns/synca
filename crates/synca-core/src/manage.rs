//! Install / remove skills and MCP servers (scope-locked).

use crate::agents::{canonical_mcp_path, canonical_skills_dir, mcp_config_paths, skill_roots};
use crate::discover::{skill_display_name, skill_frontmatter_name};
use crate::models::{normalize_key, AgentKind, McpNormalized, Scope};
use crate::scan::{canonical_transport, scan_skills};
use crate::sync::{
    copy_dir_recursive, force_symlink, remove_path, upsert_mcp_json_hub, write_mcp_to_agent,
};
use serde_json::{json, Value as JsonValue};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone)]
pub struct ManagePlan {
    pub scope: String,
    pub dry_run: bool,
    pub actions: Vec<ManageAction>,
    pub notes: Vec<String>,
}

#[derive(Debug, Clone, serde::Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum ManageAction {
    CopySkill {
        from: PathBuf,
        to: PathBuf,
        skill_key: String,
    },
    SymlinkSkill {
        link: PathBuf,
        target: PathBuf,
        skill_key: String,
        agent: String,
    },
    UnlinkSkill {
        path: PathBuf,
        skill_key: String,
        agent: String,
    },
    PurgeCanonicalSkill {
        path: PathBuf,
        skill_key: String,
    },
    UpsertMcpHub {
        hub: PathBuf,
        server: String,
    },
    WriteMcpAgent {
        path: PathBuf,
        agent: String,
        server: String,
    },
    RemoveMcpHub {
        hub: PathBuf,
        server: String,
    },
    RemoveMcpAgent {
        path: PathBuf,
        agent: String,
        server: String,
    },
}

/// Resolve a skill source: local path or git URL. Returns (skill_dir, optional temp cleanup dir).
pub fn resolve_skill_source(src: &str) -> anyhow::Result<(PathBuf, Option<tempfile::TempDir>)> {
    let trimmed = src.trim();
    if trimmed.is_empty() {
        anyhow::bail!("empty skill source");
    }
    let looks_git = trimmed.starts_with("git@")
        || trimmed.starts_with("http://")
        || trimmed.starts_with("https://")
        || trimmed.ends_with(".git")
        || trimmed.contains("github.com/")
        || trimmed.contains("gitlab.com/");

    if looks_git && !Path::new(trimmed).is_dir() {
        let tmp = tempfile::tempdir()?;
        let dest = tmp.path().join("repo");
        let status = std::process::Command::new("git")
            .args(["clone", "--depth", "1", trimmed])
            .arg(&dest)
            .status()?;
        if !status.success() {
            anyhow::bail!("git clone failed for {trimmed}");
        }
        let skill_dir = find_skill_dir(&dest)?;
        return Ok((skill_dir, Some(tmp)));
    }

    let path = PathBuf::from(trimmed);
    let path = if path.is_absolute() {
        path
    } else {
        std::env::current_dir()?.join(path)
    };
    if !path.exists() {
        anyhow::bail!("skill source not found: {}", path.display());
    }
    let skill_dir = find_skill_dir(&path)?;
    Ok((skill_dir, None))
}

fn find_skill_dir(path: &Path) -> anyhow::Result<PathBuf> {
    let path = path.canonicalize().unwrap_or_else(|_| path.to_path_buf());
    if path.join("SKILL.md").is_file() {
        return Ok(path);
    }
    // Single child with SKILL.md
    if path.is_dir() {
        let mut candidates = Vec::new();
        for entry in std::fs::read_dir(&path)? {
            let entry = entry?;
            let p = entry.path();
            if p.join("SKILL.md").is_file() {
                candidates.push(p);
            }
        }
        if candidates.len() == 1 {
            return Ok(candidates.remove(0));
        }
        if candidates.is_empty() {
            // Nested .agents/skills/<name>
            let nested = path.join(".agents/skills");
            if nested.is_dir() {
                return find_skill_dir(&nested);
            }
            anyhow::bail!("no SKILL.md under {}", path.display());
        }
        anyhow::bail!(
            "multiple skills under {}; point at a specific skill folder",
            path.display()
        );
    }
    anyhow::bail!("not a skill directory: {}", path.display());
}

fn skill_key_from_dir(dir: &Path) -> String {
    let name = skill_frontmatter_name(&dir.join("SKILL.md"))
        .or_else(|| Some(skill_display_name(dir)))
        .unwrap_or_else(|| {
            dir.file_name()
                .map(|s| s.to_string_lossy().to_string())
                .unwrap_or_else(|| "skill".into())
        });
    normalize_key(&name)
}

pub fn plan_install_skill(
    scope: Scope,
    cwd: &Path,
    source_dir: &Path,
    agents_filter: Option<&[AgentKind]>,
) -> anyhow::Result<ManagePlan> {
    let Some(canonical) = canonical_skills_dir(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let key = skill_key_from_dir(source_dir);
    let canon_skill = canonical.join(&key);
    let mut plan = ManagePlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
        notes: vec![format!(
            "install skill '{}' from {}",
            key,
            source_dir.display()
        )],
    };
    plan.actions.push(ManageAction::CopySkill {
        from: source_dir.to_path_buf(),
        to: canon_skill.clone(),
        skill_key: key.clone(),
    });
    let targets: Vec<(AgentKind, PathBuf)> = skill_roots(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .filter(|(a, _)| agents_filter.map(|f| f.contains(a)).unwrap_or(true))
        .collect();
    for (agent, root) in targets {
        plan.actions.push(ManageAction::SymlinkSkill {
            link: root.join(&key),
            target: canon_skill.clone(),
            skill_key: key.clone(),
            agent: agent.as_str().into(),
        });
    }
    Ok(plan)
}

/// Default remove: unlink agent symlinks/copies only (keep canonical).
pub fn plan_remove_skill(
    scope: Scope,
    cwd: &Path,
    key: &str,
    agents_filter: Option<&[AgentKind]>,
    purge: bool,
) -> anyhow::Result<ManagePlan> {
    let key = normalize_key(key);
    let Some(canonical) = canonical_skills_dir(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let mut plan = ManagePlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
        notes: vec![if purge {
            format!("PURGE skill '{key}' (canonical + all agent links)")
        } else {
            format!("unlink skill '{key}' from agents (canonical kept)")
        }],
    };
    // Skill keys come from SKILL.md frontmatter, so the folder name can differ
    // from the key (folder "gitbutler", name "but"). Act on the paths the scan
    // actually found for this key instead of guessing <root>/<key>.
    let _ = &canonical;
    for entry in scan_skills(scope, cwd).into_iter().filter(|e| e.key == key) {
        for p in entry.presence {
            if p.agent == AgentKind::Agents {
                if purge {
                    plan.actions.push(ManageAction::PurgeCanonicalSkill {
                        path: p.path,
                        skill_key: key.clone(),
                    });
                }
                continue;
            }
            if !agents_filter.map(|f| f.contains(&p.agent)).unwrap_or(true) {
                continue;
            }
            plan.actions.push(ManageAction::UnlinkSkill {
                path: p.path,
                skill_key: key.clone(),
                agent: p.agent.as_str().into(),
            });
        }
    }
    if plan.actions.is_empty() {
        plan.notes
            .push(format!("nothing to remove for skill '{key}'"));
    }
    Ok(plan)
}

pub fn plan_add_mcp(
    scope: Scope,
    cwd: &Path,
    name: &str,
    norm: &McpNormalized,
    agents_filter: Option<&[AgentKind]>,
) -> anyhow::Result<ManagePlan> {
    let key = normalize_key(name);
    let Some(hub) = canonical_mcp_path(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let mut plan = ManagePlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
        notes: vec![format!("add mcp '{key}' to hub + agents")],
    };
    // Stash normalized into notes for apply (apply reads from a parallel map)
    let _ = norm;
    plan.actions.push(ManageAction::UpsertMcpHub {
        hub: hub.clone(),
        server: key.clone(),
    });
    let writers: Vec<(AgentKind, PathBuf)> = mcp_config_paths(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .filter(|(a, _)| agents_filter.map(|f| f.contains(a)).unwrap_or(true))
        .collect();
    for (agent, path) in writers {
        plan.actions.push(ManageAction::WriteMcpAgent {
            path,
            agent: agent.as_str().into(),
            server: key.clone(),
        });
    }
    Ok(plan)
}

pub fn plan_remove_mcp(
    scope: Scope,
    cwd: &Path,
    name: &str,
    agents_filter: Option<&[AgentKind]>,
) -> anyhow::Result<ManagePlan> {
    let key = normalize_key(name);
    let Some(hub) = canonical_mcp_path(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let mut plan = ManagePlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
        notes: vec![format!("remove mcp '{key}' from hub + agents")],
    };
    plan.actions.push(ManageAction::RemoveMcpHub {
        hub,
        server: key.clone(),
    });
    let writers: Vec<(AgentKind, PathBuf)> = mcp_config_paths(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .filter(|(a, _)| agents_filter.map(|f| f.contains(a)).unwrap_or(true))
        .collect();
    for (agent, path) in writers {
        plan.actions.push(ManageAction::RemoveMcpAgent {
            path,
            agent: agent.as_str().into(),
            server: key.clone(),
        });
    }
    Ok(plan)
}

/// Apply manage plan. For MCP add, pass `mcp_payload`.
pub fn apply_manage_plan(
    plan: &ManagePlan,
    mcp_payload: Option<&McpNormalized>,
) -> anyhow::Result<Vec<String>> {
    let mut log = Vec::new();
    for action in &plan.actions {
        match action {
            ManageAction::CopySkill { from, to, skill_key } => {
                if let Some(parent) = to.parent() {
                    std::fs::create_dir_all(parent)?;
                }
                if to.exists() || std::fs::symlink_metadata(to).is_ok() {
                    remove_path(to)?;
                }
                copy_dir_recursive(from, to)?;
                log.push(format!(
                    "copied skill {skill_key}: {} -> {}",
                    from.display(),
                    to.display()
                ));
            }
            ManageAction::SymlinkSkill {
                link,
                target,
                skill_key: _,
                agent,
            } => {
                force_symlink(link, target, agent, &mut log)?;
            }
            ManageAction::UnlinkSkill {
                path,
                skill_key,
                agent,
            } => {
                if path.exists() || std::fs::symlink_metadata(path).is_ok() {
                    remove_path(path)?;
                    log.push(format!(
                        "unlinked skill {skill_key} from {agent}: {}",
                        path.display()
                    ));
                }
            }
            ManageAction::PurgeCanonicalSkill { path, skill_key } => {
                if path.exists() || std::fs::symlink_metadata(path).is_ok() {
                    remove_path(path)?;
                    log.push(format!(
                        "purged canonical skill {skill_key}: {}",
                        path.display()
                    ));
                }
            }
            ManageAction::UpsertMcpHub { hub, server } => {
                let Some(norm) = mcp_payload else {
                    anyhow::bail!("mcp payload required for UpsertMcpHub");
                };
                upsert_mcp_json_hub(hub, server, norm)?;
                log.push(format!("hub upsert {server} in {}", hub.display()));
            }
            ManageAction::WriteMcpAgent {
                path,
                agent,
                server,
            } => {
                let Some(norm) = mcp_payload else {
                    anyhow::bail!("mcp payload required for WriteMcpAgent");
                };
                let agent_kind = AgentKind::all()
                    .iter()
                    .copied()
                    .find(|a| a.as_str() == agent.as_str())
                    .unwrap_or(AgentKind::Cursor);
                write_mcp_to_agent(path, agent_kind, server, norm)?;
                log.push(format!(
                    "wrote mcp {server} -> {} ({agent})",
                    path.display()
                ));
            }
            ManageAction::RemoveMcpHub { hub, server } => {
                remove_mcp_from_json_hub(hub, server)?;
                log.push(format!("hub remove {server} from {}", hub.display()));
            }
            ManageAction::RemoveMcpAgent {
                path,
                agent,
                server,
            } => {
                remove_mcp_from_agent(path, agent, server)?;
                log.push(format!(
                    "removed mcp {server} from {} ({agent})",
                    path.display()
                ));
            }
        }
    }
    Ok(log)
}

fn remove_mcp_from_json_hub(hub: &Path, server: &str) -> anyhow::Result<()> {
    if !hub.is_file() {
        return Ok(());
    }
    let mut root: JsonValue = serde_json::from_str(&std::fs::read_to_string(hub)?)?;
    if let Some(obj) = root.get_mut("mcpServers").and_then(|v| v.as_object_mut()) {
        obj.remove(server);
    }
    std::fs::write(hub, serde_json::to_string_pretty(&root)?)?;
    Ok(())
}

fn remove_mcp_from_agent(path: &Path, agent: &str, server: &str) -> anyhow::Result<()> {
    if !path.is_file() {
        return Ok(());
    }
    match agent {
        "grok" => remove_mcp_from_grok_toml(path, server),
        "opencode" => {
            let mut root: JsonValue =
                serde_json::from_str(&std::fs::read_to_string(path).unwrap_or_else(|_| "{}".into()))
                    .unwrap_or(json!({}));
            if let Some(obj) = root.get_mut("mcp").and_then(|v| v.as_object_mut()) {
                obj.remove(server);
            }
            std::fs::write(path, serde_json::to_string_pretty(&root)?)?;
            Ok(())
        }
        _ => {
            let mut root: JsonValue =
                serde_json::from_str(&std::fs::read_to_string(path).unwrap_or_else(|_| "{}".into()))
                    .unwrap_or(json!({}));
            if let Some(obj) = root.get_mut("mcpServers").and_then(|v| v.as_object_mut()) {
                obj.remove(server);
            }
            std::fs::write(path, serde_json::to_string_pretty(&root)?)?;
            Ok(())
        }
    }
}

fn remove_mcp_from_grok_toml(path: &Path, server: &str) -> anyhow::Result<()> {
    let text = std::fs::read_to_string(path)?;
    if text.trim().is_empty() {
        return Ok(());
    }
    let mut value: toml::Value = toml::from_str(&text)?;
    if let Some(servers) = value
        .as_table_mut()
        .and_then(|t| t.get_mut("mcp_servers"))
        .and_then(|v| v.as_table_mut())
    {
        servers.remove(server);
    }
    std::fs::write(path, toml::to_string_pretty(&value)?)?;
    Ok(())
}

/// Convenience: install from path/URL string.
pub fn install_skill(
    scope: Scope,
    cwd: &Path,
    source: &str,
    agents_filter: Option<&[AgentKind]>,
    dry_run: bool,
) -> anyhow::Result<(ManagePlan, Vec<String>)> {
    let (dir, _tmp) = resolve_skill_source(source)?;
    let mut plan = plan_install_skill(scope, cwd, &dir, agents_filter)?;
    plan.dry_run = dry_run;
    if dry_run {
        return Ok((plan, vec![]));
    }
    let log = apply_manage_plan(&plan, None)?;
    Ok((plan, log))
}

pub fn remove_skill(
    scope: Scope,
    cwd: &Path,
    key: &str,
    agents_filter: Option<&[AgentKind]>,
    purge: bool,
    dry_run: bool,
) -> anyhow::Result<(ManagePlan, Vec<String>)> {
    let mut plan = plan_remove_skill(scope, cwd, key, agents_filter, purge)?;
    plan.dry_run = dry_run;
    if dry_run {
        return Ok((plan, vec![]));
    }
    let log = apply_manage_plan(&plan, None)?;
    Ok((plan, log))
}

pub fn add_mcp(
    scope: Scope,
    cwd: &Path,
    name: &str,
    norm: McpNormalized,
    agents_filter: Option<&[AgentKind]>,
    dry_run: bool,
) -> anyhow::Result<(ManagePlan, Vec<String>)> {
    let mut plan = plan_add_mcp(scope, cwd, name, &norm, agents_filter)?;
    plan.dry_run = dry_run;
    if dry_run {
        return Ok((plan, vec![]));
    }
    let log = apply_manage_plan(&plan, Some(&norm))?;
    Ok((plan, log))
}

pub fn remove_mcp(
    scope: Scope,
    cwd: &Path,
    name: &str,
    agents_filter: Option<&[AgentKind]>,
    dry_run: bool,
) -> anyhow::Result<(ManagePlan, Vec<String>)> {
    let mut plan = plan_remove_mcp(scope, cwd, name, agents_filter)?;
    plan.dry_run = dry_run;
    if dry_run {
        return Ok((plan, vec![]));
    }
    let log = apply_manage_plan(&plan, None)?;
    Ok((plan, log))
}

pub fn parse_command_line(s: &str) -> Vec<String> {
    // Simple whitespace split; quoted segments supported lightly.
    let mut out = Vec::new();
    let mut cur = String::new();
    let mut in_quote: Option<char> = None;
    for ch in s.chars() {
        if let Some(q) = in_quote {
            if ch == q {
                in_quote = None;
            } else {
                cur.push(ch);
            }
        } else if ch == '"' || ch == '\'' {
            in_quote = Some(ch);
        } else if ch.is_whitespace() {
            if !cur.is_empty() {
                out.push(std::mem::take(&mut cur));
            }
        } else {
            cur.push(ch);
        }
    }
    if !cur.is_empty() {
        out.push(cur);
    }
    out
}

pub fn mcp_from_cli(
    transport: &str,
    command: Option<&str>,
    url: Option<&str>,
    enabled: Option<bool>,
) -> anyhow::Result<McpNormalized> {
    let requested = transport.trim().to_ascii_lowercase();
    let canonical = canonical_transport(Some(&requested), url, command.is_some());
    let mut norm = McpNormalized {
        transport: canonical.clone(),
        command: None,
        url: None,
        args: None,
        enabled,
        env_keys: vec![],
        env: BTreeMap::new(),
    };
    match canonical.as_str() {
        "stdio" => {
            let cmd = command.ok_or_else(|| anyhow::anyhow!("--command required for stdio"))?;
            let parts = parse_command_line(cmd);
            if parts.is_empty() {
                anyhow::bail!("empty --command");
            }
            if parts.len() == 1 {
                norm.command = Some(parts);
            } else {
                norm.command = Some(vec![parts[0].clone()]);
                norm.args = Some(parts[1..].to_vec());
            }
        }
        "http" | "sse" => {
            let u = url.ok_or_else(|| anyhow::anyhow!("--url required for {requested}"))?;
            norm.url = Some(u.to_string());
        }
        _ => anyhow::bail!("unknown transport '{requested}' (use stdio|http|sse|remote|local)"),
    }
    Ok(norm)
}
