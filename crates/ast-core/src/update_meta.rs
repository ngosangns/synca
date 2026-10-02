//! Constants for self-update (GitHub Releases).

pub const GITHUB_REPO: &str = "ngosangns/agent-skills-tui";
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

pub fn release_api_url() -> String {
    format!("https://api.github.com/repos/{GITHUB_REPO}/releases/latest")
}

pub fn install_bin_dir() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/share/agent-skills-tui/bin")
}

pub fn symlink_path() -> std::path::PathBuf {
    crate::paths::home_dir().join(".local/bin/agent-skills-tui")
}
