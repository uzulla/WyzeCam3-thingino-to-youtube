# トラブルシュート

配信 (youtube-relay) を中心に、症状別の見方と対処。コマンドは ssh でカメラに入って実行する。
カメラの IP アドレスが分からない (Web UI / SSH に入れない) 時は [find-camera.md](find-camera.md)、
Wi-Fi に繋がらない時は本体ボタン 5 秒長押しで AP モード ([lapcam.md](lapcam.md)) か SD の `wpa_supplicant.conf`
([wifi-from-sd.md](wifi-from-sd.md))。カードから焼いた直後は root のパスワードが Thingino 既定の `root` に戻っている
([sdcard.md](sdcard.md))。

## まず状態を見る (この順で)

lapcam の Camera タブ (LIVE と経過時間、Stream 欄の ffmpeg 再起動回数) と Stream タブの Log で大体分かる。ssh なら:

```sh
/etc/init.d/S93youtube-relay status     # supervisor が生きているか
logread | grep youtube-relay            # supervisor のログ (最重要)
ps | grep ffmpeg | grep -v grep         # ffmpeg が実際に走っているか
```

正常時のログはこの 2 行:

```text
youtube-relay: Started, watching for config: /mnt/mmcblk0p1/youtube-relay.json /etc/youtube-relay.json
youtube-relay: Starting ffmpeg -> rtmps://.../REDACTED (config: ..., ffmpeg: ...)
```

ffmpeg 自身の出力 (stderr) も `youtube-relay: ffmpeg: ...` として同じログに出る。通常は
`-loglevel error` なのでエラー時だけ。**起動から 60 秒もたずに落ちた場合は、次の 1 回だけ
`-loglevel debug` で起動し直し、接続まわりの行 (`[tcp]` / `[tls]` / `[rtmps]` とエラー行) だけを
記録する** (失敗が続いても 1 回だけ。60 秒以上配信できたらリセット)。どこまで進んで切られたかが分かる:

```text
youtube-relay: ffmpeg exited (rc=251) after 6s, restarting in 4s
youtube-relay: Starting ffmpeg -> rtmps://.../REDACTED (config: ..., ffmpeg: ..., loglevel debug to diagnose the failure)
youtube-relay: ffmpeg: [tcp @ ...] Successfully connected to 2404:6800:... port 443
youtube-relay: ffmpeg: [rtmps @ ...] Handshaking...
youtube-relay: ffmpeg: [rtmps @ ...] Server version 4.0.0.1
youtube-relay: ffmpeg: [rtmps @ ...] Sending publish command for 'REDACTED'
youtube-relay: ffmpeg: [out#0/flv @ ...] Error opening output rtmps://.../REDACTED: Input/output error
```

- 上の例 (`Sending publish command` の直後に `Input/output error`) は、**TLS も RTMP も通っていて
  YouTube が publish を拒否している**状態。キー違い、配信枠が終了済み、同じキーで別のエンコーダが
  配信中、など YouTube 側を確認する。`Successfully connected ... port 443` まで行かなければ
  ネットワーク/DNS、`Handshaking...` の前後で止まれば TLS の問題
- `[rtsp @ ...] Failed reading RTSP data: End of file` は入力側 (prudynt) が切れたもの。画質や反転を変えた・
  prudynt が再起動した直後に出る。YouTube 側の問題ではなく、次の再試行で復帰する
- ストリームキーと RTSP の認証情報は `REDACTED` に置き換えてから記録する。ただし `ffmpeg_loglevel` に
  `trace` は使わないこと (パケットのダンプまでは伏せられない)
- 1 回の ffmpeg 起動につき最大 60 行 (超えた分は捨て、その旨を 1 行出す)。`ffmpeg_loglevel` を `warning` 以上に
  すると `Invalid DTS` 警告で枠がすぐ埋まる点に注意

最終確認は YouTube Studio のプレビュー (映像と音声メーターが動いていること)。

## 症状別

| ログ / 症状 | 原因と対処 |
|---|---|
| `No usable config, standing by` | 設定が見つからない。lapcam の Stream タブでキーが保存されているか。ssh なら `mount \| grep mmcblk` で SD がマウントされているか、`ls /mnt/mmcblk0p1/` にファイルがあるか、ファイル名が `youtube-relay.json` か、`jct <path> get stream_key` で読めるか (JSON 構文エラーだと読めない)、`"enabled": false` になっていないかを順に確認 |
| `No ffmpeg binary with rtmps support found, standing by` | SD カードから入れた個体では ffmpeg はカード上 (`/mnt/mmcblk0p1/ffmpeg`)。カードが抜けている・マウントされていない・`make-sdcard.sh` を通していないカードだとこれが出る。ファイルはあるのに出る場合は、そのバイナリが今のファームで起動できていない (toolchain 世代の不一致。`/mnt/mmcblk0p1/ffmpeg -version` で起動できるか、`-protocols` の Output に `rtmps` があるかを手で確認。カードとファームの版は lapcam の Camera タブ下部の表示と `VERSION.txt`)。手で入れた個体で Thingino を入れ直した後なら `/usr/bin/ffmpeg` ごと消えている → `device/install.sh` で入れ直す。`/tmp/ffmpeg` は再起動で消える。設定の `ffmpeg_bin` が存在しないパスを指している場合も同じ (行を消せば自動探索になる)。ffmpeg を置けば 30 秒以内に自動で拾う (supervisor 再起動不要) |
| `ffmpeg exited (rc=...) after 数秒` を繰り返す | ffmpeg が即死している。RTSP の URL/認証ミス、YouTube 側のキー間違い・配信枠の終了、DNS/ネットワーク未接続が典型。直前の `ffmpeg:` 行 (上記。失敗 2 回目の起動が debug で詳しい) で原因を特定。`rc=251` は I/O エラー (-5) で、YouTube に publish を拒否されたときもこれ |
| `ffmpeg exited` が数十秒〜数分間隔 | 接続は成立するが切断されている。Wi-Fi 品質、YouTube 側の一時的な切断など。supervisor が自動復帰させるので、頻度が低ければ実害はない |
| status が `not running` | lapcam の Stream タブ「起動時に配信を始める」= `service enable youtube-relay` が有効か (`ls -la /etc/init.d/S93youtube-relay` で実行ビット確認)、`/run/portal_mode` が無いか (Wi-Fi 未設定 / AP モード)。手動起動は `/etc/init.d/S93youtube-relay start` |
| SD を挿してもマウントされない | `logread \| grep automount` を確認。fsck 失敗や非対応フォーマットの可能性。PC で読めるなら先に録画と設定ファイルを退避してから FAT32 でフォーマットし直す (運用だけなら exFAT でも動くが、焼き直しには FAT32 が要る。[sdcard.md](sdcard.md)) |
| `Waiting for network` / `Network down` のまま復帰しない | `ip -4 route show default` が空ならデフォルトルート喪失 (SSH は同一セグメントなので通る点に注意)。`killall -USR1 udhcpc` で DHCP 再取得 → だめなら `service restart network`。リンク断で udhcpc がルートを再設置しないことがあるため、保険として cron に `* * * * * ip -4 route show default \| grep -q . \|\| killall -USR1 udhcpc` を入れておくとよい (`/etc/cron/crontabs/root` に追記) |
| CPU が張り付く (`top` で prudynt が 50% 超) / 配信がカクつく・カメラが固まる | 解像度や fps を変えた後なら prudynt を再起動する (lapcam の Stream タブの Apply は止めて起こし直すので起きない。Thingino の UI や `prudyntctl` で変えた時に起きる)。このファーム (`ciao+da40db6`) の prudynt は動的な再構成のあと映像パイプライン (libimp の `group_update` スレッド) が高負荷のまま回り続けることがある。再起動すれば 720p10 で prudynt 2% / ffmpeg 5% 程度に戻る。ffmpeg や配信とは無関係 (ffmpeg を止めても下がらない)。Thingino 標準の `service restart prudynt` は旧プロセスが終わる前に新プロセスを起こし、新しい方が「Another Prudynt instance appears to be running」で終了して **prudynt が居なくなる**ことがある (実機で発生)。`/etc/init.d/S31prudynt stop` → 消えるのを待つ → `start` の順で |
| カメラが勝手に再起動する (約 90 秒毎、または Wi-Fi 断のたび) | netwatch がゲートウェイへの ping 失敗で OS を再起動している。ファームウェアでは無効にしてある (`jct /etc/thingino.json get netwatch.enabled` が `false`)。手で入れた個体なら `device/disable-netwatch.sh` で無効化する ([netwatch.md](netwatch.md))。Thingino を入れ直した後に再発したら設定が初期値に戻っている |
| OSD のテキストが出ない (再起動や設定変更の後から) | `prudyntctl json` で直接変えた設定は prudynt の再起動で消える。残るのは SD の `prudynt-osd.json` に書いたもの (lapcam の OSD タブはそこに書き、osd-config が起動時に送り直す)。`prudyntctl json '{"osd":{"textfile":null}}'` で `enabled` が false に戻っていないか見る ([osd.md](osd.md)) |
| SD カードの `prudynt-osd.json` を置いた/変えたら配信が数秒切れた | `general.osd_pool_size` の値が `/etc/prudynt.json` と違うと、`osd-config` がそれを書いて prudynt を再起動する (`logread \| grep osd-config` に `restarting prudynt`)。値が同じなら起きない (ファームウェアの既定は 2048。[osd.md](osd.md)) |
| OSD のテキストが出ない / 指定より小さい字で出る | `logread \| grep textfile` に `reduced to ...` や `not shown` が出ていれば OSD プールの不足。桁数・行数・scale を下げるか `general.osd_pool_size` を上げる ([osd.md](osd.md) の「大きさの上限と OSD プール」)。`prudyntctl json '{"osd":{"textfile":null}}'` が `{}` を返すなら、動いているのはパッチなしの prudynt (ファームウェアの prudynt はパッチ済み。手で入れた個体なら `/etc/init.d/S30prudynt-osd status`) |
| SD の `prudynt-osd.json` が反映されない | `logread \| grep osd-config`。`no usable "osd" object` は JSON の構文エラーか `osd` キーが無い。`does not accept` はキーの綴り間違いか、パッチなしの prudynt。何も出ていなければ `service start osd-config` |
| YouTube Studio に何も出ない (ログは Starting ffmpeg) | ストリームキーの間違いが最有力。YouTube 側は間違ったキーでも接続を受けてから切断するため、`ffmpeg exited` の繰り返しになっていないかログを確認 |
| 映像は出るが音が出ない | RTSP に複数の音声トラックが載っている可能性。設定の **`ffmpeg_out_opts`** に `-map 0:v:0 -map 0:a:0` を指定して AAC トラックを明示する (`-map` は出力オプションなので `-i` の前に展開される `ffmpeg_opts` に書くと ffmpeg が起動エラーになる) |
| lapcam が開けない (ポート 80 が Thingino の UI) | SD が無い・マウントされていないと lapcam は起動せず uhttpd が動く。カードの `lapcam` と `lapcam.args` を確認 ([lapcam.md](lapcam.md))。版の不一致 (Camera タブ下部が赤) はカードかファームウェアを更新する ([release.md](release.md)) |

## ffmpeg のエラーを直接見る

supervisor のログ (`ffmpeg:` 行) で足りない場合は、手動で 1 回実行する:

```sh
# supervisor が実際に使っている設定ファイルと ffmpeg を、直近の起動ログで確認する
#   youtube-relay: Starting ffmpeg -> ... (config: /mnt/mmcblk0p1/youtube-relay.json, ffmpeg: /mnt/mmcblk0p1/ffmpeg)
logread | grep "youtube-relay: Starting ffmpeg" | tail -1
CFG=/mnt/mmcblk0p1/youtube-relay.json   # ↑ の config: の値 (/etc/youtube-relay.json のこともある)
FFMPEG=/mnt/mmcblk0p1/ffmpeg            # ↑ の ffmpeg: の値 (/usr/bin/ffmpeg や /tmp/ffmpeg のこともある)

/etc/init.d/S93youtube-relay stop
# 設定に書いていない項目は supervisor と同じ既定値を使う
RTSP_URL=$(jct "$CFG" get rtsp_url 2>/dev/null); [ -n "$RTSP_URL" ] || RTSP_URL='rtsp://thingino:thingino@127.0.0.1:554/ch0'
RTMP_URL=$(jct "$CFG" get rtmp_url 2>/dev/null); [ -n "$RTMP_URL" ] || RTMP_URL='rtmps://a.rtmps.youtube.com:443/live2'
# ffmpeg_opts (-i の前) と ffmpeg_out_opts (-i の後) も supervisor と同じ位置に展開する (未設定なら空)。
# -loglevel だけは設定の ffmpeg_loglevel ではなく info に固定している (診断のために出力を増やすのが目的)
FFMPEG_OPTS=$(jct "$CFG" get ffmpeg_opts 2>/dev/null)
FFMPEG_OUT_OPTS=$(jct "$CFG" get ffmpeg_out_opts 2>/dev/null)
set -f   # supervisor と同じ: オプション中の * (例: -map 0:*) をファイル名に展開させない
"$FFMPEG" -loglevel info -rtsp_transport tcp $FFMPEG_OPTS \
  -i "$RTSP_URL" \
  -c copy $FFMPEG_OUT_OPTS -f flv \
  "$RTMP_URL/$(jct "$CFG" get stream_key)"
set +f
# (オプションは必ず出力 URL より前に置く。後ろに置くと警告が出て無視されることがある)
# 原因を直したら:
/etc/init.d/S93youtube-relay start
```

`Invalid DTS ... replacing by guess` の警告は prudynt の仕様によるもので**無害** ([NOTES.md](../NOTES.md) 参照)。
