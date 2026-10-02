//! Frontmatter + skill folder discovery helpers.

use regex::Regex;
use std::path::Path;
use std::sync::OnceLock;

fn name_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| {
        // name: foo  OR  name: "foo"  OR  name: 'foo'
        Regex::new(r#"(?m)^name:\s*["']?([^"'\n]+)["']?\s*$"#).unwrap()
    })
}

/// Extract `name` from YAML frontmatter in SKILL.md, if present.
pub fn skill_frontmatter_name(skill_md: &Path) -> Option<String> {
    let text = std::fs::read_to_string(skill_md).ok()?;
    if !text.starts_with("---") {
        return None;
    }
    let rest = text.strip_prefix("---")?;
    let end = rest.find("\n---")?;
    let fm = &rest[..end];
    name_re()
        .captures(fm)
        .and_then(|c| c.get(1).map(|m| m.as_str().trim().to_string()))
}

pub fn skill_display_name(dir: &Path) -> String {
    let folder = dir
        .file_name()
        .map(|s| s.to_string_lossy().to_string())
        .unwrap_or_else(|| "unknown".into());
    let md = dir.join("SKILL.md");
    if md.is_file() {
        if let Some(n) = skill_frontmatter_name(&md) {
            return n;
        }
    }
    folder
}
