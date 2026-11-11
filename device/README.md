# device/ — カメラに入るファイル

カメラ側で動くものの置き場。ファームウェア ([docs/sdcard.md](../docs/sdcard.md)) は `firmware/prepare.sh` がここの
ファイルを Thingino の `user/<camera>/overlay/` に写してイメージに焼き込み、Go のプログラムは `firmware/make-sdcard.sh` が
SD カードに置く。**ここを変えたらイメージ (かカード) も作り直す。** 使い方は [docs/](../docs/) の各文書。

## 1. ファームウェアに焼き込むもの・SD カードに置くもの

| ファイル | 置き場所 (カメラ上) | 役割 | 文書 |
|---|---|---|---|
| `S37wifi-from-sd` | `/etc/init.d/` | 起動時に SD の `wpa_supplicant.conf` (複数の Wi-Fi 可) を適用する | [wifi-from-sd.md](../docs/wifi-from-sd.md) |
| `S37keys-from-sd` | `/etc/init.d/` | 起動時に SD の `authorized_keys` (SSH 公開鍵、1 行 1 鍵) を `/root/.ssh/authorized_keys` に写す (違う時だけ。カードが正)。焼くと `/root` が消えるため | [sdcard.md](../docs/sdcard.md) |
| `osd-config` / `S93osd-config` | `/usr/sbin/` / `/etc/init.d/` | OSD の設定を SD の `prudynt-osd.json` から読み、prudynt に送り直す (`general.osd_pool_size` だけは `/etc/prudynt.json` に書いて prudynt を再起動する) | [osd.md](../docs/osd.md) |
| `youtube-relay` / `S93youtube-relay` | `/usr/sbin/` / `/etc/init.d/` | 配信 supervisor (ffmpeg を監視・再起動) と起動スクリプト。設定は SD の `youtube-relay.json` | [relay.md](../docs/relay.md) |
| `S94osd-feed` | `/etc/init.d/` | SD の `osd-feed` と `osd-feed.json` があれば起動する | [osd-feed.md](../docs/osd-feed.md) |
| `S95beacon` | `/etc/init.d/` | SD の `beacon` があれば起動する | [beacon.md](../docs/beacon.md) |
| `rec-led` / `S96rec-led` | `/usr/sbin/` / `/etc/init.d/` | prudynt が SD に録画している間 (`/run/prudynt/mp4ctl-ch*.active` がある間) 正面の赤 LED を点ける常駐スクリプト | [recording.md](../docs/recording.md) |
| `S97lapcam` | `/etc/init.d/` | SD の `lapcam` を起動し、その間 uhttpd (Thingino の Web UI) を止める | [lapcam.md](../docs/lapcam.md) |
| `rec-button` | `/usr/sbin/` | 本体ボタンの押す・離すから、ダブルクリック (SD 録画の開始・停止) と 1 回押し (IP の読み上げ) を判定する。ボタン設定 `/etc/thingino-button.conf` は prepare.sh が Thingino のものを書き換えて作る | [recording.md](../docs/recording.md#本体のボタンのダブルクリックで開始停止-rec-button) |
| `wifi-ap` | `/usr/sbin/` | 本体ボタン 5 秒長押しで、再起動までカメラをパスワード無しの AP (名前はホスト名) にする | [lapcam.md](../docs/lapcam.md) |
| `agent-adapter` | `/usr/libexec/agent/adapter.sh` | 録画 (Rec) が使う agent API の prudynt 用バックエンド。Thingino のビルドは何もしない null 版を入れるので、録画引数を直した版 (`patches/thingino-agent-adapter-record-args.diff`) で上書きする | [recording.md](../docs/recording.md#thingino-の-agent-adapter-旧-ui-の-rec-ボタン用) |
| `lapcam/` | SD カード直下の `lapcam` (と `lapcam.args`) | スマホ向けの Web UI (Go、ポート 80)。プレビュー、配信・録画、OSD、Wi-Fi、パスワード変更。`build.sh` でビルド | [lapcam.md](../docs/lapcam.md) |
| `osd-feed/` | SD カード直下の `osd-feed` と `osd-feed.json` | メモリ量・時刻・カウンタ・HTTP JSON から OSD テキストを生成し、PNG を画像 OSD 用に変換する常駐プログラム (Go)。`build.sh` でビルド | [osd-feed.md](../docs/osd-feed.md) |
| `beacon/` | SD カード直下の `beacon` (と `beacon.args`) | IP・配信の状態・Wi-Fi の電波などを数秒ごとに UDP ブロードキャストし、LAN からの指令 (映像の反転) を受け付ける常駐プログラム (Go)。送り主は確かめない。`build.sh` でビルド | [beacon.md](../docs/beacon.md) |
| `youtube-relay.json.example` / `prudynt-osd.json.example` | (SD カード直下に置く設定の例) | 配信の設定 (ストリームキー等) と OSD の設定。カードの初期ファイルは `firmware/sdcard/` | [relay.md](../docs/relay.md) / [osd.md](../docs/osd.md) |

ホスト名 `lapcam-xxxx` (`S04hostname`)、mDNS の `_lapcam._tcp`、`/etc/lapcam-release`、タイムゾーン、`prudynt.json` /
`thingino.json` の既定、U-Boot の赤 LED は prepare.sh がその場で生成する (ここにファイルは無い)。SD から実行する
プログラム (lapcam、osd-feed、beacon、ffmpeg) は、動作中に SD カードを抜けない。

## 2. 素の Thingino に手で入れる (自前のファームウェアを焼かない場合)

`ciao+da40db6` の Thingino が入ったカメラに、上のものを ssh で 1 台ずつ入れるスクリプト。`/etc` や `/usr` は overlayfs で
フラッシュに残るので再起動後も残る (Thingino を入れ直す・更新すると消える)。すべて PC 側で `root@<camera-ip>` を引数に実行する。`common.sh` が `BUILD_ID` を
確かめ、`ciao+da40db6` でなければ何も変えずに中止する。

| ファイル | 入れるもの | 文書 |
|---|---|---|
| `install.sh` | ffmpeg、`youtube-relay` / `S93youtube-relay`、Thingino の Web UI の「YouTube Live」ページ (下記 `www/`)。再実行可能。Thingino の設定には触らない | [relay.md](../docs/relay.md) |
| `disable-netwatch.sh` | netwatch (ping 失敗で OS を再起動) の無効化。OS 設定の変更なので `install.sh` とは別 | [netwatch.md](../docs/netwatch.md) |
| `install-wifi-from-sd.sh` | `S37wifi-from-sd` | [wifi-from-sd.md](../docs/wifi-from-sd.md) |
| `install-keys-from-sd.sh` | `S37keys-from-sd` (引数の公開鍵をカードの `authorized_keys` に追加する) | [sdcard.md](../docs/sdcard.md) |
| `install-prudynt-osd.sh` | パッチ入り prudynt (テキスト 3 枠・画像 1 枠) を bind mount で差し替える `S30prudynt-osd`、`osd-config` / `S93osd-config`、`osd-progress-demo`、Thingino の Web UI の「OSD Settings」ページ (下記 `www/`) | [osd.md](../docs/osd.md) |
| `install-osd-feed.sh` | `osd-feed` (SD) と `S94osd-feed` | [osd-feed.md](../docs/osd-feed.md) |
| `install-agent-adapter.sh` | `agent-adapter` (元は `adapter.sh.null` に残す) | [recording.md](../docs/recording.md#thingino-の-agent-adapter-旧-ui-の-rec-ボタン用) |
| `install-rec-button.sh` | `rec-button` と `/etc/thingino-button.conf` の書き換え (元は `.orig` に残す) | [recording.md](../docs/recording.md#本体のボタンのダブルクリックで開始停止-rec-button) |
| `install-rec-led.sh` | `rec-led` / `S96rec-led` | [recording.md](../docs/recording.md) |
| `install-lapcam.sh` | `lapcam` (SD) と `S97lapcam`。uhttpd を止めて無効にする | [lapcam.md](../docs/lapcam.md) |
| `install-wifi-ap.sh` | `wifi-ap` と `/etc/thingino-button.conf` の 5 秒の行の差し替え | [lapcam.md](../docs/lapcam.md) |
| `install-beacon.sh` | `beacon` (SD) と `S95beacon` | [beacon.md](../docs/beacon.md) |
| `set-camera-name.sh` | ホスト名を `lapcam` に、mDNS に `_lapcam._tcp`、録画フォルダを `recordings` に固定 (ファームウェアでは `S04hostname` が `lapcam-xxxx` を付ける) | [lapcam.md](../docs/lapcam.md#カメラの名前) |

Thingino の Web UI 用のページ (lapcam に置き換えたので、ファームウェアには入れていない):

| ファイル | インストール先 | 役割 | 文書 |
|---|---|---|---|
| `www/youtube.html` / `www/x/json-youtube.cgi` / `www/a/plugins/youtube-relay.webui.json` | `/var/www/youtube.html` / `/var/www/x/json-youtube.cgi` / `/var/www/a/plugins/` | (`install.sh` が入れる) 「YouTube Live」ページと CGI。設定の編集、配信 / prudynt の再起動、サービスの開始・停止・自動起動。Services メニューに項目を足す (`/var/www/a/plugins.js` に 1 行追記) | [relay.md](../docs/relay.md#素の-thingino-に手で入れる場合) |
| `www/osd-settings.html` / `www/x/json-osd-text.cgi` | `/var/www/osd-settings.html` / `/var/www/x/json-osd-text.cgi` | (`install-prudynt-osd.sh` が入れる) 「OSD Settings」ページと CGI (テキストの書き込み、SD の `prudynt-osd.json` の保存)。Streamer メニューの「OSD Elements」の行き先をこのページに変える | [osd.md](../docs/osd.md#素の-thingino-に手で入れる場合) |
| `S30prudynt-osd` | `/etc/init.d/S30prudynt-osd` | (`install-prudynt-osd.sh` が入れる) パッチ入り prudynt を bind mount で標準のものと入れ替える。ファームウェアでは prudynt 自体にパッチが入っているので不要 | [osd.md](../docs/osd.md) |
| `osd-progress-demo` | `/usr/sbin/osd-progress-demo` | (`install-prudynt-osd.sh` が入れる) OSD テキストを書き換える側のサンプル (0.5 秒更新のプログレスバー) | [osd.md](../docs/osd.md) |
