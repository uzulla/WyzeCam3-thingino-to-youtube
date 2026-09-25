# wzc-yt — Wyze Cam v3 (Thingino) → YouTube Live 直接配信・OSD ツール

> **English summary**: Build recipe for a minimal FFmpeg binary (2.5MB) that runs on a
> Wyze Cam v3 flashed with [Thingino](https://thingino.com/), letting the camera push its
> H.264/AAC stream **directly to YouTube Live over RTMPS** — no relay PC required.
> Pure stream copy (no re-encoding): measured load on the Ingenic T31 is 3-10% CPU depending on
> bitrate (3.3% at 330kbps, 4.8% at 1Mbps, ~10% at 1080p25 / 2.1Mbps) and ~3.4MB RSS.
> Binaries are specific to a Thingino build; pick the Release matching your camera's `BUILD_ID`.
> Includes a streaming supervisor with automatic recovery, a Thingino Web UI, SD-card
> configuration, and optional text / transparent PNG overlays with live data from HTTP JSON.
> See [docs/](docs/) for setup and [NOTES.md](NOTES.md) for implementation details (Japanese).

Thingino 化した Wyze Cam v3 から、中継マシンなしで YouTube Live へ直接配信するための
minimal FFmpeg バイナリと、カメラだけで常設運用するためのツール一式です。
配信の自動起動・切断後の自動復帰、ブラウザからの設定、SD カードでの設定持ち運びに加え、
映像へのテキスト・ロゴ画像の重ね合わせ (OSD) や外部データの自動表示にも対応しています。

## できること

| 機能 | 内容 | 導入・使い方 |
|---|---|---|
| YouTube Live へ直接配信 | H.264/AAC を再エンコードせず RTMPS で送信。中継 PC 不要 | [まず配信を試す](#まず配信を試す) |
| 常設・自動復帰 | `youtube-relay` が ffmpeg を監視し、終了後に再起動。ネットワーク復帰を待って再開 | [配信 supervisor](docs/relay.md) |
| ブラウザから配信を管理 | ストリームキー・URL の設定、開始・停止・再起動、自動起動の切り替え、状態とログの表示 | [YouTube Live Web UI](docs/relay.md#web-ui) |
| SD カードで設定を持ち運ぶ | 配信キー・OSD 設定を SD に保存。複数の Wi-Fi 設定を起動時に適用する機能も用意 | [配信設定](docs/relay.md)、[Wi-Fi 設定](docs/wifi-from-sd.md) |
| テキスト・画像 OSD | 独立したテキスト 3 か所と透過画像 1 か所を映像に合成。Web UI で配置や大きさを設定 | [OSD](docs/osd.md) |
| 表示内容の自動更新 | `osd-feed` がメモリ量・時刻・カウンタ・HTTP JSON をテンプレートに反映し、PNG を画像 OSD に変換 | [osd-feed](docs/osd-feed.md) |
| セルラー回線での運用 | RTMP チャンクサイズの調整で RTMPS の送信量を削減。切断・復帰の実測も記録 | [セルラー配信](docs/cellular.md) |

OSD はカメラのストリーマ `prudynt` 側で合成するため、OSD を使っても ffmpeg は stream copy のままです。
直接配信だけなら ffmpeg 単体で試せます。常設運用や OSD は必要なものを追加してください。

## まず配信を試す

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
  supervisor を導入する ([docs/relay.md](docs/relay.md))
- **2026-09 以降の Thingino は、ゲートウェイへの ping が約 90 秒通らないとカメラを OS ごと
  再起動する (netwatch、デフォルト有効)。** 配信が切れるので、長時間配信する前に無効化しておく:

  ```sh
  device/disable-netwatch.sh root@<camera-ip>
  # カメラ上で直接やるなら: jct /etc/thingino.json set netwatch.enabled false && service restart netwatch
  ```

  Thingino の OS 設定を変えるものなので、ffmpeg やスクリプトのインストールとは別の手順にしてある。
  理由と代償 (Wi-Fi が固まっても自動復旧しなくなる) は [docs/netwatch.md](docs/netwatch.md)

## 常設運用する

`device/` のインストーラは **Thingino `ciao+da40db6` 専用**です。他のビルドでは変更せずに中止します。
以下は、このリポジトリを取得した PC のリポジトリ直下で実行します。

```sh
# 対応する Release の ffmpeg を指定して、配信 supervisor と Web UI をインストール
device/install.sh root@<camera-ip> path/to/ffmpeg
```

インストール後、Thingino の **Services → YouTube Live** (`http://<camera-ip>/youtube.html`) を開き、
ストリームキーを設定・保存します。配信の開始・停止、再起動、自動起動の設定、ログ確認もこのページで行えます。

![YouTube Live の設定・状態表示](docs/images/youtube-webui.png)

配信設定は SD カード直下の `youtube-relay.json` が優先され、なければ `/etc/youtube-relay.json` を使います。
SD の抜去で配信を止めたい場合は、内蔵側の設定を無効にしておきます。
ただし、後述の `osd-feed` も使う場合は SD からプログラムを実行するため、動作中に SD を抜かないでください。
設定例・保存先・運用コマンドは [docs/relay.md](docs/relay.md) を参照してください。

長時間配信では、前述の netwatch の無効化も検討してください。
Wi-Fi 設定を SD で持ち運ぶ場合は [docs/wifi-from-sd.md](docs/wifi-from-sd.md) の別インストーラを使います。

## テキスト・ロゴ・外部データを映像に重ねる (任意)

OSD には `patches/prudynt-osd-textfile.diff` を当ててビルドした prudynt が必要です。
Releases の ffmpeg 単体には含まれません。ビルド手順は [docs/build.md](docs/build.md) を参照してください。

```sh
# パッチ入り prudynt と OSD の設定サービス・Web UI をインストール
device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt

# データの自動表示や PNG ロゴを使う場合は osd-feed も追加 (Go 1.25 以降と SD カードが必要)
device/osd-feed/build.sh
device/install-osd-feed.sh root@<camera-ip>
```

prudynt の導入時は配信が数秒中断します。ファームウェアの焼き直しは不要ですが、パッチ入り prudynt への
切り替えと、標準の起動スクリプトの調整を行います。詳しくは [docs/osd.md](docs/osd.md) を参照してください。

- **テキスト**: 3 か所に独立した複数行テキストを表示できます (ASCII のみ)。ファイルの更新は 0.1 秒間隔で確認されます。
- **画像**: 1 か所に透過 PNG のロゴなどを表示できます。`osd-feed` が PNG を prudynt 用の画像データへ変換します。
- **レイアウト**: **Streamer → OSD Settings** で、表示の有効化・位置・大きさ・色などを設定し、SD の `prudynt-osd.json` に保存できます。
- **自動表示**: SD の `osd-feed.json` にデータ源とテンプレートを設定すると、時刻・メモリ量・プログレスバー・HTTP JSON の値を表示できます。
  表示枠の有効化や配置は `prudynt-osd.json`、表示内容は `osd-feed.json` で設定します。

![透過ロゴ画像とテキスト OSD の表示例](docs/images/osd-image-logo.jpg)

PNG の置き場所は `osd-feed.json` で指定します (設定例では SD 直下の `logo.png`)。
大きな画像やテキストには OSD 用メモリの調整が必要です。
設定項目と制約は [docs/osd.md](docs/osd.md)、データ源・テンプレート・PNG 変換は
[docs/osd-feed.md](docs/osd-feed.md) にまとめています。

## 配信の仕組み

```text
従来:  Wyze Cam v3 → RTSP → 中継PC (ffmpeg) → RTMPS → YouTube Live
これ:  Wyze Cam v3 (ffmpeg入り) ──────────── RTMPS → YouTube Live
```

カメラ内部では以下を行います。ffmpeg での再エンコードは一切しません:

```text
localhost RTSP (prudynt) → H.264/AAC stream copy → FLV mux → RTMPS/TLS → YouTube Live
```

## リポジトリ構成

```text
patches/   Thingino / FFmpeg / prudynt に当てる差分
             thingino-ffmpeg-rtmps.diff         thingino-ffmpeg パッケージを RTMPS 対応にする
             thingino-ffmpeg-rtmp-chunk-size.diff  FFmpeg 本体: RTMP の送信チャンクサイズを rtmp_chunk_size オプション (既定 4096、128〜65536) で指定できるようにする。素の FFmpeg は 128 固定で、RTMPS の送信量が映像の 1.7 倍になる (4096 で 1.08 倍)
             prudynt-osd-textfile.diff          任意: prudynt にテキスト 3 枠・画像 1 枠の OSD を足す
             prudynt-msgchannel-warning.diff    任意: prudynt が 5 秒ごとに出す誤報の警告 (msgChannel sink clogged) を止める
device/    カメラ側の常設運用一式 (ファイルの一覧は device/README.md、使い方は docs/)
             youtube-relay / S93youtube-relay   supervisor と起動スクリプト
             www/youtube.html / www/x/json-youtube.cgi
                                                Web UI の「YouTube Live」ページ (キーの設定、再起動、サービスの有効/無効)
             install.sh                         上記と ffmpeg をカメラへ入れる (自前のファイルを置く + メニュー 1 行の追記)
             disable-netwatch.sh                Thingino の netwatch (OS 自動再起動) を無効化する
             install-wifi-from-sd.sh / S37wifi-from-sd
                                                任意: SD の wpa_supplicant.conf (複数 Wi-Fi 可) を起動時に適用
             install-prudynt-osd.sh / S30prudynt-osd / osd-progress-demo
                                                任意: 上記 OSD パッチ入りの prudynt を入れる、操作側のサンプル
             osd-config / S93osd-config / prudynt-osd.json.example
                                                任意: OSD の設定を SD カードのファイルから読んで prudynt に送り直す
             www/osd-settings.html / www/x/json-osd-text.cgi
                                                任意: Thingino の Web UI に足す OSD 設定ページ (テキスト・画像) と CGI
             osd-feed/ / S94osd-feed / install-osd-feed.sh
                                                任意: OSD テキストの自動更新と PNG の変換 (Go、SD カードから実行)
             common.sh                          対応ファームの判定 (違えば何も変更せず中止)
docs/      使い方と設定の文書 (下の「ドキュメント」参照)
NOTES.md   実装の詳細・設計判断・ハマりどころ・実測値の記録 (手順は書かない)
dist/      ビルド成果物 (git 管理外)。配布は GitHub Releases (ffmpeg バイナリ単体) で行う
LICENSE    MIT (第三者由来のものは LICENSE.md)。LICENSES/ に GPL-3.0-or-later、FFmpeg、Thingino のライセンス本文
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
| [docs/osd.md](docs/osd.md) | テキスト・画像 OSD: インストール、使い方、Web UI での編集、設定項目、SD カードの設定ファイル、大きさの上限 |
| [docs/osd-feed.md](docs/osd-feed.md) | `osd-feed`: データ源 (メモリ、カウンタ、時刻、HTTP JSON) とテンプレート、PNG の画像 OSD への変換、設定、計測 |
| [docs/wifi-from-sd.md](docs/wifi-from-sd.md) | Wi-Fi 設定を SD カードで運ぶ |
| [docs/netwatch.md](docs/netwatch.md) | netwatch (ping 失敗での OS 再起動) の無効化 |
| [docs/cellular.md](docs/cellular.md) | セルラー回線 (車載) での配信: 送信量 (RTMPS と平文 RTMP の実測)、切断からの復帰の実測、チューニングの選択肢 |
| [docs/troubleshooting.md](docs/troubleshooting.md) | 症状別の対処 |
| [docs/build.md](docs/build.md) | ffmpeg / prudynt のビルド手順 |
| [device/README.md](device/README.md) | `device/` のファイル一覧 |
| [NOTES.md](NOTES.md) | 経緯、設計判断、ハマりどころ、実測値、未検証事項の記録 |

## FFmpeg の特徴・実測負荷

- **バイナリ 2.5MB** (stripped)。FFmpeg 8.0.1 を RTSP 入力 + FLV/RTMPS 出力だけに絞った構成
- **stream copy 専用** — encoder/decoder/filter を一切含まない
- **TLS は mbedTLS** — Thingino 実機に入っている `libmbedtls.so.3.6.6` に動的リンク
- **ファームウェア世代ごとにビルドが必要** — toolchain (GCC / uClibc) を実機に揃えるため。
  対応表は下記「動作確認環境」
- **ファームウェア書き換え不要** — `/tmp` に転送して実行するだけ (PoC 用途)
- **RTMP チャンクサイズの調整** — パッチで送信チャンクの既定値を 128 から 4096 バイトへ変更。
  720p10 / 約 1Mbps で RTMPS の送信量は映像・音声の約 1.7 倍から約 1.08 倍へ低減
  (測定条件は [docs/cellular.md](docs/cellular.md))
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

## 補足

- **ライセンス**: このリポジトリの自作部分は **MIT** ([LICENSE](LICENSE))。第三者由来のもの (FFmpeg のソースを変える
  `patches/thingino-ffmpeg-rtmp-chunk-size.diff` と Releases の FFmpeg バイナリは **GPL-3.0-or-later**、prudynt のパッチは
  上流 prudynt-t にライセンス表記が無く再配布の許諾が確認できていない、など) と、配布バイナリの対応ソースの入手先は
  [LICENSE.md](LICENSE.md)
- **TLS 証明書検証**: FFmpeg 8.x のデフォルトでは無効です (通信は暗号化されます)。
  検証したい場合は CA バンドルを置き `-tls_verify 1 -ca_file <path>` を付けてください
- **ストリームキー**: シェル履歴やログに残ります。露出した場合は YouTube Studio で再生成を
- 実装の詳細・経緯・ハマりどころは [NOTES.md](NOTES.md) を参照

## ステータス

実機 (Wyze Cam v3、Thingino `ciao+da40db6`) で、YouTube Live への直接配信、supervisor による自動復帰、
SD カードでのスタンドアロン運用、長時間配信、テキスト・画像 OSD (720p)、`osd-feed` の動作・負荷を確認済みです。
`S37wifi-from-sd` による複数 Wi-Fi の適用も 2026-09-22 に実機で確認済みです。

1080p での OSD の負荷・メモリ上限と、Thingino 標準の `uenv.txt` による Wi-Fi 設定は未確認です。
Thingino パッケージとしての統合・ファームウェア組み込みは未実装です。
確認の経緯は [NOTES.md](NOTES.md)、画像 OSD と `osd-feed` の実測は
[docs/osd.md](docs/osd.md)・[docs/osd-feed.md](docs/osd-feed.md) を参照してください。

## 謝辞

- [Thingino](https://github.com/themactep/thingino-firmware) — オープンな IP カメラファームウェアと
  クロスビルド環境
- [FFmpeg](https://ffmpeg.org/)
