# mins-gui

A small desktop frontend for `mins`. It is a thin client: it builds a normal
`mins` command line, runs it as a child process, and renders the NDJSON stream
that `--events` writes. All encoding, quality search, gating and file naming
stay in the Go engine, so the GUI cannot produce different output than the CLI.

## Build and run

```sh
make build          # the engine: ./mins
make gui-run        # MINS_BIN=./mins cargo run --manifest-path gui/Cargo.toml
```

`make gui` builds an optimized binary at `gui/target/release/mins-gui`.
`make gui-check` runs its unit tests.

The GUI looks for the engine in this order: `MINS_BIN`, a sibling `mins`
executable next to `mins-gui`, then `mins` on `PATH`.

### Linux build dependencies

GPUI needs a Vulkan loader and the usual windowing/input libraries; the file
dialog uses GTK 3:

```sh
sudo apt install libvulkan1 libxkbcommon-dev libwayland-dev libx11-xcb-dev \
                 libfontconfig-dev libfreetype-dev libgtk-3-dev
```

Windows and macOS use native dialogs and need no extra packages.

## What it does

- Add files, see one row per file with its live state. Files can also be
  dragged from the file manager onto the window; directories and dead paths are
  ignored, and drops wait until the current run finishes.
- **Queue** tab: the settings people change on every batch above the file
  list — mode, VMAF target as a slider, video speed (encoder), image speed (the
  effort flag for the chosen image format), audio codec and bitrate search,
  output folder, and the common on/off switches. Run, cancel, progress and the
  log pane are here too.
- **All options** tab: every engine flag the GUI can drive, with a plain-language
  label and one-sentence explanation. Enum values become buttons, numbers get
  −/+ steppers bounded by the engine's limits, booleans are on/off, and path
  flags open a picker. Only changed values are passed on the
  command line, so an untouched form runs exactly like a bare `mins` invocation.
- Video speed is `--encoder`: mins has no separate video preset flag, so the
  choices are labelled by what they do (SVT fast, AOM smallest, NVENC GPU).
  Image speed is the per-format effort flag (`--method`, `--aspeed`,
  `--jxl-effort`), picked from the current `--img-fmt`.
- The form is built from `mins --describe`, so a new engine flag shows up with
  its real range and a test fails until it has GUI copy.
- The queue shows the exact `mins ...` command it will run, with a Copy button,
  and each finished row shows its output file; click it to open the folder.
- The queue and the options you changed are saved to the OS config directory
  (`~/.config/mins/gui/state.json` and the platform equivalents) and restored
  on the next launch. A batch that was running when the app closed comes back
  marked `interrupted` and is not restarted automatically.

Still terminal-only: custom track lists (`--atracks 0,2`), custom
`--audio-band` values, and `--start`/`--end` beyond whole seconds.

Cancel sends `SIGINT`, the same as Ctrl-C, so the engine removes its temp files
and keeps finished outputs. On Windows it terminates the child, which can leave
a temp directory behind.

## Contract with the engine

The GUI passes `--no-tui --events <tmp>` plus the controls it exposes, then the
queued paths. It understands these event types from ADR 0011:

| event | fields used |
|---|---|
| `run_start` | `total`, `mode` |
| `file_start` | `index`, `input` |
| `file_done` | `index`, `status`, `q`, `vmaf`, `size`, `failed` |
| `progress` | `done`, `total`, `failures` |
| `log` | `line` |
| `run_end` | `failures` |

Unknown types and fields are ignored, which is what lets the engine add events
without breaking older GUIs.

## Not here yet

Presets, history, per-item overrides, notifications, themes,
and `--dry-run` validation/previews are designed but not implemented. Until
they are, use the CLI for those.
