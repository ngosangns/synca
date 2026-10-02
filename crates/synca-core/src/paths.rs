use std::path::{Path, PathBuf};

pub fn home_dir() -> PathBuf {
    dirs::home_dir().unwrap_or_else(|| PathBuf::from("/"))
}

/// Walk up from cwd looking for `.git`. Returns the directory containing `.git`.
pub fn project_root(cwd: &Path) -> Option<PathBuf> {
    let mut cur = cwd.canonicalize().unwrap_or_else(|_| cwd.to_path_buf());
    loop {
        if cur.join(".git").exists() {
            return Some(cur);
        }
        if !cur.pop() {
            return None;
        }
    }
}

pub fn hash_bytes(data: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let mut h = Sha256::new();
    h.update(data);
    hex::encode(h.finalize())
}

pub fn hash_file(path: &Path) -> std::io::Result<String> {
    let data = std::fs::read(path)?;
    Ok(hash_bytes(&data))
}

pub fn hash_skill_dir(dir: &Path) -> std::io::Result<String> {
    // Prefer SKILL.md; fall back to hashing sorted file contents.
    let skill_md = dir.join("SKILL.md");
    if skill_md.is_file() {
        return hash_file(&skill_md);
    }
    let mut parts: Vec<(String, Vec<u8>)> = Vec::new();
    if dir.is_dir() {
        for entry in walkdir::WalkDir::new(dir).into_iter().filter_map(|e| e.ok()) {
            if entry.file_type().is_file() {
                let rel = entry
                    .path()
                    .strip_prefix(dir)
                    .unwrap_or(entry.path())
                    .to_string_lossy()
                    .to_string();
                if let Ok(bytes) = std::fs::read(entry.path()) {
                    parts.push((rel, bytes));
                }
            }
        }
    }
    parts.sort_by(|a, b| a.0.cmp(&b.0));
    let mut buf = Vec::new();
    for (rel, bytes) in parts {
        buf.extend_from_slice(rel.as_bytes());
        buf.push(0);
        buf.extend_from_slice(&bytes);
        buf.push(0);
    }
    Ok(hash_bytes(&buf))
}
