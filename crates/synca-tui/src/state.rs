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
    /// First visible row in the Skills pane (bounded viewport, not wrap-around).
    pub skill_scroll: usize,
    /// First visible row in the MCPs pane.
    pub mcp_scroll: usize,
    /// Last known inner height of Skills pane (rows); set on draw.
    pub skill_viewport: usize,
    /// Last known inner height of MCPs pane (rows); set on draw.
    pub mcp_viewport: usize,
    /// Selection highlight within the visible window (always 0..viewport).
    pub skill_list_state: ListState,
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
            skill_scroll: 0,
            mcp_scroll: 0,
            skill_viewport: 0,
            mcp_viewport: 0,
            skill_list_state: ListState::default(),
            mcp_list_state: ListState::default(),
            detail_scroll: 0,
            status: "Ready.".into(),
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

    /// Clamp scroll offsets so the selected row stays inside a fixed-height
    /// viewport. Selection in `ListState` is relative to the visible window
    /// (index - scroll), never the full unbounded list.
    pub fn sync_list_states(&mut self) {
        self.skill_scroll = ensure_visible(
            self.skill_idx,
            self.skill_scroll,
            self.skill_viewport.max(1),
            self.skills().len(),
        );
        self.mcp_scroll = ensure_visible(
            self.mcp_idx,
            self.mcp_scroll,
            self.mcp_viewport.max(1),
            self.mcps().len(),
        );
        if self.skills().is_empty() {
            self.skill_list_state.select(None);
        } else {
            self.skill_list_state
                .select(Some(self.skill_idx.saturating_sub(self.skill_scroll)));
        }
        if self.mcps().is_empty() {
            self.mcp_list_state.select(None);
        } else {
            self.mcp_list_state
                .select(Some(self.mcp_idx.saturating_sub(self.mcp_scroll)));
        }
    }

    /// Record pane inner heights from the latest draw so scroll stays bounded.
    pub fn set_viewports(&mut self, skill_h: usize, mcp_h: usize) {
        let skill_changed = skill_h != self.skill_viewport;
        let mcp_changed = mcp_h != self.mcp_viewport;
        self.skill_viewport = skill_h;
        self.mcp_viewport = mcp_h;
        if skill_changed || mcp_changed {
            self.sync_list_states();
        }
    }

    /// After Tab / section change: clamp indices, reset detail scroll, re-sync list states
    /// so a long scroll on one page/section cannot hide the selection on another.
    pub fn on_context_change(&mut self) {
        self.clamp_idx();
        self.detail_scroll = 0;
        self.sync_list_states();
    }

    /// Move selection by `delta` rows. Stops at list ends — no wrap-around
    /// (no endless list).
    pub fn move_sel(&mut self, delta: i32) {
        match self.section {
            Section::Skills => {
                let n = self.skills().len();
                if n == 0 {
                    return;
                }
                let cur = self.skill_idx as i32 + delta;
                self.skill_idx = cur.clamp(0, (n as i32) - 1) as usize;
            }
            Section::Mcps => {
                let n = self.mcps().len();
                if n == 0 {
                    return;
                }
                let cur = self.mcp_idx as i32 + delta;
                self.mcp_idx = cur.clamp(0, (n as i32) - 1) as usize;
            }
        }
        self.detail_scroll = 0;
        self.sync_list_states();
    }

    /// Jump by one viewport page (PgUp/PgDn on the focused list).
    pub fn page_sel(&mut self, forward: bool) {
        let step = match self.section {
            Section::Skills => self.skill_viewport.max(1) as i32,
            Section::Mcps => self.mcp_viewport.max(1) as i32,
        };
        self.move_sel(if forward { step } else { -step });
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

/// Keep `selected` inside `[offset, offset+visible)`. Returns clamped offset.
/// Does not wrap — ends are hard stops (bounded viewport).
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

#[cfg(test)]
mod tests {
    use super::ensure_visible;

    #[test]
    fn ensure_visible_scrolls_down_when_past_bottom() {
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
    fn ensure_visible_jump_to_start_clamps_offset() {
        assert_eq!(ensure_visible(0, 15, 5, 20), 0);
    }

    #[test]
    fn ensure_visible_empty_or_zero_height() {
        assert_eq!(ensure_visible(0, 0, 0, 10), 0);
        assert_eq!(ensure_visible(0, 3, 5, 0), 0);
    }

    #[test]
    fn ensure_visible_never_exceeds_max_offset() {
        // len=10, visible=4 → max offset 6; selecting last row → offset 6
        assert_eq!(ensure_visible(9, 0, 4, 10), 6);
    }
}
