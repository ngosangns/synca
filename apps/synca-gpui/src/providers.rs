//! Settings → "All skills.sh providers": every agent the `skills` CLI can
//! install to, read from `synca providers list --json` on a background task.
//! The six agents Synca syncs to keep their toggle rows in Settings and gain
//! a status line here; the others are read-only rows. A sequence number per
//! request keeps a slow answer from replacing a newer one.

use ely_gpui_component::buttons::ButtonVariant;
use ely_gpui_component::feedback::{EmptyState, InlineMessage};
use ely_gpui_component::primitives::{IconName, Severity, Tooltip};
use gpui::{
    Context, FontWeight, InteractiveElement, IntoElement, ParentElement, ScrollHandle,
    SharedString, Styled, div, prelude::*, px,
};
use ns_ui::{Tone, chip, section_label, skeleton_list, text_field, theme, working_row};

use crate::SyncaWindow;
use crate::engine::{self, PROVIDERS, ProviderInfo, Scope};
use crate::registry::{Load, rows_window, run_parsed};

/// One provider row, and the list area that scrolls them.
const ROW_H: f32 = 54.;
const LIST_H: f32 = 324.;
const ROW_OVERSCAN: f32 = 108.;

/// Tooltip of a provider Synca does not sync to.
const READ_ONLY_TIP: &str = "Synca does not sync to this agent yet. skills.sh can install to it.";

pub(crate) struct ProvidersState {
    pub(crate) query: String,
    /// Bumped for every request and every invalidation; an answer for an
    /// older number is dropped.
    pub(crate) seq: u64,
    pub(crate) status: Load<()>,
    /// Every provider, managed first, then installed, then the rest.
    pub(crate) list: Vec<ProviderInfo>,
    /// Indexes into `list` of the read-only rows that match `query`.
    pub(crate) shown: Vec<usize>,
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

    /// Rebuilds `shown` from `list` and `query`.
    pub(crate) fn refilter(&mut self) {
        let needle = self.query.trim().to_lowercase();
        self.shown = self
            .list
            .iter()
            .enumerate()
            .filter(|(_, item)| is_listed(item) && item.matches(&needle))
            .map(|(index, _)| index)
            .collect();
    }
}

/// Whether a provider gets a row in the read-only list. A managed provider
/// with a toggle row in Settings is shown there instead.
fn is_listed(info: &ProviderInfo) -> bool {
    !(info.managed
        && info
            .synca_agent
            .as_deref()
            .is_some_and(|id| PROVIDERS.iter().any(|(known, _)| *known == id)))
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

/// The status block under a managed provider's toggle row in Settings.
/// `None` until the list has loaded or when the CLI did not list the agent.
pub(crate) fn managed_detail(window: &SyncaWindow, id: &str) -> Option<gpui::AnyElement> {
    let info = window.providers.managed(id)?;
    Some(
        div()
            .w_full()
            .min_w(px(0.))
            .flex()
            .flex_col()
            .gap_1()
            .child(chips(info))
            .child(path_line(
                info,
                window.providers.project,
                &format!("managed-{id}"),
                false,
            ))
            .into_any_element(),
    )
}

/// The "All skills.sh providers" section of Settings.
pub(crate) fn section(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    let theme = theme();
    body = body.child(section_label("All skills.sh providers"));
    let state = &window.providers;
    match &state.status {
        Load::Idle | Load::Loading => body
            .child(working_row("providers-wait", "Loading providers…"))
            .child(skeleton_list("providers-skeleton", 5)),
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
            ))),
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
                        .child("The providers above sync with Synca. The others are read-only: skills.sh can install to them."),
                )
                .child(text_field("providers-query", &window.providers_input))
                .child(
                    div()
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
            body.child(rows_view(&window.providers))
        }
    }
}

fn rows_view(state: &ProvidersState) -> gpui::AnyElement {
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
    let count = state.shown.len();
    let seen = -f32::from(state.scroll.offset().y);
    let window = rows_window(count, ROW_H, LIST_H, seen.max(0.), ROW_OVERSCAN);
    let mut list = div().flex().flex_col();
    if window.start > 0 {
        list = list.child(div().w_full().h(px(window.start as f32 * ROW_H)));
    }
    for at in window.clone() {
        list = list.child(provider_row(&state.list[state.shown[at]], state.project));
    }
    if window.end < count {
        list = list.child(div().w_full().h(px((count - window.end) as f32 * ROW_H)));
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

fn provider_row(info: &ProviderInfo, project: bool) -> impl IntoElement {
    let theme = theme();
    let tip = if info.managed {
        format!("{} syncs with Synca.", info.title())
    } else {
        READ_ONLY_TIP.to_string()
    };
    div()
        .id(SharedString::from(format!("provider-row-{}", info.id)))
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
                        .text_color(theme.text)
                        .child(SharedString::from(info.title().to_string())),
                )
                .child(chips(info)),
        )
        .child(path_line(info, project, &format!("list-{}", info.id), true))
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
    fn managed_rows_with_a_toggle_are_not_listed_twice() {
        let state = loaded();
        // 8 providers, 3 managed ones have a toggle row in Settings.
        assert_eq!(state.shown.len(), 5);
        assert!(state.shown.iter().all(|&at| !state.list[at].managed));
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
    fn the_query_filters_the_read_only_rows_only() {
        let mut state = loaded();
        state.query = "  GOOSE ".into();
        state.refilter();
        assert_eq!(state.shown.len(), 1);
        assert_eq!(state.list[state.shown[0]].id, "goose");
        // "cursor" is managed with a toggle row, so no read-only row matches.
        state.query = "cursor".into();
        state.refilter();
        assert!(state.shown.is_empty());
        state.query.clear();
        state.refilter();
        assert_eq!(state.shown.len(), 5);
    }

    #[test]
    fn a_managed_agent_without_a_toggle_row_is_listed() {
        let mut state = loaded();
        state.list.push(ProviderInfo {
            id: "newcomer".into(),
            managed: true,
            synca_agent: Some("newcomer".into()),
            ..ProviderInfo::default()
        });
        state.refilter();
        assert_eq!(state.shown.len(), 6);
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
}
