//! The MCP Registry tab of the Install dialog: search and browse the Official
//! MCP Registry, a detail area with the server's packages and remotes, and an
//! Install that prefills the MCP form and previews the plan. Every call runs
//! the `synca` CLI on a background task. A sequence number per request keeps a
//! slow answer from overwriting a newer one.

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

use crate::engine;
use crate::registry::{LIST_H, Load, ROW_H, ROW_OVERSCAN, rows_window, run_parsed};
use crate::{SyncaWindow, contain_scroll, tipped_button};

/// Quiet period after a keystroke before a search goes to the network.
const MCP_DEBOUNCE_MS: u64 = 300;
/// Page size asked from the registry.
const MCP_LIMIT: usize = 50;

/// The two MCP Registry lists.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub(crate) enum McpRegistryView {
    Search,
    Browse,
}

impl McpRegistryView {
    pub(crate) fn key(self) -> &'static str {
        match self {
            Self::Search => "search",
            Self::Browse => "browse",
        }
    }
}

pub(crate) struct McpRegistryState {
    pub(crate) view: McpRegistryView,
    pub(crate) query: String,
    /// Bumped for every list request; an answer for an older number is dropped.
    pub(crate) list_seq: u64,
    pub(crate) status: Load<()>,
    pub(crate) servers: Vec<engine::McpRegistryServer>,
    pub(crate) next_cursor: Option<String>,
    pub(crate) loading_more: bool,
    pub(crate) summary: String,
    pub(crate) selected: Option<usize>,
    /// Bumped for every selection; the info answer carries it.
    pub(crate) detail_seq: u64,
    pub(crate) info: Load<engine::McpRegistryInfo>,
    /// Bumped for every query change so only the latest debounce fires.
    pub(crate) query_job: u64,
    pub(crate) list_scroll: ScrollHandle,
}

impl McpRegistryState {
    pub(crate) fn new() -> Self {
        Self {
            view: McpRegistryView::Browse,
            query: String::new(),
            list_seq: 0,
            status: Load::Idle,
            servers: Vec::new(),
            next_cursor: None,
            loading_more: false,
            summary: String::new(),
            selected: None,
            detail_seq: 0,
            info: Load::Idle,
            query_job: 0,
            list_scroll: ScrollHandle::new(),
        }
    }

    fn selected_server(&self) -> Option<&engine::McpRegistryServer> {
        self.selected.and_then(|index| self.servers.get(index))
    }

    fn clear_list(&mut self) {
        self.servers.clear();
        self.next_cursor = None;
        self.loading_more = false;
        self.summary.clear();
        self.clear_detail();
        self.list_scroll.set_offset(gpui::point(px(0.), px(0.)));
    }

    fn clear_detail(&mut self) {
        self.selected = None;
        self.detail_seq = self.detail_seq.wrapping_add(1);
        self.info = Load::Idle;
    }
}

impl SyncaWindow {
    /// The MCP Registry tab came to the front: load the first list once.
    pub(crate) fn mcp_registry_activate(&mut self, cx: &mut Context<Self>) {
        if matches!(self.mcp_registry.status, Load::Idle) {
            self.mcp_registry_load(false, cx);
        }
        cx.notify();
    }

    pub(crate) fn mcp_registry_set_view(&mut self, view: McpRegistryView, cx: &mut Context<Self>) {
        if self.mcp_registry.view == view && matches!(self.mcp_registry.status, Load::Ready(())) {
            return;
        }
        self.mcp_registry.view = view;
        self.mcp_registry.query_job = self.mcp_registry.query_job.wrapping_add(1);
        self.mcp_registry.list_seq = self.mcp_registry.list_seq.wrapping_add(1);
        self.mcp_registry.status = Load::Idle;
        self.mcp_registry.clear_list();
        if view == McpRegistryView::Browse || !self.mcp_registry.query.trim().is_empty() {
            self.mcp_registry_load(false, cx);
        }
        cx.notify();
    }

    /// The search text changed; a quiet period follows so typing does not
    /// hammer the registry.
    pub(crate) fn mcp_registry_query_changed(&mut self, cx: &mut Context<Self>) {
        self.mcp_registry.query_job = self.mcp_registry.query_job.wrapping_add(1);
        let job = self.mcp_registry.query_job;
        if self.mcp_registry.query.trim().is_empty() {
            cx.notify();
            return;
        }
        cx.spawn(async move |this, cx| {
            cx.background_executor()
                .timer(Duration::from_millis(MCP_DEBOUNCE_MS))
                .await;
            this.update(cx, |this, cx| {
                if this.mcp_registry.query_job == job {
                    this.mcp_registry_search_now(cx);
                }
            })
            .ok();
        })
        .detach();
        cx.notify();
    }

    /// Enter in the search box, or the end of the debounce.
    pub(crate) fn mcp_registry_search_now(&mut self, cx: &mut Context<Self>) {
        self.mcp_registry.query_job = self.mcp_registry.query_job.wrapping_add(1);
        if self.mcp_registry.query.trim().is_empty() {
            return;
        }
        self.mcp_registry.view = McpRegistryView::Search;
        self.mcp_registry.clear_list();
        self.mcp_registry_load(false, cx);
    }

    /// Asks the CLI for the current view. `append` loads the next page by cursor.
    pub(crate) fn mcp_registry_load(&mut self, append: bool, cx: &mut Context<Self>) {
        let view = self.mcp_registry.view;
        let query = self.mcp_registry.query.trim().to_string();
        let args = match view {
            McpRegistryView::Search => engine::mcp_registry_search_args(&query, MCP_LIMIT),
            McpRegistryView::Browse => Ok(engine::mcp_registry_browse_args(MCP_LIMIT)),
        };
        let mut args = match args {
            Ok(args) => args,
            Err(message) => {
                self.mcp_registry.status = Load::Failed(message);
                cx.notify();
                return;
            }
        };
        if append && let Some(cursor) = self.mcp_registry.next_cursor.clone() {
            args.push("--cursor".into());
            args.push(cursor);
        }
        self.mcp_registry.list_seq = self.mcp_registry.list_seq.wrapping_add(1);
        let seq = self.mcp_registry.list_seq;
        if append {
            self.mcp_registry.loading_more = true;
        } else {
            self.mcp_registry.status = Load::Loading;
        }
        cx.notify();
        cx.spawn(async move |this, cx| {
            let page = cx
                .background_spawn(async move { run_parsed(args, engine::parse_mcp_registry_list) })
                .await;
            this.update(cx, |this, cx| {
                if this.mcp_registry.list_seq != seq {
                    return;
                }
                this.mcp_registry_apply(page, append);
                cx.notify();
            })
            .ok();
        })
        .detach();
    }

    fn mcp_registry_apply(
        &mut self,
        fetched: Result<engine::McpRegistryPage, String>,
        append: bool,
    ) {
        let reg = &mut self.mcp_registry;
        reg.loading_more = false;
        match fetched {
            Err(message) => {
                if append {
                    reg.has_more_stop();
                }
                reg.status = Load::Failed(message);
            }
            Ok(page) => {
                if !append {
                    reg.servers.clear();
                }
                let count = page.servers.len();
                reg.servers.extend(page.servers);
                reg.next_cursor = page.next_cursor;
                reg.summary = match reg.view {
                    McpRegistryView::Search => match reg.servers.len() {
                        1 => "1 result".to_string(),
                        count => format!("{count} results"),
                    },
                    McpRegistryView::Browse => format!("{} newest servers", reg.servers.len()),
                };
                reg.status = Load::Ready(());
                let _ = count;
            }
        }
    }

    /// Selects a result and asks for its packages and remotes.
    pub(crate) fn mcp_registry_select(&mut self, index: usize, cx: &mut Context<Self>) {
        let Some(server) = self.mcp_registry.servers.get(index) else {
            return;
        };
        let name = server.name.clone();
        self.mcp_registry.selected = Some(index);
        self.mcp_registry.detail_seq = self.mcp_registry.detail_seq.wrapping_add(1);
        let seq = self.mcp_registry.detail_seq;
        match engine::mcp_registry_info_args(&name) {
            Ok(args) => {
                self.mcp_registry.info = Load::Loading;
                cx.spawn(async move |this, cx| {
                    let info = cx
                        .background_spawn(async move {
                            run_parsed(args, engine::parse_mcp_registry_info)
                        })
                        .await;
                    this.update(cx, |this, cx| {
                        if this.mcp_registry.detail_seq != seq {
                            return;
                        }
                        this.mcp_registry.info = match info {
                            Ok(info) => Load::Ready(info),
                            Err(message) => Load::Failed(message),
                        };
                        cx.notify();
                    })
                    .ok();
                })
                .detach();
            }
            Err(message) => self.mcp_registry.info = Load::Failed(message),
        }
        cx.notify();
    }

    /// Prefills the MCP form from the selected server, then previews the plan.
    /// Values the registry marks secret are left empty for the user to fill.
    pub(crate) fn mcp_registry_install(&mut self, cx: &mut Context<Self>) {
        let info = match &self.mcp_registry.info {
            Load::Ready(info) => info.clone(),
            _ => return,
        };
        self.reset_mcp_form(cx);
        let key = info
            .name
            .rsplit('/')
            .next()
            .unwrap_or(&info.name)
            .to_string();
        let (transport, url, command) = if let Some(remote) = info.remotes.first() {
            let transport = if remote.remote_type == "sse" {
                "sse".to_string()
            } else {
                "http".to_string()
            };
            (transport, remote.url.clone(), String::new())
        } else if let Some(package) = info.packages.first() {
            let runner = match package.registry_type.as_str() {
                "npm" => format!("npx -y {}", package.identifier),
                "pypi" => format!("uvx {}", package.identifier),
                "oci" => format!("docker run -i --rm {}", package.identifier),
                other => {
                    ns_ui::notify(
                        &self.toaster,
                        Severity::Warning,
                        &format!(
                            "Package type \"{other}\" has no local runner; fill the command yourself."
                        ),
                        cx,
                    );
                    package.identifier.clone()
                }
            };
            ("stdio".to_string(), String::new(), runner)
        } else {
            ("stdio".to_string(), String::new(), String::new())
        };
        let env = info
            .packages
            .first()
            .map(|package| {
                package
                    .env_keys
                    .iter()
                    .map(|key| format!("{key}="))
                    .collect::<Vec<_>>()
                    .join("\n")
            })
            .unwrap_or_default();
        let headers = info
            .remotes
            .first()
            .map(|remote| {
                remote
                    .header_names
                    .iter()
                    .map(|name| format!("{name}="))
                    .collect::<Vec<_>>()
                    .join("\n")
            })
            .unwrap_or_default();
        Self::set_mcp_field(&mut self.mcp_name, &self.name_input, Some(&key), cx);
        Self::set_mcp_field(
            &mut self.mcp_command,
            &self.command_input,
            Some(&command),
            cx,
        );
        Self::set_mcp_field(&mut self.mcp_url, &self.url_input, Some(&url), cx);
        Self::set_mcp_field(&mut self.mcp_env, &self.mcp_env_input, Some(&env), cx);
        Self::set_mcp_field(
            &mut self.mcp_headers,
            &self.mcp_headers_input,
            Some(&headers),
            cx,
        );
        self.mcp_transport = transport.clone();
        self.mcp_transport_kind = match transport.as_str() {
            "http" => crate::McpTransportKind::Http,
            "sse" => crate::McpTransportKind::Sse,
            _ => crate::McpTransportKind::Stdio,
        };
        self.onboarding = Some(crate::InstallTab::Mcp);
        self.preview_mcp(cx);
    }

    /// Opens the server's website. Only an https link is handed to the system.
    pub(crate) fn mcp_registry_open_page(&mut self, cx: &mut Context<Self>) {
        let Some(url) = self
            .mcp_registry
            .info
            .as_ready()
            .map(|info| info.website.clone())
            .filter(|url| !url.is_empty())
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

impl Load<engine::McpRegistryInfo> {
    fn as_ready(&self) -> Option<&engine::McpRegistryInfo> {
        match self {
            Load::Ready(info) => Some(info),
            _ => None,
        }
    }
}

impl McpRegistryState {
    /// A failed page load leaves nothing more to append.
    fn has_more_stop(&mut self) {
        self.next_cursor = None;
    }
}

/// The JSON the `state` dev command reports under `mcpRegistry`.
#[cfg(feature = "devtools")]
pub(crate) fn state_value(reg: &McpRegistryState, open: bool) -> serde_json::Value {
    let error = match &reg.status {
        Load::Failed(message) => Some(message.as_str()),
        _ => None,
    };
    serde_json::json!({
        "open": open,
        "view": reg.view.key(),
        "query": reg.query,
        "status": reg.status.label(),
        "error": error,
        "count": reg.servers.len(),
        "summary": reg.summary,
        "selected": reg.selected_server().map(|server| server.name.as_str()),
        "detail": {
            "info": reg.info.label(),
            "packages": reg.info.as_ready().map(|info| info.packages.len()).unwrap_or(0),
            "remotes": reg.info.as_ready().map(|info| info.remotes.len()).unwrap_or(0),
        },
        "has_more": reg.next_cursor.is_some(),
        "loading_more": reg.loading_more,
        "seq": reg.list_seq,
    })
}

/// Button row, list, and detail of the MCP Registry tab.
pub(crate) fn body(
    window: &mut SyncaWindow,
    mut body: gpui::Div,
    cx: &mut Context<SyncaWindow>,
) -> gpui::Div {
    body = body
        .child(text_field("mcp-registry-query", &window.mcp_registry_input))
        .child(view_buttons(window.mcp_registry.view, cx))
        .child(list_section(&window.mcp_registry, cx));
    if let Some(detail) = detail_section(window, cx) {
        body = body.child(detail);
    }
    body
}

fn view_buttons(current: McpRegistryView, cx: &mut Context<SyncaWindow>) -> impl IntoElement {
    let mut row = div().flex().flex_wrap().gap_1();
    for (view, label, tip) in [
        (
            McpRegistryView::Search,
            "Search",
            "Search the Official MCP Registry by name",
        ),
        (
            McpRegistryView::Browse,
            "Browse",
            "The registry's newest servers",
        ),
    ] {
        row = row.child(tipped_button(
            format!("mcp-registry-view-{}", view.key()),
            None,
            label,
            tip,
            if view == current {
                ButtonVariant::Primary
            } else {
                ButtonVariant::Secondary
            },
            false,
            cx.listener(move |this, _, _, cx| this.mcp_registry_set_view(view, cx)),
        ));
    }
    row
}

fn list_frame(child: impl IntoElement) -> gpui::AnyElement {
    div()
        .w_full()
        .h(px(LIST_H))
        .overflow_hidden()
        .child(child)
        .into_any_element()
}

fn list_section(reg: &McpRegistryState, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    match &reg.status {
        Load::Loading => list_frame(
            div()
                .flex()
                .flex_col()
                .child(working_row(
                    "mcp-registry-wait",
                    "Searching the MCP registry…",
                ))
                .child(skeleton_list("mcp-registry-skeleton", 5)),
        ),
        Load::Idle if reg.view == McpRegistryView::Search => list_frame(
            EmptyState::new("mcp-registry-idle", IconName::Search, "Search MCP servers")
                .body("Type a name, or switch to Browse for the newest servers."),
        ),
        Load::Idle => list_frame(loading_pane("mcp-registry-idle-wait", "Loading…")),
        Load::Failed(message) => list_frame(failure_view(message, cx)),
        Load::Ready(()) if reg.servers.is_empty() => list_frame(
            EmptyState::new("mcp-registry-empty", IconName::Inbox, "No servers found")
                .body("Try another word, or switch to Browse."),
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
        .child(tipped_button(
            "mcp-registry-retry",
            Some("refresh"),
            "Try again",
            "Ask the MCP registry again",
            ButtonVariant::Secondary,
            false,
            cx.listener(|this, _, _, cx| {
                this.mcp_registry.list_seq = this.mcp_registry.list_seq.wrapping_add(1);
                this.mcp_registry.status = Load::Idle;
                this.mcp_registry_activate(cx);
            }),
        ))
        .into_any_element()
}

fn rows_view(reg: &McpRegistryState, cx: &mut Context<SyncaWindow>) -> gpui::AnyElement {
    let theme = theme();
    let count = reg.servers.len();
    let seen = -f32::from(reg.list_scroll.offset().y);
    let window = rows_window(count, ROW_H, LIST_H, seen.max(0.), ROW_OVERSCAN);
    let mut list = div().flex().flex_col();
    if window.start > 0 {
        list = list.child(div().w_full().h(px(window.start as f32 * ROW_H)));
    }
    for index in window.clone() {
        let server = &reg.servers[index];
        list = list.child(server_row(index, server, reg.selected == Some(index), cx));
    }
    if window.end < count {
        list = list.child(div().w_full().h(px((count - window.end) as f32 * ROW_H)));
    }
    if reg.next_cursor.is_some() {
        list = list.child(
            div()
                .w_full()
                .py_2()
                .flex()
                .justify_center()
                .child(tipped_button(
                    "mcp-registry-more",
                    Some("download"),
                    if reg.loading_more {
                        "Loading…"
                    } else {
                        "Load more"
                    },
                    "Load the next page from the registry",
                    ButtonVariant::Secondary,
                    reg.loading_more,
                    cx.listener(|this, _, _, cx| this.mcp_registry_load(true, cx)),
                )),
        );
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
                .id("mcp-registry-list")
                .w_full()
                .h(px(LIST_H))
                .overflow_y_scroll()
                .track_scroll(&reg.list_scroll)
                .on_scroll_wheel(move |event, window, cx| {
                    contain_scroll(&handle, event, window, cx)
                })
                .rounded_md()
                .border_1()
                .border_color(theme.border)
                .child(list),
        )
        .into_any_element()
}

fn server_row(
    index: usize,
    server: &engine::McpRegistryServer,
    selected: bool,
    cx: &mut Context<SyncaWindow>,
) -> impl IntoElement {
    let theme = theme();
    let tip = format!(
        "{} · {}. Click to see packages, remotes and actions.",
        server.name,
        if server.version.is_empty() {
            "unknown version".to_string()
        } else {
            format!("v{}", server.version)
        },
    );
    div()
        .id(SharedString::from(format!("mcp-registry-row-{index}")))
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
        .on_click(cx.listener(move |this, _, _, cx| this.mcp_registry_select(index, cx)))
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
                        .child(SharedString::from(if server.title.is_empty() {
                            server.name.clone()
                        } else {
                            server.title.clone()
                        })),
                )
                .child(
                    div()
                        .truncate()
                        .text_xs()
                        .text_color(theme.muted)
                        .child(SharedString::from(server.name.clone())),
                ),
        )
        .when(!server.version.is_empty(), |row| {
            row.child(chip(
                SharedString::from(format!("v{}", server.version)),
                Tone::Neutral,
            ))
        })
}

fn detail_section(
    window: &mut SyncaWindow,
    cx: &mut Context<SyncaWindow>,
) -> Option<gpui::AnyElement> {
    let theme = theme();
    let info = match &window.mcp_registry.info {
        Load::Ready(info) => info.clone(),
        Load::Loading => {
            return Some(
                div()
                    .w_full()
                    .pt_2()
                    .border_t_1()
                    .border_color(theme.border)
                    .child(working_row("mcp-registry-info-wait", "Reading the server…"))
                    .into_any_element(),
            );
        }
        Load::Failed(message) => {
            return Some(
                div()
                    .w_full()
                    .pt_2()
                    .border_t_1()
                    .border_color(theme.border)
                    .child(InlineMessage::new(
                        Severity::Info,
                        format!("No server detail: {message}"),
                    ))
                    .into_any_element(),
            );
        }
        Load::Idle => return None,
    };
    let reg = &window.mcp_registry;
    let selected = reg.selected_server()?;
    let mut meta = vec![selected.name.clone()];
    if !info.version.is_empty() {
        meta.push(format!("v{}", info.version));
    }
    if !info.status.is_empty() {
        meta.push(info.status.clone());
    }
    let install_tip = if info.packages.is_empty() && info.remotes.is_empty() {
        "The registry lists no package or remote for this server".to_string()
    } else {
        "Fill the MCP form from this server, then preview the plan".to_string()
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
            div().flex().items_center().gap_2().flex_wrap().child(
                div()
                    .min_w(px(0.))
                    .text_lg()
                    .font_weight(FontWeight::SEMIBOLD)
                    .child(SharedString::from(if info.title.is_empty() {
                        info.name.clone()
                    } else {
                        info.title.clone()
                    })),
            ),
        )
        .child(
            div()
                .text_xs()
                .text_color(theme.muted)
                .child(SharedString::from(meta.join(" · "))),
        )
        .when(!info.description.is_empty(), |panel| {
            panel.child(
                div()
                    .text_sm()
                    .child(SharedString::from(info.description.clone())),
            )
        })
        .child(
            div()
                .flex()
                .flex_wrap()
                .gap_2()
                .child(tipped_button(
                    "mcp-registry-install",
                    Some("download"),
                    "Install…",
                    install_tip,
                    ButtonVariant::Primary,
                    info.packages.is_empty() && info.remotes.is_empty(),
                    cx.listener(|this, _, _, cx| this.mcp_registry_install(cx)),
                ))
                .children((!info.website.is_empty()).then(|| {
                    tipped_button(
                        "mcp-registry-open",
                        Some("globe"),
                        "Open website",
                        info.website.clone(),
                        ButtonVariant::Secondary,
                        false,
                        cx.listener(|this, _, _, cx| this.mcp_registry_open_page(cx)),
                    )
                })),
        );
    if !info.packages.is_empty() {
        let mut packages = div().flex().flex_col().gap_1();
        for package in &info.packages {
            let mut line = format!("{} {}", package.registry_type, package.identifier);
            if !package.version.is_empty() {
                line.push_str(&format!(" @ {}", package.version));
            }
            if !package.env_keys.is_empty() {
                line.push_str(&format!(" · env: {}", package.env_keys.join(", ")));
            }
            packages = packages.child(
                div()
                    .text_xs()
                    .text_color(theme.muted)
                    .child(SharedString::from(line)),
            );
        }
        panel = panel.child(section_label("Packages")).child(packages);
    }
    if !info.remotes.is_empty() {
        let mut remotes = div().flex().flex_col().gap_1();
        for remote in &info.remotes {
            let mut line = format!("{} {}", remote.remote_type, remote.url);
            if !remote.header_names.is_empty() {
                line.push_str(&format!(" · headers: {}", remote.header_names.join(", ")));
            }
            if !remote.variables.is_empty() {
                line.push_str(&format!(" · variables: {}", remote.variables.join(", ")));
            }
            remotes = remotes.child(
                div()
                    .text_xs()
                    .text_color(theme.muted)
                    .child(SharedString::from(line)),
            );
        }
        panel = panel.child(section_label("Remotes")).child(remotes);
    }
    Some(panel.into_any_element())
}

/// Dev commands set the search box without waiting for a keystroke.
#[cfg(feature = "devtools")]
pub(crate) fn dev_open(
    window: &mut SyncaWindow,
    view: Option<McpRegistryView>,
    query: Option<&str>,
    select: Option<usize>,
    more: bool,
    cx: &mut Context<SyncaWindow>,
) {
    window.onboarding = Some(crate::InstallTab::McpRegistry);
    if let Some(query) = query {
        window.mcp_registry.query = query.to_string();
        let input: gpui::Entity<TextInput> = window.mcp_registry_input.clone();
        input.update(cx, |input, cx| input.set_text(query.to_string(), cx));
    }
    match (view, query) {
        (Some(view), _) => {
            window.mcp_registry.status = Load::Idle;
            window.mcp_registry_set_view(view, cx);
        }
        (None, Some(query)) if !query.trim().is_empty() => {
            window.mcp_registry_search_now(cx);
        }
        _ => window.mcp_registry_activate(cx),
    }
    if let Some(index) = select {
        window.mcp_registry_select(index, cx);
    }
    if more && window.mcp_registry.next_cursor.is_some() {
        window.mcp_registry_load(true, cx);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_older_answer_is_recognised_by_its_sequence() {
        let mut state = McpRegistryState::new();
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

    #[test]
    fn a_failed_page_stops_the_cursor() {
        let mut state = McpRegistryState::new();
        state.next_cursor = Some("abc".into());
        state.has_more_stop();
        assert!(state.next_cursor.is_none());
    }
}
