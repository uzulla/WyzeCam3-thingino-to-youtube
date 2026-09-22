# 配信 supervisor (youtube-relay)

カメラ上で ffmpeg を監視・再起動し、YouTube Live への配信を続けるスクリプト一式の、インストール・設定・運用。
コマンド例はリポジトリのルートで実行する前提。うまく動かない時は [troubleshooting.md](troubleshooting.md)。

## まず `/tmp` で試す (PoC)

まず `/tmp` で動作確認する (**`/tmp` は RAM 上なので再起動で消える = PoC 専用**。
常設する場合は overlay で永続化される `/usr/bin/ffmpeg` へ —
場所の選び方は下の「ffmpeg バイナリの設置場所」):

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

## インストール手順

```sh
# まとめて入れる (再実行可能。既存の /etc/youtube-relay.json は上書きしない)
device/install.sh root@<camera-ip> dist/ffmpeg
# スクリプトだけ入れ直す場合は ffmpeg の引数を省略

# 推奨: ping 失敗で OS ごと再起動する netwatch を止める (OS 設定の変更。[netwatch.md](netwatch.md) 参照)
device/disable-netwatch.sh root@<camera-ip>
```

配信中に再実行してもよい (旧 supervisor の停止を待って入れ替え、数秒で配信が再開する)。
うまくいかない場合は下の手作業の手順で。

追加の ffmpeg オプションは設定 JSON で渡せる。置き場所が違うと ffmpeg が起動エラーになるので注意:

| キー | 展開位置 | 例 |
|---|---|---|
| `ffmpeg_opts` | `-i` の**前** (入力オプション) | `-timeout 5000000` |
| `ffmpeg_out_opts` | `-i` の**後** (出力オプション) | `-map 0:v:0 -map 0:a:0` |
| `ffmpeg_loglevel` | `-loglevel` の値 (省略時 `error`) | `warning` |

`install.sh` / `disable-netwatch.sh` は**特定のファーム (`ciao+da40db6`) 決め打ち**で、カメラの
`/etc/os-release` が違えば何も変更せずに止まる。ffmpeg はそのファームの toolchain でビルドした
ものしか動かず、スクリプトもそのファームの構成 (netwatch がある等) を前提にしているため。
別のファームに上げるときは、ビルドし直した上で `common.sh` の `SUPPORTED_BUILD` を更新する。

`install.sh` がやっていることを手作業で行う場合:

```sh
CAM=root@<camera-ip>

# 1. ffmpeg バイナリ (dist/ffmpeg) を永続領域へ
#    まず空き容量を確認 (2.6MB 必要。/overlay が JFFS2 データパーティション)
ssh $CAM 'df -h | grep -E "overlay|Filesystem"'
cat dist/ffmpeg | ssh $CAM 'cat > /usr/bin/ffmpeg && chmod +x /usr/bin/ffmpeg'
#    空きが足りない場合は SD カードに置き、設定の ffmpeg_bin をそのパスにする

# 2. スクリプト
cat device/youtube-relay      | ssh $CAM 'cat > /usr/sbin/youtube-relay && chmod +x /usr/sbin/youtube-relay'
cat device/S93youtube-relay   | ssh $CAM 'cat > /etc/init.d/S93youtube-relay && chmod +x /etc/init.d/S93youtube-relay'

# 3. 設定 — どちらか:
#  a) 内蔵に置く (据え置き運用。キーはカメラ上で直接編集)
cat device/youtube-relay.json.example | ssh $CAM 'cat > /etc/youtube-relay.json && chmod 600 /etc/youtube-relay.json'
ssh $CAM 'vi /etc/youtube-relay.json'    # stream_key を記入
#  b) SD カードに置く (スタンドアロン運用)
#     PC で SD 直下に youtube-relay.json を書いてカメラに挿すだけ

# 4. 起動
ssh $CAM '/etc/init.d/S93youtube-relay start'

# 状態確認・ログ
ssh $CAM '/etc/init.d/S93youtube-relay status'
ssh $CAM 'logread | grep youtube-relay | tail -20'
```

## 設定ファイルの探索順と「SD カードモード」

supervisor は常駐し、以下の順で設定を探します (10秒間隔でポーリング):

```text
1. /mnt/mmcblk0p1/youtube-relay.json   (SD カード。Thingino が自動マウント)
2. /etc/youtube-relay.json             (内蔵フラッシュ、chmod 600)
どちらも無い/無効 → 配信せず待機
```

これにより **SD カードが物理スイッチ**になります:

- `youtube-relay.json` を書いた SD を**挿す** → 自動マウント後、次のポーリングで配信開始
- SD を**抜く** → 配信中の監視 (15秒間隔) が設定消失を検知して配信を止める。内蔵の `/etc/youtube-relay.json` が
  有効ならその設定で配信を再開し、内蔵の設定も無い/無効なら待機に戻る
  (抜いたら必ず止まるようにしたい時は、内蔵の設定を置かないか `"enabled": false` にしておく)
- 最小構成は `{"stream_key": "xxxx"}` の 1 行だけで動く (他はデフォルト値)
- `"enabled": false` を書くと明示的に無効化できる (デフォルトは有効)

ffmpeg バイナリとスクリプトは内蔵フラッシュに常設し、SD はキーだけを運ぶ分担を推奨。
Wyze Cam v3 の overlay (データパーティション) は 8.5MB で、ffmpeg 2.6MB は問題なく入る。

## ffmpeg バイナリの設置場所

**`/tmp` は tmpfs (RAM) なので再起動で消える。PoC 専用であり、常設運用では使わないこと。**

Thingino はルート全体が overlayfs (SquashFS + JFFS2 データパーティション) で覆われており、
どこに書いてもフラッシュに永続化される。設置場所は以下から選ぶ:

| 場所 | 永続性 | 用途 | 備考 |
|---|---|---|---|
| `/usr/bin/ffmpeg` | overlay で永続 | **常設 (推奨)** | PATH も通る |
| `/mnt/mmcblk0p1/ffmpeg` | SD カード | フル SD 運用 / テスト | バイナリ+設定を SD で完結できる。SD を抜くと配信不可 |
| `/tmp/ffmpeg` | **再起動で消える** | PoC / テスト | 動作確認が終わったら /usr/bin へ移す |

- 事前に空き容量を確認する: `df -h /overlay`
  (Wyze Cam v3 実機ではデータパーティション 8.5MB 中 約8.2MB 空き。ffmpeg は 2.6MB なので余裕)
- supervisor の探索順は **一時的な場所が常設を上書きする** 設計:

  ```text
  /mnt/mmcblk0p1/ffmpeg → /tmp/ffmpeg → /usr/bin/ffmpeg → /usr/local/bin/ffmpeg
  ```

  新しいビルドを試すときは SD か /tmp に置くだけでよく、常設バイナリはそのまま残る。
  再起動 (/tmp が消える) や SD 抜去で自動的に常設版へ戻る。
  明示指定したい場合は設定の `ffmpeg_bin` にフルパスを書く (探索より優先)。ただし指定先が存在しない・
  起動できない・RTMPS 非対応の場合は上記の探索にフォールバックする
- PoC で `/tmp/ffmpeg` に置いたバイナリを常設に昇格するには (実機上で):

  ```sh
  cp /tmp/ffmpeg /usr/bin/ffmpeg
  chmod +x /usr/bin/ffmpeg
  md5sum /tmp/ffmpeg /usr/bin/ffmpeg   # 一致を確認
  ```

## 運用

```sh
service stop youtube-relay      # 配信停止 (supervisor ごと止まる)
service start youtube-relay     # 配信開始
service disable youtube-relay   # boot 時の自動開始を無効化
service enable youtube-relay    # 有効化
```

設定変更後は `service restart youtube-relay`。編集するのは **relay が使っている方のファイル** (SD カードに
`youtube-relay.json` があればそちらが優先され、`/etc/youtube-relay.json` を直しても反映されない。次の節)。Web UI の
ページは使っている方に書く。`stop` は supervisor と ffmpeg が終わるまで待ってから戻る (最大 45 秒。待たずに `start`
すると新旧 2 つが同じキーへ publish する)。

## Web UI

`install.sh` が Thingino の Web UI に **Services → YouTube Live** (`http://<camera-ip>/youtube.html`) を足す。
上の運用コマンドと設定ファイルの編集を、ブラウザからできるようにしたもの。

![YouTube Live ページ](images/youtube-webui.png)

- **Stream settings**: `enabled`、ストリームキー (伏せ字、目のボタンで表示)、Ingest URL (RTMPS / 平文 RTMP のプリセットか任意)、
  Advanced に RTSP の URL と ffmpeg のオプション。**Save** は relay が今使っているファイル (SD → `/etc` の順。無ければ SD が
  あれば SD、なければ `/etc`) に 600 で書く。ファイルにあってページに無いキー (`ffmpeg_bin` など) は残る
  - キー・URL・オプションを変えた時は、配信中なら「今すぐ再起動して反映するか」を聞く (配信が数秒切れる)。
    Cancel すると保存だけして、次の再起動まで古い設定で配信を続ける
  - `enabled` の変更だけなら聞かない: relay が自分で追従する (OFF は watcher が 15 秒以内に止める、ON は待機ループが 10 秒以内に再開する)
- **Status**: relay サービスと ffmpeg の稼働 (pid、配信の経過時間)、起動時の自動開始、使っている設定ファイル。5 秒ごとに更新
- **Relay service**: Start / Stop / **Restart stream (ffmpeg)** (= `service restart youtube-relay`)。「Start the relay service at
  boot」のスイッチが `service enable/disable` (init スクリプトの実行属性。切っても動いているものは止まらない)
- **Restart prudynt**: Thingino の `/x/restart-prudynt.cgi` を呼ぶ (映像と OSD が数秒止まり、ffmpeg は自動で再接続)
- **Log**: `logread` の youtube-relay 行 (キーは relay が伏せている)。5 秒ごとに更新
- 裏側の CGI は `/x/json-youtube.cgi` (`?action=status` / `save[&restart=1]` / `service&op=…`)。Thingino の認証 (セッション
  Cookie か `?token=<API キー>`) を通せばカメラ外からも使える。ストリームキーは認証済みのブラウザにそのまま返す
  (Thingino が API キーや Wi-Fi のパスワードを画面に出すのと同じ扱い。[下記](#ストリームキーの取り扱い))。
  Thingino の Web UI は平文 HTTP なので、**LAN の外からは使わない** (この CGI に限らず、ログイン情報も API キーも
  ストリームキーも平文で流れる。`?token=` は URL に残るのでブラウザ履歴やプロキシのログにも載る)。カメラ外の
  プログラムから叩くのは同じ LAN 内 (または VPN 越し) に限る
- Restart / Stop は supervisor の終了を待つので、詰まっている時は最大 45 秒応答が返らない (uhttpd は CGI を
  打ち切らない設定 `-t 0`。実機で 40 秒無応答の CGI が通ることを確認)。普段は 2 秒程度
- 「Restart prudynt」は Thingino 標準の `/x/restart-prudynt.cgi` を呼ぶ (対応ファーム ciao+da40db6 に入っている。
  無いファームでは 404 になり、ページにエラーが出るだけ)
- 手で入れるなら: `device/www/youtube.html` → `/var/www/youtube.html` (644)、`device/www/x/json-youtube.cgi` → `/var/www/x/json-youtube.cgi`
  (755)、メニューは `/var/www/a/plugins.js` の `cfg.plugins = {` の次の行に install.sh が挿している 1 行 (`"youtube-relay": {…},`) を足して 644 にする
  (無くても URL で開ける)
- Save や Start / Stop / Restart は CGI 側で 1 つずつ直列化される (同時に押すと 409)。ボタンは操作中はまとめて無効になる
- 消す: `/var/www/youtube.html` `/var/www/x/json-youtube.cgi` を削除し、`/var/www/a/plugins.js` から `"youtube-relay": {…},` の
  1 行を消す

## Thingino を入れ直した / 更新した後

このディレクトリのファイルと `/usr/bin/ffmpeg` は、Thingino を新規インストールした直後の
カメラに入れる想定。Thingino を入れ直したり更新したりするとカメラ上の書き込み領域ごと消えるので、
その後にもう一度 `install.sh` を実行する (ストリームキーも入れ直す):

```sh
# ffmpeg はそのファーム (toolchain 世代) に合わせてビルドしたものを使う (対応表は [README](../README.md))
device/install.sh root@<camera-ip> dist/ffmpeg
```

Thingino 自体の更新や設定のバックアップはこのリポジトリの範囲外。

## 動作の仕組み (Thingino の流儀に準拠)

- **systemd はない**。busybox init が `/etc/init.d/S*` を番号順に実行する。`S93` は
  ネットワーク (S40前後) と prudynt (S31) の後
- クラッシュ・切断対応は **supervisor のシェルループ**が担う: ffmpeg 終了を検知して再起動、
  60秒以上動いていた後の失敗は2秒で即時再起動、連続の即死は 4→8→10秒 (上限10秒)
  (ライブ配信の欠損を最小にする方針。恒久的な失敗でも約10秒毎の再試行で YouTube 相手には無害)。
  ネットワーク断 (デフォルトルート消失) 中は ffmpeg を起動せず5秒間隔で復帰を待つ
  (「Network down」のログが1回出る)。全ての起動試行は「Starting ffmpeg」としてログに残る
  `thingino-ha` パッケージの watchdog と同じ構造
- 起動前に**デフォルトルートができるまで待ち** (ICMP を落とすルーターで詰まらないよう
  ping は意図的にしない)、NTP 同期フラグ (`/run/sync_success`) を最大60秒待つ
- `start-stop-daemon -N 10` で低優先度起動 — prudynt / ISP 処理と CPU を取り合わない
  (`telegrambot` と同じ流儀)
- ストリームキーが未設定でも supervisor は常駐し、設定が現れるまで**配信せず待機** (無害)
- ffmpeg は `-protocols` を実行して「このファームで起動でき、出力プロトコル (rtmps) を
  持つ」ことを確認してから使う。ファーム更新後に SD に残った旧 toolchain 世代のバイナリや、
  Thingino 自身の録画用 ffmpeg (RTMPS 非対応。`BR2_PACKAGE_PRUDYNT_T_FFMPEG` を有効にした
  カスタムビルドにだけ入る。公式イメージには無い) を誤って使わないため
- 有効/無効の切り替えは Thingino 標準の `service enable|disable youtube-relay`
  (init スクリプトの実行ビットで制御) か、JSON の `"enabled"` フラグ
- **送信量**: 素の FFmpeg は RTMP を 128 バイトのチャンクに刻み、チャンクごとに (しかも継続ヘッダの 1 バイトも別に)
  書き出す。RTMPS ではその 1 回ごとが TLS レコード (レコード 1 つに約 29 バイトの枝葉。128 バイトのチャンク 1 つで
  レコード 2 つ = 約 58 バイト) になり、小さな書き込みが多いぶん TCP/IPv6 ヘッダの比率も上がるので、**送信量が映像 + 音声の
  約 1.7 倍**になる (720p10 / 1Mbps で実測 1.05 Mbps → 1.77 Mbps。TLS・TCP・IPv6 ヘッダをすべて含む線上の値)。`patches/thingino-ffmpeg-rtmp-chunk-size.diff` を当てた
  ffmpeg は接続直後にチャンクサイズ 4096 をサーバへ宣言し、**1.13 Mbps** (1.08 倍) になる。65536 にしても 1.11 Mbps で
  差は無い。値は `-rtmp_chunk_size` (`ffmpeg_out_opts` に書く。128〜65536、既定 4096) で変えられ、128 で素の動作に戻る。
  **平文の RTMP** (`rtmp_url` を `rtmp://a.rtmp.youtube.com/live2` に。ポート 1935) なら TLS の枝葉が無く、パッチ入りで
  1.09 Mbps、パッチ無しでも TCP が小さな書き込みをまとめるので 1.13 Mbps。ffmpeg の CPU は 2.5% → 2.2% (パッチ無しの
  平文は 3.6%)。RTMPS との差は約 3% で、代わりにストリームキーが経路上を平文で流れる (下の「ストリームキーの取り扱い」)
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
- 漏洩したら YouTube Studio でキーを再生成し、JSON を更新して restart
