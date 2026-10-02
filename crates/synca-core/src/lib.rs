//! Scan and sync skills + MCP configs across coding agents.

pub mod agents;
pub mod discover;
pub mod models;
pub mod paths;
pub mod scan;
pub mod sync;
pub mod manage;
pub mod update;
pub mod update_meta;

pub use models::*;
pub use scan::{scan_mcp, scan_skills, scan_all, Inventory};
pub use sync::{
    apply_plan, filter_missing, merge_plans, plan_sync_mcp, plan_sync_skills, SyncAction,
    SyncPlan,
};
pub use update::{check_update, install_update, UpdateInfo};
pub use update_meta::expected_asset_name;
pub use manage::{
    add_mcp, apply_manage_plan, install_skill, mcp_from_cli, plan_add_mcp,
    plan_install_skill, plan_remove_mcp, plan_remove_skill, remove_mcp, remove_skill,
    resolve_skill_source, ManageAction, ManagePlan,
};

#[cfg(test)]
mod tests_unit;
