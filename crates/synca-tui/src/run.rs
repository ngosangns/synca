use crate::actions;
use crate::desk::{self, contains, list_row_at};
use crate::state::{AppState, Page, Pending, Section};
use anyhow::Context;
use crossterm::event::{
    self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEventKind, MouseButton,
    MouseEvent, MouseEventKind,
};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;
use std::io::{stdout, Stdout};
use std::path::Path;
use std::time::Duration;
use synca_core::models::ConflictPolicy;

pub fn run(cwd: &Path) -> anyhow::Result<()> {
    let cwd = cwd.canonicalize().unwrap_or_else(|_| cwd.to_path_buf());
    let mut state = AppState::new(&cwd);

    enable_raw_mode().context("enable raw mode")?;
    let mut out = stdout();
    execute!(out, EnterAlternateScreen, EnableMouseCapture)
        .context("enter alt screen + mouse")?;
    let backend = CrosstermBackend::new(out);
    let mut terminal = Terminal::new(backend).context("terminal")?;

    let result = event_loop(&mut terminal, &mut state);

    // Always tear down mouse + alt screen (match hearth Restorer pattern).
    let _ = disable_raw_mode();
    let _ = execute!(
        terminal.backend_mut(),
        DisableMouseCapture,
        LeaveAlternateScreen
    );
    result
}

fn event_loop(
    terminal: &mut Terminal<CrosstermBackend<Stdout>>,
    state: &mut AppState,
) -> anyhow::Result<()> {
    loop {
        terminal.draw(|f| desk::draw(f, state))?;

        if !event::poll(Duration::from_millis(200))? {
            continue;
        }
        match event::read()? {
            Event::Key(key) => {
                if key.kind != KeyEventKind::Press {
                    continue;
                }
                if handle_key(state, key.code) {
                    break;
                }
            }
            Event::Mouse(mouse) => handle_mouse(state, mouse),
            Event::Resize(_, _) => {}
            _ => {}
        }
    }
    Ok(())
}

/// Returns true when the event loop should quit.
fn handle_key(state: &mut AppState, code: KeyCode) -> bool {
    if let Some(pending) = state.pending.clone() {
        match pending {
            Pending::SyncConfirm => match code {
                KeyCode::Char('y') | KeyCode::Char('Y') => actions::on_sync_confirm_yes(state),
                KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                    actions::cancel_pending(state)
                }
                _ => {
                    state.status = "confirm sync: press y to continue, n to cancel".into();
                }
            },
            Pending::ResolveConflict { .. } => match code {
                KeyCode::Char('a') | KeyCode::Char('A') => {
                    actions::on_conflict_choice(state, ConflictPolicy::KeepSource)
                }
                KeyCode::Char('b') | KeyCode::Char('B') => {
                    actions::on_conflict_choice(state, ConflictPolicy::KeepTarget)
                }
                KeyCode::Char('s') | KeyCode::Char('S') => {
                    actions::on_conflict_choice(state, ConflictPolicy::Skip)
                }
                KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                    actions::cancel_pending(state)
                }
                _ => {
                    state.status =
                        "conflict: [a] keep-source  [b] keep-target  [s] skip  [n] cancel".into();
                }
            },
            Pending::UpdateInstall => match code {
                KeyCode::Char('y') | KeyCode::Char('Y') => actions::confirm_update_install(state),
                KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                    actions::cancel_pending(state)
                }
                _ => {
                    state.status = "install update? y / n".into();
                }
            },
        }
        return false;
    }

    match code {
        KeyCode::Char('q') | KeyCode::Esc => return true,
        KeyCode::Tab => {
            state.page = state.page.toggle();
            state.on_context_change();
            state.status = format!("page: {}", state.page.label());
        }
        KeyCode::Char('[') => {
            state.section = Section::Skills;
            state.on_context_change();
            state.status = format!("section: {}", state.section.label());
        }
        KeyCode::Char(']') => {
            state.section = Section::Mcps;
            state.on_context_change();
            state.status = format!("section: {}", state.section.label());
        }
        KeyCode::Char(' ') => {
            state.section = state.section.toggle();
            state.on_context_change();
            state.status = format!("section: {}", state.section.label());
        }
        KeyCode::PageDown => {
            state.detail_scroll = state.detail_scroll.saturating_add(3);
        }
        KeyCode::PageUp => {
            state.detail_scroll = state.detail_scroll.saturating_sub(3);
        }
        KeyCode::Char('j') | KeyCode::Down => state.move_sel(1),
        KeyCode::Char('k') | KeyCode::Up => state.move_sel(-1),
        KeyCode::Char('s') => actions::dry_run_focused(state),
        KeyCode::Char('S') => actions::dry_run_all_skills(state),
        KeyCode::Char('M') => actions::dry_run_all_mcp(state),
        KeyCode::Char('A') => actions::dry_run_all(state),
        KeyCode::Char('u') => actions::check_update_action(state),
        KeyCode::Char('r') => {
            state.reload();
            state.status = "reloaded".into();
        }
        KeyCode::Char('?') | KeyCode::Char('h') => {
            state.help = !state.help;
        }
        _ => {}
    }
    false
}

fn handle_mouse(state: &mut AppState, mouse: MouseEvent) {
    // Ignore mouse while a confirm dialog is pending (keys only).
    if state.pending.is_some() {
        return;
    }
    // Dismiss help on any click.
    if state.help {
        if matches!(mouse.kind, MouseEventKind::Down(MouseButton::Left)) {
            state.help = false;
        }
        return;
    }

    let col = mouse.column;
    let row = mouse.row;
    let layout = state.layout;

    match mouse.kind {
        MouseEventKind::Down(MouseButton::Left) => {
            if contains(layout.user_tab, col, row) {
                if state.page != Page::User {
                    state.page = Page::User;
                    state.on_context_change();
                    state.status = "page: User".into();
                }
                return;
            }
            if contains(layout.project_tab, col, row) {
                if state.page != Page::Project {
                    state.page = Page::Project;
                    state.on_context_change();
                    state.status = "page: Project".into();
                }
                return;
            }
            if contains(layout.skills, col, row) {
                state.section = Section::Skills;
                let offset = state.skill_list_state.offset();
                let len = state.skills().len();
                if let Some(idx) = list_row_at(layout.skills, row, offset, len) {
                    state.select_skill(idx);
                    state.status = format!("skill: {}", state.skills()[idx].display_name);
                } else {
                    state.on_context_change();
                    state.status = "section: Skills".into();
                }
                return;
            }
            if contains(layout.mcps, col, row) {
                state.section = Section::Mcps;
                let offset = state.mcp_list_state.offset();
                let len = state.mcps().len();
                if let Some(idx) = list_row_at(layout.mcps, row, offset, len) {
                    state.select_mcp(idx);
                    state.status = format!("mcp: {}", state.mcps()[idx].key);
                } else {
                    state.on_context_change();
                    state.status = "section: MCPs".into();
                }
                return;
            }
        }
        MouseEventKind::ScrollUp | MouseEventKind::ScrollDown => {
            let down = matches!(mouse.kind, MouseEventKind::ScrollDown);
            if contains(layout.detail, col, row) {
                if down {
                    state.detail_scroll = state.detail_scroll.saturating_add(3);
                } else {
                    state.detail_scroll = state.detail_scroll.saturating_sub(3);
                }
                return;
            }
            if contains(layout.skills, col, row) {
                if state.section != Section::Skills {
                    state.section = Section::Skills;
                    state.on_context_change();
                }
                state.move_sel(if down { 1 } else { -1 });
                return;
            }
            if contains(layout.mcps, col, row) {
                if state.section != Section::Mcps {
                    state.section = Section::Mcps;
                    state.on_context_change();
                }
                state.move_sel(if down { 1 } else { -1 });
                return;
            }
        }
        _ => {}
    }
}
