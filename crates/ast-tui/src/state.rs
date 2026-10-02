use ast_core::models::{McpEntry, Scope, SkillEntry};
use ast_core::scan::Inventory;
use ast_core::sync::SyncPlan;

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
pub enum Pending {
    /// Sync focused item (after dry-run shown): await y/n
    SyncFocused { kind: Section },
    /// Sync all missing: await y/n
    SyncMissing,
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
    pub project_ok: bool,
}

impl AppState {
    pub fn new(cwd: &std::path::Path) -> Self {
        let user_inv = ast_core::scan::scan_all(Scope::User, cwd);
        let project_inv = ast_core::scan::scan_all(Scope::Project, cwd);
        let project_ok = project_inv.project_root.is_some();
        Self {
            cwd: cwd.to_path_buf(),
            page: Page::User,
            section: Section::Skills,
            user_inv,
            project_inv,
            skill_idx: 0,
            mcp_idx: 0,
            status: "Tab pages · [/] sections · j/k move · s sync · S sync-missing · u update · q quit".into(),
            help: false,
            pending: None,
            last_plan: None,
            project_ok,
        }
    }

    pub fn reload(&mut self) {
        self.user_inv = ast_core::scan::scan_all(Scope::User, &self.cwd);
        self.project_inv = ast_core::scan::scan_all(Scope::Project, &self.cwd);
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
