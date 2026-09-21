# device/ — カメラへインストールするファイル

Thingino 実機に置く supervisor 一式。`/etc` 以下は overlayfs でフラッシュに永続化されるため、
ファームウェアを書き換えずにインストールでき、再起動後も残ります。

| ファイル | インストール先 | 役割 |
|---|---|---|
| `youtube-relay` | `/usr/sbin/youtube-relay` | supervisor 本体 (ffmpeg を監視・再起動するループ) |
| `S93youtube-relay` | `/etc/init.d/S93youtube-relay` | 起動スクリプト (boot 時に自動開始) |
| `youtube-relay.json.example` | `/etc/youtube-relay.json` または SD カード直下 | 設定ファイル (下記「SD カードモード」参照) |
| `install.sh` | (PC 側で実行) | 上記と ffmpeg を SSH 経由でまとめて入れるインストーラ。再実行可能。**このリポジトリのファイルを置くだけで、Thingino 側の設定やファーム更新には触らない** |
| `disable-netwatch.sh` | (PC 側で実行) | Thingino の netwatch (ping 失敗で OS を再起動) を無効化する。OS 設定の変更なので `install.sh` とは別 (下記「netwatch」参照) |
| `install-wifi-from-sd.sh` / `S37wifi-from-sd` | (PC 側で実行) / `/etc/init.d/S37wifi-from-sd` | **任意**。SD の `wpa_supplicant.conf` (複数の Wi-Fi 可) を起動時に適用する。OS 設定を書き換えるので `install.sh` とは別 (下記「Wi-Fi 設定も SD カードで運ぶ」参照) |
| `install-prudynt-osd.sh` / `S30prudynt-osd` / `osd-progress-demo` | (PC 側で実行) / `/etc/init.d/S30prudynt-osd` / `/usr/sbin/osd-progress-demo` | **任意**。映像に任意のテキストを重ねられる prudynt (パッチ入り) を入れる。Thingino のストリーマを差し替えるので `install.sh` とは別 (下記「OSD テキストオーバーレイ」参照) |
| `osd-config` / `S93osd-config` / `prudynt-osd.json.example` | `/usr/sbin/osd-config` / `/etc/init.d/S93osd-config` / SD カード直下または `/etc/prudynt-osd.json` | **任意** (`install-prudynt-osd.sh` が入れる)。OSD の設定を SD カードのファイルから読み、prudynt が再起動するたびに送り直す (下記「設定を SD カードに置く」参照) |
| `common.sh` | (PC 側の各スクリプトが読み込む) | 対応ファームの判定。カメラの `/etc/os-release` の `BUILD_ID` が **`ciao+da40db6`** でなければ何も変更せず中止する |

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

## Wi-Fi 設定も SD カードで運ぶ

上の SD カードモードと組み合わせると、**SD 1 枚に Wi-Fi 設定とストリームキーを入れて持ち運べる**
(車載で現地のモバイルルーターに繋ぎ替える、など)。方法は 2 つ。**併用はしないこと**
(両方あると A が後から上書きする)。

| | A. `uenv.txt` (Thingino 標準) | B. `wpa_supplicant.conf` (このリポジトリの追加スクリプト) |
|---|---|---|
| 書ける Wi-Fi | **1 つだけ** | **複数** (自宅 + モバイルルーター等。`priority` も使える) |
| インストール | 不要 | `./install-wifi-from-sd.sh root@<camera-ip>` |
| SD に置くファイル | `uenv.txt` | `wpa_supplicant.conf` |

どちらも共通の注意:

- 読み込まれるのは**起動時だけ**。SD を挿しただけでは反映されないので再起動する
- 適用された設定はカメラ側に保存される。**SD を抜いても元の Wi-Fi には戻らない**
  (最後に適用されたものが残る)。繋がらなくなったら、SD のファイルを直して再起動すれば復旧できる
  (毎回 SD の内容が正になる)。**試す前に、確実に繋がる設定を書いた SD を用意しておくこと**
  (繋がらないと SSH でも直せない)
- **SD 上のパスワードは平文**。ストリームキーと同じく、SD を抜かれたら読まれる前提で使う

### A. `uenv.txt` — Wi-Fi が 1 つでよい場合 (Thingino 標準)

このリポジトリの機能ではなく Thingino 本体の機能 (`S38wpa_supplicant` の `credentials_from_card`)。
SD 直下に `uenv.txt` を置く (改行は LF):

```text
wlan_ssid=MyNetwork
wlan_pass=MyPassword
```

- カメラ側にはパスワードを PSK ハッシュにして保存する
- `wlan_ssid` / `wlan_pass` のどちらかが欠けていると適用されない (既存の設定のまま)
- 同じキーを複数行書くことはできない (値が壊れる)

### B. `wpa_supplicant.conf` — 複数の Wi-Fi を書きたい場合

```sh
./install-wifi-from-sd.sh root@<camera-ip>    # 起動スクリプト /etc/init.d/S37wifi-from-sd を入れるだけ
```

`/etc/wpa_supplicant.conf` (Thingino の OS 設定) を書き換える機能なので、`install.sh` とは別の
任意のスクリプトにしてある。SD 直下に通常の `wpa_supplicant.conf` を置く:

```text
network={
        ssid="Home"
        psk="home-password"
        priority=10
}
network={
        ssid="Pocket-WiFi"
        psk="pocket-password"
        scan_ssid=1
}
```

- Thingino の Wi-Fi 起動 (`S38`) の直前に動き、内容が変わったときだけ書き換える
  (毎回フラッシュに書かない)。Windows の改行 (CRLF) は取り除く
- `ctrl_interface=` の行が無ければ Thingino 標準のヘッダを補う。`network={}` ブロックだけ書けばよい
- `psk=` は `wpa_passphrase <ssid> <password>` で作った 64 桁のハッシュでも可 (平文を置きたくない場合)
- **`psk=` の無いオープンな Wi-Fi は使えない** (Thingino が「未設定」と判断して設定用ポータルを
  起動してしまうため)。その場合ファイルは無視され、ログに理由が出る
- 適用前の設定は一度だけ `/etc/wpa_supplicant.conf.before-sd` に退避する
- ログ: `logread | grep wifi-from-sd`。やめるとき: `rm /etc/init.d/S37wifi-from-sd`
- Thingino を入れ直した/更新した後は、`install.sh` と同じく入れ直しが必要

> **未検証の点** (A・B とも、ソース `ciao+da40db6` の読解と PC 上の busybox でのテストに基づく。
> 実機確認は #10):
> - Wi-Fi が一度も設定されていないカメラに A を使うと、その起動は設定用ポータルで立ち上がり、
>   **もう一度再起動して初めて接続される**可能性がある (接続モードの判定が SD の読み込みより前のため)。
>   B は `S38` より前にファイルを置くのでこの問題は起きないはず
> - 起動スクリプトの時点で SD がマウント済みか (B は最大 10 秒待つ)
> - A は Thingino の公式ドキュメントに記載が無く、将来のファームで変わりうる

## OSD テキストオーバーレイ (任意)

配信映像に任意の複数行テキスト (プログレスバー等) を焼き込み、カメラ上の別のプログラムから
更新する機能。テキストの矩形は **3 つ** (既定の位置は左下・右上・右下。左上は prudynt 標準の日時) で、
それぞれ別のファイルで独立に更新できる。`patches/prudynt-osd-textfile.diff` を当ててビルドした prudynt が必要
(ビルド手順はトップの README)。

```sh
./install-prudynt-osd.sh root@<camera-ip> path/to/prudynt
```

- **Thingino の `/usr/bin/prudynt` は上書きしない。** パッチ入りのバイナリを `/usr/bin/prudynt-osd` に
  置き、起動スクリプト `S30prudynt-osd` (`S31prudynt` の直前) が `/usr/bin/prudynt` の上に bind mount する。
  ファームの焼き直しは不要
- インストール時に prudynt を再起動するので、**配信が数秒切れる** (supervisor が自動で再接続する)
- バイナリはビルドしたファーム専用。`/etc/os-release` の `BUILD_ID` がインストール時と違っていたら
  bind mount せず、標準の prudynt のまま起動する
- 元に戻す: `service disable prudynt-osd` して再起動 (すぐ戻すなら
  `service stop prudynt; /etc/init.d/S30prudynt-osd stop; service start prudynt`)。
  完全に消すなら `/etc/init.d/S30prudynt-osd` `/usr/bin/prudynt-osd` `/usr/bin/prudynt-osd.build`
  `/usr/sbin/osd-progress-demo` `/usr/sbin/osd-config` `/etc/init.d/S93osd-config` を削除する
- Thingino を入れ直した/更新した後は、他のファイルと同じく入れ直しが必要 (新しいファームに合わせて
  prudynt をビルドし直す)

### 使い方

```sh
# 1. 有効化 (実行中の prudynt にだけ反映。再起動で無効に戻る)
prudyntctl json '{"osd":{"textfile":{"enabled":true}}}'

# 2. テキストを出す/書き換える: 一時ファイルに書いて mv で置き換える (0.1 秒以内に反映)
printf 'UPLOAD job-42\n[##########----------] 50%%\n' > /run/prudynt/osd-text.tmp \
  && mv /run/prudynt/osd-text.tmp /run/prudynt/osd-text

# 3. 消す: ファイルを消す (空にしても同じ)
rm /run/prudynt/osd-text

# 右上・右下も同じ。設定名とファイル名が違うだけ
prudyntctl json '{"osd":{"textfile2":{"enabled":true},"textfile3":{"enabled":true}}}'
echo 'REC' > /run/prudynt/osd-text2.tmp && mv /run/prudynt/osd-text2.tmp /run/prudynt/osd-text2
```

| 設定 | 既定の位置 | 既定のファイル | 既定の桁×行 |
|---|---|---|---|
| `osd.textfile` | 左下 (`pos_x` 8, `pos_y` -8) | `/run/prudynt/osd-text` | 40×4 |
| `osd.textfile2` | 右上 (-8, 8) | `/run/prudynt/osd-text2` | 24×2 |
| `osd.textfile3` | 右下 (-8, -8) | `/run/prudynt/osd-text3` | 24×2 |

位置はどれも自由に変えられる (3 つの違いは既定値だけ)。矩形同士が重なった場合は番号の大きい方が手前。

- `/run` は tmpfs なので、何度書き換えてもフラッシュは減らない。**必ず `mv` で置き換える**
  (直接 `>` で書くと、書きかけの内容が一瞬映ることがある)
- prudynt は 0.1 秒ごとにファイルの inode / mtime / サイズを見て、変わった時だけ描き直す。
  内容が同じなら描き直さない
- 表示できるのは ASCII (8x8 フォント)。桁数・行数を超えた分は切り捨て。タブや制御文字は空白になる
- `osd-progress-demo [秒数]` が 0.5 秒更新のプログレスバーのサンプル (`osd_text` / `osd_clear` / `bar` の
  シェル関数はそのまま流用できる)

### 設定 (`osd.textfile.*` / `osd.textfile2.*` / `osd.textfile3.*`)

3 つとも同じキーを持つ。`prudyntctl json '{"osd":{"textfile":{...}}}'` で実行中に変えられる。現在値は
`prudyntctl json '{"osd":{"textfile":null,"textfile2":null,"textfile3":null}}'`。

| キー | 既定値 | 意味 |
|---|---|---|
| `enabled` | `false` | 表示する |
| `substream_disabled` | `true` | サブストリーム (ch1) には出さない |
| `path` | `/run/prudynt/osd-text` | 映すファイル |
| `scale` | `0` | 文字の倍率 1〜10。0 = 自動 (映像の幅 / 480。720p で 2、1080p で 4) |
| `cols` / `rows` | 上の表 | 最大の桁数 / 行数 (1〜128 / 1〜32)。**リージョンは常にこのサイズで確保される** (背景ボックスもこの大きさ) |
| `pos_x` / `pos_y` | 上の表 | 位置 (px)。0 以上は左/上端から、負の値は右/下端からの距離 |
| `fill_color` | `#ffffffff` | 文字色 `#RRGGBBAA` |
| `outline_color` | `#00000000` | 縁取りの色。alpha が 0 なら縁取りなし (既定) |
| `background_color` | `#00000080` | 背景ボックスの色。alpha が 0 なら背景なし |

- 常用するなら `/etc/prudynt.json` の `osd.textfile` 等に書く。このリポジトリのスクリプトは prudynt の
  設定ファイルを書き換えない (`prudyntctl json` に `save_config` を送ればフラッシュに保存されるが、
  スクリプトからは送っていない)。**`prudyntctl json` での変更は prudynt の再起動で消える**
  (再起動後に何も出なくなったら、まず `enabled` が false に戻っていないか見る)

### 設定を SD カードに置く (再起動しても消えないようにする)

`prudyntctl json` で変えた設定は、prudynt の再起動やカメラの再起動で消える。SD カード直下に
`prudynt-osd.json` を置いておくと、常駐スクリプト `osd-config` が自動で送り直す
(ストリームキーの `youtube-relay.json` と同じく **SD → `/etc/prudynt-osd.json`** の順で探す)。

```json
{
  "osd": {
    "textfile":  { "enabled": true, "cols": 40, "rows": 10 },
    "textfile2": { "enabled": true },
    "textfile3": { "enabled": true, "rows": 4 }
  }
}
```

- 中身は `prudyntctl json` に渡す JSON と同じ形。**送られるのは `osd` の部分だけ**で、それ以外のキー
  (`action` や映像の設定など) は無視される。`osd.burnin` (日時の書式や大きさ) も書ける
- 送り直すのは、prudynt が再起動した時 (WebUI で設定を変えた後など) と、ファイルの中身が変わった時
  (SD を挿した、書き換えた)。5 秒間隔で見ているので、カメラの再起動は要らない
- ファイルが無くなると (SD を抜くと)、このスクリプトが有効にしたテキストを全部無効に戻す。
  設定ファイルを一度も置いていなければ、prudynt には何も送らない (手で `prudyntctl json` した設定に干渉しない)
- **送るのはファイルに書いてあるキーだけ。** ファイルから消したキーは、prudynt が再起動するまで前の値のまま残る
  (例: `textfile2` の行を消しても右上は消えない。消したい時は `"enabled": false` と書く)
- JSON が壊れている時は何も変えず、`logread | grep osd-config` に理由を 1 回出す
- フラッシュには何も書かない (`save_config` を送らない、`/etc/prudynt.json` に触らない)
- **OSD プールのサイズ (`general.osd_pool_size`) はこの方法では変えられない** (prudynt の起動時にしか確保されない)。
  大きくしたい時は次の節の手順で `/etc/prudynt.json` に 1 回だけ書く
- ログ: `logread | grep osd-config`。止める: `service disable osd-config`

### 大きさの上限と OSD プール

テキストの矩形と日時は、libimp の **OSD プール** というメモリを分け合う (1 画素 4 バイト)。プールに入らない
矩形を libimp は**エラーなしで表示しない**ので、パッチ側で予算を計算して、入らない設定は自動で縮める
(`logread | grep textfile` に `reduced to ...` / `not shown` の警告が出る)。

- 予算は `osd.textfile` → `textfile2` → `textfile3` の順に割り当てる。足りなくなると後ろの矩形から
  scale → 行数 → 桁数の順に縮み、それでも入らなければ消える。前の矩形を小さくすれば自動で戻る
- 既定のプールは 720p で約 616KB (テキストに使えるのは約 535KB)。**40 桁×10 行 (scale 2、476KB) だけで
  ほぼ使い切る**ので、3 つとも既定の大きさで出すにはプールを上げる
- プールは `/etc/prudynt.json` の `general.osd_pool_size` (KB、0 = 自動)。起動時にしか確保されないので、
  変えたら `service restart prudynt`:

  ```sh
  jct /etc/prudynt.json set general.osd_pool_size 2048 && service restart prudynt
  ```

  これは prudynt (Thingino) の設定ファイルをフラッシュに書き換える操作なので、このリポジトリのスクリプトでは
  行わない。戻すには 0 を設定する

実測 (`ciao+da40db6`、720p、2026-09-21。詳細は NOTES.md):

| `osd_pool_size` | 結果 |
|---|---|
| 0 (自動 ≒ 616KB) | 1 つの矩形で約 535KB まで (scale 2 なら 40 桁×11 行、scale 3 なら 40 桁×5 行) |
| 1024 / 2048 | 起動・表示とも問題なし。2048 で 52 桁×25 行の scale 2 (840x458px) が出る |
| 4096 | **画面ほぼ全面** (1260x688px = 52 桁×25 行の scale 3) が出る。720p ではこれ以上大きくする意味がない |
| 8192 / 16384 | 起動・表示とも問題なし (Linux 側の空きメモリは変わらない) |
| 32768 | **prudynt が起動しない** (プールは予約メモリ rmem 29MB から取られるため)。設定を戻せば復旧する |

上げすぎると、同じ予約メモリを使う映像バッファと取り合いになる。720p なら 2048〜4096 で足りる。
1080p での上限は未実測。

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

# 推奨: ping 失敗で OS ごと再起動する netwatch を止める (OS 設定の変更。下記「netwatch」参照)
./disable-netwatch.sh root@<camera-ip>
```

> `install.sh` は `ciao+da40db6` の実機で確認済み (2026-09-21: 新規インストール、および配信中の
> 再実行 = 旧 supervisor の停止待ち → 入れ替え → 約 5 秒で配信再開)。うまくいかない場合は下の手作業の手順で。

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
#     PC で SD 直下に youtube-relay.json を書いてカメラに挿すだけ

# 4. 起動
ssh $CAM '/etc/init.d/S93youtube-relay start'

# 状態確認・ログ
ssh $CAM '/etc/init.d/S93youtube-relay status'
ssh $CAM 'logread | grep youtube-relay | tail -20'
```

## Thingino を入れ直した / 更新した後

このディレクトリのファイルと `/usr/bin/ffmpeg` は、Thingino を新規インストールした直後の
カメラに入れる想定。Thingino を入れ直したり更新したりするとカメラ上の書き込み領域ごと消えるので、
その後にもう一度 `install.sh` を実行する (ストリームキーも入れ直す):

```sh
# ffmpeg はそのファーム (toolchain 世代) に合わせてビルドしたものを使う (対応表はトップの README)
./install.sh root@<camera-ip> ../dist/ffmpeg
```

Thingino 自体の更新や設定のバックアップはこのリポジトリの範囲外。

## netwatch (ネットワーク監視による OS 再起動) は無効化する

2026-09 以降の Thingino には `S52netwatch` が入っており、**デフォルトで有効**。
デフォルトゲートウェイへ 30 秒毎に ping し、**3 回連続で失敗するとカメラを再起動する**。
プロセスの再起動ではなく **OS ごとの強制リブート** (`reboot -f`。効かなければ sysrq、それも
効かなければ watchdog デーモンを止めてあるのでハードウェアリセット) で、配信は一度切れる。
2026-08 以前のファームにこの仕組みは無かった (Wi-Fi が切れても待つだけで再起動はしなかった)。

ライブ配信用途では**無効化を推奨**する:

- ネットワーク断からの復帰は supervisor が担う (デフォルトルートが戻るのを待って配信を再開)。
  OS ごと再起動しても復帰は早くならず、起動時間の分だけ配信の欠損が延びる
- ICMP に応答しないモバイルルーター / テザリングでは、Wi-Fi が正常でも約 90 秒毎に再起動して
  配信が成立しない

```sh
./disable-netwatch.sh root@<camera-ip>
```

これは Thingino の OS 設定 (`/etc/thingino.json` の `netwatch.enabled`) を変えるものなので、
`install.sh` とは別のスクリプトにしてある (`install.sh` は OS の設定に触らない)。
設定は再起動後も残るが、**Thingino を入れ直した/更新した後は初期値 (有効) に戻ることがある**ので、
`install.sh` と合わせて実行し直す。手作業なら:

```sh
jct /etc/thingino.json set netwatch.enabled false && service restart netwatch
jct /etc/thingino.json set netwatch.enabled true  && service restart netwatch   # 元に戻す
```

無効化の代償: Wi-Fi ドライバが固まって復帰しなくなった場合に自動では直らなくなる
(電源の入れ直しが必要)。無人・遠隔設置でそちらの方が困る場合は、無効化せず
`netwatch.target` を確実に ping に応答するホストにする手もある。

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

### 症状別

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
| カメラが勝手に再起動する (約 90 秒毎、または Wi-Fi 断のたび) | netwatch がゲートウェイへの ping 失敗で OS を再起動している。`./disable-netwatch.sh` で無効化する (上記「netwatch」参照)。Thingino を更新した後に再発したら設定が初期値に戻っている |
| YouTube Studio に何も出ない (ログは Starting ffmpeg) | ストリームキーの間違いが最有力。YouTube 側は間違ったキーでも接続を受けてから切断するため、`ffmpeg exited` の繰り返しになっていないかログを確認 |
| 映像は出るが音が出ない | RTSP に複数の音声トラックが載っている可能性。設定の **`ffmpeg_out_opts`** に `-map 0:v:0 -map 0:a:0` を指定して (`-map` は出力オプションなので `-i` の前に展開される `ffmpeg_opts` に書くと ffmpeg が起動エラーになる)。 AAC トラックを明示する |

### ffmpeg のエラーを直接見る

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
