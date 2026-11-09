# webui: スマホ向けの Web UI (Thingino の Web UI を置き換える)

Thingino の Web UI は監視カメラ向けの機能が多く、このカメラの使い方 (YouTube に配信し、SD に録画し、
OSD を整える) には合わなくなってきたので、必要なものだけを持つ UI を**別のプログラムとして**作り直す。
サーキットでスマホから操作することを前提に、プレビューが主役の 1 画面で済むようにする。

機能: プレビュー、配信の開始・停止 (ストリームキーの切り替え、画質)、録画の開始・停止とファイルの再生・ダウンロード・削除、
プレビュー上での OSD の編集、映像の反転、Wi-Fi の設定、本体ボタンでの AP モード、再起動。Thingino の Web UI
(uhttpd、ポート 80/443) は止めて無効にし、webui がポート 80 で答える (2026-11-09 にユーザーの判断で置き換え。
戻し方は末尾)。

## 使う

```sh
device/webui/build.sh                      # PC で Go のクロスビルド -> dist/webui (約 8.6 MB、静的リンク)
device/install-webui.sh root@<camera-ip>   # SD カードにバイナリ、overlay に起動スクリプト S97webui を置いて起動。
                                           # 初回は SD に webui.args (-listen :80) を書き、uhttpd を止めて無効にする
```

スマホのブラウザで `http://<camera-ip>/` を開き、カメラの root パスワード (Thingino の Web UI や ssh と同じ)
でサインインする。ブラウザにパスワードを記憶させれば次回からは 1 タップ。サインインは 30 日残る (webui の再起動で消える)。

画面は下のタブで切り替える (Camera / OSD / Stream)。

| 画面 | できること |
|---|---|
| プレビュー (Camera) | 配信しているのと同じ映像 (OSD 込み) を MJPEG で 4 fps。その下の行に、配信中は LIVE と経過時間、録画中は REC と経過時間 (映像には重ねない) |
| Go live / Stop live | 配信 (youtube-relay) の開始・停止。停止は 3 秒以内にもう 1 回タップ (誤操作よけ) |
| Record / Stop recording | SD 録画の開始・停止 (Rec ボタンや本体ボタンと同じ `record` コマンド)。停止も 2 回タップ |
| Stream | エンコーダの fps と kb/s、Wi-Fi の送信 kb/s (全部の通信)、webui 起動後の ffmpeg 再起動回数 |
| Profile | 今使っているストリームキーの名前、起動時の自動開始の有無 |
| Picture | 左右反転・上下反転の切り替え (prudynt に即時反映 + SD の `prudynt-osd.json` に保存) |
| Camera | ホスト名と IP、Wi-Fi (SSID と dBm)、SD の空き、稼働時間、負荷、空きメモリ。一番下に Reboot camera (2 回タップ) と Sign out |
| OSD タブ | プレビューの上に 7 つの要素 (日時、テキスト 3 枠、ロゴ、G ボール、走行図) を枠で描き、**指でドラッグして配置** (端に寄せると 8 px の余白にスナップ、右・下に寄せた要素は右・下端からの距離で保存)。タップで選ぶと、表示の有無、文字の大きさ・桁・行、画像のサイズ (縦横比を保って 8 px ずつ)、テキスト枠の内容 (osd-feed のテンプレート、保存で osd-feed を再起動)、ロゴ PNG のアップロード (スマホの写真や画像から)、G ボール・走行図の有無を変えられる。非表示の要素は点線で出るので、そこから表示に戻せる |
| Recordings タブ | SD のクリップを日付ごとに一覧 (新しい順、最大 400 本)。Play でその場で再生 (`<video>`、Range 対応なのでシークできる)、Save でダウンロード、Delete は 2 回タップ (書き込み中のファイルは消せない)。チェックボックス (日付の見出しで一日まとめて) で選んで、まとめて削除 (2 回タップ) か **1 つの zip でダウンロード** (選んだ一覧を POST して短い id をもらい、`<a href>` でその id の zip を取る。圧縮はせず格納のみで、ファイルを順に流すのでメモリも一時ファイルも使わない。id は 15 分で消える)。上に録画の設定: 起動時に録画を始める、1 ファイルの長さ、カードがいっぱいになったら古いものから消す (上限 GB と最低空き MB)。設定は Thingino の Video Recorder ページと同じ `/etc/prudynt.json` の `recorder.*` に `jct` で書き、自動削除の設定を変えた時は `S95recordmgr` を再起動 |
| Stream タブ | ストリームキーを名前付きで複数保存し、使うものを選ぶ。Save で `youtube-relay.json` に書き、配信中なら再起動 (数秒切れる)。起動時に配信を始めるか (`service enable/disable youtube-relay`)。**Video quality**: 解像度 (1920×1080 / 1280×720 / 960×540 / 640×360)、fps (5〜30)、ビットレート、CBR/VBR を選んで Apply (2 回タップ)。`/etc/prudynt.json` の `stream0.*` に `jct` で書き (gop = fps×2)、prudynt を再起動する (osd-config / 旧 CGI と同じ `/run/prudynt-restart.lock` の手順。配信と録画が 10〜20 秒止まる)。配信側の最近のログ (ストリームキーは伏せる) |
| Wi-Fi タブ | 今の接続 (SSID、dBm、IP)、保存済みのネットワーク一覧 (優先度順。▲ で先頭へ、Remove)、Scan で見つかった SSID をタップしてパスワードを入れて追加、または手で入力。Save and apply で `/etc/wpa_supplicant.conf` を書き直し (ヘッダ行は残し、network ブロックを作り直す。パスワードは既存のものを引き継ぐ)、SD に `wpa_supplicant.conf` があればそれも同じ内容にし (wifi-from-sd が次の起動で適用するのと一致させる)、`wpa_cli reconfigure` で即時反映。初回だけ元のファイルを `.before-webui` に残す。パスワードはブラウザに送らない。今つながっているネットワークを消す保存は 2 回タップで警告 |

## つながらない時: 本体ボタン 5 秒長押しでカメラが Wi-Fi (AP) を出す (`wifi-ap`)

```sh
device/install-wifi-ap.sh root@<camera-ip>   # /usr/sbin/wifi-ap を置き、ボタンの 5 秒長押しの割り当てを変える
```

サーキットで保存済みの Wi-Fi が無い時のリカバリ用。本体のボタンを **5 秒押し続け (チャイムが鳴る) て離す**と、
カメラが**パスワード無しのアクセスポイント `Wyze-cam`** になる。スマホをそれにつなぎ、`http://100.64.1.1:8000/` を
開いて Wi-Fi タブで設定を直し、Camera 画面一番下の **Reboot camera** で再起動すると、保存済みの Wi-Fi に戻る。
AP になっている間は Camera 画面の上に黄色い帯が出る。自動では AP にならず、AP から自動で戻ることもしない (ユーザーの決定)。

- Thingino の `S38wpa_supplicant` は設定ファイルに `mode=2` があると AP として起動する (100.64.1.1/24、DHCP、DNS)。
  `wifi-ap` はクライアントとして止めてから一時的に AP の設定を書いて起動し、直後に保存済みの設定ファイルを戻す。
  なので再起動すれば元通りで、フラッシュに残るものはない
- Thingino 既定の 5 秒長押しは「Wi-Fi 設定を消して再起動」(その後は Thingino の設定ポータル) で、保存した
  ネットワークが消える。インストーラはこの行を `wifi-ap` に差し替える (元の行はコメントで残す。20 秒の初期化はそのまま)
- 配信は AP の間は当然止まる (インターネットが無い)。再起動で戻る
- ログ: `logread | grep wifi-ap`。AP が上がらなかった時は "configuration portal is down" の音声が鳴る

## 仕組み

- Go の単体バイナリ (`device/webui/`)。beacon / osd-feed と同じく SD カードから起動し、`/etc/init.d/S97webui` だけ
  overlay に置く。SD が無ければ起動しない。ポートは SD の `webui.args` (`-listen :80`)。Thingino の UI を残したければ
  インストール前に `-listen :8000` と書いておく (その場合 uhttpd には触らない)。ポータル / AP モードでも起動する
- **何も自前では持たない**: 操作は既存のスクリプトと同じコマンド (`record`、`/etc/init.d/S93youtube-relay`、`service`、
  `prudyntctl json`)、設定は元の場所 (SD の `youtube-relay.json`、`prudynt-osd.json`)。webui 自身のファイルは
  SD の `webui.json` (ストリームキーのプロファイル一覧と、使っているもの) だけ。`/etc/prudynt.json` には書かない
- 認証: `/etc/shadow` の root の `$6$` (SHA-512 crypt) を Go で検証する (`sha512crypt.go`、仕様のテストベクタで確認)。
  間違えると 1.5 秒待たせ、同時に 1 件しか試せない。セッションはメモリ上のトークン (Cookie、HttpOnly)
- プレビュー: prudynt が `:8080/mjpeg` で出す MJPEG (JPEG チャンネル、5 fps、1 枚 50 KB) を webui が中継し、
  `?fps=` (既定 4、最大 10) に間引く。`<img>` で映るのでどのスマホでも動く。Android 向けのなめらかな動画 (fMP4) は後で
- OSD: 要素の位置・大きさは `prudyntctl json` で即時に反映し、同じ値を SD の `prudynt-osd.json` の `osd.*` に書く
  (osd-config が再起動後に送り直す)。テキスト枠の枠の大きさは prudynt のビットマップフォント (1 文字 8×9 px、余白 2 px、
  × 倍率) から計算して描く。日時 (burnin) は位置が設定できないので動かせない (大きさと表示の有無だけ)。
  テンプレート・G ボール・走行図・ロゴの場所は SD の `osd-feed.json` を書き換えて `S94osd-feed restart`。
  ロゴは PNG を SD の `logo.png` (osd-feed.json の `slots.imagefile.png`) に置き換えるだけで、osd-feed が mtime で気付いて変換する
- 状態は webui の中で 2 秒ごとに 1 回だけ集め (`prudyntctl json` の stats、`/proc`、`wpa_cli`)、スマホが何台見ても
  コマンドは増えない。配信の ffmpeg は youtube-relay の子プロセスをコマンドラインで見分ける (json-youtube.cgi と同じ)

## 実測 (2026-11-09、`ciao+da40db6`)

- メモリ RSS 6.7 MB (待機) 〜 8.4 MB (プレビュー中)。CPU は待機で約 1%
- MJPEG を引いている間の prudynt の CPU 上乗せは約 1% (ハードウェア JPEG)
- API を一通り確認: サインイン (正しい / 間違い)、状態、プロファイル取得、スナップショット、プレビュー 5 秒で 12 枚 (4 fps)、
  未サインインは 401。OSD: 要素の取得、位置の変更が prudynt と SD の両方に入ること、奇数幅や日時の位置などの拒否、
  テンプレートの保存と osd-feed の再起動、ロゴ PNG のアップロードと PNG 以外の拒否。スマホでは Camera 画面を
  ユーザーが確認 (バッジの位置、上のバー、タブの大きさを直した)。OSD タブのドラッグはこれから

## 戻す (Thingino の Web UI に)

```sh
ssh root@<camera-ip> '/etc/init.d/S97webui stop; rm /etc/init.d/S97webui /mnt/mmcblk0p1/webui /mnt/mmcblk0p1/webui.args; service enable uhttpd; /etc/init.d/S60uhttpd start'
```

`webui.json` (プロファイル) は SD に残る。Thingino の UI のファイル (`/var/www`) には何も変えていないので、uhttpd を
戻せばそのまま使える。工場出荷状態に戻した場合も overlay が消えるので Thingino の UI に戻る。

旧 Thingino UI 向けに作った自前ページ (`device/www/` の YouTube Live と OSD Settings、Rec ボタンの agent adapter) は
uhttpd が止まっているので使えないが、ファイルとインストーラは残してある (uhttpd を戻せば動く)。
