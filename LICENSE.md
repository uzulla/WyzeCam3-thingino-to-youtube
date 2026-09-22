# Licensing

This repository is licensed under the **MIT License** ([LICENSE](LICENSE)),
Copyright (c) 2026 uzulla aka Junichi Ishida <zishida@gmail.com>.

Some of what it contains or distributes derives from other projects and is
bound by their licenses. The table below says what applies to what.

| What | License | Notes |
|---|---|---|
| `device/` (shell scripts, init scripts, example configs), `docs/`, `README.md`, `NOTES.md` | MIT | Original work of this repository |
| `device/osd-feed/` (Go) | MIT | Uses only Go's standard library (BSD-3-Clause). If you distribute a built binary, ship Go's LICENSE with it |
| `device/www/` (pages and CGIs added to the Thingino web UI) | MIT | New files, written after the page structure and shared JS/CSS conventions of [Thingino](https://github.com/themactep/thingino-firmware) (MIT, Copyright (c) 2024 thingino). On the camera, the installers add one menu line to Thingino's `/var/www/a/plugins.js` |
| `patches/thingino-ffmpeg-rtmps.diff` | MIT | A change to thingino-firmware's build recipe (`package/thingino-ffmpeg/thingino-ffmpeg.mk`, MIT), not to FFmpeg's source |
| `patches/thingino-ffmpeg-rtmp-chunk-size.diff` | **GPL-3.0-or-later** ([LICENSES/GPL-3.0-or-later.txt](LICENSES/GPL-3.0-or-later.txt)) | A change to FFmpeg's `libavformat/rtmpproto.c` (a derivative work). That file is LGPL-2.1-or-later in FFmpeg; the additions in this patch are offered under GPL-3.0-or-later, the same terms as the binaries below. Ask the author if you need them for an LGPL build of FFmpeg |
| `patches/prudynt-osd-textfile.diff`, `patches/prudynt-msgchannel-warning.diff` | Additions: MIT. **The prudynt-t parts stay under prudynt-t's terms** | Derivative works of [prudynt-t](https://github.com/gtxaspec/prudynt-t) (the diff context contains upstream code). As of 2026-09 prudynt-t publishes no license file or notice (its ancestor prudynt-v3 appears to be MIT). This entry will follow whatever license upstream adopts |
| `ffmpeg-thingino-wyze-cam3-t31-mipsel` on GitHub Releases | **GPL-3.0-or-later** | See "Corresponding source" below |

## Corresponding source for the FFmpeg binaries (GPLv3 section 6)

The FFmpeg binaries on GitHub Releases are built with Thingino's build system
using `--enable-gpl --enable-version3 --enable-mbedtls`; the binary itself
reports "GNU General Public License version 3 or later" (`ffmpeg -L`).
`--enable-gpl` makes it GPL, and `--enable-version3` is required because
mbedTLS is Apache-2.0. mbedTLS is linked dynamically against the camera's
`/lib/libmbedtls.so` and is not part of the binary.

The corresponding source is the following, all available free of charge:

| Release | FFmpeg source | Changes to FFmpeg | Build recipe |
|---|---|---|---|
| [v1.1.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.1.0) | [ffmpeg-8.0.1.tar.xz](https://ffmpeg.org/releases/ffmpeg-8.0.1.tar.xz), unmodified | none | `package/thingino-ffmpeg/thingino-ffmpeg.mk` of [thingino-firmware `da40db6`](https://github.com/themactep/thingino-firmware/tree/da40db6) with `patches/thingino-ffmpeg-rtmps.diff` (as of tag v1.1.0) applied |
| [v1.0.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.0.0) | same | none | the same file of [thingino-firmware `c334a03`](https://github.com/themactep/thingino-firmware/tree/c334a03) with the same patch (as of tag v1.0.0) |

Build instructions: [docs/build.md](docs/build.md) (in Japanese). The toolchain
(GCC / uClibc-ng) is fetched by thingino-firmware from its GitHub Releases at
build time. `patches/thingino-ffmpeg-rtmp-chunk-size.diff`, which does change
FFmpeg's source, was added after v1.1.0 and is part of the binaries from the
next release on; from then on it belongs to the corresponding source. Each
release carries FFmpeg's `LICENSE.md` and `COPYING.GPLv3` as assets.

## Not distributed here

- **prudynt binaries.** prudynt links against Ingenic's SDK (libimp and
  friends, Ingenic's proprietary libraries). This repository ships patches and
  [build instructions](docs/build.md) only, never a built prudynt.
- **Thingino's files.** `device/install.sh` and `device/install-prudynt-osd.sh`
  edit `/var/www/a/plugins.js` on the camera; the file itself is not
  distributed.
- **Bootstrap / Bootstrap Icons** (MIT). The web UI pages reference them from
  a CDN, like Thingino's own pages; nothing is bundled.

## Upstream licenses referred to

- Thingino firmware — MIT — https://github.com/themactep/thingino-firmware/blob/master/LICENSE
- FFmpeg — LGPL-2.1-or-later / GPL — [LICENSES/FFmpeg-LICENSE.md](LICENSES/FFmpeg-LICENSE.md) (copy of `LICENSE.md` from 8.0.1)
- mbedTLS — Apache-2.0 OR GPL-2.0-or-later — https://github.com/Mbed-TLS/mbedtls/blob/development/LICENSE
- prudynt-t — no license published — https://github.com/gtxaspec/prudynt-t
- Go — BSD-3-Clause — https://go.dev/LICENSE
