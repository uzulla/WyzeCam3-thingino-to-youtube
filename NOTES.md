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

+ 既存の typo 修正: `--enable-bsf=aac_adtastoasc` → `aac_adtstoasc` (このリポジトリのパッチ内で直している)。

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
/tmp/ffmpeg -loglevel error -rtsp_transport tcp \
  -i 'rtsp://thingino:thingino@127.0.0.1:554/ch0' \
  -c copy -f flv \
  'rtmps://a.rtmps.youtube.com:443/live2/<STREAM_KEY>'
```

- RTSP 認証は Thingino デフォルト `thingino:thingino`、パス `/ch0` (メイン) / `/ch1` (サブ)
- ストリームキーはシェル履歴・ログ・チャットに残る。**露出したら YouTube Studio で再生成**

---

## 2026-09 追記: Thingino `ciao+da40db6` (GCC16) への追従

2026-09-21 実施。実機を `ciao+c334a03` (2026-08-01) から `ciao+da40db6` (2026-09-14) へ
更新するにあたっての再調査。間は 853 コミット。以降の本文は c334a03 時点の記録のまま残し、
差分だけここに書く。

### ビルド側: ほぼ無変更

| 項目 | c334a03 | da40db6 |
|---|---|---|
| toolchain | GCC 15 | **GCC 16.2.0** (`thingino-toolchain-x86_64_xburst1_uclibc_gcc16-linux-mipsel.tar.gz`) |
| uClibc-ng | 1.0.57 | **1.0.59** |
| buildroot pin | `313414b` | `d518030` |
| mbedTLS / soname | 3.6.6 / .21 .7 .16 | 同じ |
| FFmpeg | 8.0.1 | 同じ (`package/thingino-ffmpeg` は 853 コミット間で無変更) |

- パッチは無修正で当たる。upstream の typo (`aac_adtastoasc`) も未修正のまま
- 成果物: 2,604,580 バイト、NEEDED は `libatomic.so.1` `libmbedtls.so.21` `libmbedx509.so.7`
  `libmbedcrypto.so.16` `libc.so.0` で従来と同一
- `br-%` ターゲットは `Makefile` から `Makefile.utils` に移っただけで使い方は同じ
- **`da40db6` は `ciao` ブランチにしか無い。** master と ciao は 2026-05 に分岐しており、
  「master を checkout して BUILD_ID 付近を探す」やり方は通用しない

**ハマり5: ビルダーイメージが古いと `Dependency check failed`。** `scripts/dep_check.sh` が
`libgmp-dev` / `python3-gmpy2` を要求するようになった。最新の builder image には入っているが、
`make -f Makefile.container container-pull` はローカルにイメージがあると pull し直さない。
`docker pull ghcr.io/themactep/thingino-builder-image:latest` で解決。

### mbedTLS の出力バッファが 4KB になった

`3fe80e9dd` (2026-08-06) で `MBEDTLS_SSL_OUT_CONTENT_LEN` が 16KB → 4KB に縮小され、
`mbedtls_ssl_write()` が 4KB 超で部分書き込みを返すようになった。FFmpeg は
`ffurl_write` (`retry_transfer_wrapper`) が全量書き切るまでループするので影響しない見立て。
QEMU では問題なし (下記)。実機での CPU 再計測は未了 (#5)。

QEMU 検証 (新 sysroot = uClibc 1.0.59 + 4KB バッファの mbedTLS):

- `-protocols` に rtmp / rtmps / tls、`-muxers` に flv、`-bsfs` に aac_adtstoasc / extract_extradata
- H.264+AAC の MP4 → FLV stream copy 成功 (ffprobe で h264 300 / aac 863 パケット)
- YouTube 実エンドポイントに無効キーで接続 → `Handshaking...` → `Server version 4.0.0.1` →
  `Releasing stream` / `FCPublish` / `Creating stream` → `Sending publish command` → サーバ切断。
  **c334a03 のビルドと同じ地点まで到達** = TLS も RTMP handshake (C0+C1 1537 バイト) も問題なし

**読み違い注意**: このテストは成功しても最後は `ffurl_read returned 0xdfb9b0bb` (= AVERROR_EOF)
と `Error opening output ...: Input/output error` で終わる (無効キーなので publish 後に切られる)。
`-loglevel verbose` 以下だと RTMP 層の行が出ず、TLS handshake で失敗したように見えて紛らわしい。
判定は `-loglevel debug 2>&1 | grep '^\[rtmps'` で `Server version` と `Sending publish command`
が出ているかで行う (`qemu-mipsel-static -strace` で send/recv の往復を見ても分かる)。

### 実機側: こちらの方が影響が大きい

- **netwatch** (`S52netwatch`, 2026-09-10 追加, デフォルト有効): ゲートウェイへの ping が
  30 秒間隔で 3 回連続失敗すると **OS ごと強制リブート**する (`reboot -f` → sysrq → watchdog 停止による
  ハードウェアリセット)。旧ファームには無かった。supervisor は「ICMP を落とすルーターがあるので
  ping しない。ネットワークが戻るのを待って再開する」設計なので、配信用途では再起動は欠損を
  延ばすだけになる。このため配信用途では無効化することにした
  (手順は docs/netwatch.md)
- **prudynt が `ad6294e` → `354b1b4` (142 コミット) に更新され、既定値が変わった。**
  全ストリームの既定サイズがセンサー解像度になり (`9f3d309`。以前 stream1 / JPEG は 640x360)、
  bitrate 0 = 約 1Mbps/メガピクセルの自動値になった (`3188189`)。ファーム更新で設定が初期化
  されると main が **1920x1080 / 25fps / 約 2.1Mbps**、JPEG プレビューも 1080p になる。
  **設定を WebUI で動的に変えた後は `service restart prudynt` が必要** (下記「prudynt の
  高負荷」)。なお RTSP サーバは旧ピンの時点で既に自前実装 (`src/simple-rtsp`) で、
  9/13 の "drop live555-era hybrid linking" はリンク方式の整理だけ。RTSP の挙動は変わっていない
- **Thingino を更新するとカメラ上の書き込み領域 (overlayfs の data パーティション) ごと消える。**
  このリポジトリで入れたファイルも全部消えるので、更新後は `device/install.sh` で入れ直す
  (公式イメージに `/usr/bin/ffmpeg` は無い。`BR2_PACKAGE_PRUDYNT_T_FFMPEG` は opt-in)。
  OS の更新や設定バックアップ自体はこのリポジトリの範囲外とし、インストーラは自前のファイルを
  置くだけにした。supervisor には「起動できて rtmps を持つか」の `-protocols` チェックを追加
- `S41ifplugd` が `overlay/` から消えたのは `package/thingino-ethernet` へ移っただけで、もともと
  有線 (`eth0`) 専用。Wi-Fi 側の DHCP (`S38wpa_supplicant` の udhcpc まわり) は新旧で同一なので、
  docs/troubleshooting.md の「デフォルトルートが戻らないときの cron 回避策」はそのまま有効
- 変わっていなかったもの: `jct` (1.2.0→1.2.1)、`/run/sync_success`、`/run/portal_mode`、
  `service enable|disable`、SD の自動マウント (`/mnt/mmcblk0p1`)、
  デフォルト streamer (prudynt。Raptor / timps / Strero は選択肢として追加されただけ)

### 実機確認 (`ciao+da40db6`, 2026-09-21)

- GCC16 ビルドのバイナリは実機で起動。mbedTLS soname・libatomic とも実機に存在 (同梱不要)
- RTSP は `thingino:thingino` / `/ch0` のまま。h264 (Main) + aac 16kHz mono が 1 本ずつ。
  SDP に sprop があり (デコーダ無しでも解像度が取れる)、`extract_extradata` は保険のまま
- **`Invalid DTS` は引き続き出る。** DTS が PTS より 106ms 進んで始まり、
  約 2 秒後 (最初の RTCP SR で同期し直した時点) に 28ms に縮む。B フレーム無しなので補正結果は
  正しく、2 分超の FLV が正常に再生できた。加えて先頭で 1 回
  `[flv] Timestamps are unset in a packet for stream 0. This is deprecated` が出るようになった。
  今は警告だけだが、FFmpeg のバージョンを上げるときは要注意
- **ファーム更新で prudynt の設定が初期値に戻り 1920x1080 / 25fps / 約 2.1Mbps になった。**
  旧計測 (720p15 / 約 330kbps で ffmpeg CPU 3.3%) はこのビットレートには当てはまらない。
  実測は 1080p25 / 2.1Mbps で **ffmpeg CPU 約 10%** (単発値)、720p10 / 1Mbps で **4.8% / RSS 3.4MB**。
  TLS の負荷はビットレートにほぼ比例するという見立てどおりで、ffmpeg 側は問題ない
- **ffmpeg のオプションは出力 URL より前に置く。** 後ろに置いた `-t 10` は
  `Trailing option(s) found in the command: may be ignored.` の警告とともに本当に無視され、
  tmpfs に 37MB 書き込む事故になった。旧版のコマンド例は `-loglevel error` を末尾に置いていた
  (`-loglevel` だけは先読みされるので効いていたが、警告は出る) ため先頭へ移した

### prudynt の高負荷: 動的再構成の後遺症だった (#3)

更新直後、ffmpeg を止めていても prudynt が CPU 55〜70% を食い、idle が尽きて解像度/fps を
上げるとカメラが固まる状態になった。切り分けの経過:

1. 最初は「既定値が 1080p25 / 2.1Mbps になったせい」と考えたが、720p10 / 1Mbps に下げても 55%
2. `/proc/<pid>/task/*/stat` の差分でスレッド別に測ると、libimp の **`group_update` 1 本が 53.8%**。
   prudynt 自身のスレッド (RTSP / HTTP / 音声) は合計 5% 程度、外部クライアントは 0
3. そのスレッドの tid が起動時のスレッド群よりずっと新しい = WebUI での設定変更でパイプラインが
   動的に作り直されていた
4. **設定は何も変えずに `service restart prudynt` しただけで prudynt 2.1% / idle 82% に戻った**

| 状態 (720p10 / 1Mbps) | prudynt | ffmpeg | idle |
|---|---|---|---|
| WebUI で解像度等を変更した後 | 55% | (停止中) | 30% |
| prudynt 再起動後 | 2.1% | 4.8% | 82% |

つまり原因は ffmpeg でも設定値でもなく、prudynt (`354b1b4`) の動的再構成後の異常状態。
upstream でも直近で pipeline リークの修正が入っている領域。回避策は単純で、
**WebUI で stream の設定を変えたら prudynt を再起動する**。

スレッド別 CPU の測り方 (busybox の `top -H` に頼らない):

```sh
P=$(pidof prudynt)
snap() { for t in /proc/$P/task/*; do echo "${t##*/} $(tr ' ' _ <$t/comm) $(sed 's/.*) //' $t/stat | awk '{print $12+$13}')"; done; }
snap >/tmp/s1; sleep 10; snap >/tmp/s2
awk 'NR==FNR{a[$1]=$3;next}{printf "%5.1f%%  tid=%s  %s\n",($3-a[$1])/10,$1,$2}' /tmp/s1 /tmp/s2 | sort -rn | head
```

なお `top -b -n 1` の単発値は busybox では当てにならない。`top -b -d 5 -n 2` の 2 サンプル目を見る。

### 調査のやり方メモ

thingino-firmware を `--filter=blob:none` で clone して `git grep <commit>` すると blob を
1 個ずつ取りに行って数分単位で固まる。2 コミットの比較は
`https://github.com/themactep/thingino-firmware/archive/<commit>.tar.gz` を 2 つ展開して
`diff -r` / `grep -r` する方が圧倒的に速い (buildroot は submodule なので含まれない。
pin は `git ls-tree <commit> buildroot` で見る)。

---

## 2026-09 追記: OSD テキストオーバーレイ (prudynt パッチ, #17)

配信映像に任意の複数行テキストを載せ、別プログラムから 0.5 秒単位で更新する機能。
`patches/prudynt-osd-textfile.diff` (prudynt `354b1b4` = `ciao+da40db6` のピン留め向け)。
以下の測定は矩形 1 つの版 (#17) のもの。3 つに増やした後の測定は「テキストの矩形を 3 つに (#19)」。

### なぜ prudynt にパッチを当てるのか

```text
センサー → ISP → FrameSource → [IMP OSD: 画像を重ねる] → H.264 エンコーダ → RTSP → ffmpeg (stream copy) → YouTube
```

- ffmpeg は stream copy なので、文字を載せられるのはエンコーダより前だけ。`drawtext` は再エンコードが
  要るので T31 では無理。SEI OSD (`osd.sei`) は受信側で描画する方式で、YouTube には映らない
- IMP OSD (`libimp`、バイナリ) は「渡された BGRA 画像を重ねる」だけで文字を扱わない。文字のラスタライズは
  prudynt が CPU で行っている (`src/video/OSD.cpp`)。libimp はプロセス内の状態が前提なので、
  prudynt の外から同じ OSD グループにリージョンを足すこともできない
- prudynt 標準の burn-in OSD が「1 行 / 63 文字 / 1Hz / 大文字と数字だけ」なのは、時刻表示専用に
  書かれているため (`char base[64]` に `strftime`、`\n` を扱わない描画、秒が変わった時だけ更新、
  既定フォントが 5x7)。ハードウェアの制約ではない。OSD スレッド自体は 100ms 周期で回っている

### 事前の実機測定 (パッチ前の prudynt、720p10 / 1Mbps、YouTube へ配信中)

「リージョンの面積で CPU が増えるか」は libimp がバイナリなので実測するしかなかった。標準の時刻表示を
`osd.burnin.scale` 10 (1170x110px ≒ 12.9 万画素) にして測った。30 秒間の `/proc` 差分、各 1 回:

| 条件 | prudynt 合計 | OSD 描画スレッド | `group_update` (libimp) | 系全体 idle |
|---|---|---|---|---|
| scale 2 (232x22)、毎秒描き直し、縁取りあり | 5.2% | 0.3% 未満 | 0.4% | 79.4% |
| scale 10、毎秒描き直し、縁取りあり | 18.2% | 12.6% | 0.7% | 67.2% |
| scale 10、文字列固定 (描き直しなし) | 5.9% | 0.3% 未満 | 0.8% | 79.3% |
| scale 10、毎秒描き直し、縁取りなし | 6.9% | 0.8% | 1.5% | 76.7% |

- **全フレームの合成コストは CPU にほぼ出ない** (12.9 万画素を載せても prudynt +0.7 ポイント、idle 変化なし)
- **重いのは upstream の縁取り描画だけ** (scale 10 で 1 回 約 126ms。縁取りなしなら約 8ms)。字の画素ごとに
  半径 scale/2 の円 × scale² のブロックを `std::function` 経由でアルファブレンドする実装で、scale の 4 乗で効く
- OSD プール (720p の既定値は `画素数*4*0.1 + 256KB` ≒ 616KB) に約 515KB のリージョンは入った
- 既定フォントは 5x7 で、小文字と `[ ] = | _` は出ない (`#` は出る)。ASCII アートには 8x8 が要る
- `prudyntctl json` での変更は `save_config` を送らない限りフラッシュに書かれない
  (`/etc/prudynt.json` の md5 不変、prudynt 再起動で元に戻ることを確認)
- `prudyntctl snapshot` の JPEG にも burn-in OSD は映る (映像の確認に使える)。ただし JPEG の更新は
  1〜2 秒粒度で、反映の遅延を細かく測るのには使えない

### 設計判断

- **時刻表示のコードには触らない。** 別リージョン + 別バッファ + 別の描画関数を新しいファイル
  (`OSDTextfile.cpp` / `OSDTextRender.hpp`) に置き、既存ファイルへの変更は「OSD スレッドの周回から呼ぶ 1 行」
  「`exit()` で破棄する 1 行」と設定項目の追加だけにした。Thingino 追従時の当て直しを軽くするため。
  prudynt の Makefile は `src/*/*.cpp` を wildcard で拾うので、ファイルを足すだけでビルドに入る
- **本文はファイル、設定は JSON API。** 0.5 秒ごとに `prudyntctl` を起動するのは無駄で、改行や引用符の
  エスケープも操作側の負担になる。tmpfs 上のファイルを `mv` で置き換える契約にし、prudynt は 100ms の
  周回ごとに `stat()` して inode / mtime / サイズが変わった時だけ読み直す。読み直しても内容が同じなら
  描き直さない
- **リージョンは `cols`×`rows` の最大サイズで固定確保する。** 内容の更新は `IMP_OSD_UpdateRgnAttrData`
  (中身の差し替え) だけになり、サイズ変更 (`IMP_OSD_SetRgnAttr`) は設定を変えた時しか起きない
- **縁取りは既定でオフ、半透明の背景ボックスで可読性を確保する。** 上の実測のとおり縁取りが唯一の
  高コスト要因だった。縁取りの実装も、円ではなく正方形の膨張 + ブレンドなしの上書きにして軽くしてある
- **一度作ったリージョンは破棄せず、表示/非表示は `IMP_OSD_ShowRgn` で切り替える。** upstream の時刻表示は
  `osd.burnin.enabled` を false→true にするとリージョンを作り直す実装 (古いハンドルを破棄しない) に読めるので、
  同じ作りにはしなかった
- **8x8 フォントは 1 セル = 8x9 フォント画素** (横は詰める、縦は 1 画素の行間)。`####` のようなバーが
  横に途切れずつながり、行同士の字は接触しない。scale 2・40 桁×4 行で 648x80px (約 203KB)
- **ファームは焼き直さない。** prudynt パッケージだけビルドし (`br-prudynt-t`)、バイナリを
  `/usr/bin/prudynt-osd` に置いて起動時に `/usr/bin/prudynt` へ bind mount する。overlayfs 上で
  `/usr/bin/prudynt` を直接上書きすると、元に戻すのに upperdir (`/overlay/usr/bin/prudynt`) を直接消す
  必要があり、Thingino のファイルに手を入れない方針にも反するため
- ビルド時のフォント選択は Thingino の `user/<camera>/local.fragment` (ユーザー設定の仕組み。
  Thingino 側で git 管理外) に `BR2_PACKAGE_PRUDYNT_T_OSD_FONT_8X8=y` を書く。prudynt へのパッチは
  `package/prudynt-t/` に `*.patch` として置けば Buildroot が自動で当てる

### 実機確認 (パッチ入り prudynt、`ciao+da40db6`、720p10 / 1Mbps、YouTube へ配信中、2026-09-21)

30 秒間の `/proc` 差分、各 1 回。既定の 40 桁×4 行・scale 2 (648x80px、約 203KB)、8x8 フォント:

| 条件 | prudynt 合計 | OSD スレッド | 系全体 idle |
|---|---|---|---|
| パッチ入り、オーバーレイ無効 | 5.3% | 0.3% 未満 | 79.6% |
| 表示のみ (内容は固定) | 5.7% | 0.3% 未満 | 79.1% |
| **0.5 秒ごとに更新、縁取りなし** | **6.0%** | 0.5% | 75.1% |
| 0.5 秒ごとに更新、縁取りあり | 6.1% | 0.6% | 78.2% |

- 0.5 秒更新のコストは prudynt で +0.3〜0.7 ポイント。scale 2 では縁取りの有無の差は測定誤差の範囲
  (縁取りが効くのは scale を大きくした時)。idle の 75.1% は、操作側のデモ (シェルスクリプトが
  0.5 秒ごとに `date` や `mv` を起動する) の分と測定のばらつきを含む
- プライバシーカバー (レイヤー 1) の黒画面の上にも、時刻と同じく表示され続ける (`PRIVACY ch=0 value=on` で確認)
- 時刻表示 (レイヤー 2) と共存できる。8x8 フォントを有効にすると**時刻表示も 8x8 になり、少し横に伸びる**
  (720p の自動 scale で 234x22 → 348x24、約 20KB → 約 33KB)
- ファイルの削除 → 再作成、`enabled` の false → true を繰り返しても、リージョンは作り直されず表示が戻る
- バイナリは 830,948 → 848,388 バイト。overlay (JFFS2、圧縮あり) の使用量は 1.8MB → 2.1MB
- 再起動後、`S30prudynt-osd` の bind mount → `S31prudynt` の順で自動的にパッチ入りが立ち上がることを確認。
  `S30prudynt-osd stop` で標準の prudynt (md5 一致) に戻ることも確認

**ハマり6: OSD プールに入らないリージョンは、エラーなしで表示されない。** `IMP_OSD_SetRgnAttr` は 0 を返し、
`logread` にも何も出ず、ただ映らない。720p (プール約 616KB、時刻リージョン約 20KB と同居) での境界:

| リージョン | サイズ | 結果 |
|---|---|---|
| 972x148 (scale 3、40x5) | 562KB | 表示される (この測定は予算の処理を入れる前。今のパッチは予算 約 535KB を超える設定として自動で縮める) |
| 972x174 (scale 3、40x6) | 660KB | **表示されない** (エラーなし) |
| 972x228 / 1028x292 | 866KB / 1173KB | 表示されない。小さいサイズに戻せば復帰する |

libimp 側で検出できないので、パッチ側で「プールサイズ (`IMPSystem.cpp` と同じ式) − 同じストリームの
時刻リージョン − 48KB (もう一方のストリームの時刻分の余裕)」を予算とし (サブストリームにも出す設定では
さらに半分ずつ)、超える設定は scale → 行数 → 桁数の順に
自動で縮めて `logread` に警告を出すようにした。時刻表示を scale 7 (約 409KB) にすると、テキスト側が
scale 1 に縮んで両方表示されることを実機で確認した。大きく出したい場合は `general.osd_pool_size` (KB) を
上げる (RAM とのトレードオフ。未実測)

- 1 時間の連続動作 (0.5 秒更新 × 約 7200 回、配信中): prudynt の RSS 5992KB → 6516KB (最初の 10 分で増えた後は
  横ばい)、fd 24 → 24、スレッド 36 → 36、ffmpeg の再起動 0 回、エラーログなし。終了後の prudynt 5.2%

### テキストの矩形を 3 つに (#19)

`osd.textfile` / `textfile2` / `textfile3` の固定 3 スロット (既定は左下・右上・右下)。固定本数なのは、
prudynt の設定機構が固定キーの表で、可変長の配列を素直に扱えないため。状態をスロットごとの構造体にして
同じ処理を 3 回まわすだけで、描画コードは共通。

- プール予算はスロット番号順に割り当てる。各周回でまず全スロットの大きさを決め (計画)、次に
  **縮む/消えるスロットを先に、大きくなるスロットを後に**適用する。逆順だと一瞬プールを超え、
  libimp が黙って表示しなくなるため
- **日時を更新する周回 (1 秒に 1 回) では、日時 → テキストの順に処理する。** テキストの予算は日時リージョンの
  今の大きさから計算するので、逆順だと起動直後や `osd.burnin.scale` の変更時に古い大きさを前提にしてしまう。
  それでも upstream は日時の新しいサイズを先に設定するので、その瞬間にプールが足りないと日時が黙って消える。
  日時のサイズが変わった周回では、テキスト側が譲った後で日時の `IMP_OSD_SetRgnAttr` をもう一度呼ぶ
  (upstream の関数は変えず、テキスト側のコードから呼ぶ)。実機で、テキスト 476KB を出した状態から日時を
  scale 5 にして、日時が表示されテキストが scale 1 に縮むことを確認
- `path` と色は 1 秒に 1 回だけ、ロックを取って読み直してコピーを持つ。これらは JSON API のスレッドが free して
  差し替えるポインタで、upstream はロックなしで読んでいる (`osd.burnin.format`)。頻度を下げるだけでは同期に
  ならないので、`Config.hpp` の `set<const char *>` が差し替える箇所に `stringMutex` を足し、テキスト側は同じロックの
  中でコピーする (upstream のコードへの変更はこの 4 行だけ。upstream 自身の読み取りは元のまま)
- prudynt は `osd.sei.enabled || osd.burnin.enabled` の時しか OSD オブジェクトを作らない (`IMPEncoder.cpp`)。
  作成条件に `osd.textfile*.enabled` を足したが、両方オフの構成では実行中の有効化はできない (相手がいない)
- 無効にしたスロット、予算が尽きて出せないスロットは、非表示ではなくリージョンごと破棄する。非表示のままだと
  プールを掴んだままになり、計画 (空いた前提) と実態がずれる。ファイルが空なだけのスロットは予算を
  確保したまま非表示にする
- 実測 (720p10、配信中、プール 2048KB): 左下 40x10 + 右上 24x2 + 右下 24x4 を **3 つとも 0.5 秒更新して
  prudynt 7.4%** (オーバーレイなし 5.3%)。idle 74.7% (書き込み側のシェルスクリプト込み)
- 既定のプールで同じ設定にすると、右上・右下が scale 1 に縮んで表示され、左下を 11 行にすると右上は 1 行、
  右下は 6 桁×1 行まで縮み、左下を 4 行に戻すと 3 つとも元の大きさに戻ることを確認

### OSD プールの上限 (`general.osd_pool_size`)

フラッシュに書かずに試すため、`/etc/prudynt.json` の tmpfs コピーを書き換えて元のファイルの上に bind mount し、
prudynt を再起動して測った (720p10、配信中)。各サイズで 52 桁×25 行・scale 3 (1260x688px、約 3.4MB) を要求:

| プール | 起動 | 表示された矩形 | prudynt CPU (内容固定) | RSS | Linux 側 free |
|---|---|---|---|---|---|
| 0 (≒616KB) | OK | 420x230 (scale 1 に縮小) | 6.1% | 6.1MB | 51.4MB |
| 1024 | OK | 420x230 (scale 1) | 6.2% | 6.1MB | 52.5MB |
| 2048 | OK | 840x458 (scale 2) | 7.5% | 8.5MB | 50.1MB |
| 4096 | OK | **1260x688 (要求どおり、画面ほぼ全面)** | 10.8% | 12.2MB | 46.0MB |
| 8192 | OK | 同上 | 9.4% | 12.2MB | 45.8MB |
| 16384 | OK | 同上 | 9.4% | 12.1MB | 45.5MB |
| 32768 | **起動しない** | — | — | — | — |

- プールは Linux 側のメモリではなく予約メモリ (`rmem=29M`) から取られる (プールを 8MB → 16MB にしても free が
  変わらない)。32MB は rmem を超えるので起動できない。設定を戻せば復旧する
- RSS の増加はパッチ側の描画バッファ (矩形と同じ大きさ) など。矩形を大きくした分だけ増える
- 全フレームの合成コストは、画面ほぼ全面でも prudynt +4〜5 ポイント。事前測定 (12.9 万画素で +0.7) と
  合わせると、面積にほぼ比例している
- scale を先に縮める仕様なので、プール 1024KB で scale 3・52x25 を要求すると scale 2 の小さい格子ではなく
  scale 1 の 52x25 になる。大きい字で出したい時は、桁数・行数を自分で減らす
- 16MB が「安全」とは限らない。720p10・2 ストリームで起動と表示を確認しただけで、解像度を上げると同じ rmem を
  使う映像バッファが増える。720p で必要なのは最大でも約 3.5MB

**未確認 / 既知の制約:**

- 1080p での負荷とプール境界は未確認 (この運用は 720p10 固定なので確認しない)
- サブストリームへの表示 (`substream_disabled: false`) は 2026-09-22 に確認: 640x360 に 3 つとも scale 1 で出る。
  メイン側の予算が半分になるので、既定のプールではメインの右上・右下が scale 1 に縮む (設定を戻すと scale 2 に戻る)
- 3 スロット版でのプライバシーカバーとの共存、3 つとも 0.5 秒更新での 15 分連続動作 (RSS 7208KB で不変、fd 24、
  スレッド 36、prudynt / ffmpeg の再起動なし、prudynt 約 6.7%) も同日に確認
- `osd.textfile.path` の読み込みは OSD スレッドで同期的に行う。tmpfs 前提で、固まるファイルシステム
  (NFS 等) を指すと時刻表示ごと止まる
- upstream 自身の `osd.burnin.format` などの読み取りはロックなしのまま (テキストオーバーレイの `path` と色は
  `stringMutex` で保護した)
- 測定中、標準の prudynt の時点から `video channel:0 - msgChannel sink clogged, 5x frames dropped in last 5s`
  の警告が 5 秒ごとに出続けていた。このパッチとは無関係 (パッチ前の prudynt でも同じ頻度)。配信は流れている。
  原因は次の節

### `msgChannel sink clogged` の警告は誤報 (#21)

prudynt (`354b1b4`) が RTSP クライアントの接続中、5 秒ごとに
`video channel:N - msgChannel sink clogged, 5x frames dropped in last 5s` を出し続ける。**フレームは落ちていない。
警告のカウンタが、キューに書かなかった NAL を「落とした」と数えているだけ。**

- `VideoWorker.cpp` は NAL ごとに `bool delivered = false` で始め、`onDataCallback != nullptr` (= `msgChannel` を
  読む側がいる) の時だけ `msgChannel->write()` の結果を `delivered` に入れる。読む側がいない時はバッファを
  プールに返して何も書かないが、**`delivered` は false のまま**なので、その下の「`!delivered` なら clog として数える」に
  毎回入る
- このバージョンには `onDataCallback` に代入している箇所が**どこにも無い** (宣言と nullptr での初期化、
  nullptr かどうかの判定だけ)。RTSP は tap (`video_taps`) 経由に移っていて、`msgChannel` は使われていない。
  つまり「読む側がいない」が常に成り立ち、`msgChannel` には 1 個も書かれず、キューは空のまま
  (#21 を立てた時の「キューが満杯に張り付いている」という読みは誤り)
- この処理全体が `hasDataCallback` (クライアント接続中) の内側にあるので、警告が出るのはクライアントがいる
  チャンネルだけ。ch0 は youtube-relay の ffmpeg が常時つないでいるので出続ける
- 実機での裏付け (2026-09-22): 普段は ch1 の警告は出ていない。ch1 に RTSP クライアントを 17 秒つなぐと、その間だけ
  `video channel:1 - ... 53 frames dropped in last 5s` が 5 秒ごとに出て、切断すると止まった。数は ch0 と同じく
  5 秒間の NAL の個数とほぼ一致。受信側の ffmpeg にエラーなし
- 影響はログだけ: 5 秒に 1 行 (1 時間で 720 行) 出るので `logread` のリングバッファが早く流れる。メモリや CPU の
  無駄は無い (バッファは即座にプールへ返している)
- 対処: `patches/prudynt-msgchannel-warning.diff` (読む側がいない時は `delivered = true` にする 1 行)。OSD のパッチとは
  独立。2026-09-22 に両方のパッチを当ててビルドし直して実機に入れ、配信中 (ch0 に ffmpeg が接続中) に警告が
  出なくなったこと、OSD テキストと配信がそのまま動くことを確認した。パッチを当てない場合は
  `logread | grep -v 'sink clogged'` で読み飛ばせば足りる (`general.loglevel` を `ERROR` にしても消えるが、
  他の警告も消える)

### SD カードの設定ファイル (`osd-config`, #22) の実機確認と、OSD プールを SD から指定する仕組み

2026-09-22、本物の SD カード (exFAT) で確認:

- SD 直下に `prudynt-osd.json` を書く → 2 秒後に `osd-config: Applied ...`、3 スロット表示
- SD を抜く → 10 秒以内に `Config file is gone - turned off: textfile textfile2 textfile3`、表示が消える
- 挿し直す → 自動マウント後 10 秒以内に再適用
- 挿したまま再起動 → 起動から約 24 秒で自動適用 (`S30prudynt-osd` の bind mount も自動)。テキスト本体は
  tmpfs なので消えており、書き直せば表示される

**`general.osd_pool_size` を SD から指定する。** プールは prudynt の起動時にしか確保されないので、
`prudyntctl json` では変えられない。`osd-config` は設定ファイルの `general.osd_pool_size` が `/etc/prudynt.json` と
違う時だけ、`jct` でそこに書いて prudynt を再起動する (このリポジトリのスクリプトがフラッシュの prudynt 設定を
書く唯一の箇所。ユーザーの判断で例外にした)。設計上の選択と実測:

- tmpfs のコピーを bind mount する案 (フラッシュ無傷) は採らなかった。bind mount 中に WebUI や `save_config` で
  保存した prudynt の設定が tmpfs 側に書かれて再起動で消える罠があるため
- `S31prudynt restart` は stop が返ってから prudynt が消えるまで待たないので、そのまま使うと新しい prudynt が
  `Another Prudynt instance appears to be running` で終了する (実機で 1 回踏んだ)。stop → `pidof` が空になるまで
  待つ → start にした (インストーラと同じ)
- 新しい値で起動しなかった時は元の値に戻して起動し直す。上の失敗がちょうどこの経路を通り、復元が動くことを確認した
- 値の変更から `Applied` まで約 4〜5 秒 (prudynt の再起動込み)。ffmpeg は RTSP 切断で終了し 4 秒後に再接続
- 2048KB で 40×10 + 24×2 + 24×2 が 3 つとも scale 2 で出ることをスナップショットで確認。4096 → 2048 の変更も
  同じ経路で動作。範囲外 (99999) は 1 回ログして無視、`osd` の部分は普通に適用される
- SD を抜いてもプールの値は戻さない (戻すたびに再起動 + フラッシュ書き込みになる)。0 を書けば既定に戻る

### SD カードからの Wi-Fi 設定 (#10)

使い方は docs/wifi-from-sd.md。**B (`wpa_supplicant.conf` + `S37wifi-from-sd`) は 2026-09-22 に実機で確認した**:

- SD に「今の Wi-Fi のブロック (カメラの設定からコピー) + 存在しないダミーの Wi-Fi (priority -10)」の 2 つを書いて再起動
  → `wifi-from-sd: applied ... (2 network(s))`、`wpa_cli list_networks` に 2 つ、元の Wi-Fi に接続、SSH 復帰。
  `S37` は `S38wpa_supplicant` の直前に動き、SD は `S09mmc` で先にマウントされている
- **見つけたバグ (修正済み)**: 「内容が変わった時だけ書く」判定を `/etc/wpa_supplicant.conf` との md5 比較で
  やっていたが、wpa_supplicant 自身が動作中にそのファイルを書き換える (`update_config=1`: `dik_cipher=` /
  `dik=` の行を足し、`ap_scan=` を落とし、空行も変える) ので、毎回の起動で「変わった」と判断して書き直していた。
  最後に適用した内容の md5 を `/etc/wpa_supplicant.conf.sd-md5` に置いて、それと比べるようにした。
  修正後、同じ SD で再起動 → ログなし (書き換えなし)、接続は正常
- 確認後、SD のファイルを消して `.before-sd` から元の設定に戻した (md5 が元と一致)。`S37wifi-from-sd` は
  入れたまま (SD にファイルが無ければ何もしない)

A (`uenv.txt`) は未確認 (ソースの読解に基づく):

- Wi-Fi が一度も設定されていないカメラに A を使うと、その起動は設定用ポータルで立ち上がり、
  **もう一度再起動して初めて接続される**可能性がある (接続モードの判定が SD の読み込みより前のため)。
  B は `S38` より前にファイルを置くのでこの問題は起きないはず
- 起動スクリプトの時点で SD がマウント済みか (B は最大 10 秒待つ)
- A は Thingino の公式ドキュメントに記載が無く、将来のファームで変わりうる

---

## 2026-09 追記: RTMPS の送信量が映像の 1.7 倍だった (FFmpeg パッチ, #31)

wlan0 の送信が 1.8 Mbps なのに、prudynt の RTSP を録って測ると映像 + 音声は 1.05 Mbps (エンコーダは CBR 1024 の設定どおり)。
`/proc/net/snmp6` の `Ip6OutOctets` (YouTube は IPv6) と `/proc/net/netstat` の `IpExt OutOctets` (IPv4 = LAN + loopback) に
分けると、IPv6 = 1.77 Mbps ≒ wlan0 全体で、YouTube への RTMPS 接続そのものが 1.69 倍だった (`Ip6OutOctets` は IPv6 全体の
値だが、`netstat -tn` で他に IPv6 の接続が無い (ssh は IPv4) ことを確認した上での測定。倍率は TLS・TCP・IPv6 ヘッダを
含む線上の実測値)。TCP 再送は 0。

原因は FFmpeg の RTMP 実装: 送信チャンクは 128 バイト固定 (`rtmpproto.c` の `rt->out_chunk_size = 128`、変えるオプション無し。
Set Chunk Size を送るコードは listen 側にしか無い) で、`ff_rtmp_packet_write` はパケットヘッダ・128 バイトのデータ・次の
チャンクの 1 バイトの継続ヘッダをそれぞれ別の `ffurl_write` で書く。`tls_mbedtls.c` の `tls_write` は書き込み 1 回 =
`mbedtls_ssl_write` 1 回 = TLS レコード 1 つ (バッファ無し) なので、128 バイトごとに TLS レコード 2 つ (データ + 継続ヘッダ)
= 2 × 約 29 = 約 58 バイトの枝葉が付き、
小さな書き込みが多い分 TCP/IPv6 ヘッダの比率も上がる。平文の RTMP では TCP がまとめるので目立たない。

対処 `patches/thingino-ffmpeg-rtmp-chunk-size.diff`: `rtmp_chunk_size` オプション (既定 4096、128 で宣言しない) を足し、
ハンドシェイク直後・`connect` の前に Set Chunk Size を送って `out_chunk_size` を切り替える (RTMP では送信側が自分のチャンク
サイズを宣言できる。最大 65536)。`package/thingino-ffmpeg/0002-rtmp-chunk-size.patch` として Buildroot に当てる。
FFmpeg は publish 中にサーバから Set Chunk Size を受けると、それを送り返して自分の送信サイズにも反映する
(`handle_chunk_size`) ので、自分でサイズを宣言した時はこの反映をしないようにした (YouTube は接続時に送ってこないことを
無効なキーで接続して確認したが、送ってきても 4096 が縮まないように)。

実測 (2026-09-22、`Ip6OutOctets` の 120 秒差分、720p10 / 1Mbps):

| 接続 | ffmpeg | YouTube への送信 | 映像 + 音声 (1046 kbps) に対して | ffmpeg CPU |
|---|---|---|---|---|
| RTMPS | パッチなし (128) | 1771 kbps | 1.69 倍 | (前日の実測 4.8%) |
| RTMPS | パッチ (4096、既定) | 1126〜1128 kbps | 1.08 倍 | 2.5% |
| RTMPS | `-rtmp_chunk_size 65536` | 1111 kbps | 1.06 倍 | — |
| 平文 RTMP (1935) | パッチ (4096) | 1093 kbps | 1.04 倍 | 2.2% |
| 平文 RTMP (1935) | `-rtmp_chunk_size 128` (素の動作) | 1133 kbps | 1.08 倍 | 3.6% |

4096 と 65536 の差は誤差の範囲なので既定は 4096。YouTube 側は問題なく受信を継続 (ffmpeg の再起動なし、再送 0)。
平文 RTMP は TLS の枝葉が無いぶん RTMPS より約 3% 少なく、パッチ無しでも TCP が小さな書き込みをまとめるので 1.08 倍で済む
(TLS が無ければこの問題はほぼ顕在化しない)。CPU の差も 0.3 ポイント。ユーザーの判断で、この実機は平文 RTMP に切り替えた
(帯域を優先。ストリームキーが経路上を平文で流れることは了承済み)。
パッチを当てる前後で ffmpeg のバイナリサイズは同じ 2,604,580 バイト。パッチなしのビルドは実機に入っていたバイナリと md5 が
一致した (ビルド環境の再現性の確認になった)。

- `youtube-relay` は `/etc/youtube-relay.json` を書き換えても動いている ffmpeg を再起動しない (起動時に読むだけ)。
  `ffmpeg_out_opts` を試す時は `service restart youtube-relay`
- モバイル回線では 1 時間あたり約 800MB → 約 510MB (RTMPS + パッチ)、約 490MB (平文 RTMP + パッチ)

## 残タスク

- [x] 長時間試験 — **約4時間の連続配信で完走、リーク・劣化なし**
  (1時間時点の計測: ffmpeg RSS 3312K→3324K (+12KB)、CPU 3.3%→3.2%、
  free RAM 変化なし、A/V ズレなし。以降も問題なく4時間で試験完了とした)
- [x] supervisor 実機動作確認 — SD カードに設定を置いて再起動 →
  **自動マウント → 設定検出 → 自動配信開始** まで実機で成功 (スタンドアロン動作の実証)
- [x] supervisor スクリプト — 切断/Wi-Fi 断からの自動再起動 (Thingino init script 形式)。
  クラッシュ再起動・ネットワーク断の待機と復帰後の自動再開まで実機の障害試験で確認済み。
  復帰しないケースが今後見つかればバグとして対応する
- [x] `ciao+da40db6` での実機確認 — YouTube Live へ映像・音声とも配信成功 (2026-09-21)。
  `install.sh` は新規インストールと、配信中の再実行 (旧 supervisor の停止待ち → 入れ替え → 約 5 秒で配信再開) を確認。
  ffmpeg CPU 4.8% @720p10/1Mbps。`install.sh` (新規インストール / 配信中の再実行) と
  `disable-netwatch.sh` も同日に実機で確認。長時間試験も完了
- [x] インストーラ (`install.sh`)、netwatch 無効化 (`disable-netwatch.sh`)、SD カードからの Wi-Fi 設定 (複数可)
- [x] 映像に任意のテキストを重ねる prudynt の OSD パッチ (#17、3 か所化 #19、SD からの設定 #22) — 実機 (720p) で確認:
  3 か所同時の 0.5 秒更新、再起動後の自動有効化、OSD プールの上限、サブストリームへの表示、プライバシーカバーとの共存。
  連続動作は 1 か所の版 (#17) で 1 時間、3 か所の版で 15 分。本物の SD カードでの `osd-config` (抜き差し・再起動) と
  `general.osd_pool_size` の SD からの指定も確認。1080p は未確認 (この運用では使わない)
- [x] `S37wifi-from-sd` の実機確認 (#10、2026-09-22)。`uenv.txt` (Thingino 標準) の方は未確認
- [ ] Thingino パッケージとしての統合 / ファームウェア組み込み (Config.in オプション化、stream key の安全な保持)。
  将来的には Thingino の新ストリーマ [Raptor](https://github.com/gtxaspec/raptor) の RTMPS push 機能 (RSP) への
  移行も選択肢
