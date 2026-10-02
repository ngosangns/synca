//! Constants for self-update (GitHub Releases).

pub const GITHUB_REPO: &str = "ngosangns/agent-skills-tui";
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Asset naming (must match `.github/workflows/release.yml` and `scripts/release-local.sh`):
/// - binary: `agent-skills-tui-v{VERSION}-darwin-arm64`
/// - checksum: `agent-skills-tui-v{VERSION}-darwin-arm64.sha256` (hex sha256, first field)

pub fn release_api_url() -> String {
    format!("https://api.github.com/repos/{GITHUB_REPO}/releases/latest")
}

pub fn install_bin_dir() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/share/agent-skills-tui/bin")
}

pub fn symlink_path() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/bin/agent-skills-tui")
}

pub fn expected_asset_name(version: &str) -> String {
    let ver = version.trim_start_matches('v');
    format!("agent-skills-tui-v{ver}-darwin-arm64")
}
