//! Unit tests (included from lib via cfg(test)).

use crate::discover::{skill_description, skill_display_name, skill_frontmatter_description, skill_frontmatter_name};
use crate::models::{
    normalize_key, AgentKind, ConflictDecisions, ConflictPolicy, McpNormalized, Scope,
};
use crate::scan::{json_to_normalized, mcp_fingerprint, scan_skills, toml_to_normalized};
use crate::sync::{
    apply_plan, merge_plans, plan_sync_mcp, plan_sync_skills, upsert_mcp_json_hub, SyncAction,
};
use crate::manage::{
    add_mcp, install_skill, mcp_from_cli, remove_mcp, remove_skill,
};
use serde_json::json;
use std::collections::BTreeMap;
use std::path::Path;
use std::sync::Mutex;

static HOME_LOCK: Mutex<()> = Mutex::new(());

fn with_temp_home<F: FnOnce(&Path)>(f: F) {
    let _guard = HOME_LOCK.lock().unwrap();
    let tmp = tempfile::tempdir().unwrap();
    let old = std::env::var_os("HOME");
    std::env::set_var("HOME", tmp.path());
    f(tmp.path());
    match old {
        Some(v) => std::env::set_var("HOME", v),
        None => std::env::remove_var("HOME"),
    }
}

fn write_skill(dir: &Path, name: &str, body: &str) {
    let skill = dir.join(name);
    std::fs::create_dir_all(&skill).unwrap();
    std::fs::write(
        skill.join("SKILL.md"),
        format!("---\nname: {name}\n---\n{body}\n"),
    )
    .unwrap();
}

#[test]
fn normalize_key_basic() {
    assert_eq!(normalize_key("Foo_Bar"), "foo-bar");
    assert_eq!(normalize_key("  Hello World "), "hello-world");
    assert_eq!(normalize_key("already-ok"), "already-ok");
}

#[test]
fn skill_frontmatter_and_display_name() {
    let tmp = tempfile::tempdir().unwrap();
    let skill = tmp.path().join("my-skill");
    std::fs::create_dir_all(&skill).unwrap();
    std::fs::write(
        skill.join("SKILL.md"),
        "---\nname: Fancy Name\ndescription: x\n---\n# hi\n",
    )
    .unwrap();
    assert_eq!(
        skill_frontmatter_name(&skill.join("SKILL.md")).as_deref(),
        Some("Fancy Name")
    );
    assert_eq!(
        skill_frontmatter_description(&skill.join("SKILL.md")).as_deref(),
        Some("x")
    );
    assert_eq!(skill_display_name(&skill), "Fancy Name");
    assert_eq!(skill_description(&skill).as_deref(), Some("x"));

    let skill2 = tmp.path().join("folder-only");
    std::fs::create_dir_all(&skill2).unwrap();
    std::fs::write(skill2.join("SKILL.md"), "# no frontmatter\n").unwrap();
    assert_eq!(skill_display_name(&skill2), "folder-only");
    assert_eq!(skill_description(&skill2), None);
}

#[test]
fn mcp_normalize_json_command_string_and_array() {
    let s = json_to_normalized(&json!({
        "type": "stdio",
        "command": "npx",
        "args": ["-y", "foo"],
        "env": {"TOKEN": "secret"}
    }));
    assert_eq!(s.transport, "stdio");
    assert_eq!(s.command.as_ref().unwrap(), &vec!["npx".to_string()]);
    assert_eq!(s.args.as_ref().unwrap(), &vec!["-y".to_string(), "foo".to_string()]);
    assert_eq!(s.env.get("TOKEN").map(String::as_str), Some("secret"));
    assert_eq!(s.env_keys, vec!["TOKEN".to_string()]);

    let a = json_to_normalized(&json!({
        "command": ["python", "-m", "server"],
        "url": null
    }));
    assert_eq!(
        a.command.as_ref().unwrap(),
        &vec!["python".to_string(), "-m".to_string(), "server".to_string()]
    );
    assert_eq!(a.transport, "stdio");

    let u = json_to_normalized(&json!({"url": "https://example.com/sse"}));
    assert_eq!(u.transport, "sse");
    assert_eq!(u.url.as_deref(), Some("https://example.com/sse"));
}

#[test]
fn mcp_normalize_toml_and_opencode() {
    let toml_cfg: toml::Value = toml::from_str(
        r#"
type = "stdio"
command = "uvx"
args = ["mcp-server"]
[env]
KEY = "val"
"#,
    )
    .unwrap();
    let n = toml_to_normalized(&toml_cfg);
    assert_eq!(n.command.as_ref().unwrap(), &vec!["uvx".to_string()]);
    assert_eq!(n.env.get("KEY").map(String::as_str), Some("val"));

    // OpenCode-style: same json_to_normalized
    let oc = json_to_normalized(&json!({
        "type": "remote",
        "url": "https://mcp.example",
        "enabled": true
    }));
    assert_eq!(oc.transport, "remote");
    assert_eq!(oc.enabled, Some(true));
}

#[test]
fn mcp_fingerprint_ignores_env_values() {
    let a = McpNormalized {
        transport: "stdio".into(),
        command: Some(vec!["npx".into()]),
        url: None,
        args: None,
        enabled: Some(true),
        env_keys: vec!["TOKEN".into()],
        env: BTreeMap::from([("TOKEN".into(), "aaa".into())]),
    };
    let mut b = a.clone();
    b.env.insert("TOKEN".into(), "bbb".into());
    b.enabled = Some(false); // enabled ignored in fingerprint currently
    assert_eq!(mcp_fingerprint(&a), mcp_fingerprint(&b));

    let mut c = a.clone();
    c.command = Some(vec!["other".into()]);
    assert_ne!(mcp_fingerprint(&a), mcp_fingerprint(&c));
}

#[test]
fn skill_sync_dry_run_symlink_plan() {
    with_temp_home(|home| {
        let agents = home.join(".agents/skills");
        let grok = home.join(".grok/skills");
        std::fs::create_dir_all(&agents).unwrap();
        std::fs::create_dir_all(&grok).unwrap();
        write_skill(&agents, "demo-skill", "# demo");

        let plan = plan_sync_skills(Scope::User, home, None, Some("demo-skill")).unwrap();
        assert!(
            plan.actions.iter().any(|a| matches!(
                a,
                SyncAction::SymlinkSkill { skill_key, agent: AgentKind::Grok, .. }
                    if skill_key == "demo-skill"
            )),
            "expected SymlinkSkill for grok, got {:?}",
            plan.actions
        );
    });
}

#[test]
fn skill_conflict_detected_when_hashes_differ() {
    with_temp_home(|home| {
        let agents = home.join(".agents/skills");
        let grok = home.join(".grok/skills");
        std::fs::create_dir_all(&agents).unwrap();
        std::fs::create_dir_all(&grok).unwrap();
        write_skill(&agents, "clash", "# version A");
        write_skill(&grok, "clash", "# version B different");

        let skills = scan_skills(Scope::User, home);
        let clash = skills.iter().find(|s| s.key == "clash").expect("clash skill");
        assert!(clash.mismatch, "expected mismatch");

        let plan = plan_sync_skills(Scope::User, home, None, Some("clash")).unwrap();
        assert!(
            plan.actions
                .iter()
                .any(|a| matches!(a, SyncAction::ConflictSkill { skill_key, .. } if skill_key == "clash")),
            "expected ConflictSkill, got {:?}",
            plan.actions
        );
    });
}

#[test]
fn skill_conflict_keep_source_overwrites() {
    with_temp_home(|home| {
        let agents = home.join(".agents/skills");
        let grok = home.join(".grok/skills");
        std::fs::create_dir_all(&agents).unwrap();
        std::fs::create_dir_all(&grok).unwrap();
        write_skill(&agents, "clash", "# version A");
        write_skill(&grok, "clash", "# version B different");

        let plan = plan_sync_skills(Scope::User, home, None, Some("clash")).unwrap();
        let decisions = ConflictDecisions::with_default(ConflictPolicy::KeepSource);
        let log = apply_plan(&plan, home, Scope::User, &decisions).unwrap();
        assert!(
            log.iter().any(|l| l.contains("conflict skill")),
            "log={log:?}"
        );

        // Grok should now be symlink to canonical
        let link = grok.join("clash");
        let meta = std::fs::symlink_metadata(&link).unwrap();
        assert!(meta.file_type().is_symlink(), "expected symlink at {link:?}");
        let body = std::fs::read_to_string(agents.join("clash/SKILL.md")).unwrap();
        assert!(body.contains("version A"));
    });
}

#[test]
fn mcp_hub_merge_preserves_target_env_secrets() {
    let tmp = tempfile::tempdir().unwrap();
    let hub = tmp.path().join("mcp.json");
    std::fs::write(
        &hub,
        r#"{"mcpServers":{"svc":{"type":"stdio","command":"old","env":{"SECRET":"keep-me","SHARED":"old"}}}}"#,
    )
    .unwrap();

    let mut norm = McpNormalized {
        transport: "stdio".into(),
        command: Some(vec!["new".into()]),
        url: None,
        args: None,
        enabled: None,
        env_keys: vec!["SHARED".into()],
        env: BTreeMap::from([("SHARED".into(), "new".into())]),
    };
    // source lacks SECRET
    upsert_mcp_json_hub(&hub, "svc", &norm).unwrap();
    let v: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&hub).unwrap()).unwrap();
    let env = &v["mcpServers"]["svc"]["env"];
    assert_eq!(env["SECRET"], "keep-me", "secret must be preserved");
    assert_eq!(env["SHARED"], "new", "shared key from source wins when present");
    assert_eq!(v["mcpServers"]["svc"]["command"], "new");

    // also verify source key inserted when target missing
    norm.env.insert("NEWKEY".into(), "x".into());
    upsert_mcp_json_hub(&hub, "svc", &norm).unwrap();
    let v2: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&hub).unwrap()).unwrap();
    assert_eq!(v2["mcpServers"]["svc"]["env"]["SECRET"], "keep-me");
    assert_eq!(v2["mcpServers"]["svc"]["env"]["NEWKEY"], "x");
}

#[test]
fn conflict_policy_parse() {
    assert_eq!(ConflictPolicy::parse("keep-source"), Some(ConflictPolicy::KeepSource));
    assert_eq!(ConflictPolicy::parse("keep-target"), Some(ConflictPolicy::KeepTarget));
    assert_eq!(ConflictPolicy::parse("skip"), Some(ConflictPolicy::Skip));
    assert_eq!(ConflictPolicy::parse("nope"), None);
}

#[test]
fn sync_plan_never_crosses_user_and_project_scopes() {
    with_temp_home(|home| {
        // User-level skill
        let agents = home.join(".agents/skills");
        std::fs::create_dir_all(&agents).unwrap();
        write_skill(&agents, "user-only", "# user");

        // Project repo with its own skill
        let proj_raw = home.join("proj");
        std::fs::create_dir_all(proj_raw.join(".agents/skills")).unwrap();
        std::fs::create_dir_all(proj_raw.join(".git")).unwrap();
        write_skill(&proj_raw.join(".agents/skills"), "proj-only", "# project");
        // macOS temp dirs often resolve via /private — canonicalize for prefix checks.
        let proj = proj_raw.canonicalize().unwrap();

        let user_plan = plan_sync_skills(Scope::User, &proj, None, None).unwrap();
        assert_eq!(user_plan.scope, "user");
        for a in &user_plan.actions {
            let paths: Vec<&std::path::Path> = match a {
                SyncAction::EnsureCanonicalCopy { from, to, .. } => vec![from, to],
                SyncAction::SymlinkSkill { link, target, .. } => vec![link, target],
                SyncAction::SkipSame { path, .. } => vec![path],
                SyncAction::ConflictSkill { paths, .. } => paths.iter().map(|p| p.as_path()).collect(),
                _ => vec![],
            };
            for path in paths {
                assert!(
                    !path.starts_with(&proj.join(".agents"))
                        && !path.starts_with(&proj.join(".grok"))
                        && !path.starts_with(&proj.join(".kiro")),
                    "user-scope action leaked into project path: {path:?} action={a:?}"
                );
            }
        }
        // User plan must not be planning the project-only skill
        assert!(
            !user_plan.actions.iter().any(|a| match a {
                SyncAction::EnsureCanonicalCopy { skill_key, .. }
                | SyncAction::SymlinkSkill { skill_key, .. }
                | SyncAction::SkipSame { skill_key, .. }
                | SyncAction::ConflictSkill { skill_key, .. } => skill_key == "proj-only",
                _ => false,
            }),
            "user plan must not include proj-only skill: {:?}",
            user_plan.actions
        );

        let proj_plan = plan_sync_skills(Scope::Project, &proj, None, None).unwrap();
        assert_eq!(proj_plan.scope, "project");
        for a in &proj_plan.actions {
            let paths: Vec<&std::path::Path> = match a {
                SyncAction::EnsureCanonicalCopy { from, to, .. } => vec![from, to],
                SyncAction::SymlinkSkill { link, target, .. } => vec![link, target],
                SyncAction::SkipSame { path, .. } => vec![path],
                SyncAction::ConflictSkill { paths, .. } => paths.iter().map(|p| p.as_path()).collect(),
                _ => vec![],
            };
            for path in paths {
                assert!(
                    path.starts_with(&proj),
                    "project-scope action escaped project root: {path:?} action={a:?}"
                );
            }
        }
        assert!(
            !proj_plan.actions.iter().any(|a| match a {
                SyncAction::EnsureCanonicalCopy { skill_key, .. }
                | SyncAction::SymlinkSkill { skill_key, .. }
                | SyncAction::SkipSame { skill_key, .. }
                | SyncAction::ConflictSkill { skill_key, .. } => skill_key == "user-only",
                _ => false,
            }),
            "project plan must not include user-only skill: {:?}",
            proj_plan.actions
        );

        // merge_plans must refuse to combine different scopes
        let mixed = merge_plans(user_plan.clone(), proj_plan.clone());
        assert_eq!(mixed.scope, "user");
        assert_eq!(mixed.actions.len(), user_plan.actions.len());
    });
}

#[test]
fn plan_sync_mcp_records_single_scope() {
    with_temp_home(|home| {
        let hub = home.join(".agents/mcp.json");
        std::fs::create_dir_all(hub.parent().unwrap()).unwrap();
        std::fs::write(
            &hub,
            r#"{"mcpServers":{"demo":{"type":"stdio","command":"echo"}}}"#,
        )
        .unwrap();
        let plan = plan_sync_mcp(Scope::User, home, None, Some("demo")).unwrap();
        assert_eq!(plan.scope, "user");
        for a in &plan.actions {
            if let SyncAction::EnsureMcpHub { hub, .. } = a {
                assert!(hub.ends_with(".agents/mcp.json"));
                assert!(!hub.ends_with(".mcp.json") || hub.to_string_lossy().contains(".agents"));
            }
            if let SyncAction::WriteMcpServer { path, .. } = a {
                let s = path.to_string_lossy();
                assert!(!s.contains("/proj/"), "user mcp write leaked: {s}");
            }
        }
    });
}


#[test]
fn skill_install_and_unlink() {
    with_temp_home(|home| {
        let src = home.join("src-skill");
        write_skill(&src, "fresh-skill", "# fresh");
        // install from the skill dir itself
        let skill_dir = src.join("fresh-skill");
        let (plan, log) =
            install_skill(Scope::User, home, skill_dir.to_str().unwrap(), None, false).unwrap();
        assert!(!plan.actions.is_empty());
        assert!(!log.is_empty());
        let canon = home.join(".agents/skills/fresh-skill/SKILL.md");
        assert!(canon.is_file(), "canonical missing");
        let grok = home.join(".grok/skills/fresh-skill");
        assert!(
            std::fs::symlink_metadata(&grok).unwrap().file_type().is_symlink(),
            "expected grok symlink"
        );

        // unlink only
        let (_p, log2) =
            remove_skill(Scope::User, home, "fresh-skill", None, false, false).unwrap();
        assert!(log2.iter().any(|l| l.contains("unlinked")));
        assert!(
            !grok.exists() && std::fs::symlink_metadata(&grok).is_err(),
            "grok link should be gone"
        );
        assert!(canon.is_file(), "canonical must remain after unlink");

        // purge
        let (_p, log3) =
            remove_skill(Scope::User, home, "fresh-skill", None, true, false).unwrap();
        assert!(log3.iter().any(|l| l.contains("purged")));
        assert!(!canon.exists());
    });
}

#[test]
fn mcp_add_and_remove() {
    with_temp_home(|home| {
        let norm = mcp_from_cli("stdio", Some("npx -y demo-mcp"), None, Some(true)).unwrap();
        let (_p, log) = add_mcp(Scope::User, home, "demo-mcp", norm, None, false).unwrap();
        assert!(!log.is_empty());
        let hub = home.join(".agents/mcp.json");
        let v: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(&hub).unwrap()).unwrap();
        assert!(v["mcpServers"].get("demo-mcp").is_some());

        let (_p2, log2) = remove_mcp(Scope::User, home, "demo-mcp", None, false).unwrap();
        assert!(log2.iter().any(|l| l.contains("hub remove")));
        let v2: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(&hub).unwrap()).unwrap();
        assert!(v2["mcpServers"].get("demo-mcp").is_none());
    });
}

#[test]
fn mcp_from_cli_url() {
    let n = mcp_from_cli("sse", None, Some("https://example.com/mcp"), None).unwrap();
    assert_eq!(n.transport, "sse");
    assert_eq!(n.url.as_deref(), Some("https://example.com/mcp"));
}
