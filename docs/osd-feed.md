# osd-feed: OSD テキストを自動更新する常駐プログラム

[OSD テキストオーバーレイ](osd.md) の矩形に出す文字列を、カメラ上の常駐プログラム `osd-feed` が
**データ源 (source) → テンプレート → ファイル** の流れで書き続ける。まずはメモリの空き量、0→100% をループする
バー、時刻のサンプル。外部のデータ (HTTP で JSON を取る) も同じ仕組みで足せる。

```
osd-feed (Go、SD カードから実行)
  sources ─ それぞれ自分の間隔で取得して最新値を持つ ─┐
    meminfo  /proc/meminfo                               │  250ms ごとに描く
    tick     0→100 をループするカウンタ                  ├─▶ スロットごとの text/template
    clock    時刻                                        │      → cols×rows に切る → ASCII 以外を ? に
    http     URL を GET して JSON をそのまま値に          ┘      → 前回と違う時だけ tmp + mv で書く
```

- **言語は Go**。カメラにはインタプリタが無く、Mac で `go build` したバイナリ (静的、libc 非依存) がそのまま動く。
  外部データに必要な HTTP / JSON / タイムアウト / 並行取得が標準ライブラリで済むのが選んだ理由
- **バイナリ (約 8MB) と設定は SD カードに置く** (`/mnt/mmcblk0p1/osd-feed`、`osd-feed.json`)。内蔵フラッシュの
  overlay (空き 6MB 程度) には入らない。overlay に置くのは起動スクリプト `S94osd-feed` だけで、SD に両方あれば起動、
  無ければ何もしない
- 実測 (720p 配信中の Wyze Cam v3): 4 Hz 更新で CPU 0.8%、RSS 8MB で頭打ち (下の「計測」)
- prudynt との約束は [osd.md](osd.md) のとおり: tmpfs 上に一時ファイルを書いて `mv`。**フラッシュには何も書かない**
- 矩形の**有効化・位置・大きさは osd-feed の仕事ではない**。従来どおり SD の `prudynt-osd.json` か
  [Web UI](osd.md#web-ui-から編集する) で行う (有効になっていないスロットに書いても映らない。起動時のログに出る)
- Web UI の Show / Clear と同じファイルを書くので、osd-feed が動いている間は Show は使わない (すぐ上書きされる)

## インストール

```sh
device/osd-feed/build.sh                          # Mac/PC で。Go 1.25 以降。dist/osd-feed ができる
device/install-osd-feed.sh root@<camera-ip>       # SD にバイナリと (無ければ) 設定例、overlay に S94osd-feed を置いて起動
```

- 手で置くなら: `dist/osd-feed` → SD の `osd-feed` (実行属性は exFAT なので不要)、`device/osd-feed/osd-feed.json.example` →
  SD の `osd-feed.json`、`device/S94osd-feed` → `/etc/init.d/S94osd-feed` (chmod +x)
- 起動・停止: `service start osd-feed` / `service stop osd-feed` / `service disable osd-feed`。ログは `logread | grep osd-feed`
- 設定を変えたら `service restart osd-feed`。先に `/mnt/mmcblk0p1/osd-feed -once` を実行すると、各スロットの描画結果を
  標準出力に 1 回出して終わるので、テンプレートの確認に使える
- 消す: `service stop osd-feed; rm /etc/init.d/S94osd-feed /mnt/mmcblk0p1/osd-feed /mnt/mmcblk0p1/osd-feed.json`。
  終了時に書いていたファイル (`/run/prudynt/osd-text*`) は消す (`clear_on_exit`)

## 設定 (`osd-feed.json`)

```json
{
  "interval_ms": 250,
  "sources": {
    "mem":   { "type": "meminfo", "interval_ms": 1000 },
    "tick":  { "type": "tick",    "interval_ms": 100 },
    "clock": { "type": "clock",   "interval_ms": 1000 }
  },
  "slots": {
    "textfile": {
      "template": "MEM {{lpad 3 .mem.free_mb}}MB free  {{lpad 3 .mem.used_pct}}% used\n{{bar 30 .tick.pct}} {{lpad 3 .tick.pct}}%\n{{.clock.date}} {{.clock.hms}}"
    },
    "textfile2": { "template": "{{.clock.hms}}" }
  }
}
```

| キー | 意味 |
|---|---|
| `interval_ms` | 描く間隔 (既定 250、下限 100)。prudynt 側がファイルを見るのは 100ms ごとなので、それより短くしても速くならない |
| `clear_on_exit` | 終了時にスロットのファイルを消す (既定 true) |
| `sources.<名前>` | データ源。`type` と `interval_ms` (取得間隔、既定 1000)、type ごとの項目 (下表)。名前はテンプレートから `.<名前>.<キー>` で参照する |
| `slots.<スロット>` | `textfile` / `textfile2` / `textfile3`。`template` は Go の text/template。`path` / `cols` / `rows` は普通は書かない: 起動時と以後 30 秒ごとに prudynt から読んで追従する (Web UI で大きさを変えれば 30 秒以内に切る幅も変わる。prudynt が答えない時は docs/osd.md の既定値)。**ここに書いた値は prudynt の値より常に優先される**ので、Web UI で変えたのに反映されない時はここに古い値が残っていないか見る |

### source の種類

| `type` | 出す値 | 追加の設定 |
|---|---|---|
| `meminfo` | `free_kb` `free_mb` `total_mb` `used_pct` (buffers/cache は空きに数える。`free` コマンドと同じ) | — |
| `tick` | `n` (0→max をループ) `pct` (n の百分率) `max` | `step` (1 回の増分、既定 1)、`max` (既定 100)。`interval_ms` 100 で 10 秒で 1 周 |
| `clock` | `hms` `date` `unix` (カメラのローカル時刻。`/etc/TZ` の POSIX 形式を読む。DST の規則は無視) | — |
| `http` | GET した JSON オブジェクトのメンバーがそのままキー (入れ子もそのまま `.car.gps.lat`)。JSON でなければ `body` (文字列)。`status` は HTTP ステータス | `url` (必須)、`timeout_ms` (既定 3000)、`headers` (`{"Authorization": "..."}`) |

どの source にも **`ok`** (直前の取得が成功した) と **`age_s`** (最後に成功してからの秒数。一度も無ければ -1) が付く。
取得に失敗しても前回の値は残るので、外部データが途切れた時の見せ方はテンプレート側で決める:
`{{if .car.ok}}{{.car.speed}} km/h{{else}}-- km/h{{end}}`。失敗は種類が変わった時と復帰した時だけログに出る
(ファイルの書き込みエラーも同じ)。取得が `timeout_ms` を超えると打ち切られ、その間も他の source と描画は止まらない。

### テンプレートで使える関数

text/template 標準の `printf` `if` `range` などに加えて:

| 関数 | 例 | 結果 |
|---|---|---|
| `bar 幅 値` | `{{bar 30 .tick.pct}}` | `[#########---------------------]` (幅 30、値は 0〜100) |
| `lpad 桁 値` | `{{lpad 5 .mem.free_mb}}` | 右寄せ `   67` |
| `rpad 桁 値` | `{{rpad 8 .clock.hms}}` | 左寄せ |
| `trunc 桁 値` | `{{trunc 10 .car.name}}` | 先頭 10 文字 |

- 行数・桁数を超えた分は切る (prudynt も切るが、こちらで切っておくと何が映るか手元で分かる)。タブは空白、ASCII 以外は `?`
- source がまだ値を持っていないキーは空文字になる (`lpad` などの関数に渡しても同じ。`bar` は 0)。**設定に無い source をテンプレートが参照していると起動時にエラーで止まる** (綴り間違いをその場で見つけるため)
- `http` の JSON の数値は小数として入る (`{{printf "%.0f" .car.speed}}` で整数表示)。`bar` はそのまま受け付ける

### 外部データを足す

`http` source を書き足すだけで、テンプレートから参照できる:

```json
"car": { "type": "http", "url": "http://192.168.11.5/status.json", "interval_ms": 2000, "timeout_ms": 1500 }
```

新しい種類の source (シリアル、MQTT、…) は `device/osd-feed/source.go` の `Source` インタフェース
(`Poll(ctx) (Values, error)`) を実装して `sourceTypes` に登録し、`build.sh` で作り直す。ループ内で浮動小数点の計算を多用しない
(MIPS 向けはソフトウェア浮動小数点でビルドしている)。

## 計測

720p 配信中の Wyze Cam v3 (load average 1.6、prudynt + ffmpeg が動いている状態) で、設定例 (250ms 描画、tick は 100ms、
スロット 2 つ) を 15 分 + 追跡 5 分:

| 項目 | 値 |
|---|---|
| CPU | **0.8%** (15 分で 7.3 秒。その後も 1 分あたり約 0.5 秒で一定) |
| RSS | 起動直後 4.6MB → 15 分で **8.0MB** で頭打ち (その後 5 分で +8KB)。Go の GC がヒープを 2 倍まで育ててから止まる分で、リークではない |
| 書き込み | tick が変わる 100ms ごとの描画のうち、内容が変わった時だけ (実質 4 回/秒) |
| バイナリ | 8.3MB (net/http、text/template 込み。SD カード上) |

比較: 同じ処理を busybox sh で書くと CPU 1.4% (fork 分を除く) / RSS 0.5MB、C なら CPU・RSS ともこの数分の 1 になる見込み。
このカメラで問題になる量ではないと判断して Go にした (経緯は ISSUE #35)。
