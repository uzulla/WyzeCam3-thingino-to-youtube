# SD カードへの録画

配信しているのと同じ映像を SD カードに MP4 で保存する。Thingino の prudynt に入っている録画機能
(MP4 レコーダー) を使うので、このリポジトリの配信側 (`youtube-relay` / ffmpeg) には手を入れない。
lapcam のファームウェアには、録画に必要なもの (Thingino の出荷ファームで壊れていた agent adapter の修正、本体ボタンの
ダブルクリックで開始・停止する `rec-button`、録画中に正面の赤 LED を点ける `rec-led`、保存先 SD の `recordings/`) が
入っていて、操作は lapcam の Camera タブ (Record / Stop recording) と Recordings タブ (一覧・再生・ダウンロード・削除・設定)
([lapcam.md](lapcam.md))。この文書は仕組みと実測、PC で分割ファイルを 1 本につなぐ `tools/concat-recordings.sh`。

## 仕組み

- prudynt は RTSP に出すのと同じエンコード済みのフレーム (メインストリーム ch0 の H.264 と AAC) を
  そのまま MP4 に書く。OSD も含めて配信と同一の映像になる。再エンコードしないので CPU の上乗せは
  数 % (実測: prudynt が 6% → 8〜10%)
- 1 ファイルの長さ (既定 60 秒) ごとにファイルを切り替える。切り替えで映像は欠けない (実測で
  タイムスタンプが連続)。各ファイルは fragmented MP4 なので、途中で電源が落ちても書けたところまでは
  再生できる
- 保存先は SD の `recordings/` (`/etc/prudynt.json` の `recorder.device_path`。ファームウェアで固定) の下に
  `%Y/%m/%d/%H-%M-%S.mp4` (日付のフォルダの下に時刻のファイル名)。SD は FAT32 だが 1 ファイルは 1 分 × 約 8 MB なので
  4 GB 制限は関係ない
- 録画中は `/run/prudynt/mp4ctl-ch0.active` ができる (中身は今書いているファイル名)。`rec-led` と lapcam の REC 表示は
  これを見ている
- 録画中に SD カードを抜かないこと (osd-feed と同じ)

| 設定 (720p 10 fps CBR 1 Mbps + AAC 16 kHz) | 実測 |
|---|---|
| 1 分 | 約 8 MB |
| 1 時間 | 約 465 MB |
| 6 時間 | 約 2.8 GB |

## 録画を始める

どの方法でも、保存先と 1 ファイルの長さは `/etc/prudynt.json` の `recorder.*` (Recordings タブの設定) が使われる。

1. **lapcam の Camera タブの Record / Stop recording** (停止は 2 回タップ)。録画中はプレビューの下に REC と経過時間
2. **起動時に自動で始める**: Recordings タブの設定「起動時に録画を始める」(`recorder.autostart`)。保存しただけでは始まらないので、
   すぐ始めたい時は Record を押す
3. **本体のボタンをダブルクリック** (下記 `rec-button`)。もう一度で停止
4. **コマンド** (設定ファイルには何も書かず、その場限り。再起動で止まる):

```sh
record -c 0 start            # lapcam / ボタンと同じ: recorder.* の設定で SD の recordings/ へ連続録画
record -t 60 -s rec          # 60 秒ごとに SD の rec/ へ連続録画 (保存先と長さを指定する例)
record -x -t 10              # 10 秒 1 本だけ
record stop                  # 停止
cat /run/prudynt/mp4ctl-ch0.active   # 録画中なら今のファイル名が出る
```

Recordings タブの設定 (Thingino の Video Recorder ページと同じ `/etc/prudynt.json` の `recorder.*` に書く。フラッシュに保存される):

| 項目 | 内容 |
|---|---|
| 起動時に録画を始める | `recorder.autostart` |
| 1 ファイルの長さ | 秒。既定 60。長くしても継ぎ目で欠けないので、ファイル数を減らしたければ 600 など |
| カードがいっぱいになったら古いものから消す | Thingino の `recordmgr` (`S95recordmgr`)。上限 (GB) と最低空き容量 (MB) で決める。変えると lapcam が `recordmgr` を再起動する |

## lapcam の Record ボタンが頼る agent adapter

Record ボタン (旧 Thingino UI の Rec ボタンも) は prudynt に直接ではなく、Thingino の agent API (`/api/v1/actions/record`) に
録画を頼む。agent はその要求を `/usr/libexec/agent/adapter.sh` (ストリーマごとのバックエンド) に渡すが、Thingino のビルドは
prudynt 用の adapter を入れた後に thingino-agent の「null」版で上書きしてしまうため、出荷ファーム `ciao+da40db6` では
"record action is not supported without an active backend adapter" (旧 UI では "Failed to start recording on channel 0") になる。
lapcam のファームウェアは prudynt 版 (`device/agent-adapter`) を overlay に置いて直している。直したのはもう 1 点、
`record start -c 0` の引数順 (この版の `record` はオプションを先に書かないと usage で終わる。`patches/thingino-agent-adapter-record-args.diff`)。

- 「開始」は agent が `record -c 0 start` を呼ぶので、`recorder.*` の設定で連続録画になる。「停止」は全チャンネル停止
- 録画中の判定は prudynt の `.active` (Thingino の heartbeat `/run/thingino/heartbeat.json` の `rec_ch0` も同じ) なので、
  コマンドやボタン、自動開始で始めた録画も lapcam に出て、どこからでも止められる
- 副作用: motion / daynight / privacy の切り替えなども同じ agent 経由なので、null 版で失敗していたのが動く (ファーム本来の状態)。
  prudynt や配信には触れない

## 本体のボタンのダブルクリックで開始・停止 (`rec-button`)

底面のボタンを**素早く 2 回押す**と録画が始まり、もう一度 2 回押すと止まる。開始・停止のたびに
Thingino に入っている英語の音声 ("video recording started" / "stopped") がスピーカーから出る
(`/usr/sbin/rec-button` 先頭の `SOUND=0` で止められる。配信の音声にも入るので注意)。

- Thingino のボタンデーモン (`thingino-button`、設定 `/etc/thingino-button.conf`) は押した・離した・
  長押しにコマンドを割り当てられるだけでダブルクリックが無いので、押す・離すのたびに `/usr/sbin/rec-button`
  を呼び、`/run/rec-button/` の時刻で判定する。離してから 0.7 秒以内にもう一度離したらダブルクリック、
  1 秒以上の長押しは無視 (5 秒の長押しは「カメラが AP になる」: [lapcam.md](lapcam.md)。20 秒は Thingino の初期化)
- Thingino 既定の「短く押すと IP を読み上げる」(`tell %ip`) は設定ファイル側ではコメントアウトし、
  `rec-button` がシングルクリックとして 0.7 秒待ってから実行する (ダブルクリックで 2 回読み上げないため)。
  短く押すたびに鳴るチャイムは Thingino のまま
- 開始は Record ボタンと同じ `record -c 0 start`、停止は `record stop`。録画中かどうかは prudynt の `.active` で見るので、
  lapcam やコマンドで始めた録画もボタンで止められる (逆も同じ)
- ログ: `ssh root@<camera-ip> 'logread | grep rec-button'`

## 録画中に赤 LED を点ける (`rec-led`)

正面の赤 LED を録画中だけ点ける常駐スクリプト (`S96rec-led`)。Record ボタン・コマンド・起動時の自動開始のどれで
始めても反応する。1 秒ごとに `/run/prudynt/mp4ctl-ch*.active` の有無を見て、赤のチャンネル
(`/sys/class/leds/led_r`) だけを点け消しする。青には触らない。サービスを止めると消灯する
(`service stop rec-led`)。ログは `logread | grep rec-led` (recording on / off)。LED そのものの操作は [front-led.md](front-led.md)。

## PC に取り出す

lapcam の Recordings タブで 1 本ずつ (Save)、または選んで 1 つの zip でダウンロードできる。ssh で取るなら、Thingino には
sftp-server が無いので scp は `-O` (旧プロトコル) を付ける:

```sh
scp -O root@<camera-ip>:/mnt/mmcblk0p1/recordings/2026/11/09/14-42-31.mp4 ~/Downloads/   # 1 本
scp -O -r root@<camera-ip>:/mnt/mmcblk0p1/recordings ~/Downloads/                        # フォルダごと
```

録画中にコピーすると、書き込み中の最後の 1 本だけ途中までの内容になる。

## 1 本につなぐ (`tools/concat-recordings.sh`)

```sh
tools/concat-recordings.sh ~/Downloads/recordings ~/Downloads/rec-all.mp4   # PC に ffmpeg が必要 (brew install ffmpeg)
```

再エンコードはしない。`ffmpeg -f concat -c copy` をそのまま使ってはいけない理由:

- prudynt の録画ファイルは、映像と音声のタイムスタンプの基準がファイルごとにそろっていない
  (62 本中 47 本で、例: 映像が 179.8 秒から、音声が 0 秒から始まる)。そのままつなぐと長さが
  4〜5 倍の壊れたファイルになる。スクリプトは映像と音声を別々に 0 秒始まりにそろえてから、
  それぞれをつなぎ、最後に合わせる
- 映像のタイムスタンプは 10 fps ちょうどで打たれているが、実際のフレームは毎分 595〜601 枚なので、
  映像の時計は音声より 1 時間で数秒〜10 秒遅れる。スクリプトは音声の長さを基準に映像を並べるので、
  つないだ後も同期は保たれる (代わりに、短かった分のファイルの境目で映像が 0.5 秒ほど止まる)。
  遅れの正体は prudynt のタイムスタンプ処理で、センサーが落としたフレーム (1 分に 1 枚ほど) の分だけ
  時計が遅れていき、配信側の ffmpeg が RTSP に接続し直した時にまとめて先に飛ぶ (下記)。フレームは
  失われていない
- 1 本ずつ単体で再生する分には、1 分で 0.17 秒のずれなので気にならない。ただし上の「基準がそろって
  いない」ファイルは、プレイヤーによっては映像が遅れて出る可能性がある

## 素の Thingino に手で入れる場合

自前のファームウェアを焼かずに、素の Thingino (`ciao+da40db6`) に入れる時だけ。ファームウェアには全部入っている。

```sh
device/install-agent-adapter.sh root@<camera-ip>   # adapter.sh を prudynt 版 (引数順の修正入り) に。元は adapter.sh.null に残る
device/install-rec-button.sh root@<camera-ip>      # /usr/sbin/rec-button と thingino-button.conf の書き換え (元は .orig)。ボタンデーモンを再起動
device/install-rec-led.sh root@<camera-ip>         # /usr/sbin/rec-led と /etc/init.d/S96rec-led
```

戻す: `mv /usr/libexec/agent/adapter.sh.null /usr/libexec/agent/adapter.sh`、
`mv /etc/thingino-button.conf.orig /etc/thingino-button.conf; rm /usr/sbin/rec-button; /etc/init.d/S15thingino-button restart`、
`/etc/init.d/S96rec-led stop; rm /etc/init.d/S96rec-led /usr/sbin/rec-led`。保存先は Thingino の既定 `%hostname` (ホスト名の
フォルダ) のままなので、`recordings` に固定するなら `device/set-camera-name.sh` ([lapcam.md](lapcam.md))。

## 確認した範囲 (2026-11-09、`ciao+da40db6`、OSD パッチ入り prudynt)

- 配信中に 60 秒 × 62 本 (61.8 分) を録画: 479 MB、prudynt の CPU +2〜4%、メモリ増なし、
  ファイル境界のフレーム欠落なし、OSD 入りの映像と音声を確認
- 録画中に 1 度だけ映像のタイムスタンプが 2.1 秒先に飛んだ (13:52:21、ファイル `13-51-26.mp4`)。
  **フレームは欠けていない** (その 1 分のフレーム数は 599 枚で他の分と同じ)。同じ時刻に配信側の
  ffmpeg が終了コード 0 で再起動しており、prudynt は RTSP クライアントが 0 → 1 になった時に映像の
  時計を取り直す (`VideoWorker.cpp` の subscriber startup) ので、それまでに溜まった時計の遅れが
  一気に戻った形。再生では、その箇所で映像が 2.1 秒止まって見え、その分以降の映像が音声より
  2.1 秒遅れる (次のファイルからは元に戻る)。詳しくは [NOTES.md](../NOTES.md) の 2026-11 追記。
  ffmpeg が終了した側の原因はログが流れて不明 (4.5 時間で 1 回)
- `rec-led`: 録画の開始 1 秒以内に `brightness` が 255、終了 1 秒以内に 0。サービス停止で消灯
- Rec ボタン: 素のファームでは失敗を再現 (agent の応答は上記)。adapter 差し替え後、ボタンと同じ
  agent API への start / stop で `.active` の生成・削除、heartbeat の `rec_ch0` が 1 / 0、LED の点灯・
  消灯を確認。ブラウザのボタンそのものはユーザーが確認 (LED 点灯)
- `rec-button`: 押す・離すを ssh から模擬して、0.25 秒間隔の 2 クリックで切り替え、1 クリックで
  `tell %ip` 相当、1.2 秒の長押しは無視、1 秒空けた 2 クリックは 2 回のシングル、になるのを確認。
  実際のボタンでの確認はこれから
