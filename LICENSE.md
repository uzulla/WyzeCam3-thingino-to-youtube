# Licensing

The original work in this repository is licensed under the **MIT License**
([LICENSE](LICENSE)), Copyright (c) 2026 uzulla aka Junichi Ishida
<zishida@gmail.com>. "Original work" means everything here except the files
listed below, which derive from other projects and follow their terms.

## What is licensed how

| What | License | Notes |
|---|---|---|
| `device/` (shell scripts, init scripts, example configs), `docs/`, `README.md`, `NOTES.md` | MIT | Original work |
| `device/osd-feed/` (Go source) | MIT | Uses only Go's standard library (BSD-3-Clause). Built binaries are not distributed here; if you distribute one, ship Go's LICENSE with it |
| `device/www/` (pages and CGIs added to the Thingino web UI) | MIT | New files, written to fit [Thingino](https://github.com/themactep/thingino-firmware)'s page structure and shared JS/CSS conventions (Thingino is MIT, Copyright (c) 2024 thingino; its notice is kept in [LICENSES/Thingino-LICENSE.txt](LICENSES/Thingino-LICENSE.txt)). On the camera, the installers add one menu line to Thingino's `/var/www/a/plugins.js` and place a manifest in `/var/www/a/plugins/` |
| `patches/thingino-ffmpeg-rtmps.diff` | MIT (derivative of an MIT file) | A change to thingino-firmware's build recipe `package/thingino-ffmpeg/thingino-ffmpeg.mk`, not to FFmpeg's source. The context lines are Thingino's (MIT, notice in [LICENSES/Thingino-LICENSE.txt](LICENSES/Thingino-LICENSE.txt)) |
| `patches/thingino-ffmpeg-rtmp-chunk-size.diff` | **GPL-3.0-or-later** ([LICENSES/GPL-3.0-or-later.txt](LICENSES/GPL-3.0-or-later.txt)) | A change to FFmpeg's `libavformat/rtmpproto.c`. That file is LGPL-2.1-or-later in FFmpeg; the LGPL allows relicensing under the GPL, and the additions in this patch are offered under GPL-3.0-or-later, the same terms as the binaries below. Ask the author if you need them for an LGPL build |
| `patches/prudynt-osd-textfile.diff`, `patches/prudynt-msgchannel-warning.diff` | Additions: MIT. **Redistribution of the whole patch is not cleared** | Derivative works of prudynt-t, whose context lines are prudynt-t code. prudynt-t ([gtxaspec/prudynt-t](https://github.com/gtxaspec/prudynt-t), and the [themactep/prudynt-t](https://github.com/themactep/prudynt-t) fork that thingino-firmware actually builds) publishes no license file or notice as of 2026-09; its ancestor prudynt-v3 appears to be MIT, but that has not been confirmed for these lines. The author's additions are MIT; permission to redistribute the upstream lines has not been obtained, so treat the patch as "use at your own judgment" until upstream states a license |
| `LICENSES/GPL-3.0-or-later.txt` | Text of the GNU GPL v3, © Free Software Foundation | Verbatim copy; the license text itself may not be modified |
| `LICENSES/FFmpeg-LICENSE.md` | FFmpeg's `LICENSE.md` (8.0.1) | Verbatim copy, for reference |
| `LICENSES/Thingino-LICENSE.txt` | Thingino's MIT notice | Verbatim copy (from thingino-firmware `da40db6`) |
| `ffmpeg-thingino-wyze-cam3-t31-mipsel` on GitHub Releases | **GPL-3.0-or-later** | See "Corresponding source" below |

## Corresponding source for the FFmpeg binaries (GPLv3 section 6)

The FFmpeg binaries on GitHub Releases are built with Thingino's build system
using `--enable-gpl --enable-version3 --enable-mbedtls`; the binary reports
"GNU General Public License version 3 or later" (`ffmpeg -L`). `--enable-gpl`
makes it GPL; `--enable-version3` is required for license compatibility with
mbedTLS (Apache-2.0), regardless of how mbedTLS is linked.

Each release's notes carry the list below for that release, next to the
binary, as section 6(d) asks; this section is the same information for all
releases. Everything is available free of charge.

| | v1.0.0 | v1.1.0 |
|---|---|---|
| Release page | [v1.0.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.0.0) | [v1.1.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.1.0) |
| FFmpeg source | [ffmpeg-8.0.1.tar.xz](https://ffmpeg.org/releases/ffmpeg-8.0.1.tar.xz), **unmodified** | same |
| Changes to FFmpeg | none | none |
| Build recipe (thingino-firmware) | [`c334a03`](https://github.com/themactep/thingino-firmware/tree/c334a03), `package/thingino-ffmpeg/thingino-ffmpeg.mk` with [`patches/thingino-ffmpeg-rtmps.diff` @ v1.0.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/blob/v1.0.0/patches/thingino-ffmpeg-rtmps.diff) applied | [`da40db6`](https://github.com/themactep/thingino-firmware/tree/da40db6), same file with [the patch @ v1.1.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/blob/v1.1.0/patches/thingino-ffmpeg-rtmps.diff) |
| Buildroot (thingino-firmware submodule) | [`313414b`](https://github.com/buildroot/buildroot/tree/313414b92c2501a2bc123ffa1b6383dca464de05) | [`d518030`](https://github.com/buildroot/buildroot/tree/d5180309b1b66ef3b8eaccca70ad69be8e0729a1) |
| mbedTLS (linked dynamically against the camera's `/lib/libmbedtls.so`) | mbedTLS 3.6.6 as built by Buildroot's `package/mbedtls` at the commit above; source: [mbedtls-3.6.6](https://github.com/Mbed-TLS/mbedtls/releases/tag/mbedtls-3.6.6) | same |
| Build steps as written at the time | [README @ v1.0.0, "ビルド手順"](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/blob/v1.0.0/README.md#ビルド手順) | [README @ v1.1.0, "ビルド手順"](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/blob/v1.1.0/README.md#ビルド手順) |

Notes:

- The build steps use the Docker image `ghcr.io/themactep/thingino-builder-image:latest`,
  a floating tag; the toolchain (GCC 16.2.0 / uClibc-ng 1.0.59 for v1.1.0) is
  fetched by thingino-firmware at build time. The pinned inputs above (FFmpeg
  tarball, thingino-firmware and Buildroot commits, our patch at its tag) are
  the source; an identical rebuild is not guaranteed if the image changes.
- mbedTLS, uClibc-ng and libatomic (GCC runtime) come with the Thingino
  firmware the binary runs on and are used as the system's own libraries; they
  are listed above for completeness rather than shipped here.
- The binary is not distributed inside a device or firmware image. Users copy
  it onto a camera they already run Thingino on and can modify freely, so no
  Installation Information beyond these instructions is involved.
- `patches/thingino-ffmpeg-rtmp-chunk-size.diff`, which does change FFmpeg's
  source, was added after v1.1.0 and is not in any released binary yet. From
  the first release that includes it, that patch (at the release's tag) becomes
  part of the corresponding source and the release notes will say so.
- Each release carries FFmpeg's `LICENSE.md` and `COPYING.GPLv3` as assets.

## Not distributed here

- **prudynt binaries.** prudynt links against Ingenic's SDK (libimp and
  friends, Ingenic's proprietary libraries). This repository ships patches and
  [build instructions](docs/build.md) only, never a built prudynt.
- **Thingino's files.** The installers edit `/var/www/a/plugins.js` and
  `/var/www/a/plugins/prudynt.webui.json` on the camera; the files themselves
  are not distributed.
- **Bootstrap, Bootstrap Icons (MIT) and the Montserrat font (Google Fonts,
  OFL).** The web UI pages reference them from CDNs, like Thingino's own
  pages; nothing is bundled.

## Upstream licenses referred to

- Thingino firmware — MIT — [LICENSES/Thingino-LICENSE.txt](LICENSES/Thingino-LICENSE.txt) (copy from `da40db6`)
- FFmpeg — LGPL-2.1-or-later / GPL — [LICENSES/FFmpeg-LICENSE.md](LICENSES/FFmpeg-LICENSE.md) (copy of `LICENSE.md` from 8.0.1)
- mbedTLS 3.6.6 — Apache-2.0 OR GPL-2.0-or-later — https://github.com/Mbed-TLS/mbedtls/blob/mbedtls-3.6.6/LICENSE
- prudynt-t — no license published — https://github.com/themactep/prudynt-t (fork built by thingino-firmware), https://github.com/gtxaspec/prudynt-t (origin)
- Go — BSD-3-Clause — https://go.dev/LICENSE
