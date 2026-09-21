# wzc-yt — Wyze Cam v3 (Thingino) → YouTube Live 直接配信用 FFmpeg ビルド

> **English summary**: Build recipe for a minimal FFmpeg binary (2.5MB) that runs on a
> Wyze Cam v3 flashed with [Thingino](https://thingino.com/), letting the camera push its
> H.264/AAC stream **directly to YouTube Live over RTMPS** — no relay PC required.
> Pure stream copy (no re-encoding): measured load on the Ingenic T31 is 3-10% CPU depending on
> bitrate (3.3% at 330kbps, 4.8% at 1Mbps, ~10% at 1080p25 / 2.1Mbps) and ~3.4MB RSS.
> Binaries are specific to a Thingino build; pick the Release matching your camera's `BUILD_ID`.
> See [NOTES.md](NOTES.md) for implementation details (Japanese).

Thingino 化した Wyze Cam v3 から、中継マシンなしで YouTube Live へ直接配信するための
minimal FFmpeg バイナリと、その周辺ツールです。

## 典型的な使い方

[Releases](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases) からバイナリを取得し、
カメラに転送して実行するだけです。**バイナリはカメラの Thingino のビルドごとに別物**なので、
カメラで `grep BUILD_ID /etc/os-release` を見て、対応する Release のものを使ってください
(違うものは起動しません):

| カメラの `BUILD_ID` | Release |
|---|---|
| `ciao+da40db6` (2026-09-14) | [v1.1.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.1.0) |
| `ciao+c334a03` (2026-08-01) | [v1.0.0](https://github.com/uzulla/WyzeCam3-thingino-to-youtube/releases/tag/v1.0.0) |
| それ以外 | 下の「ビルド手順」で、その commit に合わせて自分でビルドする |

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
- 再エンコードなし (stream copy) なので、カメラの負荷は CPU 3〜10% (ビットレート次第) / RAM 3MB 程度
- `/tmp` は再起動で消える。常設・自動起動・自動復帰したくなったら
  [device/](device/) の supervisor を導入する (おまけ)
- **2026-09 以降の Thingino は、ゲートウェイへの ping が約 90 秒通らないとカメラを OS ごと
  再起動する (netwatch、デフォルト有効)。** 配信が切れるので、長時間配信する前に無効化しておく:

  ```sh
  device/disable-netwatch.sh root@<camera-ip>
  # カメラ上で直接やるなら: jct /etc/thingino.json set netwatch.enabled false && service restart netwatch
  ```

  Thingino の OS 設定を変えるものなので、ffmpeg やスクリプトのインストールとは別の手順にしてある。
  理由と代償 (Wi-Fi が固まっても自動復旧しなくなる) は [device/README.md](device/README.md) の netwatch 節

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
patches/   このリポジトリの本体。Thingino に当てる差分
             thingino-ffmpeg-rtmps.diff         thingino-ffmpeg パッケージを RTMPS 対応にする
             prudynt-osd-textfile.diff          任意: prudynt (ストリーマ) に「テキストファイルを映像へ重ねる OSD」を足す
device/    カメラ側の常設運用一式 (詳細は device/README.md)
             youtube-relay / S93youtube-relay   supervisor と起動スクリプト
             install.sh                         上記と ffmpeg をカメラへ入れる (自前のファイルを置くだけ)
             disable-netwatch.sh                Thingino の netwatch (OS 自動再起動) を無効化する
             install-wifi-from-sd.sh / S37wifi-from-sd
                                                任意: SD の wpa_supplicant.conf (複数 Wi-Fi 可) を起動時に適用
             install-prudynt-osd.sh / S30prudynt-osd / osd-progress-demo
                                                任意: 上記 OSD パッチ入りの prudynt を入れる、操作側のサンプル
             osd-config / S93osd-config / prudynt-osd.json.example
                                                任意: OSD の設定を SD カードのファイルから読んで prudynt に送り直す
             common.sh                          対応ファームの判定 (違えば何も変更せず中止)
NOTES.md   実装の詳細・設計判断・ハマりどころ・実測値の記録 (手順は書かない)
dist/      ビルド成果物 (git 管理外)。配布は GitHub Releases (ffmpeg バイナリ単体) で行う
```

OS (Thingino) の更新・バックアップ・設定変更と、このリポジトリの成果物のインストールは分けてあります。
`install.sh` は自前のファイルを置くだけで、OS の設定を変えるもの (`disable-netwatch.sh`、
`install-wifi-from-sd.sh`) は別のスクリプトです。

`thingino-firmware/` (Thingino のソースツリー) はビルド時にこの直下へ clone しますが、
git 管理外です。必要な変更はすべて `patches/` に分離してあります。

## 特徴

- **バイナリ 2.5MB** (stripped)。FFmpeg 8.0.1 を RTSP 入力 + FLV/RTMPS 出力だけに絞った構成
- **stream copy 専用** — encoder/decoder/filter を一切含まない
- **TLS は mbedTLS** — Thingino 実機に入っている `libmbedtls.so.3.6.6` に動的リンク
- **ファームウェア世代ごとにビルドが必要** — toolchain (GCC / uClibc) を実機に揃えるため。
  対応表は下記「動作確認環境」
- **ファームウェア書き換え不要** — `/tmp` に転送して実行するだけ (PoC 用途)
- **SD カードが物理スイッチになるスタンドアロンモード** — 設定 (ストリームキー) を書いた
  SD を挿すと配信開始、抜くと停止 ([device/](device/) の supervisor が提供)
- 実測負荷: ffmpeg CPU **3.3%** / RSS **3.3MB**(720p15 / ~330kbps 配信時、CPU idle 81%→73%)。
  TLS の負荷はビットレートにほぼ比例する: 720p10 / 1Mbps で 4.8%、1080p25 / 2.1Mbps で約 10%
  (2026-09 以降の Thingino は既定が 1080p25 / 約 2.1Mbps)

## 動作確認環境

| 項目 | 値 |
|---|---|
| カメラ | Wyze Cam v3 (Ingenic T31X, GC2053, ATBM6031) |
| ファームウェア | Thingino `ciao+da40db6` (2026-09-14) — 下表参照 |
| ABI | mipsel / MIPS32 o32 / hard-float / uClibc-ng |
| ビルドホスト | x86_64 Linux + Docker |

| Thingino | toolchain | uClibc-ng | mbedTLS | Release | 状態 |
|---|---|---|---|---|---|
| `ciao+c334a03` (2026-08-01) | GCC 15 | 1.0.57 | 3.6.6 | v1.0.0 | 実機で長時間配信まで確認済み |
| `ciao+da40db6` (2026-09-14) | GCC 16.2 | 1.0.59 | 3.6.6 | v1.1.0 | 実機で長時間配信まで確認済み。`device/` のスクリプトはこのビルド専用 |

> **注意**: バイナリは実機ファームと同じ toolchain 世代・同じ mbedTLS soname に依存します。
> 実機の `/etc/os-release` の `BUILD_ID` (`ciao+<commit>`) と `TOOLCHAIN_GCC`、および
> `ls /usr/lib | grep mbed` を確認の上、**その commit でビルドし直してください** (手順は同じです)。
> パッチ (`patches/`) は上記どちらの commit にも無修正で当たります。

## ビルド手順

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

## (任意) 映像に任意のテキストを重ねる — prudynt の OSD パッチ

配信映像に、時刻以外の**任意の複数行テキスト** (プログレスバーなどの ASCII アート) を 3 か所まで
(既定は左下・右上・右下) 焼き込み、
カメラ上の別のプログラムから 0.5 秒単位で更新できるようにするパッチです。ffmpeg は stream copy
なので、文字を載せられるのはエンコーダより前の prudynt (Thingino のストリーマ) だけです。
標準の prudynt の burn-in OSD は時刻表示専用 (1 行・1 秒更新・大文字と数字だけの 5x7 フォント) なので、
`patches/prudynt-osd-textfile.diff` で「tmpfs 上のテキストファイルの中身を映す OSD リージョン」を足します。
設計と実測値は [NOTES.md](NOTES.md) の「OSD テキストオーバーレイ」、経緯は #17 / #19。

```sh
# 操作側 (カメラ上) がやることは「一時ファイルに書いて mv で置き換える」だけ
printf 'UPLOAD job-42\n[##########----------] 50%%\n' > /run/prudynt/osd-text.tmp \
  && mv /run/prudynt/osd-text.tmp /run/prudynt/osd-text
```

### ビルド

上の「ビルド手順」の 1〜4 を済ませた `thingino-firmware/` で:

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

### カメラへ入れる・使う

ファームの焼き直しは不要です。手順と設定項目は [device/README.md](device/README.md) の
「OSD テキストオーバーレイ」を参照:

```sh
device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt   # prudynt が再起動し、配信が数秒切れる
ssh root@<camera-ip> osd-progress-demo 20                        # プログレスバーのデモ
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
/tmp/ffmpeg -loglevel error -rtsp_transport tcp \
  -i 'rtsp://thingino:thingino@127.0.0.1:554/ch0' \
  -c copy -f flv \
  'rtmps://a.rtmps.youtube.com:443/live2/<STREAM_KEY>'
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
- [x] 長時間安定性試験 — 約4時間の連続配信でリーク・劣化・A/V ズレなし (`c334a03`)
- [x] Thingino `ciao+da40db6` (GCC16) への追従 — 再ビルド、実機で YouTube Live 配信を確認
- [x] インストーラ (`install.sh`)、netwatch 無効化、SD カードからの Wi-Fi 設定 (複数可)
- [x] `install.sh` / `disable-netwatch.sh` の実機確認 (`da40db6`。新規インストール、配信中の再実行、
  netwatch 無効化後の状態)
- [x] `da40db6` での長時間試験
- [x] 映像に任意のテキストを重ねる prudynt の OSD パッチ (#17、3 か所化 #19) — 実機で 0.5 秒更新・1 時間連続・再起動後の自動有効化・OSD プールの上限まで確認
- [ ] `S37wifi-from-sd` の実機確認
- [ ] Thingino パッケージとしての統合 / ファームウェア組み込み
  (将来的には Thingino の新ストリーマ [Raptor](https://github.com/gtxaspec/raptor) の
  RTMPS push 機能 (RSP) への移行も選択肢)

## 謝辞

- [Thingino](https://github.com/themactep/thingino-firmware) — オープンな IP カメラファームウェアと
  クロスビルド環境
- [FFmpeg](https://ffmpeg.org/)
