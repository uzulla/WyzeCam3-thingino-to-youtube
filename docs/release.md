# バージョンとリリース

lapcam は **1 つの番号**で管理する。イメージ (フラッシュ)、SD カードのプログラム 3 本 (lapcam / osd-feed / beacon)、
ffmpeg、カードの中身は全部同じコミットから作るので、全部に同じ番号が付く。土台の Thingino の版
(`BUILD_ID`、例 `ciao+da40db6`) は別物で、併記する。

## 番号の源

git のタグ `vX.Y.Z` だけ。`tools/version.sh` が `git describe` で出す:

```text
v2.0.0              タグの上
v2.0.0-3-g1a2b3c4   タグから 3 コミット進んだ (1a2b3c4)
v2.0.0-dirty        未コミットの変更あり
g1a2b3c4            届くタグが無い
```

手で書く VERSION ファイルは置かない (更新忘れの元)。各ビルドスクリプトがこれを読んで埋め込む。

## どこに入るか

| 場所 | 書くもの | 中身 |
|---|---|---|
| イメージの `/etc/lapcam-release` | `firmware/prepare.sh` | `VERSION=`、`COMMIT=`、`BUILD_DATE=`、`THINGINO=ciao+da40db6`、`CAMERA=` |
| mDNS `_lapcam._tcp` の TXT | 同上 | `version=` (lapcam の版)、`thingino=` |
| `lapcam` / `osd-feed` / `beacon` | 各 `device/*/build.sh` (`-ldflags -X main.version=`) | `-version` で表示。lapcam は `/api/status` の `versions` (`lapcam`、`image`、`thingino`、`mismatch`) と Camera カードの一番下に出す。beacon の UDP には `lapcam` フィールド |
| `dist/` | 各ビルドスクリプト | `dist/firmware/VERSION`、`dist/lapcam.version` など、成果物の横に |
| SD カードの `VERSION.txt` | `firmware/make-sdcard.sh` | カードに入れた各部品の版。食い違っていれば警告 |

lapcam は自分の版と `/etc/lapcam-release` を比べ、違えば Camera カードに赤く出す
(カードだけ更新した、イメージだけ古い、が分かる)。`/etc/lapcam-release` が無い個体 (手で入れた構成) は
「no lapcam image」と出すだけ。

## 番号の上げ方

- 機能追加・修正: patch か minor。
- Thingino の土台を変える (別コミットに追従): minor を上げ、README の対応表に Thingino の版との組を足す。
  `device/common.sh` の判定と `firmware/prepare.sh` の `EXPECT_HASH` も変える。
- v1.x は ffmpeg 単体の時代の番号。v2.0.0 からがイメージ + lapcam UI の体系。

## リリースの手順

```sh
git tag -a v2.0.0 -m "v2.0.0"            # main で
device/lapcam/build.sh; device/osd-feed/build.sh; device/beacon/build.sh
# ビルドホストで (docs/sdcard.md):
#   firmware/build.sh thingino-firmware   -> dist/firmware/ と dist/ffmpeg を手元の dist/ へ
tools/release.sh v2.0.0                  # dist/ の版が全部タグと一致するか確かめ、カード一式と zip を作る
# 表示された gh release create を、ノート (dist/release/notes-v2.0.0.md) を書いてから実行
git push origin v2.0.0
```

`tools/release.sh` が作るもの: `dist/release/lapcam-v2.0.0-sdcard/` (素の個体用のカード一式。FAT32 のカードに展開するだけ)、
その zip、`lapcam-v2.0.0-firmware.bin` (イメージ単体。Thingino 済みの個体なら `autoupdate-full.bin` の名前で
カードに置くか `sysupgrade` で)、`SHA256SUMS`。
