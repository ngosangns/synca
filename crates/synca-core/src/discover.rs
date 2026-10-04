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

/// Slice of YAML between the opening `---` and closing `\n---`.
pub(crate) fn frontmatter_block(text: &str) -> Option<&str> {
    if !text.starts_with("---") {
        return None;
    }
    let rest = text.strip_prefix("---")?;
    let end = rest.find("\n---")?;
    Some(&rest[..end])
}

/// Extract `name` from YAML frontmatter in SKILL.md, if present.
pub fn skill_frontmatter_name(skill_md: &Path) -> Option<String> {
    let text = std::fs::read_to_string(skill_md).ok()?;
    let fm = frontmatter_block(&text)?;
    name_re()
        .captures(fm)
        .and_then(|c| c.get(1).map(|m| m.as_str().trim().to_string()))
}

/// Extract `description` from YAML frontmatter in SKILL.md.
/// Supports a single-line value (quoted or bare) and simple `|` / `>` block scalars.
pub fn skill_frontmatter_description(skill_md: &Path) -> Option<String> {
    let text = std::fs::read_to_string(skill_md).ok()?;
    let fm = frontmatter_block(&text)?;
    parse_description_field(fm)
}

fn parse_description_field(fm: &str) -> Option<String> {
    let mut lines = fm.lines().peekable();
    while let Some(line) = lines.next() {
        let trimmed = line.trim_start();
        let Some(rest) = trimmed.strip_prefix("description:") else {
            continue;
        };
        let rest = rest.trim();
        if rest.is_empty() || matches!(rest, "|" | ">" | "|-" | ">-" | "|+" | ">+") {
            let mut parts: Vec<&str> = Vec::new();
            while let Some(&cont) = lines.peek() {
                if cont.starts_with(' ') || cont.starts_with('\t') {
                    parts.push(cont.trim());
                    lines.next();
                } else if cont.trim().is_empty() {
                    lines.next();
                    break;
                } else {
                    break;
                }
            }
            let joined = parts.join(" ");
            let joined = joined.trim();
            return if joined.is_empty() {
                None
            } else {
                Some(joined.to_string())
            };
        }
        let v = rest
            .trim_matches(|c| c == '"' || c == '\'')
            .trim();
        return if v.is_empty() {
            None
        } else {
            Some(v.to_string())
        };
    }
    None
}

/// Best-effort description for a skill directory (reads `SKILL.md`).
pub fn skill_description(dir: &Path) -> Option<String> {
    let md = dir.join("SKILL.md");
    if md.is_file() {
        skill_frontmatter_description(&md)
    } else {
        None
    }
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

#[cfg(test)]
mod frontmatter_tests {
    use super::*;
    use std::fs;

    #[test]
    fn description_single_line_and_block() {
        let tmp = tempfile::tempdir().unwrap();
        let md = tmp.path().join("SKILL.md");
        fs::write(
            &md,
            "---\nname: n\ndescription: Hello world skill\n---\n# body\n",
        )
        .unwrap();
        assert_eq!(
            skill_frontmatter_description(&md).as_deref(),
            Some("Hello world skill")
        );

        fs::write(
            &md,
            "---\nname: n\ndescription: |\n  line one\n  line two\n---\n",
        )
        .unwrap();
        assert_eq!(
            skill_frontmatter_description(&md).as_deref(),
            Some("line one line two")
        );

        fs::write(&md, "---\nname: n\n---\n# no desc\n").unwrap();
        assert_eq!(skill_frontmatter_description(&md), None);
    }
}
