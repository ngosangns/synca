use serde::Serialize;
use crate::agents::{mcp_config_paths, skill_roots};
use crate::discover::{skill_description, skill_display_name};
use crate::models::*;
use crate::paths::{hash_bytes, hash_skill_dir, project_root};
use serde_json::Value as JsonValue;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Serialize)]
pub struct Inventory {
    pub scope: Scope,
    pub project_root: Option<PathBuf>,
    pub skills: Vec<SkillEntry>,
    pub mcps: Vec<McpEntry>,
}


pub fn scan_all(scope: Scope, cwd: &Path) -> Inventory {
    let root = match scope {
        Scope::User => None,
        Scope::Project => project_root(cwd),
    };
    Inventory {
        scope,
        project_root: root.clone(),
        skills: scan_skills(scope, cwd),
        mcps: scan_mcp(scope, cwd),
    }
}

pub fn scan_skills(scope: Scope, cwd: &Path) -> Vec<SkillEntry> {
    let mut by_key: BTreeMap<String, SkillEntry> = BTreeMap::new();

    for (agent, root) in skill_roots(scope, cwd) {
        if !root.is_dir() {
            continue;
        }
        let Ok(entries) = std::fs::read_dir(&root) else {
            continue;
        };
        for ent in entries.flatten() {
            let path = ent.path();
            if !path.is_dir() && !path.is_symlink() {
                // also accept symlink-to-dir
                let meta = std::fs::symlink_metadata(&path).ok();
                let is_link = meta.as_ref().map(|m| m.file_type().is_symlink()).unwrap_or(false);
                if !is_link {
                    continue;
                }
            }
            // Resolve for hashing but keep original path for presence
            let is_symlink = std::fs::symlink_metadata(&path)
                .map(|m| m.file_type().is_symlink())
                .unwrap_or(false);
            let symlink_target = if is_symlink {
                std::fs::read_link(&path).ok()
            } else {
                None
            };
            let resolve_for_hash = if is_symlink {
                // Prefer canonical if possible
                path.canonicalize().unwrap_or_else(|_| {
                    symlink_target
                        .as_ref()
                        .map(|t| {
                            if t.is_absolute() {
                                t.clone()
                            } else {
                                root.join(t)
                            }
                        })
                        .unwrap_or_else(|| path.clone())
                })
            } else {
                path.clone()
            };
            if !resolve_for_hash.is_dir() && !resolve_for_hash.join("SKILL.md").exists() {
                // still try hash if dir-like
            }
            let content_hash = hash_skill_dir(&resolve_for_hash).unwrap_or_else(|_| "missing".into());
            let display = skill_display_name(&resolve_for_hash);
            let description = skill_description(&resolve_for_hash);
            let key = normalize_key(&display);

            let presence = SkillPresence {
                agent,
                path: path.clone(),
                is_symlink,
                symlink_target,
                content_hash: content_hash.clone(),
            };

            by_key
                .entry(key.clone())
                .and_modify(|e| {
                    if e.description.is_none() {
                        if let Some(ref d) = description {
                            e.description = Some(d.clone());
                        }
                    }
                    e.presence.push(presence.clone());
                })
                .or_insert_with(|| SkillEntry {
                    key: key.clone(),
                    display_name: display,
                    description,
                    scope,
                    presence: vec![presence],
                    mismatch: false,
                });
        }
    }

    let mut out: Vec<SkillEntry> = by_key.into_values().collect();
    for e in &mut out {
        let mut hashes: Vec<&str> = e.presence.iter().map(|p| p.content_hash.as_str()).collect();
        hashes.sort_unstable();
        hashes.dedup();
        e.mismatch = hashes.len() > 1;
        e.presence.sort_by_key(|p| p.agent);
    }
    out.sort_by(|a, b| a.key.cmp(&b.key));
    out
}

pub fn scan_mcp(scope: Scope, cwd: &Path) -> Vec<McpEntry> {
    let mut by_key: BTreeMap<String, McpEntry> = BTreeMap::new();

    for (agent, path) in mcp_config_paths(scope, cwd) {
        if !path.is_file() {
            continue;
        }
        let servers = match read_mcp_servers(agent, &path) {
            Ok(s) => s,
            Err(_) => continue,
        };
        for (name, norm) in servers {
            let key = normalize_key(&name);
            let fingerprint = mcp_fingerprint(&norm);
            let presence = McpPresence {
                agent,
                path: path.clone(),
                normalized: norm,
                fingerprint: fingerprint.clone(),
            };
            by_key
                .entry(key.clone())
                .and_modify(|e| e.presence.push(presence.clone()))
                .or_insert_with(|| McpEntry {
                    key: key.clone(),
                    scope,
                    presence: vec![presence],
                    mismatch: false,
                });
        }
    }

    let mut out: Vec<McpEntry> = by_key.into_values().collect();
    for e in &mut out {
        let mut fps: Vec<&str> = e.presence.iter().map(|p| p.fingerprint.as_str()).collect();
        fps.sort_unstable();
        fps.dedup();
        e.mismatch = fps.len() > 1;
        e.presence.sort_by_key(|p| p.agent);
    }
    out.sort_by(|a, b| a.key.cmp(&b.key));
    out
}

pub fn mcp_fingerprint(n: &McpNormalized) -> String {
    let mut parts = Vec::new();
    parts.push(n.transport.clone());
    if let Some(ref cmd) = n.command {
        parts.push(format!("cmd:{}", cmd.join("\u{1f}")));
    }
    if let Some(ref args) = n.args {
        parts.push(format!("args:{}", args.join("\u{1f}")));
    }
    if let Some(ref url) = n.url {
        parts.push(format!("url:{url}"));
    }
    // env keys only (not values) for equality of "structure"
    let mut keys = n.env_keys.clone();
    keys.sort();
    parts.push(format!("env:{}", keys.join(",")));
    hash_bytes(parts.join("|").as_bytes())
}

pub fn read_mcp_servers(
    agent: AgentKind,
    path: &Path,
) -> anyhow::Result<BTreeMap<String, McpNormalized>> {
    match agent {
        AgentKind::Grok => read_grok_toml(path),
        AgentKind::OpenCode => read_opencode_json(path),
        _ => read_mcp_servers_json(path),
    }
}

fn read_mcp_servers_json(path: &Path) -> anyhow::Result<BTreeMap<String, McpNormalized>> {
    let text = std::fs::read_to_string(path)?;
    let v: JsonValue = serde_json::from_str(&text)?;
    let servers = v
        .get("mcpServers")
        .or_else(|| v.get("mcp"))
        .cloned()
        .unwrap_or(JsonValue::Object(Default::default()));
    let obj = servers
        .as_object()
        .cloned()
        .unwrap_or_default();
    let mut out = BTreeMap::new();
    for (name, cfg) in obj {
        out.insert(name, json_to_normalized(&cfg));
    }
    Ok(out)
}

fn read_opencode_json(path: &Path) -> anyhow::Result<BTreeMap<String, McpNormalized>> {
    let text = std::fs::read_to_string(path)?;
    let v: JsonValue = serde_json::from_str(&text)?;
    let mcp = v.get("mcp").cloned().unwrap_or(JsonValue::Object(Default::default()));
    let obj = mcp.as_object().cloned().unwrap_or_default();
    let mut out = BTreeMap::new();
    for (name, cfg) in obj {
        out.insert(name, json_to_normalized(&cfg));
    }
    Ok(out)
}

fn read_grok_toml(path: &Path) -> anyhow::Result<BTreeMap<String, McpNormalized>> {
    let text = std::fs::read_to_string(path)?;
    let value: toml::Value = toml::from_str(&text)?;
    let mut out = BTreeMap::new();
    let Some(table) = value.get("mcp_servers").and_then(|v| v.as_table()) else {
        return Ok(out);
    };
    for (name, cfg) in table {
        out.insert(name.clone(), toml_to_normalized(cfg));
    }
    Ok(out)
}

pub fn json_to_normalized(cfg: &JsonValue) -> McpNormalized {
    let transport = cfg
        .get("type")
        .or_else(|| cfg.get("transport"))
        .and_then(|v| v.as_str())
        .unwrap_or_else(|| {
            if cfg.get("url").and_then(|v| v.as_str()).is_some() {
                "sse"
            } else {
                "stdio"
            }
        })
        .to_string();

    let command = match cfg.get("command") {
        Some(JsonValue::String(s)) => Some(vec![s.clone()]),
        Some(JsonValue::Array(a)) => Some(
            a.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect(),
        ),
        _ => None,
    };
    let args = cfg.get("args").and_then(|v| {
        v.as_array().map(|a| {
            a.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect()
        })
    });
    let url = cfg
        .get("url")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string());
    let enabled = cfg.get("enabled").and_then(|v| v.as_bool())
        .or_else(|| cfg.get("disabled").and_then(|v| v.as_bool()).map(|d| !d));

    let mut env = BTreeMap::new();
    let mut env_keys = Vec::new();
    if let Some(obj) = cfg.get("env").and_then(|v| v.as_object()) {
        for (k, v) in obj {
            env_keys.push(k.clone());
            if let Some(s) = v.as_str() {
                env.insert(k.clone(), s.to_string());
            } else {
                env.insert(k.clone(), v.to_string());
            }
        }
    }
    env_keys.sort();

    McpNormalized {
        transport,
        command,
        url,
        args,
        enabled,
        env_keys,
        env,
    }
}

pub fn toml_to_normalized(cfg: &toml::Value) -> McpNormalized {
    let transport = cfg
        .get("type")
        .or_else(|| cfg.get("transport"))
        .and_then(|v| v.as_str())
        .unwrap_or_else(|| {
            if cfg.get("url").is_some() {
                "sse"
            } else {
                "stdio"
            }
        })
        .to_string();

    let command = match cfg.get("command") {
        Some(toml::Value::String(s)) => Some(vec![s.clone()]),
        Some(toml::Value::Array(a)) => Some(
            a.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect(),
        ),
        _ => None,
    };
    let args = cfg.get("args").and_then(|v| {
        v.as_array().map(|a| {
            a.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect()
        })
    });
    let url = cfg.get("url").and_then(|v| v.as_str()).map(|s| s.to_string());
    let enabled = cfg.get("enabled").and_then(|v| v.as_bool());

    let mut env = BTreeMap::new();
    let mut env_keys = Vec::new();
    if let Some(tbl) = cfg.get("env").and_then(|v| v.as_table()) {
        for (k, v) in tbl {
            env_keys.push(k.clone());
            if let Some(s) = v.as_str() {
                env.insert(k.clone(), s.to_string());
            } else {
                env.insert(k.clone(), v.to_string());
            }
        }
    }
    env_keys.sort();

    McpNormalized {
        transport,
        command,
        url,
        args,
        enabled,
        env_keys,
        env,
    }
}
