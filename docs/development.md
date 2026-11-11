# 開発

リポジトリの構成、対応するカメラとファームウェア、ビルドとテスト。ビルドの手順そのものは [build.md](build.md)、
リリースは [release.md](release.md)、設計の判断と実測の記録は [NOTES.md](../NOTES.md)。

## リポジトリの構成

```text
firmware/   ファームウェアイメージと SD カードを作る (docs/sdcard.md)
              prepare.sh        Thingino のソース木にパッチと user/<camera>/ (焼き込む設定、overlay) を用意する
              build.sh          ビルドホストでイメージを作る -> dist/firmware/、dist/ffmpeg
              make-sdcard.sh    PC でカードに一式を書く (fatextract.py は公式インストーラの取り出し)
              sdcard/           カードに置く雛形 (README.txt、OSD の設定、ロゴ)
device/     カメラに入るもの (一覧は device/README.md)
              lapcam/           スマホ向けの Web UI (Go、ポート 80。SD カードから起動)
              osd-feed/         OSD のテキスト更新と PNG / G ボール / 走行図の描画 (Go、SD カードから起動)
              beacon/           状態の UDP 放送と指令の受付 (Go、SD カードから起動)
              youtube-relay, osd-config, rec-led, rec-button, wifi-ap, S9x…   ファームウェアに焼き込むスクリプト
              install*.sh       素の Thingino に手で入れるためのインストーラ (ファームウェアを使うなら不要)
              www/              旧 Thingino Web UI 向けのページ (lapcam に置き換えた。uhttpd を使う個体でだけ動く)
patches/    Thingino / FFmpeg / prudynt に当てる差分 (prepare.sh が当てる)
              thingino-ffmpeg-rtmps.diff             thingino-ffmpeg を RTMPS 対応にする
              thingino-ffmpeg-rtmp-chunk-size.diff   RTMP の送信チャンクを 128 から 4096 バイトへ (送信量 1.7 倍 -> 1.08 倍)
              prudynt-osd-textfile.diff              prudynt にテキスト 3 枠と画像 1 枠の OSD を足す
              prudynt-msgchannel-warning.diff        5 秒ごとの誤報 (msgChannel sink clogged) を止める
              thingino-agent-adapter-record-args.diff  agent の prudynt 用 adapter で record の引数順を直す
              thingino-wifi-portal-ssid.diff         設定用 AP の名前をホスト名 (lapcam-xxxx) にする
tools/      PC で使う道具
              find-camera/      LAN のカメラを mDNS で探す (Go)
              beacon-listen/, beacon-send/   beacon の受信と指令 (Go)
              concat-recordings.sh   1 分ごとの録画を 1 本につなぐ (ffmpeg)
              version.sh, release.sh  版の決定とリリースの組み立て
docs/       文書。NOTES.md は経緯と判断の記録
dist/       ビルド成果物 (git 管理外)。配布は GitHub Releases
thingino-firmware/   Thingino のソース木 (ビルド時にここへ clone、git 管理外。変更は全部 patches/ と firmware/prepare.sh に)
```

## 対応するカメラとファームウェア

| 項目 | 値 |
|---|---|
| カメラ | Wyze Cam v3 (Ingenic T31X、GC2053、Wi-Fi ATBM6031)。機種名 `wyze_cam3_t31x_gc2053_atbm6031` |
| 土台 | Thingino `ciao+da40db6` (2026-09-14、`ciao` ブランチ)。イメージの `BUILD_ID` も同じ値 |
| ABI | mipsel / MIPS32 o32 / hard-float / uClibc-ng 1.0.59 / GCC 16.2 / mbedTLS 3.6.6 |
| ビルドホスト | x86_64 Linux + Docker (Thingino 公式のビルダーイメージ) |
| PC 側 | Go 1.25 以降 (lapcam / osd-feed / beacon / tools)、Python 3 と sh (firmware/ のスクリプト) |

Wyze Cam v3 には Wi-Fi チップが RTL8189FTV の個体と、SoC が T31ZX / T31AL の個体もある。イメージは機種ごとで、作っているのは上の 1 機種だけ
(`firmware/build.sh … <機種名>` で他機種も作れるはずだが未検証)。

Thingino の土台を別のコミットに移す時は、`patches/` を当て直し、`firmware/prepare.sh` の `EXPECT_HASH` と `device/common.sh` の
`SUPPORTED_BUILD` を変え、minor を上げる ([release.md](release.md))。v1.x (2026-08〜09) は ffmpeg 単体を配っていた時代の番号で、
v2.0.0 からがファームウェア + Web UI の体系。

## ビルド

| 何を | どこで | コマンド |
|---|---|---|
| ファームウェアイメージと ffmpeg | ビルドホスト | `firmware/build.sh thingino-firmware` (初回 25 分ほど。device/ の変更後は `PACK_ONLY=1` で数秒) |
| lapcam / osd-feed / beacon | PC (Go) | `device/lapcam/build.sh` など。`go vet` と `go test` を通してから静的リンクのバイナリを `dist/` に出す |
| SD カード | PC | `firmware/make-sdcard.sh [-u] [-w SSID PASS]… /Volumes/カード` |
| リリース一式 | PC | `tools/release.sh vX.Y.Z` ([release.md](release.md)) |

版は git のタグから `tools/version.sh` が決め、全部のビルドスクリプトがそれを埋め込む。

## テスト

カメラは要らない。全部 PC で回る。

```sh
go -C device/lapcam test ./... && go -C device/osd-feed test ./... && go -C device/beacon test ./... && go -C tools/find-camera test ./...
python3 -I firmware/fatextract_test.py     # FAT32 の取り出し
sh firmware/make-sdcard_test.sh            # カード作成の各オプション、設定の保持、失敗時に何も変えないこと
sh tools/version_test.sh                   # 版の決定 (タグ上、タグ後、dirty、git 無し)
sh tools/release_test.sh                   # リリースの組み立てと検査
```

実機でしか確かめられないもの (prudynt、init スクリプト、wpa_cli、U-Boot の書き込み) は、各文書の「確認したこと」に日付つきで記録している。

## 実機で守ること

- prudynt の設定の保存に Thingino の `save_config` は使わない (osd-config の仕組みと衝突する)。`/etc/prudynt.json` を書くのは
  lapcam が画質と録画の設定を書く時だけ
- ストリームキーはログや文書に出さない (`REDACTED` にする)。露出したら YouTube Studio で再生成する
- SD カードに書いた後は `sync`。カメラの電源は普通に抜かれる前提で、設定ファイルは隣に書いてから rename する
- カメラに sftp は無い。ファイルは `cat file | ssh root@… 'cat > …'` で送る
