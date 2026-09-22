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
- `/tmp` は再起動で消える。`/tmp` で試す時の細かい注意 (scp が使えない、libatomic など) と、常設・自動起動・自動復帰は
  supervisor を導入する ([docs/relay.md](docs/relay.md)、おまけ)
- **2026-09 以降の Thingino は、ゲートウェイへの ping が約 90 秒通らないとカメラを OS ごと
  再起動する (netwatch、デフォルト有効)。** 配信が切れるので、長時間配信する前に無効化しておく:

  ```sh
  device/disable-netwatch.sh root@<camera-ip>
  # カメラ上で直接やるなら: jct /etc/thingino.json set netwatch.enabled false && service restart netwatch
  ```

  Thingino の OS 設定を変えるものなので、ffmpeg やスクリプトのインストールとは別の手順にしてある。
  理由と代償 (Wi-Fi が固まっても自動復旧しなくなる) は [docs/netwatch.md](docs/netwatch.md)

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
             thingino-ffmpeg-rtmp-chunk-size.diff  FFmpeg 本体: RTMP の送信チャンクサイズを rtmp_chunk_size オプション (既定 4096、128〜65536) で指定できるようにする。素の FFmpeg は 128 固定で、RTMPS の送信量が映像の 1.7 倍になる (4096 で 1.08 倍)
             prudynt-osd-textfile.diff          任意: prudynt (ストリーマ) に「テキストファイルを映像へ重ねる OSD」を足す
             prudynt-msgchannel-warning.diff    任意: prudynt が 5 秒ごとに出す誤報の警告 (msgChannel sink clogged) を止める
device/    カメラ側の常設運用一式 (ファイルの一覧は device/README.md、使い方は docs/)
             youtube-relay / S93youtube-relay   supervisor と起動スクリプト
             www/youtube.html / www/x/json-youtube.cgi
                                                Web UI の「YouTube Live」ページ (キーの設定、再起動、サービスの有効/無効)
             install.sh                         上記と ffmpeg をカメラへ入れる (自前のファイルを置くだけ)
             disable-netwatch.sh                Thingino の netwatch (OS 自動再起動) を無効化する
             install-wifi-from-sd.sh / S37wifi-from-sd
                                                任意: SD の wpa_supplicant.conf (複数 Wi-Fi 可) を起動時に適用
             install-prudynt-osd.sh / S30prudynt-osd / osd-progress-demo
                                                任意: 上記 OSD パッチ入りの prudynt を入れる、操作側のサンプル
             osd-config / S93osd-config / prudynt-osd.json.example
                                                任意: OSD の設定を SD カードのファイルから読んで prudynt に送り直す
             www/osd-text.html / www/x/json-osd-text.cgi
                                                任意: Thingino の Web UI に足す OSD テキストの編集ページと CGI
             osd-feed/ / S94osd-feed / install-osd-feed.sh
                                                任意: OSD テキストを自動更新する常駐プログラム (Go、SD カードから実行)
             common.sh                          対応ファームの判定 (違えば何も変更せず中止)
docs/      使い方と設定の文書 (下の「ドキュメント」参照)
NOTES.md   実装の詳細・設計判断・ハマりどころ・実測値の記録 (手順は書かない)
dist/      ビルド成果物 (git 管理外)。配布は GitHub Releases (ffmpeg バイナリ単体) で行う
```

OS (Thingino) の更新・バックアップ・設定変更と、このリポジトリの成果物のインストールは分けてあります。
`install.sh` は自前のファイルを置くだけで、OS の設定を変えるもの (`disable-netwatch.sh`、
`install-wifi-from-sd.sh`) は別のスクリプトです。

`thingino-firmware/` (Thingino のソースツリー) はビルド時にこの直下へ clone しますが、
git 管理外です。必要な変更はすべて `patches/` に分離してあります。

## ドキュメント

| 文書 | 内容 |
|---|---|
| [docs/relay.md](docs/relay.md) | 配信 supervisor: インストール、設定ファイル (SD カードモード)、ffmpeg の置き場所、運用、Web UI (YouTube Live ページ)、ストリームキーの取り扱い |
| [docs/osd.md](docs/osd.md) | 映像にテキストを重ねる (OSD テキストオーバーレイ): インストール、使い方、Web UI での編集、設定項目、SD カードの設定ファイル、大きさの上限 |
| [docs/osd-feed.md](docs/osd-feed.md) | OSD テキストを自動更新する常駐プログラム `osd-feed`: データ源 (メモリ、カウンタ、時刻、HTTP JSON) とテンプレート、設定、計測 |
| [docs/wifi-from-sd.md](docs/wifi-from-sd.md) | Wi-Fi 設定を SD カードで運ぶ |
| [docs/netwatch.md](docs/netwatch.md) | netwatch (ping 失敗での OS 再起動) の無効化 |
| [docs/cellular.md](docs/cellular.md) | セルラー回線 (車載) での配信: 送信量 (RTMPS と平文 RTMP の実測)、切断からの復帰の実測、チューニングの選択肢 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 症状別の対処 |
| [docs/build.md](docs/build.md) | ffmpeg / prudynt のビルド手順 |
| [device/README.md](device/README.md) | `device/` のファイル一覧 |
| [NOTES.md](NOTES.md) | 経緯、設計判断、ハマりどころ、実測値、未検証事項の記録 |

## 特徴

- **バイナリ 2.5MB** (stripped)。FFmpeg 8.0.1 を RTSP 入力 + FLV/RTMPS 出力だけに絞った構成
- **stream copy 専用** — encoder/decoder/filter を一切含まない
- **TLS は mbedTLS** — Thingino 実機に入っている `libmbedtls.so.3.6.6` に動的リンク
- **ファームウェア世代ごとにビルドが必要** — toolchain (GCC / uClibc) を実機に揃えるため。
  対応表は下記「動作確認環境」
- **ファームウェア書き換え不要** — `/tmp` に転送して実行するだけ (PoC 用途)
- **SD カードが物理スイッチになるスタンドアロンモード** — 設定 (ストリームキー) を書いた
  SD を挿すと配信開始、抜くと停止 (supervisor が提供。[docs/relay.md](docs/relay.md))
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

自分でビルドする場合の手順 (ffmpeg、OSD パッチ入りの prudynt) は [docs/build.md](docs/build.md)。
必要なもの: Docker が使える x86_64 Linux、ディスク ~10GB。

## (任意) 映像に任意のテキストを重ねる

prudynt (Thingino のストリーマ) へのパッチ `patches/prudynt-osd-textfile.diff` で、配信映像に任意の複数行テキスト
(プログレスバーなどの ASCII アート) を 3 か所 (既定は左下・右上・右下) まで焼き込み、カメラ上の別のプログラムから
0.5 秒単位で更新できます。操作側は tmpfs 上のファイルを `mv` で置き換えるだけで、ファームの焼き直しは不要です。
矩形の位置や大きさ、表示するテキストは Thingino の Web UI (Streamer → OSD text) からも編集でき、設定は SD カードに保存されます。
表示内容を自動で更新する側のプログラム (`osd-feed`: メモリ量やカウンタ、HTTP で取った JSON をテンプレートで並べる) は
[docs/osd-feed.md](docs/osd-feed.md) (ビルドは `device/osd-feed/build.sh`)。OSD 自体の使い方と設定は [docs/osd.md](docs/osd.md)、
prudynt のビルドは [docs/build.md](docs/build.md)。

## 補足

- **ライセンス**: この構成の FFmpeg バイナリは `--enable-gpl --enable-version3 --enable-mbedtls`
  でビルドされるため **LGPL v3 / GPL v3** 相当です。バイナリ配布時は FFmpeg / mbedTLS の
  ライセンス表記に従ってください
- **TLS 証明書検証**: FFmpeg 8.x のデフォルトでは無効です (通信は暗号化されます)。
  検証したい場合は CA バンドルを置き `-tls_verify 1 -ca_file <path>` を付けてください
- **ストリームキー**: シェル履歴やログに残ります。露出した場合は YouTube Studio で再生成を
- 実装の詳細・経緯・ハマりどころは [NOTES.md](NOTES.md) を参照

## ステータス

実機 (Wyze Cam v3、Thingino `ciao+da40db6`) で、YouTube Live への直接配信、supervisor による自動復帰、
SD カードでのスタンドアロン運用、長時間配信、OSD テキストオーバーレイ (720p) まで確認済み。
残っているのは `S37wifi-from-sd` の実機確認と、Thingino パッケージとしての統合。
確認の経緯と残タスクの一覧は [NOTES.md](NOTES.md) の「残タスク」。

## 謝辞

- [Thingino](https://github.com/themactep/thingino-firmware) — オープンな IP カメラファームウェアと
  クロスビルド環境
- [FFmpeg](https://ffmpeg.org/)
