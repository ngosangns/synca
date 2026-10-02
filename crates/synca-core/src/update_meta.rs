//! Constants for self-update (GitHub Releases).

pub const GITHUB_REPO: &str = "ngosangns/synca";
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Asset naming (must match `.github/workflows/release.yml` and `scripts/release-local.sh`):
/// - binary: `synca-v{VERSION}-darwin-arm64`
/// - checksum: `synca-v{VERSION}-darwin-arm64.sha256` (hex sha256, first field)

pub fn release_api_url() -> String {
    format!("https://api.github.com/repos/{GITHUB_REPO}/releases/latest")
}

pub fn install_bin_dir() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/share/synca/bin")
}

pub fn symlink_path() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/bin/synca")
}

pub fn expected_asset_name(version: &str) -> String {
    let ver = version.trim_start_matches('v');
    format!("synca-v{ver}-darwin-arm64")
}
