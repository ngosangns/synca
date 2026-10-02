use crate::state::{AppState, Page, Section};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Clear, List, ListItem, Paragraph, Wrap};
use ratatui::Frame;
use synca_core::models::AgentKind;

pub fn draw(frame: &mut Frame, state: &mut AppState) {
    let area = frame.area();
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(1),
            Constraint::Min(5),
            // Status + nav hotkeys + sync hotkeys (never wiped by status).
            Constraint::Length(3),
        ])
        .split(area);

    draw_header(frame, chunks[0], state);
    draw_body(frame, chunks[1], state);
    draw_footer(frame, chunks[2], state);

    if matches!(
        state.pending,
        Some(crate::state::Pending::ResolveConflict { .. })
    ) {
        draw_conflict_overlay(frame, area, state);
    }

    if state.help {
        draw_help(frame, area);
    }
}

fn draw_header(frame: &mut Frame, area: Rect, state: &mut AppState) {
    state.layout.header = area;
    // " synca " (7) + " User " (6) + " " (1) + " Project " (9)
    state.layout.user_tab = Rect::new(area.x.saturating_add(7), area.y, 6, area.height.max(1));
    state.layout.project_tab = Rect::new(area.x.saturating_add(14), area.y, 9, area.height.max(1));

    let user_style = if state.page == Page::User {
        Style::default()
            .fg(Color::Black)
            .bg(Color::Cyan)
            .add_modifier(Modifier::BOLD)
    } else {
        Style::default().fg(Color::Gray)
    };
    let proj_style = if state.page == Page::Project {
        Style::default()
            .fg(Color::Black)
            .bg(Color::Cyan)
            .add_modifier(Modifier::BOLD)
    } else {
        Style::default().fg(Color::Gray)
    };
    let root = state
        .project_inv
        .project_root
        .as_ref()
        .map(|p| p.display().to_string())
        .unwrap_or_else(|| "(not a git repo)".into());
    let line = Line::from(vec![
        Span::styled(" synca ", Style::default().fg(Color::Yellow)),
        Span::styled(" User ", user_style),
        Span::raw(" "),
        Span::styled(" Project ", proj_style),
        Span::raw(format!("  ·  cwd project: {root}")),
    ]);
    frame.render_widget(Paragraph::new(line), area);
}

fn draw_body(frame: &mut Frame, area: Rect, state: &mut AppState) {
    let cols = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Percentage(45), Constraint::Percentage(55)])
        .split(area);

    let lists = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
        .split(cols[0]);

    state.layout.skills = lists[0];
    state.layout.mcps = lists[1];
    state.layout.detail = cols[1];

    draw_skills_list(frame, lists[0], state);
    draw_mcps_list(frame, lists[1], state);
    draw_detail(frame, cols[1], state);
}

fn draw_skills_list(frame: &mut Frame, area: Rect, state: &mut AppState) {
    let focused = state.section == Section::Skills;
    let title = if focused { " Skills * " } else { " Skills " };
    let border = if focused { Color::Cyan } else { Color::DarkGray };
    let items: Vec<ListItem> = state
        .skills()
        .iter()
        .map(|s| {
            let agents: Vec<_> = s.presence.iter().map(|p| p.agent.as_str()).collect();
            let mark = if s.mismatch {
                Span::styled("!", Style::default().fg(Color::Red).add_modifier(Modifier::BOLD))
            } else {
                Span::raw(" ")
            };
            ListItem::new(Line::from(vec![
                mark,
                Span::raw(" "),
                Span::styled(
                    s.display_name.clone(),
                    Style::default()
                        .fg(Color::Cyan)
                        .add_modifier(Modifier::BOLD),
                ),
                Span::styled(
                    format!("  ({}) [{}]", s.presence.len(), agents.join(",")),
                    Style::default().fg(Color::DarkGray),
                ),
            ]))
        })
        .collect();
    // Keep ListState selection synced before stateful render so ratatui scrolls
    // the selected row into the viewport (j/k past the visible height).
    if state.skills().is_empty() {
        state.skill_list_state.select(None);
    } else {
        state.skill_list_state.select(Some(state.skill_idx));
    }
    let mut list = List::new(items).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(border))
            .title(title),
    );
    if focused {
        list = list
            .highlight_style(
                Style::default()
                    .bg(Color::Blue)
                    .fg(Color::White)
                    .add_modifier(Modifier::BOLD),
            )
            .highlight_symbol("› ");
    } else if !state.skills().is_empty() {
        // Dim marker on the other list so scroll position / selection stay obvious.
        list = list.highlight_style(Style::default().fg(Color::DarkGray));
    }
    frame.render_stateful_widget(list, area, &mut state.skill_list_state);
}

fn draw_mcps_list(frame: &mut Frame, area: Rect, state: &mut AppState) {
    let focused = state.section == Section::Mcps;
    let title = if focused { " MCPs * " } else { " MCPs " };
    let border = if focused { Color::Cyan } else { Color::DarkGray };
    let items: Vec<ListItem> = state
        .mcps()
        .iter()
        .map(|m| {
            let agents: Vec<_> = m.presence.iter().map(|p| p.agent.as_str()).collect();
            let mark = if m.mismatch {
                Span::styled("!", Style::default().fg(Color::Red).add_modifier(Modifier::BOLD))
            } else {
                Span::raw(" ")
            };
            ListItem::new(Line::from(vec![
                mark,
                Span::raw(" "),
                Span::styled(
                    m.key.clone(),
                    Style::default()
                        .fg(Color::Magenta)
                        .add_modifier(Modifier::BOLD),
                ),
                Span::styled(
                    format!("  ({}) [{}]", m.presence.len(), agents.join(",")),
                    Style::default().fg(Color::DarkGray),
                ),
            ]))
        })
        .collect();
    if state.mcps().is_empty() {
        state.mcp_list_state.select(None);
    } else {
        state.mcp_list_state.select(Some(state.mcp_idx));
    }
    let mut list = List::new(items).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(border))
            .title(title),
    );
    if focused {
        list = list
            .highlight_style(
                Style::default()
                    .bg(Color::Blue)
                    .fg(Color::White)
                    .add_modifier(Modifier::BOLD),
            )
            .highlight_symbol("› ");
    } else if !state.mcps().is_empty() {
        list = list.highlight_style(Style::default().fg(Color::DarkGray));
    }
    frame.render_stateful_widget(list, area, &mut state.mcp_list_state);
}

fn preferred_mcp_presence(
    presence: &[synca_core::models::McpPresence],
) -> Option<&synca_core::models::McpPresence> {
    presence
        .iter()
        .find(|p| p.agent == AgentKind::Agents)
        .or_else(|| presence.first())
}

fn draw_detail(frame: &mut Frame, area: Rect, state: &AppState) {
    let mut lines: Vec<Line> = Vec::new();
    match state.section {
        Section::Skills => {
            if let Some(s) = state.skills().get(state.skill_idx) {
                lines.push(Line::from(vec![
                    Span::styled("Skill: ", Style::default().fg(Color::Yellow)),
                    Span::styled(
                        s.display_name.clone(),
                        Style::default()
                            .fg(Color::Cyan)
                            .add_modifier(Modifier::BOLD),
                    ),
                ]));
                lines.push(Line::from(Span::styled(
                    "Description:",
                    Style::default().add_modifier(Modifier::BOLD),
                )));
                match s.description.as_deref().map(str::trim).filter(|d| !d.is_empty()) {
                    Some(desc) => {
                        lines.push(Line::from(Span::styled(
                            desc.to_string(),
                            Style::default().fg(Color::White),
                        )));
                    }
                    None => {
                        lines.push(Line::from(Span::styled(
                            "(no description in SKILL.md frontmatter)",
                            Style::default().fg(Color::DarkGray),
                        )));
                    }
                }
                lines.push(Line::from(format!("key: {}", s.key)));
                lines.push(Line::from(vec![
                    Span::raw("mismatch: "),
                    if s.mismatch {
                        Span::styled("YES", Style::default().fg(Color::Red).add_modifier(Modifier::BOLD))
                    } else {
                        Span::styled("no", Style::default().fg(Color::Green))
                    },
                ]));
                crate::diffview::push_skill_mismatch(&mut lines, s);
                lines.push(Line::from(""));
                lines.push(Line::from(Span::styled(
                    "Agents:",
                    Style::default().add_modifier(Modifier::BOLD),
                )));
                for p in &s.presence {
                    let link = if p.is_symlink {
                        format!(
                            " → {}",
                            p.symlink_target
                                .as_ref()
                                .map(|t| t.display().to_string())
                                .unwrap_or_default()
                        )
                    } else {
                        String::new()
                    };
                    lines.push(Line::from(vec![
                        Span::raw("  · "),
                        Span::styled(
                            p.agent.as_str().to_string(),
                            Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD),
                        ),
                        Span::raw(format!("  {}{}", p.path.display(), link)),
                    ]));
                    lines.push(Line::from(format!(
                        "      hash: {}",
                        crate::diffview::short_hash(&p.content_hash)
                    )));
                }
            } else {
                lines.push(Line::from("No skills in this scope."));
                if state.page == Page::Project && !state.project_ok {
                    lines.push(Line::from(
                        "Current directory is not inside a git repository.",
                    ));
                }
            }
        }
        Section::Mcps => {
            if let Some(m) = state.mcps().get(state.mcp_idx) {
                lines.push(Line::from(vec![
                    Span::styled("MCP: ", Style::default().fg(Color::Yellow)),
                    Span::styled(
                        m.key.clone(),
                        Style::default()
                            .fg(Color::Magenta)
                            .add_modifier(Modifier::BOLD),
                    ),
                ]));
                if let Some(p) = preferred_mcp_presence(&m.presence) {
                    lines.push(Line::from(Span::styled(
                        "Summary:",
                        Style::default().add_modifier(Modifier::BOLD),
                    )));
                    lines.push(Line::from(format!(
                        "  transport: {}",
                        p.normalized.transport
                    )));
                    if let Some(ref cmd) = p.normalized.command {
                        let mut parts = cmd.clone();
                        if let Some(ref args) = p.normalized.args {
                            parts.extend(args.iter().cloned());
                        }
                        lines.push(Line::from(format!("  command: {}", parts.join(" "))));
                    }
                    if let Some(ref url) = p.normalized.url {
                        lines.push(Line::from(format!("  url: {url}")));
                    }
                    if let Some(en) = p.normalized.enabled {
                        lines.push(Line::from(format!("  enabled: {en}")));
                    }
                }
                lines.push(Line::from(vec![
                    Span::raw("mismatch: "),
                    if m.mismatch {
                        Span::styled("YES", Style::default().fg(Color::Red).add_modifier(Modifier::BOLD))
                    } else {
                        Span::styled("no", Style::default().fg(Color::Green))
                    },
                ]));
                crate::diffview::push_mcp_mismatch(&mut lines, m);
                lines.push(Line::from(""));
                lines.push(Line::from(Span::styled(
                    "Agents:",
                    Style::default().add_modifier(Modifier::BOLD),
                )));
                for p in &m.presence {
                    lines.push(Line::from(vec![
                        Span::raw("  · "),
                        Span::styled(
                            p.agent.as_str().to_string(),
                            Style::default().fg(Color::Magenta).add_modifier(Modifier::BOLD),
                        ),
                        Span::raw(format!("  {}", p.path.display())),
                    ]));
                    lines.push(Line::from(format!(
                        "      transport={}  fp={}",
                        p.normalized.transport,
                        crate::diffview::short_hash(&p.fingerprint)
                    )));
                    if let Some(ref cmd) = p.normalized.command {
                        lines.push(Line::from(format!("      command: {}", cmd.join(" "))));
                    }
                    if let Some(ref url) = p.normalized.url {
                        lines.push(Line::from(format!("      url: {url}")));
                    }
                    if !p.normalized.env_keys.is_empty() {
                        lines.push(Line::from(format!(
                            "      env keys: {}",
                            p.normalized.env_keys.join(", ")
                        )));
                    }
                }
            } else {
                lines.push(Line::from("No MCP servers in this scope."));
            }
        }
    }

    if let Some(ref plan) = state.last_plan {
        lines.push(Line::from(""));
        lines.push(Line::from(Span::styled(
            format!("Plan ({} actions):", plan.actions.len()),
            Style::default().fg(Color::Green),
        )));
        for (i, a) in plan.actions.iter().take(12).enumerate() {
            let s = serde_json::to_string(a).unwrap_or_default();
            let short = if s.len() > 90 { format!("{}…", &s[..90]) } else { s };
            lines.push(Line::from(format!("  {}. {short}", i + 1)));
        }
        if plan.actions.len() > 12 {
            lines.push(Line::from(format!("  … +{} more", plan.actions.len() - 12)));
        }
    }

    let para = Paragraph::new(lines)
        .wrap(Wrap { trim: false })
        .scroll((state.detail_scroll, 0))
        .block(
            Block::default()
                .borders(Borders::ALL)
                .title(" Detail ")
                .border_style(Style::default().fg(Color::DarkGray)),
        );
    frame.render_widget(para, area);
}

fn key_chip(label: &'static str) -> Span<'static> {
    Span::styled(label, Style::default().fg(Color::Black).bg(Color::DarkGray))
}

fn pending_chip(label: &'static str) -> Span<'static> {
    Span::styled(label, Style::default().fg(Color::Black).bg(Color::Yellow))
}

fn nav_hotkey_spans() -> Vec<Span<'static>> {
    vec![
        key_chip(" Tab "),
        Span::raw("pages "),
        key_chip(" [/]/Space "),
        Span::raw("sections "),
        key_chip(" j/k "),
        Span::raw("nav "),
        key_chip(" click/wheel "),
        Span::raw("mouse "),
        key_chip(" u "),
        Span::raw("update "),
        key_chip(" r "),
        Span::raw("reload "),
        key_chip(" ? "),
        Span::raw("help "),
        key_chip(" q "),
        Span::raw("quit"),
    ]
}

fn sync_hotkey_spans(state: &AppState) -> Vec<Span<'static>> {
    let mut spans = vec![
        key_chip(" s "),
        Span::raw("focused "),
        key_chip(" S "),
        Span::raw("skills-all "),
        key_chip(" M "),
        Span::raw("mcp-all "),
        key_chip(" A "),
        Span::raw("all "),
        Span::raw("(this page only) "),
    ];
    match &state.pending {
        Some(crate::state::Pending::SyncConfirm) | Some(crate::state::Pending::UpdateInstall) => {
            spans.push(Span::raw(" · "));
            spans.push(pending_chip(" y "));
            spans.push(Span::raw("confirm "));
            spans.push(pending_chip(" n "));
            spans.push(Span::raw("cancel"));
        }
        Some(crate::state::Pending::ResolveConflict { .. }) => {
            spans.push(Span::raw(" · "));
            spans.push(pending_chip(" a "));
            spans.push(Span::raw("keep-src "));
            spans.push(pending_chip(" b "));
            spans.push(Span::raw("keep-tgt "));
            spans.push(pending_chip(" s "));
            spans.push(Span::raw("skip"));
        }
        None => {}
    }
    spans
}

fn draw_footer(frame: &mut Frame, area: Rect, state: &AppState) {
    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(1),
            Constraint::Length(1),
            Constraint::Length(1),
        ])
        .split(area);

    // Line 1: transient status / confirm messages (may be empty).
    let status_style = if state.pending.is_some() {
        Style::default().fg(Color::Black).bg(Color::Yellow)
    } else {
        Style::default().fg(Color::Gray)
    };
    let status_text = if state.status.is_empty() {
        " "
    } else {
        state.status.as_str()
    };
    frame.render_widget(Paragraph::new(status_text).style(status_style), rows[0]);

    // Lines 2–3: persistent hotkey bars — never replaced by status.
    let bar = Style::default().fg(Color::White).bg(Color::Black);
    frame.render_widget(Paragraph::new(Line::from(nav_hotkey_spans())).style(bar), rows[1]);
    frame.render_widget(
        Paragraph::new(Line::from(sync_hotkey_spans(state))).style(bar),
        rows[2],
    );
}

fn draw_conflict_overlay(frame: &mut Frame, area: Rect, state: &AppState) {
    let Some(crate::state::Pending::ResolveConflict { remaining }) = &state.pending else {
        return;
    };
    let Some(item) = remaining.first() else {
        return;
    };

    let w = area.width.min(90).max(40);
    let h = area.height.min(28).max(12);
    let x = area.x + (area.width.saturating_sub(w)) / 2;
    let y = area.y + (area.height.saturating_sub(h)) / 2;
    let rect = Rect::new(x, y, w, h);
    frame.render_widget(Clear, rect);

    let mut lines: Vec<Line> = Vec::new();
    let kind = match item.kind {
        crate::state::ConflictKind::Skill => "skill",
        crate::state::ConflictKind::Mcp => "mcp",
    };
    lines.push(Line::from(Span::styled(
        format!("Conflict · {kind}"),
        Style::default()
            .fg(Color::Black)
            .bg(Color::Yellow)
            .add_modifier(Modifier::BOLD),
    )));
    lines.push(Line::from(vec![
        Span::raw("Name: "),
        Span::styled(
            item.key.clone(),
            Style::default()
                .fg(Color::Cyan)
                .add_modifier(Modifier::BOLD),
        ),
    ]));
    lines.push(Line::from(Span::styled(
        "[a] keep-source   [b] keep-target   [s] skip   [n] cancel",
        Style::default().fg(Color::Yellow),
    )));
    lines.push(Line::from(""));

    match item.kind {
        crate::state::ConflictKind::Skill => {
            if let Some(s) = state
                .skills()
                .iter()
                .find(|e| e.key == item.key)
                .or_else(|| {
                    state
                        .user_inv
                        .skills
                        .iter()
                        .chain(state.project_inv.skills.iter())
                        .find(|e| e.key == item.key)
                })
            {
                lines.push(Line::from(vec![
                    Span::styled("Skill: ", Style::default().fg(Color::Yellow)),
                    Span::styled(
                        s.display_name.clone(),
                        Style::default()
                            .fg(Color::Cyan)
                            .add_modifier(Modifier::BOLD),
                    ),
                ]));
                crate::diffview::push_skill_mismatch(&mut lines, s);
            } else {
                lines.push(Line::from(format!(
                    "(skill '{}' not in current inventory)",
                    item.key
                )));
            }
        }
        crate::state::ConflictKind::Mcp => {
            if let Some(m) = state
                .mcps()
                .iter()
                .find(|e| e.key == item.key)
                .or_else(|| {
                    state
                        .user_inv
                        .mcps
                        .iter()
                        .chain(state.project_inv.mcps.iter())
                        .find(|e| e.key == item.key)
                })
            {
                lines.push(Line::from(vec![
                    Span::styled("MCP: ", Style::default().fg(Color::Yellow)),
                    Span::styled(
                        m.key.clone(),
                        Style::default()
                            .fg(Color::Magenta)
                            .add_modifier(Modifier::BOLD),
                    ),
                ]));
                crate::diffview::push_mcp_mismatch(&mut lines, m);
            } else {
                lines.push(Line::from(format!(
                    "(mcp '{}' not in current inventory)",
                    item.key
                )));
            }
        }
    }

    let left = remaining.len().saturating_sub(1);
    if left > 0 {
        lines.push(Line::from(""));
        lines.push(Line::from(Span::styled(
            format!("… {left} more conflict(s) after this"),
            Style::default().fg(Color::DarkGray),
        )));
    }

    frame.render_widget(
        Paragraph::new(lines)
            .wrap(Wrap { trim: false })
            .block(
                Block::default()
                    .borders(Borders::ALL)
                    .title(" Resolve conflict ")
                    .border_style(Style::default().fg(Color::Yellow)),
            ),
        rect,
    );
}

fn draw_help(frame: &mut Frame, area: Rect) {
    let w = area.width.min(72);
    let h = area.height.min(22);
    let x = area.x + (area.width.saturating_sub(w)) / 2;
    let y = area.y + (area.height.saturating_sub(h)) / 2;
    let rect = Rect::new(x, y, w, h);
    frame.render_widget(Clear, rect);
    let text = vec![
        Line::from("Keys / Mouse"),
        Line::from("  Tab / click tabs   User ↔ Project"),
        Line::from("  [ / ] / click list Skills / MCPs section"),
        Line::from("  j / k / click row  move selection"),
        Line::from("  wheel on list      move selection"),
        Line::from("  wheel / PgUp/PgDn  scroll detail pane"),
        Line::from("  s                  sync focused item (current page) → y/n"),
        Line::from("  S                  sync ALL skills (current page) → y/n"),
        Line::from("  M                  sync ALL MCPs (current page) → y/n"),
        Line::from("  A                  sync ALL skills + MCPs (current page) → y/n"),
        Line::from("  u                  check/install update from GitHub"),
        Line::from("  r                  reload inventory"),
        Line::from("  ?                  toggle help"),
        Line::from("  q                  quit"),
        Line::from(""),
        Line::from("Conflicts: a=keep-source b=keep-target s=skip"),
        Line::from("Never silent overwrite."),
    ];
    frame.render_widget(
        Paragraph::new(text).block(
            Block::default()
                .borders(Borders::ALL)
                .title(" Help ")
                .border_style(Style::default().fg(Color::Yellow)),
        ),
        rect,
    );
}

/// Whether (col,row) lies inside `r`.
pub fn contains(r: Rect, col: u16, row: u16) -> bool {
    col >= r.x
        && row >= r.y
        && col < r.x.saturating_add(r.width)
        && row < r.y.saturating_add(r.height)
}

/// Map a mouse row inside a bordered list pane to a list index, using ListState offset.
pub fn list_row_at(area: Rect, mouse_row: u16, offset: usize, len: usize) -> Option<usize> {
    if len == 0 || area.height < 3 {
        return None;
    }
    let inner_top = area.y.saturating_add(1);
    let inner_h = area.height.saturating_sub(2);
    if mouse_row < inner_top || mouse_row >= inner_top.saturating_add(inner_h) {
        return None;
    }
    let rel = (mouse_row - inner_top) as usize;
    let idx = offset.saturating_add(rel);
    if idx < len {
        Some(idx)
    } else {
        None
    }
}
