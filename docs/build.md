# ビルド手順

ファームウェアイメージ (と、カードに置く ffmpeg) を Thingino のビルド環境で作る手順。必要なものは Docker が使える x86_64 Linux とディスク ~10 GB。
Go のプログラム (lapcam / osd-feed / beacon) は PC の Go だけで作れる ([development.md](development.md#ビルド))。

Thingino のソース木 `thingino-firmware/` は**このリポジトリの直下**に clone して使う (git 管理外)。
必要な変更は全部 `patches/` と `firmware/prepare.sh` にあり、ソース木には手を入れない。

## 1. 準備 (初回だけ)

```sh
# 実機と同じコミット (BUILD_ID ciao+da40db6 の da40db6)。リリースは master ではなく ciao ブランチから作られている
git clone --branch ciao https://github.com/themactep/thingino-firmware
cd thingino-firmware
git checkout da40db6
git submodule update --init          # buildroot をピン位置で checkout

# 公式ビルダーイメージと DL キャッシュ
WORKFLOW=1 make -f Makefile.container container-pull
docker pull ghcr.io/themactep/thingino-builder-image:latest   # 古いイメージが残っていると container-pull は更新しない
#   (古いままだと "Dependency check failed" (libgmp-dev / python3-gmpy2 不足) になる)

# DL キャッシュボリュームを書き込み可能にする (root 所有のため)
docker run --rm --user 0:0 --volumes-from thingino-dl-cache \
  ghcr.io/themactep/thingino-builder-image:latest chmod -R a+rwX /dl
cd ..
```

## 2. ファームウェアイメージを作る

```sh
firmware/build.sh thingino-firmware            # 初回はフルビルド (4 コアで 25 分ほど)
PACK_ONLY=1 firmware/build.sh thingino-firmware   # device/ のスクリプトや overlay だけ変えた後: イメージの組み直しだけ (数秒〜1 分)
```

`firmware/prepare.sh` が `patches/` を当てて `user/<機種>/` (焼き込む設定と overlay) を用意し、ビルダーコンテナの中で Thingino の
ビルドとパッケージングを走らせ、最後に ffmpeg を作る。成果物:

```text
dist/firmware/thingino-wyze_cam3_t31x_gc2053_atbm6031.bin   16 MB のイメージ (フラッシュ全体)
dist/firmware/BUILD                                         BUILD_ID (ciao+da40db6, 日付)
dist/firmware/VERSION                                       イメージに書いた lapcam の版
dist/ffmpeg, dist/ffmpeg.version                            カードに置く ffmpeg (RTMPS 対応、パッチ入り)
```

- 出力先は `output/ciao/<機種>-3.10.14-uclibc/`。prepare.sh がコミット `da40db6` に `ciao` ブランチを付けるため
  (detached のままだと BUILD_ID が `HEAD+da40db6` になり、`device/common.sh` が実機で弾く)
- パッチや `prudynt.json` が変わった時は、prepare.sh が `user/<機種>/.rebuild` に記録し、build.sh が `make rebuild-<pkg>` する
  (Buildroot はパッケージディレクトリの変更を見ない)。この時は `PACK_ONLY=1` では組み直せず、その旨を出して止まるので、
  `PACK_ONLY` を付けずに実行する
- rootfs (squashfs 5696 KB) の空きは約 50 KB。入らなくなるとビルドが失敗する。大きいものは SD カードへ ([sdcard.md](sdcard.md))
- 他機種は `firmware/build.sh thingino-firmware <機種名>` (未検証)

## 3. (任意) 実機に入れる前に ffmpeg を QEMU で確かめる

```sh
SYSROOT=thingino-firmware/output/ciao/wyze_cam3_t31x_gc2053_atbm6031-3.10.14-uclibc/per-package/thingino-ffmpeg/target
docker run --rm -v "$PWD/$SYSROOT":/sysroot:ro debian:stable-slim bash -c \
  "apt-get update -qq && apt-get install -qq -y qemu-user-static >/dev/null && \
   qemu-mipsel-static -L /sysroot /sysroot/usr/bin/ffmpeg -protocols"
# 出力に rtmps / tls が含まれていれば OK
```

## 単品で作る (ファームウェアを焼かず、素の Thingino に手で入れる場合)

上の 1. を済ませた `thingino-firmware/` で、パッケージごとに作る。パッチは手で置く。

```sh
cd thingino-firmware
git apply ../patches/thingino-ffmpeg-rtmps.diff
cp ../patches/thingino-ffmpeg-rtmp-chunk-size.diff package/thingino-ffmpeg/0002-rtmp-chunk-size.patch
cp ../patches/prudynt-osd-textfile.diff        package/prudynt-t/0001-osd-textfile.patch
cp ../patches/prudynt-msgchannel-warning.diff  package/prudynt-t/0002-msgchannel-warning.patch
mkdir -p user/wyze_cam3_t31x_gc2053_atbm6031
echo 'BR2_PACKAGE_PRUDYNT_T_OSD_FONT_8X8=y' >> user/wyze_cam3_t31x_gc2053_atbm6031/local.fragment   # フル ASCII の 8x8 フォント

run() { docker run --rm --user $(id -u):$(id -g) --network=host --volumes-from thingino-dl-cache \
  -v "$PWD":/workspace -v "$PWD/overrides":/overrides -w /workspace -e BR2_DL_DIR=/dl \
  ghcr.io/themactep/thingino-builder-image:latest \
  bash -c "sudo update-alternatives --install /usr/bin/install install /usr/bin/gnuinstall 100 2>/dev/null; make CAMERA=wyze_cam3_t31x_gc2053_atbm6031 $*"; }
run br-thingino-ffmpeg        # -> output/<branch>/…/per-package/thingino-ffmpeg/target/usr/bin/ffmpeg
run br-prudynt-t              # -> output/<branch>/…/per-package/prudynt-t/target/usr/bin/prudynt (依存も育つので初回は長い)
# パッチやフォント設定を変えた後は: run br-prudynt-t-dirclean br-prudynt-t
```

`<branch>` は checkout の状態で `ciao` か `HEAD`。入れ方は [relay.md](relay.md) (ffmpeg) と [osd.md](osd.md) (prudynt)。
