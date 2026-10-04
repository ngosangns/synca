//! Make skill frontmatter loadable by Pi.
//!
//! Pi (Agent Skills spec) rejects a skill when:
//! - `name` is not `^[a-z0-9-]+$` (also no leading/trailing `-`, no `--`, max 64)
//! - YAML frontmatter has an unquoted `:` (plain scalars become nested mappings)
//! - the same `name` exists at two real paths (symlinks to one file are silent)
//!
//! Repairs run as part of skill sync. Identical extra copies become symlinks so
//! Pi's realpath dedupe drops them. Copies whose trees differ are left alone.

use crate::discover::frontmatter_block;
use crate::models::{normalize_key, Scope};
use crate::paths::project_root;
use crate::sync::SyncAction;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

const MAX_NAME_LEN: usize = 64;

pub fn pi_skill_name_ok(name: &str) -> bool {
    if name.is_empty() || name.len() > MAX_NAME_LEN {
        return false;
    }
    if name.starts_with('-') || name.ends_with('-') || name.contains("--") {
        return false;
    }
    name.bytes()
        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
}

pub fn slugify_skill_name(name: &str) -> String {
    let mut out = String::new();
    let mut prev_hyphen = false;
    for c in name.chars() {
        let c = c.to_ascii_lowercase();
        if c.is_ascii_lowercase() || c.is_ascii_digit() {
            out.push(c);
            prev_hyphen = false;
        } else if !prev_hyphen && !out.is_empty() {
            out.push('-');
            prev_hyphen = true;
        }
    }
    while out.ends_with('-') {
        out.pop();
    }
    if out.len() > MAX_NAME_LEN {
        out.truncate(MAX_NAME_LEN);
        while out.ends_with('-') {
            out.pop();
        }
    }
    if out.is_empty() {
        "skill".into()
    } else {
        out
    }
}

fn yaml_double_quote(value: &str) -> String {
    let mut out = String::from("\"");
    for c in value.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            _ => out.push(c),
        }
    }
    out.push('"');
    out
}

fn split_top_key(line: &str) -> Option<(&str, &str)> {
    if line.starts_with(' ') || line.starts_with('\t') || line.is_empty() {
        return None;
    }
    let (key, value) = line.split_once(':')?;
    if key.is_empty()
        || !key
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_')
    {
        return None;
    }
    Some((key, value.trim()))
}

fn plain_scalar_needs_quote(value: &str) -> bool {
    if value.is_empty() {
        return false;
    }
    match value.as_bytes()[0] {
        b'"' | b'\'' | b'|' | b'>' | b'[' | b'{' | b'*' | b'&' | b'!' => false,
        _ => value.contains(':'),
    }
}

fn unquote_scalar(value: &str) -> &str {
    let value = value.trim();
    if value.len() >= 2 {
        let bytes = value.as_bytes();
        if (bytes[0] == b'"' && bytes[value.len() - 1] == b'"')
            || (bytes[0] == b'\'' && bytes[value.len() - 1] == b'\'')
        {
            return &value[1..value.len() - 1];
        }
    }
    value
}

/// Rewrite frontmatter so Pi accepts it. `None` when the file is already safe.
pub fn repair_skill_frontmatter(text: &str, folder_name: &str) -> Option<String> {
    let fm = frontmatter_block(text)?;
    let fm_body = fm.trim_start_matches('\n');
    if fm_body.is_empty() {
        return None;
    }
    let mut lines: Vec<String> = fm_body.lines().map(|s| s.to_string()).collect();
    let mut changed = false;
    let folder_ok = pi_skill_name_ok(folder_name);

    for line in &mut lines {
        let Some((key, value)) = split_top_key(line) else {
            continue;
        };
        if key != "name" {
            continue;
        }
        let current = unquote_scalar(value);
        if pi_skill_name_ok(current) {
            continue;
        }
        let replacement = if folder_ok {
            folder_name.to_string()
        } else {
            slugify_skill_name(current)
        };
        if !pi_skill_name_ok(&replacement) || replacement == current {
            continue;
        }
        *line = format!("name: {replacement}");
        changed = true;
    }

    for line in &mut lines {
        let Some((key, value)) = split_top_key(line) else {
            continue;
        };
        if key == "name" || !plain_scalar_needs_quote(value) {
            continue;
        }
        let quoted = yaml_double_quote(value);
        *line = format!("{key}: {quoted}");
        changed = true;
    }

    if !changed {
        return None;
    }

    let rest = text.strip_prefix("---")?;
    let end = rest.find("\n---")?;
    let after = &rest[end..];
    Some(format!("---\n{}{after}", lines.join("\n")))
}

fn effective_name(text: &str, folder_name: &str) -> String {
    let rendered = repair_skill_frontmatter(text, folder_name).unwrap_or_else(|| text.to_string());
    let fm = frontmatter_block(&rendered).unwrap_or("");
    for line in fm.lines() {
        if let Some(("name", value)) = split_top_key(line) {
            let name = unquote_scalar(value);
            if !name.is_empty() {
                return name.to_string();
            }
        }
    }
    folder_name.to_string()
}

pub(crate) fn tree_hash(dir: &Path) -> std::io::Result<String> {
    let mut parts: Vec<(String, Vec<u8>)> = Vec::new();
    if dir.is_dir() {
        for entry in walkdir::WalkDir::new(dir)
            .follow_links(false)
            .into_iter()
            .filter_map(|e| e.ok())
        {
            let rel = entry
                .path()
                .strip_prefix(dir)
                .unwrap_or(entry.path())
                .to_string_lossy()
                .replace('\\', "/");
            if entry.file_type().is_symlink() {
                let target = std::fs::read_link(entry.path()).unwrap_or_default();
                parts.push((rel, target.to_string_lossy().as_bytes().to_vec()));
            } else if entry.file_type().is_file() {
                if let Ok(bytes) = std::fs::read(entry.path()) {
                    parts.push((rel, bytes));
                }
            }
        }
    }
    parts.sort_by(|a, b| a.0.cmp(&b.0));
    let mut buf = Vec::new();
    for (rel, bytes) in parts {
        buf.extend_from_slice(rel.as_bytes());
        buf.push(0);
        buf.extend_from_slice(&bytes);
        buf.push(0);
    }
    Ok(crate::paths::hash_bytes(&buf))
}

struct SkillDir {
    path: PathBuf,
    folder: String,
    name: String,
    hash: String,
}

fn read_skill_dir(path: &Path) -> Option<SkillDir> {
    let meta = std::fs::symlink_metadata(path).ok()?;
    if meta.file_type().is_symlink() || !meta.file_type().is_dir() {
        return None;
    }
    let md = path.join("SKILL.md");
    if !md.is_file() {
        return None;
    }
    let folder = path.file_name()?.to_string_lossy().to_string();
    let text = std::fs::read_to_string(&md).ok()?;
    let name = effective_name(&text, &folder);
    let hash = tree_hash(path).ok()?;
    Some(SkillDir {
        path: path.to_path_buf(),
        folder,
        name,
        hash,
    })
}

fn list_real_skill_dirs(root: &Path) -> Vec<SkillDir> {
    let Ok(entries) = std::fs::read_dir(root) else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for ent in entries.flatten() {
        if let Some(info) = read_skill_dir(&ent.path()) {
            out.push(info);
        }
    }
    out.sort_by(|a, b| a.folder.cmp(&b.folder));
    out
}

fn same_path(a: &Path, b: &Path) -> bool {
    match (a.canonicalize(), b.canonicalize()) {
        (Ok(a), Ok(b)) => a == b,
        _ => a == b,
    }
}

fn pick_keeper<'a>(dirs: &[&'a SkillDir]) -> &'a SkillDir {
    if let Some(hit) = dirs.iter().find(|d| d.folder == d.name) {
        return hit;
    }
    let key = normalize_key(&dirs[0].name);
    if let Some(hit) = dirs.iter().find(|d| d.folder == key) {
        return hit;
    }
    dirs[0]
}

/// Pi-compat actions for this scope. User scope also relinks a cwd project
/// skill when its tree is identical to the user skill (Pi would otherwise
/// warn that the project copy shadows the user copy).
pub fn plan_pi_compat(
    scope: Scope,
    cwd: &Path,
    canonical: &Path,
    only_key: Option<&str>,
) -> Vec<SyncAction> {
    if !canonical.is_dir() {
        return Vec::new();
    }
    let mut actions = Vec::new();
    let mut seen_md = BTreeMap::<PathBuf, ()>::new();

    for info in list_real_skill_dirs(canonical) {
        let md = info.path.join("SKILL.md");
        let Ok(real_md) = md.canonicalize() else {
            continue;
        };
        if seen_md.insert(real_md.clone(), ()).is_some() {
            continue;
        }
        let Ok(text) = std::fs::read_to_string(&md) else {
            continue;
        };
        if repair_skill_frontmatter(&text, &info.folder).is_none() {
            continue;
        }
        if only_key.is_some_and(|k| {
            normalize_key(k) != normalize_key(&info.name) && k != info.folder && k != info.name
        }) {
            continue;
        }
        actions.push(SyncAction::RepairSkillFrontmatter {
            path: md,
            skill_key: info.name.clone(),
        });
    }

    let dirs = list_real_skill_dirs(canonical);
    let mut by_name: BTreeMap<String, Vec<&SkillDir>> = BTreeMap::new();
    for info in &dirs {
        by_name.entry(info.name.clone()).or_default().push(info);
    }
    for (name, group) in &by_name {
        if only_key.is_some_and(|k| normalize_key(k) != normalize_key(name) && k != name) {
            continue;
        }
        let mut by_hash: BTreeMap<&str, Vec<&SkillDir>> = BTreeMap::new();
        for info in group {
            by_hash.entry(info.hash.as_str()).or_default().push(info);
        }
        for same in by_hash.values() {
            if same.len() < 2 {
                continue;
            }
            let keeper = pick_keeper(same);
            for extra in same {
                if same_path(&extra.path, &keeper.path) {
                    continue;
                }
                actions.push(SyncAction::CollapseSkillAlias {
                    from: extra.path.clone(),
                    to: keeper.path.clone(),
                    skill_key: name.clone(),
                });
            }
        }
    }

    if scope == Scope::User {
        actions.extend(plan_project_shadows(cwd, canonical, only_key));
    }
    actions
}

fn plan_project_shadows(cwd: &Path, user_root: &Path, only_key: Option<&str>) -> Vec<SyncAction> {
    let Some(proj) = project_root(cwd) else {
        return Vec::new();
    };
    let proj_skills = proj.join(".agents/skills");
    if !proj_skills.is_dir() || same_path(&proj_skills, user_root) {
        return Vec::new();
    }
    let user_dirs = list_real_skill_dirs(user_root);
    let mut user_by_name: BTreeMap<String, Vec<&SkillDir>> = BTreeMap::new();
    for info in &user_dirs {
        user_by_name.entry(info.name.clone()).or_default().push(info);
    }
    let mut actions = Vec::new();
    for proj_dir in list_real_skill_dirs(&proj_skills) {
        if only_key.is_some_and(|k| {
            normalize_key(k) != normalize_key(&proj_dir.name) && k != proj_dir.folder
        }) {
            continue;
        }
        let Some(group) = user_by_name.get(&proj_dir.name) else {
            continue;
        };
        let Some(user_dir) = group.iter().find(|u| u.hash == proj_dir.hash) else {
            continue;
        };
        let keeper = pick_keeper(group);
        let target = if keeper.hash == proj_dir.hash {
            &keeper.path
        } else {
            &user_dir.path
        };
        if same_path(&proj_dir.path, target) {
            continue;
        }
        actions.push(SyncAction::CollapseSkillAlias {
            from: proj_dir.path.clone(),
            to: target.clone(),
            skill_key: proj_dir.name.clone(),
        });
    }
    actions
}

/// Repair every real skill directory under `root`. Symlinks are left alone
/// (they share the target file, which is repaired when that directory is real).
pub fn repair_installed_skills(root: &Path) -> Vec<String> {
    let mut log = Vec::new();
    if !root.is_dir() {
        return log;
    }
    for info in list_real_skill_dirs(root) {
        let md = info.path.join("SKILL.md");
        match apply_frontmatter_repair(&md) {
            Ok(true) => log.push(format!(
                "repaired frontmatter {}: {}",
                info.folder,
                md.display()
            )),
            Ok(false) => {}
            Err(err) => log.push(format!("repair failed {}: {err}", md.display())),
        }
    }
    log
}

/// Rewrite one SKILL.md. Returns false when nothing changed.
pub fn apply_frontmatter_repair(path: &Path) -> std::io::Result<bool> {
    let text = std::fs::read_to_string(path)?;
    let folder = path
        .parent()
        .and_then(|p| p.file_name())
        .map(|s| s.to_string_lossy().to_string())
        .unwrap_or_default();
    let Some(repaired) = repair_skill_frontmatter(&text, &folder) else {
        return Ok(false);
    };
    std::fs::write(path, repaired)?;
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn repairs_invalid_name_and_unquoted_colon() {
        let raw = "---\nname: Make Bot UI\ndescription: Triggers: money resolver\n---\n# body\n";
        let fixed = repair_skill_frontmatter(raw, "make-bot-ui").unwrap();
        assert!(fixed.contains("name: make-bot-ui\n"));
        assert!(fixed.contains("description: \"Triggers: money resolver\"\n"));
        assert!(fixed.contains("# body\n"));
        assert!(repair_skill_frontmatter(&fixed, "make-bot-ui").is_none());
    }

    #[test]
    fn leaves_block_scalars_and_valid_names() {
        let raw = "---\nname: poteto-mode\ndescription: >-\n  line: still a block\n---\n";
        assert!(repair_skill_frontmatter(raw, "poteto-mode").is_none());
    }

    #[test]
    fn slugifies_when_folder_name_is_also_invalid() {
        let raw = "---\nname: Poteto Mode\n---\n";
        let fixed = repair_skill_frontmatter(raw, "Poteto Mode").unwrap();
        assert!(fixed.contains("name: poteto-mode\n"));
    }
}
