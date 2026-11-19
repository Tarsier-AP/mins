# Release checklist

Run through this list for every tagged release. ADR 0009 explains why the
license steps are blocking rather than advisory.

## Before tagging

- [ ] `tools/versions.env` holds the intended versions; no `-dirty` source
      checkouts.
- [ ] `tools/licenses/inventory.tsv` matches `tools/versions.env`: every new
      component, version, SPDX expression, link set, and license file is
      recorded.
- [ ] `make licenses` regenerated `THIRD_PARTY_NOTICES.md`, and the diff is
      only what you intended to change.
- [ ] `make check` passes (unit tests plus the offline license gate).
- [ ] `make integration` passes against the rebuilt toolchain.
- [ ] The toolchain was rebuilt after any `tools/build.sh` change. Confirm the
      FFmpeg banner:
      `tools/.build/prefix/bin/ffmpeg -hide_banner -L | head -20` says
      "GNU Lesser General Public License", not "GNU General Public License".
- [ ] `bash tools/check-licenses.sh --built` passes with `PREFIX` pointing at
      the release toolchain.
- [ ] `make relink TOOLCHAIN_TARGET=<os>_<arch>` produces an archive whose
      `relink/relink-heif-dec.sh` and `relink/relink-heif-info.sh` both link
      successfully from a clean extraction.
- [ ] `make sources` produces `mins_<version>_sources.tar.xz`; extracting it
      and running `go build ./cmd/mins` succeeds, and `SOURCES.md` lists the
      same versions as `tools/versions.env`.
- [ ] `make release` archives contain `LICENSE`, `THIRD_PARTY_NOTICES.md`, and
      `LICENSES/`, and `sha256sums.txt` covers every artifact.
- [ ] No `--enable-gpl`, `--enable-nonfree`, or `--enable-version3` appears in
      `tools/build.sh` or any FFmpeg build log.

## Tag and publish

- [ ] Push the `v*` tag. The workflow runs the offline license gate before
      each toolchain build, the `--built` audit after it, the `sources` job,
      and the per-target relink archive step.
- [ ] All five target jobs and the `sources` job are green before `publish`
      runs. Do not bypass a red license step.

## After publishing

- [ ] The release page has five platform archives, `mins_<version>_sources.tar.xz`,
      five `mins_<version>_relink_<os>_<arch>.tar.xz`, and `sha256sums.txt`.
- [ ] Download one platform archive, verify its checksum, and confirm
      `LICENSES/` and `THIRD_PARTY_NOTICES.md` are present.
- [ ] Run the downloaded binary once with `--no-tui` and confirm the embedded
      toolchain extracts and processes a sample file.
- [ ] If any version changed after tagging, fix forward with a new tag. Do not
      rewrite published archives: the published source and relink materials
      must correspond to the published binaries.
