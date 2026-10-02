use synca_core::models::{ConflictPolicy, McpEntry, Scope, SkillEntry};
use synca_core::scan::Inventory;
use synca_core::sync::SyncPlan;
use std::collections::BTreeMap;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Page {
    #[default]
    User,
    Project,
}

impl Page {
    pub fn scope(self) -> Scope {
        match self {
            Page::User => Scope::User,
            Page::Project => Scope::Project,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Page::User => "User",
            Page::Project => "Project",
        }
    }

    pub fn toggle(self) -> Self {
        match self {
            Page::User => Page::Project,
            Page::Project => Page::User,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Section {
    #[default]
    Skills,
    Mcps,
}

impl Section {
    pub fn toggle(self) -> Self {
        match self {
            Section::Skills => Section::Mcps,
            Section::Mcps => Section::Skills,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Section::Skills => "Skills",
            Section::Mcps => "MCPs",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConflictKind {
    Skill,
    Mcp,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConflictItem {
    pub kind: ConflictKind,
    pub key: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Pending {
    /// Sync focused / missing after dry-run: await y/n
    SyncConfirm,
    /// Resolve next conflict: a=keep-source b=keep-target s=skip
    ResolveConflict { remaining: Vec<ConflictItem> },
    /// Update available: await y/n to install
    UpdateInstall,
}

#[derive(Debug)]
pub struct AppState {
    pub cwd: std::path::PathBuf,
    pub page: Page,
    pub section: Section,
    pub user_inv: Inventory,
    pub project_inv: Inventory,
    pub skill_idx: usize,
    pub mcp_idx: usize,
    pub status: String,
    pub help: bool,
    pub pending: Option<Pending>,
    pub last_plan: Option<SyncPlan>,
    pub skill_decisions: BTreeMap<String, ConflictPolicy>,
    pub mcp_decisions: BTreeMap<String, ConflictPolicy>,
    pub project_ok: bool,
    pub update_msg: String,
}

impl AppState {
    pub fn new(cwd: &std::path::Path) -> Self {
        let user_inv = synca_core::scan::scan_all(Scope::User, cwd);
        let project_inv = synca_core::scan::scan_all(Scope::Project, cwd);
        let project_ok = project_inv.project_root.is_some();
        Self {
            cwd: cwd.to_path_buf(),
            page: Page::User,
            section: Section::Skills,
            user_inv,
            project_inv,
            skill_idx: 0,
            mcp_idx: 0,
            status: "Tab pages · [/] sections · j/k move · s sync · S sync-missing · u update · q quit"
                .into(),
            help: false,
            pending: None,
            last_plan: None,
            skill_decisions: BTreeMap::new(),
            mcp_decisions: BTreeMap::new(),
            project_ok,
            update_msg: String::new(),
        }
    }

    pub fn reload(&mut self) {
        self.user_inv = synca_core::scan::scan_all(Scope::User, &self.cwd);
        self.project_inv = synca_core::scan::scan_all(Scope::Project, &self.cwd);
        self.project_ok = self.project_inv.project_root.is_some();
        self.clamp_idx();
    }

    pub fn inv(&self) -> &Inventory {
        match self.page {
            Page::User => &self.user_inv,
            Page::Project => &self.project_inv,
        }
    }

    pub fn skills(&self) -> &[SkillEntry] {
        &self.inv().skills
    }

    pub fn mcps(&self) -> &[McpEntry] {
        &self.inv().mcps
    }

    pub fn clamp_idx(&mut self) {
        if self.skill_idx >= self.skills().len() && !self.skills().is_empty() {
            self.skill_idx = self.skills().len() - 1;
        }
        if self.skills().is_empty() {
            self.skill_idx = 0;
        }
        if self.mcp_idx >= self.mcps().len() && !self.mcps().is_empty() {
            self.mcp_idx = self.mcps().len() - 1;
        }
        if self.mcps().is_empty() {
            self.mcp_idx = 0;
        }
    }

    pub fn move_sel(&mut self, delta: i32) {
        match self.section {
            Section::Skills => {
                let n = self.skills().len();
                if n == 0 {
                    return;
                }
                let cur = self.skill_idx as i32 + delta;
                self.skill_idx = cur.rem_euclid(n as i32) as usize;
            }
            Section::Mcps => {
                let n = self.mcps().len();
                if n == 0 {
                    return;
                }
                let cur = self.mcp_idx as i32 + delta;
                self.mcp_idx = cur.rem_euclid(n as i32) as usize;
            }
        }
    }
}
