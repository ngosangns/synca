use std::hint::black_box;
use std::time::Instant;

use gpui::SharedString;

use crate::{
    Apply, Conflict, Entry, InventoryCache, Kind, ListSort, PROVIDERS, PlanView, Policy,
    SortColumn, Target, TreeItem, chosen_file, entry_matches, inventory_row_meta, inventory_rows,
    keys_for_kind, markdown_with_metadata, open_folders, relative_label, row_top, rows_in_view,
    shown_scope, tree_nodes, visible_entries, workspace_parts,
};

const ENTRIES: usize = 5_000;
const PROJECTS: usize = 200;
/// The viewport a cached frame slices rows for.
const VIEW_H: f32 = 800.;
const RUNS: usize = 21;

fn inventory(count: usize) -> Vec<Entry> {
    (0..count)
        .map(|index| {
            let kind = if index % 10 < 7 {
                Kind::Skill
            } else {
                Kind::Mcp
            };
            let noun = if kind == Kind::Skill { "skill" } else { "mcp" };
            let topic = ["Search", "Review", "Build", "Deploy", "Cache"][index % 5];
            let stale = index % 7 == 0;
            Entry {
                kind,
                key: format!("{noun}-{index:05}-{}", topic.to_lowercase()),
                title: format!("{topic} {noun} {index:05} for a large workspace"),
                description: format!("{topic} guidance across providers. ").repeat(16),
                paths: (0..3)
                    .map(|offset| {
                        let provider = PROVIDERS[(index + offset) % PROVIDERS.len()].0;
                        format!(
                            "/bench/projects/project-{:03}/.{provider}/{noun}s/{noun}-{index:05}",
                            index % PROJECTS
                        )
                    })
                    .collect(),
                links: (0..3)
                    .map(|offset| {
                        let provider = PROVIDERS[(index + offset) % PROVIDERS.len()].0;
                        crate::AgentLink {
                            agent: provider.to_string(),
                            path: format!(
                                "/bench/projects/project-{:03}/.{provider}/{noun}s/{noun}-{index:05}",
                                index % PROJECTS
                            ),
                            synced: Some(!stale || offset > 0),
                        }
                    })
                    .collect(),
                synced: Some(if stale { 2 } else { 3 }),
                tokens: Some(120 + index % 4_800),
                mismatch: stale,
                origin: if kind == Kind::Skill && index % 3 == 0 {
                    crate::engine::Origin::Remote
                } else {
                    crate::engine::Origin::Local
                },
                source: (index % 3 == 0).then(|| "vercel-labs/skills".to_string()),
                source_type: (index % 3 == 0).then(|| "github".to_string()),
            }
        })
        .collect()
}

fn document(bytes: usize) -> String {
    let mut text = "---\r\nname: benchmark-skill\r\ndescription: First line\r\n  continuation\r\nallowed-tools: Read, Search\r\n---\r\n".to_string();
    while text.len() < bytes {
        text.push_str("## Workspace workflow\r\n\r\nReview the **inventory**, `search` results and provider state.\r\n\r\n- Inspect entries\r\n- Compare plans\r\n\r\n```rust\r\nlet cached = false;\r\n```\r\n\r\n");
    }
    text.truncate(bytes);
    text
}

fn tree() -> Vec<TreeItem> {
    let mut items = vec![TreeItem {
        name: "SKILL.md".into(),
        path: "/bench/skill/SKILL.md".into(),
        dir: false,
        depth: 0,
    }];
    for folder in 0..9 {
        items.push(TreeItem {
            name: format!("references-{folder}"),
            path: format!("/bench/skill/references-{folder}"),
            dir: true,
            depth: 0,
        });
        for file in 0..8 {
            items.push(TreeItem {
                name: format!("guide-{file}.md"),
                path: format!("/bench/skill/references-{folder}/guide-{file}.md"),
                dir: false,
                depth: 1,
            });
        }
    }
    items.truncate(80);
    items
}

/// The row data a cached frame reads for the rows the viewport materializes.
fn sidebar_strings_cached(
    cache: &InventoryCache,
    rows: &[usize],
    build: std::ops::Range<usize>,
) -> usize {
    let mut built = 0;
    let std::ops::Range { start, end } = build;
    for &index in &rows[start..end] {
        black_box((
            cache.ids[index].clone(),
            cache.titles[index].clone(),
            cache.titles[index].clone(),
            cache.meta[index].clone(),
            cache.status[index].clone(),
            cache.mismatch[index],
        ));
        built += 1;
    }
    built
}

/// The strings the detail header builds for the selected entry, without the
/// deep clone the uncached frame paid.
fn selected_detail_strings(entry: &Entry) -> usize {
    let place = entry
        .paths
        .first()
        .map(|path| ns_ui::display_path(path))
        .unwrap_or_else(|| "not installed".into());
    black_box((
        entry.title.clone(),
        entry.title.clone(),
        entry.key.clone(),
        place,
        entry.description.clone(),
        entry.links.len(),
    ));
    1
}

fn project_strings(projects: &[String]) {
    let projects = black_box(projects.to_vec());
    for (index, path) in projects.into_iter().enumerate() {
        black_box((
            workspace_parts(&path),
            path.clone(),
            path.clone(),
            format!("ws-{index}"),
            format!("ws-rm-{index}"),
        ));
    }
}

/// The strings a borrowed frame builds per activity row; collapsed output is
/// no longer cloned.
fn activity_rows(activity: &[crate::engine::LogEntry]) -> usize {
    let mut built = 0;
    for entry in activity {
        black_box((
            format!(
                "{}  {}  exit {}",
                entry.status,
                shown_scope(&entry.scope),
                entry.exit
            ),
            entry.command.clone(),
            format!("act-{}", entry.id),
        ));
        built += 1;
    }
    built
}

/// The strings the plan screen builds per frame from the borrowed plan.
fn plan_rows(plan: &PlanView) -> usize {
    let mut built = 0;
    for (index, conflict) in plan.conflicts.iter().enumerate() {
        black_box((
            format!(
                "{} {}",
                if conflict.skill { "skill" } else { "mcp" },
                conflict.key
            ),
            conflict.key.clone(),
            format!("pol-{index}-{}", conflict.policy.as_str()),
        ));
        built += 1;
    }
    black_box(plan.title.clone());
    for (kind, count) in &plan.chips {
        black_box((kind.clone(), count.to_string()));
    }
    built
}

fn bench<T>(name: &str, mut run: impl FnMut() -> T) -> f64 {
    black_box(run());
    let mut samples = Vec::with_capacity(RUNS);
    for _ in 0..RUNS {
        let started = Instant::now();
        let value = run();
        samples.push(started.elapsed().as_secs_f64() * 1000.0);
        black_box(value);
    }
    samples.sort_by(f64::total_cmp);
    let median = samples[RUNS / 2];
    println!("BENCH {name}_ms={median:.6}");
    median
}

#[test]
#[ignore]
fn perf_bench_large() {
    let entries = inventory(ENTRIES);
    let projects: Vec<String> = (0..PROJECTS)
        .map(|index| format!("/bench/projects/team-{:02}/project-{index:03}", index % 20))
        .collect();
    let markdown = document(8_000);
    let long_markdown = document(256 * 1024);
    let items = tree();
    let plan = PlanView {
        notes: Vec::new(),
        title: "Synthetic sync preview".into(),
        output: "\u{1b}[33mconflict\u{1b}[0m skill entry across providers\n".repeat(4_000),
        conflicts: (0..500)
            .map(|index| Conflict {
                skill: index % 2 == 0,
                key: format!("entry-{index:05}"),
                policy: Policy::Skip,
            })
            .collect(),
        chips: vec![("conflict_skill".into(), 250), ("conflict_mcp".into(), 250)],
        apply: Apply::Sync {
            target: Target::All,
            key: None,
        },
    };
    let activity: Vec<crate::engine::LogEntry> = (0..400)
        .map(|index| crate::engine::LogEntry {
            id: index,
            ts: 1_792_886_400.0 + index as f64,
            scope: "user".into(),
            command: format!("synca sync all --dry-run --json entry-{index}"),
            status: "ok".into(),
            exit: 0,
            output: markdown.clone(),
        })
        .collect();
    eprintln!(
        "bench setup: entries={} skills={} mcp={} providers={} projects={} tree_items={} markdown_bytes={} stress_markdown_bytes={} conflicts={} activity={} runs={RUNS}",
        entries.len(),
        entries
            .iter()
            .filter(|entry| entry.kind == Kind::Skill)
            .count(),
        entries
            .iter()
            .filter(|entry| entry.kind == Kind::Mcp)
            .count(),
        PROVIDERS.len(),
        projects.len(),
        items.len(),
        markdown.len(),
        long_markdown.len(),
        plan.conflicts.len(),
        activity.len(),
    );

    let mut cache = InventoryCache::default();
    cache.rebuild_entries(&entries);
    cache.refilter("");
    // A keystroke's recompute: one refilter pass over the pre-lowercased keys.
    let groups = bench("inventory_groups_empty", || {
        cache.refilter("");
        cache.skills.len() + cache.mcps.len()
    });
    bench("inventory_groups_search", || {
        cache.refilter(" search ");
        cache.skills.len() + cache.mcps.len()
    });
    bench("inventory_groups_no_match", || {
        cache.refilter("no-such-entry");
        cache.skills.len() + cache.mcps.len()
    });
    // Cold recompute: everything a reload or a fresh window fills.
    bench("cache_refresh_cold", || {
        let mut fresh = InventoryCache::default();
        fresh.rebuild_entries(&entries);
        fresh.refilter("");
        fresh.skills.len() + fresh.mcps.len()
    });
    // A repaint reads the cached row strings for the rows in the viewport.
    cache.refilter("");
    let rows = bench("inventory_row_strings", || {
        sidebar_strings_cached(
            &cache,
            &cache.skills,
            rows_in_view(cache.skills.len(), ns_ui::SIDEBAR_SECTION_H, VIEW_H, 0.),
        )
    });
    // The popover panel is built only while open; this is its string prep.
    let workspaces = bench("project_row_strings", || project_strings(&projects));
    let selected = bench("selected_entry_clone", || {
        selected_detail_strings(&entries[0])
    });
    let nodes = bench("tree_nodes", || tree_nodes(&items));
    let file = bench("chosen_file", || chosen_file("/bench/skill", "", &items));
    let folders = bench("open_folders_twice", || {
        (
            open_folders("/bench/skill", "/bench/skill/SKILL.md", &items),
            open_folders("/bench/skill", "/bench/skill/SKILL.md", &items),
        )
    });
    bench("relative_file_label", || {
        relative_label("/bench/skill", "/bench/skill/SKILL.md")
    });
    let metadata = bench("markdown_metadata_8k", || markdown_with_metadata(&markdown));
    bench("markdown_metadata_256k_stress", || {
        markdown_with_metadata(&long_markdown)
    });
    bench("activity_clone", || activity_rows(&activity));
    bench("plan_clone", || plan_rows(&plan));
    // The plan screen strips once when a plan loads; a frame reads the cache.
    let mut plan_clean: Option<SharedString> = None;
    bench("plan_strip_ansi", || {
        let clean = plan_clean
            .get_or_insert_with(|| SharedString::from(ns_ui::strip_ansi(&plan.output)))
            .clone();
        black_box(clean);
    });
    bench("keyboard_visible_entries", || cache.visible());
    bench("keyboard_row_top", || cache.row_top(entries.len() - 1));

    // Workspace + skill-detail data subtotal, same pass composition as the
    // baseline. After caching the groups pass is the debounced recompute and
    // the row pass reads the cache for a viewport, not all 5,000 rows.
    let total = groups + rows + workspaces + selected + nodes + file + folders + metadata;
    println!("BENCH per_frame_total_ms={total:.6}");
    eprintln!(
        "per_frame_total_ms sums median data passes for an unfiltered workspace skill-detail frame; not wall-clock frame time. inventory_groups_* and cache_refresh_cold are recompute costs (reload or a settled keystroke); inventory_row_strings reads the cache for the rows one viewport shows. Keyboard, activity, plan and 256k stress timings are separate."
    );
}

#[test]
fn cached_groups_match_the_reference_for_every_query() {
    let mut entries = inventory(60);
    entries[0].title = "ÉCLAIR skill".into();
    entries[0].key = "unique-key".into();
    entries[7].title = "boundary\nneedle".into();
    let mut cache = InventoryCache::default();
    cache.rebuild_entries(&entries);
    for query in [
        "",
        "  ",
        "SKILL",
        " search ",
        "UNIQUE-KEY",
        "éclair",
        "missing",
        "skill\nx",
        "boundary\nneedle",
    ] {
        cache.refilter(&query.trim().to_lowercase());
        let (needle, skills, stale_skills) = inventory_rows(&entries, Kind::Skill, query);
        let (_, mcps, stale_mcps) = inventory_rows(&entries, Kind::Mcp, query);
        assert_eq!(cache.query_key, needle, "needle for {query:?}");
        assert_eq!(cache.skills, skills, "skills for {query:?}");
        assert_eq!(cache.mcps, mcps, "mcps for {query:?}");
        assert_eq!(
            cache.skills_note.as_ref().map(|note| note.to_string()),
            (stale_skills > 0).then(|| match stale_skills {
                1 => "1 out of sync".to_string(),
                stale => format!("{stale} out of sync"),
            }),
            "skills note for {query:?}"
        );
        assert_eq!(
            cache.mcps_note.as_ref().map(|note| note.to_string()),
            (stale_mcps > 0).then(|| match stale_mcps {
                1 => "1 out of sync".to_string(),
                stale => format!("{stale} out of sync"),
            }),
            "mcps note for {query:?}"
        );
        assert_eq!(cache.visible(), visible_entries(&entries, query));
        let mut targets: Vec<usize> = skills.iter().chain(&mcps).copied().take(7).collect();
        targets.push(entries.len() - 1);
        for target in targets {
            assert_eq!(
                cache.row_top(target),
                row_top(&entries, query, target),
                "row_top for {target} under {query:?}"
            );
        }
    }
}

#[test]
fn column_sort_reorders_each_group_and_keeps_token_totals() {
    let mut entries = inventory(20);
    entries[0].title = "zeta skill".into();
    entries[0].tokens = Some(10);
    entries[0].synced = Some(0);
    entries[1].title = "alpha skill".into();
    entries[1].tokens = None;
    entries[1].synced = None;
    let mut cache = InventoryCache::default();
    cache.rebuild_entries(&entries);
    cache.refilter("");
    let skill_sum: usize = cache
        .skills
        .iter()
        .filter_map(|&index| entries[index].tokens)
        .sum();
    let mcp_sum: usize = cache
        .mcps
        .iter()
        .filter_map(|&index| entries[index].tokens)
        .sum();
    assert!(skill_sum > 0 && mcp_sum > 0);
    assert_eq!(cache.skills_tokens, Some(skill_sum));
    assert_eq!(cache.mcps_tokens, Some(mcp_sum));

    cache.sort = ListSort {
        column: SortColumn::Name,
        ascending: true,
    };
    cache.refilter("");
    let names: Vec<_> = cache
        .skills
        .iter()
        .map(|&index| entries[index].title.to_lowercase())
        .collect::<Vec<_>>();
    let mut ordered = names.clone();
    ordered.sort();
    assert_eq!(names, ordered);
    assert_eq!(cache.skills.first().copied(), Some(1));
    assert_eq!(cache.mcps_tokens, Some(mcp_sum));

    cache.sort = ListSort {
        column: SortColumn::Tokens,
        ascending: false,
    };
    cache.refilter("");
    let tokens: Vec<_> = cache
        .skills
        .iter()
        .map(|&index| entries[index].tokens)
        .collect::<Vec<_>>();
    assert!(tokens.last().is_some_and(Option::is_none));
    for pair in tokens.windows(2) {
        if let (Some(left), Some(right)) = (pair[0], pair[1]) {
            assert!(left >= right);
        }
    }

    cache.sort = ListSort {
        column: SortColumn::Sync,
        ascending: true,
    };
    cache.refilter("");
    let synced: Vec<_> = cache
        .skills
        .iter()
        .map(|&index| entries[index].synced)
        .collect::<Vec<_>>();
    let missing = synced.iter().position(Option::is_none).unwrap();
    assert!(synced[missing..].iter().all(Option::is_none));
    let known: Vec<_> = synced.iter().copied().flatten().collect();
    let mut rising = known.clone();
    rising.sort();
    assert_eq!(known, rising);
}

#[test]
fn an_empty_inventory_keeps_the_cached_groups_empty() {
    let mut cache = InventoryCache::default();
    cache.rebuild_entries(&[]);
    cache.refilter("x");
    assert_eq!(
        (cache.query_key.as_str(), &cache.skills, &cache.mcps),
        ("x", &Vec::new(), &Vec::new())
    );
    assert!(cache.skills_note.is_none() && cache.mcps_note.is_none());
    assert_eq!(cache.visible(), Vec::<usize>::new());
    assert_eq!(cache.row_top(0), row_top(&[], "x", 0));
}

#[test]
fn cached_row_strings_match_the_per_frame_originals() {
    let entries = inventory(50);
    let mut cache = InventoryCache::default();
    cache.rebuild_entries(&entries);
    for (index, entry) in entries.iter().enumerate() {
        let status = if entry.mismatch {
            "Out of sync"
        } else {
            "In sync"
        };
        assert_eq!(cache.ids[index].as_ref(), format!("row-{index}"));
        assert_eq!(cache.titles[index].as_ref(), entry.title);
        assert_eq!(cache.meta[index], inventory_row_meta(entry, status));
        assert_eq!(cache.status[index].as_ref(), status);
        assert_eq!(cache.mismatch[index], entry.mismatch);
        assert_eq!(cache.kind[index], entry.kind);
        assert_eq!(
            cache.titles_lc[index],
            entry.title.to_lowercase(),
            "prelowercased title"
        );
        assert_eq!(
            cache.keys_lc[index],
            entry.key.to_lowercase(),
            "prelowercased key"
        );
    }
}

#[test]
fn prelowercased_matching_matches_the_per_entry_original() {
    let entries = inventory(40);
    let (titles_lc, keys_lc) = InventoryCache::prepare_search(&entries);
    for needle in ["", "skill", "SEARCH", "éclair", "a\nb", "7", "  x  "] {
        let needle = needle.trim().to_lowercase();
        for ((entry, title), key) in entries.iter().zip(&titles_lc).zip(&keys_lc) {
            assert_eq!(
                title.contains(&needle) || key.contains(&needle),
                entry_matches(entry, &needle),
                "{needle:?} against {}",
                entry.key
            );
        }
    }
}

#[test]
fn the_viewport_slice_covers_the_scrolled_window() {
    assert_eq!(rows_in_view(0, 24., VIEW_H, 0.), 0..0);
    // Unknown viewport height (before the first layout): every row.
    assert_eq!(rows_in_view(100, 24., 0., 9_000.), 0..100);
    assert_eq!(rows_in_view(100, 24., VIEW_H, 0.), 0..28);
    assert_eq!(rows_in_view(100, 24., VIEW_H, 3_200.), 96..100);
    // Far past the end: nothing to build.
    assert!(rows_in_view(100, 24., VIEW_H, 100_000.).is_empty());
    // A group that starts below the viewport builds none of its rows.
    assert_eq!(rows_in_view(100, 24. + 32. * 50., VIEW_H, 0.), 0..0);
}

#[test]
fn inventory_groups_preserve_matching_order_and_stale_counts() {
    let mut entries = inventory(20);
    entries[0].title = "ÉCLAIR skill".into();
    entries[0].key = "unique-key".into();
    for query in [
        "",
        "  ",
        "SKILL",
        " search ",
        "UNIQUE-KEY",
        "éclair",
        "missing",
    ] {
        let (needle, skills, stale_skills) = inventory_rows(&entries, Kind::Skill, query);
        let (_, mcps, stale_mcps) = inventory_rows(&entries, Kind::Mcp, query);
        let expected: Vec<usize> = [Kind::Skill, Kind::Mcp]
            .into_iter()
            .flat_map(|kind| {
                entries
                    .iter()
                    .enumerate()
                    .filter_map(|(index, entry)| {
                        (entry.kind == kind
                            && (needle.is_empty()
                                || entry.title.to_lowercase().contains(&needle)
                                || entry.key.to_lowercase().contains(&needle)))
                        .then_some(index)
                    })
                    .collect::<Vec<_>>()
            })
            .collect();
        assert_eq!(
            stale_skills,
            skills
                .iter()
                .filter(|&&index| entries[index].mismatch)
                .count()
        );
        assert_eq!(
            stale_mcps,
            mcps.iter()
                .filter(|&&index| entries[index].mismatch)
                .count()
        );
        assert_eq!([skills, mcps].concat(), expected);
        assert_eq!(visible_entries(&entries, query), expected);
    }
    assert!(inventory_rows(&[], Kind::Skill, "x").1.is_empty());
    assert_eq!(inventory_rows(&entries, Kind::Skill, "éclair").1, vec![0]);
    assert_eq!(
        inventory_rows(&entries, Kind::Skill, "UNIQUE-KEY").1,
        vec![0]
    );
}

#[test]
fn inventory_meta_preserves_empty_and_populated_paths() {
    let mut entries = inventory(1);
    let entry = &mut entries[0];
    entry.paths.clear();
    entry.links.clear();
    entry.synced = None;
    entry.tokens = None;
    assert_eq!(
        inventory_row_meta(entry, "In sync").as_ref(),
        "In sync · No agent links"
    );
    entry.paths.push("/bench/skill".into());
    assert!(
        inventory_row_meta(entry, "Out of sync")
            .as_ref()
            .ends_with("· /bench/skill")
    );
}

#[test]
fn bulk_keys_filter_to_the_row_kind_in_sidebar_order() {
    let entries = inventory(20);
    // Kinds repeat per decade: 0-6 and 10-16 are skills, 7-9 and 17-19 MCP.
    let bulk = [0usize, 1, 7, 8]
        .into_iter()
        .collect::<std::collections::BTreeSet<_>>();
    let skills = keys_for_kind(&entries, &bulk, Kind::Skill);
    assert_eq!(skills, [0usize, 1].map(|index| entries[index].key.clone()));
    let mcps = keys_for_kind(&entries, &bulk, Kind::Mcp);
    assert_eq!(mcps, [7usize, 8].map(|index| entries[index].key.clone()));
    assert!(keys_for_kind(&entries, &std::collections::BTreeSet::new(), Kind::Skill).is_empty());
}
