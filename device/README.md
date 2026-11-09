# device/ — カメラへインストールするファイル

Thingino 実機に置くスクリプト一式。`/etc` や `/usr` は overlayfs でフラッシュに永続化されるため、
ファームウェアを書き換えずにインストールでき、再起動後も残る。使い方は [docs/](../docs/) の各文書。

| ファイル | インストール先 | 役割 | 文書 |
|---|---|---|---|
| `install.sh` | (PC 側で実行) | ffmpeg と配信 supervisor、Web UI のページをまとめて入れる。再実行可能。Thingino の設定には触らない (書き換えるのは `/var/www/a/plugins.js` のメニュー 1 行だけ) | [relay.md](../docs/relay.md) |
| `youtube-relay` / `S93youtube-relay` | `/usr/sbin/youtube-relay` / `/etc/init.d/S93youtube-relay` | 配信 supervisor (ffmpeg を監視・再起動) と起動スクリプト | [relay.md](../docs/relay.md) |
| `youtube-relay.json.example` | SD カード直下または `/etc/youtube-relay.json` | 配信の設定 (ストリームキー等) | [relay.md](../docs/relay.md) |
| `www/youtube.html` / `www/x/json-youtube.cgi` | `/var/www/youtube.html` / `/var/www/x/json-youtube.cgi` | (`install.sh` が入れる) Web UI の「YouTube Live」ページと CGI。設定の編集、配信 / prudynt の再起動、サービスの開始・停止・自動起動の切り替え。Services メニューに項目を足す (`plugins.js` に 1 行追記) | [relay.md](../docs/relay.md#web-ui) |
| `disable-netwatch.sh` | (PC 側で実行) | Thingino の netwatch (ping 失敗で OS を再起動) を無効化する。OS 設定の変更なので `install.sh` とは別 | [netwatch.md](../docs/netwatch.md) |
| `install-wifi-from-sd.sh` / `S37wifi-from-sd` | (PC 側で実行) / `/etc/init.d/S37wifi-from-sd` | 任意。SD の `wpa_supplicant.conf` (複数の Wi-Fi 可) を起動時に適用する。OS 設定を書き換えるので `install.sh` とは別 | [wifi-from-sd.md](../docs/wifi-from-sd.md) |
| `install-prudynt-osd.sh` / `S30prudynt-osd` | (PC 側で実行) / `/etc/init.d/S30prudynt-osd` | 任意。テキスト 3 枠・画像 1 枠を重ねられる prudynt (パッチ入り) を入れる。bind mount でストリーマを切り替え、標準の起動スクリプト `S31prudynt` の生存確認も調整するため、`install.sh` とは別 | [osd.md](../docs/osd.md) |
| `osd-config` / `S93osd-config` / `prudynt-osd.json.example` | `/usr/sbin/osd-config` / `/etc/init.d/S93osd-config` / SD カード直下または `/etc/prudynt-osd.json` | 任意 (`install-prudynt-osd.sh` が入れる)。OSD の設定を SD カードのファイルから読み、prudynt に送り直す (`general.osd_pool_size` だけは `/etc/prudynt.json` に書いて prudynt を再起動する) | [osd.md](../docs/osd.md) |
| `osd-progress-demo` | `/usr/sbin/osd-progress-demo` | (`install-prudynt-osd.sh` が入れる) OSD テキストを書き換える側のサンプル (0.5 秒更新のプログレスバー) | [osd.md](../docs/osd.md) |
| `www/osd-settings.html` / `www/x/json-osd-text.cgi` | `/var/www/osd-settings.html` / `/var/www/x/json-osd-text.cgi` | (`install-prudynt-osd.sh` が入れる) Thingino の Web UI に足す「OSD Settings」ページと、その CGI (テキストの書き込み、SD の `prudynt-osd.json` の保存)。Streamer メニューの「OSD Elements」の行き先を `/var/www/a/plugins.js` の書き換えでこのページに変える | [osd.md](../docs/osd.md#web-ui-から編集する) |
| `common.sh` | (PC 側の各スクリプトが読み込む) | 対応ファームの判定。カメラの `BUILD_ID` が `ciao+da40db6` でなければ何も変更せず中止する | — |
| `osd-feed/` / `install-osd-feed.sh` / `S94osd-feed` | (PC 側でビルド・実行) / SD カード直下の `osd-feed` と `osd-feed.json` / `/etc/init.d/S94osd-feed` | 任意。メモリ量・時刻・カウンタ・HTTP JSON から OSD テキストを生成し、PNG を画像 OSD 用に変換する常駐プログラム (Go)。バイナリと設定は SD カード、起動スクリプトだけ overlay。SD に両方あれば起動する。動作中の SD 抜去は不可 | [osd-feed.md](../docs/osd-feed.md) |
| `rec-led` / `S96rec-led` / `install-rec-led.sh` | `/usr/sbin/rec-led` / `/etc/init.d/S96rec-led` / (PC 側で実行) | 任意。prudynt が SD カードに録画している間 (`/run/prudynt/mp4ctl-ch*.active` がある間)、正面の赤 LED を点ける常駐スクリプト。青には触らない。停止で消灯 | [recording.md](../docs/recording.md) |
| `agent-adapter` / `install-agent-adapter.sh` | `/usr/libexec/agent/adapter.sh` / (PC 側で実行) | 任意。Web UI の「Rec ch0」ボタンが使う agent API の prudynt 用バックエンド。ファーム (`ciao+da40db6`) には何もしない null 版が入っていて、ボタンが "Failed to start recording" になる。同じソース木の prudynt 版 + 1 行修正 (`patches/thingino-agent-adapter-record-args.diff`) に差し替える。元は `adapter.sh.null` に残す | [recording.md](../docs/recording.md#web-ui-の-rec-ボタンを直す-install-agent-adaptersh) |
| `rec-button` / `install-rec-button.sh` | `/usr/sbin/rec-button` / (PC 側で実行) | 任意。本体のボタンのダブルクリックで SD 録画を開始・停止する。Thingino のボタンデーモンの設定 `/etc/thingino-button.conf` に押す・離すの行を足し、短押しの IP 読み上げを `rec-button` 側に移す (元は `.orig` に残す)。OS 設定の変更なので `install.sh` とは別 | [recording.md](../docs/recording.md#本体のボタンのダブルクリックで開始停止-rec-button) |
| `lapcam/` / `install-lapcam.sh` / `S97lapcam` | (PC 側でビルド・実行) / SD カード直下の `lapcam` (と任意の `lapcam.args`) / `/etc/init.d/S97lapcam` | スマホ向けの Web UI (Go)。Thingino の Web UI を置き換える。プレビュー (MJPEG の中継)、配信・録画の開始と停止、反転、状態。root パスワードでサインイン。既存のスクリプトと設定ファイルをそのまま使い、自前のファイルは SD の `lapcam.json` だけ。インストーラが uhttpd (Thingino の Web UI) を止めて無効にし、ポート 80 で動く | [lapcam.md](../docs/lapcam.md) |
| `wifi-ap` / `install-wifi-ap.sh` | `/usr/sbin/wifi-ap` / (PC 側で実行) | 任意。本体ボタンの 5 秒長押し (Thingino 既定は Wi-Fi 設定を消して再起動) を「再起動までカメラがパスワード無しの AP `lapcam` になる」に変える。保存した Wi-Fi は消えない。`/etc/thingino-button.conf` の 1 行を差し替える | [lapcam.md](../docs/lapcam.md) |
| `set-camera-name.sh` | (PC 側で実行) | 任意。ホスト名を `lapcam` に、mDNS に `_lapcam._tcp` を足し、録画フォルダを `recordings` に固定して旧フォルダを移す。OS 設定の変更なので `install.sh` とは別 | [lapcam.md](../docs/lapcam.md#カメラの名前-set-camera-namesh) |
| `beacon/` / `install-beacon.sh` / `S95beacon` | (PC 側でビルド・実行) / SD カード直下の `beacon` (と任意の `beacon.args`) / `/etc/init.d/S95beacon` | 任意。IP・配信の状態・Wi-Fi の電波などを数秒ごとに UDP ブロードキャストし、LAN からの指令 (映像の反転) を受け付ける常駐プログラム (Go)。送り主は確かめない。SD にバイナリがあれば起動する。動作中の SD 抜去は不可 | [beacon.md](../docs/beacon.md) |
