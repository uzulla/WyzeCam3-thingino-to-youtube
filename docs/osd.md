# OSD テキストオーバーレイ

配信映像に任意の複数行テキスト (プログレスバー等) を焼き込み、カメラ上の別のプログラムから
更新する機能。テキストの矩形は **3 つ** (既定の位置は左下・右上・右下。左上は prudynt 標準の日時) で、
それぞれ別のファイルで独立に更新できる。`patches/prudynt-osd-textfile.diff` を当ててビルドした prudynt が必要
(ビルド手順は [build.md](build.md))。

```sh
# リポジトリのルートで
device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt
```

- **Thingino の `/usr/bin/prudynt` は上書きしない。** パッチ入りのバイナリを `/usr/bin/prudynt-osd` に
  置き、起動スクリプト `S30prudynt-osd` (`S31prudynt` の直前) が `/usr/bin/prudynt` の上に bind mount する。
  ファームの焼き直しは不要
- インストール時に prudynt を再起動するので、**配信が数秒切れる** (supervisor が自動で再接続する)
- バイナリはビルドしたファーム専用。`/etc/os-release` の `BUILD_ID` がインストール時と違っていたら
  bind mount せず、標準の prudynt のまま起動する
- Web UI にページが増える (Streamer メニューの「OSD Elements」が「OSD text」= `/osd-text.html` に差し替わる。
  [下記](#web-ui-から編集する))
- 元に戻す: `service disable prudynt-osd; service disable osd-config` して再起動 (すぐ戻すなら
  `service stop osd-config; service stop prudynt; /etc/init.d/S30prudynt-osd stop; service start prudynt`)。
  完全に消すなら `/etc/init.d/S30prudynt-osd` `/usr/bin/prudynt-osd` `/usr/bin/prudynt-osd.build`
  `/usr/sbin/osd-progress-demo` `/usr/sbin/osd-config` `/etc/init.d/S93osd-config` `/var/www/osd-text.html`
  `/var/www/x/json-osd-text.cgi` を削除し、メニューを戻す
  (`sed -i 's#/osd-text.html#/streamer-osd.html#; s#"OSD text"#"OSD Elements"#' /var/www/a/plugins.js`)。置いていれば
  設定ファイル (`/etc/prudynt-osd.json` と、SD カード直下の `prudynt-osd.json` = カメラ上では
  `/mnt/mmcblk0p1/prudynt-osd.json`) を削除する。どちらかが残っていると、入れ直した時や `osd-config` を
  有効に戻した時に、古い OSD 設定が自動で反映される。設定ファイルで `general.osd_pool_size` を使っていた場合は
  `/etc/prudynt.json` にその値が残っているので、`jct /etc/prudynt.json set general.osd_pool_size 0` で戻す
- Thingino を入れ直した/更新した後は、他のファイルと同じく入れ直しが必要 (新しいファームに合わせて
  prudynt をビルドし直す)

## 使い方

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

## Web UI から編集する

Thingino の Web UI の Streamer メニュー → **OSD text** (`http://<camera-ip>/osd-text.html`) で、3 つの矩形の設定
(有効、位置、桁×行、倍率、色) と日時 (`osd.burnin`) の書式・倍率・色、`general.osd_pool_size`、そして
矩形ごとのテキストの中身を編集できる。レイアウトの確認用で、下の節の JSON を手で書くのと同じことが GUI でできる。

- プレビューは既存ページと同じ MJPEG。テキストは映像に焼き込まれるので、配信に出るものがそのまま映る
- **Show** / **Clear** は矩形のファイル (`/run/prudynt/osd-text*`) を書く/消す (上の `mv` の手順と同じ。tmpfs のみ)。
  桁数・行数を超える行や ASCII 以外の文字はテキスト欄の下に警告が出る (prudynt 側では切り捨て/空白になる)。
  同じファイルを別のプログラム (プログレスバー等) が更新している時は取り合いになるので、動作確認用と割り切る
- **Apply now** は実行中の prudynt にだけ送る (`prudyntctl json` と同じ。prudynt の再起動で消える)
- **Save to SD card** は SD 直下の `prudynt-osd.json` を書き換える (次の節)。`osd-config` が 5 秒以内に反映するので
  prudynt の再起動は要らない。ファイルに既にあるキーのうち、ページにない項目 (`path`、`substream_disabled` など) は
  残る。`general.osd_pool_size` を変えた時だけ、`osd-config` が `/etc/prudynt.json` に書いて prudynt を再起動する
  (配信が数秒切れる) ので、保存前に確認が出る。SD カードが挿さっていない時は保存できない (エラーになる)
- **Log** に `logread` の `textfile` (プールに入らず縮めた警告など) と `osd-config` の行を出す。Reload で読み直す
- Thingino 標準の OSD ページ (`/streamer-osd.html`) はメニューから外れるだけで残っている。URL で開いて
  「Save configuration」を押すと `/etc/prudynt.json` (フラッシュ) に保存されるので、使わない

裏側の CGI (`/x/json-osd-text.cgi`) は Thingino の認証 (ログインのセッション Cookie、または API キー) を通せば
カメラの外からも叩ける。API キー (Settings → Web Interface の API key、`/etc/thingino-api.key`) は `?token=` で渡す
(`X-API-Key` ヘッダはこの uhttpd が CGI に渡さない)。テキストを更新するプログラムをカメラの外に置く時の入口になる:

```sh
KEY=$(ssh root@<camera-ip> cat /etc/thingino-api.key)
# 左下 (slot=1) に表示。slot=2 が右上、3 が右下。空のボディで消える
printf 'UPLOAD job-42\n[##########----------] 50%%\n' \
  | curl -s --data-binary @- "http://<camera-ip>/x/json-osd-text.cgi?action=text&slot=1&token=$KEY"
# 現在の設定・テキスト・ログ
curl -s "http://<camera-ip>/x/json-osd-text.cgi?action=status&token=$KEY"
# prudynt-osd.json をまるごと置き換える (中身は次の節の形。osd-config が反映する)
curl -s --data-binary @prudynt-osd.json "http://<camera-ip>/x/json-osd-text.cgi?action=save&token=$KEY"
```

書き込み先は `osd.textfileN.path` (`/run` か `/tmp` の下に限る) と SD の `prudynt-osd.json` だけで、フラッシュには何も書かない。
カメラの中で更新するなら、この CGI を通す必要はない (`osd-progress-demo` のように直接ファイルを `mv` する)。

## 設定 (`osd.textfile.*` / `osd.textfile2.*` / `osd.textfile3.*`)

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

- prudynt は `osd.sei.enabled` と `osd.burnin.enabled` が**両方 false** だと OSD の処理自体を起動しない。その構成で
  テキストだけ出したい時は、`/etc/prudynt.json` に `osd.textfile.enabled: true` (または `textfile2` / `textfile3`) を書いて
  起動する必要がある (実行中に `prudyntctl json` で有効化しても出ない)。どちらかが true なら (既定は両方 true)
  実行中に有効化できる
- `path` と色の変更が映像に出るまで最大 1 秒かかる (文字列の設定は 1 秒に 1 回だけ読み直す。ファイルの中身の
  更新は 0.1 秒以内)
- 常用するなら次の節のとおり SD カードに設定ファイルを置く (または `/etc/prudynt.json` の `osd.textfile` 等に書く)。
  このリポジトリのスクリプトは、`general.osd_pool_size` (後述) 以外は prudynt の設定ファイルを書き換えない
  (`prudyntctl json` に `save_config` を送ればフラッシュに保存されるが、スクリプトからは送っていない)。
  **`prudyntctl json` での変更は prudynt の再起動で消える**
  (再起動後に何も出なくなったら、まず `enabled` が false に戻っていないか見る)

## 設定を SD カードに置く (再起動しても消えないようにする)

`prudyntctl json` で変えた設定は、prudynt の再起動やカメラの再起動で消える。SD カード直下に
`prudynt-osd.json` を置いておくと、常駐スクリプト `osd-config` が自動で送り直す
(ストリームキーの `youtube-relay.json` と同じく **SD → `/etc/prudynt-osd.json`** の順で探す)。

```json
{
  "general": { "osd_pool_size": 2048 },
  "osd": {
    "textfile":  { "enabled": true, "cols": 40, "rows": 10 },
    "textfile2": { "enabled": true },
    "textfile3": { "enabled": true, "rows": 4 }
  }
}
```

- 中身は `prudyntctl json` に渡す JSON と同じ形。**送られるのは `osd` の部分だけ**で、それ以外のキー
  (`action` や映像の設定など) は無視される。`osd.burnin` (日時の書式や大きさ) も書ける。
  例外は `general.osd_pool_size` (OSD プールの大きさ、KB) で、これだけは prudynt に送るのではなく
  `/etc/prudynt.json` に書く (下記)
- 送り直すのは、prudynt が再起動した時 (WebUI で設定を変えた後など) と、ファイルの中身が変わった時
  (SD を挿した、書き換えた)。5 秒間隔で見ているので、カメラの再起動は要らない
- ファイルが無くなると (SD を抜くと)、**そのファイルに書いてあったテキストの矩形だけ**を無効に戻す
  (ファイルに出てこない矩形や日時の設定には触らない)。設定ファイルを置いていなければ、prudynt には何も送らない
  (手で `prudyntctl json` した設定に干渉しない)
- prudynt が設定を受け付けない状態が 30 秒続くと (キーの綴り間違い、パッチなしの prudynt など)、
  `logread | grep osd-config` に 1 回出して、送り続ける
- テキストの矩形 (`textfile` / `textfile2` / `textfile3`) のブロックをファイルから消すと、その矩形は無効に戻る
  (別の設定ファイルに切り替わった時も同じ: 前のファイルにだけあった矩形は消える)。それ以外は
  **ファイルに書いてあるキーだけを送る**ので、消したキー (`rows` や `osd.burnin` の設定など) は prudynt が
  再起動するまで前の値のまま残る
- JSON が壊れている時は `logread | grep osd-config` に理由を 1 回出す。反映済みのファイルを編集して壊した時は
  表示を変えない (直せば反映される)。別のファイルに切り替わった先が壊れていた時 (SD を抜いたら内蔵の設定が
  壊れていた、など) は、前のファイルが有効にした矩形を無効に戻す
- `general.osd_pool_size` 以外はフラッシュに何も書かない (`save_config` を送らない、`/etc/prudynt.json` に触らない)
- **`general.osd_pool_size`** (OSD プールの大きさ、KB、0 = 既定。次の節) は prudynt の起動時にしか確保されないので、
  他のキーと違って `/etc/prudynt.json` に `jct` で書いて **prudynt を再起動する** (配信が数秒切れる)。
  やるのはファイルの値と `/etc/prudynt.json` の値が違う時だけなので、普通は最初の 1 回 (ファイルを置いた時、
  または値を変えた時) だけ。`logread` に `general.osd_pool_size 0 -> 2048 KB ... restarting prudynt` と出る
  - 受け付ける値は 0〜16384。範囲外や数字でないものは `logread` に 1 回出して無視する
  - 新しい値で prudynt が起動しなかった時は元の値に戻して起動し直す (`... restored 0`)。その値はファイルを
    書き換えるまで再試行しない。古い prudynt が 20 秒たっても止まらなかった時は再起動を諦め、値は
    `/etc/prudynt.json` に残る (次に prudynt が再起動した時に効く。`... applies at its next restart`)
  - ファイルが無くなっても (SD を抜いても) この値は戻さない (メモリを予約するだけで、何も表示しない。
    戻すたびに再起動と書き込みをする方が害が大きい)。既定に戻したい時はファイルに 0 を書く
- ログ: `logread | grep osd-config`。止めて、次回の起動でも動かないようにする:
  `service stop osd-config; service disable osd-config`

## 大きさの上限と OSD プール

テキストの矩形と日時は、libimp の **OSD プール** というメモリを分け合う (1 画素 4 バイト)。プールに入らない
矩形を libimp は**エラーなしで表示しない**ので、パッチ側で予算を計算して、入らない設定は自動で縮める
(`logread | grep textfile` に `reduced to ...` / `not shown` の警告が出る)。

- 予算は `osd.textfile` → `textfile2` → `textfile3` の順に割り当てる。足りなくなると後ろの矩形から
  scale → 行数 → 桁数の順に縮み、それでも入らなければ消える。前の矩形を小さくすれば自動で戻る
- 既定のプールは 720p で約 616KB (テキストに使えるのは約 535KB)。**40 桁×10 行 (scale 2、476KB) だけで
  ほぼ使い切る**ので、3 つとも既定の大きさで出すにはプールを上げる
- プールは `/etc/prudynt.json` の `general.osd_pool_size` (KB、0 = 自動)。起動時にしか確保されないので、
  変えたら prudynt の再起動が要る。SD カードの `prudynt-osd.json` に `"general": {"osd_pool_size": 2048}` と
  書けば `osd-config` がやる (前の節)。手でやるなら:

  ```sh
  jct /etc/prudynt.json set general.osd_pool_size 2048 && service restart prudynt
  ```

  どちらも prudynt (Thingino) の設定ファイルをフラッシュに書き換える操作 (値が変わる時だけ)。戻すには 0 を設定する

720p での目安 (測定の詳細は [NOTES.md](../NOTES.md) の「OSD プールの上限」):

| `osd_pool_size` | 結果 |
|---|---|
| 0 (自動 ≒ 616KB) | 1 つの矩形で約 535KB まで (scale 2 なら 40 桁×11 行 = 521KB、scale 3 なら 40 桁×4 行 = 456KB。scale 3 の 5 行は 562KB で予算を超え、自動で縮む) |
| 1024 / 2048 | 起動・表示とも問題なし。2048 で 52 桁×25 行の scale 2 (840x458px) が出る |
| 4096 | **画面ほぼ全面** (1260x688px = 52 桁×25 行の scale 3) が出る。720p ではこれ以上大きくする意味がない |
| 8192 / 16384 | 起動・表示とも問題なし (Linux 側の空きメモリは変わらない) |
| 32768 | **prudynt が起動しない** (プールは予約メモリ rmem 29MB から取られるため)。設定を戻せば復旧する |

上げすぎると、同じ予約メモリを使う映像バッファと取り合いになる。720p なら 2048〜4096 で足りる。
1080p では映像バッファが増える分だけ上限が下がるはずだが、値は測っていない。

