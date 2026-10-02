use crate::state::{AppState, ConflictItem, ConflictKind, Pending, Section};
use synca_core::models::{ConflictDecisions, ConflictPolicy};
use synca_core::sync::{
    apply_plan, merge_plans, plan_sync_mcp, plan_sync_skills, SyncAction,
};
use synca_core::manage::{
    add_mcp, install_skill, mcp_from_cli, plan_install_skill, remove_mcp, remove_skill,
    resolve_skill_source,
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
    // Always name the scope so confirm never looks like a cross-page sync.
    let scope = plan.scope.clone();
    state.skill_decisions.clear();
    state.mcp_decisions.clear();
    state.last_plan = Some(plan);
    if conflicts.is_empty() {
        state.status = format!(
            "{label} [{scope}]: {n} action(s). Press y to apply, n to cancel."
        );
        state.pending = Some(Pending::SyncConfirm);
    } else {
        state.status = format!(
            "{label} [{scope}]: {n} action(s), {} conflict(s). Press y to resolve, n to cancel.",
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
        Ok(p) => begin_confirm(state, p, "sync focused"),
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
    // Prefer the plan's recorded scope (set at dry-run) so apply cannot
    // cross User↔Project even if the page somehow changed mid-confirm.
    let scope = match plan.scope.as_str() {
        "project" => synca_core::models::Scope::Project,
        _ => synca_core::models::Scope::User,
    };
    let mut decisions = ConflictDecisions::with_default(ConflictPolicy::Skip);
    decisions.skills = state.skill_decisions.clone();
    decisions.mcps = state.mcp_decisions.clone();
    match apply_plan(&plan, &state.cwd, scope, &decisions) {
        Ok(log) => {
            state.status = format!("applied {} step(s) [{}]", log.len(), scope.as_str());
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

pub fn begin_install(state: &mut AppState) {
    match state.section {
        Section::Skills => {
            state.pending = Some(Pending::InstallSkillInput {
                buffer: String::new(),
            });
            state.status =
                "Install skill: type local path or git URL, Enter to preview, Esc cancel".into();
        }
        Section::Mcps => {
            state.pending = Some(Pending::InstallMcpName {
                buffer: String::new(),
            });
            state.status = "Add MCP: type server name, Enter next, Esc cancel".into();
        }
    }
}

pub fn begin_delete(state: &mut AppState) {
    match state.section {
        Section::Skills => {
            let Some(s) = state.skills().get(state.skill_idx) else {
                state.status = "no skill selected".into();
                return;
            };
            let key = s.key.clone();
            state.status = format!(
                "Delete skill '{key}' [{scope}]: [y] unlink agents  [p] PURGE canonical  [n] cancel",
                scope = state.page.scope().as_str()
            );
            state.pending = Some(Pending::DeleteSkillConfirm { key });
        }
        Section::Mcps => {
            let Some(m) = state.mcps().get(state.mcp_idx) else {
                state.status = "no mcp selected".into();
                return;
            };
            let key = m.key.clone();
            state.status = format!(
                "Remove MCP '{key}' from hub+agents [{scope}]? y/n",
                scope = state.page.scope().as_str()
            );
            state.pending = Some(Pending::DeleteMcpConfirm { key });
        }
    }
}

pub fn install_skill_preview(state: &mut AppState, source: &str) {
    let scope = state.page.scope();
    match resolve_skill_source(source) {
        Ok((dir, _tmp)) => match plan_install_skill(scope, &state.cwd, &dir, None) {
            Ok(plan) => {
                let key = plan
                    .actions
                    .iter()
                    .find_map(|a| match a {
                        synca_core::ManageAction::CopySkill { skill_key, .. } => {
                            Some(skill_key.clone())
                        }
                        _ => None,
                    })
                    .unwrap_or_else(|| "skill".into());
                state.status = format!(
                    "Install '{key}' → canonical + {} link(s) [{scope}]. y=apply n=cancel",
                    plan.actions.len().saturating_sub(1),
                    scope = scope.as_str()
                );
                state.pending = Some(Pending::InstallSkillConfirm {
                    source: source.to_string(),
                    key,
                });
            }
            Err(e) => {
                state.status = format!("plan error: {e}");
                state.pending = Some(Pending::InstallSkillInput {
                    buffer: source.to_string(),
                });
            }
        },
        Err(e) => {
            state.status = format!("resolve error: {e}");
            state.pending = Some(Pending::InstallSkillInput {
                buffer: source.to_string(),
            });
        }
    }
}

pub fn confirm_install_skill(state: &mut AppState, source: &str) {
    let scope = state.page.scope();
    match install_skill(scope, &state.cwd, source, None, false) {
        Ok((_plan, log)) => {
            state.status = format!("installed ({} steps) [{}]", log.len(), scope.as_str());
            state.reload();
        }
        Err(e) => state.status = format!("install error: {e}"),
    }
    state.pending = None;
}

pub fn confirm_install_mcp(state: &mut AppState, name: &str, transport: &str, endpoint: &str) {
    let scope = state.page.scope();
    let norm = if matches!(transport, "stdio" | "local") {
        mcp_from_cli(transport, Some(endpoint), None, Some(true))
    } else {
        mcp_from_cli(transport, None, Some(endpoint), Some(true))
    };
    match norm.and_then(|n| add_mcp(scope, &state.cwd, name, n, None, false)) {
        Ok((_plan, log)) => {
            state.status = format!("mcp '{name}' added ({} steps) [{}]", log.len(), scope.as_str());
            state.reload();
        }
        Err(e) => state.status = format!("mcp add error: {e}"),
    }
    state.pending = None;
}

pub fn confirm_unlink_skill(state: &mut AppState, key: &str) {
    let scope = state.page.scope();
    match remove_skill(scope, &state.cwd, key, None, false, false) {
        Ok((_plan, log)) => {
            state.status = format!("unlinked '{key}' ({} steps) [{}]", log.len(), scope.as_str());
            state.reload();
        }
        Err(e) => state.status = format!("unlink error: {e}"),
    }
    state.pending = None;
}

pub fn confirm_purge_skill(state: &mut AppState, key: &str) {
    let scope = state.page.scope();
    match remove_skill(scope, &state.cwd, key, None, true, false) {
        Ok((_plan, log)) => {
            state.status = format!("PURGED '{key}' ({} steps) [{}]", log.len(), scope.as_str());
            state.reload();
        }
        Err(e) => state.status = format!("purge error: {e}"),
    }
    state.pending = None;
}

pub fn confirm_remove_mcp(state: &mut AppState, key: &str) {
    let scope = state.page.scope();
    match remove_mcp(scope, &state.cwd, key, None, false) {
        Ok((_plan, log)) => {
            state.status = format!("removed mcp '{key}' ({} steps) [{}]", log.len(), scope.as_str());
            state.reload();
        }
        Err(e) => state.status = format!("mcp remove error: {e}"),
    }
    state.pending = None;
}
