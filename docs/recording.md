# SD カードへの録画

配信しているのと同じ映像を SD カードに MP4 で保存する。Thingino の prudynt に入っている録画機能
(MP4 レコーダー) を使うので、このリポジトリの配信側 (`youtube-relay` / ffmpeg) には手を入れない。
このリポジトリが足すのは、Web UI の Rec ボタンを動くようにする `install-agent-adapter.sh`、本体のボタンの
ダブルクリックで開始・停止する `rec-button`、録画中に正面の赤 LED を点ける `rec-led`、PC で分割ファイルを
1 本につなぐ `tools/concat-recordings.sh`。

## 仕組み

- prudynt は RTSP に出すのと同じエンコード済みのフレーム (メインストリーム ch0 の H.264 と AAC) を
  そのまま MP4 に書く。OSD も含めて配信と同一の映像になる。再エンコードしないので CPU の上乗せは
  数 % (実測: prudynt が 6% → 8〜10%)
- 1 ファイルの長さ (既定 60 秒) ごとにファイルを切り替える。切り替えで映像は欠けない (実測で
  タイムスタンプが連続)。各ファイルは fragmented MP4 なので、途中で電源が落ちても書けたところまでは
  再生できる
- SD は exFAT で、1 ファイルは 1 分 × 約 8 MB なので、FAT32 の 4 GB 制限のようなものはない
- 録画中は `/run/prudynt/mp4ctl-ch0.active` ができる (中身は今書いているファイル名)。`rec-led` は
  これを見ている

| 設定 (720p 10 fps CBR 1 Mbps + AAC 16 kHz) | 実測 |
|---|---|
| 1 分 | 約 8 MB |
| 1 時間 | 約 465 MB |
| 6 時間 | 約 2.8 GB |

## 録画を始める

どの方法でも、保存先と 1 ファイルの長さは「Video Recorder」ページの設定が使われる。

1. **Web UI の上のバーの「Rec ch0」ボタン**: 押すと始まり、もう一度押すと止まる (録画中はボタンが
   点く)。「Rec ch1」はサブストリーム用なので使わない。**そのままのファームでは "Failed to start
   recording on channel 0" になる**ので、先に下の `install-agent-adapter.sh` を 1 回入れる
2. **起動時に自動で始める**: Web UI の Tools → **Video Recorder** (`/tool-record.html`) で
   「Start recording automatically on boot」をオンにして保存。設定は `/etc/prudynt.json` の
   `recorder.*` に入る (フラッシュに保存される)。保存しただけでは始まらないので、すぐ始めたい時は
   Rec ch0 を押す
3. **本体のボタンをダブルクリック** (下の `install-rec-button.sh` を入れてから)。もう一度で停止
4. **コマンド** (設定ファイルには何も書かず、その場限り。再起動で止まる):

```sh
record -t 60 -s rec          # 60 秒ごとに SD の rec/ へ連続録画 (-s を省くとホスト名のフォルダ)
record -x -t 10              # 10 秒 1 本だけ
record stop                  # 停止
cat /run/prudynt/mp4ctl-ch0.active   # 録画中なら今のファイル名が出る
```

Video Recorder ページの主な項目:

| 項目 | 内容 |
|---|---|
| Storage mount | `/mnt/mmcblk0p1` (SD カード) |
| Device-specific path | SD の中のフォルダ。既定 `%hostname` (ホスト名のフォルダ) |
| File name template | 既定 `%Y/%m/%d/%H-%M-%S`。日付のフォルダの下に時刻のファイル名 |
| Clip duration | 1 ファイルの秒数。既定 60。長くしても継ぎ目で欠けないので、ファイル数を減らしたければ 600 など |
| Storage manager | オンにすると古いファイルから消して SD を回し続ける。上限 (GB) と最低空き容量 (MB) で決める |

録画中に SD カードを抜かないこと (osd-feed と同じ)。

## Web UI の Rec ボタンを直す (`install-agent-adapter.sh`)

```sh
device/install-agent-adapter.sh root@<camera-ip>   # 1 回だけ。入れた直後からボタンが効く (ページは再読み込み)
```

Rec ボタンは prudynt に直接ではなく、Thingino の agent API (`/api/v1/actions/record`) に録画を頼む。
agent はその要求を `/usr/libexec/agent/adapter.sh` (ストリーマごとのバックエンド) に渡すが、
`ciao+da40db6` のファームには何もしない null 版が入っていて、"record action is not supported
without an active backend adapter" を返す (prudynt 版はソース木にあるのにイメージに入っていない)。
このスクリプトは同じソース木の prudynt 版 (`package/prudynt-t/files/agent-adapter`) を 1 行だけ直して
置く。直したのは `record start -c 0` の引数順 (この版の `record` はオプションを先に書かないと usage で
終わる。`patches/thingino-agent-adapter-record-args.diff`)。元のファイルは `adapter.sh.null` に残る。

- 「開始」は agent が `record -c 0 start` を呼ぶので、Video Recorder ページの設定 (保存先・1 ファイルの
  長さ) で連続録画になる。「停止」は全チャンネル停止
- ボタンの点灯は heartbeat (`/run/thingino/heartbeat.json` の `rec_ch0`) で、prudynt の `.active` を
  見ているので、コマンドや自動開始で始めた録画でも点く
- 副作用: Web UI の motion / daynight / privacy の切り替えや設定の一部も同じ agent 経由なので、
  これらも null 版で失敗していたのが動くようになる (ファーム本来の状態)。prudynt や配信には触れない
- 戻すには `ssh root@<camera-ip> 'mv /usr/libexec/agent/adapter.sh.null /usr/libexec/agent/adapter.sh'`

## 本体のボタンのダブルクリックで開始・停止 (`rec-button`)

```sh
device/install-rec-button.sh root@<camera-ip>   # 1 回だけ。ボタンデーモンを再起動する (配信には影響しない)
```

底面のボタンを**素早く 2 回押す**と録画が始まり、もう一度 2 回押すと止まる。開始・停止のたびに
Thingino に入っている英語の音声 ("video recording started" / "stopped") がスピーカーから出る
(`rec-button` 先頭の `SOUND=0` で止められる。配信の音声にも入るので注意)。

- Thingino のボタンデーモン (`thingino-button`、設定 `/etc/thingino-button.conf`) は押した・離した・
  長押しにコマンドを割り当てられるだけでダブルクリックが無いので、押す・離すのたびに `/usr/sbin/rec-button`
  を呼び、`/run/rec-button/` の時刻で判定する。離してから 0.7 秒以内にもう一度離したらダブルクリック、
  1 秒以上の長押しは無視 (5 秒 = 再起動、20 秒 = 初期化、という Thingino の長押しはそのまま)
- Thingino 既定の「短く押すと IP を読み上げる」(`tell %ip`) は設定ファイル側ではコメントアウトし、
  `rec-button` がシングルクリックとして 0.7 秒待ってから実行する (ダブルクリックで 2 回読み上げないため)。
  短く押すたびに鳴るチャイムは Thingino のまま
- 開始は Web UI の Rec ボタンと同じ `record -c 0 start` (Video Recorder ページの設定で連続録画)、
  停止は `record stop`。録画中かどうかは prudynt の `.active` で見るので、Web UI やコマンドで始めた録画も
  ボタンで止められる (逆も同じ)
- 設定ファイルの元は `/etc/thingino-button.conf.orig` に残る。戻すには
  `ssh root@<camera-ip> 'mv /etc/thingino-button.conf.orig /etc/thingino-button.conf; rm /usr/sbin/rec-button; /etc/init.d/S15thingino-button restart'`
- ログ: `ssh root@<camera-ip> 'logread | grep rec-button'`

## 録画中に赤 LED を点ける (`rec-led`)

正面の赤 LED を録画中だけ点ける常駐スクリプト。Rec ボタン・コマンド・起動時の自動開始のどれで
始めても反応する。1 秒ごとに `/run/prudynt/mp4ctl-ch*.active` の有無を見て、赤のチャンネル
(`/sys/class/leds/led_r`) だけを点け消しする。青には触らない。サービスを止めると消灯する。

```sh
device/install-rec-led.sh root@<camera-ip>     # /usr/sbin/rec-led と /etc/init.d/S96rec-led を置いて起動
ssh root@<camera-ip> 'logread | grep rec-led'  # recording on / off のログ
```

外すには `ssh root@<camera-ip> '/etc/init.d/S96rec-led stop; rm /etc/init.d/S96rec-led /usr/sbin/rec-led'`。
LED そのものの操作は [front-led.md](front-led.md)。

## PC に取り出す

Thingino には sftp-server が無いので、scp は `-O` (旧プロトコル) を付ける:

```sh
scp -O root@<camera-ip>:/mnt/mmcblk0p1/rec/2026/11/09/14-42-31.mp4 ~/Downloads/   # 1 本
scp -O -r root@<camera-ip>:/mnt/mmcblk0p1/rec ~/Downloads/                        # フォルダごと
```

録画中にコピーすると、書き込み中の最後の 1 本だけ途中までの内容になる。

## 1 本につなぐ (`tools/concat-recordings.sh`)

```sh
tools/concat-recordings.sh ~/Downloads/rec ~/Downloads/rec-all.mp4   # PC に ffmpeg が必要 (brew install ffmpeg)
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
