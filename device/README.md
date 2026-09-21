# device/ — カメラへインストールするファイル

Thingino 実機に置く supervisor 一式。`/etc` 以下は overlayfs でフラッシュに永続化されるため、
ファームウェアを書き換えずにインストールでき、再起動後も残ります。

| ファイル | インストール先 | 役割 |
|---|---|---|
| `youtube-relay` | `/usr/sbin/youtube-relay` | supervisor 本体 (ffmpeg を監視・再起動するループ) |
| `S93youtube-relay` | `/etc/init.d/S93youtube-relay` | 起動スクリプト (boot 時に自動開始) |
| `youtube-relay.json.example` | `/etc/youtube-relay.json` または SD カード直下 | 設定ファイル (下記「SD カードモード」参照) |
| `install.sh` | (PC 側で実行) | 上記と ffmpeg を SSH 経由でまとめて入れるインストーラ。再実行可能 |

## 設定ファイルの探索順と「SD カードモード」

supervisor は常駐し、以下の順で設定を探します (10秒間隔でポーリング):

```text
1. /mnt/mmcblk0p1/youtube-relay.json   (SD カード。Thingino が自動マウント)
2. /etc/youtube-relay.json             (内蔵フラッシュ、chmod 600)
どちらも無い/無効 → 配信せず待機
```

これにより **SD カードが物理スイッチ**になります:

- `youtube-relay.json` を書いた SD を**挿す** → 自動マウント後、次のポーリングで配信開始
- SD を**抜く** → 配信中の監視 (15秒間隔) が設定消失を検知し、配信停止して待機に戻る
- 最小構成は `{"stream_key": "xxxx"}` の 1 行だけで動く (他はデフォルト値)
- `"enabled": false` を書くと明示的に無効化できる (デフォルトは有効)

ffmpeg バイナリとスクリプトは内蔵フラッシュに常設し、SD はキーだけを運ぶ分担を推奨。
Wyze Cam v3 の overlay (データパーティション) は 8.5MB で、ffmpeg 2.6MB は問題なく入る。

## 設計 (Thingino の流儀に準拠)

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

## インストール手順

```sh
# まとめて入れる (再実行可能。既存の /etc/youtube-relay.json は上書きしない)
./install.sh root@<camera-ip> ../dist/ffmpeg
# スクリプトだけ入れ直す場合は ffmpeg の引数を省略
```

> `install.sh` は 2026-09 に追加したもので、実機での通し実行はまだ確認できていない
> (構文チェックと選択ロジックの単体確認のみ)。うまくいかない場合は下の手作業の手順で。

追加の ffmpeg オプションは設定 JSON で渡せる。置き場所が違うと ffmpeg が起動エラーになるので注意:

| キー | 展開位置 | 例 |
|---|---|---|
| `ffmpeg_opts` | `-i` の**前** (入力オプション) | `-timeout 5000000` |
| `ffmpeg_out_opts` | `-i` の**後** (出力オプション) | `-map 0:v:0 -map 0:a:0` |

`install.sh` がやっていることを手作業で行う場合:

```sh
CAM=root@<camera-ip>

# 1. ffmpeg バイナリ (dist/ffmpeg) を永続領域へ
#    まず空き容量を確認 (2.6MB 必要。/overlay が JFFS2 データパーティション)
ssh $CAM 'df -h | grep -E "overlay|Filesystem"'
cat ../dist/ffmpeg | ssh $CAM 'cat > /usr/bin/ffmpeg && chmod +x /usr/bin/ffmpeg'
#    空きが足りない場合は SD カードに置き、設定の ffmpeg_bin をそのパスにする

# 2. スクリプト
cat youtube-relay      | ssh $CAM 'cat > /usr/sbin/youtube-relay && chmod +x /usr/sbin/youtube-relay'
cat S93youtube-relay   | ssh $CAM 'cat > /etc/init.d/S93youtube-relay && chmod +x /etc/init.d/S93youtube-relay'

# 3. 設定 — どちらか:
#  a) 内蔵に置く (据え置き運用。キーはカメラ上で直接編集)
cat youtube-relay.json.example | ssh $CAM 'cat > /etc/youtube-relay.json && chmod 600 /etc/youtube-relay.json'
ssh $CAM 'vi /etc/youtube-relay.json'    # stream_key を記入
#  b) SD カードに置く (スタンドアロン運用)
#     PC で SD 直下に youtube-relay.json を書いてカメラに挿すだけ。手順 4 は不要

# 4. ファーム更新時のバックアップ対象に登録 (設定 + スクリプト 2 本。下記「ファームウェア更新」参照)
for p in /etc/youtube-relay.json /usr/sbin/youtube-relay /etc/init.d/S93youtube-relay; do
  ssh $CAM "grep -qxF $p /etc/cfg-backup.list || echo $p >> /etc/cfg-backup.list"
done

# 5. 起動
ssh $CAM '/etc/init.d/S93youtube-relay start'

# 状態確認・ログ
ssh $CAM '/etc/init.d/S93youtube-relay status'
ssh $CAM 'logread | grep youtube-relay | tail -20'
```

## ファームウェア更新 (重要: overlay は消える)

Thingino の rootfs / full アップグレードは **overlay を消去する**。つまりこのディレクトリの
ファイルと `/usr/bin/ffmpeg` は全部消える (公式イメージに ffmpeg は入っていない —
`ciao+da40db6` 実機で確認)。残せるのは 64KB の backup パーティションに入る分だけ:

- `/etc/cfg-backup.list` に載っているパスが、**`sysupgrade -B` (`--backup`) を付けたときだけ**
  退避され、更新後の初回起動で `S37cfg-autorestore` が自動復元する (`-B` はデフォルト無効)
- 無圧縮 tar で上限 65472 バイト。設定とスクリプト 2 本 (約 9KB) は入るが、
  **ffmpeg (2.5MB) は入らない**。`install.sh` が登録と収支チェックをする
- 手元で確認するには `cfg-backup write` (収まらなければ `backup too large` で失敗する)

更新の流れ:

```sh
ssh $CAM 'sysupgrade -B -f'                       # 設定を退避して更新
# 更新後のファーム (toolchain 世代) に合わせてビルドし直した ffmpeg を入れる
./install.sh root@<camera-ip> ../dist/ffmpeg
```

ffmpeg を SD カード (`/mnt/mmcblk0p1/ffmpeg`) に置く運用なら、更新後も `-B` の復元だけで
配信が再開する。復元後に ffmpeg が無い間は supervisor が
`No ffmpeg binary with rtmps support found` を出して待機する。

## netwatch (ネットワーク監視による自動再起動) に注意

2026-09 以降の Thingino には `S52netwatch` が入っており、**デフォルトで有効**。
デフォルトゲートウェイへ 30 秒毎に ping し、**3 回連続で失敗するとカメラを再起動する**。

ICMP に応答しないモバイルルーター / テザリングでは、Wi-Fi が正常でも約 90 秒毎に再起動して
配信が成立しない。supervisor が gateway ping をしないのと同じ理由で、該当環境では止めるか
宛先を変える:

```sh
ping -c 1 $(ip route | awk '/^default/{print $3; exit}')   # これが通らない環境は要対処
jct /etc/thingino.json set netwatch.enabled false           # 無効化
jct /etc/thingino.json set netwatch.target 1.1.1.1          # または応答するホストを監視
service restart netwatch
```

`/etc/thingino.json` は標準で cfg-backup の対象なので、この設定はファーム更新後も残る。
逆に ping が通る環境では、Wi-Fi が固まったときの最終手段として有効なままが望ましい。

## 運用

```sh
service stop youtube-relay      # 配信停止 (supervisor ごと止まる)
service start youtube-relay     # 配信開始
service disable youtube-relay   # boot 時の自動開始を無効化
service enable youtube-relay    # 有効化
```

設定変更 (`/etc/youtube-relay.json` 編集) 後は `service restart youtube-relay`。

## トラブルシュート

### まず状態を見る (この順で)

```sh
/etc/init.d/S93youtube-relay status     # supervisor が生きているか
logread | grep youtube-relay            # supervisor のログ (最重要)
ps | grep ffmpeg | grep -v grep         # ffmpeg が実際に走っているか
```

正常時のログはこの2行:

```text
youtube-relay: Started, watching for config: /mnt/mmcblk0p1/youtube-relay.json /etc/youtube-relay.json
youtube-relay: Starting ffmpeg -> rtmps://.../REDACTED (config: ..., ffmpeg: ...)
```

最終確認は YouTube Studio のプレビュー (映像と音声メーターが動いていること)。

### 症状別

| ログ / 症状 | 原因と対処 |
|---|---|
| `No usable config, standing by` | 設定が見つからない。`mount \| grep mmcblk` で SD がマウントされているか、`ls /mnt/mmcblk0p1/` にファイルがあるか、ファイル名が `youtube-relay.json` か、`jct <path> get stream_key` で読めるか (JSON 構文エラーだと読めない)、`"enabled": false` になっていないかを順に確認 |
| `No ffmpeg binary with rtmps support found, standing by` | ファーム更新で overlay ごと `/usr/bin/ffmpeg` が消えた可能性が高い → `install.sh` で入れ直す。ファイルはあるのにこれが出る場合は、そのバイナリが今のファームで起動できていない (toolchain 世代の不一致。`/usr/bin/ffmpeg -version` を手で実行して確認)。また**再起動で `/tmp/ffmpeg` は消える**。`/usr/bin/ffmpeg` へ常設するか SD に置く。設定の `ffmpeg_bin` が存在しないパスを指している場合も同じ (行を消せば自動探索になる)。ffmpeg を置けば30秒以内に自動で拾う (supervisor 再起動不要) |
| `ffmpeg exited (rc=1) after 0〜2s` を繰り返す | ffmpeg が即死している。RTSP の URL/認証ミス、YouTube 側のキー間違い、DNS/ネットワーク未接続が典型。下記「ffmpeg のエラーを直接見る」で原因を特定 |
| `ffmpeg exited` が数十秒〜数分間隔 | 接続は成立するが切断されている。Wi-Fi 品質、YouTube 側の一時的な切断など。supervisor が自動復帰させるので、頻度が低ければ実害はない |
| status が `not running` | `service enable youtube-relay` で有効化されているか (`ls -la /etc/init.d/S93youtube-relay` で実行ビット確認)、`/run/portal_mode` が無いか (Wi-Fi 未設定モード)。手動起動は `/etc/init.d/S93youtube-relay start` |
| SD を挿してもマウントされない | `logread \| grep automount` を確認。fsck 失敗や非対応フォーマットの可能性。FAT32 でフォーマットし直す |
| `Waiting for network` / `Network down` のまま復帰しない | `ip -4 route show default` が空ならデフォルトルート喪失 (SSH は同一セグメントなので通る点に注意)。`killall -USR1 udhcpc` で DHCP 再取得 → だめなら `service restart network`。リンク断で udhcpc がルートを再設置しないことがあるため、保険として cron に `* * * * * ip -4 route show default \| grep -q . \|\| killall -USR1 udhcpc` を入れておくとよい (`/etc/cron/crontabs/root` に追記) |
| CPU が張り付く (`top` で prudynt が 50% 超) / 配信がカクつく・カメラが固まる | **WebUI で解像度や fps を変えた後は `service restart prudynt`**。2026-09 時点の prudynt は動的な再構成のあと映像パイプライン (libimp の `group_update` スレッド) が高負荷のまま回り続けることがある。再起動すれば 720p10 で prudynt 2% / ffmpeg 5% 程度に戻る。ffmpeg や配信とは無関係 (ffmpeg を止めても下がらない) |
| カメラが約 90 秒毎に再起動する | netwatch がゲートウェイへの ping 失敗で再起動している (`logread \| grep netwatch`)。上記「netwatch」参照 |
| YouTube Studio に何も出ない (ログは Starting ffmpeg) | ストリームキーの間違いが最有力。YouTube 側は間違ったキーでも接続を受けてから切断するため、`ffmpeg exited` の繰り返しになっていないかログを確認 |
| 映像は出るが音が出ない | RTSP に複数の音声トラックが載っている可能性。設定の **`ffmpeg_out_opts`** に `-map 0:v:0 -map 0:a:0` を指定して (`-map` は出力オプションなので `-i` の前に展開される `ffmpeg_opts` に書くと ffmpeg が起動エラーになる)。 AAC トラックを明示する |

### ffmpeg のエラーを直接見る

supervisor は `-loglevel error` で静かに動かすため、原因調査時は手動で1回実行する:

```sh
/etc/init.d/S93youtube-relay stop
/usr/bin/ffmpeg -loglevel info -rtsp_transport tcp \
  -i "$(jct /mnt/mmcblk0p1/youtube-relay.json get rtsp_url)" \
  -c copy -f flv \
  "$(jct /mnt/mmcblk0p1/youtube-relay.json get rtmp_url)/$(jct /mnt/mmcblk0p1/youtube-relay.json get stream_key)"
# (オプションは必ず出力 URL より前に置く。後ろに置くと警告が出て無視されることがある)
# 原因を直したら:
/etc/init.d/S93youtube-relay start
```

`Invalid DTS ... replacing by guess` の警告は prudynt の仕様によるもので**無害** (NOTES.md 参照)。

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
