use anyhow::Context;
use ast_core::update_meta::{install_bin_dir, release_api_url, symlink_path, GITHUB_REPO, VERSION};
use serde::Deserialize;
use serde_json::json;
use std::io::Write;

#[derive(Debug, Deserialize)]
struct Release {
    tag_name: String,
    assets: Vec<Asset>,
    html_url: Option<String>,
}

#[derive(Debug, Deserialize)]
struct Asset {
    name: String,
    browser_download_url: String,
    size: Option<u64>,
}

pub fn run_update(check_only: bool, json: bool, force: bool) -> anyhow::Result<()> {
    let client = reqwest::blocking::Client::builder()
        .user_agent(format!("agent-skills-tui/{VERSION}"))
        .build()?;

    let url = release_api_url();
    let resp = client.get(&url).send();
    let release = match resp {
        Ok(r) if r.status().as_u16() == 404 => {
            let msg = format!("no releases yet for {GITHUB_REPO}");
            if json {
                println!(
                    "{}",
                    json!({
                        "ok": true,
                        "current": VERSION,
                        "latest": null,
                        "update_available": false,
                        "message": msg,
                    })
                );
            } else {
                println!("Current: {VERSION}");
                println!("{msg}");
            }
            return Ok(());
        }
        Ok(r) => {
            if !r.status().is_success() {
                let status = r.status();
                let body = r.text().unwrap_or_default();
                anyhow::bail!("GitHub API {status}: {body}");
            }
            r.json::<Release>()?
        }
        Err(e) => {
            if json {
                println!(
                    "{}",
                    json!({
                        "ok": false,
                        "current": VERSION,
                        "error": e.to_string(),
                    })
                );
                return Ok(());
            }
            return Err(e.into());
        }
    };

    let latest = release.tag_name.trim_start_matches('v').to_string();
    let update_available = latest != VERSION || force;

    if json && (check_only || !update_available) {
        println!(
            "{}",
            json!({
                "ok": true,
                "current": VERSION,
                "latest": latest,
                "update_available": latest != VERSION,
                "url": release.html_url,
            })
        );
        return Ok(());
    }

    if !json {
        println!("Current: {VERSION}");
        println!("Latest:  {latest}");
        if let Some(ref u) = release.html_url {
            println!("Release: {u}");
        }
    }

    if check_only {
        if latest == VERSION {
            println!("Already up to date.");
        } else {
            println!("Update available. Run: agent-skills-tui update");
        }
        return Ok(());
    }

    if latest == VERSION && !force {
        println!("Already up to date.");
        return Ok(());
    }

    // Find asset
    let asset_name = format!("agent-skills-tui-v{latest}-darwin-arm64");
    let asset = release
        .assets
        .iter()
        .find(|a| a.name == asset_name || a.name == format!("{asset_name}.tar.gz"))
        .with_context(|| format!("no asset named {asset_name} (or .tar.gz) in release"))?;

    let bin_dir = install_bin_dir();
    std::fs::create_dir_all(&bin_dir)?;
    let versioned = bin_dir.join(format!("agent-skills-tui-{latest}"));

    println!("Downloading {} ...", asset.name);
    let bytes = client
        .get(&asset.browser_download_url)
        .send()?
        .error_for_status()?
        .bytes()?;
    if let Some(sz) = asset.size {
        if bytes.len() as u64 != sz {
            anyhow::bail!("size mismatch: expected {sz}, got {}", bytes.len());
        }
    }

    // If tar.gz, extract; else write raw binary
    if asset.name.ends_with(".tar.gz") {
        anyhow::bail!("tar.gz extract not implemented in MVP; publish raw binary asset");
    } else {
        let mut f = std::fs::File::create(&versioned)?;
        f.write_all(&bytes)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&versioned, std::fs::Permissions::from_mode(0o755))?;
        }
    }

    let link = symlink_path();
    if let Some(parent) = link.parent() {
        std::fs::create_dir_all(parent)?;
    }
    if link.exists() || std::fs::symlink_metadata(&link).is_ok() {
        std::fs::remove_file(&link).ok();
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::symlink;
        symlink(&versioned, &link)?;
    }
    println!("Installed {} -> {}", link.display(), versioned.display());
    if json {
        println!(
            "{}",
            json!({
                "ok": true,
                "installed": latest,
                "path": link,
            })
        );
    }
    Ok(())
}
