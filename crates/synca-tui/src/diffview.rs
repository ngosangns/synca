//! Diff / mismatch helpers for the detail pane and conflict overlay.

use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use std::collections::BTreeMap;
use std::path::Path;
use synca_core::models::{
    AgentKind, McpEntry, McpNormalized, McpPresence, SkillEntry, SkillPresence,
};

pub fn short_hash(h: &str) -> String {
    h.chars().take(12).collect()
}

/// Canonical / hub presence used as keep-source (a).
pub fn skill_source<'a>(entry: &'a SkillEntry) -> Option<&'a SkillPresence> {
    entry
        .presence
        .iter()
        .find(|p| p.agent == AgentKind::Agents)
        .or_else(|| entry.presence.first())
}

pub fn mcp_source<'a>(entry: &'a McpEntry) -> Option<&'a McpPresence> {
    entry
        .presence
        .iter()
        .find(|p| p.agent == AgentKind::Agents)
        .or_else(|| entry.presence.first())
}

/// First non-source presence with a different hash/fingerprint (keep-target candidate).
pub fn skill_target<'a>(entry: &'a SkillEntry) -> Option<&'a SkillPresence> {
    let src = skill_source(entry)?;
    entry
        .presence
        .iter()
        .find(|p| p.content_hash != src.content_hash)
        .or_else(|| {
            entry
                .presence
                .iter()
                .find(|p| p.agent != AgentKind::Agents && !std::ptr::eq(*p, src))
        })
}

pub fn mcp_target<'a>(entry: &'a McpEntry) -> Option<&'a McpPresence> {
    let src = mcp_source(entry)?;
    entry
        .presence
        .iter()
        .find(|p| p.fingerprint != src.fingerprint)
        .or_else(|| {
            entry
                .presence
                .iter()
                .find(|p| p.agent != AgentKind::Agents && !std::ptr::eq(*p, src))
        })
}

fn style_title() -> Style {
    Style::default()
        .fg(Color::Red)
        .add_modifier(Modifier::BOLD)
}

fn style_label() -> Style {
    Style::default()
        .fg(Color::Yellow)
        .add_modifier(Modifier::BOLD)
}

fn style_name() -> Style {
    Style::default()
        .fg(Color::Cyan)
        .add_modifier(Modifier::BOLD)
}

fn style_add() -> Style {
    Style::default().fg(Color::Green)
}

fn style_del() -> Style {
    Style::default().fg(Color::Red)
}

fn style_meta() -> Style {
    Style::default().fg(Color::DarkGray)
}

fn style_path() -> Style {
    Style::default().fg(Color::White)
}

pub fn push_skill_mismatch(lines: &mut Vec<Line<'_>>, entry: &SkillEntry) {
    if !entry.mismatch {
        return;
    }
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled(
        "⚠ CONTENT MISMATCH — choose keep-source (a) / keep-target (b) when syncing",
        style_title(),
    )));

    let mut by_hash: BTreeMap<&str, Vec<&SkillPresence>> = BTreeMap::new();
    for p in &entry.presence {
        by_hash.entry(p.content_hash.as_str()).or_default().push(p);
    }

    let src = skill_source(entry);
    let tgt = skill_target(entry);

    if let Some(src) = src {
        lines.push(Line::from(vec![
            Span::styled("SOURCE (a / keep-source)", style_label()),
            Span::raw("  "),
            Span::styled(src.agent.as_str().to_string(), style_name()),
            Span::styled(
                format!("  hash={}", short_hash(&src.content_hash)),
                style_meta(),
            ),
        ]));
        lines.push(Line::from(vec![
            Span::raw("  path: "),
            Span::styled(src.path.display().to_string(), style_path()),
        ]));
        if src.is_symlink {
            if let Some(ref t) = src.symlink_target {
                lines.push(Line::from(vec![
                    Span::raw("  link→ "),
                    Span::styled(t.display().to_string(), style_meta()),
                ]));
            }
        }
    }

    if let Some(tgt) = tgt {
        lines.push(Line::from(vec![
            Span::styled("TARGET (b / keep-target)", style_label()),
            Span::raw("  "),
            Span::styled(tgt.agent.as_str().to_string(), style_name()),
            Span::styled(
                format!("  hash={}", short_hash(&tgt.content_hash)),
                style_meta(),
            ),
        ]));
        lines.push(Line::from(vec![
            Span::raw("  path: "),
            Span::styled(tgt.path.display().to_string(), style_path()),
        ]));
        if tgt.is_symlink {
            if let Some(ref t) = tgt.symlink_target {
                lines.push(Line::from(vec![
                    Span::raw("  link→ "),
                    Span::styled(t.display().to_string(), style_meta()),
                ]));
            }
        }
    }

    lines.push(Line::from(Span::styled(
        "Variants by hash:",
        Style::default().add_modifier(Modifier::BOLD),
    )));
    for (hash, group) in &by_hash {
        let agents: Vec<&str> = group.iter().map(|p| p.agent.as_str()).collect();
        let mark = match (src, tgt) {
            (Some(s), _) if s.content_hash == *hash => " [source]",
            (_, Some(t)) if t.content_hash == *hash => " [target]",
            _ => "",
        };
        lines.push(Line::from(vec![
            Span::styled(format!("  · {}", short_hash(hash)), Style::default().fg(Color::Magenta)),
            Span::styled(mark.to_string(), style_meta()),
            Span::raw(format!("  agents=[{}]", agents.join(", "))),
        ]));
        for p in group.iter().take(4) {
            lines.push(Line::from(vec![
                Span::raw("      "),
                Span::styled(p.agent.as_str().to_string(), style_name()),
                Span::raw("  "),
                Span::styled(p.path.display().to_string(), style_path()),
            ]));
        }
        if group.len() > 4 {
            lines.push(Line::from(format!("      … +{} more", group.len() - 4)));
        }
    }

    if let (Some(src), Some(tgt)) = (src, tgt) {
        push_skill_md_diff(lines, &src.path, &tgt.path);
    }
}

fn resolve_skill_md(dir: &Path) -> Option<std::path::PathBuf> {
    let real = dir.canonicalize().unwrap_or_else(|_| dir.to_path_buf());
    let md = real.join("SKILL.md");
    if md.is_file() {
        Some(md)
    } else {
        None
    }
}

fn push_skill_md_diff(lines: &mut Vec<Line<'_>>, src_dir: &Path, tgt_dir: &Path) {
    let (Some(src_md), Some(tgt_md)) = (resolve_skill_md(src_dir), resolve_skill_md(tgt_dir)) else {
        lines.push(Line::from(Span::styled(
            "(SKILL.md missing on one side — cannot show text diff)",
            style_meta(),
        )));
        return;
    };
    let src_text = std::fs::read_to_string(&src_md).unwrap_or_default();
    let tgt_text = std::fs::read_to_string(&tgt_md).unwrap_or_default();
    if src_text == tgt_text {
        lines.push(Line::from(Span::styled(
            "SKILL.md text identical (hash differs from other files in tree)",
            style_meta(),
        )));
        return;
    }
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled(
        "SKILL.md unified diff (source − / target +):",
        Style::default().add_modifier(Modifier::BOLD),
    )));
    lines.push(Line::from(Span::styled(
        format!("--- {}", src_md.display()),
        style_del(),
    )));
    lines.push(Line::from(Span::styled(
        format!("+++ {}", tgt_md.display()),
        style_add(),
    )));
    for line in unified_diff_hunks(&src_text, &tgt_text, 40) {
        lines.push(line);
    }
}

pub fn push_mcp_mismatch(lines: &mut Vec<Line<'_>>, entry: &McpEntry) {
    if !entry.mismatch {
        return;
    }
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled(
        "⚠ CONFIG MISMATCH — choose keep-source (a) / keep-target (b) when syncing",
        style_title(),
    )));

    let mut by_fp: BTreeMap<&str, Vec<&McpPresence>> = BTreeMap::new();
    for p in &entry.presence {
        by_fp.entry(p.fingerprint.as_str()).or_default().push(p);
    }

    let src = mcp_source(entry);
    let tgt = mcp_target(entry);

    if let Some(src) = src {
        lines.push(Line::from(vec![
            Span::styled("SOURCE (a / keep-source)", style_label()),
            Span::raw("  "),
            Span::styled(src.agent.as_str().to_string(), style_name()),
            Span::styled(
                format!("  fp={}", short_hash(&src.fingerprint)),
                style_meta(),
            ),
        ]));
        lines.push(Line::from(vec![
            Span::raw("  path: "),
            Span::styled(src.path.display().to_string(), style_path()),
        ]));
        push_mcp_norm_summary(lines, &src.normalized, "  ");
    }

    if let Some(tgt) = tgt {
        lines.push(Line::from(vec![
            Span::styled("TARGET (b / keep-target)", style_label()),
            Span::raw("  "),
            Span::styled(tgt.agent.as_str().to_string(), style_name()),
            Span::styled(
                format!("  fp={}", short_hash(&tgt.fingerprint)),
                style_meta(),
            ),
        ]));
        lines.push(Line::from(vec![
            Span::raw("  path: "),
            Span::styled(tgt.path.display().to_string(), style_path()),
        ]));
        push_mcp_norm_summary(lines, &tgt.normalized, "  ");
    }

    lines.push(Line::from(Span::styled(
        "Variants by fingerprint:",
        Style::default().add_modifier(Modifier::BOLD),
    )));
    for (fp, group) in &by_fp {
        let agents: Vec<&str> = group.iter().map(|p| p.agent.as_str()).collect();
        let mark = match (src, tgt) {
            (Some(s), _) if s.fingerprint == *fp => " [source]",
            (_, Some(t)) if t.fingerprint == *fp => " [target]",
            _ => "",
        };
        lines.push(Line::from(vec![
            Span::styled(format!("  · {}", short_hash(fp)), Style::default().fg(Color::Magenta)),
            Span::styled(mark.to_string(), style_meta()),
            Span::raw(format!("  agents=[{}]", agents.join(", "))),
        ]));
        for p in group.iter().take(4) {
            lines.push(Line::from(vec![
                Span::raw("      "),
                Span::styled(p.agent.as_str().to_string(), style_name()),
                Span::raw("  "),
                Span::styled(p.path.display().to_string(), style_path()),
            ]));
        }
    }

    if let (Some(src), Some(tgt)) = (src, tgt) {
        push_mcp_field_diff(lines, &src.normalized, &tgt.normalized);
    }
}

fn push_mcp_norm_summary(lines: &mut Vec<Line<'_>>, n: &McpNormalized, indent: &str) {
    lines.push(Line::from(format!(
        "{indent}transport={}  enabled={}",
        n.transport,
        n.enabled
            .map(|b| b.to_string())
            .unwrap_or_else(|| "-".into())
    )));
    if let Some(ref cmd) = n.command {
        let mut parts = cmd.clone();
        if let Some(ref args) = n.args {
            parts.extend(args.iter().cloned());
        }
        lines.push(Line::from(format!("{indent}command: {}", parts.join(" "))));
    }
    if let Some(ref url) = n.url {
        lines.push(Line::from(format!("{indent}url: {url}")));
    }
    if !n.env_keys.is_empty() {
        lines.push(Line::from(format!(
            "{indent}env keys: {}",
            n.env_keys.join(", ")
        )));
    }
}

fn push_mcp_field_diff(lines: &mut Vec<Line<'_>>, src: &McpNormalized, tgt: &McpNormalized) {
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled(
        "Field diff (source → target):",
        Style::default().add_modifier(Modifier::BOLD),
    )));
    push_field_change(lines, "transport", Some(&src.transport), Some(&tgt.transport));
    let src_cmd = flatten_cmd(src);
    let tgt_cmd = flatten_cmd(tgt);
    push_field_change(
        lines,
        "command",
        src_cmd.as_deref(),
        tgt_cmd.as_deref(),
    );
    push_field_change(lines, "url", src.url.as_deref(), tgt.url.as_deref());
    let src_en = src.enabled.map(|b| b.to_string());
    let tgt_en = tgt.enabled.map(|b| b.to_string());
    push_field_change(
        lines,
        "enabled",
        src_en.as_deref(),
        tgt_en.as_deref(),
    );
    let src_env = if src.env_keys.is_empty() {
        None
    } else {
        Some(src.env_keys.join(", "))
    };
    let tgt_env = if tgt.env_keys.is_empty() {
        None
    } else {
        Some(tgt.env_keys.join(", "))
    };
    push_field_change(
        lines,
        "env_keys",
        src_env.as_deref(),
        tgt_env.as_deref(),
    );
}

fn flatten_cmd(n: &McpNormalized) -> Option<String> {
    let cmd = n.command.as_ref()?;
    let mut parts = cmd.clone();
    if let Some(ref args) = n.args {
        parts.extend(args.iter().cloned());
    }
    Some(parts.join(" "))
}

fn push_field_change(
    lines: &mut Vec<Line<'_>>,
    field: &str,
    src: Option<&str>,
    tgt: Option<&str>,
) {
    let s = src.unwrap_or("(none)");
    let t = tgt.unwrap_or("(none)");
    if s == t {
        lines.push(Line::from(vec![
            Span::styled(format!("  {field}: "), style_meta()),
            Span::styled("same", style_meta()),
            Span::raw(format!(" ({s})")),
        ]));
    } else {
        lines.push(Line::from(vec![
            Span::styled(format!("  {field}:"), Style::default().add_modifier(Modifier::BOLD)),
        ]));
        lines.push(Line::from(vec![
            Span::styled(format!("    − {s}"), style_del()),
        ]));
        lines.push(Line::from(vec![
            Span::styled(format!("    + {t}"), style_add()),
        ]));
    }
}

/// Simple line-oriented unified hunks (no Myers). Enough context for TUI.
fn unified_diff_hunks<'a>(a: &str, b: &str, max_lines: usize) -> Vec<Line<'a>> {
    let a_lines: Vec<&str> = a.lines().collect();
    let b_lines: Vec<&str> = b.lines().collect();
    let mut out: Vec<Line<'a>> = Vec::new();
    let mut i = 0usize;
    let mut j = 0usize;
    while (i < a_lines.len() || j < b_lines.len()) && out.len() < max_lines {
        if i < a_lines.len() && j < b_lines.len() && a_lines[i] == b_lines[j] {
            // skip equal runs; show a short context marker occasionally
            let mut run = 0usize;
            while i + run < a_lines.len()
                && j + run < b_lines.len()
                && a_lines[i + run] == b_lines[j + run]
            {
                run += 1;
            }
            if run > 0 && out.is_empty() {
                // leading equals — skip
            } else if run > 3 {
                out.push(Line::from(Span::styled(
                    format!("  … {run} identical lines …"),
                    style_meta(),
                )));
            } else {
                for k in 0..run {
                    let text = a_lines[i + k].to_string();
                    out.push(Line::from(Span::styled(format!("  {text}"), style_meta())));
                    if out.len() >= max_lines {
                        break;
                    }
                }
            }
            i += run;
            j += run;
            continue;
        }
        // Prefer showing a deletion then insertion when both differ.
        if i < a_lines.len()
            && (j >= b_lines.len()
                || !b_lines[j..].iter().take(8).any(|l| *l == a_lines[i]))
        {
            let text = a_lines[i].to_string();
            out.push(Line::from(Span::styled(format!("− {text}"), style_del())));
            i += 1;
            continue;
        }
        if j < b_lines.len()
            && (i >= a_lines.len()
                || !a_lines[i..].iter().take(8).any(|l| *l == b_lines[j]))
        {
            let text = b_lines[j].to_string();
            out.push(Line::from(Span::styled(format!("+ {text}"), style_add())));
            j += 1;
            continue;
        }
        // Both sides have a near match later — emit replace pair.
        if i < a_lines.len() {
            let text = a_lines[i].to_string();
            out.push(Line::from(Span::styled(format!("− {text}"), style_del())));
            i += 1;
        }
        if j < b_lines.len() && out.len() < max_lines {
            let text = b_lines[j].to_string();
            out.push(Line::from(Span::styled(format!("+ {text}"), style_add())));
            j += 1;
        }
    }
    if i < a_lines.len() || j < b_lines.len() {
        out.push(Line::from(Span::styled(
            format!(
                "  … truncated ({} src / {} tgt lines left)",
                a_lines.len().saturating_sub(i),
                b_lines.len().saturating_sub(j)
            ),
            style_meta(),
        )));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn short_hash_truncates() {
        assert_eq!(short_hash("abcdefghijklmnop"), "abcdefghijkl");
        assert_eq!(short_hash("abc"), "abc");
    }

    #[test]
    fn unified_diff_shows_change() {
        let lines = unified_diff_hunks("a\nb\nc\n", "a\nB\nc\n", 20);
        let rendered: String = lines
            .iter()
            .map(|l| {
                l.spans
                    .iter()
                    .map(|s| s.content.as_ref())
                    .collect::<String>()
            })
            .collect::<Vec<_>>()
            .join("\n");
        assert!(rendered.contains('−') || rendered.contains('-'));
        assert!(rendered.contains('+'));
    }
}
