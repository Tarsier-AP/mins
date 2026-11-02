# Third-party notices

Release archives embed compressed copies of the tools below. They are
extracted at runtime and executed as separate programs; the Go code itself
uses only the standard library.

This project is for personal use. Distributing a binary that embeds a
GPL-enabled FFmpeg build makes the combined distribution subject to the GPL.
Review every license below before redistributing anything.

| component | version | license | source |
|---|---|---|---|
| FFmpeg | 9.0.2 | LGPL-2.1+ (GPL-2.0+ when built with GPL components) | https://ffmpeg.org/ |
| libvmaf | 3.0.0 | BSD-2-Clause | https://github.com/Netflix/vmaf |
| SVT-AV1 | 3.1.0 | BSD-2-Clause | https://gitlab.com/AOMediaCodec/SVT-AV1 |
| libaom | 3.13.1 | BSD-2-Clause | https://aomedia.googlesource.com/aom |
| libwebp | 1.6.0 | BSD-3-Clause | https://chromium.googlesource.com/webm/libwebp |
| libavif | 1.4.2 | BSD-2-Clause | https://github.com/AOMediaCodec/libavif |
| libjxl | 0.12.0 | BSD-3-Clause | https://github.com/libjxl/libjxl |
| libheif | 1.23.7 | LGPL-3.0 | https://github.com/strukturag/libheif |
| libde265 | 1.0.16 | LGPL-3.0 | https://github.com/strukturag/libde265 |
| libopus | 1.6.1 | BSD-3-Clause | https://opus-codec.org/ |
| ViSQOL | 3.3.3 | Apache-2.0 | https://github.com/google/visqol |

The ViSQOL SVR model (`libsvm_nu_svr_model.txt`) is distributed with ViSQOL
under the same Apache-2.0 license.

Full license texts ship with each upstream source tarball downloaded by
`tools/fetch-sources.sh`; include the relevant files when redistributing.
