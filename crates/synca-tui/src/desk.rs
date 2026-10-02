use crate::state::{AppState, Page, Section};
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Clear, List, ListItem, Paragraph, Wrap};
use ratatui::Frame;

pub fn draw(frame: &mut Frame, state: &AppState) {
    let area = frame.area();
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(1),
            Constraint::Min(5),
            Constraint::Length(1),
        ])
        .split(area);

    draw_header(frame, chunks[0], state);
    draw_body(frame, chunks[1], state);
    draw_footer(frame, chunks[2], state);

    if state.help {
        draw_help(frame, area);
    }
}

fn draw_header(frame: &mut Frame, area: Rect, state: &AppState) {
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

fn draw_body(frame: &mut Frame, area: Rect, state: &AppState) {
    let cols = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Percentage(45), Constraint::Percentage(55)])
        .split(area);

    let lists = Layout::default()
        .direction(Direction::Vertical)
        .constraints([Constraint::Percentage(55), Constraint::Percentage(45)])
        .split(cols[0]);

    draw_skills_list(frame, lists[0], state);
    draw_mcps_list(frame, lists[1], state);
    draw_detail(frame, cols[1], state);
}

fn draw_skills_list(frame: &mut Frame, area: Rect, state: &AppState) {
    let focused = state.section == Section::Skills;
    let title = if focused { " Skills * " } else { " Skills " };
    let border = if focused { Color::Cyan } else { Color::DarkGray };
    let items: Vec<ListItem> = state
        .skills()
        .iter()
        .enumerate()
        .map(|(i, s)| {
            let agents: Vec<_> = s.presence.iter().map(|p| p.agent.as_str()).collect();
            let mark = if s.mismatch { "!" } else { " " };
            let sel = focused && i == state.skill_idx;
            let style = if sel {
                Style::default()
                    .bg(Color::Blue)
                    .fg(Color::White)
                    .add_modifier(Modifier::BOLD)
            } else {
                Style::default()
            };
            ListItem::new(format!(
                "{mark} {}  ({}) [{}]",
                s.display_name,
                s.presence.len(),
                agents.join(",")
            ))
            .style(style)
        })
        .collect();
    let list = List::new(items).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(border))
            .title(title),
    );
    frame.render_widget(list, area);
}

fn draw_mcps_list(frame: &mut Frame, area: Rect, state: &AppState) {
    let focused = state.section == Section::Mcps;
    let title = if focused { " MCPs * " } else { " MCPs " };
    let border = if focused { Color::Cyan } else { Color::DarkGray };
    let items: Vec<ListItem> = state
        .mcps()
        .iter()
        .enumerate()
        .map(|(i, m)| {
            let agents: Vec<_> = m.presence.iter().map(|p| p.agent.as_str()).collect();
            let mark = if m.mismatch { "!" } else { " " };
            let sel = focused && i == state.mcp_idx;
            let style = if sel {
                Style::default()
                    .bg(Color::Blue)
                    .fg(Color::White)
                    .add_modifier(Modifier::BOLD)
            } else {
                Style::default()
            };
            ListItem::new(format!(
                "{mark} {}  ({}) [{}]",
                m.key,
                m.presence.len(),
                agents.join(",")
            ))
            .style(style)
        })
        .collect();
    let list = List::new(items).block(
        Block::default()
            .borders(Borders::ALL)
            .border_style(Style::default().fg(border))
            .title(title),
    );
    frame.render_widget(list, area);
}

fn draw_detail(frame: &mut Frame, area: Rect, state: &AppState) {
    let mut lines: Vec<Line> = Vec::new();
    match state.section {
        Section::Skills => {
            if let Some(s) = state.skills().get(state.skill_idx) {
                lines.push(Line::from(Span::styled(
                    format!("Skill: {}", s.display_name),
                    Style::default()
                        .fg(Color::Yellow)
                        .add_modifier(Modifier::BOLD),
                )));
                lines.push(Line::from(format!("key: {}", s.key)));
                lines.push(Line::from(format!(
                    "mismatch: {}",
                    if s.mismatch { "YES" } else { "no" }
                )));
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
                    lines.push(Line::from(format!(
                        "  · {}  {}{}",
                        p.agent.as_str(),
                        p.path.display(),
                        link
                    )));
                    lines.push(Line::from(format!(
                        "      hash: {}",
                        &p.content_hash[..p.content_hash.len().min(12)]
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
                lines.push(Line::from(Span::styled(
                    format!("MCP: {}", m.key),
                    Style::default()
                        .fg(Color::Yellow)
                        .add_modifier(Modifier::BOLD),
                )));
                lines.push(Line::from(format!(
                    "mismatch: {}",
                    if m.mismatch { "YES" } else { "no" }
                )));
                lines.push(Line::from(""));
                lines.push(Line::from(Span::styled(
                    "Agents:",
                    Style::default().add_modifier(Modifier::BOLD),
                )));
                for p in &m.presence {
                    lines.push(Line::from(format!(
                        "  · {}  {}",
                        p.agent.as_str(),
                        p.path.display()
                    )));
                    lines.push(Line::from(format!(
                        "      transport={}  fp={}",
                        p.normalized.transport,
                        &p.fingerprint[..p.fingerprint.len().min(12)]
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
        .block(
            Block::default()
                .borders(Borders::ALL)
                .title(" Detail ")
                .border_style(Style::default().fg(Color::DarkGray)),
        );
    frame.render_widget(para, area);
}

fn draw_footer(frame: &mut Frame, area: Rect, state: &AppState) {
    let style = if state.pending.is_some() {
        Style::default().fg(Color::Black).bg(Color::Yellow)
    } else {
        Style::default().fg(Color::Gray)
    };
    frame.render_widget(Paragraph::new(state.status.as_str()).style(style), area);
}

fn draw_help(frame: &mut Frame, area: Rect) {
    let w = area.width.min(70);
    let h = area.height.min(18);
    let x = area.x + (area.width.saturating_sub(w)) / 2;
    let y = area.y + (area.height.saturating_sub(h)) / 2;
    let rect = Rect::new(x, y, w, h);
    frame.render_widget(Clear, rect);
    let text = vec![
        Line::from("Keys"),
        Line::from("  Tab          switch User / Project"),
        Line::from("  [ / ]        Skills / MCPs section"),
        Line::from("  j / k        move selection"),
        Line::from("  s            dry-run sync focused → y/n"),
        Line::from("  S            dry-run sync missing → y/n"),
        Line::from("  u            check/install update from GitHub"),
        Line::from("  r            reload inventory"),
        Line::from("  ?            toggle help"),
        Line::from("  q            quit"),
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
