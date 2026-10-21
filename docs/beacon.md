# beacon: カメラの状態を LAN に知らせ続け、指令を受け付ける

`beacon` はカメラ上で動く小さな常駐プログラム (Go) で、**数秒ごとに自分の状態 (IP、配信しているか、Wi-Fi の電波の強さなど)
を JSON 1 つにまとめ、UDP のブロードキャストで同じ LAN に送る**。PC 側の `tools/beacon-listen` で受け取って表示する。
また、**LAN から UDP で届いた指令 (映像の上下・左右の反転など) を実行する** (`tools/beacon-send` で送る。[指令](#指令))。

[find-camera](find-camera.md) (mDNS) は「こちらから聞いて、答えてもらう」方式。beacon は聞かなくても流れてくるので、
受け取る側は待つだけでよく、IP が変わったことや配信が止まったことも続けて見える。

```text
$ go -C tools/beacon-listen run .
listening on UDP port 47474 (Ctrl+C to stop)
13:08:59  ing-wyze-cam3-9793        192.168.11.29    streaming 16m16s      wifi -28dBm  up 17m33s  mem 68MB  load 2.13  ciao+da40db6
```

ブロードキャストはルーターを越えないので、**受け取る PC やスマホはカメラと同じ Wi-Fi / LAN にいる必要がある** (mDNS と同じ)。

## インストール

PC に Go (1.25 以降) と、カメラに SD カードが必要。リポジトリのルートで:

```sh
device/beacon/build.sh                      # dist/beacon (カメラ用、約 4MB)
device/install-beacon.sh root@<camera-ip>
```

- バイナリは SD カード (`/mnt/mmcblk0p1/beacon`)、起動スクリプトだけ内蔵側 (`/etc/init.d/S95beacon`) に置く。
  SD にバイナリがあれば起動時に動き始める。設定用ポータルの状態 (`/run/portal_mode`) の時は動かない
- 動作中に SD カードを抜かない (プログラムを SD から実行しているため。osd-feed と同じ)
- 止める: `/etc/init.d/S95beacon stop`。自動起動もやめる: `service disable beacon`
- 消す: `ssh root@<camera-ip> '/etc/init.d/S95beacon stop; rm /etc/init.d/S95beacon /mnt/mmcblk0p1/beacon'`
- ログ: `logread | grep beacon` (送り先が変わった時と、送れなくなった時・送れるようになった時だけ出る)

## 受け取る (`tools/beacon-listen`)

```sh
go -C tools/beacon-listen run .            # Ctrl+C まで、届いたものを 1 件 1 行で出す
go -C tools/beacon-listen run . -t 15s     # 15 秒で終わる (1 件も届かなければ終了コード 1)
go -C tools/beacon-listen run . -json      # 届いた JSON をそのまま 1 行ずつ
```

- 1 行の中身: 受け取った時刻、ホスト名、IP、配信の状態と経過時間、Wi-Fi の電波 (dBm)、起動してからの時間、空きメモリ、
  ロードアベレージ、ファームの版
- カメラが複数あれば、それぞれの行が混ざって流れる (ホスト名で見分ける)
- macOS で初回に「受信接続を許可しますか」と出たら許可する。Windows ではファイアウォールの許可を求められたら
  「プライベート ネットワーク」を許可する (許可しないと届かない)
- 他の PC 用のバイナリ: `GOOS=windows GOARCH=amd64 go -C tools/beacon-listen build -o ../../dist/beacon-listen.exe .`
- **動作を確認したのは macOS だけ**

自前のプログラムで受け取るなら、UDP の 47474 番で待ち、届いた JSON の `type` が `thingino-beacon` のものだけを使えばよい。

## 送る内容

UDP のポート 47474 に、**ネットワークインターフェースごと**に 1 つ、そのネットワークのブロードキャストアドレス
(例 `192.168.11.255`) 宛てに送る。中身は 1 行の JSON (300 バイト程度):

```json
{"type":"thingino-beacon","v":1,"seq":1,"host":"ing-wyze-cam3-9793","iface":"wlan0","ip":"192.168.11.29",
 "mac":"02:b4:91:43:97:94","build":"ciao+da40db6","uptime_s":943,
 "stream":{"state":"streaming","elapsed_s":866},"rssi_dbm":-26,"mem_free_mb":68,"load1":"2.08"}
```

| キー | 中身 |
|---|---|
| `type` / `v` | 常に `thingino-beacon` / 形式の版 (今は `1`)。項目を足しても `v` は変えず、意味を変える時に上げる |
| `seq` | 送るたびに 1 増える (同じ回はどのインターフェースでも同じ)。カメラを再起動すると 1 から |
| `host` | ホスト名 |
| `iface` / `ip` / `mac` | 送ったインターフェース (普通は `wlan0`) と、その IP・MAC アドレス |
| `build` | ファームの版 (`/etc/os-release` の `BUILD_ID`) |
| `uptime_s` | 起動してからの秒数 |
| `stream.state` | 配信 (`youtube-relay`) の状態。下の表 |
| `stream.elapsed_s` | 配信の ffmpeg が動き続けている秒数。ffmpeg が無ければ `null` |
| `rssi_dbm` | Wi-Fi の電波の強さ (wpa_supplicant の `signal_poll`。-50 より大きければ強い、-75 を下回ると弱い、が目安)。`wlan` 以外と、読めない時は無い |
| `mem_free_mb` | 空きメモリ (バッファ・キャッシュを空きに数える。osd-feed の `mem` と同じ) |
| `load1` | 1 分間のロードアベレージ (`/proc/loadavg` の文字のまま) |

読めなかった項目は省く (または `null`)。推測した値は入れない。

### `stream.state`

Web UI の YouTube Live ページの Status と同じ方法で見ている (relay の pidfile と、その子の ffmpeg)。

| 値 | 意味 |
|---|---|
| `stopped` | `youtube-relay` が動いていない (止めた、入っていない) |
| `idle` | `youtube-relay` は動いているが ffmpeg がいない (設定が無い、ネットワークが無い、再起動までの待ち) |
| `connecting` | ffmpeg が動き始めて 60 秒未満 |
| `streaming` | ffmpeg が 60 秒以上動き続けている |

60 秒は relay が「正常な実行」とみなす長さ ([relay.md の見分け方](relay.md#配信できているかの見分け方))。
キーの間違いなどで YouTube に拒否されると ffmpeg は数秒で終わるので、`connecting` と `idle` を繰り返し、`streaming` にはならない。
`streaming` でも、YouTube の画面に出ているか (配信枠の状態など) までは分からない。

## 指令

beacon は UDP のポート 47475 で指令を待ち、実行して結果を送り主に返す。PC からは `tools/beacon-send` で送る:

```sh
go -C tools/beacon-send run . image vflip=toggle                     # LAN 内のすべてのカメラ (ブロードキャスト)
go -C tools/beacon-send run . -to 192.168.11.29 image vflip=true hflip=false   # 1 台に (IP か名前)
go -C tools/beacon-send run . -host ing-wyze-cam3-9793 ping          # ブロードキャストで、この名前のカメラだけ
```

```text
ing-wyze-cam3-9793       192.168.11.29   ok     {"image":{"vflip":false}}
```

| 指令 | 引数 | 動作 |
|---|---|---|
| `ping` | なし | 今の状態を返す (ブロードキャストと同じ内容。`seq` は 0) |
| `image` | `vflip` / `hflip` に `true` / `false` / `toggle` (1 つでも両方でも) | 映像の上下 (`vflip`) / 左右 (`hflip`) の反転を変える。`toggle` は今の値を prudynt に聞いて逆にする。結果は prudynt の答え |

- 変更は**その場だけ**。prudynt の再起動 (カメラの再起動を含む) で、SD の `prudynt-osd.json` の `image` の設定に戻る
  (osd-config が送り直す)。SD のファイルは書き換えない。逆に、`prudynt-osd.json` の `image` を書き換えると、osd-config がそちらを送って上書きする
- 反転するのは映像だけで、OSD (文字・ロゴ) の位置や向きは変わらない
- 決まった指令しか実行しない (シェルのコマンドやファイルの書き込みは受け付けない)。知らない指令や引数にはエラーを返す
- 実行した指令はログに残る (`logread | grep beacon`: `Command from 192.168.11.6: image {"vflip":"toggle"} -> ok`)。`ping` は残さない
- `beacon-send` は返事が無ければ 0.7 秒後に同じ指令をもう一度送る。カメラは同じ送り主の同じ `id` で成功した指令を 2 度実行せず、
  前の結果を返す (`toggle` が 2 回効いて元に戻ることは無い)。失敗した指令 (prudynt が止まっていた、など) は何も変えていないので、
  同じ `id` で送り直せば実行し直す
- カメラは指令のパケットを、送り主全体で 1 秒に 10 個までしか処理しない (それを超えた分は捨てて、ログに 1 行残す)。
  送り主ごとに数えないのは、UDP の送り主のアドレスは偽れるため
- 終了コード: 1 台でも `ok` なら 0、返事が無ければ 1、すべてエラーなら 3
- `-to` を付けないと `255.255.255.255` に送る。PC にネットワークが複数ある (有線と Wi-Fi など) と、片方にしか出ないことがある。
  その時は `-to` にカメラの IP か、そのネットワークのブロードキャストアドレス (例 `192.168.11.255`) を指定する

### 指令の形式

自前のプログラムから送るなら、UDP でカメラ (かブロードキャストアドレス) の 47475 番に、1 パケットに JSON 1 つ (4096 バイトまで) を送る:

```json
{"type":"thingino-command","v":1,"id":"5f2c9a01d4e3b7c8","host":"ing-wyze-cam3-9793","cmd":"image","args":{"vflip":"toggle"}}
```

| キー | |
|---|---|
| `type` / `v` | `thingino-command` / `1` |
| `id` | 送る側が決める任意の文字列。結果にそのまま入る。送り直す時は同じ `id` にする |
| `host` | 省略可。付けるとその名前 (大文字小文字は区別しない) のカメラだけが実行し、他は返事もしない |
| `cmd` / `args` | 指令と引数 |

結果は、送ってきたアドレスとポートに返す:

```json
{"type":"thingino-command-result","v":1,"id":"5f2c9a01d4e3b7c8","host":"ing-wyze-cam3-9793","cmd":"image","ok":true,"result":{"image":{"vflip":false}}}
{"type":"thingino-command-result","v":1,"id":"...","host":"ing-wyze-cam3-9793","cmd":"reboot","ok":false,"error":"unknown command \"reboot\" (known: ping, image)"}
```

## 設定

SD カードに `beacon.args` を置くと、その 1 行目をオプションとして使う (起動時に読む。変えたら `/etc/init.d/S95beacon restart`):

```text
-port 47474 -interval 5s -command-port 47475
```

| オプション | 既定 | |
|---|---|---|
| `-port` | `47474` | 送り先の UDP ポート。受け取る側も `-port` を合わせる |
| `-interval` | `5s` | 送る間隔 (1 秒以上) |
| `-command-port` | `47475` | 指令を待つ UDP ポート。`0` で指令を受け付けない |

手で試す: `/mnt/mmcblk0p1/beacon -once` (各インターフェースの JSON を 1 回だけ画面に出して終わる。送りはしない)。

## 注意

- **同じ LAN にいる誰でも受け取れる** (暗号化も認証もしない)。送るのはホスト名・IP・MAC・配信の状態などで、
  **ストリームキーや Wi-Fi のパスワードは入れていない**。ホスト名と IP は mDNS でも流しているものと同じ。
  共用の Wi-Fi に繋ぐ時に状態を知られたくなければ止める
- 受け取ったものは、送り主を確かめられない (同じ LAN の誰でも同じ形の JSON を送れる)。表示や目安に使い、
  これを根拠に危ない操作をしない
- **指令も送り主を確かめない。同じ LAN にいる誰でも、映像を反転させられる** (暗号化も認証もしない、と決めた。
  できることを決まった指令に絞り、変更をその場だけにしているのはそのため)。共用の Wi-Fi に繋ぐ時は
  `beacon.args` に `-command-port 0` を書いて受け付けを止める
- 負荷: 実測でメモリ (RSS) 約 3MB。送るたびに `/proc` を読み、`wpa_cli` を 1 回起動する

## 確認した範囲

実機 (`ciao+da40db6`) と PC (macOS) の `beacon-listen` で:

- インストール、`beacon.args` (CRLF の行も含む) での間隔の変更
- **再起動後の自動起動**: 起動から 18 秒で最初の 1 件が届き、配信の ffmpeg の起動から `connecting` → 60 秒で `streaming`
- `youtube-relay` を止めると `stopped`、起動し直すと `idle` (ffmpeg の起動前の一瞬) → `connecting` → `streaming`

- 指令: `ping` (ブロードキャスト、`-host` の一致・不一致)、`image vflip=toggle` を 2 回 (スナップショットで映像の上下が
  反転して戻ることを確認。12 秒後も osd-config に戻されていない)、知らない指令・変えられないキーのエラー、ログ

**有線や複数のインターフェース、Windows / Linux での送受信、prudynt の再起動後に SD の設定に戻ることは未確認。**
