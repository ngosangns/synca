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

/// Where a skill came from, as `skills list --json` reports it. An old binary
/// omits the field, and a skill that was never recorded is local.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum Origin {
    Remote,
    #[default]
    Local,
}

impl Origin {
    pub fn from_field(value: Option<&str>) -> Self {
        match value {
            Some(value) if value.trim().eq_ignore_ascii_case("remote") => Self::Remote,
            _ => Self::Local,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Self::Remote => "remote",
            Self::Local => "local",
        }
    }
}

/// Tooltip text for the origin badge.
pub fn origin_tip(origin: Origin, source: Option<&str>, source_type: Option<&str>) -> String {
    match origin {
        Origin::Local => "Installed from a local folder".into(),
        Origin::Remote => {
            let source = source.map(str::trim).filter(|text| !text.is_empty());
            let kind = source_type.map(str::trim).filter(|text| !text.is_empty());
            match (source, kind) {
                (Some(source), Some(kind)) => format!("Installed from {source} ({kind})"),
                (Some(source), None) => format!("Installed from {source}"),
                (None, Some(kind)) => format!("Installed from a remote source ({kind})"),
                (None, None) => "Installed from a remote source".into(),
            }
        }
    }
}

/// The skills.sh views the Registry tab can show.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum RegistryView {
    Search,
    Trending,
    Hot,
    AllTime,
    Official,
    /// The skills one repository holds (`skills discover`). It has no button of
    /// its own; a result or a typed `owner/repo` opens it.
    Repo,
}

impl RegistryView {
    pub const ALL: [RegistryView; 5] = [
        Self::Search,
        Self::Trending,
        Self::Hot,
        Self::AllTime,
        Self::Official,
    ];

    pub fn key(self) -> &'static str {
        match self {
            Self::Search => "search",
            Self::Trending => "trending",
            Self::Hot => "hot",
            Self::AllTime => "all-time",
            Self::Official => "official",
            Self::Repo => "repo",
        }
    }

    #[cfg(any(feature = "devtools", test))]
    pub fn from_key(key: &str) -> Option<Self> {
        Self::ALL
            .into_iter()
            .chain([Self::Repo])
            .find(|view| view.key() == key)
    }

    /// The `--view` value of `skills browse`; Search and Official have none.
    pub fn browse_name(self) -> Option<&'static str> {
        match self {
            Self::Trending => Some("trending"),
            Self::Hot => Some("hot"),
            Self::AllTime => Some("all-time"),
            Self::Search | Self::Official | Self::Repo => None,
        }
    }
}

/// True for `owner/repo`, the shape `skills discover` takes from the search box.
pub fn looks_like_repo(text: &str) -> bool {
    let text = text.trim();
    let mut parts = text.split('/');
    let (Some(owner), Some(repo), None) = (parts.next(), parts.next(), parts.next()) else {
        return false;
    };
    let segment = |part: &str| {
        !part.is_empty()
            && !part.starts_with('-')
            && part
                .chars()
                .all(|ch| ch.is_ascii_alphanumeric() || matches!(ch, '.' | '_' | '-'))
    };
    segment(owner) && segment(repo)
}

/// The registry-shaped rows for a `skills discover` answer, so the Registry
/// list and detail can show them like any other result.
pub fn discovered_page(source: &str, found: Vec<DiscoveredSkill>) -> RegistryPage {
    let skills: Vec<RegistrySkill> = found
        .into_iter()
        .map(|skill| RegistrySkill {
            id: format!("{source}/{}", skill.slug),
            source: source.to_string(),
            name: skill.name,
            url: format!("https://skills.sh/{source}/{}", skill.slug),
            source_type: "github".into(),
            slug: skill.slug,
            ..RegistrySkill::default()
        })
        .collect();
    RegistryPage {
        total: skills.len(),
        skills,
        has_more: false,
        page: 0,
    }
}

/// A search needs at least this many characters (the CLI exits 2 below it).
pub const SEARCH_MIN_CHARS: usize = 2;
/// Results asked for in one search.
pub const SEARCH_LIMIT: usize = 50;

/// A value that goes on the command line as a positional argument. The CLI
/// reads `--x` as a flag, so a leading dash is refused.
fn positional(value: &str, what: &str) -> Result<String, String> {
    let value = value.trim();
    if value.is_empty() {
        return Err(format!("Missing {what}."));
    }
    if value.starts_with('-') {
        return Err(format!("The {what} cannot start with a dash."));
    }
    Ok(value.to_string())
}

/// `skills search <query> --limit N --json`. Leading dashes are dropped so a
/// query can never be read as a flag.
pub fn registry_search_args(query: &str, limit: usize) -> Result<Vec<String>, String> {
    let query = query.trim().trim_start_matches('-').trim();
    if query.chars().count() < SEARCH_MIN_CHARS {
        return Err(format!(
            "Type at least {SEARCH_MIN_CHARS} characters to search."
        ));
    }
    Ok(vec![
        "skills".into(),
        "search".into(),
        query.into(),
        "--limit".into(),
        limit.to_string(),
        "--json".into(),
    ])
}

/// `skills browse --view V --page N --json`. Pages count from 0 and hold 200
/// items; the CLI has no page-size flag.
pub fn registry_browse_args(view: RegistryView, page: usize) -> Result<Vec<String>, String> {
    let name = view
        .browse_name()
        .ok_or_else(|| "This view is not a browse list.".to_string())?;
    Ok(vec![
        "skills".into(),
        "browse".into(),
        "--view".into(),
        name.into(),
        "--page".into(),
        page.to_string(),
        "--json".into(),
    ])
}

pub fn registry_official_args() -> Vec<String> {
    vec!["skills".into(), "official".into(), "--json".into()]
}

/// `skills info <id> [--contents] --json`.
pub fn registry_info_args(id: &str, contents: bool) -> Result<Vec<String>, String> {
    let mut args = vec!["skills".into(), "info".into(), positional(id, "skill id")?];
    if contents {
        args.push("--contents".into());
    }
    args.push("--json".into());
    Ok(args)
}

pub fn registry_audit_args(id: &str) -> Result<Vec<String>, String> {
    Ok(vec![
        "skills".into(),
        "audit".into(),
        positional(id, "skill id")?,
        "--json".into(),
    ])
}

/// `skills use <source> --skill <slug> --json`: the prompt a user pastes into an agent.
pub fn registry_use_args(source: &str, slug: &str) -> Result<Vec<String>, String> {
    Ok(vec![
        "skills".into(),
        "use".into(),
        positional(source, "source")?,
        "--skill".into(),
        positional(slug, "skill name")?,
        "--json".into(),
    ])
}

/// `skills discover <source> --json`: the skills a repository holds.
pub fn registry_discover_args(source: &str) -> Result<Vec<String>, String> {
    Ok(vec![
        "skills".into(),
        "discover".into(),
        positional(source, "source")?,
        "--json".into(),
    ])
}

/// `owner/repo@slug`, the form `skills install` takes for one skill of a repository.
pub fn source_at(source: &str, slug: &str) -> Result<String, String> {
    let source = positional(source, "source")?;
    let slug = positional(slug, "skill name")?;
    Ok(format!("{source}@{slug}"))
}

/// The `<source>@<skill>` form of `skills install`. A well-known site has no
/// install path yet.
pub fn registry_install_source(skill: &RegistrySkill) -> Result<String, String> {
    if skill.source_type == "well-known" {
        return Err(format!(
            "Installing from a well-known site ({}) is not supported yet.",
            skill.source
        ));
    }
    source_at(&skill.source, &skill.slug)
}

/// `skills install <source> … --json`, the registry install that prints a manage plan.
pub fn registry_install_args(
    scope: Scope,
    dir: Option<&str>,
    source: &str,
    dry_run: bool,
) -> Result<Vec<String>, String> {
    let mut args = install_args(scope, dir, source, dry_run)?;
    args.push("--json".into());
    Ok(args)
}

/// `skills update [names…]`. With `dry_run` it is the check for updates;
/// without it the CLI applies at once.
pub fn update_args(
    scope: Scope,
    dir: Option<&str>,
    names: &[String],
    dry_run: bool,
) -> Result<Vec<String>, String> {
    let mut args = vec!["skills".into(), "update".into()];
    for name in names.iter().filter(|name| !name.is_empty()) {
        args.push(positional(name, "skill name")?);
    }
    args.extend(scope_args(scope, dir)?);
    if dry_run {
        args.push("--dry-run".into());
    }
    args.push("--json".into());
    Ok(args)
}

/// `skills restore`: reinstall every remote skill the lock file records.
pub fn restore_args(scope: Scope, dir: Option<&str>, dry_run: bool) -> Result<Vec<String>, String> {
    let mut args = vec!["skills".into(), "restore".into()];
    args.extend(scope_args(scope, dir)?);
    if dry_run {
        args.push("--dry-run".into());
    }
    args.push("--json".into());
    Ok(args)
}

/// `providers list [--scope S [--cwd D]] --json`: every agent the skills.sh
/// CLI can install to, with the folder it uses in the requested scope.
pub fn providers_args(scope: Scope, dir: Option<&str>) -> Result<Vec<String>, String> {
    let mut args = vec!["providers".into(), "list".into()];
    args.extend(scope_args(scope, dir)?);
    args.push("--json".into());
    Ok(args)
}

/// One agent of the skills.sh `agents` table. Every field is optional on the
/// wire, so a newer or older CLI still parses.
#[derive(Clone, Debug, PartialEq, Default, Deserialize)]
#[serde(default)]
pub struct ProviderInfo {
    pub id: String,
    pub name: String,
    pub project_dir: Option<String>,
    pub global_dir: Option<String>,
    /// The folder for the requested scope; `None` when it is unknown.
    pub path: Option<String>,
    /// Reads the shared `.agents/skills` folder.
    pub universal: bool,
    /// Synca syncs to this agent.
    pub managed: bool,
    /// The id Synca uses in `--agents` when `managed`.
    pub synca_agent: Option<String>,
    /// `path` exists on disk.
    pub present: bool,
    pub skills: u64,
}

impl ProviderInfo {
    /// What to print in the row: the resolved folder, else the folder
    /// skills.sh documents for the scope (`project` picks the project folder,
    /// which is relative to a project root).
    pub fn location(&self, project: bool) -> Option<&str> {
        let documented = if project {
            self.project_dir.as_deref()
        } else {
            self.global_dir.as_deref()
        };
        [self.path.as_deref(), documented]
            .into_iter()
            .flatten()
            .find(|text| !text.trim().is_empty())
    }

    /// The label shown to the user: the display name, else the id.
    pub fn title(&self) -> &str {
        if self.name.trim().is_empty() {
            &self.id
        } else {
            &self.name
        }
    }

    /// Case-insensitive match of `needle` (already lower case) on name or id.
    pub fn matches(&self, needle: &str) -> bool {
        needle.is_empty()
            || self.name.to_lowercase().contains(needle)
            || self.id.to_lowercase().contains(needle)
    }

    fn rank(&self) -> u8 {
        if self.managed {
            0
        } else if self.present {
            1
        } else {
            2
        }
    }
}

/// `providers list --json`, in display order: managed agents first, then the
/// installed ones, then the rest, each group by name.
pub fn parse_providers(text: &str) -> Result<Vec<ProviderInfo>, String> {
    let value = document(text)?;
    let items = value
        .get("providers")
        .and_then(|item| item.as_array())
        .ok_or_else(|| "synca returned no provider list.".to_string())?;
    let mut providers: Vec<ProviderInfo> = items
        .iter()
        .filter_map(|item| serde_json::from_value::<ProviderInfo>(item.clone()).ok())
        .filter(|provider| !provider.id.trim().is_empty())
        .collect();
    providers.sort_by_cached_key(|provider| {
        (
            provider.rank(),
            provider.title().to_lowercase(),
            provider.id.clone(),
        )
    });
    Ok(providers)
}

/// "79 providers, 12 installed, 6 managed by Synca".
pub fn providers_summary(providers: &[ProviderInfo]) -> String {
    let installed = providers.iter().filter(|item| item.present).count();
    let managed = providers.iter().filter(|item| item.managed).count();
    let noun = if providers.len() == 1 {
        "provider"
    } else {
        "providers"
    };
    format!(
        "{} {noun}, {installed} installed, {managed} managed by Synca",
        providers.len()
    )
}

/// Shown when the installed `synca` has no `providers` command.
pub const PROVIDERS_OUTDATED: &str = "Update the synca CLI to list all providers";

/// The text shown when `providers list` fails. An older binary has no such
/// command and answers with a usage error.
pub fn providers_failure(message: &str) -> String {
    if message.contains("unknown command") {
        PROVIDERS_OUTDATED.into()
    } else {
        message.to_string()
    }
}

/// One skill as the skills.sh registry lists it.
#[derive(Clone, Debug, PartialEq, Default)]
pub struct RegistrySkill {
    pub id: String,
    pub source: String,
    pub slug: String,
    pub name: String,
    pub installs: u64,
    pub source_type: String,
    pub install_url: Option<String>,
    pub url: String,
    pub official: bool,
    pub duplicate: bool,
    pub weekly_installs: Vec<u64>,
    pub installs_yesterday: Option<u64>,
    pub change: Option<i64>,
}

/// One page of search or browse results.
#[derive(Clone, Debug, PartialEq, Default)]
pub struct RegistryPage {
    pub skills: Vec<RegistrySkill>,
    pub total: usize,
    pub has_more: bool,
    pub page: usize,
}

#[derive(Clone, Debug, PartialEq)]
pub struct OfficialOwner {
    pub owner: String,
    pub total_installs: u64,
    pub skills: Vec<RegistrySkill>,
}

#[derive(Clone, Debug, PartialEq, Default)]
pub struct OfficialList {
    pub owners: Vec<OfficialOwner>,
    pub total_owners: usize,
    pub total_skills: usize,
}

/// A row of the Registry list: an owner heading (Official view) or a skill.
#[derive(Clone, Debug, PartialEq)]
pub enum RegistryRow {
    Owner {
        name: String,
        installs: u64,
        skills: usize,
    },
    Skill(usize),
}

#[derive(Clone, Debug, PartialEq)]
pub struct RegistryFile {
    pub path: String,
    pub size: u64,
    pub contents: Option<String>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct RegistryInfo {
    pub installs: Option<u64>,
    pub hash: Option<String>,
    pub files: Vec<RegistryFile>,
}

impl RegistryInfo {
    /// The `SKILL.md` snapshot, if the registry has one.
    pub fn skill_md(&self) -> Option<&RegistryFile> {
        self.files
            .iter()
            .find(|file| file.path.eq_ignore_ascii_case("SKILL.md"))
            .or_else(|| {
                self.files
                    .iter()
                    .find(|file| file.path.to_ascii_lowercase().ends_with("/skill.md"))
            })
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AuditStatus {
    Pass,
    Warn,
    Fail,
    Unknown,
}

impl AuditStatus {
    pub fn label(self) -> &'static str {
        match self {
            Self::Pass => "pass",
            Self::Warn => "warn",
            Self::Fail => "fail",
            Self::Unknown => "unknown",
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct AuditRow {
    pub provider: String,
    pub status: AuditStatus,
    pub summary: String,
    pub audited_at: String,
    pub risk_level: Option<String>,
    pub categories: Vec<String>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct DiscoveredSkill {
    pub name: String,
    pub description: String,
    pub path: String,
    pub slug: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct UsePrompt {
    pub name: String,
    pub prompt: String,
}

/// The JSON document of a successful call. A contract error document becomes
/// its message and hint, and anything else that is not JSON a plain message.
fn document(text: &str) -> Result<Value, String> {
    if let Some(message) = contract_failure(text) {
        return Err(message);
    }
    serde_json::from_str::<Value>(text.trim())
        .map_err(|_| "synca printed something that is not JSON.".to_string())
}

fn field_text(value: &Value, key: &str) -> String {
    value
        .get(key)
        .and_then(|item| item.as_str())
        .unwrap_or("")
        .trim()
        .to_string()
}

fn field_opt(value: &Value, key: &str) -> Option<String> {
    Some(field_text(value, key)).filter(|text| !text.is_empty())
}

fn as_count(item: &Value) -> Option<u64> {
    item.as_u64()
        .or_else(|| item.as_f64().map(|number| number.max(0.) as u64))
}

fn field_count(value: &Value, key: &str) -> Option<u64> {
    as_count(value.get(key)?)
}

fn registry_skill(value: &Value) -> Option<RegistrySkill> {
    let mut source = field_text(value, "source");
    let mut slug = field_text(value, "slug");
    let id = field_opt(value, "id")
        .or_else(|| (!source.is_empty() && !slug.is_empty()).then(|| format!("{source}/{slug}")))?;
    if slug.is_empty() {
        slug = id.rsplit('/').next().unwrap_or("").to_string();
    }
    if source.is_empty() {
        source = id
            .rsplit_once('/')
            .map(|(head, _)| head.to_string())
            .unwrap_or_default();
    }
    let name = field_opt(value, "name").unwrap_or_else(|| slug.clone());
    let url = field_opt(value, "url").unwrap_or_else(|| format!("https://skills.sh/{id}"));
    Some(RegistrySkill {
        name,
        installs: field_count(value, "installs").unwrap_or(0),
        source_type: field_opt(value, "sourceType").unwrap_or_else(|| "github".into()),
        install_url: field_opt(value, "installUrl"),
        url,
        official: value
            .get("official")
            .and_then(|item| item.as_bool())
            .unwrap_or(false),
        duplicate: value
            .get("duplicate")
            .and_then(|item| item.as_bool())
            .unwrap_or(false),
        weekly_installs: value
            .get("weeklyInstalls")
            .and_then(|item| item.as_array())
            .map(|items| {
                items
                    .iter()
                    .map(|item| as_count(item).unwrap_or(0))
                    .collect()
            })
            .unwrap_or_default(),
        installs_yesterday: field_count(value, "installsYesterday"),
        change: value.get("change").and_then(|item| item.as_i64()),
        id,
        source,
        slug,
    })
}

fn registry_skills(value: Option<&Value>) -> Vec<RegistrySkill> {
    value
        .and_then(|item| item.as_array())
        .map(|items| items.iter().filter_map(registry_skill).collect())
        .unwrap_or_default()
}

/// `skills search --json`.
pub fn parse_search(text: &str) -> Result<RegistryPage, String> {
    let value = document(text)?;
    let skills = registry_skills(value.get("skills"));
    let total = field_count(&value, "count").unwrap_or(skills.len() as u64) as usize;
    Ok(RegistryPage {
        skills,
        total,
        has_more: false,
        page: 0,
    })
}

/// `skills browse --json`.
pub fn parse_browse(text: &str) -> Result<RegistryPage, String> {
    let value = document(text)?;
    let skills = registry_skills(value.get("skills"));
    let total = field_count(&value, "total").unwrap_or(skills.len() as u64) as usize;
    Ok(RegistryPage {
        has_more: value
            .get("hasMore")
            .and_then(|item| item.as_bool())
            .unwrap_or(false),
        page: field_count(&value, "page").unwrap_or(0) as usize,
        skills,
        total,
    })
}

/// `skills official --json`.
pub fn parse_official(text: &str) -> Result<OfficialList, String> {
    let value = document(text)?;
    let owners: Vec<OfficialOwner> = value
        .get("owners")
        .and_then(|item| item.as_array())
        .map(|items| {
            items
                .iter()
                .filter_map(|item| {
                    Some(OfficialOwner {
                        owner: field_opt(item, "owner")?,
                        total_installs: field_count(item, "totalInstalls").unwrap_or(0),
                        skills: registry_skills(item.get("skills")),
                    })
                })
                .collect()
        })
        .unwrap_or_default();
    let skills: usize = owners.iter().map(|owner| owner.skills.len()).sum();
    Ok(OfficialList {
        total_owners: field_count(&value, "totalOwners").unwrap_or(owners.len() as u64) as usize,
        total_skills: field_count(&value, "totalSkills").unwrap_or(skills as u64) as usize,
        owners,
    })
}

/// Flat skill list plus the rows that draw it, one owner heading before each group.
pub fn official_rows(list: &OfficialList) -> (Vec<RegistrySkill>, Vec<RegistryRow>) {
    let mut skills = Vec::new();
    let mut rows = Vec::new();
    for owner in &list.owners {
        rows.push(RegistryRow::Owner {
            name: owner.owner.clone(),
            installs: owner.total_installs,
            skills: owner.skills.len(),
        });
        for skill in &owner.skills {
            rows.push(RegistryRow::Skill(skills.len()));
            skills.push(skill.clone());
        }
    }
    (skills, rows)
}

/// `skills info --json`.
pub fn parse_info(text: &str) -> Result<RegistryInfo, String> {
    let value = document(text)?;
    let files = value
        .get("files")
        .and_then(|item| item.as_array())
        .map(|items| {
            items
                .iter()
                .filter_map(|item| {
                    Some(RegistryFile {
                        path: field_opt(item, "path")?,
                        size: field_count(item, "size").unwrap_or(0),
                        contents: item
                            .get("contents")
                            .and_then(|contents| contents.as_str())
                            .map(str::to_string),
                    })
                })
                .collect()
        })
        .unwrap_or_default();
    Ok(RegistryInfo {
        installs: field_count(&value, "installs"),
        hash: field_opt(&value, "hash"),
        files,
    })
}

/// `skills audit --json`. An empty list means the registry has no audits.
pub fn parse_audit(text: &str) -> Result<Vec<AuditRow>, String> {
    let value = document(text)?;
    Ok(value
        .get("audits")
        .and_then(|item| item.as_array())
        .map(|items| {
            items
                .iter()
                .map(|item| AuditRow {
                    provider: field_opt(item, "provider")
                        .or_else(|| field_opt(item, "slug"))
                        .unwrap_or_else(|| "unknown".into()),
                    status: match field_text(item, "status").to_lowercase().as_str() {
                        "pass" => AuditStatus::Pass,
                        "warn" => AuditStatus::Warn,
                        "fail" => AuditStatus::Fail,
                        _ => AuditStatus::Unknown,
                    },
                    summary: field_text(item, "summary"),
                    audited_at: field_text(item, "auditedAt"),
                    risk_level: field_opt(item, "riskLevel"),
                    categories: item
                        .get("categories")
                        .and_then(|list| list.as_array())
                        .map(|list| {
                            list.iter()
                                .filter_map(|entry| entry.as_str().map(str::to_string))
                                .collect()
                        })
                        .unwrap_or_default(),
                })
                .collect()
        })
        .unwrap_or_default())
}

/// `skills use --json`.
pub fn parse_use(text: &str) -> Result<UsePrompt, String> {
    let value = document(text)?;
    let prompt = value
        .get("prompt")
        .and_then(|item| item.as_str())
        .filter(|prompt| !prompt.trim().is_empty())
        .ok_or_else(|| "synca returned no prompt.".to_string())?;
    Ok(UsePrompt {
        name: field_text(&value, "name"),
        prompt: prompt.to_string(),
    })
}

/// `skills discover --json`.
pub fn parse_discover(text: &str) -> Result<Vec<DiscoveredSkill>, String> {
    let value = document(text)?;
    Ok(value
        .get("skills")
        .and_then(|item| item.as_array())
        .map(|items| {
            items
                .iter()
                .filter_map(|item| {
                    let slug = field_opt(item, "slug").or_else(|| field_opt(item, "name"))?;
                    Some(DiscoveredSkill {
                        name: field_opt(item, "name").unwrap_or_else(|| slug.clone()),
                        description: field_text(item, "description"),
                        path: field_text(item, "path"),
                        slug,
                    })
                })
                .collect()
        })
        .unwrap_or_default())
}

/// The `notes` lines of a manage plan, such as `up to date: x`.
pub fn plan_notes(output: &str) -> Vec<String> {
    json_document(output)
        .and_then(|value| {
            value
                .get("notes")
                .and_then(|item| item.as_array())
                .map(|notes| {
                    notes
                        .iter()
                        .filter_map(|note| note.as_str())
                        .map(|note| note.trim().to_string())
                        .filter(|note| !note.is_empty())
                        .collect()
                })
        })
        .unwrap_or_default()
}

/// `4082959` -> `4.1M`, `1200` -> `1.2k`, `950` -> `950`.
pub fn format_installs(count: u64) -> String {
    let (value, unit) = if count < 1_000 {
        return count.to_string();
    } else if count < 1_000_000 {
        (count as f64 / 1_000., "k")
    } else {
        (count as f64 / 1_000_000., "M")
    };
    let text = format!("{value:.1}");
    format!("{}{unit}", text.strip_suffix(".0").unwrap_or(&text))
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

    fn skill(source: &str, slug: &str, kind: &str) -> RegistrySkill {
        RegistrySkill {
            id: format!("{source}/{slug}"),
            source: source.into(),
            slug: slug.into(),
            name: slug.into(),
            source_type: kind.into(),
            ..RegistrySkill::default()
        }
    }

    #[test]
    fn origin_defaults_to_local_and_explains_itself() {
        assert_eq!(Origin::from_field(None), Origin::Local);
        assert_eq!(Origin::from_field(Some("local")), Origin::Local);
        assert_eq!(Origin::from_field(Some("surprise")), Origin::Local);
        assert_eq!(Origin::from_field(Some("remote")), Origin::Remote);
        assert_eq!(Origin::Remote.label(), "remote");
        assert_eq!(Origin::Local.label(), "local");
        assert_eq!(
            origin_tip(Origin::Remote, Some("vercel-labs/skills"), Some("github")),
            "Installed from vercel-labs/skills (github)"
        );
        assert_eq!(
            origin_tip(Origin::Remote, None, None),
            "Installed from a remote source"
        );
        assert_eq!(
            origin_tip(Origin::Local, Some("ignored"), None),
            "Installed from a local folder"
        );
    }

    #[test]
    fn registry_args_follow_the_cli_contract() {
        assert_eq!(
            registry_search_args("  react  ", 50).unwrap(),
            ["skills", "search", "react", "--limit", "50", "--json"]
        );
        assert_eq!(
            registry_search_args("--owner x", 10).unwrap()[2],
            "owner x",
            "a leading dash never reaches the CLI as a flag"
        );
        assert!(registry_search_args("a", 10).is_err());
        assert!(registry_search_args("  ", 10).is_err());
        assert_eq!(
            registry_browse_args(RegistryView::AllTime, 2).unwrap(),
            [
                "skills", "browse", "--view", "all-time", "--page", "2", "--json"
            ]
        );
        assert!(
            !registry_browse_args(RegistryView::Hot, 0)
                .unwrap()
                .iter()
                .any(|arg| arg == "--per-page" || arg == "--source")
        );
        assert!(registry_browse_args(RegistryView::Search, 0).is_err());
        assert!(registry_browse_args(RegistryView::Official, 0).is_err());
        assert_eq!(registry_official_args(), ["skills", "official", "--json"]);
        assert_eq!(
            registry_info_args("o/r/s", true).unwrap(),
            ["skills", "info", "o/r/s", "--contents", "--json"]
        );
        assert_eq!(
            registry_info_args("o/r/s", false).unwrap(),
            ["skills", "info", "o/r/s", "--json"]
        );
        assert!(registry_info_args("-x", false).is_err());
        assert_eq!(
            registry_audit_args("o/r/s").unwrap(),
            ["skills", "audit", "o/r/s", "--json"]
        );
        assert_eq!(
            registry_use_args("o/r", "s").unwrap(),
            ["skills", "use", "o/r", "--skill", "s", "--json"]
        );
        assert_eq!(
            registry_discover_args("vercel-labs/skills").unwrap(),
            ["skills", "discover", "vercel-labs/skills", "--json"]
        );
        assert!(registry_discover_args("--x").is_err());
        assert!(registry_use_args("o/r", "").is_err());
    }

    #[test]
    fn registry_install_uses_source_at_slug_and_refuses_well_known() {
        let github = skill("vercel-labs/skills", "find-skills", "github");
        assert_eq!(
            registry_install_source(&github).unwrap(),
            "vercel-labs/skills@find-skills"
        );
        let well_known = skill("uizze.sh", "ui-taste", "well-known");
        assert!(registry_install_source(&well_known).is_err());
        let preview = registry_install_args(
            Scope::Project,
            Some("/work"),
            "vercel-labs/skills@find-skills",
            true,
        )
        .unwrap();
        assert_eq!(
            preview,
            [
                "skills",
                "install",
                "vercel-labs/skills@find-skills",
                "--scope",
                "project",
                "--cwd",
                "/work",
                "--dry-run",
                "--json"
            ]
        );
        let apply =
            registry_install_args(Scope::User, None, "vercel-labs/skills@find-skills", false)
                .unwrap();
        assert!(!apply.iter().any(|arg| arg == "--dry-run"));
        assert_eq!(apply.last().map(String::as_str), Some("--json"));
        assert!(registry_install_args(Scope::Project, None, "o/r@s", true).is_err());
        assert_eq!(source_at("o/r", "s").unwrap(), "o/r@s");
        assert!(source_at("o/r", "-s").is_err());
    }

    #[test]
    fn update_and_restore_args_check_then_apply() {
        let check = update_args(Scope::User, None, &[], true).unwrap();
        assert_eq!(
            check,
            ["skills", "update", "--scope", "user", "--dry-run", "--json"]
        );
        let one = update_args(
            Scope::Project,
            Some("/work"),
            &["find-skills".to_string()],
            false,
        )
        .unwrap();
        assert_eq!(
            one,
            [
                "skills",
                "update",
                "find-skills",
                "--scope",
                "project",
                "--cwd",
                "/work",
                "--json"
            ]
        );
        assert!(update_args(Scope::User, None, &["--all".to_string()], true).is_err());
        assert!(update_args(Scope::Project, None, &[], true).is_err());
        assert_eq!(
            restore_args(Scope::User, None, true).unwrap(),
            [
                "skills",
                "restore",
                "--scope",
                "user",
                "--dry-run",
                "--json"
            ]
        );
        assert!(
            !restore_args(Scope::User, None, false)
                .unwrap()
                .iter()
                .any(|arg| arg == "--dry-run")
        );
    }

    #[test]
    fn search_and_browse_fixtures_parse() {
        let page = parse_search(include_str!("../fixtures/registry-search.json")).unwrap();
        assert_eq!(page.skills.len(), 2);
        assert_eq!(page.total, 2);
        assert!(!page.has_more);
        assert_eq!(page.skills[0].source, "vercel-labs/agent-skills");
        assert_eq!(page.skills[0].installs, 846089);
        assert_eq!(page.skills[0].source_type, "github");
        assert!(page.skills[0].weekly_installs.is_empty());

        let browse = parse_browse(include_str!("../fixtures/registry-browse.json")).unwrap();
        assert_eq!(browse.page, 0);
        assert!(browse.has_more);
        assert!(browse.total > browse.skills.len());
        assert_eq!(browse.skills[0].source_type, "well-known");
        assert_eq!(browse.skills[0].id, "uizze.sh/ui-taste");
        assert!(registry_install_source(&browse.skills[0]).is_err());
    }

    #[test]
    fn registry_skill_reads_optional_trend_fields() {
        let text = r#"{"query":"x","searchType":"semantic","count":1,"skills":[{"id":"o/r/s","source":"o/r","slug":"s","name":"s","installs":12,"sourceType":"github","installUrl":null,"url":"https://skills.sh/o/r/s","official":true,"duplicate":false,"weeklyInstalls":[1,2,3],"installsYesterday":5,"change":-2},{"nope":1}]}"#;
        let page = parse_search(text).unwrap();
        assert_eq!(page.skills.len(), 1, "a row without an id is skipped");
        let row = &page.skills[0];
        assert!(row.official && !row.duplicate);
        assert!(row.install_url.is_none());
        assert_eq!(row.weekly_installs, [1, 2, 3]);
        assert_eq!(row.installs_yesterday, Some(5));
        assert_eq!(row.change, Some(-2));
    }

    #[test]
    fn official_list_and_unauthorized_error() {
        let list = parse_official(include_str!("../fixtures/registry-official.json")).unwrap();
        assert_eq!(list.total_owners, 1);
        assert_eq!(list.total_skills, 2);
        let (skills, rows) = official_rows(&list);
        assert_eq!(skills.len(), 2);
        assert_eq!(
            rows,
            [
                RegistryRow::Owner {
                    name: "vercel-labs".into(),
                    installs: 5_100_000,
                    skills: 2
                },
                RegistryRow::Skill(0),
                RegistryRow::Skill(1),
            ]
        );
        let error = parse_official(include_str!(
            "../fixtures/registry-official-unauthorized.json"
        ))
        .unwrap_err();
        assert!(error.starts_with("the official list needs a skills.sh API token"));
        assert!(error.contains("\nhint: set SKILLS_API_TOKEN"));
        assert!(parse_search("not json").is_err());
    }

    #[test]
    fn info_audit_and_use_fixtures_parse() {
        let info = parse_info(include_str!("../fixtures/registry-info.json")).unwrap();
        assert_eq!(info.installs, None);
        assert!(info.hash.is_some());
        assert_eq!(info.skill_md().unwrap().path, "SKILL.md");
        assert!(
            info.skill_md()
                .unwrap()
                .contents
                .as_deref()
                .unwrap()
                .starts_with("---\nname: find-skills")
        );
        let bare = parse_info(
            r#"{"id":"o/r/s","installs":42,"hash":null,"files":[{"path":"a/skill.md","size":3}]}"#,
        )
        .unwrap();
        assert_eq!(bare.installs, Some(42));
        assert!(bare.skill_md().unwrap().contents.is_none());
        let missing = parse_info(r#"{"error":{"code":"unavailable","exit":3,"message":"not found on skills.sh: not_found"}}"#)
            .unwrap_err();
        assert_eq!(missing, "not found on skills.sh: not_found");

        let audits = parse_audit(include_str!("../fixtures/registry-audit.json")).unwrap();
        assert_eq!(audits.len(), 2);
        assert_eq!(audits[0].status, AuditStatus::Pass);
        assert_eq!(audits[0].provider, "Gen Agent Trust Hub");
        assert_eq!(audits[0].risk_level.as_deref(), Some("SAFE"));
        assert_eq!(audits[0].categories.len(), 3);
        let mixed = parse_audit(
            r#"{"audits":[{"provider":"A","slug":"a","status":"warn","summary":"s","auditedAt":"t"},{"provider":"B","slug":"b","status":"fail","summary":"","auditedAt":"t"},{"provider":"C","slug":"c","status":"??","summary":"","auditedAt":"t"}]}"#,
        )
        .unwrap();
        let statuses: Vec<AuditStatus> = mixed.iter().map(|row| row.status).collect();
        assert_eq!(
            statuses,
            [AuditStatus::Warn, AuditStatus::Fail, AuditStatus::Unknown]
        );
        assert!(mixed[0].risk_level.is_none() && mixed[0].categories.is_empty());
        assert!(parse_audit(r#"{"audits":[]}"#).unwrap().is_empty());

        let prompt = parse_use(include_str!("../fixtures/skills-use.json")).unwrap();
        assert_eq!(prompt.name, "find-skills");
        assert!(prompt.prompt.starts_with("You are being given a Skill"));
        assert!(parse_use(r#"{"name":"x","prompt":"  "}"#).is_err());

        let found = parse_discover(include_str!("../fixtures/skills-discover.json")).unwrap();
        assert_eq!(found.len(), 2);
        assert_eq!(found[0].slug, "pr-labeling");
        assert!(found[0].path.ends_with("pr-labeling"));
        assert!(!found[1].description.is_empty());
        assert!(
            parse_discover(r#"{"skills":[{"description":"no name"}]}"#)
                .unwrap()
                .is_empty()
        );
    }

    #[test]
    fn manage_plans_tally_origin_actions_and_keep_notes() {
        let (chips, conflicts) = parse_plan(include_str!("../fixtures/install-plan.json"));
        assert!(conflicts.is_empty());
        assert!(
            chips
                .iter()
                .any(|(kind, count)| kind == "record_origin" && *count == 1)
        );
        assert!(chips.iter().any(|(kind, _)| kind == "copy_skill"));
        let update = include_str!("../fixtures/update-plan.json");
        let (chips, _) = parse_plan(update);
        assert!(chips.iter().any(|(kind, _)| kind == "copy_skill"));
        assert_eq!(
            plan_notes(update),
            [
                "update: find-skills",
                "up to date: other",
                "skipped: mine (no remote origin)"
            ]
        );
        assert!(plan_notes("not json").is_empty());
        let (empty, _) = parse_plan(
            r#"{"dryRun":true,"scope":"user","actions":[],"notes":["no skills with a remote origin"]}"#,
        );
        assert!(empty.is_empty());
    }

    #[test]
    fn a_typed_repo_opens_the_discover_list() {
        assert!(looks_like_repo("vercel-labs/skills"));
        assert!(looks_like_repo(" a.b/c_d "));
        assert!(!looks_like_repo("react"));
        assert!(!looks_like_repo("a/b/c"));
        assert!(!looks_like_repo("a/"));
        assert!(!looks_like_repo("-x/y"));
        assert!(!looks_like_repo("two words/x"));
        let found = parse_discover(include_str!("../fixtures/skills-discover.json")).unwrap();
        let page = discovered_page("vercel-labs/skills", found);
        assert_eq!(page.total, 2);
        assert_eq!(page.skills[0].id, "vercel-labs/skills/pr-labeling");
        assert_eq!(page.skills[0].source, "vercel-labs/skills");
        assert_eq!(
            registry_install_source(&page.skills[0]).unwrap(),
            "vercel-labs/skills@pr-labeling"
        );
        assert_eq!(RegistryView::from_key("repo"), Some(RegistryView::Repo));
        assert!(!RegistryView::ALL.contains(&RegistryView::Repo));
        assert_eq!(RegistryView::from_key("nope"), None);
    }

    #[test]
    fn providers_args_follow_the_scope() {
        assert_eq!(
            providers_args(Scope::User, None).unwrap(),
            ["providers", "list", "--scope", "user", "--json"]
        );
        assert_eq!(
            providers_args(Scope::Project, Some("/proj")).unwrap(),
            [
                "providers",
                "list",
                "--scope",
                "project",
                "--cwd",
                "/proj",
                "--json"
            ]
        );
        assert!(providers_args(Scope::Project, None).is_err());
        assert!(providers_args(Scope::Project, Some("")).is_err());
    }

    #[test]
    fn providers_parse_and_sort_managed_then_installed_then_rest() {
        let providers = parse_providers(include_str!("../fixtures/providers-list.json")).unwrap();
        let ids: Vec<&str> = providers.iter().map(|item| item.id.as_str()).collect();
        assert_eq!(
            ids,
            [
                "claude-code",
                "cursor",
                "kiro-cli",
                "cline",
                "goose",
                "amp",
                "eve",
                "zencoder"
            ]
        );
        let claude = &providers[0];
        assert!(claude.managed && claude.present && !claude.universal);
        assert_eq!(claude.synca_agent.as_deref(), Some("claude"));
        assert_eq!(claude.skills, 12);
        assert_eq!(claude.location(false), Some("/Users/x/.claude/skills"));
        assert!(providers[1].universal && providers[1].managed);
        assert_eq!(providers[1].synca_agent.as_deref(), Some("cursor"));

        let eve = providers.iter().find(|item| item.id == "eve").unwrap();
        assert!(eve.path.is_none() && eve.global_dir.is_none());
        assert_eq!(eve.location(false), None);
        assert_eq!(eve.location(true), Some("agent/skills"));
        assert!(!eve.present && eve.skills == 0);

        // Missing optional fields fall back to defaults; unknown ones are ignored.
        let sparse = providers.iter().find(|item| item.id == "zencoder").unwrap();
        assert!(!sparse.managed && !sparse.present && !sparse.universal);
        assert_eq!(sparse.location(false), Some("~/.zencoder/skills"));
        assert_eq!(sparse.skills, 0);
        assert_eq!(sparse.location(true), Some(".zencoder/skills"));
        assert!(providers.iter().any(|item| item.id == "goose"));
    }

    #[test]
    fn providers_summary_counts_installed_and_managed() {
        let providers = parse_providers(include_str!("../fixtures/providers-list.json")).unwrap();
        assert_eq!(
            providers_summary(&providers),
            "8 providers, 5 installed, 3 managed by Synca"
        );
        assert_eq!(
            providers_summary(&providers[..1]),
            "1 provider, 1 installed, 1 managed by Synca"
        );
        assert_eq!(
            providers_summary(&[]),
            "0 providers, 0 installed, 0 managed by Synca"
        );
    }

    #[test]
    fn providers_filter_matches_name_or_id_ignoring_case() {
        let providers = parse_providers(include_str!("../fixtures/providers-list.json")).unwrap();
        let hits = |needle: &str| -> Vec<&str> {
            providers
                .iter()
                .filter(|item| item.matches(&needle.to_lowercase()))
                .map(|item| item.id.as_str())
                .collect()
        };
        assert_eq!(hits("CLAUDE"), ["claude-code"]);
        assert_eq!(hits("kiro"), ["kiro-cli"]);
        assert_eq!(hits("cli"), ["kiro-cli", "cline"]);
        assert_eq!(hits("").len(), providers.len());
        assert!(hits("nothing like this").is_empty());
    }

    #[test]
    fn providers_parse_rejects_errors_and_missing_lists() {
        assert_eq!(
            parse_providers(include_str!("../fixtures/error.json")).unwrap_err(),
            "unknown command \"nope\"\nhint: see: synca schema"
        );
        assert!(parse_providers("{\"scope\":\"user\"}").is_err());
        assert!(parse_providers("not json").is_err());
        assert!(parse_providers("{\"providers\":[]}").unwrap().is_empty());
    }

    #[test]
    fn an_old_binary_asks_for_an_update() {
        let old = r#"{"error":{"code":"usage","exit":2,"message":"unknown command 'providers' (see: synca help)","hint":"see: synca help"}}"#;
        let message = parse_providers(old).unwrap_err();
        assert_eq!(
            providers_failure(&message),
            "Update the synca CLI to list all providers"
        );
        assert_eq!(providers_failure("disk full"), "disk full");
    }

    #[test]
    fn installs_read_short() {
        assert_eq!(format_installs(950), "950");
        assert_eq!(format_installs(1_200), "1.2k");
        assert_eq!(format_installs(24_000), "24k");
        assert_eq!(format_installs(4_082_959), "4.1M");
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
