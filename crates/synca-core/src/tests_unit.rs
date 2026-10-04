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
    assert_eq!(a.command.as_ref().unwrap(), &vec!["python".to_string()]);
    assert_eq!(
        a.args.as_ref().unwrap(),
        &vec!["-m".to_string(), "server".to_string()]
    );
    assert_eq!(a.transport, "stdio");

    let u = json_to_normalized(&json!({"url": "https://example.com/sse"}));
    assert_eq!(u.transport, "sse");
    assert_eq!(u.url.as_deref(), Some("https://example.com/sse"));

    let bare = json_to_normalized(&json!({"url": "https://example.com/mcp"}));
    assert_eq!(bare.transport, "http");
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
    assert_eq!(oc.transport, "http");
    assert_eq!(oc.enabled, Some(true));
}

#[test]
fn mcp_transport_aliases_collapse_to_stdio_or_http() {
    use crate::scan::canonical_transport as ct;
    assert_eq!(ct(Some("local"), None, true), "stdio");
    assert_eq!(ct(Some("remote"), Some("https://x/mcp"), false), "http");
    assert_eq!(ct(Some("streamable-http"), Some("https://x/mcp"), false), "http");
    assert_eq!(ct(Some("sse"), Some("https://x/mcp"), false), "http", "legacy sse upgrades to http");
    assert_eq!(ct(Some("sse"), Some("https://x/sse"), false), "sse", "real /sse endpoint is kept");
    assert_eq!(ct(None, None, false), "stdio");
}

#[test]
fn mcp_command_array_splits_into_command_and_args_for_stable_fingerprint() {
    let oc = json_to_normalized(&json!({"type": "local", "command": ["uvx", "--from", "pkg", "serve"]}));
    let std = json_to_normalized(&json!({"type": "stdio", "command": "uvx", "args": ["--from", "pkg", "serve"]}));
    assert_eq!(oc.command.as_ref().unwrap(), &vec!["uvx".to_string()]);
    assert_eq!(oc.args, std.args);
    assert_eq!(mcp_fingerprint(&oc), mcp_fingerprint(&std));
}

#[test]
fn mcp_from_cli_maps_remote_and_local_aliases() {
    let r = mcp_from_cli("remote", None, Some("https://x/mcp"), None).unwrap();
    assert_eq!(r.transport, "http");
    let l = mcp_from_cli("local", Some("uvx tool serve"), None, None).unwrap();
    assert_eq!(l.transport, "stdio");
    assert_eq!(l.args.as_ref().unwrap(), &vec!["tool".to_string(), "serve".to_string()]);
    assert!(mcp_from_cli("bogus", None, None, None).is_err());
}

#[test]
fn mcp_opencode_writer_uses_local_remote_schema_and_keeps_env() {
    let tmp = tempfile::tempdir().unwrap();
    let path = tmp.path().join("opencode.json");
    std::fs::write(&path, r#"{"mcp":{"svc":{"type":"local","command":["old"],"environment":{"SECRET":"keep"}}}}"#).unwrap();
    let stdio = McpNormalized {
        transport: "stdio".into(),
        command: Some(vec!["uvx".into()]),
        url: None,
        args: Some(vec!["serve".into()]),
        enabled: Some(true),
        env_keys: vec![],
        env: BTreeMap::new(),
    };
    crate::sync::write_mcp_to_agent(&path, AgentKind::OpenCode, "svc", &stdio).unwrap();
    let v: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(v["mcp"]["svc"]["type"], "local");
    assert_eq!(v["mcp"]["svc"]["command"], json!(["uvx", "serve"]));
    assert_eq!(v["mcp"]["svc"]["environment"]["SECRET"], "keep");

    let http = McpNormalized {
        transport: "http".into(),
        command: None,
        url: Some("https://x/mcp".into()),
        args: None,
        enabled: Some(true),
        env_keys: vec![],
        env: BTreeMap::new(),
    };
    crate::sync::write_mcp_to_agent(&path, AgentKind::OpenCode, "web", &http).unwrap();
    let v: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(v["mcp"]["web"]["type"], "remote");
    assert_eq!(v["mcp"]["web"]["url"], "https://x/mcp");
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
                SyncAction::RepairSkillFrontmatter { path, .. } => vec![path],
                SyncAction::CollapseSkillAlias { from, to, .. } => vec![from, to],
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
                SyncAction::RepairSkillFrontmatter { path, .. } => vec![path],
                SyncAction::CollapseSkillAlias { from, to, .. } => vec![from, to],
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
    assert_eq!(n.transport, "http");
    assert_eq!(n.url.as_deref(), Some("https://example.com/mcp"));
}

#[test]
fn mcp_writers_keep_args_when_command_is_single_token() {
    let tmp = tempfile::tempdir().unwrap();
    let norm = McpNormalized {
        transport: "stdio".into(),
        command: Some(vec!["uvx".into()]),
        url: None,
        args: Some(vec!["--from".into(), "pkg".into(), "serve".into()]),
        enabled: Some(true),
        env_keys: vec![],
        env: BTreeMap::new(),
    };
    let json_path = tmp.path().join("mcp.json");
    crate::sync::write_mcp_to_agent(&json_path, AgentKind::Pi, "svc", &norm).unwrap();
    let v: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&json_path).unwrap()).unwrap();
    assert_eq!(v["mcpServers"]["svc"]["args"], json!(["--from", "pkg", "serve"]));

    let toml_path = tmp.path().join("config.toml");
    crate::sync::write_mcp_to_agent(&toml_path, AgentKind::Grok, "svc", &norm).unwrap();
    let t: toml::Value = toml::from_str(&std::fs::read_to_string(&toml_path).unwrap()).unwrap();
    assert_eq!(t["mcp_servers"]["svc"]["args"].as_array().unwrap().len(), 3);
}

fn write_raw_skill(dir: &Path, folder: &str, frontmatter: &str, body: &str) {
    let skill = dir.join(folder);
    std::fs::create_dir_all(&skill).unwrap();
    std::fs::write(
        skill.join("SKILL.md"),
        format!("---\n{frontmatter}\n---\n{body}\n"),
    )
    .unwrap();
}

#[test]
fn pi_sync_repairs_names_collapses_aliases_and_identical_project_shadows() {
    with_temp_home(|home| {
        let agents = home.join(".agents/skills");
        std::fs::create_dir_all(&agents).unwrap();
        write_raw_skill(
            &agents,
            "make-bot-ui",
            "name: Make Bot UI\ndescription: Triggers: webhook ui",
            "# bot",
        );
        let same = "name: design-swiftui-interfaces\ndescription: SwiftUI interfaces";
        write_raw_skill(&agents, "design-swiftui-interfaces", same, "# swift");
        write_raw_skill(&agents, "swiftui-interface-design", same, "# swift");
        write_raw_skill(&agents, "but", "name: but\ndescription: git butler", "# but");

        let proj_raw = home.join("viclass");
        std::fs::create_dir_all(proj_raw.join(".git")).unwrap();
        let proj_skills = proj_raw.join(".agents/skills");
        write_raw_skill(&proj_skills, "but", "name: but\ndescription: git butler", "# but");
        write_raw_skill(
            &proj_skills,
            "viclass-only",
            "name: viclass-only\ndescription: local",
            "# local",
        );
        // Same name, different tree: must stay a real project directory.
        write_raw_skill(
            &proj_skills,
            "make-bot-ui",
            "name: make-bot-ui\ndescription: project specific",
            "# different",
        );
        let proj = proj_raw.canonicalize().unwrap();

        let plan = plan_sync_skills(Scope::User, &proj, None, None).unwrap();
        assert!(
            plan.actions.iter().any(|a| matches!(
                a,
                SyncAction::RepairSkillFrontmatter { skill_key, .. } if skill_key == "make-bot-ui"
            )),
            "missing name repair: {:?}",
            plan.actions
        );
        assert!(
            plan.actions.iter().any(|a| matches!(
                a,
                SyncAction::CollapseSkillAlias { skill_key, .. } if skill_key == "design-swiftui-interfaces"
            )),
            "missing alias collapse: {:?}",
            plan.actions
        );
        assert!(
            plan.actions.iter().any(|a| matches!(
                a,
                SyncAction::CollapseSkillAlias { from, skill_key, .. }
                    if skill_key == "but" && from.ends_with("viclass/.agents/skills/but")
                        || (skill_key == "but" && from.components().any(|c| c.as_os_str() == "viclass"))
            )),
            "missing project shadow relink: {:?}",
            plan.actions
        );

        let decisions = ConflictDecisions::with_default(ConflictPolicy::Skip);
        apply_plan(&plan, &proj, Scope::User, &decisions).unwrap();

        let repaired = std::fs::read_to_string(agents.join("make-bot-ui/SKILL.md")).unwrap();
        assert!(repaired.contains("name: make-bot-ui\n"), "{repaired}");
        assert!(
            repaired.contains("description: \"Triggers: webhook ui\"\n"),
            "{repaired}"
        );

        let alias = agents.join("swiftui-interface-design");
        assert!(
            std::fs::symlink_metadata(&alias).unwrap().file_type().is_symlink(),
            "duplicate skill dir should be a symlink"
        );
        assert_eq!(
            alias.canonicalize().unwrap(),
            agents.join("design-swiftui-interfaces").canonicalize().unwrap()
        );

        let proj_but = proj.join(".agents/skills/but");
        assert!(
            std::fs::symlink_metadata(&proj_but).unwrap().file_type().is_symlink(),
            "identical project shadow should alias the user skill"
        );
        assert_eq!(
            proj_but.canonicalize().unwrap(),
            agents.join("but").canonicalize().unwrap()
        );
        let proj_only = proj.join(".agents/skills/viclass-only");
        assert!(
            !std::fs::symlink_metadata(&proj_only).unwrap().file_type().is_symlink(),
            "project-only skill must stay a real directory"
        );
        let proj_bot = proj.join(".agents/skills/make-bot-ui");
        assert!(
            !std::fs::symlink_metadata(&proj_bot).unwrap().file_type().is_symlink(),
            "different project tree must not be replaced"
        );
    });
}
