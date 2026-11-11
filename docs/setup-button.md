# 本体の Setup ボタン

Wyze Cam v3 の Setup ボタンは、Thingino では起動した後もいつでも使える。常駐プログラム `thingino-button`
(`/etc/init.d/S15thingino-button`) がボタン (`/dev/input/event0` の `KEY_ENTER`) を見ていて、
**押していた長さ**で動作を変える。割り当ては `/etc/thingino-button.conf`。lapcam のファームウェア ([sdcard.md](sdcard.md))
には、Thingino の既定を 2 か所変えたものが焼き込んである (短押しを `rec-button` に、5 秒長押しを `wifi-ap` に)。

## 割り当て (lapcam のファームウェア)

| 操作 | 押している間の合図 | 動作 |
|---|---|---|
| 0.1 秒未満 | — | 何もしない |
| **1 回押す** (0.1 〜 1 秒) | 0.1 秒でチャイム (`chime_3`) | 0.7 秒待って、**IP アドレスをスピーカーで読み上げる** (「IP address is 192.168.11.29」) |
| **ダブルクリック** (0.7 秒以内に 2 回) | 押すたびにチャイム | **SD カードへの録画を開始・停止** (`rec-button`。[recording.md](recording.md)) |
| **5 〜 20 秒** | 5 秒で別のチャイム (`chime_1`) | **再起動までカメラがパスワード無しの Wi-Fi AP (名前はホスト名 `lapcam-xxxx`) になる** (`wifi-ap`。[lapcam.md](lapcam.md#つながらない時-本体ボタン-5-秒長押しでカメラが-wi-fi-ap-を出す-wifi-ap)) |
| **20 秒以上** | 20 秒で別のチャイム (`chime_2`) | **工場出荷状態に戻す** (`firstboot -f`) |

- 実行されるのは、**離した時点で押していた時間以下の、いちばん長い割り当て 1 つだけ** (5 秒を過ぎてから離せば、
  IP の読み上げはせず AP だけ)。チャイムは押している間にその秒数に達した時点で鳴るので、
  **鳴ったところで離せばその動作になる**。IP を聞きたいだけなら、短く押してすぐ離す
- Thingino の既定は、短押しが直接 `tell %ip`、5 秒が **Wi-Fi の設定を消して再起動** (`wlan reset; reboot`。その後は Thingino の
  設定ポータル)。ファームウェアの設定ファイルでは元の行をコメントで残してある。素の Thingino に手で入れる場合は
  `device/install-rec-button.sh` と `device/install-wifi-ap.sh` が同じ書き換えをする
- 動作は `thingino-button` のソース ([gtxaspec/thingino-button](https://github.com/gtxaspec/thingino-button)、
  ファームが使う版 `aa1b572`) と実機の設定・スクリプトを読んで確認した。`rec-button` は ssh から押す・離すを模擬して確認した。
  **実機でボタンを指で押しての確認はしていない**

## IP の読み上げ (1 回押す)

[カメラの IP アドレスを調べる](find-camera.md) 方法の 1 つ。PC やスマホが無くても、カメラの前で IP が分かる。

- 読み上げる IP は、デフォルトルートのインターフェース (普通は `wlan0`) のアドレス
- 音声は **thingino.com の読み上げサービス** (`https://thingino.com/say2`) で作る (`/usr/sbin/tell`)。
  - **カメラがインターネットに繋がっている必要がある** (`1.1.1.1` への ping で判定)。繋がっていない時は IP を読み上げず、
    設定用ポータルについての案内の音声が流れる
  - 読み上げる文 (`IP address is <LAN の IP>`) が thingino.com に送られる
- 音は prudynt 経由で鳴らす (`/run/prudynt/audio_out`)。prudynt が止まっている時は鳴らない (動作自体は実行される)
- 読み上げと再生の仕組み、音声を先に作って置いておく方法は [speaker.md](speaker.md)
- カメラのマイクがスピーカーの音を拾うので、**配信中なら YouTube にも読み上げとチャイムが流れる** はず (未確認)

## ダブルクリック (`rec-button`)

`thingino-button` 自体には押した回数を数える機能は無い (押す・離す・押していた長さだけ)。そこで `KEY_ENTER PRESS` /
`RELEASE` の行で押す・離すのたびに `/usr/sbin/rec-button` を呼び、`/run/rec-button/` の時刻で判定する。離してから
0.7 秒以内にもう一度離したらダブルクリックで録画の開始・停止 (Thingino の「video recording started / stopped」の音声が鳴る)、
0.7 秒待っても次が来なければ 1 回押しとして `tell %ip`。1 秒以上押していた離し方はクリックに数えず、`TIMED` の行に任せる。
詳細とログの見方は [recording.md](recording.md#本体のボタンのダブルクリックで開始停止-rec-button)。

## Wi-Fi AP (5 秒)

保存した Wi-Fi に繋がらない時のリカバリ用。保存した Wi-Fi は消えず、スマホを `lapcam-xxxx` に繋いで `http://100.64.1.1/`
を開くと lapcam の Wi-Fi タブで直せる。自動では AP にならず、AP から自動で戻ることもしない (再起動で戻る。ユーザーの決定)。
詳細 (仕組み、`uenv.txt` に Wi-Fi があると AP が立たないこと、失敗時の音) は [lapcam.md](lapcam.md#つながらない時-本体ボタン-5-秒長押しでカメラが-wi-fi-ap-を出す-wifi-ap)。

素の Thingino の既定 `wlan reset` は `/overlay/etc/wpa_supplicant.conf` を消し、再起動後は設定用ポータルになる
(SSH も Web UI も今の LAN からは届かなくなる)。その時 [SD カードの `wpa_supplicant.conf`](wifi-from-sd.md) は、内容を
変えていなければ自動では入れ直さない (最後に適用した内容の記録 `/etc/wpa_supplicant.conf.sd-md5` が残り、「適用済み」と判断する。
コードからの読み取り、未確認)。SD から入れ直したい時は、SD の `wpa_supplicant.conf` を少し変えて (コメント行を 1 行足す、など) 再起動する。

## 工場出荷状態に戻す (20 秒)

`firstboot -f` は設定を保存しているフラッシュの領域 (overlay、`data` パーティション) と U-Boot の環境変数 (`env`) を消して再起動する。

- Thingino の設定 (Wi-Fi、root のパスワード、`/etc/prudynt.json` など) がすべて初期値に戻る
- ファームウェアの個体では、データ領域はイメージの overlay (起動スクリプト、ボタン設定、ホスト名、`/etc/lapcam-release`、
  mDNS の設定) をそのまま焼いたものなので、それらも消えると考えられる (未確認)。U-Boot 環境変数の赤 LED (`gpio_user`) も消える。
  戻すにはカードの `autoupdate-full.bin` で焼き直す (`autoupdate-full.done` を消してから。[sdcard.md](sdcard.md))
- **SD カードの中身は消えない** (`lapcam`、`osd-feed`、`beacon`、`ffmpeg`、`youtube-relay.json`、`prudynt-osd.json`、
  `wpa_supplicant.conf`、`authorized_keys`、録画など)
- 素の Thingino に手で入れた個体は `device/install*.sh` から入れ直す

## 割り当てを変える

`/etc/thingino-button.conf` に 1 行 1 つ、`KEY_<キー名> <動作> <秒> <コマンド>` で書く:

| 動作 | いつ |
|---|---|
| `PRESS` | 押した瞬間 (秒は 0) |
| `RELEASE` | 離した瞬間 (秒は 0) |
| `TIMED` | 離した時、押していた時間がその秒数以上なら (同じキーの `TIMED` のうち、いちばん長いもの 1 つ) |
| `TIMED_FIRE` | 押している間、その秒数に達した時 (合図の音に使われている) |

例: 5 秒を AP ではなく配信の再起動にする (`wifi-ap` の行を差し替える):

```text
KEY_ENTER TIMED_FIRE 5 play /usr/share/sounds/chime_1.opus
KEY_ENTER TIMED 5 service restart youtube-relay
```

- 書き換えたら `/etc/init.d/S15thingino-button restart`
- `/etc` の変更はフラッシュ (overlay) に保存される。工場出荷状態に戻すと既定に戻る
- 20 秒の工場出荷状態に戻す割り当ても外せるが、Wi-Fi も SSH も届かなくなった時の最後の手段なので残しておく方がよい
