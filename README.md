# wzc-yt — Wyze Cam v3 (Thingino) → YouTube Live 直接配信用 FFmpeg ビルド

> **English summary**: Build recipe for a minimal FFmpeg binary (2.5MB) that runs on a
> Wyze Cam v3 flashed with [Thingino](https://thingino.com/), letting the camera push its
> H.264/AAC stream **directly to YouTube Live over RTMPS** — no relay PC required.
> Pure stream copy (no re-encoding): measured load on the Ingenic T31 is ~3.3% CPU / 3.3MB RSS.
> See [NOTES.md](NOTES.md) for implementation details (Japanese).

Thingino 化した Wyze Cam v3 から、中継マシンなしで YouTube Live へ直接配信するための
minimal FFmpeg バイナリと、その周辺ツールです。

## 典型的な使い方

[Releases](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases) からバイナリを取得し、
カメラに転送して実行するだけです:

```sh
# PC 側: ダウンロードしてリネームし、カメラへ転送
mv ffmpeg-thingino-wyze-cam3-t31-mipsel ffmpeg
cat ffmpeg | ssh root@<camera-ip> 'cat > /tmp/ffmpeg && chmod +x /tmp/ffmpeg'

# カメラ側: これだけで YouTube Live に配信が始まる
/tmp/ffmpeg -rtsp_transport tcp \
  -i 'rtsp://thingino:thingino@127.0.0.1:554/ch0' \
  -c copy -f flv \
  'rtmps://a.rtmps.youtube.com:443/live2/{KEY}'
```

- `{KEY}` は YouTube Studio のライブ配信設定にあるストリームキー
- RTSP の認証 (`thingino:thingino`) とパス (`/ch0`) は Thingino のデフォルト。変更していれば合わせる
- 再エンコードなし (stream copy) なので、カメラの負荷は CPU 約3% / RAM 3MB 程度
- `/tmp` は再起動で消える。常設・自動起動・自動復帰したくなったら
  [device/](device/) の supervisor を導入する (おまけ)

以降のビルド手順や起動スクリプトは、自分でビルドしたい人・常設運用したい人向けのおまけです。

```text
従来:  Wyze Cam v3 → RTSP → 中継PC (ffmpeg) → RTMPS → YouTube Live
これ:  Wyze Cam v3 (ffmpeg入り) ──────────── RTMPS → YouTube Live
```

カメラ内部では以下を行います。再エンコードは一切しません:

```text
localhost RTSP (prudynt) → H.264/AAC stream copy → FLV mux → RTMPS/TLS → YouTube Live
```

## リポジトリ構成

```text
patches/   このリポジトリの本体。Thingino の thingino-ffmpeg パッケージへ当てる差分
device/    カメラに置くファイル (supervisor スクリプト・init スクリプト・設定例)
NOTES.md   実装の詳細・設計判断・ハマりどころの記録
dist/      ビルド成果物 (git 管理外)。配布は GitHub Releases の tarball で行う
```

`thingino-firmware/` (Thingino のソースツリー) はビルド時にこの直下へ clone しますが、
git 管理外です。必要な変更はすべて `patches/` に分離してあります。

## 特徴

- **バイナリ 2.5MB** (stripped)。FFmpeg 8.0.1 を RTSP 入力 + FLV/RTMPS 出力だけに絞った構成
- **stream copy 専用** — encoder/decoder/filter を一切含まない
- **TLS は mbedTLS** — Thingino 実機に入っている `libmbedtls.so.3.6.6` に動的リンク
- **ファームウェア書き換え不要** — `/tmp` に転送して実行するだけ (PoC 用途)
- **SD カードが物理スイッチになるスタンドアロンモード** — 設定 (ストリームキー) を書いた
  SD を挿すと配信開始、抜くと停止 ([device/](device/) の supervisor が提供)
- 実測負荷: ffmpeg CPU **3.3%** / RSS **3.3MB**(720p15 / ~330kbps 配信時、CPU idle 81%→73%)

## 動作確認環境

| 項目 | 値 |
|---|---|
| カメラ | Wyze Cam v3 (Ingenic T31X, GC2053, ATBM6031) |
| ファームウェア | Thingino `ciao+c334a03` (2026-08-02) |
| ABI | mipsel / MIPS32 o32 / hard-float / uClibc-ng 1.0.57 |
| ビルドホスト | x86_64 Linux + Docker |

> **注意**: バイナリは実機ファームと同じ toolchain 世代 (GCC15) ・同じ mbedTLS soname に
> 依存します。別バージョンの Thingino では、実機の `/usr/lib/libmbed*` を確認の上、
> 対応するコミットでビルドし直してください (手順は同じです)。

## ビルド手順

必要なもの: Docker が使える x86_64 Linux、ディスク ~10GB。

```sh
# 1. Thingino を実機ファームと同じコミットで取得
git clone https://github.com/themactep/thingino-firmware
cd thingino-firmware
git checkout c334a03
git submodule update --init          # buildroot をピン位置で checkout

# 2. RTMPS 対応パッチを適用
git apply ../patches/thingino-ffmpeg-rtmps.diff

# 3. 公式ビルダーイメージと DL キャッシュを取得
WORKFLOW=1 make -f Makefile.container container-pull

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

## カメラでの使い方

まず `/tmp` で動作確認する (**`/tmp` は RAM 上なので再起動で消える = PoC 専用**。
常設する場合は overlay で永続化される `/usr/bin/ffmpeg` へ —
場所の選び方と手順は [device/README.md](device/README.md) 参照):

```sh
# 転送 (Thingino は SFTP 非対応なので -O が必要。だめなら ssh + cat)
scp -O ffmpeg root@<camera-ip>:/tmp/
#   または: cat ffmpeg | ssh root@<camera-ip> 'cat > /tmp/ffmpeg && chmod +x /tmp/ffmpeg'

# 実機で起動確認
/tmp/ffmpeg -version
# もし libatomic.so.1 が無いというエラーが出たら(標準の Thingino には入っています):
#   ビルド出力の per-package/.../target/usr/lib/libatomic.so.1.2.0 を /tmp に転送し
#   ln -s /tmp/libatomic.so.1.2.0 /tmp/libatomic.so.1
#   LD_LIBRARY_PATH=/tmp /tmp/ffmpeg -version

# YouTube Live へ配信
/tmp/ffmpeg -rtsp_transport tcp \
  -i 'rtsp://thingino:thingino@127.0.0.1:554/ch0' \
  -c copy -f flv \
  'rtmps://a.rtmps.youtube.com:443/live2/<STREAM_KEY>' \
  -loglevel error
```

- RTSP の認証・パスは Thingino のデフォルト (`thingino:thingino`, `/ch0`)。環境に合わせて変更
- `/tmp` は RAM 上なので再起動で消えます (PoC 用途としては安全)
- `Invalid DTS ... replacing by guess` 警告は prudynt 側のタイムスタンプ癖によるもので実害なし
  (`-loglevel error` で抑制)

## 補足

- **ライセンス**: この構成の FFmpeg バイナリは `--enable-gpl --enable-version3 --enable-mbedtls`
  でビルドされるため **LGPL v3 / GPL v3** 相当です。バイナリ配布時は FFmpeg / mbedTLS の
  ライセンス表記に従ってください
- **TLS 証明書検証**: FFmpeg 8.x のデフォルトでは無効です (通信は暗号化されます)。
  検証したい場合は CA バンドルを置き `-tls_verify 1 -ca_file <path>` を付けてください
- **ストリームキー**: シェル履歴やログに残ります。露出した場合は YouTube Studio で再生成を
- 実装の詳細・経緯・ハマりどころは [NOTES.md](NOTES.md) を参照

## ステータス / ロードマップ

- [x] minimal FFmpeg ビルド (RTSP → FLV/RTMPS, mbedTLS)
- [x] QEMU 検証・実機動作確認・YouTube Live 配信成功
- [x] 負荷測定 (prudynt / ISP 処理への影響なしを確認)
- [x] 自動再起動 (supervisor) スクリプト — [device/](device/) 参照
  (SD カード設定 + 再起動からの自動配信開始を実機確認済み)
- [x] 長時間安定性試験 — 約4時間の連続配信でリーク・劣化・A/V ズレなし
- [ ] Thingino パッケージとしての統合 / ファームウェア組み込み
  (将来的には Thingino の新ストリーマ [Raptor](https://github.com/gtxaspec/raptor) の
  RTMPS push 機能 (RSP) への移行も選択肢)

## 謝辞

- [Thingino](https://github.com/themactep/thingino-firmware) — オープンな IP カメラファームウェアと
  クロスビルド環境
- [FFmpeg](https://ffmpeg.org/)
