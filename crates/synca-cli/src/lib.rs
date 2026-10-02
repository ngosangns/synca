mod update;

use anyhow::Context;
use synca_core::models::{AgentKind, ConflictDecisions, ConflictPolicy, Scope};
use synca_core::manage::{add_mcp, install_skill, mcp_from_cli, remove_mcp, remove_skill};
use synca_core::scan::scan_all;
use synca_core::sync::{apply_plan, merge_plans, plan_sync_mcp, plan_sync_skills, SyncAction, SyncPlan};
use clap::{Parser, Subcommand, ValueEnum};
use std::path::PathBuf;

#[derive(Parser, Debug)]
#[command(name = "synca", version, about = "Browse and sync skills + MCP across coding agents")]
pub struct Cli {
    #[command(subcommand)]
    pub command: Option<Commands>,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    /// Open the interactive TUI (default)
    Tui {
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    Skills {
        #[command(subcommand)]
        cmd: SkillsCmd,
    },
    Mcp {
        #[command(subcommand)]
        cmd: McpCmd,
    },
    Sync {
        #[command(subcommand)]
        cmd: SyncCmd,
    },
    /// Check / install updates from GitHub Releases
    Update {
        #[arg(long)]
        check: bool,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        force: bool,
    },
}

#[derive(Subcommand, Debug)]
pub enum SkillsCmd {
    List {
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    /// Install a skill from a local path or git URL into canonical + agent links
    Install {
        /// Local path or git URL containing SKILL.md
        source: String,
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    /// Remove a skill (default: unlink agents only; --purge deletes canonical too)
    Remove {
        name: String,
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        /// Also delete canonical tree (requires --yes or interactive double-confirm)
        #[arg(long)]
        purge: bool,
        /// Skip interactive purge confirmation
        #[arg(long)]
        yes: bool,
        #[arg(long)]
        dry_run: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
}

#[derive(Subcommand, Debug)]
pub enum McpCmd {
    List {
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    /// Add an MCP server to the hub and propagate to agents
    Add {
        name: String,
        #[arg(long, default_value = "stdio")]
        transport: String,
        /// Command line for stdio (e.g. "npx -y @pkg/server")
        #[arg(long)]
        command: Option<String>,
        /// URL for sse/http/remote
        #[arg(long)]
        url: Option<String>,
        #[arg(long)]
        enabled: Option<bool>,
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    /// Remove an MCP server from hub + agent configs in scope
    Remove {
        name: String,
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
}

#[derive(Subcommand, Debug)]
pub enum SyncCmd {
    Skills {
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long)]
        key: Option<String>,
        /// Conflict policy: skip | keep-source | keep-target (default: skip; prompt on tty if omitted and conflicts exist)
        #[arg(long, default_value = "skip")]
        on_conflict: String,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    Mcp {
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long)]
        key: Option<String>,
        #[arg(long, default_value = "skip")]
        on_conflict: String,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
    /// Sync all skills and all MCPs for the scope
    All {
        #[arg(long, value_enum, default_value_t = ScopeArg::User)]
        scope: ScopeArg,
        #[arg(long)]
        agents: Option<String>,
        #[arg(long)]
        dry_run: bool,
        #[arg(long, default_value = "skip")]
        on_conflict: String,
        #[arg(long, default_value = ".")]
        cwd: PathBuf,
    },
}

#[derive(Clone, Copy, Debug, ValueEnum, Default)]
pub enum ScopeArg {
    #[default]
    User,
    Project,
}

impl From<ScopeArg> for Scope {
    fn from(s: ScopeArg) -> Self {
        match s {
            ScopeArg::User => Scope::User,
            ScopeArg::Project => Scope::Project,
        }
    }
}

pub fn run() -> anyhow::Result<()> {
    let cli = Cli::parse();
    match cli.command.unwrap_or(Commands::Tui {
        cwd: std::env::current_dir().unwrap_or_else(|_| PathBuf::from(".")),
    }) {
        Commands::Tui { cwd } => {
            synca_tui::run(&cwd)?;
        }
        Commands::Skills { cmd } => match cmd {
            SkillsCmd::List { scope, json, cwd } => {
                let inv = scan_all(scope.into(), &cwd);
                if json {
                    println!("{}", serde_json::to_string_pretty(&inv.skills)?);
                } else {
                    println!("Skills ({})", scope_label(scope));
                    for s in &inv.skills {
                        let agents: Vec<_> = s.presence.iter().map(|p| p.agent.as_str()).collect();
                        let flag = if s.mismatch { " MISMATCH" } else { "" };
                        println!("  {}  [{}]{}", s.display_name, agents.join(","), flag);
                    }
                    println!("{} skill(s)", inv.skills.len());
                }
            }
            SkillsCmd::Install {
                source,
                scope,
                agents,
                dry_run,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let (plan, log) = install_skill(scope, &cwd, &source, filter.as_deref(), dry_run)?;
                print_manage_plan(&plan);
                if dry_run {
                    println!("(dry-run; no changes)");
                } else {
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
            SkillsCmd::Remove {
                name,
                scope,
                agents,
                purge,
                yes,
                dry_run,
                cwd,
            } => {
                let scope: Scope = scope.into();
                if purge && !yes && !dry_run {
                    if !confirm_purge(&name)? {
                        println!("purge cancelled");
                        return Ok(());
                    }
                }
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let (plan, log) =
                    remove_skill(scope, &cwd, &name, filter.as_deref(), purge, dry_run)?;
                print_manage_plan(&plan);
                if dry_run {
                    println!("(dry-run; no changes)");
                } else {
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
        },
        Commands::Mcp { cmd } => match cmd {
            McpCmd::List { scope, json, cwd } => {
                let inv = scan_all(scope.into(), &cwd);
                if json {
                    println!("{}", serde_json::to_string_pretty(&inv.mcps)?);
                } else {
                    println!("MCPs ({})", scope_label(scope));
                    for m in &inv.mcps {
                        let agents: Vec<_> = m.presence.iter().map(|p| p.agent.as_str()).collect();
                        let flag = if m.mismatch { " MISMATCH" } else { "" };
                        println!("  {}  [{}]{}", m.key, agents.join(","), flag);
                    }
                    println!("{} mcp server(s)", inv.mcps.len());
                }
            }
            McpCmd::Add {
                name,
                transport,
                command,
                url,
                enabled,
                scope,
                agents,
                dry_run,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let norm = mcp_from_cli(
                    &transport,
                    command.as_deref(),
                    url.as_deref(),
                    enabled,
                )?;
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let (plan, log) = add_mcp(scope, &cwd, &name, norm, filter.as_deref(), dry_run)?;
                print_manage_plan(&plan);
                if dry_run {
                    println!("(dry-run; no changes)");
                } else {
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
            McpCmd::Remove {
                name,
                scope,
                agents,
                dry_run,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let (plan, log) = remove_mcp(scope, &cwd, &name, filter.as_deref(), dry_run)?;
                print_manage_plan(&plan);
                if dry_run {
                    println!("(dry-run; no changes)");
                } else {
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
        },
        Commands::Sync { cmd } => match cmd {
            SyncCmd::Skills {
                scope,
                agents,
                dry_run,
                key,
                on_conflict,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let filter_ref = filter.as_deref();
                let mut plan = plan_sync_skills(scope, &cwd, filter_ref, key.as_deref())?;
                plan.dry_run = dry_run;
                print_plan(&plan);
                let decisions = resolve_cli_conflicts(&plan, &on_conflict, dry_run)?;
                if dry_run {
                    println!("(dry-run; no changes) on-conflict={}", decisions.default.as_str());
                } else {
                    let log = apply_plan(&plan, &cwd, scope, &decisions)?;
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
            SyncCmd::Mcp {
                scope,
                agents,
                dry_run,
                key,
                on_conflict,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let filter_ref = filter.as_deref();
                let mut plan = plan_sync_mcp(scope, &cwd, filter_ref, key.as_deref())?;
                plan.dry_run = dry_run;
                print_plan(&plan);
                let decisions = resolve_cli_conflicts(&plan, &on_conflict, dry_run)?;
                if dry_run {
                    println!("(dry-run; no changes) on-conflict={}", decisions.default.as_str());
                } else {
                    let log = apply_plan(&plan, &cwd, scope, &decisions)?;
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
            SyncCmd::All {
                scope,
                agents,
                dry_run,
                on_conflict,
                cwd,
            } => {
                let scope: Scope = scope.into();
                let filter = agents.as_ref().map(|s| AgentKind::parse_list(s));
                let filter_ref = filter.as_deref();
                let mut skills = plan_sync_skills(scope, &cwd, filter_ref, None)?;
                let mut mcps = plan_sync_mcp(scope, &cwd, filter_ref, None)?;
                skills.dry_run = dry_run;
                mcps.dry_run = dry_run;
                let plan = merge_plans(skills, mcps);
                print_plan(&plan);
                let decisions = resolve_cli_conflicts(&plan, &on_conflict, dry_run)?;
                if dry_run {
                    println!("(dry-run; no changes) on-conflict={}", decisions.default.as_str());
                } else {
                    let log = apply_plan(&plan, &cwd, scope, &decisions)?;
                    for line in log {
                        println!("  {line}");
                    }
                }
            }
        },
        Commands::Update { check, json, force } => {
            update::run_update(check, json, force)?;
        }
    }
    Ok(())
}

fn scope_label(s: ScopeArg) -> &'static str {
    match s {
        ScopeArg::User => "user",
        ScopeArg::Project => "project",
    }
}

fn print_plan(plan: &synca_core::SyncPlan) {
    println!("Sync plan (scope={}, actions={}):", plan.scope, plan.actions.len());
    for (i, a) in plan.actions.iter().enumerate() {
        println!("  {}. {}", i + 1, serde_json::to_string(a).unwrap_or_default());
    }
}


fn resolve_cli_conflicts(
    plan: &SyncPlan,
    on_conflict: &str,
    dry_run: bool,
) -> anyhow::Result<ConflictDecisions> {
    let mut policy = ConflictPolicy::parse(on_conflict)
        .with_context(|| format!("invalid --on-conflict '{on_conflict}' (use skip|keep-source|keep-target)"))?;

    let has_conflict = plan.actions.iter().any(|a| {
        matches!(
            a,
            SyncAction::ConflictSkill { .. } | SyncAction::ConflictMcp { .. }
        )
    });

    // If conflicts and default skip, and stdin is a tty, prompt once (unless dry-run).
    if has_conflict && !dry_run && on_conflict == "skip" && atty_stdin() {
        eprintln!("Conflicts detected. Choose policy:");
        eprintln!("  [s] skip (default)");
        eprintln!("  [a] keep-source");
        eprintln!("  [b] keep-target");
        eprint!("> ");
        let mut line = String::new();
        let _ = std::io::stdin().read_line(&mut line);
        let choice = line.trim();
        if let Some(p) = ConflictPolicy::parse(choice) {
            policy = p;
        } else if choice.is_empty() {
            policy = ConflictPolicy::Skip;
        } else {
            anyhow::bail!("unknown conflict choice '{choice}'");
        }
    }

    Ok(ConflictDecisions::with_default(policy))
}

fn atty_stdin() -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::io::AsRawFd;
        extern "C" {
            fn isatty(fd: i32) -> i32;
        }
        unsafe { isatty(std::io::stdin().as_raw_fd()) == 1 }
    }
    #[cfg(not(unix))]
    {
        false
    }
}



fn print_manage_plan(plan: &synca_core::ManagePlan) {
    println!(
        "Manage plan (scope={}, dry_run={}, actions={}):",
        plan.scope,
        plan.dry_run,
        plan.actions.len()
    );
    for n in &plan.notes {
        println!("  note: {n}");
    }
    for (i, a) in plan.actions.iter().enumerate() {
        println!("  {}. {}", i + 1, serde_json::to_string(a).unwrap_or_default());
    }
}

fn confirm_purge(name: &str) -> anyhow::Result<bool> {
    eprintln!("PURGE skill '{name}' will DELETE the canonical tree and unlink all agents.");
    eprintln!("Type the skill name again to confirm, or press Enter to cancel:");
    eprint!("> ");
    let mut line = String::new();
    std::io::stdin().read_line(&mut line)?;
    Ok(line.trim() == name || line.trim() == synca_core::models::normalize_key(name))
}

pub fn main_entry() -> anyhow::Result<()> {
    run().context("synca failed")
}
