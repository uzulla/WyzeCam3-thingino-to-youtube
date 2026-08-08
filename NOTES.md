# 実装ノート: Wyze Cam v3 (Thingino) から YouTube Live へ直接 RTMPS 配信

2026-08-08 実施。構想から実機 PoC 成功までの記録。
「同じことをもう一度やる人」が迷わないための、判断理由・ハマりどころ・実測値のまとめ。

## 経緯と仮説

元の構成は「カメラ → RTSP → 中継マシン (N100) 上の FFmpeg → RTMPS → YouTube Live」。
中継側の FFmpeg は `-c:v copy -c:a copy` であり、行っている処理は
RTSP 受信 / demux / packet 受け渡し / FLV mux / RTMP / TLS / TCP 送信だけで、
**映像・音声の decode / encode は存在しない**。

「この程度なら Thingino を動かしている Ingenic T31 自身で処理できるのではないか」
というのが出発点。狙いは中継マシンを不要にして車載などの構成を単純化すること。

実機の事前調査で分かっていた前提:

- Thingino `BUILD_ID="ciao+c334a03"` / `TOOLCHAIN_GCC=15` / uClibc / SoC T31 (xburst1)
- `/proc/cpuinfo` は `mips32r1` (Ingenic XBurst)
- `/proc/crypto` は `aes-generic` / `sha256-generic` のみ — **ハードウェア AES は Linux から
  認識されていない**。よってソフトウェア TLS 前提で設計し、実測で問題ないことを確認する方針とした
  (結果: 数百 kbps の映像なら ffmpeg 全体で CPU 3.3%、杞憂だった)
- baseline 負荷: CPU idle 約81%、prudynt 約8%、RAM free 約57MB → 余力は十分にあった
- 実機に openssl CLI は無いが、それと「FFmpeg が TLS を使えるか」は別問題
  (mbedTLS をリンクすればよい)

方針の優先順位 (当初から一貫):

```text
1. 既存 Thingino ビルド環境を壊さない
2. ファームウェアを書き換えず PoC する
3. 既存 thingino-ffmpeg を最大限再利用する
4. H.264/AAC は絶対に再エンコードしない
5. minimal build にする
6. 実測で CPU/RAM を見る
7. 成立した後にファームウェアへ統合する
```

## 結果サマリ

Wyze Cam v3 (Ingenic T31X, MIPS32 XBurst1, RAM 128MB, uClibc) 単体から、
RTSP → H.264/AAC stream copy → FLV → RTMPS → YouTube Live の直接配信に成功。

| 項目 | Baseline | 配信中 |
|---|---|---|
| CPU idle | 81.3% | 73.4% |
| ffmpeg CPU | — | 3.3% |
| ffmpeg RSS | — | 3.3MB |
| prudynt CPU | 8.0% | 8.8% (影響なし) |
| RAM free | 57MB | 52.9MB |

- バイナリ: FFmpeg 8.0.1 minimal build、**2.5MB (stripped, 動的リンク)**
- ハードウェア AES なしのソフト TLS (mbedTLS) でも CPU 3.3%。**AES 負荷は杞憂だった**
  (映像ビットレート ~330kbps 程度なら全く問題にならない)

---

## 設計判断とその理由

### 1. ベースコミットは実機ファームに合わせて固定する

実機の `/etc/os-release` の `BUILD_ID="ciao+c334a03"` から Thingino の commit `c334a03` を特定し、
**master ではなくそのコミットで checkout** した。

理由: master はビルド当時すでにデフォルト toolchain が **GCC15 → GCC16** に移行済みだった。
私の実機ファームは GCC15 (uClibc-ng 1.0.57) でビルドされており、動的リンクバイナリを
既存ファームのライブラリに相乗りさせる以上、toolchain 世代は実機と揃えるのが安全。

buildroot は Thingino の git submodule (`313414b` にピン)。注意点として、Thingino の
Makefile には `git submodule update --remote` を実行するターゲットがあり、
うっかり踏むと submodule が最新 master に動く。先に `git submodule update --init` で
ピン位置に checkout しておけば、`buildroot/Makefile` が存在する限り再実行されない。

### 2. 実機 ABI の確認事項

| 項目 | 値 | 確認方法 |
|---|---|---|
| エンディアン | little (mipsel) | toolchain defconfig `BR2_mipsel=y` |
| ABI | o32, FP32, legacy NaN | `BR2_MIPS_OABI32=y` 等 |
| float | hard-float | `BR2_MIPS_SOFT_FLOAT` 未設定 |
| libc | uClibc-ng 1.0.57 | buildroot pin の uclibc.mk |
| dynamic linker | `/lib/ld-uClibc.so.0` | readelf -l |
| ISA | `--cpu=mips32r2` でビルド | thingino-ffmpeg.mk の既定値 |

`/proc/cpuinfo` は `mips32r1` を名乗るが、Thingino は全パッケージを XBurst バリアント
(実質 r2 命令を含む) でビルドしており実績があるため、`--cpu=mips32r2` のままで問題なかった。

### 3. 動的リンクを選んだ理由

実機に **mbedTLS 3.6.6 の共有ライブラリが最初から入っている**ことを確認できたため
(`libmbedtls.so.21` / `libmbedx509.so.7` / `libmbedcrypto.so.16`)。
buildroot で同じ 3.6.6 をビルドしたところ soname も完全一致した。

- 転送物が ffmpeg 本体 2.5MB だけで済む
- FFmpeg 自身のライブラリ (libav*) は `--disable-shared --enable-static` でバイナリに内蔵
  (この 2 オプションは FFmpeg 自身のライブラリの話で、外部依存 (mbedTLS) は共有のままリンクされる)

**事前に実機で `ls /usr/lib/ | grep -i mbed` を必ず確認すること。** soname が違う
ファームでは起動しない。その場合は mbedTLS を静的リンクに切り替える (+300-600KB)。

`libatomic.so.1` も NEEDED に入るが、Thingino 実機には存在した。念のため成果物に同梱。

---

## FFmpeg minimal build の知見

### thingino-ffmpeg パッケージへの差分 (patches/thingino-ffmpeg-rtmps.diff)

既存の `package/thingino-ffmpeg/` (FFmpeg 8.0.1, RTSP→MP4 録画用の minimal 構成) に対し:

```makefile
THINGINO_FFMPEG_MUXERS += flv
THINGINO_FFMPEG_CONF_OPTS += \
	--enable-protocol=tls \
	--enable-protocol=rtmp \
	--enable-protocol=rtmps \
	--enable-bsf=extract_extradata \
	--enable-mbedtls \
	--extra-libs="-lmbedx509 -lmbedcrypto"
THINGINO_FFMPEG_DEPENDENCIES += mbedtls
```

+ 既存の typo 修正: `--enable-bsf=aac_adtastoasc` → `aac_adtstoasc` (upstream 報告価値あり)。

### ハマり1: mbedTLS のリンクで `DSO missing from command line`

configure は通るのに最終リンクで
`tls_mbedtls.o: undefined reference to 'mbedtls_x509_crt_init'` で失敗する。

原因: mbedtls.pc は `Libs: -lmbedtls` しか公開せず、x509/crypto は `Requires.private`。
動的リンク時の pkg-config はこれを展開しないため、`-lmbedx509 -lmbedcrypto` が
リンク行に乗らない。FFmpeg の configure テストは `mbedtls_ssl_init` (libmbedtls 内) で
成功してしまうので、失敗が最終リンクまで顕在化しない。

対策: `--extra-libs="-lmbedx509 -lmbedcrypto"` を明示。

### ハマり2: FFmpeg の依存グラフは「無効化したつもり」を上書きする

configure の `_select` はユーザーの `--disable` より強い。今回の構成で観測した事実:

- **rtsp demuxer** → `rtpdec` → **asf/mov/mpegts/rm demuxer, rtp/udp protocol, srtp, http protocol を強制リンク**。
  `-rtsp_transport tcp` 運用でも udp はバイナリから消せない (実行時に使われないだけ)。
  合計 ~180KB 程度なので削るためにパッチを書く価値はない。
- **flv muxer** → `aac_adtstoasc_bsf` を自動 select (明示は保険)。
- `h264_mp4toannexb` は RTSP→FLV では**不要** (方向が逆。FLV muxer が Annex B→AVCC 変換を内蔵)。
- **ffmpeg CLI** は stream copy 専用でも `avfilter` を要求する。swscale/swresample は不要。

### `--enable-bsf=extract_extradata` を明示する理由

FLV muxer は H.264 の extradata (SPS/PPS) が空だと `extract_extradata` bsf を挿入しようと
するが、この bsf は自動 select **されない**。通常は SDP の `sprop-parameter-sets` から
extradata が作られるので不要だが、sprop を出さないカメラだと bsf 不在時に mux が即失敗する。
+50KB 程度の保険として明示的に組み込んだ。

### バージョン選定: 8.0.x を使う

- **7.1.x には minimal build 時のリンクバグ**がある (`ff_aom_uninit_film_grain_params`
  未定義。`aom_film_grain.o` が `CONFIG_HEVC_SEI` にしか紐付いていない)。回避は
  `--enable-parser=hevc` を足すこと。8.0 で修正済み。
- mbedTLS 3.6 系 (TLS1.3/PSA) に必要なコードは **7.1 以降**にしかない。7.0 以前は不可。
- 8.x では `--disable-postproc` オプション自体が削除されている (libpostproc 消滅)。
  7.x のレシピを流用すると configure が落ちる。
- TLS 証明書検証は 8.x まで**デフォルト無効** (`tls_verify=0`)。9.0 でデフォルト有効に
  変わる予定なので、将来バージョンを上げる際は CA バンドルの用意が必要になる。

### ライセンス

`--enable-mbedtls` は `--enable-version3` が必須 (mbedTLS が Apache-2.0 のため)。
結果のバイナリは **LGPL v3** 相当。thingino-ffmpeg は元から version3 有効なので追加対応不要。

---

## Thingino ビルドシステムの知見

### 単一パッケージだけビルドする

フルファームウェアのビルドは不要。以下だけで ffmpeg + 依存 (external toolchain, mbedtls) が育つ:

```sh
make CAMERA=wyze_cam3_t31x_gc2053_atbm6031 br-thingino-ffmpeg
# .mk を変更したら:
make CAMERA=... br-thingino-ffmpeg-dirclean br-thingino-ffmpeg
```

- toolchain はプリビルトが GitHub Releases から自動ダウンロードされる
  (`thingino-toolchain-x86_64_xburst1_uclibc_gcc15-linux-mipsel.tar.gz`)
- 成果物: `output/HEAD/<camera>-3.10.14-uclibc/per-package/thingino-ffmpeg/target/usr/bin/ffmpeg`
- **per-package/thingino-ffmpeg/target/ はそれ自体が完結したミニ sysroot**
  (ld-uClibc, libc, mbedtls, libatomic 入り) — QEMU 検証にそのまま使える

### Docker ビルド

公式ビルダーイメージ `ghcr.io/themactep/thingino-builder-image` + DL キャッシュイメージ
`ghcr.io/themactep/thingino-dl` (volumes-from で `/dl` をマウント) を使用。

**ハマり3: DL キャッシュボリュームは root 所有。** 一般ユーザーでビルドすると
キャッシュにないソース (今回は ffmpeg-8.0.1.tar.xz) のダウンロードで
`mkdir: Permission denied`。事前に root で `chmod -R a+rwX /dl` しておく。

**ハマり4: ビルダーイメージの `install` は uutils 版**で buildroot が誤動作するため、
コンテナ起動毎に `sudo update-alternatives --install /usr/bin/install install /usr/bin/gnuinstall 100`
が必要 (公式の Makefile.container もこれをやっている)。

### QEMU での実機前検証

ビルダーイメージに qemu は**入っていない**。`debian:stable-slim` + `qemu-user-static` で:

```sh
qemu-mipsel-static -L <per-package/thingino-ffmpeg/target> \
  <同target>/usr/bin/ffmpeg -protocols
```

実機に触る前に確認できたこと:
1. `-protocols` / `-muxers` / `-bsfs` に rtmps / flv / aac_adtstoasc が出る
2. H.264+AAC の MP4 → FLV stream copy が成功し ffprobe で正当性確認できる
3. **YouTube の実エンドポイントに無効キーで接続**すると、TLS handshake → RTMP handshake
   (Server version 4.0.0.1) → publish 送信 → サーバ切断、まで到達する。
   つまり「TLS スタックが本物の YouTube と話せる」ことはキーなしで検証可能。

---

## 実機での知見

### 転送: scp はそのままでは使えない

Thingino には `/usr/libexec/sftp-server` がなく、新しめの OpenSSH の scp (SFTP モード) は失敗する。

```sh
scp -O ffmpeg root@cam:/tmp/          # レガシープロトコル
```

転送後は `md5sum` で照合。`/tmp` は tmpfs なので再起動で消える (PoC には好都合)。

### prudynt の RTSP はビデオ DTS が不正 (実害なし)

`Invalid DTS: 67 PTS: 0, replacing by guess` が毎ビデオパケットに出る。
prudynt が PTS より進んだ DTS を付けてくるためで、FFmpeg が自動補正する。
B フレームなしの H.264 なので DTS=PTS 補正で正しく、FLV 出力・YouTube 配信とも正常。
本番では `-loglevel error` でログを抑制する。

### 実行コマンド (確定版)

```sh
/tmp/ffmpeg -rtsp_transport tcp \
  -i 'rtsp://thingino:thingino@127.0.0.1:554/ch0' \
  -c copy -f flv \
  'rtmps://a.rtmps.youtube.com:443/live2/<STREAM_KEY>' \
  -loglevel error
```

- RTSP 認証は Thingino デフォルト `thingino:thingino`、パス `/ch0` (メイン) / `/ch1` (サブ)
- ストリームキーはシェル履歴・ログ・チャットに残る。**露出したら YouTube Studio で再生成**

---

## 残タスク

- [x] 長時間試験 — **約4時間の連続配信で完走、リーク・劣化なし**
  (1時間時点の計測: ffmpeg RSS 3312K→3324K (+12KB)、CPU 3.3%→3.2%、
  free RAM 変化なし、A/V ズレなし。以降も問題なく4時間で試験完了とした)
- [x] supervisor 実機動作確認 — SD カードに設定を置いて再起動 →
  **自動マウント → 設定検出 → 自動配信開始** まで実機で成功 (スタンドアロン動作の実証)
- [ ] supervisor スクリプト — 切断/Wi-Fi 断からの自動再起動 (Thingino init script 形式)
- [ ] Thingino パッケージとしての統合 (Config.in オプション化、stream key の安全な保持)
- [ ] typo 修正 (`aac_adtastoasc`) の upstream PR
