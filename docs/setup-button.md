# 本体の Setup ボタン

Wyze Cam v3 の Setup ボタンは、Thingino では起動した後もいつでも使える。常駐プログラム `thingino-button`
(`/etc/init.d/S15thingino-button`) がボタン (`/dev/input/event0` の `KEY_ENTER`) を見ていて、
**押していた長さ**で動作を変える。割り当ては `/etc/thingino-button.conf` (Thingino の既定のまま、このリポジトリは変えていない)。

## 既定の割り当て

| 押していた長さ | 押している間の合図 | 離した時の動作 |
|---|---|---|
| 0.1 秒未満 | — | 何もしない |
| **0.1 〜 5 秒** | 0.1 秒でチャイム (`chime_3`) | **IP アドレスをスピーカーで読み上げる** (「IP address is 192.168.11.29」) |
| **5 〜 20 秒** | 5 秒で別のチャイム (`chime_1`) | **Wi-Fi の設定を消して再起動** (`wlan reset; reboot`) |
| **20 秒以上** | 20 秒で別のチャイム (`chime_2`) | **工場出荷状態に戻す** (`firstboot -f`) |

- 実行されるのは、**離した時点で押していた時間以下の、いちばん長い割り当て 1 つだけ** (5 秒を過ぎてから離せば、
  IP の読み上げはせず Wi-Fi のリセットだけ)。チャイムは押している間にその秒数に達した時点で鳴るので、
  **鳴ったところで離せばその動作になる**。IP を聞きたいだけなら、短く押してすぐ離す
- 動作は `thingino-button` のソース ([gtxaspec/thingino-button](https://github.com/gtxaspec/thingino-button)、
  ファームが使う版 `aa1b572`) と実機の設定・スクリプトを読んで確認した。**実機でボタンを押しての確認はしていない**

## IP の読み上げ (短く押す)

[カメラの IP アドレスを調べる](find-camera.md) 方法の 1 つ。PC やスマホが無くても、カメラの前で IP が分かる。

- 読み上げる IP は、デフォルトルートのインターフェース (普通は `wlan0`) のアドレス
- 音声は **thingino.com の読み上げサービス** (`https://thingino.com/say2`) で作る (`/usr/sbin/tell`)。
  - **カメラがインターネットに繋がっている必要がある** (`1.1.1.1` への ping で判定)。繋がっていない時は IP を読み上げず、
    設定用ポータルについての案内の音声が流れる
  - 読み上げる文 (`IP address is <LAN の IP>`) が thingino.com に送られる
- 音は prudynt 経由で鳴らす (`/run/prudynt/audio_out`)。prudynt が止まっている時は鳴らない (動作自体は実行される)
- 読み上げと再生の仕組み、音声を先に作って置いておく方法は [speaker.md](speaker.md)
- カメラのマイクがスピーカーの音を拾うので、**配信中なら YouTube にも読み上げとチャイムが流れる** はず (未確認)

## Wi-Fi のリセット (5 秒)

`wlan reset` は `/overlay/etc/wpa_supplicant.conf` (保存されている Wi-Fi 設定) を消すだけ。再起動すると
ファームの既定の (未設定の) Wi-Fi 設定に戻り、カメラは設定用ポータル (カメラ自身が Wi-Fi のアクセスポイントになる) で起動する。
**SSH も Web UI も今の LAN からは届かなくなる。**

- [SD カードからの Wi-Fi 設定](wifi-from-sd.md) (`S37wifi-from-sd`) を使っていても、**SD の `wpa_supplicant.conf` を
  変えていなければ自動では入れ直さない**。最後に適用した内容の記録 (`/etc/wpa_supplicant.conf.sd-md5`) は消えずに残り、
  「SD の内容は適用済み」と判断するため (コードからの読み取り、未確認)。SD から入れ直したい時は、SD の
  `wpa_supplicant.conf` を少し変えて (コメント行を 1 行足す、など) 再起動する

## 工場出荷状態に戻す (20 秒)

`firstboot -f` は設定を保存しているフラッシュの領域 (overlay、`data` パーティション) と U-Boot の環境変数 (`env`) を消して再起動する。

- Thingino の設定 (Wi-Fi、Web UI のパスワード、`/etc/prudynt.json` など) がすべて初期値に戻る
- このリポジトリでカメラに入れたもの (`youtube-relay`、OSD 入りの prudynt、`osd-config`、Web UI のページ、
  `/etc/youtube-relay.json` のストリームキーなど) も消える。入れ直しは `device/install.sh` などから
- **SD カードの中身は消えない** (`youtube-relay.json`、`prudynt-osd.json`、`osd-feed` など)。osd-feed の起動スクリプト
  (`S94osd-feed`) は内蔵側にあるので消える

## 割り当てを変える

`/etc/thingino-button.conf` に 1 行 1 つ、`KEY_<キー名> <動作> <秒> <コマンド>` で書く:

| 動作 | いつ |
|---|---|
| `PRESS` | 押した瞬間 (秒は 0) |
| `RELEASE` | 離した瞬間 (秒は 0) |
| `TIMED` | 離した時、押していた時間がその秒数以上なら (同じキーの `TIMED` のうち、いちばん長いもの 1 つ) |
| `TIMED_FIRE` | 押している間、その秒数に達した時 (合図の音に使われている) |

例: 誤操作が怖いので Wi-Fi のリセットを外し、5 秒は配信の再起動にする:

```text
KEY_ENTER TIMED_FIRE 5 play /usr/share/sounds/chime_1.opus
KEY_ENTER TIMED 5 service restart youtube-relay
```

- 書き換えたら `/etc/init.d/S15thingino-button restart`
- `/etc` の変更はフラッシュ (overlay) に保存される。工場出荷状態に戻すと既定に戻る
- 20 秒の工場出荷状態に戻す割り当ても外せるが、Wi-Fi も SSH も届かなくなった時の最後の手段なので残しておく方がよい

## ダブルクリック・トリプルクリック

`thingino-button` 自体には押した回数を数える機能は無い (押す・離す・押していた長さだけ)。ただし割り当てた
コマンドは別プロセスで非同期に動くので、**回数を数えるスクリプトを挟めば実現できる** (未実装):

- 短く押した時の割り当て (`KEY_ENTER TIMED 0.1 tell %ip`) を、そのスクリプトの呼び出しに置き換える
- スクリプトは押されるたびに tmpfs のファイルに回数を足し、最後に押されてから 0.5 秒ほど次が来なければ、
  回数に応じたコマンドを実行する (1 回 = IP の読み上げ、2 回 = 配信の再起動、など)
- 0.1 秒のチャイム (`TIMED_FIRE 0.1`) は押すたびに鳴るので、押した回数の手応えとして残せる
- 1 回押しの動作は、次の押下を待つぶん 0.5 秒ほど遅れる
