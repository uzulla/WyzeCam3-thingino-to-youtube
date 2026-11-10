# SD カードからインストールする (firmware/)

素の Wyze Cam v3 に、SD カードを挿して電源を入れるだけで lapcam 一式を入れる仕組み。
Thingino 導入済みの個体 (lapcam 入りも含む) を同じカードで書き換えることもできる。

```text
firmware/prepare.sh      Thingino のソース木にパッチと user/<camera>/ (焼き込む設定) を用意する
firmware/build.sh        ビルドホストでイメージを作る (prepare.sh を呼ぶ) → dist/firmware/
firmware/make-sdcard.sh  PC で SD カードに一式を書く
firmware/fatextract.py   make-sdcard.sh が使う。公式インストーラの SD イメージから 1 ファイルを取り出す
firmware/sdcard/         カードに置く雛形 (OSD の設定、README.txt)
firmware/fatextract_test.py, firmware/make-sdcard_test.sh   テスト (下記)
```

## しくみ

Wyze Cam v3 には 2 つの「SD から入る」経路があり、どちらも Thingino 側にもとからある。

- **純正ブートローダー**は起動時に SD の `factory_t31_ZMC6tiIDQN` を読み込んで実行する。
  [thingino-installers](https://github.com/wltechblog/thingino-installers) の Wyze Cam v3 用インストーラはこれを使う:
  小さな Linux (initramfs) が機種を判定し (SoC と Wi-Fi チップ: ATBM6031 は `0x007a`)、
  SD 上の `thingino-<機種名>.bin` を選び、純正フラッシュを `WYZE_BACKUP_xxxxxx/combined_backup.bin` に
  退避してから、パーティションごとに書き込む。終わると `factory_t31_ZMC6tiIDQN` を `.old` に改名し (二度は走らない)、再起動する。
  カードは **FAT32** でないと読めない。
- **Thingino の U-Boot** は起動時に SD の `autoupdate-full.bin` があれば全フラッシュを書き換え、
  `autoupdate-full.done` を残して再起動する (`enable_updates=true` が既定。`.done` があれば何もしない)。
  こちらも FAT のみ。

lapcam が作るのは、この 2 経路に乗せる **自前のイメージ** `thingino-wyze_cam3_t31x_gc2053_atbm6031.bin`
(16 MB、フラッシュ全体: U-Boot、環境変数、カーネル、rootfs、データ領域) と、**SD カードの中身一式**。

### イメージに焼き込むもの (`firmware/prepare.sh`)

Thingino のビルドには `user/<camera>/` という「ユーザー層」があり、ビルド時にイメージへ混ぜられる
(ソース木の `docs/build/local-build-settings.md`)。prepare.sh はそこに次を書く。`device/` のファイルが元で、
`device/install-*.sh` が実機に手で入れるものと同じ。

| 置き場所 | 中身 |
|---|---|
| `local.fragment` | prudynt のフル ASCII 8x8 フォント (`BR2_PACKAGE_PRUDYNT_T_OSD_FONT_8X8=y`) |
| `prudynt.json` | `/etc/prudynt.json` の既定への上書き: stream0 を 1280x720 / 10 fps / gop 20 / 1 Mbps ([recording.md](recording.md) の実測条件)、`osd_pool_size` 2048、録画先 SD の `recordings`、JPEG チャンネル 5 fps 品質 60 |
| `thingino.json` | `netwatch.enabled=false` ([netwatch.md](netwatch.md))、`daynight.enabled=false` + `force_mode=day` (昼夜の自動切り替えを止めて昼に固定。車内では IR LED はガラスに反射するだけで、トンネルや日陰で白黒に切り替わると配信の見た目が途中で変わる。daynightd 自体は眠ったまま残す: 起動スクリプト S10daynightd が prudynt / agent の API キーを初回起動で作るので止めない。無効時の CPU は 0%) |
| `local.uenv.txt` | U-Boot 環境変数 `gpio_user=38o`: U-Boot が動いている間は正面の赤 LED を点ける (カーネルが起動すると消える)。通常起動なら 2 秒、SD からの書き換え中は 1〜2 分点き続けるので、焼いている最中だと分かる |
| `overlay/` | データ領域 (jffs2、起動後の `/overlay`) の初期内容。`/etc/lapcam-release` (版: [release.md](release.md))、起動スクリプト S37wifi-from-sd / S37keys-from-sd / S93osd-config / S93youtube-relay / S94osd-feed / S95beacon / S96rec-led / S97lapcam、`/usr/sbin` の osd-config / youtube-relay / rec-led / rec-button / wifi-ap、ボタン設定 (ダブルクリックで録画、5 秒で AP)、agent adapter (録画引数の修正入り)、ホスト名 `lapcam` と `/etc/hosts`、mDNS の `_lapcam._tcp`、タイムゾーン (既定 Asia/Tokyo、`LAPCAM_TIMEZONE` / `LAPCAM_TZ` で変更) |

パッチは [build.md](build.md) で手で当てていたもの (ffmpeg の RTMPS と chunk size、prudynt の OSD と警告抑止、
agent adapter の引数順) に、イメージ用の 1 本 (`patches/thingino-wifi-portal-ssid.diff`: Wi-Fi 未設定時に開く
設定用 AP の名前を `THINGINO-xxxx` から `lapcam-xxxx` に) を足して prepare.sh が当てる。新たに当たった (または中身が変わった)
パッチのパッケージと、`prudynt.json` が変わったときの prudynt-t は `user/<camera>/.rebuild` に記録され、build.sh が
`make rebuild-<pkg>` する (Buildroot はパッケージディレクトリのファイル変更を見ないし、`prudynt.json` は prudynt-t の
インストール時にしか混ぜられないため)。ソース木は `da40db6` にあることを前提にし、違えば警告する。ブランチ `ciao` が
別のコミットを指している木では止まる (勝手に動かさない)。ビルドされた rootfs の prudynt はパッチ入りなので、
実機で使っていた bind mount (`S30prudynt-osd`) は要らない。

agent adapter については注意: Thingino のビルドは prudynt 用 adapter を入れた後に thingino-agent の
「null」adapter で上書きしてしまう (出荷ファームが null だった理由)。そのため overlay 側に正しいものを置いている。

**イメージに入らないもの**: フラッシュは 16 MB で、rootfs (squashfs 5696 KB) の空きは約 50 KB、データ領域は 8.5 MB。
Go のプログラム (lapcam 9 MB、osd-feed 9.5 MB、beacon 4 MB) と ffmpeg (2.5 MB) は入らないので
**SD カードに置く**。起動スクリプトはもとから SD (`/mnt/mmcblk0p1`) を見るし、youtube-relay は
`/mnt/mmcblk0p1/ffmpeg` を最初に探す。つまり SD カードを抜くと素の Thingino として動く
(Thingino の Web UI が出る。設定は残る)。

`BUILD_ID` は `ciao+da40db6` (出荷ファームと同じ値) になるようにしている。`device/common.sh` が
この値以外の個体を拒むため、ソース木が detached checkout だと `HEAD+da40db6` になって
インストーラ類が使えなくなる。prepare.sh はそのコミットに `ciao` ブランチを付ける。
その副作用で、Thingino の出力先は `output/ciao/…` になる (build.md の `output/HEAD/…` は detached 時の名前)。

### SD カードの中身 (`firmware/make-sdcard.sh`)

| ファイル | 役割 |
|---|---|
| `factory_t31_ZMC6tiIDQN` | 純正ブートローダー用のインストーラ。thingino-installers の SD イメージ (コミット `4d8530f`、2026-08-14 版に固定) を取得し、`fatextract.py` で取り出して sha256 を照合し、`dist/installer/` にキャッシュする。`-u` では置かない |
| `thingino-wyze_cam3_t31x_gc2053_atbm6031.bin` | 自前のイメージ (インストーラが探す名前)。`-u` では `autoupdate-full.bin` の名前で置く |
| `lapcam` / `osd-feed` / `beacon` / `ffmpeg` | プログラム (`dist/` から) |
| `lapcam.args` | `-listen :80` |
| `wpa_supplicant.conf` | Wi-Fi (複数可)。`-w SSID PASS` で生成、または手で ([wifi-from-sd.md](wifi-from-sd.md))。無ければ設定用の AP (`lapcam-xxxx`、open、設定ページ http://172.16.0.1/) を開く |
| `authorized_keys` | SSH の公開鍵 (1 行 1 鍵)。`-a ~/.ssh/id_ed25519.pub` で置く (**カードのファイルを丸ごと置き換える**ので、使いたい鍵は全部そのファイルに入れておく。各行は `ssh-keygen` が読める鍵であることを検査する)。起動時に `S37keys-from-sd` が `/root/.ssh/authorized_keys` に写す (違う時だけ。カードが正)。焼くと `/root` は消えるので、これが無いと焼くたびに root / `root` で入って鍵を入れ直すことになる。手で入れた個体に後から足すのは `device/install-keys-from-sd.sh root@<ip> key.pub` (こちらはカードの鍵に追加する) |
| `youtube-relay.json` | 配信設定。`-k ストリームキー` で生成、または lapcam の Stream タブで後から |
| `prudynt-osd.json` / `osd-feed.json` | OSD の配置と内容 (レース用の雛形: ロゴ・G ボール・コース図・MEM/CPU・速度) |
| `logo.png` | OSD に出すロゴの既定 (`firmware/sdcard/logo.png`、160×160 の URS ロゴ)。lapcam の OSD タブからの差し替えで上書きされ、以後は保持 |
| `README.txt` | カードの説明 (英語) |
| `VERSION.txt` | カードに入れた各部品の版 ([release.md](release.md))。イメージとプログラムの版が違えば make-sdcard.sh が警告する |

既にカードにある設定ファイル (`wpa_supplicant.conf`、`youtube-relay.json`、`prudynt-osd.json`、`osd-feed.json`、
`lapcam.args`、`logo.png`) はオプションで指示しない限り残す。プログラムとイメージは毎回上書き。オプションは
カードに触る前に全部検査し、途中で失敗しても `*.lapcam-tmp` は残さない。

「焼く・焼かない」を決める印の扱い: 焼く側 (`autoupdate-full.done`、`uenv.done`: Thingino の U-Boot が「適用済み」とみなす印。
`factory_t31_ZMC6tiIDQN.old`、`autoupdate-full.yay`: インストーラが改名した残骸) は、他が全部書けた**最後**に消す。
`-s` では逆に `autoupdate-full.done` を**最初**に書く。途中で失敗しても、カードが「焼く」状態で残らないようにするため。
`-s` のときはブートローダーがカードを読まないので、exFAT のカードでも受け付ける (運用だけなら exFAT でも動く。
次の書き換えには FAT32 のカードが要る)。

## 手順

### 1. イメージを作る (ビルドホスト)

[build.md](build.md) の手順 1 (ソース木の取得、`da40db6`) と 3〜4 (builder イメージと DL キャッシュ) を済ませた上で:

```sh
firmware/build.sh thingino-firmware            # 初回はフルビルド (4 コアで 25 分ほど)
# device/ のスクリプトや prepare.sh の設定を変えた後は、イメージの組み直しだけ (数秒〜1 分)
PACK_ONLY=1 firmware/build.sh thingino-firmware
```

成果物は `dist/firmware/thingino-wyze_cam3_t31x_gc2053_atbm6031.bin` と `dist/firmware/BUILD` (BUILD_ID)、
それと `dist/ffmpeg`。rootfs が 5696 KB に収まらなくなるとビルドが失敗する (空きは約 50 KB しかない)。

### 2. カードを作る (PC)

カードは FAT32 (macOS: `diskutil eraseDisk FAT32 LAPCAM MBRFormat /dev/diskN`。exFAT では純正ブートローダーも
U-Boot も読めない)。`dist/` に lapcam / osd-feed / beacon (各 `device/*/build.sh`) と、上のイメージと ffmpeg があること。

```sh
# 素の Wyze Cam v3 用
firmware/make-sdcard.sh -w "自宅のSSID" "パスワード" -w "ポケットWi-Fi" "パスワード" /Volumes/LAPCAM
# Thingino / lapcam 導入済みの個体を書き換える (ストリームキーも入れておく例)
firmware/make-sdcard.sh -u -k "xxxx-xxxx-xxxx-xxxx" /Volumes/LAPCAM
# もう焼いてある個体のカードを作り直す (焼かない)
firmware/make-sdcard.sh -u -s /Volumes/LAPCAM
diskutil eject /Volumes/LAPCAM
```

他機種は `-c <機種名>` (`dist/firmware/thingino-<機種名>.bin` が要る)。カードでなく普通のディレクトリを渡すと、
空か、以前このスクリプトが作ったものに限って書く (中身を見たいとき)。

### 3. カメラに挿す

- **素の個体**: 電源を切ってカードを挿し、電源を入れる。LED が赤・青・紫に回っている間に純正ファームの退避と書き込み
  (数分)。勝手に再起動する。初回起動は 1 分ほど。`http://lapcam.local/` を開き、root / Thingino 既定のパスワード
  `root` でサインインして、パスワードを変える。
- **Thingino 導入済み**: 同じく電源オフ → カード → オン。U-Boot が `autoupdate-full.bin` を焼いて再起動する (2 分ほど)。
  **設定は全部消える** (ホスト名や Wi-Fi はイメージと SD の内容になる。root のパスワードも `root` に戻る)。

純正に戻すには、`WYZE_BACKUP_xxxxxx/combined_backup.bin` を `autoupdate-full.bin` の名前でカードに 1 つだけ置いて起動する
(thingino-installers の README)。

## 焼いた後に残るもの・消えるもの

カードから焼くとフラッシュ全体が書き換わる。このイメージの U-Boot は焼く前にチップ全体を消す (`sf erase 0 ${flash_len}`) ので、
Thingino の `sysupgrade -B` が backup 領域に残した設定も消える。試験機の 1 回目 (旧 Thingino の U-Boot が焼いた時) だけは
backup 領域が残り、`cfg-autorestore` が以前のパスワードと SSH 鍵を戻した。この U-Boot に替わった 2 回目以降は毎回初期に戻っている。

| | 焼いた後 |
|---|---|
| root のパスワード | Thingino 既定の `root` に戻る (上記のとおり旧 U-Boot で焼いた初回だけは例外) |
| SSH の鍵 | `/root/.ssh/authorized_keys` は消える → カードの `authorized_keys` があれば起動時に戻る。ホスト鍵は作り直される (PC で known_hosts の警告) |
| Wi-Fi | 消える → カードの `wpa_supplicant.conf` があれば起動時に戻る |
| ホスト名、ボタン設定、タイムゾーン、prudynt の既定、netwatch オフ、昼夜切り替えオフ | ファームウェアに焼き込んであるので同じ値 |
| ストリームキー、OSD の設定、ロゴ、録画、lapcam のプロファイル | カード上なので残る |

## 注意

- **機種違い**。Wyze Cam v3 は Wi-Fi チップが ATBM6031 と RTL8189FTV の 2 系統、SoC も T31X / T31ZX / T31AL とあり、
  イメージは機種ごと。手元の個体 (ATBM6031、T31X) 用だけ作っている。インストーラは機種を判定して合う名前の
  ファイルを探すので、他の機種では「該当なし」で何もせず終わる (純正のまま)。他機種は `firmware/build.sh thingino-firmware <機種名>`
  でビルドできるはずだが未検証 (`local.fragment` の内容は機種に依存しない)。
- **純正ファームの版**。純正ブートローダーが SD の `factory_t31_ZMC6tiIDQN` を読むのは Wyze がその経路を塞いでいない個体だけ。
  素の個体で試していないので、公式インストーラの README と同じ条件に従う。
- **二度焼き**。カードに `autoupdate-full.bin` と `factory_t31_ZMC6tiIDQN` を両方置くと、素の個体でインストーラが焼いた後、
  Thingino の U-Boot がもう一度 `autoupdate-full.bin` を焼く。make-sdcard.sh はどちらか一方しか置かない。
- **設定の復元**。Thingino の `S37cfg-autorestore` は、`sysupgrade -B` が backup 領域 (64 KB) に残した設定
  (`/etc/cfg-backup.list`: パスワード、`wpa_supplicant.conf`、タイムゾーン、`thingino.json`、SSH 鍵) を初回起動で戻す。
  以前に `sysupgrade -B` を使った個体では、イメージの `wpa_supplicant.conf` やタイムゾーンよりそれが優先される
  (ホスト名とボタン設定、起動スクリプトは対象外)。純正からの導入では backup 領域は空なので関係ない。
- **Thingino 標準の `sysupgrade -f`** (GitHub から最新を取る) を実行すると公式イメージに戻る。SD の中身は残るので、
  このカードの `-u` で入れ直せる。
- Thingino 本体の更新 (別コミット) に追従するには、パッチ (`patches/`) を当て直し、`device/common.sh` の BUILD_ID 判定も
  更新する必要がある。

## テスト

```sh
python3 -I firmware/fatextract_test.py   # FAT32 を組み立てて: 断片化した連鎖、長い名前、終端、循環、途中で切れる連鎖、不良クラスタ、MBR
sh firmware/make-sdcard_test.sh          # 偽の dist/ と一時ディレクトリで: 各オプション、設定の保持、不正な引数で何も変えない、印の順序
```

## 確認したこと・していないこと

- 2026-11-10: イメージをビルド (BUILD_ID `ciao+da40db6`、rootfs 5648 KB / 5696 KB、データ領域に overlay 20 ファイル)。
  公式インストーラの SD イメージ (`wyze-cam-3-sd.zip`、2026-08-15 版) を展開し、initramfs の `init` を読んで
  上の「しくみ」を確認した。`fatextract.py` の出力は macOS で mount した結果と md5 が一致。
- Wi-Fi 未設定で起きる設定用 AP (`lapcam-xxxx`、172.16.0.1) の中では、Thingino のポータル用 uhttpd が 80 番で上がった後に
  S97lapcam がそれを止めて lapcam が 80 番を取る (S97lapcam の設計どおり)。スマホの「ネットワークにサインイン」の
  自動表示は lapcam のサインイン画面になる。lapcam の Wi-Fi タブで保存して再起動すれば接続できるはずだが、
  **この経路は未検証**。確実なのは SD に `wpa_supplicant.conf` を置くこと。
- 2026-11-10、Thingino 導入済みの試験機 (ATBM6031) に `-u` のカードで書き込み、起動を確認した。lapcam (80 番)、osd-feed、
  beacon、youtube-relay (待機)、rec-led、agent adapter (`clip_recording: true`)、ボタン設定、ホスト名、mDNS、タイムゾーンが
  カードからの初回起動でそろった。その際に分かったこと:
  - **古い U-Boot は `.done` を残さない**。試験機に入っていた旧 Thingino の U-Boot は `autoupdate-full.bin` を焼いたが
    (無言で 2 分ほど。旧版には赤 LED の合図も無い)、`.done` を書けず、代わりに FAT32 の FSInfo ブロック (空き容量の記録) を
    壊していた (Mac の `fsck_msdos` が修復)。焼いた後の U-Boot は新しいので、次の起動からは `.done` が書かれ二度焼きはしない。
    つまり旧版からの移行では、焼いた直後にもう一度 (同じイメージを) 焼く起動が 1 回入る。
  - 試験機には `sysupgrade -B` のバックアップが残っていて、`S37cfg-autorestore` が root のパスワード・SSH 鍵・Wi-Fi を初回起動で戻した
    (上の注意どおり)。
  - **SD カードの相性 (重要)**。ASTC 製 8 GB (2018 年製) のカード 2 枚は、この機体では読めるのに**最初の書き込みで**
    `mmcblk0: unknown error -1 sending read/write command, card status 0xe00` となり、以後 `jzmmc: last operate timeout over
    3000ms ... please change sd card!!!` で SD が死ぬ (録画一覧や Wi-Fi タブも I/O エラー、prudynt は "read-only" で録画拒否、
    lapcam 等がバイナリを読めず uhttpd に戻ることも)。FAT32 でも exFAT でも同じで、Mac では普通に読み書きできる
    (Mac は書き込みの待ちに寛容)。別の型のカード (TMSA08G、2017 年製、FAT32) では書き込み・録画とも問題なし。
    `dmesg` に上の表示が出たらカードの型を変える。本番機のカードは別の型で、問題は出ていない。
- 2026-11-10 の最終確認 (TMSA08G、FAT32、`-u -s`): カードからの起動で lapcam / osd-feed / beacon が立ち上がり、ファイル・
  ディレクトリ作成、16 MB 書き込み、25 秒録画 (`recordings/2026/11/10/16-37-07.mp4`、3.4 MB) が通った。
- 素の個体への書き込みと、Wi-Fi 未設定時の設定用 AP からの設定は未実施。
