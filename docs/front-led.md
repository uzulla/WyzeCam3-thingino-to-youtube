# 正面の LED

Wyze Cam v3 の正面の状態表示 LED は、Thingino では**赤と青の 2 つの LED** として見える
(`/sys/class/leds/led_r`、`/sys/class/leds/led_b`)。Thingino の既定では起動後は消えたまま
(起動スクリプト `S99led` がトリガーを外し、`/etc/thingino.json` の `led.startup_indicator` が `off`)。
このリポジトリのスクリプトは LED を使っていない。

夜間撮影用の赤外線 LED はこれとは別 (下記)。

## コマンド (Thingino に最初から入っている)

| やりたいこと | コマンド |
|---|---|
| 点ける | `led r` (赤)、`led b` (青)、`led m` または `led p` (赤と青の両方 = 紫) |
| 消す | `led off` |
| 点滅させる | `blink r 0.5` (0.5 秒間隔)、止める: `blink r stop` |

- `led` は色を LED の組み合わせに置き換えて (`y` = 赤 + 緑、`c` = 緑 + 青、`m` / `p` = 赤 + 青、`w` = 3 色)、
  その LED を点けて残りを消す。このカメラには緑が無いので、出せるのは赤・青・紫だけ (`y` は赤だけ点いて警告が出る)
- ヘルプには `v` (violet) も載っているが、スクリプトに対応する組み合わせが無く、使えない (`Unsupported color` で終わる)
- `blink` はカーネルの `timer` トリガーを設定して、すぐ終わる (常駐しない。点滅はカーネルが続ける)

## sysfs で直接

```sh
echo 255 > /sys/class/leds/led_b/brightness    # 点ける (0 で消す)
cat /sys/class/leds/led_b/trigger              # 選べるトリガー。[ ] が今のもの
echo mmc0 > /sys/class/leds/led_r/trigger      # SD カードへのアクセスで光らせる
echo none > /sys/class/leds/led_r/trigger      # 連動をやめる
```

| トリガー | 光る時 |
|---|---|
| `none` | 連動しない (`brightness` で決める。既定) |
| `timer` | 点滅 (`delay_on` / `delay_off` に ms。`blink` が使う) |
| `mmc0` / `mmc1` | SD カード (`mmc0`) などへのアクセス |
| `rfkill0` | 無線の有効/無効 |
| `nand-disk` | NAND へのアクセス (このカメラは SPI NOR なので、おそらく光らない) |

- `max_brightness` は 255 だが、明るさを段階的に変えられるかは未確認 (オンかオフの 2 段階の可能性が高い)
- トリガーを設定した後に `brightness` を書くと、トリガーは外れる (カーネルの LED の仕様)

## 起動時に点ける

`/etc/thingino.json` の `led.startup_indicator` に色の文字を書くと、起動時 (`S99led`) にその色で点く:

```sh
jct /etc/thingino.json set led.startup_indicator b    # 青。off で点けない
```

`/etc` の変更なのでフラッシュに保存される (工場出荷状態に戻すと既定の `off` に戻る)。

## 夜間撮影用の赤外線 LED

正面の状態表示 LED とは別物で、`light` で操作する (普段は昼夜の切り替え `daynightd` が自動で操作している):

```sh
light ir read     # 今の状態
light ir on       # 点ける (off / toggle)
```

`light` の種類は `ir` / `ir850` / `ir940` / `white` / `flood` (機種による)。手で変えても、`daynightd` が
次の昼夜の判定で上書きする可能性がある (未確認)。

## 赤と青は独立に操作できる

- 2 つの LED はそれぞれ別に、消灯・点灯・点滅を指定できる。点滅の間隔 (`delay_on` / `delay_off`) も LED ごとに別
  (例: 赤は点灯のまま、青だけ 0.5 秒で点滅)
- 同時に点けると紫 (`led m`)。片方を点灯、もう片方を点滅にもできる
- 本体の正面に光る窓が 1 つ (1 つの窓に 2 色) か 2 つかは未確認。1 つの窓に 2 色が入っているなら、同時に点けた時は
  紫に混ざって見え、「赤の点灯 + 青の点滅」のような組み合わせは見分けにくいはず
- 状態の表示に使うには、点け消しするプログラム (配信の状態を見る `youtube-relay` など) の側に、`led` / `blink` を呼ぶ処理を足す

## 確認した範囲

実機 (`ciao+da40db6`) で `/sys/class/leds` の中身と各トリガー、`led` / `blink` / `light` / `S99led` のスクリプトを読んで確認した。
**LED を実際に点けての確認はしていない** (色の見え方、明るさの段階、赤外線 LED と `daynightd` の関係は未確認)。
