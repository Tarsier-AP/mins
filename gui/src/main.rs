// mins-gui: a small GPUI frontend that drives the mins binary through
// `--events` and `--describe`. It never changes engine behaviour; every run is
// a normal CLI run with the flags the user picked.

mod store;
mod surface;

use std::collections::BTreeMap;
use std::fs::File;
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::process::{Child, Command};
use std::time::Duration;

use gpui::prelude::*;
use gpui::{
    div, px, rgb, size, AnyElement, App, Application, Bounds, ClickEvent, ClipboardItem, Context,
    Div, IntoElement, ParentElement, Render, SharedString, Stateful, StatefulInteractiveElement,
    Styled, Timer, Window, WindowBounds, WindowOptions,
};
use store::State;
use surface::{FlagDesc, Surface};

const BAR_W: f32 = 820.0;

struct Row {
    name: String,
    status: String,
    detail: String,
    output: String,
}

#[derive(Clone, Copy, PartialEq)]
enum Tab {
    Queue,
    Options,
}

struct MinsApp {
    files: Vec<PathBuf>,
    rows: Vec<Row>,
    surface: Option<Surface>,
    values: BTreeMap<String, String>,
    tab: Tab,
    running: bool,
    done: usize,
    total: usize,
    failures: usize,
    log: Vec<String>,
    summary: String,
    child: Option<Child>,
    events: PathBuf,
    offset: u64,
    state_path: Option<PathBuf>,
}

impl MinsApp {
    fn new() -> Self {
        let state_path = store::default_path();
        let state = state_path.as_deref().map(State::load).unwrap_or_default();
        Self::with_state(surface::describe(&mins_bin()), state, state_path)
    }

    fn with_state(surface: Option<Surface>, state: State, state_path: Option<PathBuf>) -> Self {
        let events =
            std::env::temp_dir().join(format!("mins-gui-{}.ndjson", std::process::id()));
        let mut values = state.values;
        prune_values(&mut values, surface.as_ref());
        let interrupted = state.running;
        let rows = state
            .files
            .iter()
            .map(|f| if interrupted { interrupted_row(f) } else { queued_row(f) })
            .collect();
        let summary = if interrupted {
            format!("{} item(s) restored from an interrupted run", state.files.len())
        } else {
            String::new()
        };
        Self {
            files: state.files,
            rows,
            surface,
            values,
            tab: Tab::Queue,
            running: false,
            done: 0,
            total: 0,
            failures: 0,
            log: Vec::new(),
            summary,
            child: None,
            events,
            offset: 0,
            state_path,
        }
    }

    fn persist(&self) {
        if let Some(path) = &self.state_path {
            State {
                files: self.files.clone(),
                values: self.values.clone(),
                running: self.running,
            }
            .save(path);
        }
    }

    /// A plain mins command line: harness flags, then only the options the user
    /// actually changed, so an untouched GUI run equals a bare CLI run.
    fn argv(&self) -> Vec<std::ffi::OsString> {
        let mut args: Vec<std::ffi::OsString> = vec![
            "--no-tui".into(),
            "--events".into(),
            self.events.clone().into(),
        ];
        for (name, value) in &self.values {
            args.push(format!("--{name}").into());
            if !value.is_empty() {
                args.push(value.clone().into());
            }
        }
        args.extend(self.files.iter().map(|f| f.clone().into()));
        args
    }

    /// The same run without the GUI's own flags, for "this is what it does".
    fn display_argv(&self) -> Vec<String> {
        let mut args: Vec<String> = Vec::new();
        for (name, value) in &self.values {
            args.push(format!("--{name}"));
            if !value.is_empty() {
                args.push(value.clone());
            }
        }
        args.extend(self.files.iter().map(|f| f.to_string_lossy().into_owned()));
        args
    }

    fn current(&self, f: &FlagDesc) -> String {
        self.values
            .get(&f.name)
            .cloned()
            .unwrap_or_else(|| f.default.clone())
    }

    fn set_value(&mut self, name: &str, value: &str, cx: &mut Context<Self>) {
        self.values.insert(name.to_string(), value.to_string());
        self.persist();
        cx.notify();
    }

    fn clear_value(&mut self, name: &str, cx: &mut Context<Self>) {
        self.values.remove(name);
        self.persist();
        cx.notify();
    }

    fn is_on(&self, name: &str) -> bool {
        self.values.contains_key(name)
    }

    fn toggle(&mut self, name: &str, cx: &mut Context<Self>) {
        if self.values.remove(name).is_none() {
            self.values.insert(name.to_string(), String::new());
        }
        self.persist();
        cx.notify();
    }

    fn bump(&mut self, f: &FlagDesc, delta: f64, cx: &mut Context<Self>) {
        let current = self
            .values
            .get(&f.name)
            .filter(|v| !v.is_empty())
            .cloned()
            .unwrap_or_else(|| f.default.clone());
        self.values.insert(f.name.clone(), stepped(f, &current, delta));
        self.persist();
        cx.notify();
    }

    fn add_files(&mut self, cx: &mut Context<Self>) {
        let picked = rfd::FileDialog::new()
            .add_filter(
                "media",
                &[
                    "mp4", "mkv", "mov", "webm", "avi", "m4v", "gif", "webp", "avif", "jpg",
                    "jpeg", "png", "heic", "mp3", "flac", "wav", "opus", "m4a", "ogg",
                ],
            )
            .pick_files();
        if let Some(paths) = picked {
            self.files.extend(paths);
            self.rows = self.files.iter().map(|f| queued_row(f)).collect();
            self.total = self.files.len();
            self.summary.clear();
            self.persist();
        }
        cx.notify();
    }

    fn start(&mut self, cx: &mut Context<Self>) {
        if self.running || self.files.is_empty() {
            return;
        }
        let _ = std::fs::remove_file(&self.events);
        self.offset = 0;
        self.rows = self.files.iter().map(|f| queued_row(f)).collect();
        self.done = 0;
        self.total = self.files.len();
        self.failures = 0;
        self.log.clear();
        self.summary.clear();
        self.tab = Tab::Queue;

        let mut cmd = Command::new(mins_bin());
        cmd.args(self.argv());
        match cmd.spawn() {
            Ok(child) => {
                self.child = Some(child);
                self.running = true;
                self.persist();
                self.poll(cx);
            }
            Err(e) => self.summary = format!("cannot run mins: {e}"),
        }
        cx.notify();
    }

    fn cancel(&mut self, cx: &mut Context<Self>) {
        if let Some(child) = self.child.as_mut() {
            interrupt(child);
            self.summary = "cancelling...".into();
        }
        cx.notify();
    }

    fn clear(&mut self, cx: &mut Context<Self>) {
        if self.running {
            return;
        }
        self.files.clear();
        self.rows.clear();
        self.total = 0;
        self.done = 0;
        self.summary.clear();
        self.persist();
        cx.notify();
    }

    fn poll(&self, cx: &mut Context<Self>) {
        cx.spawn(async move |this, cx| loop {
            Timer::after(Duration::from_millis(120)).await;
            let mut exited = false;
            let alive = this.update(cx, |this, cx| {
                this.drain();
                if let Some(child) = this.child.as_mut() {
                    if let Ok(Some(_)) = child.try_wait() {
                        this.child = None;
                        this.running = false;
                        this.persist();
                        this.drain();
                        exited = true;
                    }
                }
                cx.notify();
                exited
            });
            match alive {
                Ok(true) | Err(_) => break,
                Ok(false) => {}
            }
        })
        .detach();
    }

    fn drain(&mut self) {
        let Ok(mut f) = File::open(&self.events) else {
            return;
        };
        if f.seek(SeekFrom::Start(self.offset)).is_err() {
            return;
        }
        let mut buf = Vec::new();
        if f.read_to_end(&mut buf).is_err() {
            return;
        }
        // Only consume complete lines: the writer may be mid-line.
        let Some(end) = buf.iter().rposition(|b| *b == b'\n') else {
            return;
        };
        self.offset += (end + 1) as u64;
        let text = String::from_utf8_lossy(&buf[..=end]).into_owned();
        for line in text.lines() {
            self.handle(line);
        }
    }

    fn handle(&mut self, line: &str) {
        let Ok(v) = serde_json::from_str::<serde_json::Value>(line) else {
            return;
        };
        match v["t"].as_str().unwrap_or("") {
            "run_start" => self.total = v["total"].as_u64().unwrap_or(0) as usize,
            "file_start" => {
                let i = v["index"].as_u64().unwrap_or(0) as usize;
                if let Some(row) = self.rows.get_mut(i) {
                    row.status = "running".into();
                }
            }
            "file_done" => {
                let i = v["index"].as_u64().unwrap_or(0) as usize;
                let status = v["status"].as_str().unwrap_or("").to_string();
                let detail = format!(
                    "q {} · vmaf {} · {}",
                    v["q"].as_str().unwrap_or("-"),
                    v["vmaf"].as_str().unwrap_or("-"),
                    v["size"].as_str().unwrap_or("-"),
                );
                if let Some(row) = self.rows.get_mut(i) {
                    row.status = status;
                    row.detail = detail;
                    row.output = v["output"].as_str().unwrap_or("").to_string();
                }
            }
            "progress" => {
                self.done = v["done"].as_u64().unwrap_or(0) as usize;
                self.failures = v["failures"].as_u64().unwrap_or(0) as usize;
            }
            "log" => {
                if let Some(line) = v["line"].as_str() {
                    self.log.push(line.to_string());
                    if self.log.len() > 500 {
                        self.log.remove(0);
                    }
                }
            }
            "run_end" => {
                self.failures = v["failures"].as_u64().unwrap_or(0) as usize;
                self.summary = if self.failures > 0 {
                    format!("finished with {} failure(s)", self.failures)
                } else {
                    "finished".to_string()
                };
            }
            _ => {}
        }
    }

    /// The control for one flag. `slider` asks bounded numbers for a slider;
    /// the All options tab keeps the stepper for fine adjustment.
    fn control(&self, f: &FlagDesc, cx: &mut Context<Self>, slider: bool) -> AnyElement {
        match f.kind.as_str() {
            "bool" => {
                let on = self.is_on(&f.name);
                pill(
                    cx,
                    format!("{}-on", f.name),
                    if on { "On" } else { "Off" },
                    on,
                    {
                        let name = f.name.clone();
                        move |this, cx| this.toggle(&name, cx)
                    },
                )
                .into_any_element()
            }
            "enum" => div()
                .flex()
                .items_center()
                .gap_1()
                .children(f.values.iter().map(|v| {
                    let name = f.name.clone();
                    let value = v.clone();
                    let active = self.current(f) == *v;
                    pill(cx, format!("{}-{v}", f.name), v.clone(), active, move |this, cx| {
                        this.set_value(&name, &value, cx)
                    })
                }))
                .into_any_element(),
            "number" if slider && f.min.is_some() && f.max.is_some() => self.slider(f, cx),
            "number" => self.number_stepper(f, cx),
            "path" => {
                let folder = f.name == "out-dir";
                let pick_name = f.name.clone();
                let clear_name = f.name.clone();
                let shown = self
                    .values
                    .get(&f.name)
                    .map(|p| if folder { p.clone() } else { short_path(p) })
                    .unwrap_or_else(|| {
                        if folder {
                            "beside each input".to_string()
                        } else {
                            "not set".to_string()
                        }
                    });
                let mut row = div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().child(shown))
                    .child(pill(cx, format!("{}-pick", f.name), "Choose…", false, move |this, cx| {
                        let picked = if folder {
                            rfd::FileDialog::new().pick_folder()
                        } else {
                            rfd::FileDialog::new().pick_file()
                        };
                        if let Some(p) = picked {
                            this.set_value(&pick_name, &p.to_string_lossy(), cx);
                        }
                    }));
                if self.values.contains_key(&f.name) {
                    row = row.child(pill(cx, format!("{}-clear", f.name), "Reset", false, move |this, cx| {
                        this.clear_value(&clear_name, cx)
                    }));
                }
                row.into_any_element()
            }
            _ if f.name == "atracks" || f.name == "stracks" => div()
                .flex()
                .items_center()
                .gap_1()
                .children(["all", "none"].into_iter().map(|v| {
                    let name = f.name.clone();
                    pill(cx, format!("{}-{v}", f.name), v, self.current(f) == v, move |this, cx| {
                        this.set_value(&name, v, cx)
                    })
                }))
                .into_any_element(),
            _ => div()
                .px_2()
                .py_1()
                .rounded_md()
                .bg(rgb(0x2a2a30))
                .text_sm()
                .child(format!("set with --{} in a terminal", f.name))
                .into_any_element(),
        }
    }

    fn number_stepper(&self, f: &FlagDesc, cx: &mut Context<Self>) -> AnyElement {
        let name = f.name.clone();
        let up = f.clone();
        let down = f.clone();
        div()
            .flex()
            .items_center()
            .gap_1()
            .child(pill(cx, format!("{}-down", f.name), "−", false, move |this, cx| {
                this.bump(&down, -1.0, cx)
            }))
            .child(
                div()
                    .w(px(64.0))
                    .text_center()
                    .child(if self.current(f).is_empty() {
                        "auto".to_string()
                    } else {
                        self.current(f)
                    }),
            )
            .child(pill(cx, format!("{}-up", f.name), "+", false, move |this, cx| {
                this.bump(&up, 1.0, cx)
            }))
            .child(pill(cx, format!("{}-auto", f.name), "auto", self.current(f).is_empty(), move |this, cx| {
                this.clear_value(&name, cx)
            }))
            .into_any_element()
    }

    /// A slider over an engine-bounded number. GPUI has no slider widget, so
    /// the track is a row of clickable stops, at most 21 of them.
    fn slider(&self, f: &FlagDesc, cx: &mut Context<Self>) -> AnyElement {
        let (lo, hi) = match (f.min, f.max) {
            (Some(lo), Some(hi)) if hi > lo => (lo, hi),
            _ => return self.number_stepper(f, cx),
        };
        let positions = slider_positions(lo, hi).max(2);
        let current = self
            .current(f)
            .parse::<f64>()
            .ok()
            .or_else(|| f.default.parse::<f64>().ok())
            .unwrap_or(lo);
        let step = (hi - lo) / (positions - 1) as f64;
        let filled =
            (((current - lo) / step).round() as i64).clamp(0, positions as i64 - 1) as usize;
        let cells = (0..positions).map(|i| {
            let value = slide_value(f, i, positions);
            let name = f.name.clone();
            let on = i <= filled;
            let mut cell = div()
                .id(SharedString::from(format!("{}-seg-{i}", f.name)))
                .flex_1()
                .h(px(14.0))
                .bg(if on { rgb(0x2f6fed) } else { rgb(0x2f2f36) })
                .hover(move |s| s.bg(if on { rgb(0x4a82ff) } else { rgb(0x3a3a42) }))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.set_value(&name, &value, cx)
                }));
            if i == 0 {
                cell = cell.rounded_l_md();
            }
            if i + 1 == positions {
                cell = cell.rounded_r_md();
            }
            cell
        });
        div()
            .flex()
            .items_center()
            .gap_2()
            .child(div().flex().w(px(220.0)).children(cells))
            .child(div().w(px(36.0)).text_center().child(fmt_num(current)))
            .into_any_element()
    }

    fn option_row(&self, f: &FlagDesc, cx: &mut Context<Self>, slider: bool) -> AnyElement {
        div()
            .flex()
            .items_center()
            .justify_between()
            .gap_4()
            .py_1()
            .w_full()
            .child(
                div()
                    .flex()
                    .flex_col()
                    .child(div().text_sm().child(surface::label(&f.name).to_string()))
                    .child(
                        div()
                            .text_xs()
                            .text_color(rgb(0x8b8b93))
                            .child(surface::help(&f.name).to_string()),
                    ),
            )
            .child(self.control(f, cx, slider))
            .into_any_element()
    }

    /// A bare on/off pill for the queue panel, labelled by what it does rather
    /// than by its flag name.
    fn toggle_pill(&self, f: &FlagDesc, caption: &'static str, cx: &mut Context<Self>) -> Stateful<Div> {
        let name = f.name.clone();
        let on = self.is_on(&f.name);
        pill(cx, format!("main-{}", f.name), caption, on, move |this, cx| {
            this.toggle(&name, cx)
        })
    }

    /// The speed knob for images: the effort flag that matches `--img-fmt`.
    fn image_speed(&self, cx: &mut Context<Self>) -> Option<(String, AnyElement)> {
        let surface = self.surface.as_ref()?;
        let fmt = surface
            .flags
            .iter()
            .find(|f| f.name == "img-fmt")
            .map(|f| self.current(f))
            .unwrap_or_else(|| "webp".to_string());
        let f = surface.flags.iter().find(|f| f.name == effort_flag(&fmt))?;
        let title = format!("Speed · {}", surface::label(&f.name));
        Some((title, self.control(f, cx, true)))
    }

    /// The controls people change on every batch, on the screen they are
    /// already on. Everything is a normal `--describe` flag; the All options
    /// tab keeps the rest and the explanations.
    fn render_main_settings(&self, cx: &mut Context<Self>) -> Option<Div> {
        let surface = self.surface.as_ref()?;
        let find = |n: &str| surface.flags.iter().find(|f| f.name == n);

        let mut top = div().flex().items_center().justify_between().gap_8();
        if let Some(f) = find("mode") {
            top = top.child(inline_setting("Mode", self.control(f, cx, false)));
        }
        if let Some(f) = find("target") {
            top = top.child(inline_setting(
                "Quality target (VMAF)",
                self.control(f, cx, true),
            ));
        }

        let mut speeds = div().flex().items_center().justify_between().gap_8();
        if let Some(f) = find("encoder") {
            speeds = speeds.child(inline_setting("Video speed", self.control(f, cx, false)));
        }
        if let Some((title, control)) = self.image_speed(cx) {
            speeds = speeds.child(inline_setting(&title, control));
        }

        let mut audio = div()
            .flex()
            .items_center()
            .gap_3()
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0xb8b8c0))
                    .child("Audio"),
            );
        if let Some(f) = find("acodec") {
            audio = audio.child(self.control(f, cx, false));
        }
        if let Some(f) = find("audio-search") {
            audio = audio.child(self.toggle_pill(f, "search bitrate", cx));
        }

        let mut bottom = div().flex().items_center().justify_between().gap_8();
        if let Some(f) = find("out-dir") {
            bottom = bottom.child(inline_setting("Output folder", self.control(f, cx, false)));
        }
        let mut options = div()
            .flex()
            .items_center()
            .gap_2()
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0xb8b8c0))
                    .child("Options"),
            );
        for (name, caption) in [
            ("fbf", "Frame-by-frame"),
            ("refine", "Repair weak spots"),
            ("skip-existing", "Skip existing"),
            ("no-json", "No reports"),
        ] {
            if let Some(f) = find(name) {
                options = options.child(self.toggle_pill(f, caption, cx));
            }
        }
        bottom = bottom.child(options);

        Some(
            div()
                .flex()
                .flex_col()
                .gap_2()
                .child(
                    div()
                        .text_sm()
                        .text_color(rgb(0x8b8b93))
                        .child("Main settings"),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap_2()
                        .p_3()
                        .rounded_lg()
                        .bg(rgb(0x202024))
                        .child(top)
                        .child(speeds)
                        .child(audio)
                        .child(bottom),
                ),
        )
    }

    fn render_queue(&self, cx: &mut Context<Self>) -> Div {
        let mut body = div()
            .flex()
            .flex_col()
            .gap_2()
            .flex_1()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(action(cx, "add", "Add files", |this, cx| this.add_files(cx)))
                    .child(action(cx, "clear", "Clear", |this, cx| this.clear(cx)))
                    .child(if self.running {
                        action(cx, "cancel", "Cancel", |this, cx| this.cancel(cx))
                    } else {
                        action(cx, "run", "Run", |this, cx| this.start(cx))
                    }),
            );
        if let Some(settings) = self.render_main_settings(cx) {
            body = body.child(settings);
        } else {
            body = body.child(
                div()
                    .p_2()
                    .rounded_md()
                    .bg(rgb(0x3a2a20))
                    .text_sm()
                    .child(format!(
                        "No settings: the engine at {} did not answer --describe.",
                        mins_bin().display()
                    )),
            );
        }
        body = body.child(
            div()
                .id("queue-rows")
                .flex()
                .flex_col()
                .gap_1()
                .flex_1()
                .overflow_y_scroll()
                .children(self.rows.iter().enumerate().map(|(i, row)| {
                    div()
                        .id(("row", i))
                        .flex()
                        .items_center()
                        .justify_between()
                        .gap_3()
                        .px_3()
                        .py_2()
                        .rounded_md()
                        .bg(rgb(0x24242a))
                        .child(div().text_sm().truncate().child(row.name.clone()))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap_3()
                                .child(div().text_sm().child(row.detail.clone()))
                                .when(!row.output.is_empty(), |d| {
                                    let out = row.output.clone();
                                    d.child(pill(
                                        cx,
                                        format!("out-{i}"),
                                        short_path(&out),
                                        false,
                                        move |_, _| reveal(Path::new(&out)),
                                    ))
                                })
                                .child(div().text_sm().child(row.status.clone())),
                        )
                }))
                .when(self.rows.is_empty(), |d| {
                    d.child(
                        div()
                            .p_4()
                            .text_sm()
                            .text_color(rgb(0x8b8b93))
                            .child("No files yet. Click Add files."),
                    )
                }),
        );
        let cmd = shell_join(&self.display_argv());
        body = body
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        div()
                            .flex_1()
                            .truncate()
                            .font_family("monospace")
                            .text_sm()
                            .text_color(rgb(0x8b8b93))
                            .child(format!("mins {cmd}")),
                    )
                    .child(pill(cx, "copy-cmd", "Copy", false, move |_, cx| {
                        cx.write_to_clipboard(ClipboardItem::new_string(format!("mins {cmd}")))
                    })),
            )
            .child(div().text_sm().text_color(rgb(0x8b8b93)).child(format!(
                "{} option(s) changed",
                self.values.len()
            )));
        body
    }

    fn render_options(&self, cx: &mut Context<Self>) -> AnyElement {
        let Some(surface) = self.surface.as_ref() else {
            return div()
                .p_4()
                .text_sm()
                .text_color(rgb(0x8b8b93))
                .child("The mins engine did not answer --describe. Only queue controls are available.")
                .into_any_element();
        };
        let mut list = div()
            .id("options")
            .flex()
            .flex_col()
            .flex_1()
            .gap_1()
            .overflow_y_scroll();
        for f in &surface.flags {
            list = list.child(self.option_row(f, cx, false));
        }
        div()
            .flex()
            .flex_col()
            .flex_1()
            .gap_2()
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8b8b93))
                    .child(format!("engine {} · {} options", surface.version, surface.flags.len())),
            )
            .child(list)
            .into_any_element()
    }
}

impl Drop for MinsApp {
    fn drop(&mut self) {
        // Closing the window mid-run stops the engine; it cleans its own temps.
        if let Some(child) = self.child.as_mut() {
            // running is still true here, so the next launch reports the
            // interruption instead of silently dropping the batch.
            interrupt(child);
        }
        self.persist();
    }
}

impl Render for MinsApp {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let frac = if self.total > 0 {
            self.done as f32 / self.total as f32
        } else {
            0.0
        };
        let body = match self.tab {
            Tab::Queue => self.render_queue(cx).into_any_element(),
            Tab::Options => self.render_options(cx),
        };

        div()
            .flex()
            .flex_col()
            .gap_2()
            .p_4()
            .w(px(900.0))
            .h(px(720.0))
            .bg(rgb(0x1b1b1f))
            .text_color(rgb(0xe4e4e7))
            .font_family("sans-serif")
            .child(
                div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .child(div().text_xl().child("mins"))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .child(pill(cx, "tab-queue", "Queue", self.tab == Tab::Queue, |this, cx| {
                                this.tab = Tab::Queue;
                                cx.notify()
                            }))
                            .child(pill(cx, "tab-options", "All options", self.tab == Tab::Options, |this, cx| {
                                this.tab = Tab::Options;
                                cx.notify()
                            })),
                    ),
            )
            .child(body)
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(
                        div()
                            .w(px(BAR_W))
                            .h(px(6.0))
                            .rounded_md()
                            .bg(rgb(0x2f2f36))
                            .child(
                                div()
                                    .w(px(BAR_W * frac))
                                    .h(px(6.0))
                                    .rounded_md()
                                    .bg(rgb(0x4caf50)),
                            ),
                    )
                    .child(
                        div()
                            .text_sm()
                            .child(format!("{}/{}", self.done, self.total)),
                    )
                    .child(div().text_sm().child(self.summary.clone())),
            )
            .child(
                div()
                    .id("log")
                    .h(px(180.0))
                    .p_2()
                    .rounded_md()
                    .bg(rgb(0x111114))
                    .overflow_y_scroll()
                    .font_family("monospace")
                    .text_sm()
                    .flex()
                    .flex_col()
                    .children(
                        self.log
                            .iter()
                            .rev()
                            .take(200)
                            .rev()
                            .map(|line| div().child(line.clone())),
                    ),
            )
    }
}

fn inline_setting(title: &str, control: AnyElement) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2()
        .child(
            div()
                .text_sm()
                .text_color(rgb(0xb8b8c0))
                .child(title.to_string()),
        )
        .child(control)
}

fn encoder_caption(value: &str) -> &'static str {
    match value {
        "svt" => "SVT · fast",
        "aom" => "AOM · smallest",
        "nvenc" => "NVENC · GPU",
        _ => "other",
    }
}

/// The per-format effort flag that is the image speed knob.
fn effort_flag(img_fmt: &str) -> &'static str {
    match img_fmt {
        "avif" => "aspeed",
        "jxl" => "jxl-effort",
        _ => "method",
    }
}

fn slider_positions(lo: f64, hi: f64) -> usize {
    let span = hi - lo;
    if span <= 20.0 {
        span.round() as usize + 1
    } else {
        21
    }
}

fn slide_value(f: &FlagDesc, index: usize, positions: usize) -> String {
    let lo = f.min.unwrap_or(0.0);
    let hi = f.max.unwrap_or(lo);
    let step = (hi - lo) / (positions.max(2) - 1) as f64;
    fmt_num(lo + index as f64 * step)
}

fn pill(
    cx: &mut Context<MinsApp>,
    id: impl Into<SharedString>,
    text: impl Into<SharedString>,
    active: bool,
    f: impl Fn(&mut MinsApp, &mut Context<MinsApp>) + 'static,
) -> Stateful<Div> {
    let id: SharedString = id.into();
    let text: SharedString = text.into();
    div()
        .id(id)
        .px_2()
        .py_1()
        .rounded_md()
        .bg(if active { rgb(0x2f6fed) } else { rgb(0x2a2a30) })
        .hover(|s| s.bg(if active { rgb(0x2f6fed) } else { rgb(0x3a3a42) }))
        .text_sm()
        .child(text)
        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| f(this, cx)))
}

fn action(
    cx: &mut Context<MinsApp>,
    id: &'static str,
    label: &'static str,
    f: impl Fn(&mut MinsApp, &mut Context<MinsApp>) + 'static,
) -> Stateful<Div> {
    pill(cx, id, label, false, f)
}

/// Values for flags the engine no longer describes would be sent as unknown
/// arguments, which mins treats as input paths. Drop them.
fn prune_values(values: &mut BTreeMap<String, String>, surface: Option<&Surface>) {
    let Some(surface) = surface else {
        values.clear();
        return;
    };
    values.retain(|k, _| surface.flags.iter().any(|f| f.name == *k));
}

fn fmt_num(v: f64) -> String {
    if v.fract() == 0.0 {
        return format!("{}", v as i64);
    }
    format!("{v:.3}")
        .trim_end_matches('0')
        .trim_end_matches('.')
        .to_string()
}

/// One step of a numeric control, clamped to the bounds the engine enforces.
fn stepped(f: &FlagDesc, current: &str, delta: f64) -> String {
    let base = current
        .parse::<f64>()
        .ok()
        .or_else(|| f.default.parse::<f64>().ok())
        .unwrap_or(f.min.unwrap_or(0.0));
    let mut v = base + delta;
    if let Some(lo) = f.min {
        v = v.max(lo);
    }
    if let Some(hi) = f.max {
        v = v.min(hi);
    }
    fmt_num(v)
}

fn short_path(p: &str) -> String {
    Path::new(p)
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| p.to_string())
}

/// Quote only what needs it; this is a display string, not a shell parser.
fn shell_join(args: &[String]) -> String {
    args.iter()
        .map(|a| {
            if a.contains(' ') {
                format!("\"{a}\"")
            } else {
                a.clone()
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

/// Open the folder that holds `path` in the desktop file manager.
fn reveal(path: &Path) {
    let dir = path.parent().unwrap_or(path);
    #[cfg(target_os = "windows")]
    let mut cmd = Command::new("explorer");
    #[cfg(target_os = "macos")]
    let mut cmd = Command::new("open");
    #[cfg(all(unix, not(target_os = "macos")))]
    let mut cmd = Command::new("xdg-open");
    let _ = cmd.arg(dir).spawn();
}

fn interrupted_row(path: &Path) -> Row {
    Row {
        status: "interrupted".into(),
        ..queued_row(path)
    }
}

fn queued_row(path: &Path) -> Row {
    Row {
        name: path
            .file_name()
            .map(|n| n.to_string_lossy().into_owned())
            .unwrap_or_else(|| path.display().to_string()),
        status: "queued".into(),
        detail: String::new(),
        output: String::new(),
    }
}

fn mins_bin() -> PathBuf {
    if let Ok(p) = std::env::var("MINS_BIN") {
        return PathBuf::from(p);
    }
    if let Ok(exe) = std::env::current_exe() {
        let sibling = exe.with_file_name(if cfg!(windows) { "mins.exe" } else { "mins" });
        if sibling.is_file() {
            return sibling;
        }
    }
    // shortcut: dev builds fall back to PATH; installers ship the sibling.
    PathBuf::from("mins")
}

fn interrupt(child: &mut Child) {
    #[cfg(unix)]
    unsafe {
        libc::kill(child.id() as i32, libc::SIGINT);
    }
    #[cfg(not(unix))]
    {
        let _ = child.kill();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn app() -> MinsApp {
        // No state path: tests never read or write the user's real state file.
        let mut a = MinsApp::with_state(None, State::default(), None);
        a.files = vec![PathBuf::from("/tmp/a.mp4"), PathBuf::from("/tmp/b.png")];
        a.rows = a.files.iter().map(|f| queued_row(f)).collect();
        a
    }

    #[test]
    fn unknown_flags_are_dropped_on_restore() {
        let surface = Surface {
            version: "x".into(),
            flags: vec![FlagDesc {
                name: "target".into(),
                kind: "number".into(),
                default: "95".into(),
                values: vec![],
                min: None,
                max: None,
            }],
        };
        let mut values = BTreeMap::new();
        values.insert("target".into(), "90".into());
        values.insert("flag-from-2.0".into(), "x".into());
        prune_values(&mut values, Some(&surface));
        assert_eq!(values.len(), 1);
        assert!(values.contains_key("target"));

        let mut all = values.clone();
        prune_values(&mut all, None);
        assert!(all.is_empty());
    }

    #[test]
    fn interrupted_run_restores_as_interrupted() {
        let mut st = State::default();
        st.files = vec![PathBuf::from("/tmp/a.mp4")];
        st.running = true;
        let a = MinsApp::with_state(None, st, None);
        assert_eq!(a.rows.len(), 1);
        assert_eq!(a.rows[0].status, "interrupted");
        assert!(a.summary.contains("interrupted"));
        assert!(!a.running, "restoring must not pretend a child is running");
    }

    #[test]
    fn clean_state_restores_as_queued() {
        let mut st = State::default();
        st.files = vec![PathBuf::from("/tmp/a.mp4")];
        let a = MinsApp::with_state(None, st, None);
        assert_eq!(a.rows[0].status, "queued");
        assert!(a.summary.is_empty());
    }

    fn args_of(a: &MinsApp) -> Vec<String> {
        a.argv()
            .iter()
            .map(|s| s.to_string_lossy().into_owned())
            .collect()
    }

    #[test]
    fn untouched_app_is_a_bare_run() {
        let a = app();
        let args = args_of(&a);
        assert_eq!(args[0], "--no-tui");
        assert_eq!(args[1], "--events");
        assert_eq!(&args[3..], &["/tmp/a.mp4", "/tmp/b.png"]);
    }

    #[test]
    fn changed_values_become_flags() {
        let mut a = app();
        a.values.insert("target".into(), "92".into());
        a.values.insert("fbf".into(), String::new());
        let args = args_of(&a);
        assert!(args.windows(2).any(|w| w == ["--target", "92"]));
        assert!(args.iter().any(|a| a == "--fbf"));
        // A boolean must not swallow the next argument.
        let i = args.iter().position(|a| a == "--fbf").unwrap();
        assert!(args[i + 1].starts_with("--") || args[i + 1].starts_with('/'));
    }

    #[test]
    fn stepper_respects_engine_bounds() {
        let f = FlagDesc {
            name: "method".into(),
            kind: "number".into(),
            default: "6".into(),
            values: vec![],
            min: Some(0.0),
            max: Some(6.0),
        };
        assert_eq!(stepped(&f, "6", 1.0), "6");
        assert_eq!(stepped(&f, "0", -1.0), "0");
        assert_eq!(stepped(&f, "", -1.0), "5");
        assert_eq!(stepped(&f, "", 1.0), "6");
        assert_eq!(stepped(&f, "2.5", 0.5), "3");
    }

    #[test]
    fn command_preview_drops_harness_flags() {
        let mut a = app();
        a.values.insert("target".into(), "92".into());
        a.values.insert("fbf".into(), String::new());
        assert_eq!(
            shell_join(&a.display_argv()),
            "--fbf --target 92 /tmp/a.mp4 /tmp/b.png"
        );
    }

    #[test]
    fn shell_join_quotes_spaces() {
        let args = vec!["--log".to_string(), "my run.log".to_string()];
        assert_eq!(shell_join(&args), "--log \"my run.log\"");
    }

    #[test]
    fn slider_stops_cover_the_engine_range() {
        let f = FlagDesc {
            name: "target".into(),
            kind: "number".into(),
            default: "95".into(),
            values: vec![],
            min: Some(0.0),
            max: Some(100.0),
        };
        let n = slider_positions(0.0, 100.0);
        assert_eq!(n, 21);
        assert_eq!(slide_value(&f, 0, n), "0");
        assert_eq!(slide_value(&f, 10, n), "50");
        assert_eq!(slide_value(&f, n - 1, n), "100");
        // Small ranges get one stop per integer.
        assert_eq!(slider_positions(0.0, 6.0), 7);
        assert_eq!(slider_positions(1.0, 9.0), 9);
    }

    #[test]
    fn image_speed_follows_the_output_format() {
        assert_eq!(effort_flag("webp"), "method");
        assert_eq!(effort_flag("avif"), "aspeed");
        assert_eq!(effort_flag("jxl"), "jxl-effort");
    }

    #[test]
    fn events_update_rows_and_summary() {
        let mut a = app();
        a.handle(r#"{"v":1,"t":"run_start","total":2}"#);
        a.handle(r#"{"v":1,"t":"file_start","index":0,"input":"/tmp/a.mp4"}"#);
        a.handle(r#"{"v":1,"t":"file_done","index":0,"q":"37","vmaf":"96.1","size":"1,2MB","status":"OK","failed":false}"#);
        a.handle(r#"{"v":1,"t":"progress","done":2,"total":2,"failures":1}"#);
        a.handle(r#"{"v":1,"t":"run_end","failures":1}"#);
        a.handle(r#"{"v":1,"t":"log","line":"[mins] hello"}"#);

        assert_eq!(a.rows[0].status, "OK");
        assert!(a.rows[0].detail.contains("q 37"));
        assert_eq!(a.rows[1].status, "queued");
        assert_eq!(a.done, 2);
        assert_eq!(a.failures, 1);
        assert_eq!(a.summary, "finished with 1 failure(s)");
        assert_eq!(a.log, vec!["[mins] hello"]);
    }
}

fn main() {
    Application::new().run(|cx: &mut App| {
        cx.on_window_closed(|cx| {
            if cx.windows().is_empty() {
                cx.quit();
            }
        })
        .detach();

        let bounds = Bounds::centered(None, size(px(900.0), px(720.0)), cx);
        cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                ..Default::default()
            },
            |_, cx| cx.new(|_| MinsApp::new()),
        )
        .unwrap();
        cx.activate(true);
    });
}
