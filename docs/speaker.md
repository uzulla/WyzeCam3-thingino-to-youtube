# スピーカー: 音声の再生と読み上げ

Wyze Cam v3 のスピーカーは、Thingino では **prudynt が鳴らす**。シェルからは `play` (音声ファイルを鳴らす) と
`tell` (文章を読み上げる) で使う。どちらも Thingino に最初から入っているもので、このリポジトリは変えていない。

## 仕組み

```text
tell "文章"                 (Setup ボタンの短押しは tell %ip)
  │ 1. 1.1.1.1 に ping してインターネットに繋がっているか確かめる
  │    繋がっていない -> 用意された音声 (設定用ポータルの案内) を鳴らして終わる
  │ 2. %ip -> "IP address is <デフォルトルートのインターフェースの IP>"、%time -> date の出力、それ以外はそのまま
  │ 3. curl で https://thingino.com/say2?q=<文章> を取得 -> 音声ファイル (opus) を /tmp に保存
  ▼
play <ファイル>
  │ prudynt の名前付きパイプ /run/prudynt/audio_out に "PLAY url=<ファイル> [vol=.. ...]" を 1 行書いて、すぐ終わる
  ▼
prudynt -> デコードしてスピーカーへ
```

- **音声合成は thingino.com のサービス**で行う (カメラに音声合成のエンジンは無い)。`tell` にはインターネットが必須で、
  読み上げる文章がそのまま thingino.com に送られる
- 鳴らすのは prudynt。`play` は依頼を書くだけなので、**prudynt が止まっている時は鳴らない** (パイプが 5 秒で現れなければ
  `play` はエラーで終わる)
- スピーカーの既定の音量・増幅は `/etc/prudynt.json` の `audio.spk_vol` (80) と `audio.spk_gain` (20)。
  `audio.spk_enabled` が `false` だと鳴らない
- `tell` は `play` の直後に音声ファイルを消す。`play` は再生の終わりを待たないので、prudynt がファイルを開く前に
  消えて鳴らない可能性がある (コードからの読み取り。実際には問題なく鳴っている可能性が高いが、未確認)

## `play`

```sh
play /usr/share/sounds/chime_1.opus
play -v 50 -l 3 -d 300 /usr/share/sounds/chime_2.opus   # 音量 50 で、0.3 秒あけて 3 回
play -A /mnt/mmcblk0p1/sounds/b.opus                     # 今の音に続けて鳴らす
play stop                                                # 止める
```

| オプション | 意味 |
|---|---|
| `-v 0〜100` | 音量 |
| `-g 0〜31` | 増幅 |
| `-l N` | N 回繰り返す (最大 32) |
| `-d ms` | 繰り返しの間隔 (最大 5000) |
| `-A` | **前の音に続けて鳴らす**。付けないと、待っている音 (キュー) を消してから鳴らす |
| `-f 形式` | 形式を指定 (`pcm` / `wav` / `aac` / `opus` / `mp3` / `flac`)。指定しなければ prudynt が判断する (判断の方法は未確認) |
| `-r Hz` | サンプリングレートを指定 |
| `-s` / `-` | 標準入力から読む (`curl ... \| play -s -f aac`) |

- ファイルの代わりに URL (`https://...`) も渡せる (`play` は URL のままを prudynt に渡す。prudynt 側で取得する。未確認)
- `/usr/share/sounds/` に用意された音声 (opus): `chime_1`〜`chime_3`、`wificonnected`、`wificonnectionfailed`、
  `configurationportal*`、`motiondetection*`、`videorecording*` など。インターネット無しで鳴らせる

## 音声を先に作っておく (オフラインで鳴らす)

`tell` はその場で thingino.com に音声を作ってもらうが、**音声ファイルを先に作って置いておけば、`play` で鳴らすだけ**になる。
インターネットも thingino.com も要らず、内容も声も自由に変えられる。

作り方の例:

```sh
# thingino.com で作る (tell と同じ声)
curl -o stream-started.opus --get --data-urlencode "q=Stream started" https://thingino.com/say2

# macOS の音声合成で作る (日本語の声も使える) -> ffmpeg で変換
say -v Kyoko -o stream-started.aiff "配信を開始しました"
ffmpeg -i stream-started.aiff -c:a libopus -b:a 32k stream-started.opus
```

- 置き場所は **SD カード** (例 `/mnt/mmcblk0p1/sounds/`) が向く。内蔵フラッシュの空き (6MB 程度) を使わず、PC で差し替えられる
- 鳴らす: `play /mnt/mmcblk0p1/sounds/stream-started.opus`。[Setup ボタン](setup-button.md) の割り当てや、
  状態が変わった時に呼ぶスクリプトから使える
- `tell` に任意の英文を渡すこともできる (`tell "Stream started"`)。日本語を thingino.com が読めるかは未確認
- **形式の相性は未確認** (サンプリングレート、チャンネル数、ビットレート)。作ったら最初に 1 つ鳴らして確かめる。
  迷ったら `/usr/share/sounds/` の opus と同じ形式に揃える (`ffprobe` で見られる)

### 変わる内容 (IP アドレスなど) を読み上げる

単語ごとのファイルを用意して、`play -A` で順に鳴らせばよい。たとえば「IP address is」「zero」〜「nine」「dot」の
12 個を作っておけば、IP の読み上げもオフラインでできる:

```sh
S=/mnt/mmcblk0p1/sounds
play "$S/ip-address-is.opus"
for c in $(echo 192.168.11.29 | sed 's/./& /g'); do
	case $c in .) play -A "$S/dot.opus" ;; *) play -A "$S/$c.opus" ;; esac
done
```

区切りが少し機械的に聞こえる可能性がある。

## 注意

- カメラのマイクがスピーカーの音を拾うので、**配信中に鳴らすと YouTube にも流れる** はず (未確認)
- 夜間や、人がいる場所に置いたカメラでは、音が出ること自体に注意

## 確認した範囲

実機 (`ciao+da40db6`) の `/usr/sbin/tell`、`/usr/sbin/play`、`/usr/share/prudynt-helpers`、`/etc/prudynt.json` の `audio`、
`/run/prudynt/` を読んで確認した。**実際に音を鳴らしての確認はしていない** (形式の相性、URL の再生、配信への混入、
日本語の読み上げは未確認)。
