# はじめる

カードを作ってカメラに入れ、最初の配信までの手順。仕組みや細かい注意は [sdcard.md](sdcard.md)、画面の説明は [lapcam.md](lapcam.md)。

## 必要なもの

| もの | 備考 |
|---|---|
| Wyze Cam v3 | 動作を確認しているのは Wi-Fi チップが ATBM6031、SoC が T31X の個体。カードのインストーラは機種を判定し、合わない個体では何もしない (純正のまま) |
| microSD カード | **FAT32** でフォーマット (exFAT ではインストールできない)。8〜32 GB 程度。相性で書き込みに失敗する型がある ([sdcard.md](sdcard.md#注意)) |
| 2.4 GHz の Wi-Fi | 自宅のルーター、モバイルルーター、スマホのテザリングなど。SSID とパスワード |
| スマホか PC | 操作はブラウザから。アプリは要らない |
| YouTube のストリームキー | YouTube Studio のライブ配信の設定にある。後から Web UI で入れてもよい |

## 1. カードを作る

[Releases](https://github.com/uzulla/lapcam/releases) から `lapcam-vX.Y.Z-sdcard.zip` を取り、展開して**中のフォルダの中身を**カードの直下に置く
(カードの直下に `README.txt` や `lapcam` が並ぶ状態)。

Wi-Fi の設定として、カードの直下に `wpa_supplicant.conf` を作る (改行は LF):

```text
network={
        ssid="自宅のWi-Fi"
        psk="パスワード"
        priority=10
}
network={
        ssid="ポケットWi-Fi"
        psk="パスワード"
}
```

複数書いておけば、現地では現地のものにつながる。書き方の細かいところは [wifi-from-sd.md](wifi-from-sd.md)。
ssh で入るなら、公開鍵を 1 行 1 鍵で `authorized_keys` に置いておく (起動のたびにカメラへ写される)。

リポジトリがある PC なら `firmware/make-sdcard.sh -w "SSID" "パスワード" /Volumes/カード` で同じものを作れる ([sdcard.md](sdcard.md#手順))。

## 2. カメラに入れる

**純正のままの Wyze Cam v3**: 電源を抜き、カードを挿し、電源を入れる。LED が赤・青・紫に回っている数分の間に、純正ファームウェアがカードへ退避され
(`WYZE_BACKUP_xxxxxx/`)、lapcam のファームウェアが書き込まれる。終わると勝手に再起動する。カードのインストーラは自分を `.old` に改名するので、
同じカードで二度は走らない。

**Thingino や lapcam が入っている個体**: `lapcam-vX.Y.Z-firmware.bin` (Releases にある。zip の中の `thingino-wyze_cam3_….bin` と同じもの) を
`autoupdate-full.bin` の名前でカードに置き、電源を入れ直す。正面の赤 LED が点いている 1〜2 分の間に書き込まれ、再起動する。
終わると `autoupdate-full.done` がカードに残り、以後は焼かない。設定はフラッシュの分 (ホスト名、Wi-Fi、パスワード) が消えるが、
カードの分は全部残る ([sdcard.md](sdcard.md#焼いた後に残るもの消えるもの))。

## 3. 開く

初回の起動は 1 分ほど。スマホを同じ Wi-Fi につなぎ、ブラウザで **`http://lapcam-xxxx.local/`** を開く。
xxxx はカメラごとの 4 桁で、ルーターの接続一覧や [`tools/find-camera`](find-camera.md) で分かる (名前で開けなければ IP で)。

ユーザー `root`、パスワード `root` でサインインすると、パスワードを変えるフォームが出るので、ここで自分のものにする
(ssh と共通。ファームウェアを焼くたびに `root` に戻る)。

- **Stream** タブでストリームキーを入れて Save。**Start streaming when the camera boots** を入れておくと、以後は電源を入れるだけで配信が始まる
- **Camera** タブの **Go live** で配信、**Record** で録画。映像の上下左右は **Picture** で反転できる
- **OSD** タブでロゴや時計の配置。ロゴはここから PNG をアップロードできる
- **Recordings** タブで録画の再生・ダウンロード・削除と、起動時に録画を始めるかの設定

Wi-Fi の設定を間違えてつながらない時は、本体のボタンを **5 秒押して離す**とカメラがパスワード無しの Wi-Fi `lapcam-xxxx` を出す。
それにつないで `http://100.64.1.1/` を開き、Wi-Fi タブで直して Reboot する ([lapcam.md](lapcam.md#つながらない時-本体ボタン-5-秒長押しでカメラが-wi-fi-ap-を出す-wifi-ap))。

## 更新する

新しい Release の zip から、`lapcam`、`osd-feed`、`beacon`、`ffmpeg`、`VERSION.txt` をカードに上書きし、
`lapcam-vX.Y.Z-firmware.bin` を `autoupdate-full.bin` の名前で置いて (古い `autoupdate-full.done` は消す)、電源を入れ直す。
設定ファイルはそのまま。Web UI の Camera タブの一番下に `lapcam vX · firmware vX` と出るので、両方が新しい版になっていれば完了。
片方だけ古いと赤く出る。

ssh が使える個体なら、カードにイメージを置いて `sysupgrade -x /mnt/mmcblk0p1/<イメージ>` でも焼ける (exFAT のカードでもよい)。

## 純正に戻す

カードの `WYZE_BACKUP_xxxxxx/combined_backup.bin` を `autoupdate-full.bin` の名前でカードに 1 つだけ置いて電源を入れる。
