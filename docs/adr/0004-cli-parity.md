# ADR 0004 — Keep the shell CLI, reports and log layout

**Status:** accepted

## Context

`mins` is used in scripts and habits built around `mins.sh` v1.17.0. A rewrite
that renames flags, reports, or log lines is not a drop-in replacement.

## Decision

Reproduce the v1.17.0 interface exactly:

- the same flags, aliases, defaults and validation errors;
- the same output filenames, including the `.min` collision suffix and trim
  suffix;
- the same JSON schemas, field order, float formatting (`95.0`), and lack of a
  trailing newline;
- the same `[mins]` log lines, `mins-logs/` filenames (including the trailing
  underscore in the tag), summary widths and exit codes.

The only intentional differences are the version string, `--help` text
(bundled tools, no ImageMagick), and a clear error for `--img-encoder magick`.

## Consequences

- Parity tests can diff log lines and reports directly.
- Quirks are preserved deliberately (for example the trailing `_` in log
  tags, and `numfmt`'s ceiling rounding and locale decimal separator).
- Python's JSON float shape is reproduced with a custom `report.F` type.
