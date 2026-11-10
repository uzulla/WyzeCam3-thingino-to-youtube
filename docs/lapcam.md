# lapcam: スマホ向けの Web UI (Thingino の Web UI を置き換える)

Thingino の Web UI は監視カメラ向けの機能が多く、このカメラの使い方 (YouTube に配信し、SD に録画し、
OSD を整える) には合わなくなってきたので、必要なものだけを持つ UI を**別のプログラムとして**作り直す。
サーキットでスマホから操作することを前提に、プレビューが主役の 1 画面で済むようにする。

機能: プレビュー、配信の開始・停止 (ストリームキーの切り替え、画質)、録画の開始・停止とファイルの再生・ダウンロード・削除、
プレビュー上での OSD の編集、映像の反転、Wi-Fi の設定、本体ボタンでの AP モード、再起動。Thingino の Web UI
(uhttpd、ポート 80/443) は lapcam が動いている間だけ止め、lapcam がポート 80 で答える (2026-11-09 にユーザーの判断で
置き換え。SD カードや本体が無くて lapcam が起動できない時は uhttpd がそのまま動く。戻し方は末尾)。

## 使う

```sh
device/lapcam/build.sh                      # PC で Go のクロスビルド -> dist/lapcam (約 8.6 MB、静的リンク)
device/install-lapcam.sh root@<camera-ip>   # SD カードにバイナリ、overlay に起動スクリプト S97lapcam を置いて起動。
                                           # 初回は SD に lapcam.args (-listen :80) を書く。起動後に答えるまで確かめる
```

スマホのブラウザで `http://<camera-ip>/` を開き、カメラの root パスワード (Thingino の Web UI や ssh と同じ)
でサインインする。ブラウザにパスワードを記憶させれば次回からは 1 タップ。サインインは 30 日残る (lapcam の再起動では消えない。カメラの再起動とパスワード変更で消える)。

画面は下のタブで切り替える (Camera / OSD / Stream / Recordings / Wi-Fi)。

| 画面 | できること |
|---|---|
| プレビュー (Camera) | 配信しているのと同じ映像 (OSD 込み)。既定は **Video** (prudynt の fMP4 を MediaSource で再生、配信と同じ fps でなめらか。Android と PC、新しめの iPhone)。右の **Video / Pictures** ボタンで MJPEG 4 fps の静止画連続に切り替えられ (どのブラウザでも動く、軽い)、Video が動かないブラウザでは自動で Pictures になる (カメラや Wi-Fi の一時的な不調は 3 秒後に 2 回まで Video をやり直してから)。選択はスマホに記憶。同時に見られるのは 3 台まで。他のタブや裏に回った時は止まる。その下の行に、配信中は LIVE と経過時間、録画中は REC と経過時間 (映像には重ねない) |
| Go live / Stop live | 配信 (youtube-relay) の開始・停止。停止は 3 秒以内にもう 1 回タップ (誤操作よけ) |
| Record / Stop recording | SD 録画の開始・停止 (Rec ボタンや本体ボタンと同じ `record` コマンド)。停止も 2 回タップ |
| Stream | エンコーダの fps と kb/s、Wi-Fi の送信 kb/s (全部の通信)、lapcam 起動後の ffmpeg 再起動回数。ストリームキーが未設定 (`youtube-relay.json` が無いか `stream_key` が空) の間、Go live ボタンは押せず「set a stream key in the Stream tab」と出る (自動起動で supervisor が待機していても「Stop」にはならない) |
| Profile | 今使っているストリームキーの名前、起動時の自動開始の有無 |
| Picture | 左右反転・上下反転の切り替え (prudynt に即時反映 + SD の `prudynt-osd.json` に保存) |
| Camera | ホスト名と IP、Wi-Fi (SSID と dBm)、SD の空き、稼働時間、負荷、空きメモリ。一番下に Reboot camera (2 回タップ)、Change password (今のパスワードと新しいパスワード 2 回。`/etc/shadow` の root を sha512-crypt で書き換える: この UI、ssh、Thingino の UI に共通。初期パスワード `root` でサインインした直後はこのフォームを自動で出す)、Sign out。その下に版 (`lapcam v2.0.0 · firmware v2.0.0`: SD 上のこのバイナリの版と、フラッシュの lapcam ファームウェアの版 `/etc/lapcam-release`。違えば赤で「update the card or the firmware」。土台の Thingino の版は `/api/status` の `versions.thingino` にだけ入れ、画面には出さない: [release.md](release.md))。サインイン画面の下にも lapcam の版 |
| OSD タブ | プレビューの上に 7 つの要素 (日時、テキスト 3 枠、ロゴ、G ボール、走行図) を枠で描き、**指でドラッグして配置** (端に寄せると 8 px の余白にスナップ、右・下に寄せた要素は右・下端からの距離で保存)。タップで選ぶと、表示の有無、文字の大きさ・桁・行、画像のサイズ (縦横比を保って 8 px ずつ)、テキスト枠の内容 (osd-feed のテンプレート、保存で osd-feed を再起動)、ロゴ PNG のアップロード (スマホの写真や画像から)、G ボール・走行図の有無を変えられる。非表示の要素は点線で出るので、そこから表示に戻せる |
| Recordings タブ | SD のクリップを日付ごとに一覧 (新しい順、最大 400 本)。Play でその場で再生 (`<video>`、Range 対応なのでシークできる)、Save でダウンロード、Delete は 2 回タップ (書き込み中のファイルは消せない)。チェックボックス (日付の見出しで一日まとめて) で選んで、まとめて削除 (2 回タップ) か **1 つの zip でダウンロード** (選んだ一覧を POST して短い id をもらい、`<a href>` でその id の zip を取る。圧縮はせず格納のみで、ファイルを順に流すのでメモリも一時ファイルも使わない。id は 15 分で消える)。上に録画の設定: 起動時に録画を始める、1 ファイルの長さ、カードがいっぱいになったら古いものから消す (上限 GB と最低空き MB)。設定は Thingino の Video Recorder ページと同じ `/etc/prudynt.json` の `recorder.*` に `jct` で書き、自動削除の設定を変えた時は `S95recordmgr` を再起動 |
| Stream タブ | ストリームキーを名前付きで複数保存し、使うものを選ぶ。Save で `youtube-relay.json` に書き、配信中なら再起動 (数秒切れる)。起動時に配信を始めるか (`service enable/disable youtube-relay`)。**Video quality**: 解像度 (1920×1080 / 1280×720 / 960×540 / 640×360)、fps (5〜30)、ビットレート、CBR/VBR を選んで Apply (2 回タップ)。`/etc/prudynt.json` の `stream0.*` に `jct` で書き (gop = fps×2)、prudynt を再起動する (osd-config / 旧 CGI と同じ `/run/prudynt-restart.lock` の手順。配信と録画が 10〜20 秒止まる)。配信側の最近のログ (ストリームキーは伏せる) |
| Wi-Fi タブ | 今の接続 (SSID、dBm、IP)、保存済みのネットワーク一覧 (優先度順。▲ で先頭へ、Remove)、Scan で見つかった SSID をタップしてパスワードを入れて追加、または手で入力 (スキャンの間は配信が一瞬途切れることがある)。Save and apply で `/etc/wpa_supplicant.conf` を書き直し (ヘッダ行と、network ブロックの他の行 (`key_mgmt=SAE`、`bssid` など) は残す。パスワードは既存のものを引き継ぐ。16 進表記の SSID や引用符を含む名前は 16 進のまま)、SD に `wpa_supplicant.conf` があればそれも同じ内容にし (wifi-from-sd が次の起動で適用するのと一致させる)、`wpa_cli reconfigure` で即時反映。**保存前につながっていたのに 30 秒たっても再接続しなければ、直前の設定に自動で戻す** (打ち間違いで操作不能にならない)。初回だけ元のファイルを `.before-lapcam` に残す。パスワードはブラウザに送らない。今つながっているネットワークを消す保存は 2 回タップで警告 |

## カメラの名前 (`set-camera-name.sh`)

```sh
device/set-camera-name.sh root@<camera-ip>          # 名前は lapcam。別名なら第 2 引数
```

Thingino が機体の名前から決めている 3 つを lapcam に揃える。/etc (overlay) への変更で、工場出荷状態に戻すと消える。

- **ホスト名**: 既定の `ing-wyze-cam3-xxxx` を `lapcam` に (`hostname`、`/etc/hostname`、`/etc/hosts`。Thingino の
  `S04hostname` は自分で付けた名前を起動時に残す)。LAN では `lapcam.local` で引ける (Android は引けない機種がある)
- **mDNS のサービス**: `/etc/mdns.d/lapcam.service` を置いて `_lapcam._tcp` を出す (Thingino 標準の `_thingino._tcp` は
  そのまま)。`tools/find-camera` はこれを探す ([find-camera.md](find-camera.md))
- **録画フォルダ**: 録画の保存先は既定で `%hostname` (ホスト名のフォルダ) なので、名前を lapcam にすると SD 直下の
  バイナリ `lapcam` とぶつかる。保存先を固定の `recordings` フォルダにし (`recorder.device_path`)、旧フォルダ
  (`ing-wyze-cam3-xxxx/`) の中身を `recordings/` へ移す (録画中なら移さずに知らせる)
- beacon と lapcam を再起動して新しい名前を読ませる。AP モードの SSID も `lapcam` (`wifi-ap`)

## つながらない時: 本体ボタン 5 秒長押しでカメラが Wi-Fi (AP) を出す (`wifi-ap`)

```sh
device/install-wifi-ap.sh root@<camera-ip>   # /usr/sbin/wifi-ap を置き、ボタンの 5 秒長押しの割り当てを変える
```

サーキットで保存済みの Wi-Fi が無い時のリカバリ用。本体のボタンを **5 秒押し続け (チャイムが鳴る) て離す**と、
カメラが**パスワード無しのアクセスポイント `lapcam`** になる。スマホをそれにつなぎ、`http://100.64.1.1/` を
開いて Wi-Fi タブで設定を直し (AP の間は保存だけで、適用は再起動時)、Camera 画面一番下の **Reboot camera** で再起動すると、
保存済みの Wi-Fi に戻る。
AP になっている間は Camera 画面の上に黄色い帯が出る。自動では AP にならず、AP から自動で戻ることもしない (ユーザーの決定)。

- Thingino の `S38wpa_supplicant` は設定ファイルに `mode=2` があると AP として起動する (100.64.1.1/24、DHCP、DNS)。
  `wifi-ap` はクライアントとして止めてから一時的に AP の設定を書いて起動し、直後に保存済みの設定ファイルを戻す
  (保存済みの設定は `/etc/wpa_supplicant.conf.station` にも残す)。なので再起動すれば元通り。/etc への書き込みは
  2 回で、その数秒の間に電源が落ちたら `.station` を手で戻す
- Thingino 既定の 5 秒長押しは「Wi-Fi 設定を消して再起動」(その後は Thingino の設定ポータル) で、保存した
  ネットワークが消える。インストーラはこの行を `wifi-ap` に差し替える (元の行はコメントで残す。20 秒の初期化はそのまま)
- AP にパスワードは無い (ユーザーの決定: リカバリ時にしか使わない)。その間は電波の届く範囲の誰でもつなげて通信も見える
- SD の `uenv.txt` に Thingino 標準の `wlan_ssid=` / `wlan_pass=` があると、Wi-Fi の起動処理がそれで設定を上書きして AP が
  立たないので、`wifi-ap` はその場合は何もせず音 ("portal is down") とログで知らせる。保存済みの設定を戻せなかった時も同じ
  (再起動すると AP のままになるので、`/etc/wpa_supplicant.conf.station` を手で戻す)
- 配信は AP の間は当然止まる (インターネットが無い)。再起動で戻る
- ログ: `logread | grep wifi-ap`。AP が上がらなかった時は "configuration portal is down" の音声が鳴る

## 仕組み

- Go の単体バイナリ (`device/lapcam/`)。beacon / osd-feed と同じく SD カードから起動し、`/etc/init.d/S97lapcam` だけ
  overlay に置く。SD が無ければ起動しない (その時は uhttpd = Thingino の UI がそのまま動く)。ポートは SD の `lapcam.args`
  (`-listen :80`)。`:80` の時、S97lapcam は起動の直前に uhttpd を止め、lapcam を止めた時に uhttpd を戻す。Thingino の UI を
  残したければインストール前に `-listen :8000` と書いておく。ポータル / AP モードでも起動する
- **何も自前では持たない**: 操作は既存のスクリプトと同じコマンド (`record`、`/etc/init.d/S93youtube-relay`、`service`、
  `prudyntctl json`)、設定は元の場所 (SD の `youtube-relay.json`、`prudynt-osd.json`)。lapcam 自身のファイルは
  SD の `lapcam.json` (ストリームキーのプロファイル一覧と、使っているもの) だけ。`/etc/prudynt.json` に書くのは
  Thingino のページが書いていたのと同じキー (画質の `stream0.*`、録画の `recorder.*`) だけで、書いた後は osd-config の
  メモを更新して「Web UI で反転を保存した」と誤認させない (`etcnote.go`)
- **通信は平文の HTTP**。LAN (テザリングや現地の Wi-Fi) の中で使う前提。パスワード無しの AP モードでは周囲から
  見えるので、AP の間はパスワードの入力を手短に。API への書き込みは同一サイトからの JSON (ロゴだけ multipart) に限り、
  他のサイトからブラウザ経由で操作させない (CSRF 対策)。ページは iframe に入れられない
- 認証: `/etc/shadow` の root の `$6$` (SHA-512 crypt) を Go で検証する (`sha512crypt.go`、仕様のテストベクタで確認)。
  間違えると 1.5 秒待たせ、同じアドレスから 1 分に 5 回間違えると以後は即 429。セッションのトークンは Cookie (HttpOnly) にだけあり、
  カメラ側は SHA-256 だけを持つ。`/run/lapcam-sessions.json` (0600、メモリ上の一時領域) に保存するので lapcam の更新や再起動では切れず、
  カメラの再起動では切れる。root のパスワードを変えると保存分は全部無効になる (ファイルにパスワードのダイジェストを添えて照合)
- プレビュー (Video): prudynt が `:8080/ch0.mp4` で出す fragmented MP4 (配信と同じ H.264 + AAC) を lapcam がそのまま中継し
  (`/api/preview.mp4`)、ページ側 (`static/video.js`、Thingino の preview-fmp4.js と同じ方式) が ftyp+moov からコーデックを
  読んで MediaSource に moof+mdat 単位で流し込む。最新から 3 秒以上遅れたら追いつき、再生済みは 20 秒だけ残す。
  iPhone は ManagedMediaSource (iOS 17.1 以降) を試し、無理なら Pictures に落ちる
- プレビュー (Pictures): prudynt が `:8080/mjpeg` で出す MJPEG (JPEG チャンネル、5 fps、1 枚 50 KB) を lapcam が中継し、
  `?fps=` (既定 4、最大 10) に間引く。`<img>` で映るのでどのスマホでも動く
- OSD: 要素の位置・大きさは `prudyntctl json` で即時に反映し、同じ値を SD の `prudynt-osd.json` の `osd.*` に書く
  (osd-config が再起動後に送り直す)。テキスト枠の枠の大きさは prudynt のビットマップフォント (1 文字 8×9 px、余白 2 px、
  × 倍率) から計算して描く。日時 (burnin) は位置が設定できないので動かせない (大きさと表示の有無だけ)。
  テンプレート・G ボール・走行図・ロゴの場所は SD の `osd-feed.json` を書き換えて `S94osd-feed restart`。
  ロゴは PNG を SD の `logo.png` (osd-feed.json の `slots.imagefile.png`) に置き換えるだけで、osd-feed が mtime で気付いて変換する
- 状態は lapcam の中で 2 秒ごとに 1 回だけ集め (`prudyntctl json` の stats、`/proc`、`wpa_cli`)、スマホが何台見ても
  コマンドは増えない。配信の ffmpeg は youtube-relay の子プロセスをコマンドラインで見分ける (json-youtube.cgi と同じ)

## テスト

`device/lapcam/build.sh` が `go vet` と `go test` を通してからビルドする (Mac だけで回る。カメラは要らない)。
テストにあるもの: SHA-512 crypt (仕様のテストベクタ)、`/etc/shadow` の読み方 (空ハッシュ・ロック・無いユーザー・`$6$` 以外)、
セッションの期限と延長、`wpa_supplicant.conf` の解析と書き戻し (16 進 SSID、SAE、引用符、`scan_ssid` の保持)、
スキャン結果の解析 (エスケープの復号、WPA3 の判定、重複の統合)、録画ファイルのパス正規化 (`safeRel`)、ストリームキーの
伏せ字、OSD と画質の入力検証、`/proc/<pid>/stat` の解析、プロファイル保存の検証と書き込み・壊れた `lapcam.json` からの
復帰、そして HTTP の表テスト (全 API が未サインインで 401、フォームや他サイトからの POST は 415 / 403、未知の API は
JSON の 404、ヘッダ、サインインの成否と Cookie、不明なフィールドや op は 400、prudynt 不在でスナップショットが 502)。
実機の挙動 (prudynt、init スクリプト、wpa_cli) に依存する部分はテストにせず、インストール時の起動確認と手での確認に任せる。

## 実測 (2026-11-09、`ciao+da40db6`)

- メモリ RSS 6.7 MB (待機) 〜 8.4 MB (プレビュー中)。CPU は待機で約 1%
- MJPEG を引いている間の prudynt の CPU 上乗せは約 1% (ハードウェア JPEG)
- API を一通り確認: サインイン (正しい / 間違い)、状態、プロファイル取得、スナップショット、プレビュー 5 秒で 21 枚 (4 fps。間引きを直す前は 12 枚だった)、4 台目の閲覧は 503、
  未サインインは 401、他サイトからの POST は 403、JSON でない POST は 415。OSD: 要素の取得、位置の変更が prudynt と SD の両方に入ること、奇数幅や日時の位置などの拒否、
  テンプレートの保存と osd-feed の再起動、ロゴ PNG のアップロードと PNG 以外の拒否。スマホでは Camera 画面を
  ユーザーが確認 (バッジの位置、上のバー、タブの大きさを直した)。OSD タブのドラッグはこれから

## 戻す (Thingino の Web UI に)

```sh
ssh root@<camera-ip> '/etc/init.d/S97lapcam stop; rm /etc/init.d/S97lapcam /mnt/mmcblk0p1/lapcam /mnt/mmcblk0p1/lapcam.args'
```

lapcam を止めた時点で uhttpd が起動し直す (S97lapcam の stop がそうする)。`lapcam.json` (プロファイル) は SD に残る。
Thingino の UI のファイル (`/var/www`) には何も変えていない。工場出荷状態に戻した場合も overlay が消えるので
Thingino の UI に戻る。

旧 Thingino UI 向けに作った自前ページ (`device/www/` の YouTube Live と OSD Settings、Rec ボタンの agent adapter) は
uhttpd が止まっているので使えないが、ファイルとインストーラは残してある (uhttpd を戻せば動く)。
