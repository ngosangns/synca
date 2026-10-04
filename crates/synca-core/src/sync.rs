use crate::agents::{
    canonical_mcp_path, canonical_skills_dir, mcp_config_paths, skill_roots,
};
use crate::models::*;
use crate::scan::{scan_mcp, scan_skills};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value as JsonValue};
use std::collections::{BTreeMap, BTreeSet};
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum SyncAction {
    EnsureCanonicalCopy {
        from: PathBuf,
        to: PathBuf,
        skill_key: String,
    },
    SymlinkSkill {
        link: PathBuf,
        target: PathBuf,
        skill_key: String,
        agent: AgentKind,
    },
    SkipSame {
        path: PathBuf,
        skill_key: String,
        reason: String,
    },
    ConflictSkill {
        skill_key: String,
        paths: Vec<PathBuf>,
        hashes: Vec<String>,
    },
    EnsureMcpHub {
        hub: PathBuf,
        server: String,
        source_agent: AgentKind,
    },
    WriteMcpServer {
        path: PathBuf,
        agent: AgentKind,
        server: String,
    },
    SkipMcpSame {
        path: PathBuf,
        server: String,
        reason: String,
    },
    ConflictMcp {
        server: String,
        fingerprints: Vec<String>,
    },
    /// Rewrite SKILL.md frontmatter so Pi accepts the name and YAML.
    RepairSkillFrontmatter {
        path: PathBuf,
        skill_key: String,
    },
    /// Replace `from` with a symlink to `to`. Same skill name, identical tree.
    /// Pi realpath-dedupes the alias and stops reporting a collision.
    CollapseSkillAlias {
        from: PathBuf,
        to: PathBuf,
        skill_key: String,
    },
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct SyncPlan {
    pub scope: String,
    pub dry_run: bool,
    pub actions: Vec<SyncAction>,
}

pub fn plan_sync_skills(
    scope: Scope,
    cwd: &Path,
    agents_filter: Option<&[AgentKind]>,
    only_key: Option<&str>,
) -> anyhow::Result<SyncPlan> {
    let skills = scan_skills(scope, cwd);
    let Some(canonical) = canonical_skills_dir(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let targets: Vec<(AgentKind, PathBuf)> = skill_roots(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .filter(|(a, _)| {
            agents_filter
                .map(|f| f.contains(a))
                .unwrap_or(true)
        })
        .collect();

    let mut plan = SyncPlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
    };
    plan.actions
        .extend(crate::pi_skills::plan_pi_compat(scope, cwd, &canonical, only_key));

    for skill in &skills {
        if let Some(k) = only_key {
            if skill.key != normalize_key(k) && skill.display_name != k {
                continue;
            }
        }
        if skill.mismatch {
            plan.actions.push(SyncAction::ConflictSkill {
                skill_key: skill.key.clone(),
                paths: skill.presence.iter().map(|p| p.path.clone()).collect(),
                hashes: skill
                    .presence
                    .iter()
                    .map(|p| p.content_hash.clone())
                    .collect(),
            });
            continue;
        }

        // Pick a source: prefer canonical Agents presence, else first presence
        let source = skill
            .presence
            .iter()
            .find(|p| p.agent == AgentKind::Agents)
            .or_else(|| skill.presence.first())
            .cloned();
        let Some(source) = source else { continue };

        let canon_skill = canonical.join(&skill.key);
        // Resolve real source dir
        let source_real = if source.is_symlink {
            source
                .path
                .canonicalize()
                .unwrap_or_else(|_| source.path.clone())
        } else {
            source.path.clone()
        };

        if !canon_skill.exists() {
            plan.actions.push(SyncAction::EnsureCanonicalCopy {
                from: source_real.clone(),
                to: canon_skill.clone(),
                skill_key: skill.key.clone(),
            });
        } else {
            // already have canonical
            let same = source_real
                .canonicalize()
                .ok()
                .and_then(|s| canon_skill.canonicalize().ok().map(|c| s == c))
                .unwrap_or(false);
            if !same {
                // if hashes match we're fine; mismatch already handled
                plan.actions.push(SyncAction::SkipSame {
                    path: canon_skill.clone(),
                    skill_key: skill.key.clone(),
                    reason: "canonical already present".into(),
                });
            }
        }

        for (agent, root) in &targets {
            let link = root.join(&skill.key);
            if link.exists() || std::fs::symlink_metadata(&link).is_ok() {
                // check if already points to canonical
                if let Ok(meta) = std::fs::symlink_metadata(&link) {
                    if meta.file_type().is_symlink() {
                        if let Ok(tgt) = std::fs::read_link(&link) {
                            let resolved = if tgt.is_absolute() {
                                tgt.clone()
                            } else {
                                root.join(&tgt)
                            };
                            if resolved.canonicalize().ok() == canon_skill.canonicalize().ok()
                                || tgt == canon_skill
                            {
                                plan.actions.push(SyncAction::SkipSame {
                                    path: link.clone(),
                                    skill_key: skill.key.clone(),
                                    reason: format!("already linked for {}", agent.as_str()),
                                });
                                continue;
                            }
                        }
                    }
                    // exists but not our link — conflict-ish; skip with note
                    plan.actions.push(SyncAction::SkipSame {
                        path: link.clone(),
                        skill_key: skill.key.clone(),
                        reason: format!(
                            "path exists for {} (not overwriting; resolve manually)",
                            agent.as_str()
                        ),
                    });
                    continue;
                }
            }
            plan.actions.push(SyncAction::SymlinkSkill {
                link,
                target: canon_skill.clone(),
                skill_key: skill.key.clone(),
                agent: *agent,
            });
        }
    }

    Ok(plan)
}

pub fn plan_sync_mcp(
    scope: Scope,
    cwd: &Path,
    agents_filter: Option<&[AgentKind]>,
    only_key: Option<&str>,
) -> anyhow::Result<SyncPlan> {
    let mcps = scan_mcp(scope, cwd);
    let Some(hub) = canonical_mcp_path(scope, cwd) else {
        anyhow::bail!("no project root for project scope");
    };
    let writers: Vec<(AgentKind, PathBuf)> = mcp_config_paths(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .filter(|(a, _)| agents_filter.map(|f| f.contains(a)).unwrap_or(true))
        .collect();

    let mut plan = SyncPlan {
        scope: scope.as_str().into(),
        dry_run: true,
        actions: vec![],
    };

    for entry in &mcps {
        if let Some(k) = only_key {
            if entry.key != normalize_key(k) {
                continue;
            }
        }
        if entry.mismatch {
            plan.actions.push(SyncAction::ConflictMcp {
                server: entry.key.clone(),
                fingerprints: entry
                    .presence
                    .iter()
                    .map(|p| p.fingerprint.clone())
                    .collect(),
            });
            continue;
        }
        let source = entry
            .presence
            .iter()
            .find(|p| p.agent == AgentKind::Agents)
            .or_else(|| entry.presence.first())
            .cloned();
        let Some(source) = source else { continue };

        plan.actions.push(SyncAction::EnsureMcpHub {
            hub: hub.clone(),
            server: entry.key.clone(),
            source_agent: source.agent,
        });

        for (agent, path) in &writers {
            // If agent already has same fingerprint, skip
            if let Some(p) = entry.presence.iter().find(|p| p.agent == *agent) {
                if p.fingerprint == source.fingerprint {
                    plan.actions.push(SyncAction::SkipMcpSame {
                        path: path.clone(),
                        server: entry.key.clone(),
                        reason: format!("already present for {}", agent.as_str()),
                    });
                    continue;
                }
            }
            plan.actions.push(SyncAction::WriteMcpServer {
                path: path.clone(),
                agent: *agent,
                server: entry.key.clone(),
            });
        }
    }

    Ok(plan)
}

/// Apply a plan. Conflicts are resolved per `decisions` (default skip = never overwrite).
pub fn apply_plan(
    plan: &SyncPlan,
    cwd: &Path,
    scope: Scope,
    decisions: &ConflictDecisions,
) -> anyhow::Result<Vec<String>> {
    let mut log = Vec::new();
    let skills = scan_skills(scope, cwd);
    let skill_by_key: BTreeMap<String, SkillEntry> =
        skills.iter().map(|s| (s.key.clone(), s.clone())).collect();

    let mcps = scan_mcp(scope, cwd);
    let mcp_by_key: BTreeMap<String, McpEntry> =
        mcps.iter().map(|m| (m.key.clone(), m.clone())).collect();

    let skill_source: BTreeMap<String, PathBuf> = skills
        .iter()
        .filter_map(|s| {
            let p = s
                .presence
                .iter()
                .find(|p| p.agent == AgentKind::Agents)
                .or_else(|| s.presence.first())?;
            let real = if p.is_symlink {
                p.path.canonicalize().unwrap_or_else(|_| p.path.clone())
            } else {
                p.path.clone()
            };
            Some((s.key.clone(), real))
        })
        .collect();

    let mcp_source: BTreeMap<String, McpNormalized> = mcps
        .iter()
        .filter_map(|m| {
            let p = m
                .presence
                .iter()
                .find(|p| p.agent == AgentKind::Agents)
                .or_else(|| m.presence.first())?;
            Some((m.key.clone(), p.normalized.clone()))
        })
        .collect();

    for action in &plan.actions {
        match action {
            SyncAction::ConflictSkill { skill_key, .. } => {
                let policy = decisions.for_skill(skill_key);
                match policy {
                    ConflictPolicy::Skip => {
                        log.push(format!("conflict skipped (skill): {skill_key}"));
                    }
                    ConflictPolicy::KeepSource | ConflictPolicy::KeepTarget => {
                        let Some(entry) = skill_by_key.get(skill_key) else {
                            log.push(format!("conflict skill missing from inventory: {skill_key}"));
                            continue;
                        };
                        let lines = resolve_skill_conflict(scope, cwd, entry, policy)?;
                        log.extend(lines);
                    }
                }
            }
            SyncAction::ConflictMcp { server, .. } => {
                let policy = decisions.for_mcp(server);
                match policy {
                    ConflictPolicy::Skip => {
                        log.push(format!("conflict skipped (mcp): {server}"));
                    }
                    ConflictPolicy::KeepSource | ConflictPolicy::KeepTarget => {
                        let Some(entry) = mcp_by_key.get(server) else {
                            log.push(format!("conflict mcp missing from inventory: {server}"));
                            continue;
                        };
                        let lines = resolve_mcp_conflict(scope, cwd, entry, policy)?;
                        log.extend(lines);
                    }
                }
            }
            SyncAction::SkipSame { path, reason, .. }
            | SyncAction::SkipMcpSame { path, reason, .. } => {
                log.push(format!("skip {}: {reason}", path.display()));
            }
            SyncAction::EnsureCanonicalCopy { from, to, skill_key } => {
                let src = skill_source.get(skill_key).cloned().unwrap_or_else(|| from.clone());
                if let Some(parent) = to.parent() {
                    std::fs::create_dir_all(parent)?;
                }
                if to.exists() {
                    remove_path(to)?;
                }
                copy_dir_recursive(&src, to)?;
                log.push(format!("copied {} -> {}", src.display(), to.display()));
            }
            SyncAction::SymlinkSkill {
                link,
                target,
                skill_key: _,
                agent,
            } => {
                force_symlink(link, target, agent.as_str(), &mut log)?;
            }
            SyncAction::EnsureMcpHub {
                hub,
                server,
                source_agent: _,
            } => {
                let Some(norm) = mcp_source.get(server) else {
                    log.push(format!("no source mcp for {server}"));
                    continue;
                };
                upsert_mcp_json_hub(hub, server, norm)?;
                log.push(format!("hub upsert {} in {}", server, hub.display()));
            }
            SyncAction::RepairSkillFrontmatter { path, skill_key } => {
                match crate::pi_skills::apply_frontmatter_repair(path) {
                    Ok(true) => log.push(format!(
                        "repaired frontmatter {skill_key}: {}",
                        path.display()
                    )),
                    Ok(false) => log.push(format!(
                        "skip repair {skill_key}: {}",
                        path.display()
                    )),
                    Err(err) => log.push(format!(
                        "repair failed {skill_key} ({}): {err}",
                        path.display()
                    )),
                }
            }
            SyncAction::CollapseSkillAlias { from, to, skill_key } => {
                if !to.exists() {
                    log.push(format!(
                        "skip collapse {skill_key}: target missing {}",
                        to.display()
                    ));
                    continue;
                }
                let same = from.canonicalize().ok() == to.canonicalize().ok();
                if same {
                    log.push(format!(
                        "skip collapse {skill_key}: {} already aliases {}",
                        from.display(),
                        to.display()
                    ));
                    continue;
                }
                match (crate::pi_skills::tree_hash(from), crate::pi_skills::tree_hash(to)) {
                    (Ok(a), Ok(b)) if a == b => {
                        force_symlink(from, to, "pi-dedupe", &mut log)?;
                    }
                    (Ok(_), Ok(_)) => log.push(format!(
                        "skip collapse {skill_key}: trees differ {} vs {}",
                        from.display(),
                        to.display()
                    )),
                    (Err(err), _) | (_, Err(err)) => log.push(format!(
                        "skip collapse {skill_key}: {err}"
                    )),
                }
            }
            SyncAction::WriteMcpServer {
                path,
                agent,
                server,
            } => {
                let Some(norm) = mcp_source.get(server) else {
                    continue;
                };
                write_mcp_to_agent(path, *agent, server, norm)?;
                log.push(format!(
                    "wrote mcp {server} -> {} ({})",
                    path.display(),
                    agent.as_str()
                ));
            }
        }
    }
    // Copies and conflict resolution can land a SKILL.md after the planned
    // repair. Run once more so Pi still sees a valid name and quoted scalars.
    if let Some(canon) = canonical_skills_dir(scope, cwd) {
        log.extend(crate::pi_skills::repair_installed_skills(&canon));
    }
    Ok(log)
}

fn pick_skill_winner<'a>(
    entry: &'a SkillEntry,
    policy: ConflictPolicy,
) -> &'a crate::models::SkillPresence {
    match policy {
        ConflictPolicy::KeepSource => entry
            .presence
            .iter()
            .find(|p| p.agent == AgentKind::Agents)
            .or_else(|| entry.presence.first())
            .expect("presence non-empty"),
        ConflictPolicy::KeepTarget => entry
            .presence
            .iter()
            .find(|p| p.agent != AgentKind::Agents)
            .or_else(|| entry.presence.last())
            .or_else(|| entry.presence.first())
            .expect("presence non-empty"),
        ConflictPolicy::Skip => unreachable!(),
    }
}

fn pick_mcp_winner<'a>(
    entry: &'a McpEntry,
    policy: ConflictPolicy,
) -> &'a crate::models::McpPresence {
    match policy {
        ConflictPolicy::KeepSource => entry
            .presence
            .iter()
            .find(|p| p.agent == AgentKind::Agents)
            .or_else(|| entry.presence.first())
            .expect("presence non-empty"),
        ConflictPolicy::KeepTarget => entry
            .presence
            .iter()
            .find(|p| p.agent != AgentKind::Agents)
            .or_else(|| entry.presence.last())
            .or_else(|| entry.presence.first())
            .expect("presence non-empty"),
        ConflictPolicy::Skip => unreachable!(),
    }
}

fn resolve_skill_conflict(
    scope: Scope,
    cwd: &Path,
    entry: &SkillEntry,
    policy: ConflictPolicy,
) -> anyhow::Result<Vec<String>> {
    let mut log = Vec::new();
    let winner = pick_skill_winner(entry, policy);
    let winner_real = if winner.is_symlink {
        winner
            .path
            .canonicalize()
            .unwrap_or_else(|_| winner.path.clone())
    } else {
        winner.path.clone()
    };
    let Some(canonical) = canonical_skills_dir(scope, cwd) else {
        anyhow::bail!("no canonical skills dir");
    };
    let canon_skill = canonical.join(&entry.key);
    if let Some(parent) = canon_skill.parent() {
        std::fs::create_dir_all(parent)?;
    }
    // If winner is already the canonical path, keep it; else replace canonical.
    let winner_is_canon = winner_real.canonicalize().ok()
        == canon_skill.canonicalize().ok()
        || winner_real == canon_skill;
    if !winner_is_canon {
        if canon_skill.exists() || std::fs::symlink_metadata(&canon_skill).is_ok() {
            remove_path(&canon_skill)?;
        }
        copy_dir_recursive(&winner_real, &canon_skill)?;
        log.push(format!(
            "conflict skill {}: keep {} → canonical {}",
            entry.key,
            winner.agent.as_str(),
            canon_skill.display()
        ));
    } else {
        log.push(format!(
            "conflict skill {}: keep {} (already canonical)",
            entry.key,
            winner.agent.as_str()
        ));
    }

    let targets: Vec<(AgentKind, PathBuf)> = skill_roots(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
        .collect();
    for (agent, root) in targets {
        let link = root.join(&entry.key);
        force_symlink(&link, &canon_skill, agent.as_str(), &mut log)?;
    }
    Ok(log)
}

fn resolve_mcp_conflict(
    scope: Scope,
    cwd: &Path,
    entry: &McpEntry,
    policy: ConflictPolicy,
) -> anyhow::Result<Vec<String>> {
    let mut log = Vec::new();
    let winner = pick_mcp_winner(entry, policy);
    let norm = &winner.normalized;
    let Some(hub) = canonical_mcp_path(scope, cwd) else {
        anyhow::bail!("no mcp hub");
    };
    upsert_mcp_json_hub(&hub, &entry.key, norm)?;
    log.push(format!(
        "conflict mcp {}: keep {} → hub {}",
        entry.key,
        winner.agent.as_str(),
        hub.display()
    ));
    for (agent, path) in mcp_config_paths(scope, cwd)
        .into_iter()
        .filter(|(a, _)| *a != AgentKind::Agents)
    {
        write_mcp_to_agent(&path, agent, &entry.key, norm)?;
        log.push(format!(
            "conflict mcp {}: wrote {} ({})",
            entry.key,
            path.display(),
            agent.as_str()
        ));
    }
    Ok(log)
}

pub(crate) fn remove_path(path: &Path) -> anyhow::Result<()> {
    let meta = std::fs::symlink_metadata(path)?;
    if meta.file_type().is_symlink() || meta.file_type().is_file() {
        std::fs::remove_file(path)?;
    } else if meta.file_type().is_dir() {
        std::fs::remove_dir_all(path)?;
    }
    Ok(())
}

pub(crate) fn force_symlink(
    link: &Path,
    target: &Path,
    agent: &str,
    log: &mut Vec<String>,
) -> anyhow::Result<()> {
    if let Some(parent) = link.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let link_target = pathdiff_relative(link, target).unwrap_or_else(|| target.to_path_buf());
    #[cfg(unix)]
    {
        use std::os::unix::fs::symlink;
        if link.exists() || std::fs::symlink_metadata(link).is_ok() {
            // Already correct?
            if let Ok(meta) = std::fs::symlink_metadata(link) {
                if meta.file_type().is_symlink() {
                    if let Ok(tgt) = std::fs::read_link(link) {
                        let resolved = if tgt.is_absolute() {
                            tgt.clone()
                        } else {
                            link.parent().unwrap_or(Path::new(".")).join(&tgt)
                        };
                        if resolved.canonicalize().ok() == target.canonicalize().ok()
                            || tgt == *target
                        {
                            log.push(format!(
                                "skip symlink for {agent}: {} already linked",
                                link.display()
                            ));
                            return Ok(());
                        }
                    }
                }
            }
            remove_path(link)?;
        }
        symlink(&link_target, link)?;
        log.push(format!(
            "symlink {} -> {} ({agent})",
            link.display(),
            link_target.display()
        ));
    }
    #[cfg(not(unix))]
    {
        let _ = (link_target, agent);
        log.push("symlink unsupported on this platform".into());
    }
    Ok(())
}


fn pathdiff_relative(link: &Path, target: &Path) -> Option<PathBuf> {
    // Simple relative: if both under same parent chain use pathdiff-like logic manually
    let link_parent = link.parent()?;
    let target_c = target.canonicalize().ok().unwrap_or_else(|| target.to_path_buf());
    let link_c = link_parent.canonicalize().ok().unwrap_or_else(|| link_parent.to_path_buf());
    pathdiff(&link_c, &target_c)
}

fn pathdiff(from_dir: &Path, to: &Path) -> Option<PathBuf> {
    let from_comps: Vec<_> = from_dir.components().collect();
    let to_comps: Vec<_> = to.components().collect();
    let mut i = 0;
    while i < from_comps.len() && i < to_comps.len() && from_comps[i] == to_comps[i] {
        i += 1;
    }
    let mut rel = PathBuf::new();
    for _ in i..from_comps.len() {
        rel.push("..");
    }
    for c in &to_comps[i..] {
        rel.push(c.as_os_str());
    }
    if rel.as_os_str().is_empty() {
        rel.push(".");
    }
    Some(rel)
}

pub(crate) fn copy_dir_recursive(src: &Path, dst: &Path) -> anyhow::Result<()> {
    std::fs::create_dir_all(dst)?;
    for entry in walkdir::WalkDir::new(src).into_iter().filter_map(|e| e.ok()) {
        let rel = entry.path().strip_prefix(src).unwrap_or(entry.path());
        let dest = dst.join(rel);
        if entry.file_type().is_dir() {
            std::fs::create_dir_all(&dest)?;
        } else if entry.file_type().is_file() {
            if let Some(p) = dest.parent() {
                std::fs::create_dir_all(p)?;
            }
            std::fs::copy(entry.path(), &dest)?;
        }
    }
    Ok(())
}

pub fn upsert_mcp_json_hub(hub: &Path, server: &str, norm: &McpNormalized) -> anyhow::Result<()> {
    if let Some(parent) = hub.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut root: JsonValue = if hub.is_file() {
        serde_json::from_str(&std::fs::read_to_string(hub)?)?
    } else {
        json!({ "mcpServers": {} })
    };
    if !root.get("mcpServers").map(|v| v.is_object()).unwrap_or(false) {
        root["mcpServers"] = json!({});
    }
    let existing_env = root["mcpServers"]
        .get(server)
        .and_then(|s| s.get("env"))
        .cloned();
    let mut obj = normalized_to_json(norm);
    // Preserve secrets on hub if already present and source lacks
    if let Some(JsonValue::Object(old_env)) = existing_env {
        let env = obj
            .as_object_mut()
            .unwrap()
            .entry("env")
            .or_insert_with(|| json!({}));
        if let Some(env_obj) = env.as_object_mut() {
            for (k, v) in old_env {
                env_obj.entry(k).or_insert(v);
            }
        }
    }
    root["mcpServers"][server] = obj;
    std::fs::write(hub, serde_json::to_string_pretty(&root)?)?;
    Ok(())
}

pub(crate) fn write_mcp_to_agent(
    path: &Path,
    agent: AgentKind,
    server: &str,
    norm: &McpNormalized,
) -> anyhow::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    match agent {
        AgentKind::Grok => write_grok_toml(path, server, norm),
        AgentKind::OpenCode => write_opencode(path, server, norm),
        _ => write_mcp_servers_json(path, server, norm, false),
    }
}

pub fn normalized_to_json(norm: &McpNormalized) -> JsonValue {
    let mut obj = serde_json::Map::new();
    obj.insert("type".into(), json!(norm.transport));
    if let Some(ref cmd) = norm.command {
        if let Some((head, rest)) = cmd.split_first() {
            obj.insert("command".into(), json!(head));
            let mut args = rest.to_vec();
            args.extend(norm.args.clone().unwrap_or_default());
            if !args.is_empty() {
                obj.insert("args".into(), json!(args));
            }
        }
    } else if let Some(ref a) = norm.args {
        obj.insert("args".into(), json!(a));
    }
    if let Some(ref url) = norm.url {
        obj.insert("url".into(), json!(url));
    }
    if let Some(en) = norm.enabled {
        obj.insert("enabled".into(), json!(en));
    }
    if !norm.env.is_empty() {
        obj.insert("env".into(), json!(norm.env));
    }
    JsonValue::Object(obj)
}

fn write_mcp_servers_json(
    path: &Path,
    server: &str,
    norm: &McpNormalized,
    _opencode: bool,
) -> anyhow::Result<()> {
    let mut root: JsonValue = if path.is_file() {
        serde_json::from_str(&std::fs::read_to_string(path).unwrap_or_else(|_| "{}".into()))
            .unwrap_or(json!({}))
    } else {
        json!({})
    };
    if root.get("mcpServers").is_none() {
        root["mcpServers"] = json!({});
    }
    // Preserve env secrets
    let existing_env = root["mcpServers"]
        .get(server)
        .and_then(|s| s.get("env"))
        .cloned();
    let mut obj = normalized_to_json(norm);
    if let Some(JsonValue::Object(old_env)) = existing_env {
        let env = obj
            .as_object_mut()
            .unwrap()
            .entry("env")
            .or_insert_with(|| json!({}));
        if let Some(env_obj) = env.as_object_mut() {
            for (k, v) in old_env {
                env_obj.entry(k).or_insert(v);
            }
        }
    }
    root["mcpServers"][server] = obj;
    std::fs::write(path, serde_json::to_string_pretty(&root)?)?;
    Ok(())
}

/// OpenCode's schema differs from the `mcpServers` format: `type` is `local` or
/// `remote`, `command` is a single array, and env lives under `environment`.
fn opencode_entry(norm: &McpNormalized) -> JsonValue {
    let mut obj = serde_json::Map::new();
    if norm.transport == "stdio" {
        obj.insert("type".into(), json!("local"));
        let mut cmd = norm.command.clone().unwrap_or_default();
        cmd.extend(norm.args.clone().unwrap_or_default());
        obj.insert("command".into(), json!(cmd));
    } else {
        obj.insert("type".into(), json!("remote"));
        if let Some(ref url) = norm.url {
            obj.insert("url".into(), json!(url));
        }
    }
    if let Some(en) = norm.enabled {
        obj.insert("enabled".into(), json!(en));
    }
    if !norm.env.is_empty() {
        obj.insert("environment".into(), json!(norm.env));
    }
    JsonValue::Object(obj)
}

fn write_opencode(path: &Path, server: &str, norm: &McpNormalized) -> anyhow::Result<()> {
    let mut root: JsonValue = if path.is_file() {
        serde_json::from_str(&std::fs::read_to_string(path).unwrap_or_else(|_| "{}".into()))
            .unwrap_or(json!({}))
    } else {
        json!({})
    };
    if root.get("mcp").is_none() {
        root["mcp"] = json!({});
    }
    let existing_env = root["mcp"]
        .get(server)
        .and_then(|s| s.get("environment").or_else(|| s.get("env")))
        .cloned();
    let mut obj = opencode_entry(norm);
    if let Some(JsonValue::Object(old_env)) = existing_env {
        let env = obj
            .as_object_mut()
            .unwrap()
            .entry("environment")
            .or_insert_with(|| json!({}));
        if let Some(env_obj) = env.as_object_mut() {
            for (k, v) in old_env {
                env_obj.entry(k).or_insert(v);
            }
        }
    }
    root["mcp"][server] = obj;
    std::fs::write(path, serde_json::to_string_pretty(&root)?)?;
    Ok(())
}

fn write_grok_toml(path: &Path, server: &str, norm: &McpNormalized) -> anyhow::Result<()> {
    let text = if path.is_file() {
        std::fs::read_to_string(path)?
    } else {
        String::new()
    };
    let mut value: toml::Value = if text.trim().is_empty() {
        toml::Value::Table(toml::map::Map::new())
    } else {
        toml::from_str(&text)?
    };
    let root = value.as_table_mut().unwrap();
    if !root.contains_key("mcp_servers") {
        root.insert(
            "mcp_servers".into(),
            toml::Value::Table(toml::map::Map::new()),
        );
    }
    let servers = root
        .get_mut("mcp_servers")
        .unwrap()
        .as_table_mut()
        .unwrap();

    // Preserve env
    let mut existing_env: BTreeMap<String, String> = BTreeMap::new();
    if let Some(old) = servers.get(server).and_then(|v| v.as_table()) {
        if let Some(env) = old.get("env").and_then(|e| e.as_table()) {
            for (k, v) in env {
                if let Some(s) = v.as_str() {
                    existing_env.insert(k.clone(), s.to_string());
                }
            }
        }
    }

    let mut tbl = toml::map::Map::new();
    tbl.insert("type".into(), toml::Value::String(norm.transport.clone()));
    if let Some(ref cmd) = norm.command {
        if let Some((head, rest)) = cmd.split_first() {
            tbl.insert("command".into(), toml::Value::String(head.clone()));
            let mut args: Vec<toml::Value> = rest
                .iter()
                .chain(norm.args.iter().flatten())
                .map(|s| toml::Value::String(s.clone()))
                .collect();
            if !args.is_empty() {
                tbl.insert("args".into(), toml::Value::Array(std::mem::take(&mut args)));
            }
        }
    } else if let Some(ref a) = norm.args {
        tbl.insert(
            "args".into(),
            toml::Value::Array(a.iter().map(|s| toml::Value::String(s.clone())).collect()),
        );
    }
    if let Some(ref url) = norm.url {
        tbl.insert("url".into(), toml::Value::String(url.clone()));
    }
    if let Some(en) = norm.enabled {
        tbl.insert("enabled".into(), toml::Value::Boolean(en));
    }
    let mut env_tbl = toml::map::Map::new();
    for (k, v) in &existing_env {
        env_tbl.insert(k.clone(), toml::Value::String(v.clone()));
    }
    for (k, v) in &norm.env {
        env_tbl
            .entry(k.clone())
            .or_insert_with(|| toml::Value::String(v.clone()));
    }
    if !env_tbl.is_empty() {
        tbl.insert("env".into(), toml::Value::Table(env_tbl));
    }
    servers.insert(server.to_string(), toml::Value::Table(tbl));

    std::fs::write(path, toml::to_string_pretty(&value)?)?;
    Ok(())
}

pub fn merge_plans(mut a: SyncPlan, b: SyncPlan) -> SyncPlan {
    // Never silently combine User + Project; keep `a.scope` and drop mismatched B.
    // Callers (TUI/CLI) always pass same-scope plans; this is a safety net.
    if a.scope != b.scope {
        // Prefer keeping A intact rather than mixing scopes.
        return a;
    }
    a.actions.extend(b.actions);
    a.dry_run = a.dry_run && b.dry_run;
    a
}

/// Filter plan to only "missing" actions (symlinks/writes that aren't skips/conflicts).
pub fn filter_missing(plan: &SyncPlan) -> SyncPlan {
    let actions: Vec<_> = plan
        .actions
        .iter()
        .filter(|a| {
            matches!(
                a,
                SyncAction::EnsureCanonicalCopy { .. }
                    | SyncAction::SymlinkSkill { .. }
                    | SyncAction::EnsureMcpHub { .. }
                    | SyncAction::WriteMcpServer { .. }
            )
        })
        .cloned()
        .collect();
    SyncPlan {
        scope: plan.scope.clone(),
        dry_run: plan.dry_run,
        actions,
    }
}

pub fn agents_present_in_plan(plan: &SyncPlan) -> BTreeSet<String> {
    let mut s = BTreeSet::new();
    for a in &plan.actions {
        match a {
            SyncAction::SymlinkSkill { agent, .. } | SyncAction::WriteMcpServer { agent, .. } => {
                s.insert(agent.as_str().into());
            }
            _ => {}
        }
    }
    s
}
