use crate::state::{AppState, Pending, Section};
use ast_core::sync::{apply_plan, filter_missing, plan_sync_mcp, plan_sync_skills};

pub fn dry_run_focused(state: &mut AppState) {
    let scope = state.page.scope();
    let plan = match state.section {
        Section::Skills => {
            let key = state.skills().get(state.skill_idx).map(|s| s.key.clone());
            match key {
                Some(k) => plan_sync_skills(scope, &state.cwd, None, Some(&k)),
                None => {
                    state.status = "no skill selected".into();
                    return;
                }
            }
        }
        Section::Mcps => {
            let key = state.mcps().get(state.mcp_idx).map(|m| m.key.clone());
            match key {
                Some(k) => plan_sync_mcp(scope, &state.cwd, None, Some(&k)),
                None => {
                    state.status = "no mcp selected".into();
                    return;
                }
            }
        }
    };
    match plan {
        Ok(p) => {
            let n = p.actions.len();
            state.status = format!("dry-run: {n} action(s). Press y to apply, n to cancel.");
            state.last_plan = Some(p);
            state.pending = Some(Pending::SyncFocused {
                kind: state.section,
            });
        }
        Err(e) => state.status = format!("plan error: {e}"),
    }
}

pub fn dry_run_missing(state: &mut AppState) {
    let scope = state.page.scope();
    let skills = plan_sync_skills(scope, &state.cwd, None, None);
    let mcps = plan_sync_mcp(scope, &state.cwd, None, None);
    match (skills, mcps) {
        (Ok(mut s), Ok(m)) => {
            s.actions.extend(m.actions);
            let missing = filter_missing(&s);
            let n = missing.actions.len();
            state.status = format!("sync-missing dry-run: {n} action(s). y apply / n cancel.");
            state.last_plan = Some(missing);
            state.pending = Some(Pending::SyncMissing);
        }
        (Err(e), _) | (_, Err(e)) => state.status = format!("plan error: {e}"),
    }
}

pub fn confirm_apply(state: &mut AppState) {
    let Some(plan) = state.last_plan.take() else {
        state.pending = None;
        state.status = "nothing to apply".into();
        return;
    };
    let scope = state.page.scope();
    match apply_plan(&plan, &state.cwd, scope) {
        Ok(log) => {
            state.status = format!("applied {} step(s)", log.len());
            state.reload();
        }
        Err(e) => state.status = format!("apply error: {e}"),
    }
    state.pending = None;
}

pub fn cancel_pending(state: &mut AppState) {
    state.pending = None;
    state.last_plan = None;
    state.status = "cancelled".into();
}

pub fn check_update(state: &mut AppState) {
    // Lightweight: just hit GitHub API via curl-less reqwest in-process would pull cli;
    // keep TUI free of reqwest — shell out message.
    state.status = format!(
        "run: agent-skills-tui update --check  (current {})",
        env!("CARGO_PKG_VERSION")
    );
}
