//! Scan and sync skills + MCP configs across coding agents.

pub mod agents;
pub mod discover;
pub mod models;
pub mod paths;
pub mod scan;
pub mod sync;
pub mod update;
pub mod update_meta;

pub use models::*;
pub use scan::{scan_mcp, scan_skills, scan_all, Inventory};
pub use sync::{
    apply_plan, filter_missing, plan_sync_mcp, plan_sync_skills, SyncAction, SyncPlan,
};
pub use update::{check_update, install_update, UpdateInfo};
pub use update_meta::expected_asset_name;

#[cfg(test)]
mod tests_unit;
