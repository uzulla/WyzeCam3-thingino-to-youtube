# 配信 supervisor (youtube-relay)

カメラ上で ffmpeg を監視・再起動し、YouTube Live への配信を続けるスクリプト (`/usr/sbin/youtube-relay` と
`/etc/init.d/S93youtube-relay`) の設定ファイル・運用・動作の仕組み。
**ファームウェアに入っているので、SD カードから入れた個体 ([sdcard.md](sdcard.md)) では何も入れなくてよい**:
ffmpeg はカード上の `/mnt/mmcblk0p1/ffmpeg`、設定はカード上の `youtube-relay.json` で、ストリームキーは
lapcam の Stream タブ ([lapcam.md](lapcam.md)) か `firmware/make-sdcard.sh -k` で書く。
うまく動かない時は [troubleshooting.md](troubleshooting.md)。

## 設定ファイル (`youtube-relay.json`) と「SD カードモード」

supervisor は常駐し、以下の順で設定を探す (10 秒間隔でポーリング):

```text
1. /mnt/mmcblk0p1/youtube-relay.json   (SD カード。Thingino が自動マウント)
2. /etc/youtube-relay.json             (内蔵フラッシュ、chmod 600)
どちらも無い/無効 → 配信せず待機
```

これにより **SD カードが物理スイッチ**になる (ただし `/etc/youtube-relay.json` で配信している最中に SD を挿しても、監視しているのは
使用中の設定だけなので、SD の設定に切り替わるのは ffmpeg が次に終わった時。すぐ切り替えるなら `service restart youtube-relay`):

- `youtube-relay.json` を書いた SD を**挿す** → 自動マウント後、次のポーリングで配信開始
- SD を**抜く** → 配信中の監視 (15 秒間隔) が設定消失を検知して配信を止める。内蔵の `/etc/youtube-relay.json` が
  有効ならその設定で配信を再開し、内蔵の設定も無い/無効なら待機に戻る
  (抜いたら必ず止まるようにしたい時は、内蔵の設定を置かないか `"enabled": false` にしておく)
- 最小構成は `{"stream_key": "xxxx"}` の 1 行だけで動く (他はデフォルト値)
- `"enabled": false` を書くと明示的に無効化できる (デフォルトは有効)

| キー | 意味 (省略時) |
|---|---|
| `stream_key` | YouTube のストリームキー (必須) |
| `enabled` | `false` で配信しない (`true`) |
| `rtmp_url` | 送信先 (`rtmps://a.rtmps.youtube.com:443/live2`。平文は `rtmp://a.rtmp.youtube.com/live2`) |
| `rtsp_url` | 入力 (`rtsp://thingino:thingino@127.0.0.1:554/ch0`。Thingino の既定の認証とパス) |
| `ffmpeg_bin` | ffmpeg のフルパス (下の探索順) |
| `ffmpeg_opts` | `-i` の**前**に展開する入力オプション。例 `-timeout 5000000` |
| `ffmpeg_out_opts` | `-i` の**後**に展開する出力オプション。例 `-map 0:v:0 -map 0:a:0`、`-rtmp_chunk_size 4096` |
| `ffmpeg_loglevel` | `-loglevel` の値 (`error`) |

オプションは置き場所が違うと ffmpeg が起動エラーになるので注意 (`-map` は出力オプション)。
lapcam の Stream タブは `stream_key` と `rtmp_url` を書き (`enabled` が無ければ `true` を足す)、ファイルにある他のキーは残す。

## ffmpeg バイナリの探索順

supervisor は **一時的な場所が常設を上書きする**順で探す:

```text
/mnt/mmcblk0p1/ffmpeg → /tmp/ffmpeg → /usr/bin/ffmpeg → /usr/local/bin/ffmpeg
```

- SD カードから入れた個体は `/mnt/mmcblk0p1/ffmpeg` (make-sdcard.sh が置く) を使う。フラッシュには入れていない
  (rootfs の空きが足りない。[sdcard.md](sdcard.md))。カードを抜くと配信できない
- 新しいビルドを試すときは `/tmp/ffmpeg` (tmpfs。**再起動で消える**) に置き、設定の `ffmpeg_bin` に `/tmp/ffmpeg` と書く
  (カードの ffmpeg の方が探索で先なので、置くだけでは切り替わらない)。カードや常設のバイナリはそのまま残り、再起動すると
  `/tmp` が消えて探索に戻る (`ffmpeg_bin` の行は消す)
- 明示指定したい場合は設定の `ffmpeg_bin` にフルパスを書く (探索より優先)。ただし指定先が存在しない・起動できない・
  RTMPS 非対応の場合は上記の探索にフォールバックする
- 使う前に `-protocols` を実行して「このファームで起動でき、出力プロトコル (rtmps) を持つ」ことを確認する。
  ファーム更新後に SD に残った旧 toolchain 世代のバイナリや、Thingino 自身の録画用 ffmpeg (RTMPS 非対応。
  `BR2_PACKAGE_PRUDYNT_T_FFMPEG` を有効にしたカスタムビルドにだけ入る。公式イメージには無い) を誤って使わないため

## 運用

普段の操作は lapcam の Camera タブ (Go live / Stop live) と Stream タブ (キー、自動起動、画質、ログ) で済む。ssh からは:

```sh
service stop youtube-relay      # 配信停止 (supervisor ごと止まる)
service start youtube-relay     # 配信開始
service disable youtube-relay   # boot 時の自動開始を無効化 (init スクリプトの実行ビット)
service enable youtube-relay    # 有効化
/etc/init.d/S93youtube-relay status
logread | grep youtube-relay | tail -20
```

ストリームキー・URL・ffmpeg のオプションを変えた後は `service restart youtube-relay` (`enabled` の変更だけなら
relay が自分で追従するので不要。OFF は watcher が 15 秒以内に止め、ON は待機ループが 10 秒以内に再開する)。
編集するのは **relay が使っている方のファイル** (SD カードに `youtube-relay.json` があればそちらが優先され、
`/etc/youtube-relay.json` を直しても反映されない)。lapcam も同じ順で選んで書く。
`stop` は supervisor と ffmpeg が終わるまで待ってから戻る (45 秒待っても残っていれば kill し、さらに最大 5 秒確認する
ので、詰まった時は約 50 秒。待たずに `start` すると新旧 2 つが同じキーへ publish する)。

### 配信できているかの見分け方

カメラ側から分かるのは「YouTube のサーバーが受け取り続けているか」まで。YouTube の画面に実際に出ているか
(配信枠の状態、YouTube Studio の自動開始の設定など) はカメラからは分からない (YouTube Data API の
`liveStreams` の `streamStatus` / `healthStatus` なら分かるが、Google アカウントの OAuth が要る。未実装)。

| 状況 | `logread \| grep youtube-relay` での見え方 |
|---|---|
| **正常に配信している** | `Starting ffmpeg` の後、`ffmpeg exited` が出ないまま続く。relay は **60 秒以上動いた実行を正常**とみなす (終わっても 2 秒で再起動し、debug 診断はしない) |
| **キーの間違い / YouTube が拒否** | YouTube は接続 (TLS、RTMP の handshake) を受け付けた後、publish を受けたところで切断する。ffmpeg は `Input/output error` (`rc=251`) で数秒で終わり、`ffmpeg exited (rc=251) after 数秒` を繰り返す |
| **既定経路が無い** (Wi-Fi 未接続など) | ffmpeg を起動せず `Network down (no default route), waiting` を 1 回出して待つ |
| **名前が引けない / 届かない / 拒否される** | ffmpeg が接続の段階で数秒以内に終わる。直前の `ffmpeg:` 行に `Failed to resolve hostname` / `Network is unreachable` / `Connection refused` / `Connection timed out` など (文言は ffmpeg の一般的なもので、このカメラでの確認はしていない) |

- 60 秒未満で終わる実行が続くと、relay は次の 1 回だけ ffmpeg を `-loglevel debug` で起動する (`loglevel debug to diagnose the failure`)。
  その出力に `Server version` と `Sending publish command` まで出ていれば YouTube までは届いていて、**キー (または配信枠) の問題**。
  そこまで出ずに終わっていれば、**ネットワークか TLS の問題** (`rc=251` だけでは両者を区別できないため)
- 無効なキーで YouTube の実エンドポイントに繋いだ時の流れ (`Handshaking...` → `Server version 4.0.0.1` → ... →
  `Sending publish command` → サーバ切断) は [NOTES.md](../NOTES.md) に記録がある
- lapcam の Camera タブには配信中 LIVE と経過時間、Stream 欄に ffmpeg の再起動回数が出る。経過時間が 1 分を超えて伸びていれば配信できている

## 動作の仕組み (Thingino の流儀に準拠)

- **systemd はない**。busybox init が `/etc/init.d/S*` を番号順に実行する。`S93` は
  ネットワーク (S40 前後) と prudynt (S31) の後
- クラッシュ・切断対応は **supervisor のシェルループ**が担う: ffmpeg 終了を検知して再起動、
  60 秒以上動いていた後の失敗は 2 秒で即時再起動、連続の即死は 4→8→10 秒 (上限 10 秒)
  (ライブ配信の欠損を最小にする方針。恒久的な失敗でも約 10 秒毎の再試行で YouTube 相手には無害)。
  ネットワーク断 (デフォルトルート消失) 中は ffmpeg を起動せず 5 秒間隔で復帰を待つ
  (「Network down」のログが 1 回出る)。全ての起動試行は「Starting ffmpeg」としてログに残る。
  `thingino-ha` パッケージの watchdog と同じ構造
- 起動前に**デフォルトルートができるまで待ち** (ICMP を落とすルーターで詰まらないよう
  ping は意図的にしない)、NTP 同期フラグ (`/run/sync_success`) を最大 60 秒待つ
- `start-stop-daemon -N 10` で低優先度起動 — prudynt / ISP 処理と CPU を取り合わない
  (`telegrambot` と同じ流儀)
- ストリームキーが未設定でも supervisor は常駐し、設定が現れるまで**配信せず待機** (無害)
- ネットワーク断で OS ごと再起動する Thingino の netwatch は、復帰を supervisor に任せるためファームウェアでは無効
  ([netwatch.md](netwatch.md))
- **送信量**: 素の FFmpeg は RTMP を 128 バイトのチャンクに刻み、チャンクごとに (しかも継続ヘッダの 1 バイトも別に)
  書き出す。RTMPS ではその 1 回ごとが TLS レコード (レコード 1 つに約 29 バイトの枝葉。128 バイトのチャンク 1 つで
  レコード 2 つ = 約 58 バイト) になり、小さな書き込みが多いぶん TCP/IPv6 ヘッダの比率も上がるので、**送信量が映像 + 音声の
  約 1.7 倍**になる (720p10 / 1Mbps で実測 1.05 Mbps → 1.77 Mbps。TLS・TCP・IPv6 ヘッダをすべて含む線上の値)。`patches/thingino-ffmpeg-rtmp-chunk-size.diff` を当てた
  ffmpeg (配布している `ffmpeg` はこれ) は接続直後にチャンクサイズ 4096 をサーバへ宣言し、**1.13 Mbps** (1.08 倍) になる。65536 にしても 1.11 Mbps で
  差は無い。値は `-rtmp_chunk_size` (`ffmpeg_out_opts` に書く。128〜65536、既定 4096) で変えられ、128 で素の動作に戻る。
  **平文の RTMP** (`rtmp_url` を `rtmp://a.rtmp.youtube.com/live2` に。ポート 1935) なら TLS の枝葉が無く、パッチ入りで
  1.09 Mbps、パッチ無しでも TCP が小さな書き込みをまとめるので 1.13 Mbps。ffmpeg の CPU は 2.5% → 2.2% (パッチ無しの
  平文は 3.6%)。RTMPS との差は約 3% で、代わりにストリームキーが経路上を平文で流れる (下の「ストリームキーの取り扱い」)。
  確認は `awk '/^Ip6OutOctets/{print $2}' /proc/net/snmp6` の差分 (YouTube が IPv6 の場合。この値は IPv6 の送信全体
  なので、他に IPv6 で通信しているものが無い時だけ YouTube 分と見なせる。上の実測は LAN の ssh が IPv4 で、それ以外の
  接続が無いことを `netstat -tn` で見た上での値。IPv4 なら `/proc/net/netstat` の `IpExt OutOctets` だが、こちらは
  loopback の RTSP 分も含む)

## ストリームキーの取り扱い

- 内蔵運用 (`/etc/youtube-relay.json`) は **chmod 600**。物理アクセスされない限り漏れない
- **SD カードモードはトレードオフを理解して使う**: FAT にパーミッションは無く、カードを
  抜かれればキーは読まれる。車載等でカメラごと盗まれるリスクと大差ない場面では実用的だが、
  キーだけ守りたい据え置き用途では内蔵運用を選ぶ
- **ffmpeg プロセスの引数 (`ps` / `/proc/*/cmdline`) には、ストリームキー入りの RTMPS URL と
  RTSP の認証情報がそのまま載る**。Thingino は全プロセス root のシングルユーザー機なので
  ローカルでの実害はないが、`ps` 出力を含むログ・スクリーンショット・サポート依頼の貼り付けに
  キーが写り得る点に注意 (supervisor 自身の syslog 出力だけは REDACTED 表記にしてある)
- lapcam は平文の HTTP で、Stream タブはストリームキーを認証済みのブラウザに返す。**LAN の外からは使わない**
  ([lapcam.md](lapcam.md))
- 漏洩したら YouTube Studio でキーを再生成し、lapcam の Stream タブで差し替える (配信中なら再起動して反映)

## 素の Thingino に手で入れる場合

自前のファームウェアを焼かずに、素の Thingino (`ciao+da40db6`) に入れる時だけ。配信中に再実行してもよい
(旧 supervisor の停止を待って入れ替え、数秒で配信が再開する)。

```sh
# ffmpeg (dist/ffmpeg、2.6 MB) を /usr/bin/ffmpeg に、スクリプトと init スクリプトを overlay に置く。
# 既存の /etc/youtube-relay.json は上書きしない。スクリプトだけ入れ直す場合は ffmpeg の引数を省略
device/install.sh root@<camera-ip> dist/ffmpeg
device/disable-netwatch.sh root@<camera-ip>     # 推奨 (netwatch.md)
```

- `install.sh` / `disable-netwatch.sh` は**特定のファーム (`ciao+da40db6`) 決め打ち**で、カメラの `/etc/os-release` が
  違えば何も変更せずに止まる。ffmpeg はそのファームの toolchain でビルドしたものしか動かず、スクリプトもその構成を
  前提にしているため。別のファームに上げるときは、ビルドし直した上で `common.sh` の `SUPPORTED_BUILD` を更新する
- 手作業なら: `dist/ffmpeg` → `/usr/bin/ffmpeg` (755。Thingino は SFTP 非対応なので `scp -O` か `cat | ssh ... 'cat > ...'`。
  overlay の空きは `df -h /overlay`。Wyze Cam v3 のデータパーティションは 8.5 MB で 2.6 MB は入る)、
  `device/youtube-relay` → `/usr/sbin/youtube-relay` (755)、`device/S93youtube-relay` → `/etc/init.d/S93youtube-relay` (755)、
  設定は `device/youtube-relay.json.example` を元に SD か `/etc/youtube-relay.json` (600) へ。`/etc/init.d/S93youtube-relay start`
- Thingino を入れ直したり更新したりすると overlay ごと消える (`/usr/bin/ffmpeg`、スクリプト、`/etc/youtube-relay.json`) ので、
  その後にもう一度 `install.sh` を実行する
- `install.sh` は旧 Thingino UI 向けの YouTube Live ページ (`device/www/youtube.html`、`/x/json-youtube.cgi`) も足す。
  uhttpd を使っている個体でだけ動く (lapcam がポート 80 を取っている個体では使わない)
