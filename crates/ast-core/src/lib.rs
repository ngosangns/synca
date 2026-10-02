//! Scan and sync skills + MCP configs across coding agents.

pub mod agents;
pub mod discover;
pub mod models;
pub mod paths;
pub mod scan;
pub mod sync;
pub mod update_meta;

pub use models::*;
pub use scan::{scan_mcp, scan_skills, Inventory};
pub use sync::{plan_sync_mcp, plan_sync_skills, apply_plan, SyncAction, SyncPlan};
