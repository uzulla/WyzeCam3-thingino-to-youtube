# ビルド手順

`thingino-firmware/` (Thingino のソースツリー、git 管理外) を**このリポジトリの直下**に clone して行う。
手順中の `../patches/...` はその前提のパス。

## ffmpeg (RTMPS 対応の minimal build)

必要なもの: Docker が使える x86_64 Linux、ディスク ~10GB。

```sh
# 1. Thingino を実機ファームと同じコミットで取得
#    (実機の BUILD_ID="ciao+da40db6, ..." の da40db6 の部分。リリースは master ではなく
#     ciao ブランチから作られており、master の HEAD とは別系統なので必ず commit を指定する)
git clone --branch ciao https://github.com/themactep/thingino-firmware
cd thingino-firmware
git checkout da40db6
git submodule update --init          # buildroot をピン位置で checkout

# 2. RTMPS 対応パッチを適用
git apply ../patches/thingino-ffmpeg-rtmps.diff

# 3. 公式ビルダーイメージと DL キャッシュを取得
WORKFLOW=1 make -f Makefile.container container-pull
#    以前のイメージがローカルに残っていると container-pull は更新しない。古いままだと
#    "Dependency check failed" (libgmp-dev / python3-gmpy2 不足) になるので明示的に更新する
docker pull ghcr.io/themactep/thingino-builder-image:latest

# 4. DL キャッシュボリュームを書き込み可能にする(root所有のため。初回のみ)
docker run --rm --user 0:0 --volumes-from thingino-dl-cache \
  ghcr.io/themactep/thingino-builder-image:latest chmod -R a+rwX /dl

# 5. ffmpeg パッケージだけビルド(toolchain と mbedtls は自動で用意される)
docker run --rm --user $(id -u):$(id -g) --network=host \
  --volumes-from thingino-dl-cache \
  -v "$PWD":/workspace -v "$PWD/overrides":/overrides -w /workspace \
  -e BR2_DL_DIR=/dl \
  ghcr.io/themactep/thingino-builder-image:latest \
  bash -c "sudo update-alternatives --install /usr/bin/install install /usr/bin/gnuinstall 100 2>/dev/null; \
           make CAMERA=wyze_cam3_t31x_gc2053_atbm6031 br-thingino-ffmpeg"
```

成果物:

```text
output/HEAD/wyze_cam3_t31x_gc2053_atbm6031-3.10.14-uclibc/per-package/thingino-ffmpeg/target/usr/bin/ffmpeg
```

### (任意) 実機に入れる前に QEMU で検証

```sh
SYSROOT=output/HEAD/wyze_cam3_t31x_gc2053_atbm6031-3.10.14-uclibc/per-package/thingino-ffmpeg/target
docker run --rm -v "$PWD/$SYSROOT":/sysroot:ro debian:stable-slim bash -c \
  "apt-get update -qq && apt-get install -qq -y qemu-user-static >/dev/null && \
   qemu-mipsel-static -L /sysroot /sysroot/usr/bin/ffmpeg -protocols"
# 出力に rtmps / tls が含まれていれば OK
```

## prudynt (OSD テキストオーバーレイのパッチ入り)

上の ffmpeg の手順 1〜4 を済ませた `thingino-firmware/` で (手順 2 の ffmpeg 用パッチは prudynt のビルドには不要だが、当たっていても問題ない):

```sh
# prudynt のソースに当てるパッチは、Buildroot のパッケージディレクトリに置けば自動で適用される
cp ../patches/prudynt-osd-textfile.diff package/prudynt-t/0001-osd-textfile.patch

# フル ASCII の 8x8 フォントを有効にする (既定の 5x7 は大文字・数字と一部の記号だけ)。
# user/ は Thingino のユーザー設定用ディレクトリで、Thingino 側でも git 管理外
mkdir -p user/wyze_cam3_t31x_gc2053_atbm6031
echo 'BR2_PACKAGE_PRUDYNT_T_OSD_FONT_8X8=y' >> user/wyze_cam3_t31x_gc2053_atbm6031/local.fragment

# prudynt パッケージだけビルド (依存パッケージも育つので、初回は ffmpeg より時間がかかる)
docker run --rm --user $(id -u):$(id -g) --network=host \
  --volumes-from thingino-dl-cache \
  -v "$PWD":/workspace -v "$PWD/overrides":/overrides -w /workspace \
  -e BR2_DL_DIR=/dl \
  ghcr.io/themactep/thingino-builder-image:latest \
  bash -c "sudo update-alternatives --install /usr/bin/install install /usr/bin/gnuinstall 100 2>/dev/null; \
           make CAMERA=wyze_cam3_t31x_gc2053_atbm6031 br-prudynt-t"
# パッチやフォント設定を変えた後は br-prudynt-t-dirclean br-prudynt-t
```

成果物: `output/HEAD/wyze_cam3_t31x_gc2053_atbm6031-3.10.14-uclibc/per-package/prudynt-t/target/usr/bin/prudynt`

