//! The Registry tab of the Install dialog: skills.sh search and browse, a
//! detail area with the SKILL.md preview and audits, and the actions that hand
//! over to the existing plan route. Every call runs the `synca` CLI on a
//! background task. A sequence number per request keeps a slow answer from
//! overwriting a newer one.

use std::ops::Range;
use std::time::Duration;

use ely_gpui_component::buttons::ButtonVariant;
use ely_gpui_component::feedback::{EmptyState, InlineMessage};
#[cfg(feature = "devtools")]
use ely_gpui_component::forms::TextInput;
use ely_gpui_component::primitives::{IconName, Severity, Tooltip};
use gpui::{
    Context, FontWeight, InteractiveElement, IntoElement, ParentElement, ScrollHandle,
    SharedString, Styled, div, prelude::*, px,
};
use ns_ui::{
    Tone, chip, loading_pane, section_label, skeleton_list, text_field, theme, working_row,
};

#[cfg(feature = "devtools")]
use crate::InstallTab;
use crate::engine::{
    self, AuditRow, AuditStatus, RegistryInfo, RegistryRow, RegistrySkill, RegistryView,
};
use crate::{After, Apply, MarkdownCache, SyncaWindow};

/// Quiet period after a keystroke before a search goes to the network.
const REGISTRY_DEBOUNCE_MS: u64 = 300;
/// One result row, and the list area that scrolls them.
pub(crate) const ROW_H: f32 = 42.;
pub(crate) const LIST_H: f32 = 252.;
pub(crate) const ROW_OVERSCAN: f32 = 84.;
/// Height of the file preview, and the longest preview kept.
const PREVIEW_H: f32 = 260.;
const PREVIEW_LIMIT: usize = 64 * 1024;
const FILES_SHOWN: usize = 24;

/// A request that has not answered, answered, or failed.
pub(crate) enum Load<T> {
    Idle,
    Loading,
    Ready(T),
    Failed(String),
}

impl<T> Load<T> {
    #[cfg(feature = "devtools")]
    pub(crate) fn label(&self) -> &'static str {
        match self {
            Load::Idle => "idle",
            Load::Loading => "loading",
            Load::Ready(_) => "ready",
            Load::Failed(_) => "error",
        }
    }
}

pub(crate) struct RegistryState {
    pub(crate) view: RegistryView,
    pub(crate) query: String,
    /// The repository the Repo view lists.
    pub(crate) repo: String,
    /// Bumped for every list request; an answer for an older number is dropped.
    pub(crate) list_seq: u64,
    pub(crate) status: Load<()>,
    pub(crate) skills: Vec<RegistrySkill>,
    pub(crate) rows: Vec<RegistryRow>,
    pub(crate) total: usize,
    pub(crate) page: usize,
    pub(crate) has_more: bool,
    pub(crate) loading_more: bool,
    pub(crate) summary: String,
    pub(crate) selected: Option<usize>,
    /// Bumped for every selection; info and audit answers carry it.
    pub(crate) detail_seq: u64,
    pub(crate) info: Load<RegistryInfo>,
    pub(crate) audit: Load<Vec<AuditRow>>,
    pub(crate) open_file: Option<String>,
    pub(crate) markdown: Option<MarkdownCache>,
    pub(crate) prompt_busy: bool,
    /// Bumped for every query change so only the latest debounce fires.
    pub(crate) query_job: u64,
    pub(crate) list_scroll: ScrollHandle,
    pub(crate) file_scroll: ScrollHandle,
}

impl RegistryState {
    pub(crate) fn new() -> Self {
        Self {
            view: RegistryView::Trending,
            query: String::new(),
            repo: String::new(),
            list_seq: 0,
            status: Load::Idle,
            skills: Vec::new(),
            rows: Vec::new(),
            total: 0,
            page: 0,
            has_more: false,
            loading_more: false,
            summary: String::new(),
            selected: None,
            detail_seq: 0,
            info: Load::Idle,
            audit: Load::Idle,
            open_file: None,
            markdown: None,
            prompt_busy: false,
            query_job: 0,
            list_scroll: ScrollHandle::new(),
            file_scroll: ScrollHandle::new(),
        }
    }

    pub(crate) fn selected_skill(&self) -> Option<&RegistrySkill> {
        self.selected.and_then(|index| self.skills.get(index))
    }

    fn clear_list(&mut self) {
        self.skills.clear();
        self.rows.clear();
        self.total = 0;
        self.page = 0;
        self.has_more = false;
        self.loading_more = false;
        self.summary.clear();
        self.clear_detail();
        self.list_scroll.set_offset(gpui::point(px(0.), px(0.)));
    }

    fn clear_detail(&mut self) {
        self.selected = None;
        self.detail_seq = self.detail_seq.wrapping_add(1);
        self.info = Load::Idle;
        self.audit = Load::Idle;
        self.open_file = None;
        self.prompt_busy = false;
    }

    fn search_ready(&self) -> bool {
        self.query
            .trim()
            .trim_start_matches('-')
            .trim()
            .chars()
            .count()
            >= engine::SEARCH_MIN_CHARS
    }
}

/// The rows worth building for a viewport `view` tall that is `seen` points
/// down, padded by `overscan`. An unknown viewport keeps every row.
pub(crate) fn rows_window(
    count: usize,
    row_h: f32,
    view: f32,
    seen: f32,
    overscan: f32,
) -> Range<usize> {
    if count == 0 || view <= 0. {
        return 0..count;
    }
    let first = ((seen - overscan) / row_h).floor().clamp(0., count as f32) as usize;
    let last = ((seen + view + overscan) / row_h)
        .ceil()
        .clamp(first as f32, count as f32) as usize;
    first..last
}

/// What the list call returns once parsed.
enum Fetched {
    Page(engine::RegistryPage),
    Official(engine::OfficialList),
}

/// Runs the CLI and parses a successful answer. A failed exit keeps the
/// message and hint the CLI printed.
pub(crate) fn run_parsed<T>(
    args: Vec<String>,
    parse: fn(&str) -> Result<T, String>,
) -> Result<T, String> {
    let result = ns_ui::run_tool("synca", &args);
    let text = result.text();
    if result.ok {
        parse(&text)
    } else {
        Err(engine::failure_message(&text))
    }
}

impl SyncaWindow {
    /// The Registry tab came to the front: load the first list once.
    pub(crate) fn registry_activate(&mut self, cx: &mut Context<Self>) {
        if matches!(self.registry.status, Load::Idle)
            && (self.registry.view != RegistryView::Search || self.registry.search_ready())
        {
            self.registry_load(false, cx);
        }
        cx.notify();
    }

    pub(crate) fn registry_set_view(&mut self, view: RegistryView, cx: &mut Context<Self>) {
        if self.registry.view == view && matches!(self.registry.status, Load::Ready(_)) {
            return;
        }
        self.registry.view = view;
        self.registry.query_job = self.registry.query_job.wrapping_add(1);
        self.registry.list_seq = self.registry.list_seq.wrapping_add(1);
        self.registry.status = Load::Idle;
        self.registry.clear_list();
        if view != RegistryView::Search || self.registry.search_ready() {
            self.registry_load(false, cx);
        }
        cx.notify();
    }

    /// The search text changed. Typing switches to the Search view and loads
    /// after a quiet period; a query that is too short clears the results.
    pub(crate) fn registry_query_changed(&mut self, cx: &mut Context<Self>) {
        self.registry.query_job = self.registry.query_job.wrapping_add(1);
        let job = self.registry.query_job;
        if !self.registry.search_ready() {
            if self.registry.view == RegistryView::Search {
                self.registry.list_seq = self.registry.list_seq.wrapping_add(1);
                self.registry.status = Load::Idle;
                self.registry.clear_list();
            }
            cx.notify();
            return;
        }
        if self.registry.view == RegistryView::Repo && engine::looks_like_repo(&self.registry.query)
        {
            cx.notify();
            return;
        }
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(Duration::from_millis(REGISTRY_DEBOUNCE_MS))
                .await;
            this.update(cx, |this, cx| {
                if this.registry.query_job == job {
                    this.registry_search_now(cx);
                }
            })
            .ok();
        })
        .detach();
        cx.notify();
    }

    /// Lists the skills one repository holds.
    pub(crate) fn registry_open_repo(&mut self, source: &str, cx: &mut Context<Self>) {
        self.registry.query_job = self.registry.query_job.wrapping_add(1);
        self.registry.repo = source.trim().to_string();
        self.registry.view = RegistryView::Repo;
        self.registry.status = Load::Idle;
        self.registry.clear_list();
        self.registry_load(false, cx);
    }

    /// Enter in the search box, or the end of the debounce.
    pub(crate) fn registry_search_now(&mut self, cx: &mut Context<Self>) {
        self.registry.query_job = self.registry.query_job.wrapping_add(1);
        if !self.registry.search_ready() {
            return;
        }
        self.registry.view = RegistryView::Search;
        self.registry.clear_list();
        self.registry_load(false, cx);
    }

    /// Asks the CLI for the current view. `append` loads the next browse page.
    pub(crate) fn registry_load(&mut self, append: bool, cx: &mut Context<Self>) {
        let view = self.registry.view;
        let page = if append { self.registry.page + 1 } else { 0 };
        let repo = self.registry.repo.clone();
        let args = match view {
            RegistryView::Search => {
                engine::registry_search_args(&self.registry.query, engine::SEARCH_LIMIT)
            }
            RegistryView::Official => Ok(engine::registry_official_args()),
            RegistryView::Repo => engine::registry_discover_args(&self.registry.repo),
            _ => engine::registry_browse_args(view, page),
        };
        let args = match args {
            Ok(args) => args,
            Err(message) => {
                self.registry.status = Load::Failed(message);
                cx.notify();
                return;
            }
        };
        self.registry.list_seq = self.registry.list_seq.wrapping_add(1);
        let seq = self.registry.list_seq;
        if append {
            self.registry.loading_more = true;
        } else {
            self.registry.status = Load::Loading;
        }
        cx.notify();
        cx.spawn(async move |this, cx| {
            let fetched = cx
                .background_spawn(async move {
                    if view == RegistryView::Official {
                        run_parsed(args, engine::parse_official).map(Fetched::Official)
                    } else if view == RegistryView::Repo {
                        run_parsed(args, engine::parse_discover)
                            .map(|found| Fetched::Page(engine::discovered_page(&repo, found)))
                    } else if view == RegistryView::Search {
                        run_parsed(args, engine::parse_search).map(Fetched::Page)
                    } else {
                        run_parsed(args, engine::parse_browse).map(Fetched::Page)
                    }
                })
                .await;
            this.update(cx, |this, cx| {
                if this.registry.list_seq != seq {
                    return;
                }
                this.registry_apply(fetched, append);
                cx.notify();
            })
            .ok();
        })
        .detach();
    }

    fn registry_apply(&mut self, fetched: Result<Fetched, String>, append: bool) {
        let reg = &mut self.registry;
        reg.loading_more = false;
        match fetched {
            Err(message) => {
                if append {
                    reg.has_more = false;
                }
                reg.status = Load::Failed(message);
            }
            Ok(Fetched::Page(page)) => {
                let offset = if append { reg.skills.len() } else { 0 };
                if !append {
                    reg.skills.clear();
                    reg.rows.clear();
                }
                reg.rows
                    .extend((offset..offset + page.skills.len()).map(RegistryRow::Skill));
                reg.skills.extend(page.skills);
                reg.total = page.total.max(reg.skills.len());
                reg.page = page.page;
                reg.has_more = page.has_more;
                reg.summary = if reg.view == RegistryView::Repo {
                    format!("{} skills in {}", reg.skills.len(), reg.repo)
                } else if reg.view == RegistryView::Search {
                    match reg.skills.len() {
                        1 => "1 result".to_string(),
                        count => format!("{count} results"),
                    }
                } else {
                    format!("{} of {}", reg.skills.len(), reg.total)
                };
                reg.status = Load::Ready(());
            }
            Ok(Fetched::Official(list)) => {
                let (skills, rows) = engine::official_rows(&list);
                reg.summary = format!(
                    "{} owners · {} skills",
                    list.total_owners, list.total_skills
                );
                reg.total = skills.len();
                reg.skills = skills;
                reg.rows = rows;
                reg.page = 0;
                reg.has_more = false;
                reg.status = Load::Ready(());
            }
        }
    }

    /// Selects a result and asks for its snapshot and audits.
    pub(crate) fn registry_select(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(skill) = self.registry.skills.get(index) else {
            return;
        };
        let id = skill.id.clone();
        self.registry.selected = Some(index);
        self.registry.detail_seq = self.registry.detail_seq.wrapping_add(1);
        let seq = self.registry.detail_seq;
        self.registry.open_file = None;
        self.registry.prompt_busy = false;
        self.registry.file_scroll = ScrollHandle::new();
        match engine::registry_info_args(&id, true) {
            Ok(args) => {
                self.registry.info = Load::Loading;
                cx.spawn(async move |this, cx| {
                    let info = cx
                        .background_spawn(async move { run_parsed(args, engine::parse_info) })
                        .await;
                    this.update(cx, |this, cx| {
                        if this.registry.detail_seq != seq {
                            return;
                        }
                        this.registry.info = match info {
                            Ok(info) => Load::Ready(info),
                            Err(message) => Load::Failed(message),
                        };
                        cx.notify();
                    })
                    .ok();
                })
                .detach();
            }
            Err(message) => self.registry.info = Load::Failed(message),
        }
        match engine::registry_audit_args(&id) {
            Ok(args) => {
                self.registry.audit = Load::Loading;
                cx.spawn(async move |this, cx| {
                    let audits = cx
                        .background_spawn(async move { run_parsed(args, engine::parse_audit) })
                        .await;
                    this.update(cx, |this, cx| {
                        if this.registry.detail_seq != seq {
                            return;
                        }
                        this.registry.audit = match audits {
                            Ok(rows) => Load::Ready(rows),
                            Err(message) => Load::Failed(message),
                        };
                        cx.notify();
                    })
                    .ok();
                })
                .detach();
            }
            Err(message) => self.registry.audit = Load::Failed(message),
        }
        cx.notify();
    }

    /// `skills use`, then the prompt goes to the clipboard.
    pub(crate) fn registry_copy_prompt(&mut self, cx: &mut Context<Self>) {
        if self.registry.prompt_busy {
            return;
        }
        let Some(skill) = self.registry.selected_skill() else {
            return;
        };
        let args = match engine::registry_use_args(&skill.source, &skill.slug) {
            Ok(args) => args,
            Err(message) => {
                ns_ui::notify(&self.toaster, Severity::Warning, &message, cx);
                return;
            }
        };
        self.registry.prompt_busy = true;
        let seq = self.registry.detail_seq;
        cx.notify();
        cx.spawn(async move |this, cx| {
            let prompt = cx
                .background_spawn(async move { run_parsed(args, engine::parse_use) })
                .await;
            this.update(cx, |this, cx| {
                if this.registry.detail_seq == seq {
                    this.registry.prompt_busy = false;
                }
                match prompt {
                    Ok(prompt) => {
                        cx.write_to_clipboard(gpui::ClipboardItem::new_string(prompt.prompt));
                        ns_ui::notify(
                            &this.toaster,
                            Severity::Success,
                            "Prompt copied. Paste it into an agent.",
                            cx,
                        );
                    }
                    Err(message) => {
                        ns_ui::notify(&this.toaster, Severity::Warning, &message, cx);
                    }
                }
                cx.notify();
            })
            .ok();
        })
        .detach();
    }

    /// Dry-run first: the plan route shows it and holds the Apply button.
    pub(crate) fn registry_install(&mut self, cx: &mut Context<Self>) {
        let Some(skill) = self.registry.selected_skill() else {
            return;
        };
        let source = match engine::registry_install_source(skill) {
            Ok(source) => source,
            Err(message) => {
                ns_ui::notify(&self.toaster, Severity::Warning, &message, cx);
                return;
            }
        };
        match engine::registry_install_args(self.scope, self.dir(), &source, true) {
            Ok(args) => {
                self.onboarding = None;
                self.run(
                    engine::with_agents(args, &self.disabled),
                    "dry-run",
                    cx,
                    After::Plan(format!("install skill · {source}"), Apply::Registry(source)),
                );
            }
            Err(message) => ns_ui::notify(&self.toaster, Severity::Warning, &message, cx),
        }
    }

    /// Opens the skills.sh page. Only an https link is handed to the system.
    pub(crate) fn registry_open_page(&mut self, cx: &mut Context<Self>) {
        let Some(url) = self
            .registry
            .selected_skill()
            .map(|skill| skill.url.clone())
        else {
            return;
        };
        if url.starts_with("https://") {
            ns_ui::open_url(&url);
        } else {
            ns_ui::notify(
                &self.toaster,
                Severity::Warning,
                "The registry gave a link that is not https.",
                cx,
            );
        }
    }
}

/// The JSON the `state` dev command reports under `registry`.
#[cfg(feature = "devtools")]
pub(crate) fn state_value(reg: &RegistryState, open: bool) -> serde_json::Value {
    let error = match &reg.status {
        Load::Failed(message) => Some(message.as_str()),
        _ => None,
    };
    let audits = match &reg.audit {
        Load::Ready(rows) => rows.len(),
        _ => 0,
    };
    let files = match &reg.info {
        Load::Ready(info) => info.files.len(),
        _ => 0,
    };
    serde_json::json!({
        "open": open,
        "view": reg.view.key(),
        "query": reg.query,
        "repo": reg.repo,
        "status": reg.status.label(),
        "error": error,
        "count": reg.skills.len(),
        "total": reg.total,
        "page": reg.page,
        "has_more": reg.has_more,
        "loading_more": reg.loading_more,
        "summary": reg.summary,
        "selected": reg.selected_skill().map(|skill| skill.id.as_str()),
        "detail": {
            "info": reg.info.label(),
            "audit": reg.audit.label(),
            "audits": audits,
            "files": files,
            "open_file": reg.open_file,
        },
        "seq": reg.list_seq,
    })
}

/// Button row, list, and detail of the Registry tab.
pub(crate) fn body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    body = body
        .child(text_field("registry-query", &window.registry_input))
        .child(view_buttons(window.registry.view, cx))
        .children(repo_button(&window.registry, cx))
        .child(list_section(&window.registry, cx));
    if let Some(detail) = detail_section(window, cx) {
        body = body.child(detail);
    }
    body
}

fn view_buttons(current: RegistryView, cx: &mut Context<SyncaWindow>) -> impl IntoElement {
    let tips = [
        "Search skills.sh by name",
        "Most installed in the last day",
        "Rising fastest on skills.sh right now",
        "Most installed of all time",
        "Skills from their owners' official accounts (needs an API token)",
    ];
    let labels = ["Search", "Trending", "Hot", "All time", "Official"];
    let mut row = div().flex().flex_wrap().gap_1();
    for (index, view) in RegistryView::ALL.into_iter().enumerate() {
        row = row.child(crate::tipped_button(
            format!("registry-view-{}", view.key()),
            None,
            labels[index],
            tips[index],
            if view == current {
                ButtonVariant::Primary
            } else {
                ButtonVariant::Secondary
            },
            false,
            cx.listener(move |this, _, _, cx| this.registry_set_view(view, cx)),
        ));
    }
    row
}

/// A typed `owner/repo` can list that repository's skills.
fn repo_button(reg: &RegistryState, cx: &mut Context<SyncaWindow>) -> Option<gpui::AnyElement> {
    let source = reg.query.trim().to_string();
    if !engine::looks_like_repo(&source) {
        return None;
    }
    let tip = format!("List the skills in {source}");
    Some(
        div()
            .child(crate::tipped_button(
                "registry-repo",
                Some("folder"),
                format!("Skills in {source}"),
                tip,
                ButtonVariant::Secondary,
                false,
                cx.listener(move |this, _, _, cx| this.registry_open_repo(&source, cx)),
            ))
            .into_any_element(),
    )
}

fn list_frame(child: impl IntoElement) -> gpui::AnyElement {
    div()
        .w_full()
        .h(px(LIST_H))
        .overflow_hidden()
        .child(child)
        .into_any_element()
}

fn list_section(reg: &RegistryState, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    match &reg.status {
        Load::Loading => list_frame(
            div()
                .flex()
                .flex_col()
                .child(working_row("registry-wait", "Searching skills.sh…"))
                .child(skeleton_list("registry-skeleton", 5)),
        ),
        Load::Idle if reg.view == RegistryView::Repo => list_frame(
            EmptyState::new("registry-repo-idle", IconName::Search, "Pick a repository")
                .body("Type owner/repo in the search box."),
        ),
        Load::Idle if reg.view == RegistryView::Search => list_frame(
            EmptyState::new("registry-idle", IconName::Search, "Search skills.sh")
                .body("Type at least 2 characters, or pick Trending, Hot, or All time."),
        ),
        Load::Idle => list_frame(loading_pane("registry-idle-wait", "Loading…")),
        Load::Failed(message) => list_frame(failure_view(message, cx)),
        Load::Ready(()) if reg.rows.is_empty() => list_frame(
            EmptyState::new("registry-empty", IconName::Inbox, "No skills found")
                .body("Try another word or a different list."),
        ),
        Load::Ready(()) => rows_view(reg, cx),
    }
}

/// The failure message, its hint on the next line, and a way to try again.
fn failure_view(message: &str, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    let theme = theme();
    let (head, hint) = match message.split_once("\nhint: ") {
        Some((head, hint)) => (head.to_string(), Some(hint.to_string())),
        None => (message.to_string(), None),
    };
    div()
        .flex()
        .flex_col()
        .gap_2()
        .p_2()
        .child(InlineMessage::new(Severity::Warning, head))
        .children(hint.map(|hint| {
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(hint))
        }))
        .child(crate::tipped_button(
            "registry-retry",
            Some("refresh"),
            "Try again",
            "Ask skills.sh again",
            ButtonVariant::Secondary,
            false,
            cx.listener(|this, _, _, cx| {
                this.registry.list_seq = this.registry.list_seq.wrapping_add(1);
                this.registry.status = Load::Idle;
                this.registry_activate(cx);
            }),
        ))
        .into_any_element()
}

fn rows_view(reg: &RegistryState, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    let theme = theme();
    let count = reg.rows.len();
    let seen = -f32::from(reg.list_scroll.offset().y);
    let window = rows_window(count, ROW_H, LIST_H, seen.max(0.), ROW_OVERSCAN);
    let mut list = div().flex().flex_col();
    if window.start > 0 {
        list = list.child(div().w_full().h(px(window.start as f32 * ROW_H)));
    }
    for index in window.clone() {
        list = list.child(match &reg.rows[index] {
            RegistryRow::Owner {
                name,
                installs,
                skills,
            } => owner_row(index, name, *installs, *skills).into_any_element(),
            RegistryRow::Skill(at) => {
                skill_row(index, *at, &reg.skills[*at], reg.selected == Some(*at), cx)
                    .into_any_element()
            }
        });
    }
    if window.end < count {
        list = list.child(div().w_full().h(px((count - window.end) as f32 * ROW_H)));
    }
    if reg.has_more {
        list = list.child(div().w_full().py_2().flex().justify_center().child(
            crate::tipped_button(
                "registry-more",
                Some("download"),
                if reg.loading_more {
                    "Loading…"
                } else {
                    "Load more"
                },
                "Load the next 200 skills",
                ButtonVariant::Secondary,
                reg.loading_more,
                cx.listener(|this, _, _, cx| this.registry_load(true, cx)),
            ),
        ));
    }
    let handle = reg.list_scroll.clone();
    div()
        .flex()
        .flex_col()
        .gap_1()
        .child(
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(reg.summary.clone())),
        )
        .child(
            div()
                .id("registry-list")
                .w_full()
                .h(px(LIST_H))
                .overflow_y_scroll()
                .track_scroll(&reg.list_scroll)
                .on_scroll_wheel(move |event, window, cx| {
                    crate::contain_scroll(&handle, event, window, cx)
                })
                .rounded_md()
                .border_1()
                .border_color(theme.border)
                .child(list),
        )
        .into_any_element()
}

fn owner_row(index: usize, name: &str, installs: u64, skills: usize) -> impl IntoElement {
    let theme = theme();
    div()
        .id(SharedString::from(format!("registry-owner-{index}")))
        .h(px(ROW_H))
        .w_full()
        .px_2()
        .flex()
        .items_center()
        .gap_2()
        .bg(theme.panel)
        .tooltip(Tooltip::text(format!(
            "{name}: {skills} official skills, {} installs",
            engine::format_installs(installs)
        )))
        .child(
            div()
                .flex_1()
                .min_w(px(0.))
                .truncate()
                .text_sm()
                .font_weight(FontWeight::SEMIBOLD)
                .child(SharedString::from(name.to_string())),
        )
        .child(
            div()
                .flex_none()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(format!(
                    "{skills} skills · {}",
                    engine::format_installs(installs)
                ))),
        )
}

fn skill_row(
    row: usize,
    index: usize,
    skill: &RegistrySkill,
    selected: bool,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let theme = theme();
    let place = if skill.source_type == "github" {
        skill.source.clone()
    } else {
        format!("{} · {}", skill.source, skill.source_type)
    };
    let tip = format!(
        "{} · {} installs. Click to see the preview, audits and actions.",
        skill.id,
        engine::format_installs(skill.installs)
    );
    div()
        .id(SharedString::from(format!("registry-row-{row}")))
        .h(px(ROW_H))
        .w_full()
        .px_2()
        .flex()
        .items_center()
        .gap_2()
        .cursor_pointer()
        .when(selected, |item| item.bg(theme.selected))
        .when(!selected, |item| item.hover(|style| style.bg(theme.hover)))
        .tooltip(Tooltip::text(tip))
        .on_click(cx.listener(move |this, _, _, cx| this.registry_select(index, cx)))
        .child(
            div()
                .flex_1()
                .min_w(px(0.))
                .flex()
                .flex_col()
                .child(
                    div()
                        .truncate()
                        .text_sm()
                        .text_color(theme.text)
                        .child(SharedString::from(skill.name.clone())),
                )
                .child(
                    div()
                        .truncate()
                        .text_xs()
                        .text_color(theme.muted)
                        .child(SharedString::from(place)),
                ),
        )
        .children(skill.official.then(|| chip("official", Tone::Accent)))
        .child(
            div()
                .flex_none()
                .w(px(44.))
                .flex()
                .justify_end()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(engine::format_installs(skill.installs))),
        )
}

fn audit_severity(status: AuditStatus) -> Severity {
    match status {
        AuditStatus::Pass => Severity::Success,
        AuditStatus::Warn => Severity::Warning,
        AuditStatus::Fail => Severity::Danger,
        AuditStatus::Unknown => Severity::Info,
    }
}

/// Cuts a preview on a char boundary so a huge snapshot never stalls a frame.
pub(crate) fn clip_preview(text: &str) -> String {
    if text.len() <= PREVIEW_LIMIT {
        return text.to_string();
    }
    let mut end = PREVIEW_LIMIT;
    while !text.is_char_boundary(end) {
        end -= 1;
    }
    format!("{}\n…", &text[..end])
}

fn detail_section(
    window: &mut SyncaWindow,
    cx: &mut Context<SyncaWindow>,
) -> Option<gpui::AnyElement> {
    let theme = theme();
    let scope_project = window.scope == engine::Scope::Project;
    let reg = &mut window.registry;
    let skill = reg.selected_skill()?.clone();
    let install = engine::registry_install_source(&skill);
    let installable = install.is_ok();
    let well_known = skill.source_type == "well-known";
    let mut meta = vec![skill.source.clone(), skill.source_type.clone()];
    let installs = match &reg.info {
        Load::Ready(info) => info.installs.unwrap_or(skill.installs),
        _ => skill.installs,
    };
    meta.push(format!("{} installs", engine::format_installs(installs)));
    if let Some(change) = skill.change {
        meta.push(format!("{change:+} today"));
    }
    if skill.duplicate {
        meta.push("duplicate listing".into());
    }
    let install_tip = match &install {
        Ok(_) if scope_project => {
            "Preview the install in this workspace, then apply it from the plan".to_string()
        }
        Ok(_) => "Preview the install for the user scope, then apply it from the plan".to_string(),
        Err(message) => message.clone(),
    };
    let prompt_tip = if well_known {
        "A prompt is not available for well-known sites yet".to_string()
    } else {
        "Copy a prompt with this SKILL.md to paste into an agent".to_string()
    };
    let mut panel = div()
        .w_full()
        .flex()
        .flex_col()
        .gap_2()
        .pt_2()
        .border_t_1()
        .border_color(theme.border)
        .child(
            div()
                .flex()
                .items_center()
                .gap_2()
                .flex_wrap()
                .child(
                    div()
                        .min_w(px(0.))
                        .text_lg()
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(SharedString::from(skill.name.clone())),
                )
                .children(skill.official.then(|| chip("official", Tone::Accent))),
        )
        .child(
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(meta.join(" · "))),
        )
        .child(
            div()
                .flex()
                .flex_wrap()
                .gap_2()
                .child(crate::tipped_button(
                    "registry-install",
                    Some("download"),
                    "Install…",
                    install_tip,
                    ButtonVariant::Primary,
                    !installable,
                    cx.listener(|this, _, _, cx| this.registry_install(cx)),
                ))
                .child(crate::tipped_button(
                    "registry-prompt",
                    Some("copy"),
                    if reg.prompt_busy {
                        "Copying…"
                    } else {
                        "Copy prompt"
                    },
                    prompt_tip,
                    ButtonVariant::Secondary,
                    well_known || reg.prompt_busy,
                    cx.listener(|this, _, _, cx| this.registry_copy_prompt(cx)),
                ))
                .child(crate::tipped_button(
                    "registry-repo-skills",
                    Some("folder"),
                    "More from this repo",
                    format!("List every skill in {}", skill.source),
                    ButtonVariant::Secondary,
                    well_known,
                    {
                        let source = skill.source.clone();
                        cx.listener(move |this, _, _, cx| this.registry_open_repo(&source, cx))
                    },
                ))
                .child(crate::tipped_button(
                    "registry-open",
                    Some("globe"),
                    "Open on skills.sh",
                    skill.url.clone(),
                    ButtonVariant::Secondary,
                    false,
                    cx.listener(|this, _, _, cx| this.registry_open_page(cx)),
                )),
        );

    panel = panel.child(section_label("Audits"));
    panel = panel.child(match &reg.audit {
        Load::Idle | Load::Loading => div()
            .child(working_row("registry-audit-wait", "Reading audits…"))
            .into_any_element(),
        Load::Failed(message) => {
            InlineMessage::new(Severity::Warning, message.clone()).into_any_element()
        }
        Load::Ready(rows) if rows.is_empty() => {
            InlineMessage::new(Severity::Info, "No audits for this skill yet.").into_any_element()
        }
        Load::Ready(rows) => {
            let mut list = div().flex().flex_col().gap_2();
            for row in rows {
                let mut notes = Vec::new();
                if let Some(risk) = &row.risk_level {
                    notes.push(format!("Risk {risk}"));
                }
                if !row.categories.is_empty() {
                    notes.push(row.categories.join(", "));
                }
                if let Some(day) = row.audited_at.get(..10) {
                    notes.push(format!("Audited {day}"));
                }
                list = list.child(
                    div()
                        .flex()
                        .flex_col()
                        .gap_1()
                        .child(InlineMessage::new(
                            audit_severity(row.status),
                            format!("{}: {}. {}", row.provider, row.status.label(), row.summary)
                                .trim_end_matches(". ")
                                .to_string(),
                        ))
                        .children((!notes.is_empty()).then(|| {
                            div()
                                .pl_4()
                                .text_xs()
                                .text_color(theme.muted)
                                .child(SharedString::from(notes.join(" · ")))
                        })),
                );
            }
            list.into_any_element()
        }
    });

    panel = panel.child(section_label("Files"));
    match &reg.info {
        Load::Idle | Load::Loading => {
            panel = panel.child(working_row("registry-info-wait", "Reading the snapshot…"));
        }
        Load::Failed(message) => {
            panel = panel.child(InlineMessage::new(
                Severity::Info,
                format!("No preview: {message}"),
            ));
        }
        Load::Ready(info) if info.files.is_empty() => {
            panel = panel.child(InlineMessage::new(
                Severity::Info,
                "The registry has no files for this skill.",
            ));
        }
        Load::Ready(info) => {
            let open = reg
                .open_file
                .clone()
                .or_else(|| info.skill_md().map(|file| file.path.clone()))
                .or_else(|| info.files.first().map(|file| file.path.clone()))
                .unwrap_or_default();
            let mut files = div().flex().flex_col();
            for (index, file) in info.files.iter().take(FILES_SHOWN).enumerate() {
                let path = file.path.clone();
                let on = path == open;
                files = files.child(
                    div()
                        .id(SharedString::from(format!("registry-file-{index}")))
                        .h(px(28.))
                        .px_2()
                        .flex()
                        .items_center()
                        .gap_2()
                        .rounded_md()
                        .cursor_pointer()
                        .text_xs()
                        .when(on, |item| item.bg(theme.selected))
                        .when(!on, |item| item.hover(|style| style.bg(theme.hover)))
                        .tooltip(Tooltip::text(format!("Preview {}", file.path)))
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.registry.open_file = Some(path.clone());
                            this.registry.file_scroll = ScrollHandle::new();
                            cx.notify();
                        }))
                        .child(
                            div()
                                .flex_1()
                                .min_w(px(0.))
                                .truncate()
                                .child(SharedString::from(file.path.clone())),
                        )
                        .child(
                            div()
                                .flex_none()
                                .text_color(theme.muted)
                                .child(SharedString::from(format!("{} B", file.size))),
                        ),
                );
            }
            if info.files.len() > FILES_SHOWN {
                files = files.child(div().px_2().text_xs().text_color(theme.muted).child(
                    SharedString::from(format!(
                        "and {} more files",
                        info.files.len() - FILES_SHOWN
                    )),
                ));
            }
            panel = panel.child(files);
            let content = info
                .files
                .iter()
                .find(|file| file.path == open)
                .and_then(|file| file.contents.as_deref());
            match content {
                Some(text) => {
                    let clipped = clip_preview(text);
                    let handle = reg.file_scroll.clone();
                    panel = panel.child(
                        div()
                            .id("registry-preview")
                            .w_full()
                            .h(px(PREVIEW_H))
                            .overflow_y_scroll()
                            .track_scroll(&reg.file_scroll)
                            .on_scroll_wheel(move |event, window, cx| {
                                crate::contain_scroll(&handle, event, window, cx)
                            })
                            .child(crate::file_body(&open, &clipped, &mut reg.markdown, cx)),
                    );
                }
                None => {
                    panel = panel.child(InlineMessage::new(
                        Severity::Info,
                        "This file has no preview in the registry.",
                    ));
                }
            }
        }
    }
    Some(panel.into_any_element())
}

/// Dev commands set the search box without waiting for a keystroke.
#[cfg(feature = "devtools")]
pub(crate) fn dev_open(
    window: &mut SyncaWindow,
    view: Option<RegistryView>,
    query: Option<&str>,
    cx: &mut Context<SyncaWindow>,
) {
    window.onboarding = Some(InstallTab::Registry);
    if let Some(query) = query {
        window.registry.query = query.to_string();
        let input: gpui::Entity<TextInput> = window.registry_input.clone();
        input.update(cx, |input, cx| input.set_text(query.to_string(), cx));
    }
    match (view, query) {
        (Some(view), _) => {
            window.registry.status = Load::Idle;
            window.registry_set_view(view, cx);
        }
        (None, Some(query)) if query.trim().chars().count() >= engine::SEARCH_MIN_CHARS => {
            window.registry_search_now(cx);
        }
        _ => window.registry_activate(cx),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rows_window_builds_the_viewport_plus_overscan() {
        assert_eq!(rows_window(0, 42., 252., 0., 84.), 0..0);
        assert_eq!(rows_window(100, 42., 0., 900., 84.), 0..100);
        assert_eq!(rows_window(100, 42., 252., 0., 84.), 0..8);
        assert_eq!(rows_window(100, 42., 252., 4200., 84.), 98..100);
        assert!(rows_window(100, 42., 252., 400_000., 84.).is_empty());
        let middle = rows_window(100, 42., 252., 840., 84.);
        assert!(middle.start <= 840 / 42 && middle.end >= (840 + 252) / 42);
    }

    #[test]
    fn preview_cuts_on_a_char_boundary() {
        let mut text = "a".repeat(PREVIEW_LIMIT - 1);
        text.push('ó');
        text.push_str("tail");
        let clipped = clip_preview(&text);
        assert!(clipped.ends_with("\n…"));
        assert!(clipped.len() <= PREVIEW_LIMIT + 4);
        assert_eq!(clip_preview("short"), "short");
    }

    #[test]
    fn audit_status_maps_to_theme_severity() {
        assert_eq!(audit_severity(AuditStatus::Pass), Severity::Success);
        assert_eq!(audit_severity(AuditStatus::Warn), Severity::Warning);
        assert_eq!(audit_severity(AuditStatus::Fail), Severity::Danger);
        assert_eq!(audit_severity(AuditStatus::Unknown), Severity::Info);
    }

    #[test]
    fn a_short_query_is_not_searchable() {
        let mut state = RegistryState::new();
        state.query = " - ".into();
        assert!(!state.search_ready());
        state.query = "a".into();
        assert!(!state.search_ready());
        state.query = " re ".into();
        assert!(state.search_ready());
    }

    #[test]
    fn an_older_answer_is_recognised_by_its_sequence() {
        let mut state = RegistryState::new();
        let first = state.list_seq.wrapping_add(1);
        state.list_seq = first;
        let second = state.list_seq.wrapping_add(1);
        state.list_seq = second;
        assert_ne!(state.list_seq, first);
        assert_eq!(state.list_seq, second);
        state.selected = Some(0);
        let before = state.detail_seq;
        state.clear_detail();
        assert_ne!(state.detail_seq, before);
        assert!(state.selected.is_none());
    }
}
