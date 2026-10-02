use synca_core::models::{ConflictPolicy, McpEntry, Scope, SkillEntry};
use synca_core::scan::Inventory;
use synca_core::sync::SyncPlan;
use ratatui::layout::Rect;
use ratatui::widgets::ListState;
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

#[derive(Debug, Clone, Copy, Default)]
pub struct UiLayout {
    pub header: Rect,
    pub skills: Rect,
    pub mcps: Rect,
    pub detail: Rect,
    pub user_tab: Rect,
    pub project_tab: Rect,
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
    /// Keeps Skills list scrolled so `skill_idx` stays visible.
    pub skill_list_state: ListState,
    /// Keeps MCPs list scrolled so `mcp_idx` stays visible.
    pub mcp_list_state: ListState,
    /// Detail pane vertical scroll; reset when selection / page / section changes.
    pub detail_scroll: u16,
    pub status: String,
    pub help: bool,
    pub pending: Option<Pending>,
    pub last_plan: Option<SyncPlan>,
    pub skill_decisions: BTreeMap<String, ConflictPolicy>,
    pub mcp_decisions: BTreeMap<String, ConflictPolicy>,
    pub project_ok: bool,
    pub update_msg: String,
    /// Last-drawn pane rects for mouse hit-testing.
    pub layout: UiLayout,
}

impl AppState {
    pub fn new(cwd: &std::path::Path) -> Self {
        let user_inv = synca_core::scan::scan_all(Scope::User, cwd);
        let project_inv = synca_core::scan::scan_all(Scope::Project, cwd);
        let project_ok = project_inv.project_root.is_some();
        let mut s = Self {
            cwd: cwd.to_path_buf(),
            page: Page::User,
            section: Section::Skills,
            user_inv,
            project_inv,
            skill_idx: 0,
            mcp_idx: 0,
            skill_list_state: ListState::default(),
            mcp_list_state: ListState::default(),
            detail_scroll: 0,
            status: "Tab/click pages · [/] sections · j/k/click/wheel · s sync · S sync-missing · u update · q quit"
                .into(),
            help: false,
            pending: None,
            last_plan: None,
            skill_decisions: BTreeMap::new(),
            mcp_decisions: BTreeMap::new(),
            project_ok,
            update_msg: String::new(),
            layout: UiLayout::default(),
        };
        s.sync_list_states();
        s
    }

    pub fn reload(&mut self) {
        self.user_inv = synca_core::scan::scan_all(Scope::User, &self.cwd);
        self.project_inv = synca_core::scan::scan_all(Scope::Project, &self.cwd);
        self.project_ok = self.project_inv.project_root.is_some();
        self.clamp_idx();
        self.detail_scroll = 0;
        self.sync_list_states();
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

    /// Keep `ListState` selection in sync with indices so ratatui auto-scrolls
    /// the focused row into the visible viewport. Also clears stale offsets when
    /// the list becomes empty or the selected index wraps / is clamped.
    pub fn sync_list_states(&mut self) {
        if self.skills().is_empty() {
            self.skill_list_state.select(None);
        } else {
            self.skill_list_state.select(Some(self.skill_idx));
        }
        if self.mcps().is_empty() {
            self.mcp_list_state.select(None);
        } else {
            self.mcp_list_state.select(Some(self.mcp_idx));
        }
    }

    /// After Tab / section change: clamp indices, reset detail scroll, re-sync list states
    /// so a long scroll on one page/section cannot hide the selection on another.
    pub fn on_context_change(&mut self) {
        self.clamp_idx();
        self.detail_scroll = 0;
        self.sync_list_states();
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
        self.detail_scroll = 0;
        self.sync_list_states();
    }
    pub fn select_skill(&mut self, idx: usize) {
        if self.skills().is_empty() {
            return;
        }
        self.section = Section::Skills;
        self.skill_idx = idx.min(self.skills().len() - 1);
        self.detail_scroll = 0;
        self.sync_list_states();
    }

    pub fn select_mcp(&mut self, idx: usize) {
        if self.mcps().is_empty() {
            return;
        }
        self.section = Section::Mcps;
        self.mcp_idx = idx.min(self.mcps().len() - 1);
        self.detail_scroll = 0;
        self.sync_list_states();
    }
}

#[cfg(test)]
mod tests {
    /// Pure helper documenting the viewport contract (ratatui ListState does this on render).
    fn ensure_visible(selected: usize, offset: usize, visible: usize, len: usize) -> usize {
        if len == 0 || visible == 0 {
            return 0;
        }
        let selected = selected.min(len - 1);
        let max_offset = len.saturating_sub(visible);
        let mut offset = offset.min(max_offset);
        if selected < offset {
            offset = selected;
        } else if selected >= offset.saturating_add(visible) {
            offset = selected + 1 - visible;
        }
        offset.min(max_offset)
    }

    #[test]
    fn ensure_visible_scrolls_down_when_past_bottom() {
        // viewport height 5, select index 7 with offset 0 → offset becomes 3
        assert_eq!(ensure_visible(7, 0, 5, 20), 3);
    }

    #[test]
    fn ensure_visible_scrolls_up_when_above_top() {
        assert_eq!(ensure_visible(2, 5, 5, 20), 2);
    }

    #[test]
    fn ensure_visible_unchanged_when_already_in_view() {
        assert_eq!(ensure_visible(6, 4, 5, 20), 4);
    }

    #[test]
    fn ensure_visible_wrap_to_start() {
        // after rem_euclid wrap to 0 from end, offset must return to 0
        assert_eq!(ensure_visible(0, 15, 5, 20), 0);
    }

    #[test]
    fn ensure_visible_empty_or_zero_height() {
        assert_eq!(ensure_visible(0, 0, 0, 10), 0);
        assert_eq!(ensure_visible(0, 3, 5, 0), 0);
    }
}
