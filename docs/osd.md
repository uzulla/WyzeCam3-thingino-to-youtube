# OSD テキストオーバーレイ

配信映像に任意の複数行テキスト (プログレスバー等) を焼き込み、カメラ上の別のプログラムから
更新する機能。テキストの矩形は **3 つ** (既定の位置は左下・右上・右下。左上は prudynt 標準の日時) で、
それぞれ別のファイルで独立に更新できる。`patches/prudynt-osd-textfile.diff` を当ててビルドした prudynt が必要
(ビルド手順は [build.md](build.md))。

```sh
device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt
```

- **Thingino の `/usr/bin/prudynt` は上書きしない。** パッチ入りのバイナリを `/usr/bin/prudynt-osd` に
  置き、起動スクリプト `S30prudynt-osd` (`S31prudynt` の直前) が `/usr/bin/prudynt` の上に bind mount する。
  ファームの焼き直しは不要
- インストール時に prudynt を再起動するので、**配信が数秒切れる** (supervisor が自動で再接続する)
- バイナリはビルドしたファーム専用。`/etc/os-release` の `BUILD_ID` がインストール時と違っていたら
  bind mount せず、標準の prudynt のまま起動する
- 元に戻す: `service disable prudynt-osd; service disable osd-config` して再起動 (すぐ戻すなら
  `service stop osd-config; service stop prudynt; /etc/init.d/S30prudynt-osd stop; service start prudynt`)。
  完全に消すなら `/etc/init.d/S30prudynt-osd` `/usr/bin/prudynt-osd` `/usr/bin/prudynt-osd.build`
  `/usr/sbin/osd-progress-demo` `/usr/sbin/osd-config` `/etc/init.d/S93osd-config` を削除する
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

- 常用するなら `/etc/prudynt.json` の `osd.textfile` 等に書く。このリポジトリのスクリプトは prudynt の
  設定ファイルを書き換えない (`prudyntctl json` に `save_config` を送ればフラッシュに保存されるが、
  スクリプトからは送っていない)。**`prudyntctl json` での変更は prudynt の再起動で消える**
  (再起動後に何も出なくなったら、まず `enabled` が false に戻っていないか見る)

## 設定を SD カードに置く (再起動しても消えないようにする)

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
- ファイルが無くなると (SD を抜くと)、**そのファイルに書いてあったテキストの矩形だけ**を無効に戻す
  (ファイルに出てこない矩形や日時の設定には触らない)。設定ファイルを置いていなければ、prudynt には何も送らない
  (手で `prudyntctl json` した設定に干渉しない)
- prudynt が設定を受け付けない状態が 30 秒続くと (キーの綴り間違い、パッチなしの prudynt など)、
  `logread | grep osd-config` に 1 回出して、送り続ける
- **送るのはファイルに書いてあるキーだけ。** ファイルから消したキーは、prudynt が再起動するまで前の値のまま残る
  (例: `textfile2` の行を消しても右上は消えない。消したい時は `"enabled": false` と書く)
- JSON が壊れている時は何も変えず、`logread | grep osd-config` に理由を 1 回出す
- フラッシュには何も書かない (`save_config` を送らない、`/etc/prudynt.json` に触らない)
- **OSD プールのサイズ (`general.osd_pool_size`) はこの方法では変えられない** (prudynt の起動時にしか確保されない)。
  大きくしたい時は次の節の手順で `/etc/prudynt.json` に 1 回だけ書く
- ログ: `logread | grep osd-config`。止める: `service disable osd-config`

## 大きさの上限と OSD プール

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

720p での目安 (測定の詳細は [NOTES.md](../NOTES.md) の「OSD プールの上限」):

| `osd_pool_size` | 結果 |
|---|---|
| 0 (自動 ≒ 616KB) | 1 つの矩形で約 535KB まで (scale 2 なら 40 桁×11 行、scale 3 なら 40 桁×5 行) |
| 1024 / 2048 | 起動・表示とも問題なし。2048 で 52 桁×25 行の scale 2 (840x458px) が出る |
| 4096 | **画面ほぼ全面** (1260x688px = 52 桁×25 行の scale 3) が出る。720p ではこれ以上大きくする意味がない |
| 8192 / 16384 | 起動・表示とも問題なし (Linux 側の空きメモリは変わらない) |
| 32768 | **prudynt が起動しない** (プールは予約メモリ rmem 29MB から取られるため)。設定を戻せば復旧する |

上げすぎると、同じ予約メモリを使う映像バッファと取り合いになる。720p なら 2048〜4096 で足りる。
1080p では映像バッファが増える分だけ上限が下がるはずだが、値は測っていない。

