//! Self-update via GitHub Releases.

use crate::update_meta::{
    expected_asset_name, install_bin_dir, release_api_url, symlink_path, GITHUB_REPO, VERSION,
};

use anyhow::Context;
use serde::Deserialize;
use std::io::Write;

#[derive(Debug, Clone)]
pub struct UpdateInfo {
    pub current: String,
    pub latest: Option<String>,
    pub update_available: bool,
    pub release_url: Option<String>,
    pub asset_name: Option<String>,
    pub asset_url: Option<String>,
    pub sha256_url: Option<String>,
    pub message: String,
}

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
    #[allow(dead_code)]
    size: Option<u64>,
}

fn client() -> anyhow::Result<reqwest::blocking::Client> {
    Ok(reqwest::blocking::Client::builder()
        .user_agent(format!("agent-skills-tui/{VERSION}"))
        .build()?)
}

pub fn check_update() -> anyhow::Result<UpdateInfo> {
    let client = client()?;
    let url = release_api_url();
    let resp = match client.get(&url).send() {
        Ok(r) => r,
        Err(e) => {
            return Ok(UpdateInfo {
                current: VERSION.into(),
                latest: None,
                update_available: false,
                release_url: None,
                asset_name: None,
                asset_url: None,
                sha256_url: None,
                message: format!("network error: {e}"),
            });
        }
    };

    if resp.status().as_u16() == 404 {
        return Ok(UpdateInfo {
            current: VERSION.into(),
            latest: None,
            update_available: false,
            release_url: None,
            asset_name: None,
            asset_url: None,
            sha256_url: None,
            message: format!("no releases yet for {GITHUB_REPO}"),
        });
    }
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().unwrap_or_default();
        anyhow::bail!("GitHub API {status}: {body}");
    }

    let release: Release = resp.json()?;
    let latest = release.tag_name.trim_start_matches('v').to_string();
    let asset_name = expected_asset_name(&latest);
    let asset = release
        .assets
        .iter()
        .find(|a| a.name == asset_name || a.name == format!("{asset_name}.tar.gz"));
    let sha = release
        .assets
        .iter()
        .find(|a| a.name == format!("{asset_name}.sha256") || a.name == format!("{asset_name}.sha256sum"));

    let update_available = latest != VERSION;
    let message = if update_available {
        format!("update available: {VERSION} → {latest}")
    } else {
        format!("already up to date ({VERSION})")
    };

    Ok(UpdateInfo {
        current: VERSION.into(),
        latest: Some(latest),
        update_available,
        release_url: release.html_url,
        asset_name: asset.map(|a| a.name.clone()),
        asset_url: asset.map(|a| a.browser_download_url.clone()),
        sha256_url: sha.map(|a| a.browser_download_url.clone()),
        message,
    })
}

/// Download and install the latest release binary (or `force` reinstall).
pub fn install_update(force: bool) -> anyhow::Result<UpdateInfo> {
    let mut info = check_update()?;
    if !info.update_available && !force {
        return Ok(info);
    }
    let latest = info
        .latest
        .clone()
        .context("no latest version from GitHub")?;
    let asset_url = info
        .asset_url
        .clone()
        .with_context(|| {
            format!(
                "no asset named {} in release (publish darwin-arm64 binary)",
                expected_asset_name(&latest)
            )
        })?;
    let asset_name = info.asset_name.clone().unwrap_or_else(|| expected_asset_name(&latest));

    let client = client()?;
    let bytes = client
        .get(&asset_url)
        .send()?
        .error_for_status()?
        .bytes()?;

    if let Some(ref sha_url) = info.sha256_url {
        if let Ok(sha_text) = client.get(sha_url).send().and_then(|r| r.error_for_status()).and_then(|r| r.text()) {
            let expected = sha_text
                .split_whitespace()
                .next()
                .unwrap_or("")
                .trim()
                .to_ascii_lowercase();
            if !expected.is_empty() {
                let actual = crate::paths::hash_bytes(&bytes);
                if actual != expected {
                    anyhow::bail!("sha256 mismatch: expected {expected}, got {actual}");
                }
            }
        }
    }

    if asset_name.ends_with(".tar.gz") {
        anyhow::bail!("tar.gz assets not supported; publish raw binary named {}", expected_asset_name(&latest));
    }

    let bin_dir = install_bin_dir();
    std::fs::create_dir_all(&bin_dir)?;
    let versioned = bin_dir.join(format!("agent-skills-tui-{latest}"));
    {
        let mut f = std::fs::File::create(&versioned)?;
        f.write_all(&bytes)?;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&versioned, std::fs::Permissions::from_mode(0o755))?;
    }

    let link = symlink_path();
    if let Some(parent) = link.parent() {
        std::fs::create_dir_all(parent)?;
    }
    if link.exists() || std::fs::symlink_metadata(&link).is_ok() {
        let _ = std::fs::remove_file(&link);
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::symlink;
        symlink(&versioned, &link)?;
    }

    info.message = format!("installed {} -> {}", link.display(), versioned.display());
    info.update_available = false;
    Ok(info)
}
