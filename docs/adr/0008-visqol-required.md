# ADR 0008 — ViSQOL is required on every target

**Status:** accepted

## Context

`--audio-metric visqol` is the default audio metric and part of v1.17.0
parity. ADR 0006 let Windows attempt ViSQOL on a best-effort basis, and ADR
0007 let packaging omit it. CI made that leniency invisible: the Unix ViSQOL
build ran as `bash tools/build-visqol.sh || echo "::warning::..."`, the
Windows step had no Bazel and used `continue-on-error`, the smoke test ran
audio only when `prefix/bin/visqol` already existed, and packaging skipped
`visqol` and its model when they were absent. Every job stayed green while no
release artifact contained ViSQOL.

That contradicts the product promise and the runtime defaults. A user running
the default audio path gets a preflight error demanding `VISQOL_BIN` or a
non-default metric, so the "self-contained media minimizer" is not
self-contained for its default audio mode.

## Decision

- Every release bundle embeds `visqol` and `libsvm_nu_svr_model.txt` on all
  five targets. Packaging fails when either is missing.
- The toolchain build audits ViSQOL like every required tool; a non-portable
  binary fails the build instead of being dropped.
- CI has one strict toolchain path. No warning fallback and no
  `continue-on-error` around ViSQOL.
- The SVR model is pinned by SHA-256 in `tools/versions.env` and verified
  before it is installed.
- The embedded smoke test runs the default audio metric and asserts both a
  MOS-LQO score and the resolved model path in the report.
- `WITH_VISQOL=0` remains a local-development escape for `tools/build.sh`.
  `tools/package-assets.sh` refuses to package such a prefix.
- If a target cannot build a portable ViSQOL, its release is blocked rather
  than shipped incomplete.

## Consequences

- Windows needs a working Bazel + MSVC build with the static C runtime; the
  MSVC runtime DLLs are not shippable.
- CI pays for Bazel's 2022-era WORKSPACE downloads; a repository cache keyed
  on the pins and the patch reduces that without trusting stale build output.
- Bundles grow by roughly 1.8 MB compressed.
- The optional-ViSQOL clauses in ADR 0006 and ADR 0007 are superseded.
- The existing runtime error for a missing `visqol` still serves development
  builds, but no official artifact can reach that state.
