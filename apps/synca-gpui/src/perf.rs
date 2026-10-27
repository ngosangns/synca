use std::cell::RefCell;
use std::time::{Duration, Instant};

use gpui::{AppContext as _, Context, Pixels, Point, point, px};
use serde_json::{Value, json};

use crate::{InstallTab, Kind, Panel, Route, Scope, SyncaWindow};

pub(crate) const SCENARIOS: &[&str] = &[
    "idle-overview",
    "tab-switch",
    "inventory-scroll",
    "search-typing",
    "select-item",
    "projects-view",
];

#[derive(Default)]
struct Probe {
    running: bool,
    recording: bool,
    renders: u64,
    render_us: Vec<u64>,
}

thread_local! {
    static PROBE: RefCell<Probe> = RefCell::new(Probe::default());
}

pub(crate) struct Clock(Instant);

impl Drop for Clock {
    fn drop(&mut self) {
        let micros = u64::try_from(self.0.elapsed().as_micros()).unwrap_or(u64::MAX);
        PROBE.with(|probe| {
            let mut probe = probe.borrow_mut();
            probe.renders += 1;
            if probe.recording {
                probe.render_us.push(micros);
            }
        });
    }
}

pub(crate) fn clock() -> Clock {
    Clock(Instant::now())
}

struct RunGuard;

impl Drop for RunGuard {
    fn drop(&mut self) {
        PROBE.with(|probe| {
            let mut probe = probe.borrow_mut();
            probe.running = false;
            probe.recording = false;
            probe.render_us.clear();
        });
    }
}

fn renders() -> u64 {
    PROBE.with(|probe| probe.borrow().renders)
}

fn record(on: bool) -> Vec<u64> {
    PROBE.with(|probe| {
        let mut probe = probe.borrow_mut();
        probe.recording = on;
        std::mem::take(&mut probe.render_us)
    })
}

fn percentile(values: &mut [f64], fraction: f64) -> f64 {
    if values.is_empty() {
        return 0.0;
    }
    values.sort_by(f64::total_cmp);
    values[((values.len() as f64 * fraction) as usize).min(values.len() - 1)]
}

fn round(value: f64) -> f64 {
    (value * 100.0).round() / 100.0
}

pub(crate) fn arguments(request: &Value) -> Result<(&str, usize), String> {
    let label = match request.get("label") {
        None => "run",
        Some(value) => value.as_str().ok_or("label must be a string")?,
    };
    if label.is_empty()
        || label.len() > 80
        || !label
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
    {
        return Err("label must be 1–80 letters, digits, - or _".into());
    }
    let frames = match request.get("frames") {
        None => 60,
        Some(value) => value
            .as_u64()
            .ok_or("frames must be a nonnegative integer")?,
    };
    Ok((label, frames.clamp(1, 1000) as usize))
}

struct SavedUi {
    selected: Option<usize>,
    route: Route,
    panel: Panel,
    scope: Scope,
    active_project: Option<String>,
    onboarding: Option<InstallTab>,
    query: String,
    file_path: String,
    file_preview: String,
    scroll: Point<Pixels>,
}

impl SavedUi {
    fn of(window: &SyncaWindow) -> Self {
        Self {
            selected: window.selected,
            route: window.route,
            panel: window.panel,
            scope: window.scope,
            active_project: window.active_project.clone(),
            onboarding: window.onboarding,
            query: window.query.clone(),
            file_path: window.file_path.clone(),
            file_preview: window.file_preview.clone(),
            scroll: window.list_scroll.offset(),
        }
    }

    fn restore(self, window: &mut SyncaWindow, cx: &mut Context<SyncaWindow>) {
        window.selected = self.selected;
        window.route = self.route;
        window.panel = self.panel;
        window.scope = self.scope;
        window.active_project = self.active_project;
        window.onboarding = self.onboarding;
        set_query(window, &self.query, cx);
        window.file_path = self.file_path;
        window.file_preview = self.file_preview;
        window.list_scroll.set_offset(self.scroll);
        cx.notify();
    }
}

fn set_query(window: &mut SyncaWindow, query: &str, cx: &mut Context<SyncaWindow>) {
    // Perf steps and state restores apply the search at once, ahead of the
    // input's own debounce.
    window.apply_query(query, cx);
}

fn overview(window: &mut SyncaWindow, cx: &mut Context<SyncaWindow>) {
    window.route = Route::List;
    window.panel = Panel::Closed;
    window.scope = Scope::User;
    window.onboarding = None;
    window.selected = None;
    window.file_path.clear();
    window.file_preview.clear();
    window.list_scroll.set_offset(point(px(0.), px(0.)));
    set_query(window, "", cx);
}

fn step(
    window: &mut SyncaWindow,
    name: &str,
    frame: usize,
    frames: usize,
    cx: &mut Context<SyncaWindow>,
) {
    if frame == 0 {
        overview(window, cx);
    }
    match name {
        "tab-switch" => {
            window.onboarding = None;
            window.panel = Panel::Closed;
            window.route = Route::List;
            window.scope = Scope::User;
            match frame % 8 {
                1 => window.scope = Scope::Project,
                2 => window.onboarding = Some(InstallTab::Skill),
                3 => window.onboarding = Some(InstallTab::Mcp),
                4 => window.panel = Panel::Activity,
                5 => window.panel = Panel::Settings,
                6 => window.route = Route::Plan,
                7 => window.route = Route::Update,
                _ => {}
            }
        }
        "inventory-scroll" => {
            let height =
                window.entries.len() as f32 * ns_ui::SIDEBAR_ROW_H + 2.0 * ns_ui::SIDEBAR_SECTION_H;
            window
                .list_scroll
                .set_offset(point(px(0.), px(-(frame as f32 * 480.0 % height.max(1.0)))));
        }
        "search-typing" => {
            const WORD: &str = "skill";
            set_query(window, &WORD[..frame % (WORD.len() + 1)], cx);
        }
        "select-item" => {
            if !window.entries.is_empty() {
                window.selected = Some(frame * window.entries.len() / frames);
                window.file_path.clear();
                window.file_preview.clear();
            }
        }
        "projects-view" => {
            window.scope = Scope::Project;
            window.active_project = window
                .projects
                .get(frame % window.projects.len().max(1))
                .cloned();
        }
        _ => {}
    }
}

pub(crate) fn start(
    window: &mut SyncaWindow,
    label: &str,
    frames: usize,
    cx: &mut Context<SyncaWindow>,
) -> Result<Value, String> {
    let Some(handle) = cx.windows().into_iter().next() else {
        return Err("no window".into());
    };
    if window.busy || window.entries.is_empty() {
        return Err("wait for a nonempty inventory to finish loading".into());
    }
    if window.ask.is_some() || window.dev_key.is_some() {
        return Err("dismiss the confirmation and wait for queued keys before measuring".into());
    }
    if PROBE.with(|probe| std::mem::replace(&mut probe.borrow_mut().running, true)) {
        return Err("a perf run is already active".into());
    }
    let guard = RunGuard;
    let saved = SavedUi::of(window);
    let skills = window
        .entries
        .iter()
        .filter(|entry| entry.kind == Kind::Skill)
        .count();
    let entries = window.entries.len();
    let projects = window.projects.len();
    let plan_conflicts = window.plan.as_ref().map(|plan| plan.conflicts.len());
    let path = std::env::temp_dir().join(format!("synca-perf-{label}.json"));
    let label = label.to_string();
    let out = path.clone();
    overview(window, cx);
    cx.spawn(async move |this, cx| {
        let _guard = guard;
        cx.update(|app| {
            let _ = app.update_window(handle, |_, window, cx| window.draw(cx).clear(cx));
        });
        let idle_from = renders();
        let idle_started = Instant::now();
        cx.background_executor().timer(Duration::from_secs(3)).await;
        let idle_renders = renders() - idle_from;
        let idle_ms = idle_started.elapsed().as_secs_f64() * 1000.0;
        let mut results = Vec::new();
        let mut error = None;
        for name in SCENARIOS {
            let mut frame_ms = Vec::with_capacity(frames);
            record(true);
            let started = Instant::now();
            for frame in 0..frames {
                let at = Instant::now();
                if let Err(err) = this.update(cx, |this, cx| step(this, name, frame, frames, cx)) {
                    error = Some(err.to_string());
                    break;
                }
                let draw = cx.update(|app| {
                    app.update_window(handle, |_, window, cx| window.draw(cx).clear(cx))
                });
                if let Err(err) = draw {
                    error = Some(err.to_string());
                    break;
                }
                frame_ms.push(at.elapsed().as_secs_f64() * 1000.0);
                cx.background_executor()
                    .timer(Duration::from_millis(1))
                    .await;
            }
            let elapsed = started.elapsed().as_secs_f64() * 1000.0;
            let mut render_us: Vec<f64> = record(false).into_iter().map(|us| us as f64).collect();
            let total: f64 = frame_ms.iter().sum();
            results.push(json!({
                "name": name,
                "frames": frame_ms.len(),
                "elapsed_ms": round(elapsed),
                "fps": round(frame_ms.len() as f64 * 1000.0 / total.max(0.001)),
                "frame_ms_median": round(percentile(&mut frame_ms, 0.5)),
                "frame_ms_p95": round(percentile(&mut frame_ms, 0.95)),
                "render_samples": render_us.len(),
                "render_us_median": round(percentile(&mut render_us, 0.5)),
            }));
            if error.is_some() {
                break;
            }
        }
        this.update(cx, |this, cx| saved.restore(this, cx)).ok();
        let report = json!({
            "label": label,
            "profile": if cfg!(debug_assertions) { "dev" } else { "release" },
            "entries": entries,
            "skills": skills,
            "mcp": entries - skills,
            "projects": projects,
            "plan_conflicts": plan_conflicts,
            "idle": {
                "ms": round(idle_ms),
                "renders": idle_renders,
                "renders_per_s": round(idle_renders as f64 * 1000.0 / idle_ms.max(1.0)),
            },
            "scenarios": results,
            "error": error,
        });
        if let Err(err) = std::fs::write(&out, serde_json::to_string_pretty(&report).unwrap()) {
            eprintln!("cannot write perf report {}: {err}", out.display());
        }
    })
    .detach();
    Ok(json!({"started": true, "path": path.display().to_string(), "frames": frames}))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn argument_defaults_and_bounds() {
        assert_eq!(arguments(&json!({})).unwrap(), ("run", 60));
        assert_eq!(
            arguments(&json!({"label": "before-1", "frames": 0})).unwrap(),
            ("before-1", 1)
        );
        assert_eq!(arguments(&json!({"frames": u64::MAX})).unwrap().1, 1000);
        for request in [
            json!({"frames": -1}),
            json!({"frames": 1.5}),
            json!({"label": "../escape"}),
            json!({"label": ""}),
            json!({"label": 4}),
        ] {
            assert!(arguments(&request).is_err());
        }
    }

    #[test]
    fn percentiles_match_the_reference() {
        assert_eq!(percentile(&mut [], 0.5), 0.0);
        assert_eq!(percentile(&mut [7.], 0.95), 7.0);
        assert_eq!(percentile(&mut [4., 2., 3., 1.], 0.5), 3.0);
        let mut samples: Vec<f64> = (0..100).rev().map(f64::from).collect();
        assert_eq!(percentile(&mut samples, 0.95), 95.0);
    }

    #[test]
    fn clock_counts_idle_and_records_only_when_enabled() {
        let _guard = RunGuard;
        record(false);
        let before = renders();
        drop(clock());
        assert_eq!(renders(), before + 1);
        assert!(record(true).is_empty());
        drop(clock());
        assert_eq!(record(false).len(), 1);
        assert!(record(false).is_empty());
    }
}
