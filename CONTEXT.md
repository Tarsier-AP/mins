# Context

Domain vocabulary and invariants for the `mins` rewrite. The authoritative
behavioural reference is `mins.sh` v1.17.0; this file explains the words used
in the Go code and the rules that must not drift.

## Glossary

**Master** — The untouched input file. `mins` never modifies or deletes it.

**VMAF gate** — The requirement that the mean VMAF of an encode, measured
against the decoded Master, is at least `--target` (default 95). The shell
script compared 8-bit `yuv420p` in every path; the Go implementation does the
same.

**CRF search** — The binary search over CRF/CQ that finds the highest value
(and therefore smallest file) still passing the VMAF gate. For images the
search runs in the opposite direction: the lowest encoder quality that passes.

**Quality tier** — Video search passes use the fast encoder settings
(`SVT_FAST`, `AOM_FAST`, ...); the final encode uses the quality tier
(`SVT_QUALITY`, ...). The report's `mean_vmaf` is measured on that final file,
which is why it can differ from the last search line.

**Trim window** — Optional `--start`/`--end` restricting what portion feeds
the encode and its measurement. Output names gain `.trim<start>-<end>`.

**Fragment** — A frame-exact run of the trimmed Master, at most
`--refine-seconds` long. `--refine` re-encodes fragments whose own mean VMAF
is below target, then verifies the merged file as a whole.

**Frame-by-frame (FBF)** — `--fbf` extracts every frame losslessly, CRF-searches
each as an AV1 intra frame, concatenates them, and remuxes the original
audio/subtitles. Much stronger per-frame guarantee, much larger and slower.

**Instance / worker** — One goroutine in the Go implementation. `--instances`
spreads whole files across workers (images and non-FBF video); `--fbf-workers`
spreads frame chunks within one FBF run. The shell used separate processes;
the observable result is the same.

**Backend** — The native image encoder chosen by `--img-encoder`:
`libwebp`, `libavif`, or `libjxl`. `auto` resolves once to the backend matching
`--img-fmt`. ImageMagick is deliberately absent.

**Reference / candidate** — For the image gate, the *reference* is the input
decoded once to 8-bit PNG (and composited over `gray50`); the *candidate* is
each encoded file decoded by the matching native decoder and flattened the same
way. Comparing the flattened pixels is what makes transparent images stable.

**Tag** — The sanitized input stem used in `mins-logs/` filenames. The shell's
`tr -c 'A-Za-z0-9._-' '_'` on a string with a trailing newline produces a
trailing underscore; the Go `tagOf` keeps that quirk for filename parity.

## Invariants

- **Output names**
  - video: `<stem><trim>.av1.<vid-fmt>`
  - fbf: `<stem><trim>.fbf.av1.<vid-fmt>`
  - image: `<stem>.<img-fmt>`, or `<stem>.min.<img-fmt>` when the input
    extension already equals the target format
  - animate: `<stem><trim>.<img-fmt>`
  - audio: `<stem>.<ext>`, or `<stem>.min.<ext>` on an extension collision
- **Reports** — `.vmaf.json`, `.fbf.vmaf.json`, `.audio.json` (with `.min`
  variants). Floats marshal like Python's `json.dump`: integral values keep
  `.0`, there is no trailing newline, and `crf`/`cq` are present even when 0.
- **Summary** — `%-42s %-5s %-5s %-9s %-24s %s` over
  `OUTPUT MODE Q VMAF SIZE STATUS`.
- **Logs** — `[mins]` prefix, the same line order and wording as the shell,
  `---- summary ----`, and `==== run end: N failure(s) ====`. Raw tool stderr
  copies go under `<log dir>/mins-logs/`.
- **Sizes** — `numfmt --to=iec --suffix=B` semantics: 1024-based, ceiling
  rounding, one decimal below 10, locale decimal separator.
- **Exit codes** — 0 success, 1 failures, 2 usage/fatal, 130 interrupted.
- **Tools** — resolution order is `MINS_<NAME>` override → `MINS_TOOLS_DIR` →
  embedded cache → `PATH` (development only). Embedded assets are gzip
  compressed, SHA-256 verified, and extracted atomically.

## Package map

| package | responsibility |
|---|---|
| `cmd/mins` | wiring, signal handling, exit codes |
| `cli` | raw drop-in flag parsing |
| `config` | validation, defaults, derived settings |
| `tools` | embedded assets, cache, manifests |
| `ffmpeg` | subprocess runner, progress, capture |
| `media` | probing (streams, duration, frames, alpha) |
| `escape` | filtergraph and concat-list escaping |
| `audio` | track selection, codecs, ViSQOL/ffmpeg metrics |
| `audioproc` | standalone audio path |
| `subtitle` | per-container subtitle policy |
| `video` | CRF search, refine, fbf |
| `image` | still/animated images, animate |
| `native` | image backend encode/decode/flatten |
| `report` | JSON schemas |
| `result` | summary table |
| `tui` | built-in dashboard |
| `logx` | run log and `mins-logs/` |
| `util` | time parsing, human sizes, stems |
