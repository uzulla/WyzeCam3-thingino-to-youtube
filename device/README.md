# device/ — カメラへインストールするファイル

Thingino 実機に置くスクリプト一式。`/etc` や `/usr` は overlayfs でフラッシュに永続化されるため、
ファームウェアを書き換えずにインストールでき、再起動後も残る。使い方は [docs/](../docs/) の各文書。

| ファイル | インストール先 | 役割 | 文書 |
|---|---|---|---|
| `install.sh` | (PC 側で実行) | ffmpeg と配信 supervisor をまとめて入れる。再実行可能。自前のファイルを置くだけで、Thingino の設定には触らない | [relay.md](../docs/relay.md) |
| `youtube-relay` / `S93youtube-relay` | `/usr/sbin/youtube-relay` / `/etc/init.d/S93youtube-relay` | 配信 supervisor (ffmpeg を監視・再起動) と起動スクリプト | [relay.md](../docs/relay.md) |
| `youtube-relay.json.example` | SD カード直下または `/etc/youtube-relay.json` | 配信の設定 (ストリームキー等) | [relay.md](../docs/relay.md) |
| `www/youtube.html` / `www/x/json-youtube.cgi` | `/var/www/youtube.html` / `/var/www/x/json-youtube.cgi` | (`install.sh` が入れる) Web UI の「YouTube Live」ページと CGI。設定の編集、配信 / prudynt の再起動、サービスの開始・停止・自動起動の切り替え。Services メニューに項目を足す (`plugins.js` に 1 行追記) | [relay.md](../docs/relay.md#web-ui) |
| `disable-netwatch.sh` | (PC 側で実行) | Thingino の netwatch (ping 失敗で OS を再起動) を無効化する。OS 設定の変更なので `install.sh` とは別 | [netwatch.md](../docs/netwatch.md) |
| `install-wifi-from-sd.sh` / `S37wifi-from-sd` | (PC 側で実行) / `/etc/init.d/S37wifi-from-sd` | 任意。SD の `wpa_supplicant.conf` (複数の Wi-Fi 可) を起動時に適用する。OS 設定を書き換えるので `install.sh` とは別 | [wifi-from-sd.md](../docs/wifi-from-sd.md) |
| `install-prudynt-osd.sh` / `S30prudynt-osd` | (PC 側で実行) / `/etc/init.d/S30prudynt-osd` | 任意。映像に任意のテキストを重ねられる prudynt (パッチ入り) を入れる。Thingino のストリーマを差し替えるので `install.sh` とは別 | [osd.md](../docs/osd.md) |
| `osd-config` / `S93osd-config` / `prudynt-osd.json.example` | `/usr/sbin/osd-config` / `/etc/init.d/S93osd-config` / SD カード直下または `/etc/prudynt-osd.json` | 任意 (`install-prudynt-osd.sh` が入れる)。OSD の設定を SD カードのファイルから読み、prudynt に送り直す (`general.osd_pool_size` だけは `/etc/prudynt.json` に書いて prudynt を再起動する) | [osd.md](../docs/osd.md) |
| `osd-progress-demo` | `/usr/sbin/osd-progress-demo` | (`install-prudynt-osd.sh` が入れる) OSD テキストを書き換える側のサンプル (0.5 秒更新のプログレスバー) | [osd.md](../docs/osd.md) |
| `www/osd-text.html` / `www/x/json-osd-text.cgi` | `/var/www/osd-text.html` / `/var/www/x/json-osd-text.cgi` | (`install-prudynt-osd.sh` が入れる) Thingino の Web UI に足す「OSD text」ページと、その CGI (テキストの書き込み、SD の `prudynt-osd.json` の保存)。Streamer メニューの「OSD Elements」の行き先を `/var/www/a/plugins.js` の書き換えでこのページに変える | [osd.md](../docs/osd.md#web-ui-から編集する) |
| `common.sh` | (PC 側の各スクリプトが読み込む) | 対応ファームの判定。カメラの `BUILD_ID` が `ciao+da40db6` でなければ何も変更せず中止する | — |
| `osd-feed/` / `install-osd-feed.sh` / `S94osd-feed` | (PC 側でビルド・実行) / SD カード直下の `osd-feed` と `osd-feed.json` / `/etc/init.d/S94osd-feed` | 任意。OSD テキストの中身を書き続ける常駐プログラム (Go)。バイナリと設定は SD カード、起動スクリプトだけ overlay。SD に両方あれば起動する | [osd-feed.md](../docs/osd-feed.md) |
