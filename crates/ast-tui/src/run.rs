use crate::actions;
use crate::desk;
use crate::state::{AppState, Pending};
use anyhow::Context;
use crossterm::event::{self, Event, KeyCode, KeyEventKind};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;
use std::io::{stdout, Stdout};
use std::path::Path;
use std::time::Duration;

pub fn run(cwd: &Path) -> anyhow::Result<()> {
    let cwd = cwd
        .canonicalize()
        .unwrap_or_else(|_| cwd.to_path_buf());
    let mut state = AppState::new(&cwd);

    enable_raw_mode().context("enable raw mode")?;
    let mut out = stdout();
    execute!(out, EnterAlternateScreen).context("enter alt screen")?;
    let backend = CrosstermBackend::new(out);
    let mut terminal = Terminal::new(backend).context("terminal")?;

    let result = event_loop(&mut terminal, &mut state);

    disable_raw_mode().ok();
    execute!(terminal.backend_mut(), LeaveAlternateScreen).ok();
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
        let Event::Key(key) = event::read()? else {
            continue;
        };
        if key.kind != KeyEventKind::Press {
            continue;
        }

        // Pending confirm takes priority
        if let Some(ref pending) = state.pending.clone() {
            match key.code {
                KeyCode::Char('y') | KeyCode::Char('Y') => {
                    let _ = pending;
                    actions::confirm_apply(state);
                }
                KeyCode::Char('n') | KeyCode::Char('N') | KeyCode::Esc => {
                    actions::cancel_pending(state);
                }
                _ => {
                    state.status = format!(
                        "confirm {:?}: press y or n",
                        state.pending.as_ref().unwrap_or(&Pending::SyncMissing)
                    );
                }
            }
            continue;
        }

        match key.code {
            KeyCode::Char('q') | KeyCode::Esc => break,
            KeyCode::Tab => {
                state.page = state.page.toggle();
                state.clamp_idx();
                state.status = format!("page: {}", state.page.label());
            }
            KeyCode::Char('[') => {
                state.section = crate::state::Section::Skills;
            }
            KeyCode::Char(']') => {
                state.section = crate::state::Section::Mcps;
            }
            KeyCode::Char('j') | KeyCode::Down => state.move_sel(1),
            KeyCode::Char('k') | KeyCode::Up => state.move_sel(-1),
            KeyCode::Char('s') => actions::dry_run_focused(state),
            KeyCode::Char('S') => actions::dry_run_missing(state),
            KeyCode::Char('u') => actions::check_update(state),
            KeyCode::Char('r') => {
                state.reload();
                state.status = "reloaded".into();
            }
            KeyCode::Char('?') | KeyCode::Char('h') => {
                state.help = !state.help;
            }
            _ => {}
        }
    }
    Ok(())
}
