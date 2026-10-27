//! Synca's window. Inventory, scope, projects, plans, and settings all call
//! the synca CLI. Nothing here rewrites a skill or an MCP config itself.

#[cfg(test)]
mod bench;
mod engine;
mod markdown_view;
#[cfg(feature = "devtools")]
mod perf;

use engine::{Conflict, PROVIDERS, Policy, Scope, Target};
use std::cell::RefCell;
use std::collections::BTreeSet;
use std::rc::Rc;
use std::time::Duration;

use ely_gpui_component::buttons::{Button, ButtonVariant};
use ely_gpui_component::feedback::{EmptyState, InlineMessage, ToastViewport, Toaster};
use ely_gpui_component::forms::{CheckState, Checkbox, Input, InputEvent, TextInput};
use ely_gpui_component::lists::{Tree, TreeNode};
use ely_gpui_component::menus::{ContextMenu, Menu as ElyMenu, MenuItem as ElyMenuItem};
use ely_gpui_component::overlays::{Dialog, Popover};
use ely_gpui_component::primitives::{IconName, Severity, Tooltip};
use ely_gpui_component::theme::{ActiveTheme, ControlSize};
use ely_gpui_component::typography::SelectableText;
use gpui::{
    App, Context, ElementId, Entity, FocusHandle, FontWeight, IntoElement, KeyBinding, Menu,
    MenuItem, MouseButton, Point, Render, ScrollHandle, SharedString, Subscription, Window, div,
    prelude::*, px,
};
use ns_ui::{
    Ask, IconTone, SIDEBAR_INSET, SIDEBAR_ROW_H, SIDEBAR_SECTION_H, SidebarKey, Tone, ask_banner,
    button, chip, chrome_busy_wide, field, icon, icon_button, icon_button_md, loading_pane,
    outline_button, quiet_button, quiet_button_md, section_label, segmented, side_column,
    sidebar_button, sidebar_empty, sidebar_keys, sidebar_reveal, sidebar_row,
    sidebar_scroll_tracked, sidebar_step, skeleton_list, tab_strip, tab_strip_equal, text_field,
    theme, working_row,
};
use serde::Deserialize;

gpui::actions!(
    synca,
    [
        ToggleActivity,
        ToggleSettings,
        InstallSkillTab,
        InstallMcpTab
    ]
);

#[derive(Clone, Copy, PartialEq, Eq)]
enum SkillSourceKind {
    Local,
    Remote,
    SkillsSh,
    Json,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum McpTransportKind {
    Stdio,
    Http,
    Sse,
    Json,
}

/// The two tabs of the one Install dialog. Each keeps its own form fields, so
/// switching never clears what was typed.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum InstallTab {
    Skill,
    Mcp,
}

impl InstallTab {
    fn key(self) -> &'static str {
        match self {
            InstallTab::Skill => "skill",
            InstallTab::Mcp => "mcp",
        }
    }

    /// Unknown keys fall back to the Skill tab.
    fn from_key(key: &str) -> Self {
        if key == "mcp" {
            InstallTab::Mcp
        } else {
            InstallTab::Skill
        }
    }

    fn detail(self) -> &'static str {
        match self {
            InstallTab::Skill => "Choose a local or remote source, then preview the dry-run.",
            InstallTab::Mcp => "Define a connection, then preview the config.",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Kind {
    Skill,
    Mcp,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Route {
    List,
    Plan,
    Update,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Panel {
    Closed,
    Activity,
    Settings,
}

enum Pending {
    /// Unlink the named agent links; the canonical copy stays.
    Unlink {
        keys: Vec<String>,
        agents: Vec<String>,
    },
    /// Purge the canonical copy plus the named agent links; other links dangle.
    Purge {
        keys: Vec<String>,
        agents: Vec<String>,
    },
    /// Remove MCP servers. `keep_hub` leaves the hub config, so the next sync
    /// can restore the links.
    RemoveMcp {
        keys: Vec<String>,
        agents: Vec<String>,
        keep_hub: bool,
    },
    /// Apply one sync per key in a single background run.
    SyncMany {
        target: Target,
        keys: Vec<String>,
    },
    ClearProvider(String),
    InstallUpdate,
    ClearActivity,
}

/// What the agent picker asks the CLI to do once the user confirms.
#[derive(Clone, Copy, PartialEq, Eq)]
enum PickerAction {
    Unlink,
    Purge,
    RemoveMcp,
}

impl PickerAction {
    fn noun(self) -> &'static str {
        match self {
            PickerAction::Unlink => "Unlink",
            PickerAction::Purge => "Purge",
            PickerAction::RemoveMcp => "Remove",
        }
    }
}

/// The agents an action should touch. The picker lists the agents the selected
/// entries are installed for; unchecked ones keep their copy.
struct AgentPicker {
    action: PickerAction,
    keys: Vec<String>,
    agents: BTreeSet<String>,
    checked: BTreeSet<String>,
}

/// Where a row menu was asked for. `number` is one `menu_seq` step, so the
/// same row can be asked twice and still open.
struct RowMenu {
    row: usize,
    number: u64,
    at: Point<gpui::Pixels>,
}

/// The menu a detail agent chip asked for, keyed by the entry and the agent.
struct AgentMenu {
    key: String,
    agent: String,
    number: u64,
    at: Point<gpui::Pixels>,
}

enum Apply {
    Sync {
        target: Target,
        key: Option<String>,
    },
    Install(String),
    AddMcp {
        name: String,
        transport: String,
        command: String,
        url: String,
        enabled: bool,
    },
}

/// One agent the entry is installed for. `synced` is `None` when the CLI gave
/// no hub hash to compare against, so the UI shows unknown instead of drift.
#[derive(Clone)]
struct AgentLink {
    agent: String,
    path: String,
    synced: Option<bool>,
}

#[derive(Clone)]
struct Entry {
    kind: Kind,
    key: String,
    title: String,
    description: String,
    paths: Vec<String>,
    links: Vec<AgentLink>,
    /// Linked agents whose copy matches the hub; `None` when unknown.
    synced: Option<usize>,
    /// Estimated tokens: `SKILL.md` bytes / 4 for a skill, the server's JSON
    /// bytes / 4 for an MCP entry.
    tokens: Option<usize>,
    mismatch: bool,
}

struct PlanView {
    title: String,
    output: String,
    conflicts: Vec<Conflict>,
    chips: Vec<(String, usize)>,
    apply: Apply,
}

/// The markdown entity for the open file plus the key it was built from.
struct MarkdownCache {
    path: String,
    body: String,
    view: Entity<markdown_view::MarkdownView>,
}

/// Quiet period after a keystroke before the sidebar refilters.
const QUERY_DEBOUNCE_MS: u64 = 130;

/// Sidebar rows built above and below the viewport, so a fast scroll never
/// shows a gap while the next frame catches up.
const ROW_OVERSCAN: f32 = 96.;

/// Column header under a section that has rows. An empty section skips it.
const TABLE_HEAD_H: f32 = 22.;
/// Fixed metric columns so the header and every row share one grid.
const COL_DOT_W: f32 = 8.;
const COL_SYNC_W: f32 = 56.;
const COL_TOKEN_W: f32 = 64.;

/// Which column the sidebar table is ordered by. `None` keeps load order.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
enum SortColumn {
    #[default]
    None,
    Name,
    Sync,
    Tokens,
}

/// Header sort. Clicking a column sets it ascending, then descending, then off.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
struct ListSort {
    column: SortColumn,
    ascending: bool,
}

/// Everything the sidebar derives from the loaded entries and the search, so
/// a plain repaint never refilters or reformats the inventory.
///
/// The per-entry half (search keys, row strings, kind, mismatch) is rebuilt
/// only when `entries` is replaced; the groups are rebuilt when the query
/// moves. Every mutation of those inputs runs
/// [`SyncaWindow::refresh_inventory`], which skips the halves whose inputs
/// did not change.
#[derive(Default)]
struct InventoryCache {
    /// Generation of `entries` the per-entry half was built from.
    entries_gen: u64,
    /// Trimmed, lowercased query the groups were filtered with.
    query_key: String,
    /// Matching entry indexes per kind, in sidebar order.
    skills: Vec<usize>,
    mcps: Vec<usize>,
    skills_note: Option<SharedString>,
    mcps_note: Option<SharedString>,
    /// Per-entry title and key, lowercased once per load.
    titles_lc: Vec<String>,
    keys_lc: Vec<String>,
    kind: Vec<Kind>,
    mismatch: Vec<bool>,
    /// "row-{index}", the row's element id.
    ids: Vec<SharedString>,
    titles: Vec<SharedString>,
    meta: Vec<SharedString>,
    status: Vec<SharedString>,
    /// Right-aligned row pair: synced-agent counter, then token estimate.
    counts: Vec<(SharedString, SharedString)>,
    /// Sort keys, parallel to `entries`. Missing values sort last.
    synced_n: Vec<Option<usize>>,
    tokens_n: Vec<Option<usize>>,
    /// The order `skills` and `mcps` were last arranged with.
    sort: ListSort,
    /// Estimated tokens of the rows each group is showing.
    skills_tokens: Option<usize>,
    mcps_tokens: Option<usize>,
}

impl InventoryCache {
    fn prepare_search(entries: &[Entry]) -> (Vec<String>, Vec<String>) {
        (
            entries
                .iter()
                .map(|entry| entry.title.to_lowercase())
                .collect(),
            entries
                .iter()
                .map(|entry| entry.key.to_lowercase())
                .collect(),
        )
    }

    /// Rebuilds the per-entry half once per loaded inventory.
    fn rebuild_entries(&mut self, entries: &[Entry]) {
        (self.titles_lc, self.keys_lc) = Self::prepare_search(entries);
        self.kind = entries.iter().map(|entry| entry.kind).collect();
        self.mismatch = entries.iter().map(|entry| entry.mismatch).collect();
        self.ids = entries
            .iter()
            .enumerate()
            .map(|(index, _)| SharedString::from(format!("row-{index}")))
            .collect();
        self.titles = entries
            .iter()
            .map(|entry| SharedString::from(entry.title.clone()))
            .collect();
        self.status = entries
            .iter()
            .map(|entry| {
                SharedString::from(if entry.mismatch {
                    "Out of sync"
                } else {
                    "In sync"
                })
            })
            .collect();
        self.meta = entries
            .iter()
            .map(|entry| {
                inventory_row_meta(
                    entry,
                    if entry.mismatch {
                        "Out of sync"
                    } else {
                        "In sync"
                    },
                )
            })
            .collect();
        self.counts = entries
            .iter()
            .map(|entry| (entry_sync_label(entry), entry_tokens_label(entry)))
            .collect();
        self.synced_n = entries.iter().map(|entry| entry.synced).collect();
        self.tokens_n = entries.iter().map(|entry| entry.tokens).collect();
    }

    /// Rebuilds both groups for `needle` (trimmed, lowercased) in one pass.
    fn refilter(&mut self, needle: &str) {
        self.query_key = needle.to_string();
        self.skills.clear();
        self.mcps.clear();
        let mut skills_stale = 0;
        let mut mcps_stale = 0;
        for (index, title) in self.titles_lc.iter().enumerate() {
            if !title.contains(needle) && !self.keys_lc[index].contains(needle) {
                continue;
            }
            if self.mismatch[index] {
                match self.kind[index] {
                    Kind::Skill => skills_stale += 1,
                    Kind::Mcp => mcps_stale += 1,
                }
            }
            match self.kind[index] {
                Kind::Skill => self.skills.push(index),
                Kind::Mcp => self.mcps.push(index),
            }
        }
        self.skills_note = stale_note(skills_stale);
        self.mcps_note = stale_note(mcps_stale);
        self.sort_groups();
        self.skills_tokens = sum_tokens(&self.skills, &self.tokens_n);
        self.mcps_tokens = sum_tokens(&self.mcps, &self.tokens_n);
    }

    /// Reorders both groups by [`Self::sort`]. `None` leaves load order.
    fn sort_groups(&mut self) {
        if self.sort.column == SortColumn::None {
            return;
        }
        let sort = self.sort;
        sort_indexes(
            &mut self.skills,
            sort,
            &self.titles_lc,
            &self.synced_n,
            &self.tokens_n,
        );
        sort_indexes(
            &mut self.mcps,
            sort,
            &self.titles_lc,
            &self.synced_n,
            &self.tokens_n,
        );
    }

    /// Entry indexes in sidebar order: skills, then MCP.
    fn visible(&self) -> Vec<usize> {
        self.skills.iter().chain(&self.mcps).copied().collect()
    }

    /// Points from the top of the list content to the row for `target`.
    fn row_top(&self, target: usize) -> f32 {
        let mut prior = Vec::new();
        for rows in [&self.skills, &self.mcps] {
            if let Some(pos) = rows.iter().position(|index| *index == target) {
                return rows_origin(&prior, rows.len()) + pos as f32 * SIDEBAR_ROW_H;
            }
            prior.push(rows.len());
        }
        prior.into_iter().map(group_span).sum()
    }
}

/// Height of one section, including its column header when it has rows.
fn group_span(len: usize) -> f32 {
    SIDEBAR_SECTION_H
        + if len == 0 {
            SIDEBAR_ROW_H
        } else {
            TABLE_HEAD_H + len as f32 * SIDEBAR_ROW_H
        }
}

/// Y of the first data row in a group, after `prior` groups.
fn rows_origin(prior: &[usize], len: usize) -> f32 {
    let mut y: f32 = prior.iter().copied().map(group_span).sum();
    y += SIDEBAR_SECTION_H;
    if len > 0 {
        y += TABLE_HEAD_H;
    }
    y
}

fn sort_indexes(
    indexes: &mut [usize],
    sort: ListSort,
    titles: &[String],
    synced: &[Option<usize>],
    tokens: &[Option<usize>],
) {
    indexes.sort_by(|&left, &right| {
        let ord = match sort.column {
            SortColumn::None => std::cmp::Ordering::Equal,
            SortColumn::Name => cmp_ord(&titles[left], &titles[right], sort.ascending),
            SortColumn::Sync => cmp_missing_last(synced[left], synced[right], sort.ascending),
            SortColumn::Tokens => cmp_missing_last(tokens[left], tokens[right], sort.ascending),
        };
        ord.then(left.cmp(&right))
    });
}

fn cmp_ord<T: Ord>(left: &T, right: &T, ascending: bool) -> std::cmp::Ordering {
    if ascending {
        left.cmp(right)
    } else {
        right.cmp(left)
    }
}

/// `None` stays after every value, in both directions.
fn cmp_missing_last<T: Ord>(
    left: Option<T>,
    right: Option<T>,
    ascending: bool,
) -> std::cmp::Ordering {
    match (left, right) {
        (None, None) => std::cmp::Ordering::Equal,
        (None, Some(_)) => std::cmp::Ordering::Greater,
        (Some(_), None) => std::cmp::Ordering::Less,
        (Some(left), Some(right)) => cmp_ord(&left, &right, ascending),
    }
}

fn sum_tokens(indexes: &[usize], tokens: &[Option<usize>]) -> Option<usize> {
    let mut sum = 0usize;
    let mut any = false;
    for &index in indexes {
        if let Some(count) = tokens.get(index).copied().flatten() {
            sum = sum.saturating_add(count);
            any = true;
        }
    }
    any.then_some(sum)
}

fn stale_note(stale: usize) -> Option<SharedString> {
    (stale > 0).then(|| {
        if stale == 1 {
            SharedString::from("1 out of sync")
        } else {
            SharedString::from(format!("{stale} out of sync"))
        }
    })
}

/// A pre-lowercased "title\nkey" against a trimmed, lowercased needle. The
/// separator keeps a needle with a newline of its own from matching across
/// the title/key boundary.
/// The selected skill's folder, collected once per selection: the tree, the tree, the
/// file shown until the user picks one, and that file's preview. Keyed by
/// `(entries generation, selected index)`, so a repaint reuses it and a
/// selection change or reload rebuilds it.
struct SkillFilesCache {
    key: (u64, usize),
    /// The installed directory the tree came from; `None` when the entry has none.
    root: Option<String>,
    items: Vec<TreeItem>,
    /// The file `chosen_file` shows for an empty `file_path`, and its preview.
    default_open: String,
    default_preview: String,
}

impl SkillFilesCache {
    fn empty() -> Self {
        Self {
            key: (u64::MAX, usize::MAX),
            root: None,
            items: Vec::new(),
            default_open: String::new(),
            default_preview: String::new(),
        }
    }

    /// Probes the entry's paths for an installed directory and collects it.
    fn collect(paths: &[String], key: (u64, usize)) -> Self {
        let root = paths
            .iter()
            .find(|path| std::path::Path::new(path.as_str()).is_dir())
            .cloned();
        let mut items = Vec::new();
        if let Some(dir) = root.as_deref() {
            collect_tree(std::path::Path::new(dir), 0, &mut items);
        }
        let default_open = root
            .as_deref()
            .map(|dir| chosen_file(dir, "", &items))
            .unwrap_or_default();
        let default_preview = if default_open.is_empty() {
            String::new()
        } else {
            read_preview(&default_open)
        };
        Self {
            key,
            root,
            items,
            default_open,
            default_preview,
        }
    }
}

struct SyncaWindow {
    entries: Vec<Entry>,
    /// Bumped every time `entries` is replaced, so the caches notice.
    entries_gen: u64,
    /// Sidebar groups, notes and row strings; see [`InventoryCache`].
    inventory: InventoryCache,
    /// Column sort for both sidebar tables. `None` keeps load order.
    sort: ListSort,
    /// Bumped for every query change so only the latest debounce fires.
    query_job: u64,
    /// The skill tree and default preview for the current selection.
    skill_files: SkillFilesCache,
    /// Plan output with ANSI codes stripped, cleared when a plan loads.
    plan_clean: Option<SharedString>,
    /// Update output with ANSI codes stripped, cleared when it changes.
    update_clean: Option<SharedString>,
    selected: Option<usize>,
    /// Rows the bulk actions apply to. Always contains `selected`.
    bulk: BTreeSet<usize>,
    /// The row a shift-click range starts from.
    anchor: Option<usize>,
    route: Route,
    panel: Panel,
    scope: Scope,
    projects: Vec<String>,
    active_project: Option<String>,
    disabled: Vec<String>,
    activity: Vec<engine::LogEntry>,
    activity_limit: usize,
    activity_open: Option<i64>,
    onboarding: Option<InstallTab>,
    /// A keystroke a dev command asked for; sent from the next render, which
    /// has the window.
    #[cfg(feature = "devtools")]
    dev_key: Option<String>,
    /// A pointer drag a dev command asked for, in window points. Sent from the
    /// next render, the same way `dev_key` is. Down, a few moves with the button
    /// held, then up.
    #[cfg(feature = "devtools")]
    dev_drag: Option<(f32, f32, f32, f32)>,
    plan: Option<PlanView>,
    file_preview: String,
    file_path: String,
    /// Native scroll of the file preview inside the fixed FILES section.
    file_scroll: ScrollHandle,
    /// The open file's markdown view, keyed by path and body so unrelated
    /// renders keep the selection.
    markdown: Option<MarkdownCache>,
    source: String,
    skill_source_kind: SkillSourceKind,
    mcp_name: String,
    mcp_transport: String,
    mcp_transport_kind: McpTransportKind,
    mcp_command: String,
    mcp_url: String,
    mcp_enabled: bool,
    /// Ely owns the caret, selection, and undo history of every field; the
    /// subscriptions mirror each value into the plain strings the CLI reads.
    source_input: Entity<TextInput>,
    name_input: Entity<TextInput>,
    command_input: Entity<TextInput>,
    mcp_config: String,
    mcp_config_input: Entity<TextInput>,
    _mcp_config_sub: Subscription,
    /// Shared filter for the Skills and MCP lists in the sidebar.
    query: String,
    query_input: Entity<TextInput>,
    _source_sub: Subscription,
    _name_sub: Subscription,
    _command_sub: Subscription,
    _url_sub: Subscription,
    url_input: Entity<TextInput>,
    _query_sub: Subscription,
    ask: Option<Ask>,
    pending: Option<Pending>,
    /// The open agent picker, if a bulk action is waiting for its checkboxes.
    picker: Option<AgentPicker>,
    /// The row menu a right click or a dev command asked for.
    row_menu: Option<RowMenu>,
    menu_seq: u64,
    /// The agent menu a detail chip asked for.
    agent_menu: Option<AgentMenu>,
    toaster: Entity<Toaster>,
    version: String,
    update_text: String,
    update_url: String,
    update_ready: bool,
    status: String,
    busy: bool,
    /// Arrow keys move the sidebar selection while this handle is focused.
    list_focus: FocusHandle,
    list_scroll: ScrollHandle,
}

impl SyncaWindow {
    fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let (projects, active_project) = engine::projects();
        let source_input = cx.new(|cx| TextInput::new(window, cx).placeholder("/path/to/skill"));
        let name_input = cx.new(|cx| TextInput::new(window, cx).placeholder("server name"));
        let command_input = cx.new(|cx| TextInput::new(window, cx).placeholder("command"));
        let url_input = cx.new(|cx| TextInput::new(window, cx).placeholder("https://…"));
        let mcp_config_input = cx.new(|cx| {
            TextInput::new(window, cx)
                .multi_line(5, 12)
                .placeholder(r#"{"name":"server","transport":"http","url":"https://..."}"#)
        });
        let query_input =
            cx.new(|cx| TextInput::new(window, cx).placeholder("Search skills and MCP"));
        let mirror = |input: &Entity<TextInput>, slot: fn(&mut Self) -> &mut String| {
            let input = input.clone();
            move |this: &mut Self,
                  _: &Entity<TextInput>,
                  event: &InputEvent,
                  _: &mut Window,
                  cx: &mut Context<Self>| {
                if matches!(event, InputEvent::Changed) {
                    *slot(this) = input.read(cx).text().to_string();
                    cx.notify();
                }
            }
        };
        let _source_sub = cx.subscribe_in(
            &source_input,
            window,
            mirror(&source_input, |this| &mut this.source),
        );
        let _name_sub = cx.subscribe_in(
            &name_input,
            window,
            mirror(&name_input, |this| &mut this.mcp_name),
        );
        let _command_sub = cx.subscribe_in(
            &command_input,
            window,
            mirror(&command_input, |this| &mut this.mcp_command),
        );
        let _url_sub = cx.subscribe_in(
            &url_input,
            window,
            mirror(&url_input, |this| &mut this.mcp_url),
        );
        let _mcp_config_sub = cx.subscribe_in(
            &mcp_config_input,
            window,
            mirror(&mcp_config_input, |this| &mut this.mcp_config),
        );
        // Typing refilters after a short quiet period, so a repaint in between
        // reuses the cache built for the previous query; Enter applies at once.
        let _query_sub = cx.subscribe_in(
            &query_input,
            window,
            |this, _, event: &InputEvent, _, cx| match event {
                InputEvent::Changed => {
                    let text = this.query_input.read(cx).text().to_string();
                    if text != this.query {
                        this.query = text;
                        this.schedule_refilter(cx);
                    }
                    cx.notify();
                }
                InputEvent::Submit => {
                    this.query = this.query_input.read(cx).text().to_string();
                    this.refresh_inventory();
                    cx.notify();
                }
                _ => {}
            },
        );
        let mut window_state = Self {
            entries: Vec::new(),
            entries_gen: 0,
            inventory: InventoryCache::default(),
            sort: ListSort::default(),
            query_job: 0,
            skill_files: SkillFilesCache::empty(),
            plan_clean: None,
            update_clean: None,
            selected: None,
            bulk: BTreeSet::new(),
            anchor: None,
            route: Route::List,
            panel: stored_panel(),
            scope: Scope::User,
            projects,
            active_project,
            disabled: engine::disabled_providers(),
            activity: Vec::new(),
            activity_limit: 40,
            activity_open: None,
            onboarding: None,
            #[cfg(feature = "devtools")]
            dev_key: None,
            #[cfg(feature = "devtools")]
            dev_drag: None,
            plan: None,
            file_preview: String::new(),
            file_path: String::new(),
            file_scroll: ScrollHandle::new(),
            markdown: None,
            source: String::new(),
            skill_source_kind: SkillSourceKind::Local,
            mcp_name: String::new(),
            mcp_transport: "stdio".into(),
            mcp_transport_kind: McpTransportKind::Stdio,
            mcp_command: String::new(),
            mcp_url: String::new(),
            mcp_enabled: true,
            source_input,
            name_input,
            command_input,
            url_input,
            mcp_config: String::new(),
            mcp_config_input,
            _mcp_config_sub,
            query: String::new(),
            query_input,
            _source_sub,
            _name_sub,
            _command_sub,
            _url_sub,
            _query_sub,
            ask: None,
            pending: None,
            picker: None,
            row_menu: None,
            menu_seq: 0,
            agent_menu: None,
            toaster: cx.new(|_| Toaster::default()),
            version: String::new(),
            update_text: String::new(),
            update_url: String::new(),
            update_ready: false,
            status: "Loading".into(),
            busy: true,
            list_focus: cx.focus_handle().tab_stop(true),
            list_scroll: ScrollHandle::new(),
        };
        window_state.reload(cx);
        window_state.load_activity();
        window_state.install_menus(cx);
        cx.spawn(async move |this, cx| {
            let version = cx
                .background_spawn(async {
                    let result = ns_ui::run_tool("synca", &["--version".into()]);
                    // A missing or failing CLI must not put its error text in the status bar.
                    if result.ok {
                        result
                            .text()
                            .lines()
                            .next()
                            .unwrap_or_default()
                            .trim()
                            .to_string()
                    } else {
                        String::new()
                    }
                })
                .await;
            this.update(cx, |this, cx| {
                this.version = version;
                cx.notify();
            })
            .ok();
        })
        .detach();
        window_state
    }

    fn dir(&self) -> Option<&str> {
        self.active_project.as_deref()
    }

    fn scope_name(&self) -> &'static str {
        if self.scope == Scope::User {
            "user"
        } else {
            "workspace"
        }
    }

    fn scope_title(&self) -> &'static str {
        if self.scope == Scope::User {
            "user"
        } else {
            "Workspace"
        }
    }

    /// Shows one message. A failure reads as a warning, anything else as success.
    fn toast(&mut self, text: impl Into<String>, cx: &mut Context<Self>) {
        let text: String = text.into();
        let severity = if text.starts_with("Failed")
            || text.contains("error")
            || text.contains("Enter ")
            || text.contains("Choose ")
        {
            Severity::Warning
        } else {
            Severity::Success
        };
        ns_ui::notify(&self.toaster, severity, &text, cx);
    }

    fn reload(&mut self, cx: &mut Context<Self>) {
        if self.scope == Scope::Project && self.dir().is_none() {
            self.entries.clear();
            self.entries_gen = self.entries_gen.wrapping_add(1);
            self.status = "Choose a workspace".into();
            self.busy = false;
            self.refresh_inventory();
            cx.notify();
            return;
        }
        self.busy = true;
        self.status = "Loading".into();
        cx.notify();
        let scope = self.scope;
        let dir = self.dir().map(str::to_string);
        let disabled = self.disabled.clone();
        cx.spawn(async move |this, cx| {
            let (entries, errors) = cx
                .background_spawn(async move { load_entries(scope, dir.as_deref(), &disabled) })
                .await;
            this.update(cx, |this, cx| {
                let skills = entries
                    .iter()
                    .filter(|entry| entry.kind == Kind::Skill)
                    .count();
                let mcps = entries.len().saturating_sub(skills);
                this.status = if errors.is_empty() {
                    format!("{skills} skills · {mcps} mcp")
                } else {
                    "Load failed".into()
                };
                if !errors.is_empty() {
                    this.toast(errors.join("\n"), cx);
                }
                this.entries = entries;
                this.entries_gen = this.entries_gen.wrapping_add(1);
                if this
                    .selected
                    .is_some_and(|index| index >= this.entries.len())
                {
                    this.selected = None;
                }
                self_keep_selection(this);
                this.busy = false;
                this.refresh_inventory();
                cx.notify();
            })
            .ok();
        })
        .detach();
    }

    /// Rebuilds the inventory cache when an input moved, and nothing when one
    /// did not. Returns whether the cached rows changed.
    fn refresh_inventory(&mut self) -> bool {
        let mut changed = false;
        if self.inventory.entries_gen != self.entries_gen {
            self.inventory.rebuild_entries(&self.entries);
            self.inventory.entries_gen = self.entries_gen;
            changed = true;
        }
        let needle = self.query.trim().to_lowercase();
        if changed || self.inventory.query_key != needle || self.inventory.sort != self.sort {
            self.inventory.sort = self.sort;
            self.inventory.refilter(&needle);
            changed = true;
        }
        changed
    }

    /// Name, sync, then tokens. A third press on the same column restores load order.
    fn set_sort(&mut self, column: SortColumn, cx: &mut Context<Self>) {
        self.sort = if self.sort.column != column {
            ListSort {
                column,
                ascending: true,
            }
        } else if self.sort.ascending {
            ListSort {
                column,
                ascending: false,
            }
        } else {
            ListSort::default()
        };
        self.refresh_inventory();
        cx.notify();
    }

    /// Queues a refilter after the search goes quiet. A newer keystroke bumps
    /// the job and the older timer does nothing.
    fn schedule_refilter(&mut self, cx: &mut Context<Self>) {
        self.query_job = self.query_job.wrapping_add(1);
        let job = self.query_job;
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(Duration::from_millis(QUERY_DEBOUNCE_MS))
                .await;
            this.update(cx, |this, cx| {
                if this.query_job != job {
                    return;
                }
                if this.refresh_inventory() {
                    cx.notify();
                }
            })
            .ok();
        })
        .detach();
    }

    /// Sets the search from outside the input (dev commands, state restore)
    /// and refilters at once, so the next draw sees the new rows.
    #[cfg(feature = "devtools")]
    pub(crate) fn apply_query(&mut self, query: &str, cx: &mut Context<Self>) {
        self.query_job = self.query_job.wrapping_add(1);
        self.query = query.to_string();
        self.query_input
            .update(cx, |input, cx| input.set_text(query.to_string(), cx));
        self.refresh_inventory();
    }

    fn load_activity(&mut self) {
        self.activity = engine::activity_page(&engine::activity_path(), self.activity_limit);
    }

    fn toggle_activity(&mut self, cx: &mut Context<Self>) {
        self.panel = if self.panel == Panel::Activity {
            Panel::Closed
        } else {
            Panel::Activity
        };
        if self.panel == Panel::Activity {
            self.load_activity();
        }
        self.persist_panel(cx);
        self.publish_menus(cx);
        cx.notify();
    }

    fn toggle_settings(&mut self, cx: &mut Context<Self>) {
        self.panel = if self.panel == Panel::Settings {
            Panel::Closed
        } else {
            Panel::Settings
        };
        self.persist_panel(cx);
        self.publish_menus(cx);
        cx.notify();
    }

    fn persist_panel(&self, cx: &mut Context<Self>) {
        let activity = self.panel == Panel::Activity;
        let settings = self.panel == Panel::Settings;
        cx.spawn(async move |_this, cx| {
            cx.background_spawn(async move {
                engine::write_flag("synca.showActivity", activity);
                engine::write_flag("synca.showSettings", settings);
            })
            .await;
        })
        .detach();
    }

    /// Menu actions dispatch from the focused node and then to global listeners.
    /// The listener is registered on `App` so a click in the menu bar still
    /// reaches this window when no text field is focused.
    fn install_menus(&self, cx: &mut Context<Self>) {
        let entity = cx.entity();
        cx.bind_keys([
            KeyBinding::new("cmd-alt-l", ToggleActivity, None),
            KeyBinding::new("cmd-,", ToggleSettings, None),
            KeyBinding::new("cmd-1", InstallSkillTab, None),
            KeyBinding::new("cmd-2", InstallMcpTab, None),
        ]);
        let app: &mut App = cx;
        let activity = entity.clone();
        app.on_action(move |_: &ToggleActivity, app| {
            activity.update(app, |this, cx| this.toggle_activity(cx));
        });
        let skill_tab = entity.clone();
        app.on_action(move |_: &InstallSkillTab, app| {
            skill_tab.update(app, |this, cx| this.pick_install_tab(InstallTab::Skill, cx));
        });
        let mcp_tab = entity.clone();
        app.on_action(move |_: &InstallMcpTab, app| {
            mcp_tab.update(app, |this, cx| this.pick_install_tab(InstallTab::Mcp, cx));
        });
        app.on_action(move |_: &ToggleSettings, app| {
            entity.update(app, |this, cx| this.toggle_settings(cx));
        });
        self.publish_menus(cx);
    }

    fn publish_menus(&self, cx: &mut Context<Self>) {
        let activity = self.panel == Panel::Activity;
        let settings = self.panel == Panel::Settings;
        cx.set_menus(
            ns_ui::keys::StandardMenus::new("Synca", cx)
                .menu(Menu::new("View").items(vec![
                MenuItem::action(
                    if activity {
                        "Hide Activity"
                    } else {
                        "Show Activity"
                    },
                    ToggleActivity,
                )
                .checked(activity),
                MenuItem::action(
                    if settings {
                        "Hide Settings"
                    } else {
                        "Show Settings"
                    },
                    ToggleSettings,
                )
                .checked(settings),
            ]))
                .build(),
        );
    }

    fn run(&mut self, args: Vec<String>, kind: &'static str, cx: &mut Context<Self>, after: After) {
        if let Err(error) = engine::scope_args(self.scope, self.dir())
            && args.iter().any(|arg| arg == "--scope")
        {
            self.toast(error, cx);
            return;
        }
        self.busy = true;
        self.status = "Working".into();
        cx.notify();
        let scope = self.scope_name().to_string();
        let display = format!("synca {}", args.join(" "));
        let path = engine::activity_path();
        cx.spawn(async move |this, cx| {
            let result = cx
                .background_spawn(async move { ns_ui::run_tool("synca", &args) })
                .await;
            let status = if result.ok { kind } else { "error" };
            engine::activity_append(&path, &scope, &display, status, result.code, &result.text());
            let text = result.text();
            let ok = result.ok;
            this.update(cx, |this, cx| {
                this.busy = false;
                this.load_activity();
                match after {
                    After::Reload => {
                        let message = engine::outcome_message(ok, &text);
                        if ok {
                            this.toast(message, cx);
                        } else {
                            ns_ui::notify(&this.toaster, Severity::Warning, &message, cx);
                        }
                        this.route = Route::List;
                        this.reload(cx);
                    }
                    After::Plan(title, apply) => {
                        if !ok {
                            let message = engine::failure_message(&text);
                            ns_ui::notify(&this.toaster, Severity::Warning, &message, cx);
                        } else {
                            let (chips, conflicts) = engine::parse_plan(&text);
                            this.plan = Some(PlanView {
                                title,
                                output: text,
                                conflicts,
                                chips,
                                apply,
                            });
                            this.plan_clean = None;
                            this.route = Route::Plan;
                        }
                        cx.notify();
                    }
                    After::Update => {
                        if !ok && let Some(message) = engine::contract_failure(&text) {
                            ns_ui::notify(&this.toaster, Severity::Warning, &message, cx);
                            this.update_text = message;
                            this.update_clean = None;
                            this.update_ready = false;
                            this.update_url.clear();
                            this.route = Route::Update;
                            cx.notify();
                            return;
                        }
                        let report = ns_ui::parse_update("synca", &text);
                        let severity = if report.available {
                            Severity::Info
                        } else if report.latest.is_empty() {
                            Severity::Warning
                        } else {
                            Severity::Success
                        };
                        ns_ui::notify(&this.toaster, severity, &report.text, cx);
                        this.update_text = report.text;
                        this.update_clean = None;
                        this.update_ready = report.available;
                        this.update_url = report.url;
                        this.route = Route::Update;
                        cx.notify();
                    }
                }
            })
            .ok();
        })
        .detach();
    }

    fn plan_sync(&mut self, target: Target, key: Option<String>, cx: &mut Context<Self>) {
        match engine::sync_plan(self.scope, self.dir(), target, key.as_deref()) {
            Ok(args) => {
                let title = match (&key, target) {
                    (Some(key), Target::Skills) => format!("sync skills · {key}"),
                    (Some(key), Target::Mcp) => format!("sync mcp · {key}"),
                    _ => format!(
                        "sync {}",
                        match target {
                            Target::Skills => "skills",
                            Target::Mcp => "mcp",
                            Target::All => "all",
                        }
                    ),
                };
                let args = engine::with_agents(args, &self.disabled);
                self.run(
                    args,
                    "dry-run",
                    cx,
                    After::Plan(title, Apply::Sync { target, key }),
                );
            }
            Err(error) => self.toast(error, cx),
        }
    }

    /// Opens the Install dialog on `tab`.
    fn open_install(&mut self, tab: InstallTab, cx: &mut Context<Self>) {
        self.onboarding = Some(tab);
        cx.notify();
    }

    fn pick_install_tab(&mut self, tab: InstallTab, cx: &mut Context<Self>) {
        if self.onboarding.is_some() && self.onboarding != Some(tab) {
            self.onboarding = Some(tab);
            cx.notify();
        } else {
            cx.propagate();
        }
    }

    fn preview_install(&mut self, cx: &mut Context<Self>) {
        let raw = self.source.trim().to_string();
        let source = if self.skill_source_kind == SkillSourceKind::Json {
            match Self::json_skill_source(&raw) {
                Ok(source) => source,
                Err(error) => {
                    self.toast(error, cx);
                    return;
                }
            }
        } else {
            raw
        };
        if source.is_empty() {
            self.toast("Enter a skill source or JSON descriptor.", cx);
            return;
        }
        self.onboarding = None;
        match engine::install_args(self.scope, self.dir(), &source, true) {
            Ok(args) => self.run(
                engine::with_agents(args, &self.disabled),
                "dry-run",
                cx,
                After::Plan(format!("install skill · {source}"), Apply::Install(source)),
            ),
            Err(error) => self.toast(error, cx),
        }
    }

    fn preview_mcp(&mut self, cx: &mut Context<Self>) {
        if self.mcp_transport_kind == McpTransportKind::Json
            && let Err(error) = Self::apply_json_mcp_fields(self)
        {
            self.toast(error, cx);
            return;
        }

        let name = self.mcp_name.trim().to_string();
        if name.is_empty() {
            self.toast("Enter an MCP name or JSON config with a name.", cx);
            return;
        }
        self.onboarding = None;
        match engine::add_mcp(
            self.scope,
            self.dir(),
            &name,
            &self.mcp_transport,
            self.mcp_command.trim(),
            self.mcp_url.trim(),
            self.mcp_enabled,
            true,
        ) {
            Ok(args) => {
                let apply = Apply::AddMcp {
                    name: name.clone(),
                    transport: self.mcp_transport.clone(),
                    command: self.mcp_command.trim().into(),
                    url: self.mcp_url.trim().into(),
                    enabled: self.mcp_enabled,
                };
                self.run(
                    engine::with_agents(args, &self.disabled),
                    "dry-run",
                    cx,
                    After::Plan(format!("add mcp · {name}"), apply),
                );
            }
            Err(error) => self.toast(error, cx),
        }
    }

    fn apply_plan(&mut self, cx: &mut Context<Self>) {
        let Some(plan) = self.plan.clone() else {
            return;
        };
        let scope = self.scope;
        let dir = self.dir().map(str::to_string);
        let disabled = self.disabled.clone();
        self.busy = true;
        cx.notify();
        let log_path = engine::activity_path();
        let scope_name = self.scope_name().to_string();
        cx.spawn(async move |this, cx| {
            let message = cx
                .background_spawn(async move {
                    apply_now(
                        scope,
                        dir.as_deref(),
                        &disabled,
                        &plan,
                        &log_path,
                        &scope_name,
                    )
                })
                .await;
            this.update(cx, |this, cx| {
                this.busy = false;
                this.route = Route::List;
                this.load_activity();
                this.toast(message, cx);
                this.reload(cx);
            })
            .ok();
        })
        .detach();
    }
    fn json_skill_source(raw: &str) -> Result<String, String> {
        let value: serde_json::Value =
            serde_json::from_str(raw).map_err(|error| format!("Invalid skill JSON: {error}"))?;
        value
            .get("source")
            .or_else(|| value.get("url"))
            .and_then(|value| value.as_str())
            .map(str::to_string)
            .filter(|value| !value.trim().is_empty())
            .ok_or_else(|| "Skill JSON needs a non-empty string field: source or url.".into())
    }

    fn apply_json_mcp_fields(window: &mut SyncaWindow) -> Result<(), String> {
        let value: serde_json::Value = serde_json::from_str(window.mcp_config.trim())
            .map_err(|error| format!("Invalid MCP JSON: {error}"))?;
        let root = value
            .get("mcpServers")
            .and_then(|value| value.as_object())
            .and_then(|servers| servers.values().next())
            .unwrap_or(&value);
        let name = root
            .get("name")
            .and_then(|value| value.as_str())
            .or_else(|| {
                value
                    .get("mcpServers")
                    .and_then(|value| value.as_object())
                    .and_then(|servers| servers.keys().next())
                    .map(|value| value.as_str())
            })
            .ok_or_else(|| "MCP JSON needs a name or an mcpServers key.".to_string())?;
        let transport = root
            .get("transport")
            .or_else(|| root.get("type"))
            .and_then(|value| value.as_str())
            .unwrap_or("stdio")
            .to_lowercase();
        window.mcp_name = name.to_string();
        window.mcp_transport = transport.clone();
        window.mcp_transport_kind = match transport.as_str() {
            "http" | "remote" => McpTransportKind::Http,
            "sse" => McpTransportKind::Sse,
            _ => McpTransportKind::Stdio,
        };
        if let Some(url) = root.get("url").and_then(|value| value.as_str()) {
            window.mcp_url = url.to_string();
        }
        if let Some(command) = root.get("command").and_then(|value| value.as_str()) {
            let args = root
                .get("args")
                .and_then(|value| value.as_array())
                .map(|values| {
                    values
                        .iter()
                        .filter_map(|value| value.as_str())
                        .collect::<Vec<_>>()
                        .join(" ")
                })
                .unwrap_or_default();
            window.mcp_command = if args.is_empty() {
                command.to_string()
            } else {
                format!("{command} {args}")
            };
        }
        if let Some(enabled) = root.get("enabled").and_then(|value| value.as_bool()) {
            window.mcp_enabled = enabled;
        }
        Ok(())
    }

    fn confirm(&mut self, cx: &mut Context<Self>) {
        let Some(pending) = self.pending.take() else {
            return;
        };
        self.ask = None;
        match pending {
            Pending::Unlink { keys, agents } => {
                let mut commands = Vec::new();
                for key in &keys {
                    match engine::remove_skill(self.scope, self.dir(), key, false, Some(&agents)) {
                        Ok(args) => commands.push(args),
                        Err(error) => {
                            self.toast(error, cx);
                            return;
                        }
                    }
                }
                self.run_batch(commands, "unlinked", cx);
            }
            Pending::Purge { keys, agents } => {
                let mut commands = Vec::new();
                for key in &keys {
                    match engine::remove_skill(self.scope, self.dir(), key, true, Some(&agents)) {
                        Ok(args) => commands.push(args),
                        Err(error) => {
                            self.toast(error, cx);
                            return;
                        }
                    }
                }
                self.run_batch(commands, "purged", cx);
            }
            Pending::RemoveMcp {
                keys,
                agents,
                keep_hub,
            } => {
                let mut commands = Vec::new();
                for key in &keys {
                    match engine::remove_mcp(self.scope, self.dir(), key, Some(&agents), keep_hub) {
                        Ok(args) => commands.push(args),
                        Err(error) => {
                            self.toast(error, cx);
                            return;
                        }
                    }
                }
                self.run_batch(commands, "removed", cx);
            }
            Pending::SyncMany { target, keys } => {
                let mut commands = Vec::new();
                for key in &keys {
                    match engine::sync_apply(self.scope, self.dir(), target, Some(key)) {
                        Ok(args) => commands.push(engine::with_agents(args, &self.disabled)),
                        Err(error) => {
                            self.toast(error, cx);
                            return;
                        }
                    }
                }
                self.run_batch(commands, "synced", cx);
            }
            Pending::ClearProvider(id) => self.clear_provider(id, cx),
            Pending::InstallUpdate => self.run(
                vec!["update".into(), "--json".into()],
                "ok",
                cx,
                After::Update,
            ),
            Pending::ClearActivity => {
                engine::activity_clear(&engine::activity_path());
                self.activity.clear();
                self.toast("Activity cleared.", cx);
            }
        }
    }

    /// Runs one CLI command per key in a single background pass, logging each
    /// to Activity, then reloads once. A failure does not stop the rest; the
    /// toast reports the score and the first failure.
    fn run_batch(
        &mut self,
        commands: Vec<Vec<String>>,
        done: &'static str,
        cx: &mut Context<Self>,
    ) {
        if commands.is_empty() {
            return;
        }
        self.busy = true;
        self.status = "Working".into();
        cx.notify();
        let scope = self.scope_name().to_string();
        let path = engine::activity_path();
        cx.spawn(async move |this, cx| {
            let (ok, total, failure) = cx
                .background_spawn(async move {
                    let total = commands.len();
                    let mut ok = 0;
                    let mut failure = None;
                    for args in commands {
                        let display = format!("synca {}", args.join(" "));
                        let result = ns_ui::run_tool("synca", &args);
                        let status = if result.ok { "ok" } else { "error" };
                        engine::activity_append(
                            &path,
                            &scope,
                            &display,
                            status,
                            result.code,
                            &result.text(),
                        );
                        let text = result.text();
                        if result.ok {
                            ok += 1;
                        } else if failure.is_none() {
                            failure = Some(engine::failure_message(&text));
                        }
                    }
                    (ok, total, failure)
                })
                .await;
            this.update(cx, |this, cx| {
                this.busy = false;
                this.route = Route::List;
                this.load_activity();
                let score = format!("{ok}/{total} {done}");
                match failure {
                    Some(message) => ns_ui::notify(
                        &this.toaster,
                        Severity::Warning,
                        &format!("{score}\n{message}"),
                        cx,
                    ),
                    None => this.toast(score, cx),
                }
                this.reload(cx);
            })
            .ok();
        })
        .detach();
    }

    fn clear_provider(&mut self, id: String, cx: &mut Context<Self>) {
        match engine::clear_provider(self.scope, self.dir(), &id) {
            Ok(args) => self.run(args, "ok", cx, After::Reload),
            Err(error) => self.toast(error, cx),
        }
    }

    fn ask_for(
        &mut self,
        title: &str,
        message: &str,
        confirm: &str,
        pending: Pending,
        cx: &mut Context<Self>,
    ) {
        self.ask = Some(Ask {
            title: title.into(),
            message: message.into(),
            confirm: confirm.into(),
            confirm_icon: "trash",
            destructive: true,
        });
        self.pending = Some(pending);
        cx.notify();
    }

    fn add_project(&mut self, path: String, cx: &mut Context<Self>) {
        if !self.projects.iter().any(|item| item == &path) {
            self.projects.push(path.clone());
        }
        self.active_project = Some(path);
        engine::save_projects(&self.projects, self.active_project.as_deref());
        if self.scope == Scope::Project {
            self.reload(cx);
        } else {
            cx.notify();
        }
    }

    fn on_list_key(&mut self, key: SidebarKey, cx: &mut Context<Self>) {
        if key == SidebarKey::Open {
            ns_ui::show_detail();
            cx.notify();
            return;
        }
        let visible = self.inventory.visible();
        let current = self
            .selected
            .and_then(|selected| visible.iter().position(|index| *index == selected));
        let Some(pos) = sidebar_step(current, visible.len(), key) else {
            return;
        };
        let index = visible[pos];
        sidebar_reveal(
            &self.list_scroll,
            self.inventory.row_top(index),
            SIDEBAR_ROW_H,
        );
        if self.selected != Some(index) {
            self.route = Route::List;
            self.selected = Some(index);
            self.reset_file();
        }
        self.bulk.clear();
        self.bulk.insert(index);
        self.anchor = Some(index);
        cx.notify();
    }

    /// A plain click selects one row; Cmd/Ctrl toggles one more; Shift extends
    /// the range from the anchor. The primary row is always in `bulk`.
    fn click_row(&mut self, index: usize, modifiers: gpui::Modifiers, cx: &mut Context<Self>) {
        if modifiers.shift {
            self.extend_to(index, cx);
            return;
        }
        let before = self.selected;
        self.route = Route::List;
        if modifiers.secondary() {
            if !self.bulk.remove(&index) {
                self.bulk.insert(index);
            }
            if self.bulk.is_empty() {
                self.bulk.insert(index);
            }
            if !self
                .selected
                .is_some_and(|selected| self.bulk.contains(&selected))
            {
                self.selected = self.bulk.iter().next().copied();
            }
        } else {
            self.bulk.clear();
            self.bulk.insert(index);
            self.selected = Some(index);
            self.anchor = Some(index);
        }
        if self.selected != before {
            self.reset_file();
        }
        cx.notify();
    }

    /// Selects the range between the anchor and `index`, replacing the rest.
    fn extend_to(&mut self, index: usize, cx: &mut Context<Self>) {
        let visible = self.inventory.visible();
        let anchor = self.anchor.or(self.selected).unwrap_or(index);
        let (Some(from), Some(to)) = (
            visible.iter().position(|item| *item == anchor),
            visible.iter().position(|item| *item == index),
        ) else {
            return;
        };
        self.route = Route::List;
        self.bulk = visible[from.min(to)..=from.max(to)]
            .iter()
            .copied()
            .collect();
        self.selected = Some(index);
        sidebar_reveal(
            &self.list_scroll,
            self.inventory.row_top(index),
            SIDEBAR_ROW_H,
        );
        self.reset_file();
        cx.notify();
    }

    /// Keys with a modifier, which `sidebar_keys` lets through: Cmd/Ctrl+A
    /// selects every visible row, Shift+Up/Down extends the range, Escape
    /// collapses back to the primary row.
    fn on_list_modified_key(&mut self, event: &gpui::KeyDownEvent, cx: &mut Context<Self>) {
        let keystroke = &event.keystroke;
        let modifiers = keystroke.modifiers;
        let visible = self.inventory.visible();
        if visible.is_empty() {
            return;
        }
        if modifiers.secondary() && keystroke.key == "a" {
            self.bulk = visible.iter().copied().collect();
            if self.selected.is_none() {
                self.selected = visible.first().copied();
                self.anchor = self.selected;
            }
            cx.stop_propagation();
            cx.notify();
            return;
        }
        if modifiers.shift && !modifiers.secondary() {
            let Some(delta) = (match keystroke.key.as_str() {
                "up" => Some(-1),
                "down" => Some(1),
                _ => None,
            }) else {
                return;
            };
            let current = self
                .selected
                .and_then(|selected| visible.iter().position(|item| *item == selected))
                .unwrap_or(0);
            let next = (current as isize + delta).clamp(0, visible.len() as isize - 1) as usize;
            self.extend_to(visible[next], cx);
            cx.stop_propagation();
            return;
        }
        if keystroke.key == "escape" && self.bulk.len() > 1 {
            self.bulk.retain(|index| Some(*index) == self.selected);
            if let Some(selected) = self.selected {
                self.anchor = Some(selected);
            }
            cx.notify();
        }
    }

    /// Drops the open file so the next detail render collects it again, and
    /// restarts the preview scroll from the top.
    fn reset_file(&mut self) {
        self.file_preview.clear();
        self.file_path.clear();
        self.file_scroll = ScrollHandle::new();
        self.markdown = None;
    }

    /// Makes `index` the primary row without dropping the rest of the bulk.
    fn show_row(&mut self, index: usize, cx: &mut Context<Self>) {
        if self.selected != Some(index) {
            self.reset_file();
        }
        self.route = Route::List;
        self.selected = Some(index);
        self.bulk.insert(index);
        self.anchor.get_or_insert(index);
        sidebar_reveal(
            &self.list_scroll,
            self.inventory.row_top(index),
            SIDEBAR_ROW_H,
        );
        ns_ui::show_detail();
        cx.notify();
    }

    /// A right click opens the row menu at the pointer. A row outside the
    /// selection joins it first, so the menu speaks for the row under the
    /// pointer unless it was already part of a bulk selection.
    fn right_click_row(&mut self, index: usize, at: Point<gpui::Pixels>, cx: &mut Context<Self>) {
        if !self.bulk.contains(&index) {
            self.click_row(index, gpui::Modifiers::default(), cx);
        }
        self.menu_seq = self.menu_seq.wrapping_add(1);
        self.row_menu = Some(RowMenu {
            row: index,
            number: self.menu_seq,
            at,
        });
        cx.notify();
    }

    /// Keys of the selected entries of one kind, in sidebar order.
    fn target_keys(&self, kind: Kind) -> Vec<String> {
        keys_for_kind(&self.entries, &self.bulk, kind)
    }

    /// Opens the per-agent menu for one detail chip. The `⋯` button and a
    /// right click both land here.
    fn open_agent_menu(
        &mut self,
        key: String,
        agent: String,
        at: Point<gpui::Pixels>,
        cx: &mut Context<Self>,
    ) {
        self.menu_seq = self.menu_seq.wrapping_add(1);
        self.agent_menu = Some(AgentMenu {
            key,
            agent,
            number: self.menu_seq,
            at,
        });
        cx.notify();
    }

    /// Removes one entry from one agent without the picker. Skills keep the
    /// canonical copy; MCP keeps the hub config, so the next sync can restore
    /// the link.
    fn run_agent_action(&mut self, key: String, agent: String, kind: Kind, cx: &mut Context<Self>) {
        let command = match kind {
            Kind::Skill => {
                engine::remove_skill(self.scope, self.dir(), &key, false, Some(&[agent]))
            }
            Kind::Mcp => engine::remove_mcp(self.scope, self.dir(), &key, Some(&[agent]), true),
        };
        let done = if kind == Kind::Skill {
            "unlinked"
        } else {
            "removed"
        };
        match command {
            Ok(args) => self.run_batch(vec![args], done, cx),
            Err(error) => self.toast(error, cx),
        }
    }

    /// Union of the linked agents over the selected entries of one kind.
    fn target_agents(&self, kind: Kind, keys: &[String]) -> BTreeSet<String> {
        let mut out = BTreeSet::new();
        for entry in &self.entries {
            if entry.kind == kind && keys.iter().any(|key| key == &entry.key) {
                for link in &entry.links {
                    out.insert(link.agent.clone());
                }
            }
        }
        out
    }

    /// Opens the agent picker for the selected entries of one kind. With no
    /// links to pick from, Unlink has nothing to do and the other two go
    /// straight to their confirmation.
    fn open_picker(&mut self, action: PickerAction, kind: Kind, cx: &mut Context<Self>) {
        let keys = self.target_keys(kind);
        if keys.is_empty() {
            return;
        }
        let agents = self.target_agents(kind, &keys);
        if agents.is_empty() {
            match action {
                PickerAction::Unlink => {
                    self.toast("Nothing to unlink.", cx);
                    return;
                }
                PickerAction::Purge => self.ask_purge(keys, Vec::new(), cx),
                PickerAction::RemoveMcp => self.ask_remove_mcp(keys, Vec::new(), false, cx),
            }
            return;
        }
        self.picker = Some(AgentPicker {
            action,
            keys,
            checked: agents.clone(),
            agents,
        });
        cx.notify();
    }

    fn toggle_picker_agent(&mut self, agent: &str, on: bool, cx: &mut Context<Self>) {
        if let Some(picker) = self.picker.as_mut() {
            if on {
                picker.checked.insert(agent.to_string());
            } else {
                picker.checked.remove(agent);
            }
            cx.notify();
        }
    }

    /// The picker's Apply: close the picker, then confirm with the checked
    /// agents. The Ask is the one destructive gate; the picker only narrows it.
    fn apply_picker(&mut self, cx: &mut Context<Self>) {
        let Some(picker) = self.picker.take() else {
            return;
        };
        let agents: Vec<String> = picker.checked.into_iter().collect();
        match picker.action {
            PickerAction::Unlink => self.ask_unlink(picker.keys, agents, cx),
            PickerAction::Purge => self.ask_purge(picker.keys, agents, cx),
            PickerAction::RemoveMcp => self.ask_remove_mcp(picker.keys, agents, false, cx),
        }
    }

    fn ask_unlink(&mut self, keys: Vec<String>, agents: Vec<String>, cx: &mut Context<Self>) {
        let count = keys.len();
        let title = match keys.first() {
            Some(key) if count == 1 => format!("Unlink skill “{key}”?"),
            _ => format!("Unlink {count} skills?"),
        };
        let message = if agents.is_empty() {
            "Removes the agent links but keeps the canonical copy.".to_string()
        } else {
            format!(
                "Unlinks from {}. The canonical copy stays, so a sync can restore the links.",
                agents.join(", ")
            )
        };
        let confirm = if count == 1 {
            "Unlink".to_string()
        } else {
            format!("Unlink {count}")
        };
        self.ask_for(
            &title,
            &message,
            &confirm,
            Pending::Unlink { keys, agents },
            cx,
        );
    }

    fn ask_purge(&mut self, keys: Vec<String>, agents: Vec<String>, cx: &mut Context<Self>) {
        let count = keys.len();
        let title = match keys.first() {
            Some(key) if count == 1 => format!("Purge skill “{key}”?"),
            _ => format!("Purge {count} skills?"),
        };
        let message = if agents.is_empty() {
            "Removes the canonical copy and every agent link. This cannot be undone.".to_string()
        } else {
            format!(
                "Purges from {} and deletes the canonical copy. Unchecked links are left dangling. This cannot be undone.",
                agents.join(", ")
            )
        };
        let confirm = if count == 1 {
            "Purge".to_string()
        } else {
            format!("Purge {count}")
        };
        self.ask_for(
            &title,
            &message,
            &confirm,
            Pending::Purge { keys, agents },
            cx,
        );
    }

    fn ask_remove_mcp(
        &mut self,
        keys: Vec<String>,
        agents: Vec<String>,
        keep_hub: bool,
        cx: &mut Context<Self>,
    ) {
        let count = keys.len();
        let title = match keys.first() {
            Some(key) if count == 1 => format!("Remove MCP server “{key}”?"),
            _ => format!("Remove {count} MCP servers?"),
        };
        let message = if keep_hub {
            format!(
                "Removes it from {}. The hub config keeps it, so the next sync can restore it.",
                agents.join(", ")
            )
        } else if agents.is_empty() {
            "Removes it from the hub and every agent config in this scope.".to_string()
        } else {
            format!(
                "Removes it from the hub and from {}. Other configs keep their copy until the next sync.",
                agents.join(", ")
            )
        };
        let confirm = if count == 1 {
            "Remove".to_string()
        } else {
            format!("Remove {count}")
        };
        self.ask_for(
            &title,
            &message,
            &confirm,
            Pending::RemoveMcp {
                keys,
                agents,
                keep_hub,
            },
            cx,
        );
    }

    fn ask_sync_many(&mut self, target: Target, keys: Vec<String>, cx: &mut Context<Self>) {
        let count = keys.len();
        let what = match target {
            Target::Skills => "skills",
            Target::Mcp => "MCP servers",
            Target::All => "entries",
        };
        self.ask_for(
            &format!("Sync {count} {what}?"),
            "Applies the hub copy over the selected agent links, skipping conflicts.",
            &format!("Sync {count}"),
            Pending::SyncMany { target, keys },
            cx,
        );
    }

    /// The agent picker as an Ely dialog, drawn while it is open.
    fn picker_dialog(&self, cx: &mut Context<Self>) -> Option<gpui::AnyElement> {
        let picker = self.picker.as_ref()?;
        let entity = cx.entity();
        let count = picker.keys.len();
        let subject = match picker.keys.first() {
            Some(key) if count == 1 => key.clone(),
            _ => format!("{count} entries"),
        };
        let detail = match picker.action {
            PickerAction::Unlink => {
                "Only the checked agents lose their link; the canonical copy stays."
            }
            PickerAction::Purge => {
                "Unchecked agents keep their link, which dangles once the canonical copy is gone."
            }
            PickerAction::RemoveMcp => {
                "Unchecked agents keep their config until the next sync restores or removes it."
            }
        };
        let mut body = div().flex().flex_col().gap_1().w_full();
        for agent in &picker.agents {
            let agent = agent.clone();
            let state = if picker.checked.contains(&agent) {
                CheckState::On
            } else {
                CheckState::Off
            };
            body = body.child(
                Checkbox::new(SharedString::from(format!("picker-agent-{agent}")), state)
                    .label(agent.clone())
                    .on_change({
                        let (entity, agent) = (entity.clone(), agent.clone());
                        move |on, _, app| {
                            entity.update(app, |this, cx| this.toggle_picker_agent(&agent, on, cx))
                        }
                    }),
            );
        }
        let apply_label = format!("{} {count}", picker.action.noun());
        let picker_empty = picker.checked.is_empty();
        let mut dialog = Dialog::new("agent-picker", picker.action.noun(), {
            let entity = entity.clone();
            move |_, app| {
                entity.update(app, |this, cx| {
                    this.picker = None;
                    cx.notify();
                });
            }
        })
        .detail(format!("{subject} · {detail}"));
        let cancel = entity.clone();
        dialog = dialog.action(move |close| {
            Button::new("picker-cancel", "Cancel").on_click(move |_, window, cx| {
                close(window, cx);
                cancel.update(cx, |this, cx| {
                    this.picker = None;
                    cx.notify();
                });
            })
        });
        dialog = dialog.action(move |close| {
            Button::new("picker-apply", apply_label)
                .primary()
                .disabled(picker_empty)
                .on_click(move |_, window, cx| {
                    close(window, cx);
                    entity.update(cx, |this, cx| this.apply_picker(cx));
                })
        });
        Some(dialog.child(body).into_any_element())
    }
}

fn self_keep_selection(window: &mut SyncaWindow) {
    window.bulk.retain(|index| *index < window.entries.len());
    if window
        .anchor
        .is_some_and(|anchor| anchor >= window.entries.len())
    {
        window.anchor = None;
    }
    if window
        .row_menu
        .as_ref()
        .is_some_and(|menu| menu.row >= window.entries.len())
    {
        window.row_menu = None;
    }
    if window.selected.is_none() && !window.entries.is_empty() {
        window.selected = Some(0);
        window.anchor = Some(0);
    }
    if let Some(selected) = window.selected {
        window.bulk.insert(selected);
        window.anchor.get_or_insert(selected);
    }
}

enum After {
    Reload,
    Plan(String, Apply),
    Update,
}

impl PlanView {
    fn clone_apply(apply: &Apply) -> Apply {
        match apply {
            Apply::Sync { target, key } => Apply::Sync {
                target: *target,
                key: key.clone(),
            },
            Apply::Install(source) => Apply::Install(source.clone()),
            Apply::AddMcp {
                name,
                transport,
                command,
                url,
                enabled,
            } => Apply::AddMcp {
                name: name.clone(),
                transport: transport.clone(),
                command: command.clone(),
                url: url.clone(),
                enabled: *enabled,
            },
        }
    }
}

impl Clone for PlanView {
    fn clone(&self) -> Self {
        Self {
            title: self.title.clone(),
            output: self.output.clone(),
            conflicts: self.conflicts.clone(),
            chips: self.chips.clone(),
            apply: Self::clone_apply(&self.apply),
        }
    }
}

fn apply_now(
    scope: Scope,
    dir: Option<&str>,
    disabled: &[String],
    plan: &PlanView,
    log_path: &std::path::Path,
    scope_name: &str,
) -> String {
    let mut failures = 0;
    for conflict in &plan.conflicts {
        if conflict.policy == Policy::Skip {
            continue;
        }
        let Ok(args) = engine::resolve(scope, dir, conflict) else {
            failures += 1;
            continue;
        };
        let args = engine::with_agents(args, disabled);
        let result = ns_ui::run_tool("synca", &args);
        engine::activity_append(
            log_path,
            scope_name,
            &format!("synca {}", args.join(" ")),
            if result.ok { "ok" } else { "error" },
            result.code,
            &result.text(),
        );
        if !result.ok {
            failures += 1;
        }
    }
    let built = match &plan.apply {
        Apply::Sync { target, key } => engine::sync_apply(scope, dir, *target, key.as_deref()),
        Apply::Install(source) => engine::install_args(scope, dir, source, false),
        Apply::AddMcp {
            name,
            transport,
            command,
            url,
            enabled,
        } => engine::add_mcp(scope, dir, name, transport, command, url, *enabled, false),
    };
    match built {
        Ok(args) => {
            let args = engine::with_agents(args, disabled);
            let result = ns_ui::run_tool("synca", &args);
            engine::activity_append(
                log_path,
                scope_name,
                &format!("synca {}", args.join(" ")),
                if result.ok { "ok" } else { "error" },
                result.code,
                &result.text(),
            );
            if !result.ok {
                failures += 1;
            }
        }
        Err(error) => return error,
    }
    if failures == 0 {
        format!("Applied: {}", plan.title)
    } else {
        format!("{failures} step(s) failed — see Activity")
    }
}

impl Render for SyncaWindow {
    fn render(&mut self, window: &mut gpui::Window, cx: &mut Context<Self>) -> impl IntoElement {
        #[cfg(feature = "devtools")]
        let _clock = perf::clock();
        let selected = self.selected;
        let user_on = self.scope == Scope::User;
        let loading = self.busy && self.entries.is_empty();
        let focused = self.list_focus.is_focused(window);
        let mut list = div().flex().flex_col();
        if loading {
            list = list
                .child(
                    div()
                        .px(px(SIDEBAR_INSET))
                        .py(px(8.))
                        .child(working_row("inventory-wait", "Loading…")),
                )
                .child(skeleton_list("inventory-skeleton", 8));
        } else {
            // Only the rows near the viewport are built; the others keep their
            // height through spacers, so scrolling and reveal are unchanged.
            let view = f32::from(self.list_scroll.bounds().size.height);
            let seen = -f32::from(self.list_scroll.offset().y);
            let inventory = &self.inventory;
            let entries = &self.entries;
            let row_menu = self
                .row_menu
                .as_ref()
                .map(|menu| (menu.row, menu.number, menu.at));
            let skills_top = rows_origin(&[], inventory.skills.len());
            let mcps_top = rows_origin(&[inventory.skills.len()], inventory.mcps.len());
            list = inventory_group(
                list,
                Kind::Skill,
                "Skills",
                "No skills",
                &self.query,
                &inventory.skills,
                &inventory.skills_note,
                entries,
                inventory,
                rows_in_view(inventory.skills.len(), skills_top, view, seen),
                selected,
                &self.bulk,
                row_menu,
                focused,
                &self.list_focus,
                cx,
            );
            list = inventory_group(
                list,
                Kind::Mcp,
                "MCP",
                "No MCP servers",
                &self.query,
                &inventory.mcps,
                &inventory.mcps_note,
                entries,
                inventory,
                rows_in_view(inventory.mcps.len(), mcps_top, view, seen),
                selected,
                &self.bulk,
                row_menu,
                focused,
                &self.list_focus,
                cx,
            );
        }
        let mut sidebar = div()
            .relative()
            .w_full()
            .flex_1()
            .min_h(px(0.))
            .flex()
            .flex_col()
            .child(div().flex_shrink(0.).w_full().child(tab_strip(
                "scope-tabs",
                [
                    ("user", "user", "User"),
                    ("workspace", "folder", "Workspace"),
                ],
                if user_on { "user" } else { "workspace" },
                cx.listener(|this, value: &SharedString, _, cx| {
                    this.scope = if value.as_ref() == "user" {
                        Scope::User
                    } else {
                        Scope::Project
                    };
                    this.reload(cx);
                }),
            )));
        if !user_on {
            sidebar = sidebar.child(workspace_trigger(self, cx));
        }
        sidebar = sidebar.child(inventory_search(self));
        sidebar = sidebar.child(
            div()
                .flex_1()
                .min_h(px(0.))
                .flex()
                .flex_col()
                .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, window, cx| {
                    if this.list_focus.is_focused(window) {
                        this.on_list_modified_key(event, cx);
                    }
                }))
                .child(sidebar_keys(
                    "synca-sidebar",
                    &self.list_focus,
                    sidebar_scroll_tracked(&self.list_scroll, list),
                    cx.listener(|this, key: &SidebarKey, _, cx| this.on_list_key(*key, cx)),
                )),
        );
        sidebar = sidebar.child(sync_actions(cx));

        let title = match self.route {
            Route::List => self
                .selected
                .and_then(|index| self.entries.get(index))
                .map(|entry| entry.title.clone())
                .filter(|title| !title.is_empty())
                .unwrap_or_else(|| self.scope_title().to_string()),
            Route::Plan => "Plan".into(),
            Route::Update => "Update".into(),
        };

        let mut body = div().flex().flex_col().gap_2();
        if self.ask.is_some() && !panel_owns_ask(self.panel, self.pending.as_ref()) {
            body = with_ask(self, body, cx);
        }
        body = match self.route {
            Route::List => list_body(self, body, cx),
            Route::Plan => plan_body(self, body, cx),
            Route::Update => update_body(self, body, cx),
        };
        let panel = match self.panel {
            Panel::Closed => None,
            Panel::Activity => {
                let column = activity_body(self, div().flex().flex_col().gap_2(), cx);
                Some(
                    side_column("Activity", column)
                        .w(px(SIDE_PANE_W))
                        .into_any_element(),
                )
            }
            Panel::Settings => {
                let column = settings_body(self, div().flex().flex_col().gap_2(), cx);
                Some(
                    side_column("Settings", column)
                        .w(px(SIDE_PANE_W))
                        .into_any_element(),
                )
            }
        };
        let rail = rail(self, cx).into_any_element();
        let meta = if self.version.is_empty() {
            self.scope_title().to_string()
        } else {
            format!("{} · {}", self.scope_title(), self.version)
        };
        if self.busy && !self.entries.is_empty() {
            body = body.child(working_row("synca-wait", synca_wait(self)));
        }
        #[cfg(feature = "devtools")]
        if let Some(key) = self.dev_key.take() {
            window.defer(cx, move |window, cx| {
                if let Ok(keystroke) = gpui::Keystroke::parse(&key) {
                    window.dispatch_keystroke(keystroke, cx);
                }
            });
        }
        #[cfg(feature = "devtools")]
        if let Some(drag) = self.dev_drag.take() {
            window.defer(cx, move |window, cx| dispatch_dev_drag(window, drag, cx));
        }
        #[cfg(not(feature = "devtools"))]
        let _ = &window;
        let onboarding = self.onboarding.map(|tab| {
            let entity = cx.entity();
            let close = move |window: &mut gpui::Window, app: &mut gpui::App| {
                let _ = window;
                entity.update(app, |this, cx| {
                    this.onboarding = None;
                    cx.notify();
                });
            };
            let strip = tab_strip_equal(
                "install-tabs",
                [("skill", "skill", "Skill"), ("mcp", "plug", "MCP server")],
                tab.key(),
                cx.listener(|this, value: &SharedString, _, cx| {
                    this.pick_install_tab(InstallTab::from_key(value), cx);
                }),
            );
            let start = div().flex().flex_col().gap_2().child(strip);
            let body = match tab {
                InstallTab::Skill => install_body(self, start, cx),
                InstallTab::Mcp => mcp_body(self, start, cx),
            };
            let mut dialog = Dialog::new("synca-onboarding", "Install", close).detail(tab.detail());
            // Ely's card is a fixed 440px; a narrow window gets the full-width form.
            if ns_ui::is_compact() {
                dialog = dialog.fullscreen();
            }
            dialog.child(body).into_any_element()
        });
        let picker = self.picker_dialog(cx);
        let toasts = ToastViewport::new("synca-toasts", &self.toaster);
        div()
            .size_full()
            .child(chrome_busy_wide(
                sidebar,
                title,
                body,
                None,
                panel,
                Some(rail),
                if self.busy {
                    "Working".into()
                } else {
                    self.status.clone()
                },
                self.busy,
                meta,
                None,
            ))
            .children(onboarding)
            .children(picker)
            .child(toasts)
    }
}

/// Names the feature that set `busy`. Reload says Loading, a plan says Applying,
/// and a command keeps the generic Working the status bar already uses.
fn synca_wait(window: &SyncaWindow) -> &'static str {
    if window.status == "Loading" {
        "Loading…"
    } else if window.route == Route::Plan {
        "Applying…"
    } else {
        "Working…"
    }
}

/// Activity and Settings. The shared column is 320px; this is wider so a
/// command has room, and the row still truncates inside the card.
const SIDE_PANE_W: f32 = 400.;

/// The footer actions share one three-column row: Install…, Reload and Sync
/// all get equal widths and a label. The bar is pinned under the list so the
/// rows can use the height above it.
fn sync_actions(cx: &mut Context<SyncaWindow>) -> impl IntoElement + use<> {
    let theme = theme();
    div()
        .flex_shrink_0()
        .w_full()
        .px(px(SIDEBAR_INSET))
        .py(px(SIDEBAR_INSET))
        .flex()
        .items_center()
        .gap(px(4.))
        .border_t_1()
        .border_color(theme.border)
        .child(sync_cell(sidebar_button(
            "install",
            "download",
            "Install…",
            "Install a skill or add an MCP server",
            Some("⌘1 Skill · ⌘2 MCP"),
            cx.listener(|this, _, _, cx| this.open_install(InstallTab::Skill, cx)),
        )))
        .child(sync_cell(sidebar_button(
            "reload",
            "refresh",
            "Reload",
            "Reload",
            None,
            cx.listener(|this, _, _, cx| this.reload(cx)),
        )))
        .child(sync_cell(sidebar_button(
            "sync-all",
            "sync",
            "Sync all",
            "Sync all (preview first)",
            None,
            cx.listener(|this, _, _, cx| this.plan_sync(Target::All, None, cx)),
        )))
}

/// One third of the footer row.
fn sync_cell(element: impl IntoElement) -> impl IntoElement {
    div().flex_1().min_w(px(0.)).child(element)
}

/// One field for both sidebar groups. Typing filters titles and keys; Ely's
/// `TextInput` owns the caret, selection, and undo history.
fn inventory_search(window: &SyncaWindow) -> impl IntoElement + use<> {
    div()
        .flex_shrink_0()
        .w_full()
        .px(px(SIDEBAR_INSET))
        .pt(px(8.))
        .pb(px(4.))
        .child(Input::new(&window.query_input))
}

/// The workspace picker: an Ely `Popover` whose panel lists the projects.
///
/// The opener is this app's own face, so the popover keeps its own open state
/// and closes on Escape, a press outside, or a chosen row. The panel is
/// built inside the popover's content run, so it exists only while the
/// popover is open; the popover's close run reaches it through a slot.
fn workspace_trigger(window: &mut SyncaWindow, cx: &mut Context<SyncaWindow>) -> impl IntoElement {
    let current = window
        .active_project
        .as_deref()
        .map(ns_ui::path_name)
        .unwrap_or_else(|| "Select workspace".into());
    let tip: SharedString = window
        .active_project
        .as_deref()
        .map(ns_ui::display_path)
        .unwrap_or_else(|| "Choose a workspace folder".into())
        .into();
    let named = window.active_project.is_some();
    let entity = cx.entity();
    let close_slot: CloseSlot = Rc::new(RefCell::new(None));
    div()
        .flex_shrink(0.)
        .h(px(32.))
        .px(px(SIDEBAR_INSET))
        .pt(px(4.))
        .flex()
        .items_center()
        .gap(px(4.))
        .child(
            div()
                .id("workspace-face-tip")
                .flex_1()
                .min_w(px(0.))
                .tooltip(Tooltip::text(tip))
                .child(Popover::with_opener(
                    "workspace-popover",
                    move |toggle| {
                        Button::new("workspace-face", current.clone())
                            .variant(if named {
                                ButtonVariant::Secondary
                            } else {
                                ButtonVariant::Ghost
                            })
                            .size(ControlSize::Md)
                            .icon(IconName::Folder)
                            .trailing_icon(IconName::ChevronsUpDown)
                            .on_click(move |_, window, cx| toggle(window, cx))
                    },
                    // The panel is built here, which the popover runs only
                    // while it is open; a closed popover costs no rows.
                    move |close, _window, app| {
                        *close_slot.borrow_mut() = Some(close);
                        workspace_panel(&entity, close_slot.clone(), app)
                    },
                )),
        )
        .child(icon_button_md(
            "add-workspace",
            "folder-plus",
            "Add a workspace folder",
            IconTone::Quiet,
            cx.listener(|_this, _, _, cx| {
                cx.spawn(async move |this, cx| {
                    let picked = cx
                        .background_spawn(async {
                            ns_ui::choose_folder("Choose a workspace folder")
                        })
                        .await;
                    this.update(cx, |this, cx| {
                        if let Some(path) = picked {
                            this.add_project(path.trim_end_matches('/').to_string(), cx);
                        }
                    })
                    .ok();
                })
                .detach();
            }),
        ))
}

/// The popover's close run, filled in when the panel is built.
type CloseSlot = Rc<RefCell<Option<Rc<dyn Fn(&mut Window, &mut App)>>>>;

/// The popover's panel, built only while it is open: one row per project, each
/// with a remove control at its end. The rows read the window through `entity`,
/// so nothing about the panel is built while it is closed.
///
/// Ely's `ListItem` is a large control (`px_3`, `py_1p5`, large min height) and
/// the popover already wraps this panel in `p_3`. These rows match the sidebar
/// instead, so the menu is not a padded card of tall rows.
fn workspace_panel(
    entity: &Entity<SyncaWindow>,
    close: CloseSlot,
    app: &mut App,
) -> impl IntoElement + use<> {
    let (projects, active) = {
        let window = entity.read(app);
        (window.projects.clone(), window.active_project.clone())
    };
    let theme = theme();
    let mut panel = div()
        .id("workspace-panel")
        .flex()
        .flex_col()
        .gap(px(2.))
        .min_w(px(240.))
        .max_h(px(280.))
        .overflow_y_scroll();
    if projects.is_empty() {
        panel = panel.child(
            div()
                .px(px(8.))
                .py(px(4.))
                .text_xs()
                .text_color(theme.muted)
                .child("No workspaces"),
        );
    }
    for (index, path) in projects.into_iter().enumerate() {
        let picked = active.as_ref() == Some(&path);
        let (name, parent) = workspace_parts(&path);
        panel = panel.child(workspace_row(
            index, name, parent, path, picked, entity, &close,
        ));
    }
    panel
}

fn workspace_row(
    index: usize,
    name: String,
    parent: String,
    path: String,
    picked: bool,
    entity: &Entity<SyncaWindow>,
    close: &CloseSlot,
) -> impl IntoElement {
    let theme = theme();
    let select_path = path.clone();
    let remove_path = path;
    let close_pick = close.clone();
    let pick_entity = entity.clone();
    let remove_entity = entity.clone();
    div()
        .id(SharedString::from(format!("ws-{index}")))
        .h(px(32.))
        .pl(px(8.))
        .pr(px(2.))
        .flex()
        .items_center()
        .gap(px(8.))
        .rounded_md()
        .cursor_pointer()
        .text_sm()
        .text_color(theme.text)
        .when(picked, |row| row.bg(theme.selected))
        .when(!picked, |row| row.hover(|style| style.bg(theme.selected)))
        .on_click(move |_, window, cx| {
            pick_entity.update(cx, |this, cx| {
                this.active_project = Some(select_path.clone());
                engine::save_projects(&this.projects, this.active_project.as_deref());
                if let Some(close) = close_pick.borrow().as_ref() {
                    close(window, cx);
                }
                this.reload(cx);
            });
        })
        .child(div().flex_none().child(icon("folder")))
        .child(
            div()
                .flex_1()
                .min_w(px(0.))
                .flex()
                .items_baseline()
                .gap(px(8.))
                .overflow_hidden()
                .whitespace_nowrap()
                .child(div().min_w(px(24.)).flex_shrink_1().truncate().child(name))
                .when(!parent.is_empty(), |row| {
                    row.child(
                        div()
                            .min_w(px(0.))
                            .flex_shrink(8.)
                            .truncate()
                            .text_xs()
                            .text_color(theme.muted)
                            .child(parent),
                    )
                }),
        )
        .child(icon_button(
            SharedString::from(format!("ws-rm-{index}")),
            "trash",
            "Remove",
            IconTone::Danger,
            move |_, _, cx| {
                cx.stop_propagation();
                remove_entity.update(cx, |this, cx| {
                    this.projects.retain(|item| item != &remove_path);
                    if this.active_project.as_ref() == Some(&remove_path) {
                        this.active_project = this.projects.first().cloned();
                    }
                    engine::save_projects(&this.projects, this.active_project.as_deref());
                    if this.scope == Scope::Project {
                        this.reload(cx);
                    } else {
                        cx.notify();
                    }
                });
            },
        ))
}

fn workspace_parts(path: &str) -> (String, String) {
    let trimmed = path.trim_end_matches('/');
    match trimmed.rsplit_once('/') {
        Some((parent, name)) if !name.is_empty() => (name.to_string(), parent.to_string()),
        _ => (trimmed.to_string(), String::new()),
    }
}

fn shown_scope(scope: &str) -> &str {
    match scope {
        "project" | "Project" => "workspace",
        other => other,
    }
}

/// How a skill or MCP detail pane uses the editor width, padding already removed.
#[derive(Clone, Copy, Debug, PartialEq)]
struct DetailLayout {
    /// Stat cards per row: 1, 2, or 4.
    card_cols: u32,
    /// File tree above the preview.
    files_stacked: bool,
    /// Agents card beside the stat cards.
    agents_beside: bool,
    tree_w: f32,
    /// Tree height when it sits above the preview.
    tree_h: f32,
    file_pane_h: f32,
}

fn detail_content_width() -> f32 {
    (ns_ui::body_width() - 24.).max(240.)
}

fn detail_layout(content: f32) -> DetailLayout {
    if content < 460. {
        DetailLayout {
            card_cols: 1,
            files_stacked: true,
            agents_beside: false,
            tree_w: 0.,
            tree_h: 148.,
            file_pane_h: 440.,
        }
    } else if content < 640. {
        DetailLayout {
            card_cols: 2,
            files_stacked: true,
            agents_beside: false,
            tree_w: 0.,
            tree_h: 160.,
            file_pane_h: 480.,
        }
    } else if content < 800. {
        DetailLayout {
            card_cols: 2,
            files_stacked: false,
            agents_beside: false,
            tree_w: 200.,
            tree_h: 0.,
            file_pane_h: 460.,
        }
    } else if content < 1000. {
        DetailLayout {
            card_cols: 4,
            files_stacked: false,
            agents_beside: false,
            tree_w: 220.,
            tree_h: 0.,
            file_pane_h: 480.,
        }
    } else {
        DetailLayout {
            card_cols: 4,
            files_stacked: false,
            agents_beside: true,
            tree_w: 260.,
            tree_h: 0.,
            file_pane_h: 520.,
        }
    }
}

/// Width of one stat card so `cols` of them, plus 8px gaps, fit in `area`.
fn stat_card_width(area: f32, cols: u32) -> f32 {
    let cols = cols.max(1) as f32;
    let gap = 8. * (cols - 1.);
    ((area - gap) / cols).floor() - 1.
}

fn list_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let selected = window
        .selected
        .filter(|index| window.entries.get(*index).is_some());
    let Some(index) = selected else {
        if window.busy {
            return body.child(loading_pane("detail-wait", "Loading the inventory…"));
        }
        return body.child(
            if window.scope == Scope::Project && window.dir().is_none() {
                EmptyState::new("list-empty", IconName::Folder, "No workspace selected")
                    .body("Pick or add a workspace folder to list its skills and MCP servers.")
            } else if window.entries.is_empty() {
                EmptyState::new("list-empty", IconName::Inbox, "Nothing here yet")
                    .body("Use Install… to add a skill or an MCP server.")
            } else {
                EmptyState::new(
                    "list-empty",
                    IconName::Package,
                    "Select a skill or an MCP server",
                )
            },
        );
    };
    let entry = &window.entries[index];
    let theme = theme();
    let state_badge = entry
        .mismatch
        .then(|| chip("Out of sync", Tone::Warn).into_any_element());
    let kind_label = if entry.kind == Kind::Skill {
        "Skill"
    } else {
        "MCP server"
    };
    let scope_label = if window.scope == Scope::Project {
        "workspace"
    } else {
        "user"
    };
    let location = entry
        .paths
        .first()
        .map(|path| ns_ui::display_path(path))
        .unwrap_or_else(|| "not installed".into());
    let content_w = detail_content_width();
    let layout = detail_layout(content_w);
    let stats_w = if layout.agents_beside {
        (content_w - 8. - 280.).max(240.)
    } else {
        content_w
    };
    let card_w = stat_card_width(stats_w, layout.card_cols);

    body = body.child(
        div()
            .w_full()
            .flex()
            .items_start()
            .gap_3()
            .pb_3()
            .child(
                div()
                    .w(px(38.))
                    .h(px(38.))
                    .flex_shrink_0()
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded_md()
                    .bg(theme.selected)
                    .child(icon(if entry.kind == Kind::Skill {
                        "book-open"
                    } else {
                        "plug"
                    })),
            )
            .child(
                div()
                    .flex_1()
                    .min_w(px(0.))
                    .flex()
                    .flex_col()
                    .gap_1()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .flex_wrap()
                            .child(
                                div()
                                    .min_w(px(0.))
                                    .flex_1()
                                    .whitespace_normal()
                                    .text_lg()
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .child(selectable(
                                        format!("detail-title-{index}"),
                                        entry.title.clone(),
                                    )),
                            )
                            .children(state_badge),
                    )
                    .child(
                        div()
                            .whitespace_normal()
                            .text_xs()
                            .text_color(theme.muted)
                            .child(selectable(
                                format!("detail-meta-{index}"),
                                format!("{kind_label} · {scope_label} scope · {location}"),
                            )),
                    ),
            ),
    );
    body = entry_actions(body, entry, cx);
    let agent_menu = window.agent_menu.as_ref().and_then(|menu| {
        (menu.key == entry.key).then_some((menu.agent.as_str(), menu.number, menu.at))
    });
    let synced_tone = match entry.synced {
        Some(synced) if !entry.links.is_empty() && synced == entry.links.len() => Some(theme.ok),
        Some(_) if !entry.links.is_empty() => Some(theme.warn),
        _ => None,
    };
    let mut stats = div()
        .flex()
        .flex_wrap()
        .gap(px(8.))
        .child(detail_card(
            format!("detail-scope-{index}"),
            "Scope",
            scope_label.into(),
            None,
            None,
            card_w,
            theme,
        ))
        .child(if entry.mismatch {
            detail_card(
                format!("detail-sync-{index}"),
                "Sync state",
                "Out of sync".into(),
                Some(theme.warn),
                None,
                card_w,
                theme,
            )
        } else {
            detail_card(
                format!("detail-sync-{index}"),
                "Sync state",
                "In sync".into(),
                Some(theme.ok),
                None,
                card_w,
                theme,
            )
        })
        .child(detail_card(
            format!("detail-synced-{index}"),
            "Synced agents",
            synced_agents_label(entry),
            synced_tone,
            Some(SharedString::from(
                "Agent links whose copy matches the hub. A dash means the CLI did not report the hub hash.",
            )),
            card_w,
            theme,
        ))
        .child(detail_card(
            format!("detail-tokens-{index}"),
            "Tokens",
            entry_tokens_label(entry).to_string(),
            None,
            entry.tokens.map(|_| {
                SharedString::from(match entry.kind {
                    Kind::Skill => "Estimated tokens: SKILL.md bytes / 4.",
                    Kind::Mcp => "Estimated tokens: the server's config JSON bytes / 4.",
                })
            }),
            card_w,
            theme,
        ));
    stats = if layout.agents_beside {
        stats.flex_1().min_w(px(0.))
    } else {
        stats.w_full()
    };
    let mut facts = div()
        .w_full()
        .flex()
        .gap(px(8.))
        .child(stats)
        .child(agents_card(
            entry,
            agent_menu,
            theme,
            layout.agents_beside,
            cx,
        ));
    facts = if layout.agents_beside {
        facts.items_stretch()
    } else {
        facts.flex_col()
    };
    body = body.child(facts);
    if !entry.description.is_empty() {
        body = body.child(
            div()
                .w_full()
                .flex()
                .flex_col()
                .gap_1()
                .p_3()
                .rounded_md()
                .bg(theme.raised)
                .child(
                    div()
                        .text_xs()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(theme.muted)
                        .child("ABOUT"),
                )
                .child(div().text_sm().child(selectable(
                    format!("detail-about-{index}"),
                    entry.description.clone(),
                ))),
        );
    }
    let skill = entry.kind == Kind::Skill;
    if skill {
        // The tree, the default file and its preview are read once per
        // selection; a repaint reuses them.
        let key = (window.entries_gen, index);
        if window.skill_files.key != key {
            let paths = window.entries[index].paths.clone();
            window.skill_files = SkillFilesCache::collect(&paths, key);
        }
    }
    if skill && window.skill_files.root.is_some() {
        let files = &window.skill_files;
        body = body.child(
            div()
                .w_full()
                .flex_shrink_0()
                .flex()
                .flex_col()
                .pt_2()
                .h(px(layout.file_pane_h))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap_2()
                        .pb_2()
                        .child(div().w(px(3.)).h(px(14.)).rounded_sm().bg(theme.accent))
                        .child(
                            div()
                                .text_xs()
                                .font_weight(FontWeight::SEMIBOLD)
                                .text_color(theme.muted)
                                .child("FILES"),
                        ),
                )
                .child(div().flex_1().min_h(px(0.)).child(skill_files(
                    files,
                    &window.file_path,
                    &window.file_preview,
                    &window.file_scroll,
                    &mut window.markdown,
                    cx,
                ))),
        );
    }
    body
}

/// Agents the entry is installed for: the count in the title, one chip each.
/// A chip carries a status dot (synced, drift, unknown) and opens a menu with
/// the per-agent actions; the `⋯` button and a right click both open it.
fn agents_card(
    entry: &Entry,
    menu: Option<(&str, u64, Point<gpui::Pixels>)>,
    theme: ns_ui::Theme,
    beside: bool,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let mut tags = div().pt_1().flex().flex_wrap().gap_1();
    if entry.links.is_empty() {
        tags = tags.child(
            div()
                .text_sm()
                .text_color(theme.muted)
                .child("Not installed"),
        );
    }
    // Neutral badge colors: Ely's `Tone::colors` is crate-private, and these
    // are the pair it uses for `Tone::Neutral`.
    let (hover, fg_muted) = {
        let colors = &cx.theme().colors;
        (colors.hover, colors.fg_muted)
    };
    for (index, link) in entry.links.iter().enumerate() {
        let (dot, dot_tip) = match link.synced {
            Some(true) => (theme.ok, "Matches the hub"),
            Some(false) => (theme.warn, "Drifted from the hub"),
            None => (theme.muted, "Sync state unknown"),
        };
        let request = menu
            .as_ref()
            .filter(|(agent, _, _)| *agent == link.agent)
            .map(|(_, number, at)| (*number, *at));
        let chip = div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(5.))
            .px(px(6.))
            .h(px(20.))
            .rounded_sm()
            .bg(hover)
            .text_xs()
            .font_weight(FontWeight::MEDIUM)
            .text_color(fg_muted)
            .child(
                div()
                    .id(SharedString::from(format!("agent-dot-{index}")))
                    .size(px(6.))
                    .rounded_full()
                    .bg(dot)
                    .tooltip(Tooltip::text(dot_tip)),
            )
            .child(selectable(
                format!("detail-agent-{index}"),
                link.agent.clone(),
            ))
            .child({
                let (key, agent, agent_label) =
                    (entry.key.clone(), link.agent.clone(), link.agent.clone());
                icon_button(
                    SharedString::from(format!("agent-menu-{index}")),
                    "ellipsis",
                    format!("Actions for {agent_label}"),
                    IconTone::Quiet,
                    cx.listener(move |this, event: &gpui::ClickEvent, _, cx| {
                        let at = event.position();
                        this.open_agent_menu(key.clone(), agent.clone(), at, cx);
                    }),
                )
            });
        let (host_key, host_agent) = (entry.key.clone(), link.agent.clone());
        let host = div()
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(move |this, event: &gpui::MouseDownEvent, _, cx| {
                    this.open_agent_menu(host_key.clone(), host_agent.clone(), event.position, cx);
                }),
            )
            .child(chip);
        let chip_menu = agent_menu_for(entry.kind, &entry.key, link, cx);
        tags = tags.child(
            ContextMenu::new(
                SharedString::from(format!("agent-menu-host-{index}")),
                chip_menu,
            )
            .manual(request)
            .child(host),
        );
    }
    let mut card = div().p_2().rounded_md().bg(theme.raised);
    card = if beside {
        card.w(px(280.)).flex_shrink_0()
    } else {
        card.w_full()
    };
    card.child(
        div()
            .text_xs()
            .text_color(theme.muted)
            .child(format!("Agents ({})", entry.links.len())),
    )
    .child(tags)
}

/// The per-agent chip menu: remove just this agent's copy, then copy or
/// reveal the agent's own path.
fn agent_menu_for(
    kind: Kind,
    key: &str,
    link: &AgentLink,
    cx: &mut Context<SyncaWindow>,
) -> ElyMenu {
    let entity = cx.entity();
    let mut menu = ElyMenu::new().item(match kind {
        Kind::Skill => {
            let (entity, key, agent) = (entity, key.to_string(), link.agent.clone());
            ElyMenuItem::new(format!("Unlink from {}", link.agent))
                .icon(IconName::Link)
                .on_click(move |_, app| {
                    entity.update(app, |this, cx| {
                        this.run_agent_action(key.clone(), agent.clone(), Kind::Skill, cx)
                    })
                })
        }
        Kind::Mcp => {
            let (entity, key, agent) = (entity, key.to_string(), link.agent.clone());
            ElyMenuItem::new(format!("Remove from {}", link.agent))
                .icon(IconName::Link)
                .on_click(move |_, app| {
                    entity.update(app, |this, cx| {
                        this.run_agent_action(key.clone(), agent.clone(), Kind::Mcp, cx)
                    })
                })
        }
    });
    menu = menu.separator().item({
        let copy = link.path.clone();
        ElyMenuItem::new("Copy path")
            .icon(IconName::Copy)
            .on_click(move |_, _| {
                ns_ui::copy_text(&copy);
            })
    });
    menu = menu.item({
        let reveal = link.path.clone();
        ElyMenuItem::new("Reveal in Finder")
            .icon(IconName::FolderOpen)
            .on_click(move |_, _| {
                ns_ui::reveal(&reveal);
            })
    });
    menu
}

fn detail_card(
    id: impl Into<ElementId>,
    label: &str,
    value: String,
    tone: Option<gpui::Rgba>,
    tip: Option<SharedString>,
    width: f32,
    theme: ns_ui::Theme,
) -> impl IntoElement {
    let text_id = id.into();
    let mut card = div()
        .id(SharedString::from(format!("card-{text_id}")))
        .w(px(width))
        .flex_shrink_0()
        .p_2()
        .rounded_md()
        .bg(theme.raised)
        .child(
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(label.to_string()),
        )
        .child(
            div()
                .pt_1()
                .text_sm()
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(tone.unwrap_or(theme.text))
                .child(selectable(text_id, value)),
        );
    if let Some(tip) = tip {
        card = card.tooltip(Tooltip::text(tip));
    }
    card
}

/// Detail copy the pointer can select. The parent sets size, weight, and color;
/// `SelectableText` draws the string and keeps the drag selection.
fn selectable(id: impl Into<ElementId>, text: impl Into<SharedString>) -> SelectableText {
    SelectableText::new(id, text)
}

/// One primary action; the rest are quiet. A destructive action sits after a
/// divider and is an outline, so it never reads as the main button. The
/// destructive ones open the agent picker over the whole selection.
fn entry_actions(body: gpui::Div, entry: &Entry, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let name = entry.key.clone();
    let kind = entry.kind;
    let theme = theme();
    let divider = || div().w(px(1.)).h(px(20.)).mx_1().bg(theme.border);
    let row = div().w_full().flex().items_center().gap_2().flex_wrap();
    if kind == Kind::Skill {
        let sync_name = name;
        body.child(
            row.child(button(
                "sync-one",
                "sync",
                "Sync this",
                cx.listener(move |this, _, _, cx| {
                    this.plan_sync(Target::Skills, Some(sync_name.clone()), cx)
                }),
            ))
            .child(quiet_button(
                "unlink",
                "unlink",
                "Unlink…",
                cx.listener(move |this, _, _, cx| {
                    this.open_picker(PickerAction::Unlink, kind, cx);
                }),
            ))
            .child(divider())
            .child(outline_button(
                "purge",
                "trash",
                "Purge…",
                cx.listener(move |this, _, _, cx| {
                    this.open_picker(PickerAction::Purge, kind, cx);
                }),
            )),
        )
    } else {
        let sync_name = name;
        body.child(
            row.child(button(
                "sync-mcp",
                "sync",
                "Sync this",
                cx.listener(move |this, _, _, cx| {
                    this.plan_sync(Target::Mcp, Some(sync_name.clone()), cx)
                }),
            ))
            .child(divider())
            .child(outline_button(
                "remove-mcp",
                "trash",
                "Remove…",
                cx.listener(move |this, _, _, cx| {
                    this.open_picker(PickerAction::RemoveMcp, kind, cx);
                }),
            )),
        )
    }
}

struct TreeItem {
    name: String,
    path: String,
    dir: bool,
    depth: u32,
}

fn collect_tree(dir: &std::path::Path, depth: u32, out: &mut Vec<TreeItem>) {
    if depth > 5 || out.len() >= 80 {
        return;
    }
    let Ok(read) = std::fs::read_dir(dir) else {
        return;
    };
    let mut entries: Vec<_> = read.flatten().collect();
    entries.sort_by(|left, right| {
        let left_dir = left.path().is_dir();
        let right_dir = right.path().is_dir();
        right_dir
            .cmp(&left_dir)
            .then_with(|| left.file_name().cmp(&right.file_name()))
    });
    for entry in entries {
        if out.len() >= 80 {
            return;
        }
        let path = entry.path();
        let Some(name) = path.file_name().and_then(|name| name.to_str()) else {
            continue;
        };
        if name.is_empty() || name.starts_with('.') {
            continue;
        }
        let dir = path.is_dir();
        out.push(TreeItem {
            name: name.to_string(),
            path: path.to_string_lossy().into_owned(),
            dir,
            depth,
        });
        if dir {
            collect_tree(&path, depth + 1, out);
        }
    }
}

/// The flat, depth-ordered `TreeItem`s as nested Ely nodes keyed by path.
fn tree_nodes(items: &[TreeItem]) -> Vec<TreeNode> {
    fn build(items: &[TreeItem], from: usize, depth: u32) -> (Vec<TreeNode>, usize) {
        let mut out = Vec::new();
        let mut index = from;
        while index < items.len() && items[index].depth >= depth {
            let item = &items[index];
            if item.depth > depth {
                index += 1;
                continue;
            }
            let mut node = TreeNode::new(item.path.clone(), item.name.clone()).icon(if item.dir {
                IconName::Folder
            } else {
                IconName::File
            });
            index += 1;
            if item.dir {
                let (children, next) = build(items, index, depth + 1);
                node = node.children(children);
                index = next;
            }
            out.push(node);
        }
        (out, index)
    }
    build(items, 0, 0).0
}

/// Folders to show open: every one above the open file, so it is visible.
fn open_folders(root: &str, open: &str, items: &[TreeItem]) -> Vec<String> {
    items
        .iter()
        .filter(|item| {
            item.dir && (open.starts_with(&format!("{}/", item.path)) || item.depth == 0)
        })
        .map(|item| item.path.clone())
        .chain(std::iter::once(root.to_string()))
        .collect()
}

fn chosen_file(root: &str, current: &str, items: &[TreeItem]) -> String {
    if items.iter().any(|item| !item.dir && item.path == current) {
        return current.to_string();
    }
    let skill = std::path::Path::new(root)
        .join("SKILL.md")
        .to_string_lossy()
        .into_owned();
    if items.iter().any(|item| item.path == skill) {
        return skill;
    }
    items
        .iter()
        .find(|item| !item.dir)
        .map(|item| item.path.clone())
        .unwrap_or_default()
}

fn relative_label(root: &str, path: &str) -> String {
    std::path::Path::new(path)
        .strip_prefix(root)
        .map(|path| path.to_string_lossy().into_owned())
        .unwrap_or_else(|_| path.to_string())
}

/// Command output the pointer can select and copy, in the same frame
/// `text_block` draws. `clean` already had its ANSI codes stripped by the
/// caller's cache; the clone is a refcount bump.
fn output_block(id: &str, clean: &SharedString) -> impl IntoElement + use<> {
    let theme = theme();
    div()
        .p_3()
        .rounded_md()
        .bg(theme.raised)
        .border_1()
        .border_color(theme.border)
        .text_xs()
        .text_color(theme.text)
        .child(SelectableText::new(
            SharedString::from(id.to_string()),
            if clean.is_empty() {
                SharedString::from(" ")
            } else {
                clean.clone()
            },
        ))
}

/// The stripped form of `source`, computed on first use. Every site that
/// changes `source` clears `cache`.
fn clean_output(cache: &mut Option<SharedString>, source: &str) -> SharedString {
    cache
        .get_or_insert_with(|| SharedString::from(ns_ui::strip_ansi(source)))
        .clone()
}

fn split_frontmatter(text: &str) -> (Vec<(String, String)>, String) {
    let normalized = text.replace("\r\n", "\n");
    let Some(rest) = normalized.strip_prefix("---\n") else {
        return (Vec::new(), text.to_string());
    };
    let Some(end) = rest.find("\n---\n") else {
        return (Vec::new(), text.to_string());
    };
    let frontmatter = &rest[..end];
    let body = &rest[end + "\n---\n".len()..];
    let mut rows = Vec::new();
    let mut current: Option<(String, String)> = None;
    for line in frontmatter.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        if let Some((key, value)) = trimmed.split_once(':') {
            if let Some(row) = current.take() {
                rows.push(row);
            }
            current = Some((key.trim().to_string(), value.trim().to_string()));
        } else if let Some((_, value)) = current.as_mut() {
            if !value.is_empty() {
                value.push(' ');
            }
            value.push_str(trimmed);
        }
    }
    if let Some(row) = current {
        rows.push(row);
    }
    (rows, body.to_string())
}

fn metadata_table(rows: &[(String, String)]) -> gpui::AnyElement {
    let theme = theme();
    let last = rows.len().saturating_sub(1);
    let mut table = div()
        .w_full()
        .flex()
        .flex_col()
        .rounded_md()
        .border_1()
        .border_color(theme.border)
        .overflow_hidden()
        .text_color(theme.text);
    // Keys such as `disable-model-invocation` need ~180px at this size. When
    // the pane is too narrow for key and value side by side, the key sits above.
    let stacked = detail_content_width() < 420.;
    for (index, (key, value)) in rows.iter().enumerate() {
        let mut key_cell = div()
            .id(SharedString::from(format!("meta-key-{index}")))
            .p_2()
            .text_xs()
            .font_weight(FontWeight::MEDIUM)
            .text_color(theme.muted)
            .child(key.clone());
        key_cell = if stacked {
            key_cell.pb_0()
        } else {
            key_cell.w(px(184.)).flex_shrink_0()
        };
        if key.chars().count() > 22 {
            key_cell = key_cell.tooltip(Tooltip::text(key.clone()));
        }
        table = table.child(
            div()
                .w_full()
                .flex()
                .when(stacked, |row| row.flex_col())
                .when(!stacked, |row| row.items_start())
                .when(index != last, |row| {
                    row.border_b_1().border_color(theme.border)
                })
                .child(key_cell)
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .p_2()
                        .text_sm()
                        .text_color(theme.text)
                        .child(value.clone()),
                ),
        );
    }
    table.into_any_element()
}

fn markdown_with_metadata(content: &str) -> (Option<Vec<(String, String)>>, String) {
    let (rows, body) = split_frontmatter(content);
    if rows.is_empty() {
        (None, content.to_string())
    } else {
        (Some(rows), body)
    }
}

fn file_body(
    path: &str,
    content: &str,
    markdown: &mut Option<MarkdownCache>,
    cx: &mut Context<SyncaWindow>,
) -> gpui::AnyElement {
    if path.ends_with(".md") {
        let (metadata, body) = markdown_with_metadata(content);
        let cached = markdown
            .as_ref()
            .is_some_and(|cache| cache.path == path && cache.body == body);
        if !cached {
            let view = markdown_view::markdown_entity(body.clone(), cx);
            *markdown = Some(MarkdownCache {
                path: path.to_string(),
                body,
                view,
            });
        }
        let view = markdown.as_ref().map(|cache| cache.view.clone()).unwrap();
        let mut content = div().id("skill-content").w_full().flex().flex_col().gap_3();
        if let Some(rows) = metadata {
            content = content.child(metadata_table(&rows));
        }
        content.child(view).into_any_element()
    } else {
        div()
            .id("skill-content")
            .w_full()
            .overflow_x_scroll()
            .child(
                div()
                    .p_3()
                    .rounded_md()
                    .bg(theme().raised)
                    .border_1()
                    .border_color(theme().border)
                    .text_xs()
                    .text_color(theme().text)
                    .child(SelectableText::new(
                        SharedString::from(format!("skill-text-{path}")),
                        if path.is_empty() {
                            "Select a file".to_string()
                        } else {
                            content.to_string()
                        },
                    )),
            )
            .into_any_element()
    }
}

/// Tree of the skill folder beside the open file. `SKILL.md` opens until a
/// row is picked. The tree and the default file's preview come from the
/// selection's [`SkillFilesCache`]; the stored preview is used only when it
/// still matches. The caller fixes the section height; the preview scrolls
/// natively inside it.
fn skill_files(
    files: &SkillFilesCache,
    current: &str,
    preview: &str,
    file_scroll: &ScrollHandle,
    markdown: &mut Option<MarkdownCache>,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement + use<> {
    let (root, items, default_open, default_preview) = (
        files.root.as_deref().unwrap_or(""),
        &files.items,
        &files.default_open,
        &files.default_preview,
    );
    let theme = theme();
    let open = chosen_file(root, current, items);
    let content = if !open.is_empty() && current == open && !preview.is_empty() {
        preview.to_string()
    } else if open.is_empty() {
        "Select a file".to_string()
    } else if open == default_open.as_str() {
        default_preview.to_string()
    } else {
        read_preview(&open)
    };
    let label = if open.is_empty() {
        "No file".to_string()
    } else {
        relative_label(root, &open)
    };
    let reveal = open.clone();
    let nodes = tree_nodes(items);
    // Under 640pt of content the tree sits above the preview. Wider panes put
    // them side by side, and the tree grows with the pane.
    let layout = detail_layout(detail_content_width());
    let stacked = layout.files_stacked;
    let tree = div()
        .when(stacked, |tree| tree.w_full().h(px(layout.tree_h)))
        .when(!stacked, |tree| tree.w(px(layout.tree_w)).h_full())
        .flex_shrink_0()
        .rounded_md()
        .border_1()
        .border_color(theme.border)
        .bg(theme.panel)
        .py_1()
        .child(if items.is_empty() {
            InlineMessage::new(Severity::Info, "No files").into_any_element()
        } else {
            Tree::new("skill-tree", nodes)
                .open(open_folders(root, &open, items))
                .selected([open.clone()])
                .on_select({
                    let window = cx.entity().downgrade();
                    let folders: std::collections::HashSet<String> = items
                        .iter()
                        .filter(|item| item.dir)
                        .map(|item| item.path.clone())
                        .collect();
                    move |keys, _, cx| {
                        let Some(path) = keys.first().map(|key| key.to_string()) else {
                            return;
                        };
                        if folders.contains(&path) {
                            return;
                        }
                        window
                            .update(cx, |this, cx| {
                                this.file_preview = read_preview(&path);
                                this.file_path = path.clone();
                                this.file_scroll = ScrollHandle::new();
                                cx.notify();
                            })
                            .ok();
                    }
                })
                .h_full()
                .into_any_element()
        });
    div()
        .h_full()
        .flex()
        .when(stacked, |row| row.flex_col())
        .gap_3()
        .child(tree)
        .child(
            div()
                .flex_1()
                .min_w(px(0.))
                .min_h(px(0.))
                .flex()
                .flex_col()
                .gap_2()
                .child(
                    div()
                        .h(px(28.))
                        .flex_none()
                        .flex()
                        .items_center()
                        .gap_2()
                        .child(
                            div()
                                .flex_1()
                                .min_w(px(0.))
                                .truncate()
                                .text_xs()
                                .text_color(theme.muted)
                                .child(label),
                        )
                        .when(!reveal.is_empty(), |row| {
                            let path = reveal.clone();
                            row.child(icon_button(
                                "file-reveal",
                                "reveal",
                                "Reveal",
                                IconTone::Quiet,
                                cx.listener(move |_, _, _, _| ns_ui::reveal(&path)),
                            ))
                        }),
                )
                .child(
                    div()
                        .id("file-scroll")
                        .flex_1()
                        .min_h(px(0.))
                        .overflow_y_scroll()
                        .track_scroll(file_scroll)
                        .child(file_body(&open, &content, markdown, cx)),
                ),
        )
}

fn activity_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    body = body.w_full().min_w(px(0.));
    if panel_owns_ask(Panel::Activity, window.pending.as_ref()) {
        body = with_ask(window, body, cx);
    }
    let open = window.activity_open;
    body = body.child(
        div()
            .flex()
            .flex_wrap()
            .gap_2()
            .child(quiet_button(
                "clear-activity",
                "trash",
                "Clear Activity",
                cx.listener(|this, _, _, cx| {
                    this.ask_for(
                        "Clear the activity log?",
                        "The command history file is emptied.",
                        "Clear",
                        Pending::ClearActivity,
                        cx,
                    );
                }),
            ))
            .child(quiet_button(
                "activity-earlier",
                "history",
                "Earlier",
                cx.listener(|this, _, _, cx| {
                    this.activity_limit = this.activity_limit.saturating_add(40).min(400);
                    this.load_activity();
                    cx.notify();
                }),
            )),
    );
    if window.activity.is_empty() {
        body = body.child(
            EmptyState::new("activity-empty", IconName::History, "No commands yet")
                .body("A sync, install, or update shows up here."),
        );
    }
    let theme = theme();
    // Borrowed rows: only the small per-row strings are cloned, never the
    // collapsed output of the whole log.
    for entry in &window.activity {
        let id = entry.id;
        let shown = open == Some(id);
        let command = entry.command.clone();
        body = body.child(
            div()
                .w_full()
                .min_w(px(0.))
                .overflow_hidden()
                .py_1()
                .flex()
                .flex_col()
                .gap_1()
                .child(div().text_xs().child(format!(
                    "{}  {}  exit {}",
                    entry.status,
                    shown_scope(&entry.scope),
                    entry.exit
                )))
                .child(
                    div()
                        .id(SharedString::from(format!("act-{id}")))
                        .w_full()
                        .min_w(px(0.))
                        .h(px(26.))
                        .px_2()
                        .flex()
                        .items_center()
                        .gap_1()
                        .overflow_hidden()
                        .rounded_md()
                        .bg(theme.raised)
                        .border_1()
                        .border_color(theme.border)
                        .cursor_pointer()
                        .hover(move |style| style.bg(theme.hover).border_color(theme.accent))
                        .child(div().flex_shrink_0().child(icon("history")))
                        .child(
                            div()
                                .min_w(px(0.))
                                .flex_1()
                                .truncate()
                                .text_xs()
                                .child(command),
                        )
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.activity_open = if this.activity_open == Some(id) {
                                None
                            } else {
                                Some(id)
                            };
                            cx.notify();
                        })),
                ),
        );
        if shown && !entry.output.is_empty() {
            body = body.child(output_block(
                &format!("activity-out-{}", entry.id),
                &SharedString::from(ns_ui::strip_ansi(&entry.output)),
            ));
        }
    }
    body
}

fn settings_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    if panel_owns_ask(Panel::Settings, window.pending.as_ref()) {
        body = with_ask(window, body, cx);
    }
    body = body.child(section_label("Updates"));
    if window.update_text.is_empty() && !window.busy {
        body = body.child(InlineMessage::new(
            Severity::Info,
            "Check whether synca is already up to date.",
        ));
    }
    let version = if window.version.is_empty() {
        "unknown".to_string()
    } else {
        window.version.clone()
    };
    body = body.child(field("Installed", version));
    if !window.update_text.is_empty() {
        let clean = clean_output(&mut window.update_clean, &window.update_text);
        body = body.child(output_block("update-out", &clean));
    }
    body = body.child(
        div()
            .flex()
            .flex_wrap()
            .gap_1()
            .pt_1()
            .child(quiet_button(
                "check-update",
                "refresh",
                "Check for updates",
                cx.listener(|this, _, _, cx| {
                    this.run(
                        vec!["update".into(), "--check".into(), "--json".into()],
                        "ok",
                        cx,
                        After::Update,
                    );
                }),
            ))
            .when(window.update_ready, |row| {
                row.child(button(
                    "install-update",
                    "download",
                    "Install update",
                    cx.listener(|this, _, _, cx| {
                        this.ask_for(
                            "Install the synca update?",
                            "This replaces the synca binary on this machine.",
                            "Install",
                            Pending::InstallUpdate,
                            cx,
                        );
                    }),
                ))
            }),
    );
    body = body.child(section_label("Providers"));
    for (index, (id, title)) in PROVIDERS.iter().enumerate() {
        let enabled = !window.disabled.iter().any(|item| item == id);
        let provider = (*id).to_string();
        let clear = (*id).to_string();
        let title = (*title).to_string();
        let state = if enabled { "Enabled" } else { "Disabled" };
        let theme = theme();
        body = body.child(
            div()
                .w_full()
                .flex()
                .items_center()
                .gap_2()
                .py_1()
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .overflow_hidden()
                        .whitespace_nowrap()
                        .text_sm()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(if enabled { theme.text } else { theme.muted })
                        .child(title.clone()),
                )
                .child(
                    div()
                        .flex_shrink_0()
                        .flex()
                        .items_center()
                        .justify_end()
                        .gap_2()
                        .child(icon_button(
                            SharedString::from(format!("prov-{index}")),
                            if enabled {
                                "toggle-right"
                            } else {
                                "toggle-left"
                            },
                            state,
                            if enabled {
                                IconTone::Accent
                            } else {
                                IconTone::Quiet
                            },
                            cx.listener(move |this, _, _, cx| {
                                if this.disabled.iter().any(|item| item == &provider) {
                                    this.disabled.retain(|item| item != &provider);
                                } else {
                                    this.disabled.push(provider.clone());
                                }
                                engine::save_disabled(&this.disabled);
                                cx.notify();
                            }),
                        ))
                        .child(icon_button(
                            SharedString::from(format!("clear-{index}")),
                            "trash",
                            "Delete data",
                            IconTone::Warn,
                            cx.listener(move |this, _, _, cx| {
                                if this.scope == Scope::Project && this.dir().is_none() {
                                    this.toast(
                                        "Choose a workspace folder before deleting workspace data.",
                                        cx,
                                    );
                                    return;
                                }
                                let id = clear.clone();
                                let name = title.clone();
                                this.ask_for(
                                    &format!("Delete {name} data?"),
                                    "Removes that provider’s skills and MCP entries in this scope. The canonical hub stays. This cannot be undone.",
                                    "Delete data",
                                    Pending::ClearProvider(id),
                                    cx,
                                );
                            }),
                        )),
                ),
        );
    }
    body
}

fn install_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let theme = theme();
    let source_kind = window.skill_source_kind;
    let browse = source_kind == SkillSourceKind::Local;
    body = body
        .child(section_label("Source"))
        .child(segmented(
            "skill-source-kind",
            [
                ("local", "Local folder", "folder"),
                ("remote", "Remote Git", "globe"),
                ("skills-sh", "Skills.sh link", "skill"),
                ("json", "JSON", "file"),
            ],
            match source_kind {
                SkillSourceKind::Local => "local",
                SkillSourceKind::Remote => "remote",
                SkillSourceKind::SkillsSh => "skills-sh",
                SkillSourceKind::Json => "json",
            },
            cx.listener(|this, value: &SharedString, _, cx| {
                this.skill_source_kind = match value.as_ref() {
                    "remote" => SkillSourceKind::Remote,
                    "skills-sh" => SkillSourceKind::SkillsSh,
                    "json" => SkillSourceKind::Json,
                    _ => SkillSourceKind::Local,
                };
                cx.notify();
            }),
        ))
        .child(text_field("skill-source", &window.source_input))
        .child(
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(match source_kind {
                    SkillSourceKind::Local => "Folder containing SKILL.md.",
                    SkillSourceKind::Remote => "GitHub, GitLab, or git URL. Cloned temporarily for preview and install.",
                    SkillSourceKind::SkillsSh => "Paste the repository URL from skills.sh. The CLI resolves the remote repository.",
                    SkillSourceKind::Json => r#"JSON descriptor, for example {"source":"owner/repo"}."#,
                }),
        )
        .child(
            div()
                .flex()
                .flex_wrap()
                .gap_2()
                .pt_2()
                .child(button(
                    "preview-install",
                    "eye",
                    "Preview install",
                    cx.listener(|this, _, _, cx| this.preview_install(cx)),
                ))
                .when(browse, |row| {
                    row.child(quiet_button(
                        "browse-skill",
                        "folder",
                        "Choose folder",
                        cx.listener(|_this, _, _, cx| {
                            cx.spawn(async move |this, cx| {
                                let picked = cx
                                    .background_spawn(async {
                                        ns_ui::choose_folder("Choose a skill folder")
                                    })
                                    .await;
                                this.update(cx, |this, cx| {
                                    if let Some(path) = picked {
                                        this.source = path.trim_end_matches('/').to_string();
                                        this.source_input.update(cx, |input, cx| {
                                            input.set_text(this.source.clone(), cx);
                                        });
                                        cx.notify();
                                    }
                                })
                                .ok();
                            })
                            .detach();
                        }),
                    ))
                }),
        );
    body
}

fn update_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let clean = clean_output(&mut window.update_clean, &window.update_text);
    body = body.child(output_block("update-out", &clean));
    let url = window.update_url.clone();
    if !url.is_empty() {
        body = body.child(quiet_button(
            "open-release",
            "reveal",
            "Open release page",
            cx.listener(move |_, _, _, _| {
                ns_ui::open_url(&url);
            }),
        ));
    }
    body = body.child(quiet_button(
        "check-again",
        "refresh",
        "Check again",
        cx.listener(|this, _, _, cx| {
            this.run(
                vec!["update".into(), "--check".into(), "--json".into()],
                "ok",
                cx,
                After::Update,
            );
        }),
    ));
    if window.update_ready {
        body = body.child(button(
            "install-update",
            "download",
            "Install Update",
            cx.listener(|this, _, _, cx| {
                this.ask_for(
                    "Install the synca update?",
                    "This replaces the synca binary on this machine.",
                    "Install",
                    Pending::InstallUpdate,
                    cx,
                );
            }),
        ));
    }
    body
}

fn read_preview(path: &str) -> String {
    let Ok(meta) = std::fs::metadata(path) else {
        return "Cannot read this path.".into();
    };
    if meta.is_dir() {
        let Ok(read) = std::fs::read_dir(path) else {
            return "Folder.".into();
        };
        let mut names: Vec<String> = read
            .flatten()
            .filter_map(|entry| entry.file_name().into_string().ok())
            .collect();
        names.sort();
        names.truncate(24);
        return if names.is_empty() {
            "Empty folder.".into()
        } else {
            names.join("\n")
        };
    }
    if meta.len() > 512 * 1024 {
        return format!("Binary or large file, {} bytes.", meta.len());
    }
    let Ok(bytes) = std::fs::read(path) else {
        return "Cannot read this file.".into();
    };
    if bytes.contains(&0) {
        return format!("Binary file, {} bytes.", bytes.len());
    }
    let mut text = String::from_utf8_lossy(&bytes).into_owned();
    // `truncate` panics unless the index is a char boundary. A multibyte
    // character (Vietnamese, an em dash) that straddles byte 8000 used to abort
    // the window on every repaint of that skill.
    const PREVIEW_LIMIT: usize = 8000;
    if text.len() > PREVIEW_LIMIT {
        let mut end = PREVIEW_LIMIT;
        while !text.is_char_boundary(end) {
            end -= 1;
        }
        text.truncate(end);
        text.push_str("\n…");
    }
    if text.is_empty() {
        "Empty file.".into()
    } else {
        text
    }
}

fn mcp_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let theme = theme();
    let transport = window.mcp_transport_kind;
    let remote = transport != McpTransportKind::Stdio;
    body = body
        .child(section_label("Identity"))
        .child(text_field("mcp-name", &window.name_input))
        .child(section_label("Connection"))
        .child(tab_strip_equal(
            "mcp-transport",
            [
                ("stdio", "terminal", "Local"),
                ("http", "globe", "HTTP"),
                ("sse", "server", "SSE"),
                ("json", "file", "JSON"),
            ],
            match transport {
                McpTransportKind::Stdio => "stdio",
                McpTransportKind::Http => "http",
                McpTransportKind::Sse => "sse",
                McpTransportKind::Json => "json",
            },
            cx.listener(|this, value: &SharedString, _, cx| {
                this.mcp_transport_kind = match value.as_ref() {
                    "http" => McpTransportKind::Http,
                    "sse" => McpTransportKind::Sse,
                    "json" => McpTransportKind::Json,
                    _ => McpTransportKind::Stdio,
                };
                this.mcp_transport = value.to_string();
                cx.notify();
            }),
        ))
        .when(transport == McpTransportKind::Json, |body| {
            body.child(section_label("JSON config"))
                .child(text_field("mcp-json-config", &window.mcp_config_input))
        })
        .when(transport != McpTransportKind::Json && !remote, |body| {
            body.child(section_label("Command"))
                .child(text_field("mcp-command", &window.command_input))
        })
        .when(transport != McpTransportKind::Json && remote, |body| {
            body.child(section_label("Endpoint URL"))
                .child(text_field("mcp-url", &window.url_input))
        })
        .child(
            div()
                .flex()
                .items_center()
                .justify_between()
                .pt_2()
                .child(
                    div()
                        .text_sm()
                        .text_color(theme.text)
                        .child("Enabled for agent sync"),
                )
                .child(quiet_button_md(
                    "mcp-enabled",
                    if window.mcp_enabled {
                        "toggle-right"
                    } else {
                        "toggle-left"
                    },
                    if window.mcp_enabled {
                        "Enabled"
                    } else {
                        "Disabled"
                    },
                    cx.listener(|this, _, _, cx| {
                        this.mcp_enabled = !this.mcp_enabled;
                        cx.notify();
                    }),
                )),
        )
        .child(button(
            "preview-mcp",
            "eye",
            "Preview configuration",
            cx.listener(|this, _, _, cx| this.preview_mcp(cx)),
        ));
    body
}

fn plan_body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    if let Some(output) = window.plan.as_ref().map(|plan| plan.output.as_str())
        && window.plan_clean.is_none()
    {
        let clean = ns_ui::strip_ansi(output);
        window.plan_clean = Some(SharedString::from(clean));
    }
    let Some(plan) = window.plan.as_ref() else {
        return body.child(
            EmptyState::new("plan-empty", IconName::ListChecks, "No plan yet")
                .body("Preview a sync, an install, or an MCP change first."),
        );
    };
    body = body.child(field("Plan", plan.title.clone()));
    for (kind, count) in &plan.chips {
        body = body.child(field(kind, count.to_string()));
    }
    for (index, conflict) in plan.conflicts.iter().enumerate() {
        let label = format!(
            "{} {}",
            if conflict.skill { "skill" } else { "mcp" },
            conflict.key
        );
        body = body.child(div().pt_1().child(label.clone()));
        for policy in [Policy::Skip, Policy::KeepSource, Policy::KeepTarget] {
            let marked = if conflict.policy == policy {
                format!("{} ●", policy.label())
            } else {
                policy.label().into()
            };
            let policy_icon = match policy {
                Policy::Skip => "forget",
                Policy::KeepSource => "file",
                Policy::KeepTarget => "folder",
            };
            body = body.child(quiet_button(
                SharedString::from(format!("pol-{index}-{}", policy.as_str())),
                policy_icon,
                marked,
                cx.listener(move |this, _, _, cx| {
                    if let Some(plan) = this.plan.as_mut()
                        && let Some(item) = plan.conflicts.get_mut(index)
                    {
                        item.policy = policy;
                    }
                    cx.notify();
                }),
            ));
        }
    }
    body = body.child(button(
        "apply",
        "check",
        "Apply",
        cx.listener(|this, _, _, cx| this.apply_plan(cx)),
    ));
    let clean = window
        .plan_clean
        .clone()
        .unwrap_or_else(|| SharedString::from(""));
    body.child(div().pt_2().child(output_block("plan-out", &clean)))
}

#[derive(Deserialize)]
struct SkillRow {
    key: String,
    #[serde(default)]
    display_name: String,
    #[serde(default)]
    description: String,
    #[serde(default)]
    presence: Vec<SkillPresence>,
    #[serde(default)]
    mismatch: bool,
}

#[derive(Deserialize)]
struct SkillPresence {
    agent: String,
    #[serde(default)]
    path: String,
    #[serde(default)]
    content_hash: String,
}

#[derive(Deserialize)]
struct McpRow {
    key: String,
    #[serde(default)]
    presence: Vec<McpPresence>,
    #[serde(default)]
    mismatch: bool,
}

#[derive(Deserialize)]
struct McpPresence {
    agent: String,
    #[serde(default)]
    path: String,
    #[serde(default)]
    fingerprint: String,
    #[serde(default)]
    normalized: Option<serde_json::Value>,
}

/// A hash the CLI can compare. `dangling` and `missing` mean the link or its
/// `SKILL.md` could not be read, so neither matches anything.
fn hash_known(hash: &str) -> bool {
    !hash.is_empty() && hash != "dangling" && hash != "missing"
}

/// Agent links and the hub for one row. The hub stays out of `links`, and a
/// link's state is unknown when the hub side has no hash to compare.
fn agent_links(
    hashes: impl Iterator<Item = (String, String, String)>,
) -> (Vec<AgentLink>, Option<String>, Option<usize>) {
    let rows: Vec<(String, String, String)> = hashes.collect();
    let hub_hash = rows
        .iter()
        .find(|(agent, _, _)| agent == engine::CANONICAL_AGENT)
        .map(|(_, _, hash)| hash.clone())
        .filter(|hash| hash_known(hash));
    let hub_path = rows
        .iter()
        .find(|(agent, _, _)| agent == engine::CANONICAL_AGENT)
        .map(|(_, path, _)| path.clone())
        .filter(|path| !path.is_empty());
    let mut links = Vec::new();
    let mut synced = 0;
    for (agent, path, hash) in rows {
        if agent == engine::CANONICAL_AGENT || engine::retired_agent(&agent) {
            continue;
        }
        let state = hub_hash
            .as_ref()
            .map(|hub| hash_known(&hash) && &hash == hub);
        if state == Some(true) {
            synced += 1;
        }
        links.push(AgentLink {
            agent,
            path,
            synced: state,
        });
    }
    (links, hub_path, hub_hash.map(|_| synced))
}

fn skill_tokens(hub: Option<&str>, links: &[AgentLink]) -> Option<usize> {
    let dir = hub
        .filter(|dir| !dir.is_empty())
        .or_else(|| links.first().map(|link| link.path.as_str()))?;
    let bytes = std::fs::metadata(std::path::Path::new(dir).join("SKILL.md"))
        .ok()?
        .len();
    Some(bytes as usize / 4)
}

fn mcp_tokens(normalized: Option<&serde_json::Value>) -> Option<usize> {
    let bytes = serde_json::to_string(normalized?).ok()?.len();
    Some(bytes / 4)
}

/// Hub config when the canonical copy is present, otherwise the first agent
/// that reported one. The sidebar total is the sum of these estimates.
fn mcp_row_tokens(row: &McpRow) -> Option<usize> {
    let preferred = row
        .presence
        .iter()
        .find(|item| item.agent == engine::CANONICAL_AGENT)
        .and_then(|item| item.normalized.as_ref());
    let value = preferred.or_else(|| {
        row.presence
            .iter()
            .find_map(|item| item.normalized.as_ref())
    });
    mcp_tokens(value)
}

/// `≈820`, `≈1.2k`, `≈2.4M`.
fn format_tokens(tokens: usize) -> String {
    if tokens < 1_000 {
        return format!("≈{tokens}");
    }
    let (value, unit) = if tokens < 1_000_000 {
        (tokens as f64 / 1_000., "k")
    } else {
        (tokens as f64 / 1_000_000., "M")
    };
    let text = format!("{value:.1}");
    let text = text.strip_suffix(".0").unwrap_or(&text);
    format!("≈{text}{unit}")
}

fn entry_sync_label(entry: &Entry) -> SharedString {
    match entry.synced {
        Some(synced) => SharedString::from(format!("{synced}/{}", entry.links.len())),
        None => SharedString::from("—"),
    }
}

/// The detail card's phrasing of the same counter: "5 of 6", or a dash when
/// the CLI gave no hub hash to compare against.
fn synced_agents_label(entry: &Entry) -> String {
    let total = entry.links.len();
    match entry.synced {
        Some(synced) if total > 0 => format!("{synced} of {total}"),
        _ => "—".to_string(),
    }
}

fn entry_tokens_label(entry: &Entry) -> SharedString {
    entry
        .tokens
        .map(|tokens| SharedString::from(format_tokens(tokens)))
        .unwrap_or_else(|| SharedString::from("—"))
}

fn entry_sync_tip(entry: &Entry) -> String {
    match (entry.synced, entry.links.len()) {
        (Some(synced), links) => format!("{synced} of {links} agents match the hub"),
        (None, 0) => "No agent links".into(),
        (None, _) => "Agent sync unknown (the CLI gave no hub hash)".into(),
    }
}

/// The entries, and one line per CLI call that failed. A failure used to become
/// a fake row; it is now reported to the caller, which toasts it.
fn load_entries(
    scope: Scope,
    dir: Option<&str>,
    _disabled: &[String],
) -> (Vec<Entry>, Vec<String>) {
    let mut errors = Vec::new();
    let Ok(scope_args) = engine::scope_args(scope, dir) else {
        return (Vec::new(), errors);
    };
    let mut skills_args = vec!["skills".into(), "list".into()];
    skills_args.extend(scope_args.iter().cloned());
    skills_args.push("--json".into());
    let mut mcp_args = vec!["mcp".into(), "list".into()];
    mcp_args.extend(scope_args);
    mcp_args.push("--json".into());
    let skills = ns_ui::run_tool("synca", &skills_args);
    let mcps = ns_ui::run_tool("synca", &mcp_args);
    let mut entries = Vec::new();
    match serde_json::from_str::<Vec<SkillRow>>(&skills.stdout) {
        Ok(rows) => {
            for row in rows {
                let (links, hub_path, synced) = agent_links(row.presence.iter().map(|item| {
                    (
                        item.agent.clone(),
                        item.path.clone(),
                        item.content_hash.clone(),
                    )
                }));
                let tokens = skill_tokens(hub_path.as_deref(), &links);
                let title = if row.display_name.is_empty() {
                    row.key.clone()
                } else {
                    row.display_name
                };
                entries.push(Entry {
                    kind: Kind::Skill,
                    key: row.key,
                    title,
                    description: row.description,
                    paths: row
                        .presence
                        .iter()
                        .map(|item| item.path.clone())
                        .filter(|path| !path.is_empty())
                        .collect(),
                    links,
                    synced,
                    tokens,
                    mismatch: row.mismatch,
                });
            }
        }
        Err(_) => {
            if !skills.stdout.trim().is_empty() || !skills.ok {
                errors.push(list_failure("skills", &skills));
            }
        }
    }
    match serde_json::from_str::<Vec<McpRow>>(&mcps.stdout) {
        Ok(rows) => {
            for row in rows {
                let tokens = mcp_row_tokens(&row);
                let (links, _, synced) = agent_links(row.presence.iter().map(|item| {
                    (
                        item.agent.clone(),
                        item.path.clone(),
                        item.fingerprint.clone(),
                    )
                }));
                entries.push(Entry {
                    kind: Kind::Mcp,
                    key: row.key.clone(),
                    title: row.key,
                    description: String::new(),
                    paths: row
                        .presence
                        .iter()
                        .map(|item| item.path.clone())
                        .filter(|path| !path.is_empty())
                        .collect(),
                    links,
                    synced,
                    tokens,
                    mismatch: row.mismatch,
                });
            }
        }
        Err(_) => {
            if !mcps.stdout.trim().is_empty() || !mcps.ok {
                errors.push(list_failure("mcp", &mcps));
            }
        }
    }
    (entries, errors)
}

/// The toast line for a list call that failed or printed something that is not JSON.
fn list_failure(what: &str, result: &ns_ui::ToolResult) -> String {
    let text = result.text();
    let detail = if text.trim().is_empty() {
        "no output".to_string()
    } else {
        engine::failure_message(&text)
    };
    format!("Failed to load {what}: {detail}")
}

fn stored_panel() -> Panel {
    let activity = engine::read_flag("synca.showActivity");
    let settings = engine::read_flag("synca.showSettings");
    if activity {
        if settings {
            engine::write_flag("synca.showSettings", false);
        }
        Panel::Activity
    } else if settings {
        Panel::Settings
    } else {
        Panel::Closed
    }
}

fn panel_owns_ask(panel: Panel, pending: Option<&Pending>) -> bool {
    matches!(
        (panel, pending),
        (Panel::Activity, Some(Pending::ClearActivity))
            | (Panel::Settings, Some(Pending::ClearProvider(_)))
    )
}

fn with_ask(window: &SyncaWindow, body: gpui::Div, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let Some(ask) = window.ask.clone() else {
        return body;
    };
    body.child(ask_banner(
        &ask,
        cx.listener(|this, _, _, cx| this.confirm(cx)),
        cx.listener(|this, _, _, cx| {
            this.ask = None;
            this.pending = None;
            cx.notify();
        }),
    ))
}

/// Keys of the bulk selection of one kind, in sidebar order.
fn keys_for_kind(entries: &[Entry], bulk: &BTreeSet<usize>, kind: Kind) -> Vec<String> {
    bulk.iter()
        .filter(|index| entries.get(**index).is_some_and(|entry| entry.kind == kind))
        .map(|index| entries[*index].key.clone())
        .collect()
}

/// One menu row that opens the agent picker over the selection.
fn picker_menu_item(
    entity: Entity<SyncaWindow>,
    action: PickerAction,
    kind: Kind,
    label: String,
    icon: IconName,
) -> ElyMenuItem {
    ElyMenuItem::new(label).icon(icon).on_click(move |_, app| {
        entity.update(app, |this, cx| this.open_picker(action, kind, cx));
    })
}

/// The right-click menu for one row. When the row sits inside the bulk
/// selection, the items act on every selected entry of the row's kind; alone,
/// they act on the row.
fn row_menu_for(
    index: usize,
    entry: &Entry,
    keys: Vec<String>,
    cx: &mut Context<SyncaWindow>,
) -> ElyMenu {
    let kind = entry.kind;
    let count = keys.len();
    let one = count == 1;
    let target = if kind == Kind::Skill {
        Target::Skills
    } else {
        Target::Mcp
    };
    let entity = cx.entity();
    let mut menu = ElyMenu::new().item({
        let show = entity.clone();
        ElyMenuItem::new("Show detail")
            .icon(IconName::Eye)
            .on_click(move |_, app| show.update(app, |this, cx| this.show_row(index, cx)))
    });
    let sync_label = if one {
        "Sync this".to_string()
    } else {
        format!("Sync {count} selected…")
    };
    let sync_keys = keys.clone();
    menu = menu.item({
        let sync = entity.clone();
        ElyMenuItem::new(sync_label)
            .icon(IconName::RefreshCw)
            .on_click(move |_, app| {
                sync.update(app, |this, cx| {
                    if one {
                        let key = sync_keys[0].clone();
                        this.plan_sync(target, Some(key), cx);
                    } else {
                        this.ask_sync_many(target, sync_keys.clone(), cx);
                    }
                })
            })
    });
    menu = menu.separator();
    if kind == Kind::Skill {
        let unlink_label = if one {
            "Unlink…".to_string()
        } else {
            format!("Unlink {count} skills…")
        };
        menu = menu.item(picker_menu_item(
            entity.clone(),
            PickerAction::Unlink,
            kind,
            unlink_label,
            IconName::Link,
        ));
        let purge_label = if one {
            "Purge…".to_string()
        } else {
            format!("Purge {count} skills…")
        };
        menu = menu.item(picker_menu_item(
            entity,
            PickerAction::Purge,
            kind,
            purge_label,
            IconName::Trash2,
        ));
    } else {
        let remove_label = if one {
            "Remove…".to_string()
        } else {
            format!("Remove {count} MCPs…")
        };
        menu = menu.item(picker_menu_item(
            entity,
            PickerAction::RemoveMcp,
            kind,
            remove_label,
            IconName::Trash2,
        ));
    }
    let copy_keys = keys.join("\n");
    menu = menu
        .item(
            ElyMenuItem::new(if one { "Copy key" } else { "Copy keys" })
                .icon(IconName::Copy)
                .on_click(move |_, _| {
                    ns_ui::copy_text(&copy_keys);
                }),
        )
        .separator();
    if let Some(path) = entry.paths.first().cloned() {
        menu = menu
            .item({
                let copy = path.clone();
                ElyMenuItem::new("Copy path")
                    .icon(IconName::Copy)
                    .on_click(move |_, _| {
                        ns_ui::copy_text(&copy);
                    })
            })
            .item({
                let reveal = path.clone();
                ElyMenuItem::new("Reveal in Finder")
                    .icon(IconName::FolderOpen)
                    .on_click(move |_, _| {
                        ns_ui::reveal(&reveal);
                    })
            });
    }
    menu
}

fn inventory_heading(
    title: &str,
    count: usize,
    note: &Option<SharedString>,
    tokens: Option<usize>,
) -> impl IntoElement {
    let theme = theme();
    let total = match tokens {
        Some(tokens) => format!("{count} · {}", format_tokens(tokens)),
        None => count.to_string(),
    };
    let tip = match tokens {
        Some(_) => format!("{title}: {total} estimated tokens"),
        None => format!("{title}: {count} shown"),
    };
    div()
        .id(SharedString::from(format!("heading-{title}")))
        .flex_shrink_0()
        .w_full()
        .h(px(SIDEBAR_SECTION_H))
        .px(px(SIDEBAR_INSET + 8.))
        .flex()
        .items_center()
        .gap(px(8.))
        .text_color(theme.muted)
        .tooltip(Tooltip::text(tip))
        .child(
            div()
                .flex_1()
                .min_w(px(0.))
                .truncate()
                .text_size(px(11.))
                .font_weight(gpui::FontWeight::SEMIBOLD)
                .child(title.to_uppercase()),
        )
        .children(note.as_ref().map(|text| {
            div()
                .flex_none()
                .text_size(px(11.))
                .text_color(theme.warn)
                .child(text.clone())
        }))
        .child(div().flex_none().text_size(px(11.)).child(total))
}

fn table_head(kind: Kind, sort: ListSort, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let prefix = if kind == Kind::Skill { "skills" } else { "mcp" };
    div().flex_shrink_0().w_full().h(px(TABLE_HEAD_H)).child(
        div()
            .mx(px(SIDEBAR_INSET))
            .h_full()
            .pl(px(8.))
            .pr(px(8.))
            .flex()
            .items_center()
            .gap(px(8.))
            .child(div().w(px(16.)).flex_none())
            .child(sort_head(
                format!("sort-{prefix}-name"),
                SortColumn::Name,
                sort,
                false,
                None,
                cx,
            ))
            .child(
                div()
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap(px(8.))
                    .child(div().w(px(COL_DOT_W)).flex_none())
                    .child(sort_head(
                        format!("sort-{prefix}-sync"),
                        SortColumn::Sync,
                        sort,
                        true,
                        Some(COL_SYNC_W),
                        cx,
                    ))
                    .child(sort_head(
                        format!("sort-{prefix}-tokens"),
                        SortColumn::Tokens,
                        sort,
                        true,
                        Some(COL_TOKEN_W),
                        cx,
                    )),
            ),
    )
}

fn sort_head(
    id: String,
    column: SortColumn,
    sort: ListSort,
    align_end: bool,
    width: Option<f32>,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let theme = theme();
    let active = sort.column == column;
    let mut head = div()
        .id(SharedString::from(id))
        .h_full()
        .flex()
        .items_center()
        .text_size(px(11.))
        .font_weight(gpui::FontWeight::SEMIBOLD)
        .text_color(if active { theme.text } else { theme.muted })
        .cursor_pointer()
        .tooltip(Tooltip::text(sort_tip(column, sort)))
        .hover(move |style| style.text_color(theme.accent))
        .on_click(cx.listener(move |this, _, _, cx| this.set_sort(column, cx)))
        .child(sort_label(column, sort));
    head = if let Some(width) = width {
        head.w(px(width)).flex_none()
    } else {
        head.flex_1().min_w(px(0.))
    };
    if align_end { head.justify_end() } else { head }
}

fn sort_label(column: SortColumn, sort: ListSort) -> String {
    let name = match column {
        SortColumn::Name => "Name",
        SortColumn::Sync => "Sync",
        SortColumn::Tokens => "Tokens",
        SortColumn::None => "",
    };
    if sort.column == column {
        format!("{name} {}", if sort.ascending { "↑" } else { "↓" })
    } else {
        name.to_string()
    }
}

fn sort_tip(column: SortColumn, sort: ListSort) -> String {
    let name = match column {
        SortColumn::Name => "name",
        SortColumn::Sync => "synced agents",
        SortColumn::Tokens => "estimated tokens",
        SortColumn::None => "order",
    };
    if sort.column != column {
        format!("Sort by {name}")
    } else if sort.ascending {
        format!("Sorted by {name}, ascending. Click to reverse.")
    } else {
        format!("Sorted by {name}, descending. Click to restore load order.")
    }
}

fn metric_cells(
    index: usize,
    mismatch: bool,
    status: SharedString,
    sync: SharedString,
    tokens: SharedString,
) -> gpui::Div {
    let theme = theme();
    let mut dot = div()
        .id(SharedString::from(format!("row-dot-{index}")))
        .w(px(COL_DOT_W))
        .h(px(COL_DOT_W))
        .flex_none()
        .rounded_full();
    if mismatch {
        dot = dot.bg(theme.warn).tooltip(Tooltip::text(status));
    }
    div()
        .flex_none()
        .flex()
        .items_center()
        .gap(px(8.))
        .text_xs()
        .text_color(theme.muted)
        .child(dot)
        .child(metric_text(sync, COL_SYNC_W))
        .child(metric_text(tokens, COL_TOKEN_W))
}

fn metric_text(text: SharedString, width: f32) -> gpui::Div {
    div()
        .w(px(width))
        .flex_none()
        .flex()
        .justify_end()
        .child(text)
}

/// One sidebar table. Skills and MCP share the columns, so a name lines up
/// with the same name in the other group. The heading shows the row count and
/// the estimated token total. A warn dot marks a row that is out of sync; the
/// slot stays reserved on healthy rows so the numbers do not shift.
///
/// `build` is the slice of `rows` this frame materializes; the rows outside it
/// keep their height through spacers, so the scrollbar does not move.
#[allow(clippy::too_many_arguments)]
fn inventory_group(
    mut list: gpui::Div,
    kind: Kind,
    title: &str,
    empty: &'static str,
    query: &str,
    rows: &[usize],
    note: &Option<SharedString>,
    entries: &[Entry],
    cache: &InventoryCache,
    build: std::ops::Range<usize>,
    selected: Option<usize>,
    bulk: &BTreeSet<usize>,
    row_menu: Option<(usize, u64, Point<gpui::Pixels>)>,
    focused: bool,
    focus: &FocusHandle,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let tokens_total = match kind {
        Kind::Skill => cache.skills_tokens,
        Kind::Mcp => cache.mcps_tokens,
    };
    list = list.child(inventory_heading(title, rows.len(), note, tokens_total));
    if rows.is_empty() {
        let text = if query.trim().is_empty() {
            SharedString::from(empty)
        } else {
            SharedString::from(format!(
                "No {} match “{}”",
                title.to_lowercase(),
                query.trim()
            ))
        };
        return list.child(sidebar_empty(text));
    }
    list = list.child(table_head(kind, cache.sort, cx));
    let glyph = if kind == Kind::Skill { "skill" } else { "plug" };
    let std::ops::Range { start, end } = build;
    if start > 0 {
        list = list.child(list_spacer(start));
    }
    for &index in &rows[start..end] {
        let on = selected == Some(index);
        let in_bulk = bulk.contains(&index);
        let focus = focus.clone();
        let (sync, tokens) = cache.counts[index].clone();
        let row = sidebar_row(cache.ids[index].clone(), cache.titles[index].clone())
            .icon(glyph)
            .selected(in_bulk)
            .kbd(focused && on)
            .tooltip(cache.titles[index].clone(), Some(cache.meta[index].clone()))
            .trailing(metric_cells(
                index,
                cache.mismatch[index],
                cache.status[index].clone(),
                sync,
                tokens,
            ))
            .on_click(
                cx.listener(move |this, event: &gpui::ClickEvent, window, cx| {
                    focus.focus(window, cx);
                    this.click_row(index, event.modifiers(), cx);
                }),
            );
        let row = div()
            .w_full()
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(move |this, event: &gpui::MouseDownEvent, _, cx| {
                    this.right_click_row(index, event.position, cx);
                }),
            )
            .child(row);
        let request = row_menu.and_then(|(row, number, at)| (row == index).then_some((number, at)));
        let keys = if in_bulk {
            keys_for_kind(entries, bulk, kind)
        } else {
            vec![entries[index].key.clone()]
        };
        let menu = row_menu_for(index, &entries[index], keys, cx);
        list = list.child(
            ContextMenu::new(SharedString::from(format!("row-menu-{index}")), menu)
                .manual(request)
                .w_full()
                .child(row),
        );
    }
    if end < rows.len() {
        list = list.child(list_spacer(rows.len() - end));
    }
    list
}

/// The slice of a group's rows that intersects the scroll viewport, padded by
/// [`ROW_OVERSCAN`]. An unknown viewport height (before the first layout)
/// keeps every row.
fn rows_in_view(count: usize, top: f32, view: f32, seen: f32) -> std::ops::Range<usize> {
    if count == 0 || view <= 0. {
        return 0..count;
    }
    let first = ((seen - ROW_OVERSCAN - top) / SIDEBAR_ROW_H)
        .floor()
        .clamp(0., count as f32) as usize;
    let last = ((seen + view + ROW_OVERSCAN - top) / SIDEBAR_ROW_H)
        .ceil()
        .clamp(first as f32, count as f32) as usize;
    first..last
}

/// Keeps the place of the rows a frame did not build.
fn list_spacer(rows: usize) -> gpui::Div {
    div()
        .w_full()
        .flex_shrink_0()
        .h(px(rows as f32 * SIDEBAR_ROW_H))
}

/// The reference the cache replaces: the per-frame computation this window
/// ran on every render before caching, kept for the equivalence tests.
#[cfg(test)]
fn inventory_rows(entries: &[Entry], kind: Kind, query: &str) -> (String, Vec<usize>, usize) {
    let needle = query.trim().to_lowercase();
    let shown: Vec<usize> = entries
        .iter()
        .enumerate()
        .filter(|(_, entry)| entry.kind == kind && entry_matches(entry, &needle))
        .map(|(index, _)| index)
        .collect();
    let stale = shown
        .iter()
        .filter(|index| entries[**index].mismatch)
        .count();
    (needle, shown, stale)
}

fn inventory_row_meta(entry: &Entry, status: &'static str) -> SharedString {
    let place = entry
        .paths
        .first()
        .map(|path| ns_ui::display_path(path))
        .unwrap_or_default();
    let mut parts = vec![status.to_string(), entry_sync_tip(entry)];
    if let Some(tokens) = entry.tokens {
        parts.push(format!("{} tokens (estimated)", format_tokens(tokens)));
    }
    if !place.is_empty() {
        parts.push(place);
    }
    SharedString::from(parts.join(" · "))
}

/// Reference matching for the equivalence tests: the per-entry check the
/// pre-lowercased [`entry_matches_key`] replaces.
#[cfg(test)]
fn entry_matches(entry: &Entry, needle: &str) -> bool {
    needle.is_empty()
        || entry.title.to_lowercase().contains(needle)
        || entry.key.to_lowercase().contains(needle)
}

/// Reference sidebar order for the equivalence tests.
#[cfg(test)]
fn visible_entries(entries: &[Entry], query: &str) -> Vec<usize> {
    let needle = query.trim().to_lowercase();
    let mut out = Vec::new();
    for kind in [Kind::Skill, Kind::Mcp] {
        for (index, entry) in entries.iter().enumerate() {
            if entry.kind == kind && entry_matches(entry, &needle) {
                out.push(index);
            }
        }
    }
    out
}

/// Reference geometry for the equivalence tests: where the row for `target`
/// sits, from the top of the list content.
#[cfg(test)]
fn row_top(entries: &[Entry], query: &str, target: usize) -> f32 {
    let needle = query.trim().to_lowercase();
    let mut y = 0.;
    for kind in [Kind::Skill, Kind::Mcp] {
        y += SIDEBAR_SECTION_H;
        let rows: Vec<usize> = entries
            .iter()
            .enumerate()
            .filter(|(_, entry)| entry.kind == kind && entry_matches(entry, &needle))
            .map(|(index, _)| index)
            .collect();
        if rows.is_empty() {
            y += SIDEBAR_ROW_H;
            continue;
        }
        y += TABLE_HEAD_H;
        if let Some(pos) = rows.iter().position(|index| *index == target) {
            return y + pos as f32 * SIDEBAR_ROW_H;
        }
        y += rows.len() as f32 * SIDEBAR_ROW_H;
    }
    y
}

fn rail(window: &mut SyncaWindow, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let activity = window.panel == Panel::Activity;
    let settings = window.panel == Panel::Settings;
    ns_ui::settings_rail(
        "rail-activity",
        "history",
        activity,
        if activity {
            "Hide Activity"
        } else {
            "Show Activity"
        },
        cx.listener(|this, _, _, cx| this.toggle_activity(cx)),
        settings,
        if settings {
            "Hide Settings"
        } else {
            "Show Settings"
        },
        cx.listener(|this, _, _, cx| this.toggle_settings(cx)),
    )
}

fn main() {
    ns_ui::use_tint(ns_ui::Tint::Synca);
    #[cfg(feature = "devtools")]
    ns_ui::open_dev("Synca", SyncaWindow::new);
    #[cfg(not(feature = "devtools"))]
    ns_ui::open("Synca", SyncaWindow::new);
}

/// Names `dev` handles. `spec()` lists these, and the match fallthrough checks them.
#[cfg(feature = "devtools")]
const DEV_COMMANDS: &[&str] = &[
    "install", "close", "key", "tab", "select", "menu", "state", "scope", "fail", "panel", "mcp",
    "perf",
];

#[cfg(feature = "devtools")]
struct SyncaSelection<'a> {
    index: usize,
    key: &'a str,
    kind: &'a str,
}

#[cfg(feature = "devtools")]
struct SyncaDevState<'a> {
    scope: &'a str,
    selected: Option<usize>,
    selection_rows: Vec<usize>,
    entries: usize,
    project: Option<&'a str>,
    query: &'a str,
    screen: &'a str,
    selection: Option<SyncaSelection<'a>>,
    busy: bool,
}

#[cfg(feature = "devtools")]
fn synca_screen(panel: Panel, route: Route, install_open: bool) -> &'static str {
    if install_open {
        return "install";
    }
    match panel {
        Panel::Activity => "activity",
        Panel::Settings => "settings",
        Panel::Closed => match route {
            Route::List => "list",
            Route::Plan => "plan",
            Route::Update => "update",
        },
    }
}

#[cfg(feature = "devtools")]
fn synca_state_value(state: &SyncaDevState<'_>) -> serde_json::Value {
    let selection = state.selection.as_ref().map(|item| {
        serde_json::json!({
            "index": item.index,
            "key": item.key,
            "kind": item.kind,
        })
    });
    serde_json::json!({
        "scope": state.scope,
        "selected": state.selected,
        "selection_rows": state.selection_rows,
        "entries": state.entries,
        "project": state.project,
        "query": state.query,
        "screen": state.screen,
        "selection": selection,
        "busy": state.busy,
    })
}

#[cfg(feature = "devtools")]
fn synca_state_schema() -> serde_json::Value {
    ns_ui::state_schema(
        serde_json::json!({
            "scope": {"type": "string", "enum": ["user", "workspace"]},
            "selected": {"type": ["integer", "null"], "minimum": 0},
            "selection_rows": {"type": "array", "items": {"type": "integer", "minimum": 0}},
            "entries": {"type": "integer", "minimum": 0},
            "project": {"type": ["string", "null"]},
            "query": {"type": "string"}
        }),
        &[
            "scope",
            "selected",
            "selection_rows",
            "entries",
            "project",
            "query",
        ],
    )
}

/// Press, drag, and release inside the window. Points are window coordinates.
/// A few moves with the button held keep a selection alive as the pointer
/// crosses from one markdown block into the next.
#[cfg(feature = "devtools")]
fn dispatch_dev_drag(window: &mut Window, drag: (f32, f32, f32, f32), cx: &mut App) {
    use gpui::{
        MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, PlatformInput, point, px,
    };
    let (x, y, to_x, to_y) = drag;
    let at = |fx: f32, fy: f32| point(px(fx), px(fy));
    let steps = 8;
    let _ = window.dispatch_event(
        PlatformInput::MouseDown(MouseDownEvent {
            button: MouseButton::Left,
            position: at(x, y),
            modifiers: Default::default(),
            click_count: 1,
            first_mouse: false,
        }),
        cx,
    );
    for step in 1..=steps {
        let t = step as f32 / steps as f32;
        let _ = window.dispatch_event(
            PlatformInput::MouseMove(MouseMoveEvent {
                position: at(x + (to_x - x) * t, y + (to_y - y) * t),
                pressed_button: Some(MouseButton::Left),
                modifiers: Default::default(),
            }),
            cx,
        );
    }
    let _ = window.dispatch_event(
        PlatformInput::MouseUp(MouseUpEvent {
            button: MouseButton::Left,
            position: at(to_x, to_y),
            modifiers: Default::default(),
            click_count: 1,
        }),
        cx,
    );
}

#[cfg(feature = "devtools")]
fn dev_commands() -> Vec<ns_ui::DevCommand> {
    vec![
        ns_ui::DevCommand::new("install", "Open the Install dialog on the Skill tab.", true).arg(
            "source",
            "string",
            false,
            "\"local\", \"remote\", or \"skills-sh\". Default local.",
        ),
        ns_ui::DevCommand::new("close", "Close the Install dialog.", true),
        ns_ui::DevCommand::new("key", "Queue one keystroke for the next frame.", true).arg(
            "key",
            "string",
            true,
            "A GPUI keystroke, such as escape, tab, or cmd-2.",
        ),
        ns_ui::DevCommand::new(
            "drag",
            "Drag the pointer inside the window, in points. Selects markdown text.",
            true,
        )
        .arg("x", "number", true, "Start x, in window points.")
        .arg("y", "number", true, "Start y, in window points.")
        .arg("to_x", "number", true, "End x, in window points.")
        .arg("to_y", "number", true, "End y, in window points."),
        ns_ui::DevCommand::new("tab", "Switch tab while the Install dialog is open.", true).arg(
            "name",
            "string",
            false,
            "\"skill\" or \"mcp\". Unknown names fall back to skill.",
        ),
        ns_ui::DevCommand::new("select", "Select a sidebar entry.", true)
            .arg(
                "index",
                "integer",
                false,
                "Index into the loaded entries. Default 0.",
            )
            .arg(
                "mode",
                "string",
                false,
                "\"replace\" (default), \"add\", or \"range\" from the anchor.",
            )
            .arg(
                "back",
                "boolean",
                false,
                "When true, show the navigation column.",
            ),
        ns_ui::DevCommand::new("menu", "Open the context menu for a sidebar row.", true).arg(
            "row",
            "integer",
            false,
            "Index into the loaded entries. Default 0.",
        ),
        ns_ui::DevCommand::new("state", "Read the window.", false),
        ns_ui::DevCommand::new("scope", "Switch scope in memory. Nothing is saved.", true).arg(
            "name",
            "string",
            false,
            "\"workspace\", or anything else for user.",
        ),
        ns_ui::DevCommand::new(
            "fail",
            "Show the toast for a rejected read-only CLI call.",
            true,
        ),
        ns_ui::DevCommand::new(
            "panel",
            "Open Activity, Settings, or close that column.",
            true,
        )
        .arg(
            "name",
            "string",
            false,
            "\"activity\", \"settings\", or anything else to close.",
        ),
        ns_ui::DevCommand::new("mcp", "Open the Install dialog on the MCP tab.", true).arg(
            "transport",
            "string",
            false,
            "\"stdio\", \"http\", \"sse\", or \"json\". Default stdio.",
        ),
        ns_ui::DevCommand::new(
            "perf",
            "Time forced draws over the loaded inventory; writes synca-perf-<label>.json in the temp dir. UI state only; no CLI actions.",
            true,
        )
        .arg("label", "string", false, "Report name: letters, digits, - or _. Default run.")
        .arg("frames", "integer", false, "Frames per scenario, clamped to 1–1000. Default 60."),
    ]
}

/// A listed command with no match arm is a bug. Anything else is unknown.
#[cfg(feature = "devtools")]
fn reject_unknown(command: &str) -> String {
    if DEV_COMMANDS.contains(&command) {
        format!("command \"{command}\" is listed but has no handler")
    } else {
        ns_ui::unknown_command(command)
    }
}

#[cfg(feature = "devtools")]
impl ns_ui::DevControl for SyncaWindow {
    fn spec() -> ns_ui::DevSpec {
        ns_ui::DevSpec {
            commands: dev_commands(),
            state_schema: synca_state_schema(),
        }
    }

    fn dev(
        &mut self,
        command: &str,
        request: &serde_json::Value,
        cx: &mut gpui::Context<Self>,
    ) -> Result<serde_json::Value, String> {
        match command {
            "install" => {
                self.onboarding = Some(InstallTab::Skill);
                self.skill_source_kind =
                    match request.get("source").and_then(|value| value.as_str()) {
                        Some("remote") => SkillSourceKind::Remote,
                        Some("skills-sh") => SkillSourceKind::SkillsSh,
                        _ => SkillSourceKind::Local,
                    };
                cx.notify();
                Ok(serde_json::json!({ "dialog": "install", "tab": "skill" }))
            }
            "close" => {
                let closed = self.onboarding.take();
                cx.notify();
                Ok(match closed {
                    Some(tab) => serde_json::json!({ "closed": "install", "tab": tab.key() }),
                    None => serde_json::json!({ "closed": null }),
                })
            }
            "key" => {
                let key = request
                    .get("key")
                    .and_then(|value| value.as_str())
                    .ok_or("key needs key=escape, tab, cmd-2, …")?;
                gpui::Keystroke::parse(key).map_err(|error| error.to_string())?;
                self.dev_key = Some(key.to_string());
                cx.notify();
                Ok(serde_json::json!({ "key": key }))
            }
            "drag" => {
                let number = |name: &str| -> Result<f32, String> {
                    request
                        .get(name)
                        .and_then(|value| value.as_f64())
                        .map(|value| value as f32)
                        .ok_or_else(|| format!("drag needs {name}"))
                };
                let drag = (number("x")?, number("y")?, number("to_x")?, number("to_y")?);
                self.dev_drag = Some(drag);
                cx.notify();
                Ok(serde_json::json!({
                    "from": [drag.0, drag.1],
                    "to": [drag.2, drag.3],
                }))
            }
            "tab" => {
                let tab = InstallTab::from_key(
                    request
                        .get("name")
                        .and_then(|value| value.as_str())
                        .unwrap_or(""),
                );
                if self.onboarding.is_none() {
                    return Err("the Install dialog is not open".into());
                }
                self.pick_install_tab(tab, cx);
                Ok(serde_json::json!({ "tab": tab.key() }))
            }
            "select" => {
                let index = request.get("index").and_then(|value| value.as_u64());
                let index = index.unwrap_or(0) as usize;
                if index >= self.entries.len() {
                    return Err(format!("no entry {index}; {} loaded", self.entries.len()));
                }
                self.route = Route::List;
                match request
                    .get("mode")
                    .and_then(|value| value.as_str())
                    .unwrap_or("replace")
                {
                    "add" => {
                        self.bulk.insert(index);
                        self.selected = Some(index);
                        self.anchor = Some(index);
                        self.reset_file();
                    }
                    "range" => {
                        if self.anchor.is_none() {
                            self.anchor = self.selected.or(Some(index));
                        }
                        self.extend_to(index, cx);
                    }
                    _ => {
                        self.bulk.clear();
                        self.bulk.insert(index);
                        self.selected = Some(index);
                        self.anchor = Some(index);
                        self.reset_file();
                    }
                }
                if request.get("back").and_then(|value| value.as_bool()) == Some(true) {
                    ns_ui::show_nav();
                } else {
                    ns_ui::show_detail();
                }
                cx.notify();
                Ok(serde_json::json!({
                    "selected": self.entries[index].key,
                    "rows": self.bulk.iter().copied().collect::<Vec<_>>(),
                }))
            }
            "menu" => {
                let index = request.get("row").and_then(|value| value.as_u64());
                let index = index.unwrap_or(0) as usize;
                if index >= self.entries.len() {
                    return Err(format!("no entry {index}; {} loaded", self.entries.len()));
                }
                if !self.bulk.contains(&index) {
                    self.click_row(index, gpui::Modifiers::default(), cx);
                }
                // The row's middle, in window points: the list's box plus the
                // scroll offset plus the row's place in the content.
                let bounds = self.list_scroll.bounds();
                let offset = self.list_scroll.offset();
                let top = self.inventory.row_top(index);
                let at = gpui::point(
                    bounds.origin.x + px(120.),
                    bounds.origin.y + offset.y + px(top + SIDEBAR_ROW_H / 2.),
                );
                self.menu_seq = self.menu_seq.wrapping_add(1);
                self.row_menu = Some(RowMenu {
                    row: index,
                    number: self.menu_seq,
                    at,
                });
                cx.notify();
                Ok(serde_json::json!({
                    "row": index,
                    "at": [f64::from(f32::from(at.x)), f64::from(f32::from(at.y))],
                }))
            }
            "state" => {
                let selection = self.selected.and_then(|index| {
                    self.entries.get(index).map(|entry| SyncaSelection {
                        index,
                        key: entry.key.as_str(),
                        kind: if entry.kind == Kind::Skill {
                            "skill"
                        } else {
                            "mcp"
                        },
                    })
                });
                Ok(synca_state_value(&SyncaDevState {
                    scope: self.scope_name(),
                    selected: self.selected,
                    selection_rows: self.bulk.iter().copied().collect(),
                    entries: self.entries.len(),
                    project: self.active_project.as_deref(),
                    query: self.query.as_str(),
                    screen: synca_screen(self.panel, self.route, self.onboarding.is_some()),
                    selection,
                    busy: self.busy,
                }))
            }
            "scope" => {
                // In memory only: nothing is saved, so the real workspace
                // list and its active project come back with `scope name=user`.
                if request.get("name").and_then(|value| value.as_str()) == Some("workspace") {
                    self.scope = Scope::Project;
                    self.active_project = None;
                } else {
                    self.scope = Scope::User;
                    self.active_project = engine::projects().1;
                }
                self.selected = None;
                self.bulk.clear();
                self.anchor = None;
                self.reload(cx);
                Ok(serde_json::json!({ "scope": self.scope_name() }))
            }
            "fail" => {
                // A real, read-only CLI failure: `--scope bogus` is rejected
                // before synca touches any data.
                let args = ["skills", "list", "--scope", "bogus", "--json"].map(String::from);
                let result = ns_ui::run_tool("synca", &args);
                let line = list_failure("skills", &result);
                self.status = "Load failed".into();
                self.toast(line.clone(), cx);
                Ok(serde_json::json!({ "toast": line }))
            }
            "panel" => {
                self.panel = match request.get("name").and_then(|value| value.as_str()) {
                    Some("activity") => Panel::Activity,
                    Some("settings") => Panel::Settings,
                    _ => Panel::Closed,
                };
                cx.notify();
                Ok(serde_json::json!({ "panel": request.get("name") }))
            }
            "mcp" => {
                self.onboarding = Some(InstallTab::Mcp);
                self.mcp_transport_kind =
                    match request.get("transport").and_then(|value| value.as_str()) {
                        Some("http") => McpTransportKind::Http,
                        Some("sse") => McpTransportKind::Sse,
                        Some("json") => McpTransportKind::Json,
                        _ => McpTransportKind::Stdio,
                    };
                self.mcp_transport = match self.mcp_transport_kind {
                    McpTransportKind::Stdio => "stdio",
                    McpTransportKind::Http => "http",
                    McpTransportKind::Sse => "sse",
                    McpTransportKind::Json => "json",
                }
                .into();
                cx.notify();
                Ok(serde_json::json!({ "dialog": "mcp", "tab": "mcp" }))
            }
            "perf" => {
                let (label, frames) = perf::arguments(request)?;
                perf::start(self, label, frames, cx)
            }
            other => Err(reject_unknown(other)),
        }
    }
}

#[cfg(all(test, feature = "devtools"))]
mod dev_contract_tests {
    use super::{
        DEV_COMMANDS, Panel, Route, SyncaDevState, SyncaSelection, SyncaWindow, reject_unknown,
        synca_screen, synca_state_value,
    };
    use ns_ui::DevControl;

    #[test]
    fn spec_names_match_the_command_list() {
        let spec = SyncaWindow::spec();
        let names: Vec<&str> = spec
            .commands
            .iter()
            .map(|command| command.name.as_str())
            .collect();
        assert_eq!(names, DEV_COMMANDS);
    }

    #[test]
    fn an_unlisted_command_points_at_help() {
        assert_eq!(reject_unknown("nope"), ns_ui::unknown_command("nope"));
        assert!(reject_unknown("state").contains("listed"));
    }

    #[test]
    fn argument_kinds_are_protocol_kinds() {
        for command in SyncaWindow::spec().commands {
            for arg in command.args {
                assert!(
                    ns_ui::ARG_KINDS.contains(&arg.kind.as_str()),
                    "{} {}",
                    command.name,
                    arg.kind
                );
            }
        }
    }

    #[test]
    fn state_schema_requires_the_contract_keys() {
        let schema = SyncaWindow::spec().state_schema;
        assert_eq!(
            schema["$schema"],
            "https://json-schema.org/draft/2020-12/schema"
        );
        assert_eq!(schema["type"], "object");
        let required = schema["required"].as_array().expect("required");
        for key in ["protocol", "app", "screen", "selection", "busy"] {
            assert!(required.iter().any(|item| item == key), "{key}");
        }
    }

    #[test]
    fn screen_follows_the_install_dialog_then_the_panel_then_the_route() {
        assert_eq!(synca_screen(Panel::Activity, Route::List, true), "install");
        assert_eq!(
            synca_screen(Panel::Activity, Route::List, false),
            "activity"
        );
        assert_eq!(
            synca_screen(Panel::Settings, Route::Plan, false),
            "settings"
        );
        assert_eq!(synca_screen(Panel::Closed, Route::List, false), "list");
        assert_eq!(synca_screen(Panel::Closed, Route::Plan, false), "plan");
        assert_eq!(synca_screen(Panel::Closed, Route::Update, false), "update");
    }

    #[test]
    fn a_fixture_state_keeps_old_keys_and_the_contract() {
        let stamped = ns_ui::stamp_state(
            synca_state_value(&SyncaDevState {
                scope: "user",
                selected: Some(2),
                selection_rows: vec![0, 1, 2],
                entries: 5,
                project: Some("/proj"),
                query: "demo",
                screen: "list",
                selection: Some(SyncaSelection {
                    index: 2,
                    key: "skill:demo",
                    kind: "skill",
                }),
                busy: true,
            }),
            "synca",
        );
        assert_eq!(stamped["scope"], "user");
        assert_eq!(stamped["selected"], 2);
        assert_eq!(stamped["selection_rows"], serde_json::json!([0, 1, 2]));
        assert_eq!(stamped["entries"], 5);
        assert_eq!(stamped["project"], "/proj");
        assert_eq!(stamped["query"], "demo");
        assert_eq!(stamped["screen"], "list");
        assert_eq!(stamped["selection"]["index"], 2);
        assert_eq!(stamped["selection"]["key"], "skill:demo");
        assert_eq!(stamped["selection"]["kind"], "skill");
        assert_eq!(stamped["busy"], true);
        assert_eq!(stamped["protocol"], 1);
        assert_eq!(stamped["app"], "synca");

        let empty = ns_ui::stamp_state(
            synca_state_value(&SyncaDevState {
                scope: "workspace",
                selected: None,
                selection_rows: Vec::new(),
                entries: 0,
                project: None,
                query: "",
                screen: "activity",
                selection: None,
                busy: false,
            }),
            "synca",
        );
        assert!(empty["selected"].is_null());
        assert!(
            empty["selection_rows"]
                .as_array()
                .is_some_and(Vec::is_empty)
        );
        assert!(empty["project"].is_null());
        assert!(empty["selection"].is_null());
        assert_eq!(empty["screen"], "activity");
        assert_eq!(empty["busy"], false);
    }
}

#[cfg(test)]
mod view_tests {
    use super::*;

    fn item(name: &str, path: &str, dir: bool, depth: u32) -> TreeItem {
        TreeItem {
            name: name.into(),
            path: path.into(),
            dir,
            depth,
        }
    }

    #[test]
    fn tree_nests_files_under_their_folder() {
        let items = vec![
            item("references", "/s/references", true, 0),
            item("a.md", "/s/references/a.md", false, 1),
            item("SKILL.md", "/s/SKILL.md", false, 0),
        ];
        let nodes = tree_nodes(&items);
        assert_eq!(nodes.len(), 2, "one folder and one file at the top");
    }

    #[test]
    fn open_folders_include_the_ones_above_the_open_file() {
        let items = vec![
            item("references", "/s/references", true, 0),
            item("deep", "/s/references/deep", true, 1),
            item("a.md", "/s/references/deep/a.md", false, 2),
        ];
        let open = open_folders("/s", "/s/references/deep/a.md", &items);
        assert!(open.contains(&"/s/references".to_string()));
        assert!(open.contains(&"/s/references/deep".to_string()));
    }

    #[test]
    fn skill_json_reads_source() {
        assert_eq!(
            SyncaWindow::json_skill_source(r#"{"source":"owner/repo"}"#).unwrap(),
            "owner/repo"
        );
    }

    #[test]
    fn mcp_json_accepts_server_object() {
        let value: serde_json::Value = serde_json::from_str(
            r#"{"name":"demo","transport":"http","url":"https://example.com/mcp"}"#,
        )
        .unwrap();
        assert_eq!(value["transport"], "http");
    }
    #[test]
    fn frontmatter_becomes_metadata_rows_and_body() {
        let (rows, body) =
            split_frontmatter("---\nname: demo\ndescription: first\n  second\n---\n# Body\n");
        assert_eq!(
            rows,
            vec![
                ("name".to_string(), "demo".to_string()),
                ("description".to_string(), "first second".to_string())
            ]
        );
        assert_eq!(body, "# Body\n");
    }

    #[test]
    fn metadata_parser_preserves_value_pipes() {
        let (rows, body) = markdown_with_metadata("---\nname: a | b\n---\nbody");
        assert_eq!(
            rows.unwrap(),
            vec![("name".to_string(), "a | b".to_string())]
        );
        assert_eq!(body, "body");
    }

    #[test]
    fn install_tabs_round_trip_and_unknown_falls_back_to_skill() {
        for tab in [InstallTab::Skill, InstallTab::Mcp] {
            assert_eq!(InstallTab::from_key(tab.key()), tab);
        }
        assert_eq!(InstallTab::from_key("nonsense"), InstallTab::Skill);
    }

    #[test]
    fn each_install_tab_has_its_own_detail() {
        assert_ne!(InstallTab::Skill.detail(), InstallTab::Mcp.detail());
    }

    #[test]
    fn detail_layout_changes_with_the_pane() {
        let narrow = detail_layout(360.);
        assert_eq!(narrow.card_cols, 1);
        assert!(narrow.files_stacked);
        assert!(!narrow.agents_beside);

        let medium = detail_layout(600.);
        assert_eq!(medium.card_cols, 2);
        assert!(medium.files_stacked);

        let split = detail_layout(700.);
        assert_eq!(split.card_cols, 2);
        assert!(!split.files_stacked);
        assert!(split.tree_w > 0.);

        let wide = detail_layout(900.);
        assert_eq!(wide.card_cols, 4);
        assert!(!wide.agents_beside);

        let beside = detail_layout(1100.);
        assert!(beside.agents_beside);
        assert!(beside.tree_w > wide.tree_w);
        assert!(4. * stat_card_width(800., 4) + 24. <= 800.);
        assert!(stat_card_width(400., 1) <= 400.);
    }

    #[test]
    fn mcp_token_estimate_uses_the_hub_or_any_agent() {
        let other = serde_json::json!({"command": "x"});
        let hub = serde_json::json!({"command": "hub-server-with-a-longer-body"});
        let row = McpRow {
            key: "demo".into(),
            presence: vec![
                McpPresence {
                    agent: "grok".into(),
                    path: String::new(),
                    fingerprint: String::new(),
                    normalized: Some(other),
                },
                McpPresence {
                    agent: engine::CANONICAL_AGENT.into(),
                    path: String::new(),
                    fingerprint: String::new(),
                    normalized: Some(hub.clone()),
                },
            ],
            mismatch: false,
        };
        assert_eq!(mcp_row_tokens(&row), mcp_tokens(Some(&hub)));
        let only_agent = McpRow {
            key: "demo".into(),
            presence: vec![McpPresence {
                agent: "grok".into(),
                path: String::new(),
                fingerprint: String::new(),
                normalized: Some(serde_json::json!({"command": "x"})),
            }],
            mismatch: false,
        };
        assert!(mcp_row_tokens(&only_agent).is_some());
    }

    #[test]
    fn preview_truncates_on_a_char_boundary() {
        let dir = std::env::temp_dir().join(format!("synca-preview-utf8-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("SKILL.md");
        // Byte 8000 falls inside a 2-byte char, the same panic the window hit
        // on skill files longer than the preview cap.
        let mut body = "a".repeat(7999);
        body.push('ó');
        body.push_str(&"b".repeat(100));
        std::fs::write(&path, &body).unwrap();

        let preview = read_preview(path.to_str().unwrap());
        assert!(preview.ends_with("\n…"));
        let shown = preview.trim_end_matches("\n…");
        assert!(shown.is_char_boundary(shown.len()));
        assert!(shown.len() <= 8000);
        assert_eq!(&body[..shown.len()], shown);

        let _ = std::fs::remove_dir_all(&dir);
    }
}
