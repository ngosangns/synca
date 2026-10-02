use ast_core::update::{check_update, install_update};
use serde_json::json;

pub fn run_update(check_only: bool, json: bool, force: bool) -> anyhow::Result<()> {
    if check_only {
        let info = check_update()?;
        if json {
            println!(
                "{}",
                json!({
                    "ok": true,
                    "current": info.current,
                    "latest": info.latest,
                    "update_available": info.update_available,
                    "url": info.release_url,
                    "asset_name": info.asset_name,
                    "message": info.message,
                })
            );
        } else {
            println!("Current: {}", info.current);
            if let Some(ref l) = info.latest {
                println!("Latest:  {l}");
            }
            if let Some(ref u) = info.release_url {
                println!("Release: {u}");
            }
            println!("{}", info.message);
            if info.update_available {
                println!("Run: agent-skills-tui update");
            }
        }
        return Ok(());
    }

    let info = if force || check_update()?.update_available {
        install_update(force)?
    } else {
        check_update()?
    };

    if json {
        println!(
            "{}",
            json!({
                "ok": true,
                "current": info.current,
                "latest": info.latest,
                "update_available": info.update_available,
                "message": info.message,
                "path": ast_core::update_meta::symlink_path(),
            })
        );
    } else {
        println!("Current: {}", info.current);
        if let Some(ref l) = info.latest {
            println!("Latest:  {l}");
        }
        println!("{}", info.message);
    }
    Ok(())
}
