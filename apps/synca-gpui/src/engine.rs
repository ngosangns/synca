//! Command lines for the synca CLI, plus the project list and the activity log
//! the Swift app already stores. The CLI remains the only writer of skills and MCP.

use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use serde_json::Value;

pub const DOMAIN: &str = "com.ngosangns.synca";

/// The hub row in `skills list --json` / `mcp list --json`. It is a presence
/// entry, not an agent, so chips and counts skip it.
pub const CANONICAL_AGENT: &str = "agents";

// What `synca update --check --json` means for the window.

pub const PROVIDERS: &[(&str, &str)] = &[
    ("grok", "Grok"),
    ("devin", "Devin"),
    ("kiro", "Kiro"),
    ("opencode", "OpenCode"),
    ("claude", "Claude"),
    ("cursor", "Cursor"),
];

/// Pi and OMP are no longer sync targets. A saved disabled list or an old
/// inventory row may still name them; callers skip those ids.
pub fn retired_agent(name: &str) -> bool {
    name.eq_ignore_ascii_case("pi") || name.eq_ignore_ascii_case("omp")
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Scope {
    User,
    Project,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Target {
    Skills,
    Mcp,
    All,
}

impl Target {
    fn as_str(self) -> &'static str {
        match self {
            Self::Skills => "skills",
            Self::Mcp => "mcp",
            Self::All => "all",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Policy {
    Skip,
    KeepSource,
    KeepTarget,
}

impl Policy {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Skip => "skip",
            Self::KeepSource => "keep-source",
            Self::KeepTarget => "keep-target",
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Self::Skip => "Skip",
            Self::KeepSource => "Keep source",
            Self::KeepTarget => "Keep target",
        }
    }
}

#[derive(Clone)]
pub struct Conflict {
    pub skill: bool,
    pub key: String,
    pub policy: Policy,
}

pub fn scope_args(scope: Scope, dir: Option<&str>) -> Result<Vec<String>, String> {
    match scope {
        Scope::User => Ok(vec!["--scope".into(), "user".into()]),
        Scope::Project => {
            let dir = dir.filter(|dir| !dir.is_empty()).ok_or_else(|| {
                "Choose a workspace folder to use the Workspace scope.".to_string()
            })?;
            Ok(vec![
                "--scope".into(),
                "project".into(),
                "--cwd".into(),
                dir.into(),
            ])
        }
    }
}

pub fn with_agents(mut args: Vec<String>, disabled: &[String]) -> Vec<String> {
    // An explicit per-agent filter already names its targets.
    if args.iter().any(|arg| arg == "--agents") {
        return args;
    }
    let disabled = normalize_disabled(disabled);
    let enabled: Vec<&str> = PROVIDERS
        .iter()
        .map(|(id, _)| *id)
        .filter(|id| !disabled.iter().any(|item| item == id))
        .collect();
    if enabled.len() == PROVIDERS.len() {
        return args;
    }
    args.push("--agents".into());
    args.push(if enabled.is_empty() {
        "none".into()
    } else {
        enabled.join(",")
    });
    args
}

pub fn sync_plan(
    scope: Scope,
    dir: Option<&str>,
    target: Target,
    key: Option<&str>,
) -> Result<Vec<String>, String> {
    let mut args = vec!["sync".into(), target.as_str().into()];
    args.extend(scope_args(scope, dir)?);
    args.push("--dry-run".into());
    if let Some(key) = key.filter(|key| !key.is_empty() && target != Target::All) {
        args.push("--key".into());
        args.push(key.into());
    }
    args.push("--json".into());
    Ok(args)
}

pub fn sync_apply(
    scope: Scope,
    dir: Option<&str>,
    target: Target,
    key: Option<&str>,
) -> Result<Vec<String>, String> {
    let mut args = vec!["sync".into(), target.as_str().into()];
    args.extend(scope_args(scope, dir)?);
    args.extend(["--on-conflict".into(), "skip".into()]);
    if let Some(key) = key.filter(|key| !key.is_empty() && target != Target::All) {
        args.push("--key".into());
        args.push(key.into());
    }
    args.push("--json".into());
    Ok(args)
}

pub fn resolve(
    scope: Scope,
    dir: Option<&str>,
    conflict: &Conflict,
) -> Result<Vec<String>, String> {
    let mut args = vec![
        "sync".into(),
        if conflict.skill { "skills" } else { "mcp" }.into(),
    ];
    args.extend(scope_args(scope, dir)?);
    args.extend([
        "--on-conflict".into(),
        conflict.policy.as_str().into(),
        "--key".into(),
        conflict.key.clone(),
    ]);
    if matches!(conflict.policy, Policy::KeepSource | Policy::KeepTarget) {
        args.push("--yes".into());
    }
    args.push("--json".into());
    Ok(args)
}

pub fn install_args(
    scope: Scope,
    dir: Option<&str>,
    source: &str,
    dry_run: bool,
) -> Result<Vec<String>, String> {
    let mut args = vec!["skills".into(), "install".into(), source.into()];
    args.extend(scope_args(scope, dir)?);
    if dry_run {
        args.push("--dry-run".into());
    }
    Ok(args)
}

pub fn remove_skill(
    scope: Scope,
    dir: Option<&str>,
    key: &str,
    purge: bool,
    agents: Option<&[String]>,
) -> Result<Vec<String>, String> {
    let mut args = vec!["skills".into(), "remove".into(), key.into()];
    args.extend(scope_args(scope, dir)?);
    if purge {
        args.extend(["--purge".into(), "--yes".into()]);
    }
    if let Some(agents) = agents.filter(|agents| !agents.is_empty()) {
        args.push("--agents".into());
        args.push(agents.join(","));
    }
    Ok(args)
}

#[allow(clippy::too_many_arguments)]
pub fn add_mcp(
    scope: Scope,
    dir: Option<&str>,
    name: &str,
    transport: &str,
    command: &str,
    url: &str,
    enabled: bool,
    dry_run: bool,
) -> Result<Vec<String>, String> {
    let mut args = vec![
        "mcp".into(),
        "add".into(),
        name.into(),
        "--transport".into(),
        transport.into(),
    ];
    args.extend(scope_args(scope, dir)?);
    if !command.is_empty() {
        args.extend(["--command".into(), command.into()]);
    }
    if !url.is_empty() {
        args.extend(["--url".into(), url.into()]);
    }
    args.extend([
        "--enabled".into(),
        if enabled { "true" } else { "false" }.into(),
    ]);
    if dry_run {
        args.push("--dry-run".into());
    }
    Ok(args)
}

pub fn remove_mcp(
    scope: Scope,
    dir: Option<&str>,
    key: &str,
    agents: Option<&[String]>,
    keep_hub: bool,
) -> Result<Vec<String>, String> {
    let mut args = vec!["mcp".into(), "remove".into(), key.into()];
    args.extend(scope_args(scope, dir)?);
    let agents = agents.filter(|agents| !agents.is_empty());
    match agents {
        Some(agents) => {
            args.push("--agents".into());
            args.push(agents.join(","));
            if keep_hub {
                args.push("--keep-hub".into());
            } else {
                // Deleting the hub needs a confirmation the app already asked
                // for; the CLI refuses an unconfirmed remove off a TTY.
                args.push("--yes".into());
            }
        }
        None => args.push("--yes".into()),
    }
    Ok(args)
}

pub fn clear_provider(
    scope: Scope,
    dir: Option<&str>,
    provider: &str,
) -> Result<Vec<String>, String> {
    let mut args = vec!["provider".into(), "clear".into(), provider.into()];
    args.extend(scope_args(scope, dir)?);
    args.push("--yes".into());
    Ok(args)
}

/// Kind counts, then conflicts. The counts are sorted by size, then by name.
type ParsedPlan = (Vec<(String, usize)>, Vec<Conflict>);

/// Counts action kinds and lists conflicts.
///
/// `sync --json` prints one document. `actions` are the same objects as the
/// numbered text lines, and `conflicts` carries the policy. Install and MCP
/// previews still print that text, so output without an `actions` array uses
/// the line parser.
pub fn parse_plan(output: &str) -> ParsedPlan {
    if let Some(parsed) = parse_json_plan(output) {
        return parsed;
    }
    parse_text_plan(output)
}

/// The toast for a finished command. A successful remove that changed nothing
/// still says so. A contract error on stdout becomes its message and hint.
pub fn outcome_message(ok: bool, text: &str) -> String {
    if ok && text.contains("nothing to remove") {
        "Nothing was removed — see Activity".into()
    } else if ok {
        "Done".into()
    } else {
        failure_message(text)
    }
}

/// Message for a non-zero exit. Contract JSON `{"error":{message,hint}}` is
/// shown as the message, with the hint on the next line. Anything else stays
/// the first line of output.
pub fn failure_message(text: &str) -> String {
    if let Some(message) = contract_failure(text) {
        return message;
    }
    text.lines()
        .find(|line| !line.trim().is_empty())
        .unwrap_or("Failed")
        .trim()
        .to_string()
}

/// `Some` when `text` holds the v1 error document, including a trailing `exit N`.
pub fn contract_failure(text: &str) -> Option<String> {
    let value = json_document(text)?;
    let error = value.get("error")?.as_object()?;
    let message = error
        .get("message")
        .and_then(|item| item.as_str())
        .filter(|message| !message.is_empty())?;
    let hint = error
        .get("hint")
        .and_then(|item| item.as_str())
        .unwrap_or("")
        .trim();
    if hint.is_empty() {
        Some(message.to_string())
    } else {
        Some(format!("{message}\nhint: {hint}"))
    }
}

fn parse_json_plan(output: &str) -> Option<ParsedPlan> {
    let value = json_document(output)?;
    let actions = value.get("actions")?.as_array()?;
    let chips = tally(
        actions
            .iter()
            .filter_map(|action| action.get("kind").and_then(|item| item.as_str())),
    );
    let conflicts = match value.get("conflicts").and_then(|item| item.as_array()) {
        Some(rows) => rows.iter().filter_map(conflict_row).collect(),
        None => conflicts_from_actions(actions),
    };
    Some((chips, conflicts))
}

fn parse_text_plan(output: &str) -> ParsedPlan {
    let mut actions = Vec::new();
    for line in output.lines() {
        let Some(start) = line.find('{') else {
            continue;
        };
        let Some(end) = line.rfind('}') else {
            continue;
        };
        if start >= end {
            continue;
        }
        let Ok(value) = serde_json::from_str::<Value>(&line[start..=end]) else {
            continue;
        };
        if value.get("kind").and_then(|item| item.as_str()).is_some() {
            actions.push(value);
        }
    }
    let chips = tally(
        actions
            .iter()
            .filter_map(|action| action.get("kind").and_then(|item| item.as_str())),
    );
    (chips, conflicts_from_actions(&actions))
}

fn conflicts_from_actions(actions: &[Value]) -> Vec<Conflict> {
    let mut conflicts = Vec::new();
    for action in actions {
        match action.get("kind").and_then(|item| item.as_str()) {
            Some("conflict_skill") => {
                if let Some(key) = action.get("skill_key").and_then(|item| item.as_str()) {
                    conflicts.push(Conflict {
                        skill: true,
                        key: key.into(),
                        policy: Policy::Skip,
                    });
                }
            }
            Some("conflict_mcp") => {
                if let Some(key) = action.get("server").and_then(|item| item.as_str()) {
                    conflicts.push(Conflict {
                        skill: false,
                        key: key.into(),
                        policy: Policy::Skip,
                    });
                }
            }
            _ => {}
        }
    }
    conflicts
}

fn conflict_row(value: &Value) -> Option<Conflict> {
    let key = value
        .get("key")
        .and_then(|item| item.as_str())
        .filter(|key| !key.is_empty())?;
    Some(Conflict {
        skill: value.get("skill").and_then(|item| item.as_bool())?,
        key: key.to_string(),
        policy: policy_from(value.get("policy").and_then(|item| item.as_str())),
    })
}

fn policy_from(value: Option<&str>) -> Policy {
    match value.unwrap_or("skip") {
        "keep-source" => Policy::KeepSource,
        "keep-target" => Policy::KeepTarget,
        _ => Policy::Skip,
    }
}

fn tally<'a>(kinds: impl IntoIterator<Item = &'a str>) -> Vec<(String, usize)> {
    let mut counts: Vec<(String, usize)> = Vec::new();
    for kind in kinds {
        if let Some(slot) = counts.iter_mut().find(|(name, _)| name == kind) {
            slot.1 += 1;
        } else {
            counts.push((kind.to_string(), 1));
        }
    }
    counts.sort_by(|left, right| right.1.cmp(&left.1).then_with(|| left.0.cmp(&right.0)));
    counts
}

/// The first JSON object in CLI output. Succeeds for a pretty document, a
/// one-line document, and that document with `run_tool`'s trailing `exit N`.
fn json_document(text: &str) -> Option<Value> {
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return None;
    }
    if let Ok(value) = serde_json::from_str::<Value>(trimmed) {
        return document_or_none(value);
    }
    if let Some((head, last)) = trimmed.rsplit_once('\n')
        && last.trim_start().starts_with("exit ")
        && let Ok(value) = serde_json::from_str::<Value>(head.trim())
    {
        return document_or_none(value);
    }
    for line in trimmed.lines() {
        let line = line.trim();
        if line.starts_with('{')
            && line.ends_with('}')
            && let Ok(value) = serde_json::from_str::<Value>(line)
            && let Some(value) = document_or_none(value)
        {
            return Some(value);
        }
    }
    let start = trimmed.find('{')?;
    let end = trimmed.rfind('}')?;
    if start >= end {
        return None;
    }
    document_or_none(serde_json::from_str::<Value>(&trimmed[start..=end]).ok()?)
}

fn document_or_none(value: Value) -> Option<Value> {
    if value.get("actions").is_some()
        || value
            .get("error")
            .and_then(|item| item.as_object())
            .is_some()
    {
        Some(value)
    } else {
        None
    }
}

fn normalize_disabled(disabled: &[String]) -> Vec<String> {
    disabled
        .iter()
        .filter(|id| PROVIDERS.iter().any(|(known, _)| *known == id.as_str()))
        .cloned()
        .collect()
}

pub fn projects() -> (Vec<String>, Option<String>) {
    let paths = read_list("synca.projects");
    let active = read_string("synca.projectDir").filter(|dir| paths.iter().any(|path| path == dir));
    (paths, active)
}

pub fn save_projects(paths: &[String], active: Option<&str>) {
    write_list("synca.projects", paths);
    match active {
        Some(active) => write_string("synca.projectDir", active),
        None => delete_key("synca.projectDir"),
    }
}

pub fn disabled_providers() -> Vec<String> {
    normalize_disabled(&read_list("synca.disabledProviders"))
}

pub fn save_disabled(ids: &[String]) {
    write_list("synca.disabledProviders", ids);
}

/// Same keys as the Swift app's `synca.showActivity` and `synca.showSettings`.
pub fn read_flag(key: &str) -> bool {
    matches!(
        read_string(key).as_deref(),
        Some("1") | Some("true") | Some("YES")
    )
}

pub fn write_flag(key: &str, value: bool) {
    let _ = Command::new("defaults")
        .args([
            "write",
            DOMAIN,
            key,
            "-bool",
            if value { "YES" } else { "NO" },
        ])
        .status();
}

pub fn activity_path() -> PathBuf {
    ns_ui::home_dir().join("Library/Application Support/synca/command-log.jsonl")
}

#[derive(Clone, Serialize, Deserialize)]
pub struct LogEntry {
    pub id: i64,
    pub ts: f64,
    pub scope: String,
    pub command: String,
    pub status: String,
    pub exit: i32,
    pub output: String,
}

pub fn activity_page(path: &std::path::Path, limit: usize) -> Vec<LogEntry> {
    let Ok(text) = fs::read_to_string(path) else {
        return Vec::new();
    };
    let mut entries = Vec::new();
    for line in text.lines().rev() {
        if line.trim().is_empty() {
            continue;
        }
        if let Ok(entry) = serde_json::from_str::<LogEntry>(line) {
            entries.push(entry);
        }
        if entries.len() == limit {
            break;
        }
    }
    entries
}

pub fn activity_append(
    path: &std::path::Path,
    scope: &str,
    command: &str,
    status: &str,
    exit: i32,
    output: &str,
) {
    if let Some(parent) = path.parent() {
        let _ = fs::create_dir_all(parent);
    }
    let next = activity_page(path, 1)
        .first()
        .map(|entry| entry.id + 1)
        .unwrap_or(1);
    let entry = LogEntry {
        id: next,
        ts: SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs_f64())
            .unwrap_or(0.0),
        scope: scope.into(),
        command: command.into(),
        status: status.into(),
        exit,
        output: output.into(),
    };
    let Ok(mut line) = serde_json::to_vec(&entry) else {
        return;
    };
    line.push(b'\n');
    if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
        let _ = file.write_all(&line);
    }
}

pub fn activity_clear(path: &std::path::Path) {
    let _ = fs::write(path, "");
}

fn read_list(key: &str) -> Vec<String> {
    let Ok(output) = Command::new("defaults")
        .args(["read", DOMAIN, key])
        .output()
    else {
        return Vec::new();
    };
    if !output.status.success() {
        return Vec::new();
    }
    let text = String::from_utf8_lossy(&output.stdout);
    let mut values = Vec::new();
    for line in text.lines() {
        let line = line.trim().trim_matches(',').trim();
        if let Some(value) = line
            .strip_prefix('"')
            .and_then(|rest| rest.strip_suffix('"'))
        {
            values.push(value.to_string());
        }
    }
    values
}

fn read_string(key: &str) -> Option<String> {
    let output = Command::new("defaults")
        .args(["read", DOMAIN, key])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    let text = String::from_utf8_lossy(&output.stdout)
        .trim()
        .trim_matches('"')
        .to_string();
    if text.is_empty() { None } else { Some(text) }
}

fn write_list(key: &str, values: &[String]) {
    let mut args = vec!["write".into(), DOMAIN.into(), key.into(), "-array".into()];
    args.extend(values.iter().cloned());
    let _ = Command::new("defaults").args(args).status();
}

fn write_string(key: &str, value: &str) {
    let _ = Command::new("defaults")
        .args(["write", DOMAIN, key, "-string", value])
        .status();
}

fn delete_key(key: &str) {
    let _ = Command::new("defaults")
        .args(["delete", DOMAIN, key])
        .status();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn plan_and_apply_match_the_cli_contract() {
        let plan = sync_plan(
            Scope::Project,
            Some("/work"),
            Target::Skills,
            Some("accessibility-compliance"),
        )
        .unwrap();
        assert_eq!(
            plan,
            [
                "sync",
                "skills",
                "--scope",
                "project",
                "--cwd",
                "/work",
                "--dry-run",
                "--key",
                "accessibility-compliance",
                "--json"
            ]
        );
        assert!(sync_plan(Scope::Project, None, Target::All, None).is_err());
        let apply = with_agents(
            sync_apply(Scope::User, None, Target::All, Some("ignored")).unwrap(),
            &["cursor".into()],
        );
        assert!(apply.iter().any(|arg| arg == "--on-conflict"));
        assert!(apply.iter().any(|arg| arg == "--agents"));
        assert!(apply.iter().any(|arg| arg == "--json"));
        assert!(!apply.iter().any(|arg| arg == "--key"));
        assert!(!apply.iter().any(|arg| arg == "--yes"));
        let (chips, conflicts) = parse_plan(
            "1. {\"kind\":\"conflict_skill\",\"skill_key\":\"demo\"}\n2. {\"kind\":\"link\"}\n",
        );
        assert_eq!(conflicts.len(), 1);
        assert_eq!(conflicts[0].key, "demo");
        assert!(conflicts[0].policy == Policy::Skip);
        assert!(chips.iter().any(|(kind, _)| kind == "link"));
    }

    #[test]
    fn json_plan_fixture_matches_the_sync_contract() {
        let (chips, conflicts) = parse_plan(include_str!("../fixtures/sync-plan.json"));
        assert_eq!(
            chips,
            [
                ("conflict_mcp".to_string(), 1),
                ("conflict_skill".to_string(), 1),
                ("symlink_skill".to_string(), 1),
            ]
        );
        assert_eq!(conflicts.len(), 2);
        assert!(conflicts[0].skill);
        assert_eq!(conflicts[0].key, "beta");
        assert!(conflicts[0].policy == Policy::Skip);
        assert!(!conflicts[1].skill);
        assert_eq!(conflicts[1].key, "demo");
        assert!(conflicts[1].policy == Policy::Skip);

        let applied = r#"{"dryRun":false,"scope":"user","onConflict":"keep-target","actions":[{"kind":"symlink_skill","skill_key":"alpha"},{"kind":"symlink_skill","skill_key":"gamma"},{"kind":"conflict_skill","skill_key":"beta"}],"conflicts":[{"skill":true,"key":"beta","policy":"keep-target"}],"applied":["kept target"]}"#;
        let (chips, conflicts) = parse_plan(applied);
        assert_eq!(chips[0], ("symlink_skill".to_string(), 2));
        assert_eq!(conflicts.len(), 1);
        assert!(conflicts[0].policy == Policy::KeepTarget);
    }

    #[test]
    fn confirmed_commands_pass_yes_and_json() {
        let remove = remove_mcp(Scope::User, None, "demo", None, false).unwrap();
        assert_eq!(
            remove,
            ["mcp", "remove", "demo", "--scope", "user", "--yes"]
        );
        let agents = vec!["cursor".to_string(), "grok".to_string()];
        let unlink_mcp = remove_mcp(Scope::User, None, "demo", Some(&agents), true).unwrap();
        assert_eq!(
            unlink_mcp,
            [
                "mcp",
                "remove",
                "demo",
                "--scope",
                "user",
                "--agents",
                "cursor,grok",
                "--keep-hub"
            ]
        );
        assert!(!unlink_mcp.iter().any(|arg| arg == "--yes"));
        let hub_gone = remove_mcp(Scope::User, None, "demo", Some(&agents), false).unwrap();
        assert_eq!(
            hub_gone,
            [
                "mcp",
                "remove",
                "demo",
                "--scope",
                "user",
                "--agents",
                "cursor,grok",
                "--yes"
            ]
        );
        let purge = remove_skill(Scope::Project, Some("/work"), "demo", true, None).unwrap();
        assert!(
            purge
                .windows(2)
                .any(|pair| pair[0] == "--purge" && pair[1] == "--yes")
        );
        let unlink = remove_skill(Scope::User, None, "demo", false, None).unwrap();
        assert!(!unlink.iter().any(|arg| arg == "--yes"));
        let unlink_one = remove_skill(Scope::User, None, "demo", false, Some(&agents)).unwrap();
        assert_eq!(
            unlink_one,
            [
                "skills",
                "remove",
                "demo",
                "--scope",
                "user",
                "--agents",
                "cursor,grok"
            ]
        );
        let clear = clear_provider(Scope::User, None, "claude").unwrap();
        assert!(clear.iter().any(|arg| arg == "--yes"));

        let overwrite = resolve(
            Scope::User,
            None,
            &Conflict {
                skill: false,
                key: "demo".into(),
                policy: Policy::KeepSource,
            },
        )
        .unwrap();
        assert_eq!(
            overwrite,
            [
                "sync",
                "mcp",
                "--scope",
                "user",
                "--on-conflict",
                "keep-source",
                "--key",
                "demo",
                "--yes",
                "--json"
            ]
        );
        let target = resolve(
            Scope::User,
            None,
            &Conflict {
                skill: true,
                key: "demo".into(),
                policy: Policy::KeepTarget,
            },
        )
        .unwrap();
        assert!(target.iter().any(|arg| arg == "--yes"));
        assert!(target.iter().any(|arg| arg == "--json"));
        let skip = resolve(
            Scope::User,
            None,
            &Conflict {
                skill: true,
                key: "demo".into(),
                policy: Policy::Skip,
            },
        )
        .unwrap();
        assert!(!skip.iter().any(|arg| arg == "--yes"));
        assert!(skip.iter().any(|arg| arg == "--json"));
    }

    #[test]
    fn retired_pi_and_omp_are_ignored() {
        assert!(!PROVIDERS.iter().any(|(id, label)| {
            *id == "pi" || *id == "omp" || *label == "Pi" || *label == "OMP"
        }));
        assert!(retired_agent("pi") && retired_agent("OMP"));
        assert!(!retired_agent("grok"));
        let all_on = with_agents(
            vec!["sync".into(), "skills".into()],
            &["pi".into(), "omp".into()],
        );
        assert_eq!(all_on, ["sync", "skills"]);
        let args = with_agents(
            vec!["sync".into()],
            &["cursor".into(), "pi".into(), "omp".into(), "nope".into()],
        );
        let agents = args.iter().position(|arg| arg == "--agents").unwrap();
        assert_eq!(args[agents + 1], "grok,devin,kiro,opencode,claude");

        let explicit = with_agents(
            vec![
                "skills".into(),
                "remove".into(),
                "demo".into(),
                "--agents".into(),
                "cursor".into(),
            ],
            &["grok".into()],
        );
        assert_eq!(explicit.iter().filter(|arg| *arg == "--agents").count(), 1);
        assert_eq!(explicit.last().map(String::as_str), Some("cursor"));
    }

    #[test]
    fn contract_error_json_is_a_message_and_hint() {
        let pretty = include_str!("../fixtures/error.json");
        assert_eq!(
            failure_message(pretty),
            "unknown command \"nope\"\nhint: see: synca schema"
        );
        let with_exit = format!("{}\nexit 2", pretty.trim_end());
        assert_eq!(failure_message(&with_exit), failure_message(pretty));
        let line = "{\"error\":{\"code\":\"unavailable\",\"exit\":3,\"message\":\"no MCP server named missing\"}}\nexit 3";
        assert_eq!(failure_message(line), "no MCP server named missing");
        assert!(contract_failure(line).is_some());
        assert!(!failure_message(line).starts_with('{'));
        assert_eq!(
            outcome_message(true, "  note: nothing to remove for skill 'demo'\n"),
            "Nothing was removed — see Activity"
        );
        assert_eq!(outcome_message(true, "unlinked demo\n"), "Done");
        let failed = "{\"error\":{\"code\":\"failed\",\"exit\":5,\"message\":\"nothing to remove exploded\",\"hint\":\"pass --yes\"}}\nexit 5";
        assert_eq!(
            outcome_message(false, failed),
            "nothing to remove exploded\nhint: pass --yes"
        );
        assert_eq!(
            failure_message("synca failed: choose a folder\nexit 2"),
            "synca failed: choose a folder"
        );
    }

    #[test]
    fn update_report_reads_the_current_json() {
        let report = ns_ui::parse_update(
            "synca",
            r#"{"current":"0.2.5","latest":"0.2.6","updateAvailable":true,"asset":"synca-v0.2.6-darwin-arm64","installPath":"/tmp/synca"}"#,
        );
        assert!(report.available);
        assert!(report.text.contains("0.2.6"));
        assert!(report.url.contains("v0.2.6"));
        let same = ns_ui::parse_update(
            "synca",
            r#"{"current":"0.2.6","latest":"0.2.6","updateAvailable":false}"#,
        );
        assert!(!same.available);
        assert!(same.text.contains("up to date"));
    }

    #[test]
    fn activity_round_trip_uses_a_temp_file() {
        let path = std::env::temp_dir().join(format!("synca-log-{}", std::process::id()));
        let _ = fs::remove_file(&path);
        activity_append(&path, "user", "synca skills list", "ok", 0, "done");
        let page = activity_page(&path, 10);
        assert_eq!(page.len(), 1);
        assert_eq!(page[0].command, "synca skills list");
        activity_clear(&path);
        assert!(activity_page(&path, 10).is_empty());
        let _ = fs::remove_file(&path);
    }
}
