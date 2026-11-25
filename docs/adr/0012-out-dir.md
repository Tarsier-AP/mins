# ADR 0012 — `--out-dir` separates finished files from work files

**Status:** accepted

## Context

Every output lands beside its input: `dir := AbsDir(input)` in the video, FBF,
image, animate and audio paths, and both the finished file and its
`.vmaf.json` / `.fbf.vmaf.json` / `.audio.json` report are joined onto it. That
is the right default for a CLI used on a directory of files, and it is what
v1.17.0 does.

A batch UI has a different shape: files come from anywhere, and users want the
results in one place without moving sources. Temp work, though, must stay on
the input's filesystem: FBF and the image paths build concat lists and frame
directories next to the input and rely on same-filesystem renames.

## Decision

Add `--out-dir DIR`. Finished media and reports go there; temp work does not.

- **Scope.** `--out-dir` moves the final file and its report. `core.TempDir`
  is untouched, so concat lists and frame dirs stay beside the input.
- **`--log auto` stays beside the first input.** The run log describes the
  sources, and moving it would surprise anyone who reads the old location.
- **Missing directory is created.** Relative paths resolve against the working
  directory. Failure to create it is fatal (exit 2) before any media work.
- **Default unchanged.** Without the flag, `Config.OutDirFor` returns the
  input's own directory, so paths, reports, log lines and exit codes are
  byte-for-byte what they were.
- **Collisions are refused.** With `--out-dir`, two inputs that resolve to the
  same output (same stem, same mode, different source directories) would
  overwrite each other. The first claimer wins; the second gets
  `ERROR: output collision: <a> and <b> both write <path>` as its summary row
  and the run exits 1. The first file's output is kept. Without `--out-dir`
  the check is off, because that overwrite is today's behaviour and parity is
  the stronger constraint.

The check is a run-time reservation (`Env.ClaimOutput`), not a pre-flight
scan. An exact pre-flight would have to probe every input: the video name
depends on the trim window and the audio extension depends on the source codec
when `--audio-fmt auto`. Probing before the run duplicates the work and can
disagree with what the path actually computes. Reserving at the point of
writing is exact, costs one mutex, and still guarantees no silent overwrite.
The cost is that the refusal appears after earlier files have finished rather
than before any work starts.

## Consequences

- One new flag, one config field, one helper (`OutDirFor`), one call per output
  path, and one reservation per finished file.
- `--describe` lists `out-dir` as a path option, so the GUI gets a folder
  picker without new code.
- Reports follow their media, so a moved output never points at a report that
  stayed behind.
- A batch that used to overwrite silently now fails loudly, but only when the
  user asked for a shared output directory.
