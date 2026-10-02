use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::PathBuf;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Scope {
    User,
    Project,
}

impl Scope {
    pub fn as_str(self) -> &'static str {
        match self {
            Scope::User => "user",
            Scope::Project => "project",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum AgentKind {
    Agents, // canonical .agents
    Grok,
    Devin,
    Cognition,
    Omp,
    Pi,
    Kiro,
    OpenCode,
    Claude,
    Cursor,
}

impl AgentKind {
    pub fn as_str(self) -> &'static str {
        match self {
            AgentKind::Agents => "agents",
            AgentKind::Grok => "grok",
            AgentKind::Devin => "devin",
            AgentKind::Cognition => "cognition",
            AgentKind::Omp => "omp",
            AgentKind::Pi => "pi",
            AgentKind::Kiro => "kiro",
            AgentKind::OpenCode => "opencode",
            AgentKind::Claude => "claude",
            AgentKind::Cursor => "cursor",
        }
    }

    pub fn all() -> &'static [AgentKind] {
        &[
            AgentKind::Agents,
            AgentKind::Grok,
            AgentKind::Devin,
            AgentKind::Cognition,
            AgentKind::Omp,
            AgentKind::Pi,
            AgentKind::Kiro,
            AgentKind::OpenCode,
            AgentKind::Claude,
            AgentKind::Cursor,
        ]
    }

    pub fn parse_list(s: &str) -> Vec<AgentKind> {
        s.split(',')
            .filter_map(|p| {
                let p = p.trim().to_ascii_lowercase();
                AgentKind::all()
                    .iter()
                    .copied()
                    .find(|a| a.as_str() == p)
            })
            .collect()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SkillPresence {
    pub agent: AgentKind,
    pub path: PathBuf,
    pub is_symlink: bool,
    pub symlink_target: Option<PathBuf>,
    pub content_hash: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SkillEntry {
    pub key: String,
    pub display_name: String,
    pub scope: Scope,
    pub presence: Vec<SkillPresence>,
    /// True if more than one distinct content hash among presence.
    pub mismatch: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct McpNormalized {
    pub transport: String,
    pub command: Option<Vec<String>>,
    pub url: Option<String>,
    pub args: Option<Vec<String>>,
    pub enabled: Option<bool>,
    /// Env keys present (values redacted for display/compare of secrets).
    pub env_keys: Vec<String>,
    /// Full env for writers (preserve secrets).
    #[serde(skip_serializing_if = "BTreeMap::is_empty", default)]
    pub env: BTreeMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct McpPresence {
    pub agent: AgentKind,
    pub path: PathBuf,
    pub normalized: McpNormalized,
    pub fingerprint: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct McpEntry {
    pub key: String,
    pub scope: Scope,
    pub presence: Vec<McpPresence>,
    pub mismatch: bool,
}

pub fn normalize_key(s: &str) -> String {
    s.trim().to_ascii_lowercase().replace('_', "-").replace(' ', "-")
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, Default)]
#[serde(rename_all = "kebab-case")]
pub enum ConflictPolicy {
    /// Leave conflicting entries untouched.
    #[default]
    Skip,
    /// Prefer canonical/Agents (or first) presence; overwrite others to match.
    KeepSource,
    /// Prefer non-canonical presence; overwrite canonical/others to match.
    KeepTarget,
}

impl ConflictPolicy {
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "skip" => Some(Self::Skip),
            "keep-source" | "keep_source" | "source" | "a" => Some(Self::KeepSource),
            "keep-target" | "keep_target" | "target" | "b" => Some(Self::KeepTarget),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Skip => "skip",
            Self::KeepSource => "keep-source",
            Self::KeepTarget => "keep-target",
        }
    }
}

/// Per-key overrides; keys missing fall back to `default`.
#[derive(Debug, Clone, Default)]
pub struct ConflictDecisions {
    pub default: ConflictPolicy,
    pub skills: BTreeMap<String, ConflictPolicy>,
    pub mcps: BTreeMap<String, ConflictPolicy>,
}

impl ConflictDecisions {
    pub fn with_default(policy: ConflictPolicy) -> Self {
        Self {
            default: policy,
            skills: BTreeMap::new(),
            mcps: BTreeMap::new(),
        }
    }

    pub fn for_skill(&self, key: &str) -> ConflictPolicy {
        self.skills.get(key).copied().unwrap_or(self.default)
    }

    pub fn for_mcp(&self, key: &str) -> ConflictPolicy {
        self.mcps.get(key).copied().unwrap_or(self.default)
    }
}
