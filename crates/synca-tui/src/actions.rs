use crate::state::{AppState, ConflictItem, ConflictKind, Pending, Section};
use synca_core::models::{ConflictDecisions, ConflictPolicy};
use synca_core::sync::{
    apply_plan, merge_plans, plan_sync_mcp, plan_sync_skills, SyncAction,
};
use synca_core::update::{check_update, install_update};

fn conflicts_from_plan(plan: &synca_core::SyncPlan) -> Vec<ConflictItem> {
    let mut out = Vec::new();
    for a in &plan.actions {
        match a {
            SyncAction::ConflictSkill { skill_key, .. } => out.push(ConflictItem {
                kind: ConflictKind::Skill,
                key: skill_key.clone(),
            }),
            SyncAction::ConflictMcp { server, .. } => out.push(ConflictItem {
                kind: ConflictKind::Mcp,
                key: server.clone(),
            }),
            _ => {}
        }
    }
    out
}

fn begin_confirm(state: &mut AppState, plan: synca_core::SyncPlan, label: &str) {
    let n = plan.actions.len();
    let conflicts = conflicts_from_plan(&plan);
    state.skill_decisions.clear();
    state.mcp_decisions.clear();
    state.last_plan = Some(plan);
    if conflicts.is_empty() {
        state.status = format!("{label}: {n} action(s). Press y to apply, n to cancel.");
        state.pending = Some(Pending::SyncConfirm);
    } else {
        state.status = format!(
            "{label}: {n} action(s), {} conflict(s). Press y to resolve, n to cancel.",
            conflicts.len()
        );
        state.pending = Some(Pending::SyncConfirm);
    }
}

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
        Ok(p) => begin_confirm(state, p, "dry-run"),
        Err(e) => state.status = format!("plan error: {e}"),
    }
}

pub fn dry_run_all_skills(state: &mut AppState) {
    let scope = state.page.scope();
    match plan_sync_skills(scope, &state.cwd, None, None) {
        Ok(plan) => begin_confirm(state, plan, "sync all skills"),
        Err(e) => state.status = format!("plan error: {e}"),
    }
}

pub fn dry_run_all_mcp(state: &mut AppState) {
    let scope = state.page.scope();
    match plan_sync_mcp(scope, &state.cwd, None, None) {
        Ok(plan) => begin_confirm(state, plan, "sync all mcps"),
        Err(e) => state.status = format!("plan error: {e}"),
    }
}

pub fn dry_run_all(state: &mut AppState) {
    let scope = state.page.scope();
    match (
        plan_sync_skills(scope, &state.cwd, None, None),
        plan_sync_mcp(scope, &state.cwd, None, None),
    ) {
        (Ok(skills_plan), Ok(mcps_plan)) => {
            let plan = merge_plans(skills_plan, mcps_plan);
            begin_confirm(state, plan, "sync all skills+mcps");
        }
        (Err(e), _) | (_, Err(e)) => state.status = format!("plan error: {e}"),
    }
}


pub fn on_sync_confirm_yes(state: &mut AppState) {
    let Some(plan) = state.last_plan.as_ref() else {
        state.pending = None;
        state.status = "nothing to apply".into();
        return;
    };
    let conflicts = conflicts_from_plan(plan);
    if conflicts.is_empty() {
        apply_with_decisions(state);
    } else {
        let first = conflicts[0].clone();
        state.status = conflict_prompt(&first);
        state.pending = Some(Pending::ResolveConflict {
            remaining: conflicts,
        });
    }
}

fn conflict_prompt(item: &ConflictItem) -> String {
    let kind = match item.kind {
        ConflictKind::Skill => "skill",
        ConflictKind::Mcp => "mcp",
    };
    format!(
        "Conflict {kind} '{}': [a] keep-source  [b] keep-target  [s] skip  [n] cancel",
        item.key
    )
}

pub fn on_conflict_choice(state: &mut AppState, policy: ConflictPolicy) {
    let Some(Pending::ResolveConflict { remaining }) = state.pending.clone() else {
        return;
    };
    if remaining.is_empty() {
        apply_with_decisions(state);
        return;
    }
    let current = &remaining[0];
    match current.kind {
        ConflictKind::Skill => {
            state
                .skill_decisions
                .insert(current.key.clone(), policy);
        }
        ConflictKind::Mcp => {
            state.mcp_decisions.insert(current.key.clone(), policy);
        }
    }
    let rest: Vec<_> = remaining.into_iter().skip(1).collect();
    if rest.is_empty() {
        apply_with_decisions(state);
    } else {
        state.status = conflict_prompt(&rest[0]);
        state.pending = Some(Pending::ResolveConflict { remaining: rest });
    }
}

fn apply_with_decisions(state: &mut AppState) {
    let Some(plan) = state.last_plan.take() else {
        state.pending = None;
        state.status = "nothing to apply".into();
        return;
    };
    let scope = state.page.scope();
    let mut decisions = ConflictDecisions::with_default(ConflictPolicy::Skip);
    decisions.skills = state.skill_decisions.clone();
    decisions.mcps = state.mcp_decisions.clone();
    match apply_plan(&plan, &state.cwd, scope, &decisions) {
        Ok(log) => {
            state.status = format!("applied {} step(s)", log.len());
            state.reload();
        }
        Err(e) => state.status = format!("apply error: {e}"),
    }
    state.pending = None;
    state.skill_decisions.clear();
    state.mcp_decisions.clear();
}

pub fn cancel_pending(state: &mut AppState) {
    state.pending = None;
    state.last_plan = None;
    state.skill_decisions.clear();
    state.mcp_decisions.clear();
    state.status = "cancelled".into();
}

pub fn check_update_action(state: &mut AppState) {
    state.status = "checking for updates…".into();
    match check_update() {
        Ok(info) => {
            state.update_msg = info.message.clone();
            if info.update_available {
                state.status = format!(
                    "{}  Press y to install, n to cancel.",
                    info.message
                );
                state.pending = Some(Pending::UpdateInstall);
            } else {
                state.status = info.message;
                state.pending = None;
            }
        }
        Err(e) => {
            state.status = format!("update check failed: {e}");
            state.pending = None;
        }
    }
}

pub fn confirm_update_install(state: &mut AppState) {
    state.status = "installing update…".into();
    match install_update(false) {
        Ok(info) => {
            state.status = info.message;
            state.update_msg = state.status.clone();
        }
        Err(e) => state.status = format!("update install failed: {e}"),
    }
    state.pending = None;
}
