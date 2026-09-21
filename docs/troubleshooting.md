# トラブルシュート

## まず状態を見る (この順で)

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
- `[rtsp @ ...] Failed reading RTSP data: End of file` は入力側 (prudynt) が切れたもの。WebUI で映像設定を
  変えた・prudynt が再起動した直後に出る。YouTube 側の問題ではなく、次の再試行で復帰する
- ストリームキーと RTSP の認証情報は `REDACTED` に置き換えてから記録する。ただし `ffmpeg_loglevel` に
  `trace` は使わないこと (パケットのダンプまでは伏せられない)
- 1 回の ffmpeg 起動につき最大 60 行 (超えた分は捨て、その旨を 1 行出す)。`ffmpeg_loglevel` を `warning` 以上に
  すると `Invalid DTS` 警告で枠がすぐ埋まる点に注意

最終確認は YouTube Studio のプレビュー (映像と音声メーターが動いていること)。

## 症状別

| ログ / 症状 | 原因と対処 |
|---|---|
| `No usable config, standing by` | 設定が見つからない。`mount \| grep mmcblk` で SD がマウントされているか、`ls /mnt/mmcblk0p1/` にファイルがあるか、ファイル名が `youtube-relay.json` か、`jct <path> get stream_key` で読めるか (JSON 構文エラーだと読めない)、`"enabled": false` になっていないかを順に確認 |
| `No ffmpeg binary with rtmps support found, standing by` | Thingino を入れ直した/更新した後なら `/usr/bin/ffmpeg` ごと消えている → `install.sh` で入れ直す。ファイルはあるのにこれが出る場合は、そのバイナリが今のファームで起動できていない (toolchain 世代の不一致。`/usr/bin/ffmpeg -version` を手で実行して確認)。また**再起動で `/tmp/ffmpeg` は消える**。`/usr/bin/ffmpeg` へ常設するか SD に置く。設定の `ffmpeg_bin` が存在しないパスを指している場合も同じ (行を消せば自動探索になる)。ffmpeg を置けば30秒以内に自動で拾う (supervisor 再起動不要) |
| `ffmpeg exited (rc=...) after 数秒` を繰り返す | ffmpeg が即死している。RTSP の URL/認証ミス、YouTube 側のキー間違い・配信枠の終了、DNS/ネットワーク未接続が典型。直前の `ffmpeg:` 行 (上記。失敗 2 回目の起動が debug で詳しい) で原因を特定。`rc=251` は I/O エラー (-5) で、YouTube に publish を拒否されたときもこれ |
| `ffmpeg exited` が数十秒〜数分間隔 | 接続は成立するが切断されている。Wi-Fi 品質、YouTube 側の一時的な切断など。supervisor が自動復帰させるので、頻度が低ければ実害はない |
| status が `not running` | `service enable youtube-relay` で有効化されているか (`ls -la /etc/init.d/S93youtube-relay` で実行ビット確認)、`/run/portal_mode` が無いか (Wi-Fi 未設定モード)。手動起動は `/etc/init.d/S93youtube-relay start` |
| SD を挿してもマウントされない | `logread \| grep automount` を確認。fsck 失敗や非対応フォーマットの可能性。FAT32 でフォーマットし直す |
| `Waiting for network` / `Network down` のまま復帰しない | `ip -4 route show default` が空ならデフォルトルート喪失 (SSH は同一セグメントなので通る点に注意)。`killall -USR1 udhcpc` で DHCP 再取得 → だめなら `service restart network`。リンク断で udhcpc がルートを再設置しないことがあるため、保険として cron に `* * * * * ip -4 route show default \| grep -q . \|\| killall -USR1 udhcpc` を入れておくとよい (`/etc/cron/crontabs/root` に追記) |
| CPU が張り付く (`top` で prudynt が 50% 超) / 配信がカクつく・カメラが固まる | **WebUI で解像度や fps を変えた後は `service restart prudynt`**。2026-09 時点の prudynt は動的な再構成のあと映像パイプライン (libimp の `group_update` スレッド) が高負荷のまま回り続けることがある。再起動すれば 720p10 で prudynt 2% / ffmpeg 5% 程度に戻る。ffmpeg や配信とは無関係 (ffmpeg を止めても下がらない) |
| カメラが勝手に再起動する (約 90 秒毎、または Wi-Fi 断のたび) | netwatch がゲートウェイへの ping 失敗で OS を再起動している。`device/disable-netwatch.sh` で無効化する ([netwatch.md](netwatch.md) 参照)。Thingino を更新した後に再発したら設定が初期値に戻っている |
| OSD のテキストが出ない (再起動や WebUI での設定変更の後から) | `prudyntctl json` で変えた設定は prudynt の再起動で消える。`prudyntctl json '{"osd":{"textfile":null}}'` で `enabled` が false に戻っていないか見る。再起動しても残したい時は SD カードに `prudynt-osd.json` を置く ([osd.md](osd.md)) |
| OSD のテキストが出ない / 指定より小さい字で出る | `logread \| grep textfile` に `reduced to ...` や `not shown` が出ていれば OSD プールの不足。桁数・行数・scale を下げるか `general.osd_pool_size` を上げる ([osd.md](osd.md) の「大きさの上限と OSD プール」)。`prudyntctl json '{"osd":{"textfile":null}}'` が `{}` を返すなら、動いているのはパッチなしの prudynt (`/etc/init.d/S30prudynt-osd status`) |
| SD の `prudynt-osd.json` が反映されない | `logread \| grep osd-config`。`no usable "osd" object` は JSON の構文エラーか `osd` キーが無い。`does not accept` はキーの綴り間違いか、パッチなしの prudynt。何も出ていなければ `service start osd-config` |
| YouTube Studio に何も出ない (ログは Starting ffmpeg) | ストリームキーの間違いが最有力。YouTube 側は間違ったキーでも接続を受けてから切断するため、`ffmpeg exited` の繰り返しになっていないかログを確認 |
| 映像は出るが音が出ない | RTSP に複数の音声トラックが載っている可能性。設定の **`ffmpeg_out_opts`** に `-map 0:v:0 -map 0:a:0` を指定して (`-map` は出力オプションなので `-i` の前に展開される `ffmpeg_opts` に書くと ffmpeg が起動エラーになる)。 AAC トラックを明示する |

## ffmpeg のエラーを直接見る

supervisor のログ (`ffmpeg:` 行) で足りない場合は、手動で1回実行する:

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

`Invalid DTS ... replacing by guess` の警告は prudynt の仕様によるもので**無害** ([NOTES.md](../NOTES.md) 参照)。

