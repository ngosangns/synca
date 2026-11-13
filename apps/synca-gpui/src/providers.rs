//! Settings → "Providers": one list of every agent a skills backend can
//! install to, read from `synca providers list --json` on a background task.
//! Every row carries an on/off toggle: the six agents the CLI syncs to save
//! their choice in the disabled list and show a Delete data label. The rest
//! save theirs in the enabled list (the CLI cannot sync or clear them yet).
//! The six rows render even before the list
//! loads or when the CLI cannot answer. A sequence number per request keeps a
//! slow answer from replacing a newer one. Sort buttons beside the filter
//! reorder the rows: name, status, or skills, with the sidebar tables'
//! ascending → descending → load order click cycle.

use ely_gpui_component::buttons::ButtonVariant;
use ely_gpui_component::feedback::{EmptyState, InlineMessage};
use ely_gpui_component::primitives::{IconName, Severity, Tooltip};
use gpui::{
    Context, FontWeight, InteractiveElement, IntoElement, ParentElement, ScrollHandle,
    SharedString, Styled, div, prelude::*, px,
};
use ns_ui::{
    IconTone, Tone, chip, icon_button, section_label, skeleton_list, text_field, theme,
    warn_button, working_row,
};

use crate::Pending;
use crate::SyncaWindow;
use crate::engine::{self, PROVIDERS, ProviderInfo, Scope};
use crate::registry::{Load, run_parsed};

/// One provider row, and the list area that scrolls them.
const ROW_H: f32 = 54.;
const LIST_H: f32 = 324.;

/// Tooltip of a provider outside the six built-in sync targets.
const READ_ONLY_TIP: &str =
    "Saved as a preference. The CLI currently syncs skills to the six built-in agents.";

/// Which key the rows are ordered by. `None` keeps load order: managed
/// agents first, then the installed ones, then the rest, each by name.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub(crate) enum ProviderSortColumn {
    #[default]
    None,
    Name,
    Status,
    Skills,
}

/// One click sorts ascending, another reverses, a third restores load order —
/// the cycle the sidebar tables use.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub(crate) struct ProviderSort {
    pub(crate) column: ProviderSortColumn,
    pub(crate) ascending: bool,
}

impl ProviderSort {
    fn next(self, column: ProviderSortColumn) -> Self {
        if self.column != column {
            Self {
                column,
                ascending: true,
            }
        } else if self.ascending {
            Self {
                column,
                ascending: false,
            }
        } else {
            Self::default()
        }
    }

    /// The `sort` value the dev `state` command reports.
    pub(crate) fn label(self) -> &'static str {
        match (self.column, self.ascending) {
            (ProviderSortColumn::None, _) => "load",
            (ProviderSortColumn::Name, true) => "name-asc",
            (ProviderSortColumn::Name, false) => "name-desc",
            (ProviderSortColumn::Status, true) => "status-asc",
            (ProviderSortColumn::Status, false) => "status-desc",
            (ProviderSortColumn::Skills, true) => "skills-asc",
            (ProviderSortColumn::Skills, false) => "skills-desc",
        }
    }
}

/// `sort=` on the dev `providers` command: "name", "status", or "skills",
/// each with an optional "-desc" suffix; "load" restores load order.
pub(crate) fn parse_sort(text: &str) -> Result<ProviderSort, String> {
    let (base, ascending) = match text.strip_suffix("-desc") {
        Some(base) => (base, false),
        None => (text, true),
    };
    let column = match base {
        "load" => ProviderSortColumn::None,
        "name" => ProviderSortColumn::Name,
        "status" => ProviderSortColumn::Status,
        "skills" => ProviderSortColumn::Skills,
        other => {
            return Err(format!(
                "unknown sort \"{other}\"; use name, status, skills, or load"
            ));
        }
    };
    // "load" has no direction: both spellings mean the default order.
    Ok(ProviderSort {
        column,
        ascending: column != ProviderSortColumn::None && ascending,
    })
}

pub(crate) struct ProvidersState {
    pub(crate) query: String,
    /// Bumped for every request and every invalidation; an answer for an
    /// older number is dropped.
    pub(crate) seq: u64,
    pub(crate) status: Load<()>,
    /// Every provider, sorted by name.
    pub(crate) list: Vec<ProviderInfo>,
    /// Indexes into `list` of the rows that match `query`, in `sort` order.
    pub(crate) shown: Vec<usize>,
    pub(crate) sort: ProviderSort,
    /// The answer describes project folders (the Workspace scope).
    pub(crate) project: bool,
    /// The Workspace scope has no folder yet, so the answer lists user folders.
    pub(crate) fallback: bool,
    pub(crate) scroll: ScrollHandle,
}

impl ProvidersState {
    pub(crate) fn new() -> Self {
        Self {
            query: String::new(),
            seq: 0,
            status: Load::Idle,
            list: Vec::new(),
            shown: Vec::new(),
            sort: ProviderSort::default(),
            project: false,
            fallback: false,
            scroll: ScrollHandle::new(),
        }
    }

    /// The managed provider whose Synca id is `id`.
    pub(crate) fn managed(&self, id: &str) -> Option<&ProviderInfo> {
        self.list
            .iter()
            .find(|item| item.managed && item.synca_agent.as_deref() == Some(id))
    }

    /// Rebuilds `shown` from `list`, `query`, and `sort`.
    pub(crate) fn refilter(&mut self) {
        let needle = self.query.trim().to_lowercase();
        let mut shown: Vec<usize> = self
            .list
            .iter()
            .enumerate()
            .filter(|(_, item)| item.matches(&needle))
            .map(|(index, _)| index)
            .collect();
        let column = self.sort.column;
        if column != ProviderSortColumn::None {
            let ascending = self.sort.ascending;
            let list = &self.list;
            shown.sort_by(|&left, &right| {
                let (a, b) = (&list[left], &list[right]);
                let ord = match column {
                    ProviderSortColumn::None => std::cmp::Ordering::Equal,
                    ProviderSortColumn::Name => {
                        a.title().to_lowercase().cmp(&b.title().to_lowercase())
                    }
                    ProviderSortColumn::Status => status_rank(a).cmp(&status_rank(b)),
                    ProviderSortColumn::Skills => a.skills.cmp(&b.skills),
                }
                .then_with(|| a.title().to_lowercase().cmp(&b.title().to_lowercase()))
                .then_with(|| a.id.cmp(&b.id));
                if ascending { ord } else { ord.reverse() }
            });
        }
        self.shown = shown;
    }
}

fn status_label(info: &ProviderInfo) -> (&'static str, Tone) {
    if info.managed {
        ("managed", Tone::Accent)
    } else if info.present {
        ("installed", Tone::Ok)
    } else {
        ("not installed", Tone::Neutral)
    }
}

/// The sort order of the status chip: managed, installed, not installed.
fn status_rank(info: &ProviderInfo) -> u8 {
    if info.managed {
        0
    } else if info.present {
        1
    } else {
        2
    }
}

fn skills_text(info: &ProviderInfo) -> String {
    if info.skills == 1 {
        "1 skill".into()
    } else {
        format!("{} skills", info.skills)
    }
}

impl SyncaWindow {
    /// The scope or workspace changed: the loaded answer describes other
    /// folders. Drop it and any request still in flight, then load again if
    /// Settings is open.
    pub(crate) fn providers_invalidate(&mut self, cx: &mut Context<Self>) {
        self.providers.seq = self.providers.seq.wrapping_add(1);
        self.providers.status = Load::Idle;
        self.providers_activate(cx);
    }

    /// Settings is on screen: load once if nothing is loaded or loading.
    pub(crate) fn providers_activate(&mut self, cx: &mut Context<Self>) {
        if self.panel == crate::Panel::Settings && matches!(self.providers.status, Load::Idle) {
            self.providers_load(cx);
        }
    }

    pub(crate) fn providers_load(&mut self, cx: &mut Context<Self>) {
        // The Workspace scope without a folder has no project paths to show;
        // list the user folders and say so.
        let fallback = self.scope == Scope::Project && self.dir().is_none();
        let scope = if fallback { Scope::User } else { self.scope };
        let dir = self.dir().map(str::to_string);
        let args = match engine::providers_args(scope, dir.as_deref()) {
            Ok(args) => args,
            Err(message) => {
                self.providers.status = Load::Failed(message);
                cx.notify();
                return;
            }
        };
        self.providers.seq = self.providers.seq.wrapping_add(1);
        let seq = self.providers.seq;
        self.providers.status = Load::Loading;
        cx.notify();
        cx.spawn(async move |this, cx| {
            let fetched = cx
                .background_spawn(async move { run_parsed(args, engine::parse_providers) })
                .await;
            this.update(cx, |this, cx| {
                if this.providers.seq != seq {
                    return;
                }
                match fetched {
                    Ok(list) => {
                        this.providers.list = list;
                        this.providers.project = scope == Scope::Project;
                        this.providers.fallback = fallback;
                        this.providers.refilter();
                        this.providers.status = Load::Ready(());
                    }
                    Err(message) => {
                        this.providers.list.clear();
                        this.providers.shown.clear();
                        this.providers.status = Load::Failed(engine::providers_failure(&message));
                    }
                }
                cx.notify();
            })
            .ok();
        })
        .detach();
    }

    pub(crate) fn providers_set_query(&mut self, text: String, cx: &mut Context<Self>) {
        if text == self.providers.query {
            return;
        }
        self.providers.query = text;
        self.providers.refilter();
        self.providers
            .scroll
            .set_offset(gpui::point(px(0.), px(0.)));
        cx.notify();
    }

    pub(crate) fn providers_set_sort(
        &mut self,
        column: ProviderSortColumn,
        cx: &mut Context<Self>,
    ) {
        self.providers.sort = self.providers.sort.next(column);
        self.providers.refilter();
        self.providers
            .scroll
            .set_offset(gpui::point(px(0.), px(0.)));
        cx.notify();
    }
}

/// The JSON the `state` dev command reports under `providers`.
#[cfg(feature = "devtools")]
pub(crate) fn state_value(state: &ProvidersState, open: bool) -> serde_json::Value {
    let error = match &state.status {
        Load::Failed(message) => Some(message.as_str()),
        _ => None,
    };
    serde_json::json!({
        "open": open,
        "status": state.status.label(),
        "error": error,
        "query": state.query,
        "sort": state.sort.label(),
        "count": state.list.len(),
        "managed": state.list.iter().filter(|item| item.managed).count(),
        "present": state.list.iter().filter(|item| item.present).count(),
        "shown": state.shown.len(),
        "scope": if state.project { "project" } else { "user" },
        "fallback": state.fallback,
        "seq": state.seq,
    })
}

/// The folder (selectable, ellipsised) and the skills count, led by the id
/// when `with_id`. `ident` keeps the selectable text ids apart.
fn path_line(info: &ProviderInfo, project: bool, ident: &str, with_id: bool) -> gpui::Div {
    let theme = theme();
    let mut line = div()
        .w_full()
        .min_w(px(0.))
        .flex()
        .items_center()
        .gap_2()
        .text_xs()
        .text_color(theme.muted);
    if with_id {
        line = line.child(div().flex_none().child(SharedString::from(info.id.clone())));
    }
    let place = div().flex_1().min_w(px(0.)).truncate();
    line.child(match info.location(project) {
        Some(path) => place
            .child(crate::selectable(
                SharedString::from(format!("provider-path-{ident}")),
                SharedString::from(path.to_string()),
            ))
            .into_any_element(),
        None => place.child("No folder for this scope").into_any_element(),
    })
    .child(
        div()
            .flex_none()
            .child(SharedString::from(skills_text(info))),
    )
}

fn chips(info: &ProviderInfo) -> gpui::Div {
    let (label, tone) = status_label(info);
    div()
        .flex_none()
        .flex()
        .items_center()
        .gap_1()
        .child(chip(label, tone))
        .children(info.universal.then(|| chip("universal", Tone::Neutral)))
}

/// The "Providers" section of Settings: one list with the managed agents and
/// every other agent the backend can install to.
pub(crate) fn section(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let theme = theme();
    body = body.child(section_label("Providers"));
    let state = &window.providers;
    match &state.status {
        Load::Idle | Load::Loading => body
            .child(working_row("providers-wait", "Loading providers…"))
            .child(skeleton_list("providers-skeleton", 5))
            .child(managed_rows(window, cx)),
        Load::Failed(message) => body
            .child(InlineMessage::new(Severity::Warning, message.clone()))
            .child(div().child(crate::tipped_button(
                "providers-retry",
                Some("refresh"),
                "Try again",
                "Ask synca for the provider list again",
                ButtonVariant::Secondary,
                false,
                cx.listener(|this, _, _, cx| this.providers_invalidate(cx)),
            )))
            .child(managed_rows(window, cx)),
        Load::Ready(()) => {
            let mut summary = engine::providers_summary(&state.list);
            if !state.query.trim().is_empty() {
                summary.push_str(&format!(" · {} match", state.shown.len()));
            }
            body = body
                .child(
                    div()
                        .px_2()
                        .text_xs()
                        .text_color(theme.muted)
                        .child(
                            "Every agent toggles on or off. Synca currently syncs skills and MCP servers to the six built-in agents; other choices are saved.",
                        ),
                )
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap_2()
                        .child(
                            div().flex_1().min_w(px(0.)).child(text_field(
                                "providers-query",
                                &window.providers_input,
                            )),
                        )
                        .child(sort_row(window, cx)),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .px_2()
                        .text_xs()
                        .text_color(theme.muted)
                        .child(SharedString::from(summary)),
                );
            if state.fallback {
                body = body.child(div().px_2().text_xs().text_color(theme.muted).child(
                    "Choose a workspace folder to see project folders. Showing user folders.",
                ));
            }
            body.child(list_view(window, cx))
        }
    }
}

/// The sort buttons beside the filter box, in the sidebar tables' language:
/// one click sorts ascending, another reverses, a third restores load order.
fn sort_row(window: &SyncaWindow, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let sort = window.providers.sort;
    let mut row = div().flex_none().flex().items_center().gap_3();
    for (column, label) in [
        (ProviderSortColumn::Name, "Name"),
        (ProviderSortColumn::Status, "Status"),
        (ProviderSortColumn::Skills, "Skills"),
    ] {
        row = row.child(sort_button(column, label, sort, cx));
    }
    row
}

fn sort_button(
    column: ProviderSortColumn,
    label: &str,
    sort: ProviderSort,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let theme = theme();
    let active = sort.column == column;
    let name = label.to_lowercase();
    let tip: SharedString = if !active {
        format!("Sort by {name}").into()
    } else if sort.ascending {
        format!("Sorted by {name}, ascending. Click to reverse.").into()
    } else {
        format!("Sorted by {name}, descending. Click to restore load order.").into()
    };
    div()
        .id(SharedString::from(format!("providers-sort-{name}")))
        .flex()
        .items_center()
        .text_size(px(11.))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(if active { theme.text } else { theme.muted })
        .cursor_pointer()
        .tooltip(Tooltip::text(tip))
        .hover(move |style| style.text_color(theme.accent))
        .on_click(cx.listener(move |this, _, _, cx| this.providers_set_sort(column, cx)))
        .child(if active {
            format!("{label} {}", if sort.ascending { "↑" } else { "↓" })
        } else {
            label.to_string()
        })
}

/// The six toggle rows, built from the PROVIDERS table so the sync toggles
/// keep working before the list loads or when the CLI cannot answer.
fn managed_rows(window: &SyncaWindow, cx: &mut Context<SyncaWindow>) -> gpui::Div {
    let mut rows = div().flex().flex_col();
    for (index, (id, title)) in PROVIDERS.iter().enumerate() {
        rows = rows.child(provider_toggle_row(
            window,
            index,
            id,
            title,
            true,
            window.providers.managed(id),
            cx,
        ));
    }
    rows
}

/// The `(index, id, title)` of the managed row for `info`, when its Synca
/// agent is one of the six.
fn managed_keys(info: &ProviderInfo) -> Option<(usize, &'static str, &'static str)> {
    let agent = info.synca_agent.as_deref()?;
    PROVIDERS
        .iter()
        .enumerate()
        .find(|(_, entry)| entry.0 == agent)
        .map(|(index, entry)| (index, entry.0, entry.1))
}

/// One row with an on/off toggle. The six built-in agents save their choice
/// in the disabled list and sync for real; the rest save theirs in the
/// enabled list. Only the six get a labelled Delete data button. The CLI
/// cannot clear the others yet.
fn provider_toggle_row(
    window: &SyncaWindow,
    index: usize,
    id: &str,
    title: &str,
    managed: bool,
    info: Option<&ProviderInfo>,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let theme = theme();
    let enabled = if managed {
        !window.disabled.iter().any(|item| item == id)
    } else {
        window.enabled.iter().any(|item| item == id)
    };
    let state = if enabled { "Enabled" } else { "Disabled" };
    let provider = id.to_string();
    let name = title.to_string();
    let clear = id.to_string();
    let tip: SharedString = if managed {
        format!("{title} syncs with Synca.").into()
    } else {
        READ_ONLY_TIP.into()
    };
    let toggle_id = if managed {
        SharedString::from(format!("prov-{index}"))
    } else {
        SharedString::from(format!("prov-{id}"))
    };
    div()
        .id(SharedString::from(format!("provider-row-{id}")))
        .h(px(ROW_H))
        .w_full()
        .px_2()
        .flex()
        .flex_col()
        .justify_center()
        .gap_1()
        .tooltip(Tooltip::text(tip))
        .child(
            div()
                .w_full()
                .min_w(px(0.))
                .flex()
                .items_center()
                .gap_2()
                .child(
                    div()
                        .flex_1()
                        .min_w(px(0.))
                        .truncate()
                        .text_sm()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(if enabled { theme.text } else { theme.muted })
                        .child(title.to_string()),
                )
                .children(info.map(chips))
                .child(icon_button(
                    toggle_id,
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
                        if managed {
                            if this.disabled.iter().any(|item| item == &provider) {
                                this.disabled.retain(|item| item != &provider);
                            } else {
                                this.disabled.push(provider.clone());
                            }
                            engine::save_disabled(&this.disabled);
                        } else if this.enabled.iter().any(|item| item == &provider) {
                            this.enabled.retain(|item| item != &provider);
                            engine::save_enabled(&this.enabled);
                        } else {
                            this.enabled.push(provider.clone());
                            engine::save_enabled(&this.enabled);
                        }
                        cx.notify();
                    }),
                ))
                .when(managed, |row| {
                    row.child(
                        div()
                            .id(SharedString::from(format!("clear-{index}-tip")))
                            .flex_none()
                            .tooltip(Tooltip::text(
                                "Removes this provider’s skills and MCP entries in this scope. The hub stays.",
                            ))
                            .child(warn_button(
                                SharedString::from(format!("clear-{index}")),
                                "trash",
                                "Delete data",
                                cx.listener(move |this, _, _, cx| {
                                    if this.scope == Scope::Project && this.dir().is_none() {
                                        this.toast(
                                            "Choose a workspace folder before deleting workspace data.",
                                            cx,
                                        );
                                        return;
                                    }
                                    let id = clear.clone();
                                    this.ask_for(
                                        &format!("Delete {name} data?"),
                                        "Removes that provider’s skills and MCP entries in this scope. The canonical hub stays. This cannot be undone.",
                                        "Delete data",
                                        Pending::ClearProvider(id),
                                        cx,
                                    );
                                }),
                            )),
                    )
                }),
        )
        .children(info.map(|info| {
            div().w_full().child(path_line(
                info,
                window.providers.project,
                &format!("row-{id}"),
                false,
            ))
        }))
}

fn list_view(window: &SyncaWindow, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    let state = &window.providers;
    let theme = theme();
    if state.shown.is_empty() {
        return div()
            .w_full()
            .h(px(LIST_H))
            .overflow_hidden()
            .child(
                EmptyState::new("providers-empty", IconName::Search, "No providers match")
                    .body("Try another name or id."),
            )
            .into_any_element();
    }
    let mut list = div().flex().flex_col();
    for &at in &state.shown {
        let info = &state.list[at];
        list = list.child(match managed_keys(info) {
            Some((index, id, title)) => {
                provider_toggle_row(window, index, id, title, true, Some(info), cx)
                    .into_any_element()
            }
            None => provider_toggle_row(window, 0, &info.id, info.title(), false, Some(info), cx)
                .into_any_element(),
        });
    }
    let handle = state.scroll.clone();
    div()
        .id("providers-list")
        .w_full()
        .h(px(LIST_H))
        .overflow_y_scroll()
        .track_scroll(&state.scroll)
        .on_scroll_wheel(move |event, window, cx| crate::contain_scroll(&handle, event, window, cx))
        .rounded_md()
        .border_1()
        .border_color(theme.border)
        .child(list)
        .into_any_element()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn loaded() -> ProvidersState {
        let mut state = ProvidersState::new();
        state.list =
            engine::parse_providers(include_str!("../fixtures/providers-list.json")).unwrap();
        state.refilter();
        state
    }

    #[test]
    fn the_merged_list_includes_managed_rows() {
        let state = loaded();
        // All 8 providers in one list, the 3 managed ones among them.
        assert_eq!(state.shown.len(), 8);
        let ids: Vec<&str> = state
            .shown
            .iter()
            .map(|&at| state.list[at].id.as_str())
            .collect();
        assert!(ids.contains(&"claude-code"));
        assert!(ids.contains(&"cursor"));
        assert!(ids.contains(&"kiro-cli"));
        assert_eq!(
            state.managed("claude").map(|item| item.id.as_str()),
            Some("claude-code")
        );
        assert_eq!(
            state.managed("kiro").map(|item| item.id.as_str()),
            Some("kiro-cli")
        );
        assert!(state.managed("grok").is_none());
    }

    #[test]
    fn the_query_filters_every_row() {
        let mut state = loaded();
        state.query = "  GOOSE ".into();
        state.refilter();
        assert_eq!(state.shown.len(), 1);
        assert_eq!(state.list[state.shown[0]].id, "goose");
        // A managed row matches too.
        state.query = "CURSOR".into();
        state.refilter();
        assert_eq!(state.shown.len(), 1);
        assert_eq!(state.list[state.shown[0]].id, "cursor");
        state.query.clear();
        state.refilter();
        assert_eq!(state.shown.len(), 8);
    }

    #[test]
    fn status_prefers_managed_then_installed() {
        let state = loaded();
        let by = |id: &str| state.list.iter().find(|item| item.id == id).unwrap();
        assert_eq!(status_label(by("claude-code")).0, "managed");
        assert_eq!(status_label(by("goose")).0, "installed");
        assert_eq!(status_label(by("eve")).0, "not installed");
        assert_eq!(skills_text(by("goose")), "1 skill");
        assert_eq!(skills_text(by("claude-code")), "12 skills");
        assert_eq!(skills_text(by("eve")), "0 skills");
    }

    #[test]
    fn an_older_answer_is_recognised_by_its_sequence() {
        let mut state = ProvidersState::new();
        let first = state.seq.wrapping_add(1);
        state.seq = first;
        state.seq = state.seq.wrapping_add(1);
        assert_ne!(state.seq, first);
    }

    #[test]
    fn the_sorter_reorders_the_rows() {
        let mut state = loaded();
        // Load order keeps the managed agents first.
        assert_eq!(state.list[state.shown[0]].id, "claude-code");

        // Name ascending ignores the status groups.
        state.sort = ProviderSort {
            column: ProviderSortColumn::Name,
            ascending: true,
        };
        state.refilter();
        let titles: Vec<&str> = state
            .shown
            .iter()
            .map(|&at| state.list[at].title())
            .collect();
        assert_eq!(
            titles,
            [
                "Amp",
                "Claude Code",
                "cline",
                "Cursor",
                "Eve",
                "Goose",
                "Kiro CLI",
                "Zencoder"
            ]
        );
        // Descending reverses it.
        state.sort = ProviderSort {
            column: ProviderSortColumn::Name,
            ascending: false,
        };
        state.refilter();
        let mut titles: Vec<&str> = state
            .shown
            .iter()
            .map(|&at| state.list[at].title())
            .collect();
        titles.reverse();
        assert_eq!(
            titles,
            [
                "Amp",
                "Claude Code",
                "cline",
                "Cursor",
                "Eve",
                "Goose",
                "Kiro CLI",
                "Zencoder"
            ]
        );

        // Skills descending leads with the biggest counts; ascending leads
        // with the empty not-installed rows.
        state.sort = ProviderSort {
            column: ProviderSortColumn::Skills,
            ascending: false,
        };
        state.refilter();
        assert_eq!(state.list[state.shown[0]].skills, 12);
        assert_eq!(state.list[state.shown[3]].skills, 7);
        state.sort = ProviderSort {
            column: ProviderSortColumn::Skills,
            ascending: true,
        };
        state.refilter();
        assert_eq!(state.list[state.shown[0]].id, "amp");
        assert_eq!(state.list[state.shown[0]].skills, 0);

        // Status ascending is the load order: managed first, absent last.
        state.sort = ProviderSort {
            column: ProviderSortColumn::Status,
            ascending: true,
        };
        state.refilter();
        assert!(state.list[state.shown[0]].managed);
        let last = &state.list[*state.shown.last().unwrap()];
        assert!(!last.present && !last.managed);

        // The sort survives a filter change.
        state.sort = ProviderSort {
            column: ProviderSortColumn::Name,
            ascending: false,
        };
        state.query = "e".into();
        state.refilter();
        let filtered: Vec<&str> = state
            .shown
            .iter()
            .map(|&at| state.list[at].title())
            .collect();
        assert_eq!(
            filtered,
            ["Zencoder", "Goose", "Eve", "cline", "Claude Code"]
        );
    }

    #[test]
    fn sort_clicks_cycle_like_the_sidebar() {
        let mut sort = ProviderSort::default();
        sort = sort.next(ProviderSortColumn::Name);
        assert_eq!(
            sort,
            ProviderSort {
                column: ProviderSortColumn::Name,
                ascending: true
            }
        );
        sort = sort.next(ProviderSortColumn::Name);
        assert_eq!(
            sort,
            ProviderSort {
                column: ProviderSortColumn::Name,
                ascending: false
            }
        );
        sort = sort.next(ProviderSortColumn::Name);
        assert_eq!(sort, ProviderSort::default());
        // A different key starts ascending again.
        sort = sort.next(ProviderSortColumn::Skills);
        assert_eq!(sort.column, ProviderSortColumn::Skills);
        assert!(sort.ascending);
    }

    #[test]
    fn the_dev_channel_parses_the_sort_value() {
        assert_eq!(parse_sort("load").unwrap(), ProviderSort::default());
        assert_eq!(
            parse_sort("name").unwrap(),
            ProviderSort {
                column: ProviderSortColumn::Name,
                ascending: true
            }
        );
        assert_eq!(
            parse_sort("skills-desc").unwrap(),
            ProviderSort {
                column: ProviderSortColumn::Skills,
                ascending: false
            }
        );
        assert!(parse_sort("nope").is_err());
    }
}
