# バージョンとリリース

lapcam は **1 つの番号**で管理する。ファームウェア (フラッシュに焼くイメージ。Thingino にこのリポジトリのものを
焼き込んだ、lapcam のファームウェア)、SD カードのプログラム 3 本 (lapcam / osd-feed / beacon)、ffmpeg、カードの中身は
全部同じコミットから作るので、全部に同じ番号が付く。土台にした Thingino の版 (`BUILD_ID`、例 `ciao+da40db6`) は
ファームウェアの版に含意されるので画面には出さず、`/etc/lapcam-release` と `/api/status` に記録だけする。

呼び方: 画面やカードでは「firmware」= lapcam のファームウェア、「lapcam」= SD 上の Web UI のバイナリ。「Thingino」は土台のことだけ。

## 番号の源

git のタグ `vX.Y.Z` だけ。`tools/version.sh` が `git describe` で出す:

```text
v2.0.0              タグの上
v2.0.0-3-g1a2b3c4   タグから 3 コミット進んだ (1a2b3c4)
v2.0.0-dirty        未コミットの変更あり (追跡外の新しいファイルも含む)
g1a2b3c4            届くタグが無い (タグを fetch していない clone も)
unknown             git が無い (tarball で持ってきた木など。ビルドスクリプトが警告する)
```

手で書く VERSION ファイルは置かない (更新忘れの元)。各ビルドスクリプトがこれを読んで埋め込む。
各プログラムは `-version` で `lapcam v2.0.0` のように名前と版を 1 行出す。

## どこに入るか

| 場所 | 書くもの | 中身 |
|---|---|---|
| イメージの `/etc/lapcam-release` | `firmware/prepare.sh` | `VERSION=`、`COMMIT=`、`BUILD_DATE=`、`THINGINO=ciao+da40db6`、`CAMERA=` |
| mDNS `_lapcam._tcp` の TXT | 同上 | `version=` (lapcam の版)、`thingino=` |
| `lapcam` / `osd-feed` / `beacon` | 各 `device/*/build.sh` (`-ldflags -X main.version=`) | `-version` で表示。lapcam は `/api/status` の `versions` (`lapcam`、`firmware`、`thingino`、`mismatch`) と、Camera タブの一番下に `lapcam vX · firmware vX` を出す。beacon の UDP には `lapcam` フィールド |
| `dist/` | 各ビルドスクリプト | `dist/firmware/VERSION` (イメージに実際に書いた値)、`dist/lapcam.version`、`dist/ffmpeg.version` (ffmpeg 自身は版を持たないので、一緒に作ったファームウェアの版) など、成果物の横に。機種は 1 つ (`wyze_cam3_t31x_gc2053_atbm6031`) を前提にしていて、別機種を同じ `dist/` で作ると VERSION / BUILD が最後の機種のものになる |
| SD カードの `VERSION.txt` | `firmware/make-sdcard.sh` | カードに入れた各部品の版。食い違っていれば警告 |

lapcam は自分の版と `/etc/lapcam-release` を比べ、文字列が完全に一致しなければ赤く出す (カードだけ更新した、
ファームウェアだけ古い、が分かる)。開発中にカードのバイナリだけ差し替えると当然赤になる。これは意図どおりで、
「同じリリースの組ではない」という事実の表示。`/etc/lapcam-release` が無い個体 (手で入れた構成) は
「firmware not lapcam's」と出すだけ。

`device/set-camera-name.sh` が書く mDNS も同じ形 (`version=` は `/etc/lapcam-release` の版、無ければ `none`。`thingino=` を併記)。
`tools/find-camera` はその `version=` を表示するので、手で入れた個体は `none` と見える。

## 番号の上げ方

- 機能追加・修正: patch か minor。
- Thingino の土台を変える (別コミットに追従): minor を上げ、[development.md](development.md) の対応表を更新する。
  `device/common.sh` の判定と `firmware/prepare.sh` の `EXPECT_HASH` も変える。
- v1.x は ffmpeg 単体の時代の番号。v2.0.0 からがイメージ + lapcam UI の体系。

## リリースの手順

```sh
git tag -a v2.0.0 -m "v2.0.0"            # main で
device/lapcam/build.sh; device/osd-feed/build.sh; device/beacon/build.sh
# ビルドホストで (docs/sdcard.md):
#   firmware/build.sh thingino-firmware   -> dist/firmware/ と dist/ffmpeg を手元の dist/ へ
tools/release.sh v2.0.0                  # dist/ の版が全部タグと一致するか確かめ、カード一式と zip を作る
git push origin v2.0.0                   # 先にタグを push (無いと gh release create がデフォルトブランチの先頭に勝手にタグを打つ)
# 表示された gh release create を、ノート (dist/release/notes-v2.0.0.md) を書いてから実行
```

`tools/release.sh` が作るもの: `dist/release/lapcam-v2.0.0-sdcard/` (素の個体用のカード一式。FAT32 のカードに展開するだけ)、
その zip、`lapcam-v2.0.0-firmware.bin` (イメージ単体。Thingino 済みの個体なら `autoupdate-full.bin` の名前で
カードに置くか `sysupgrade` で)、`SHA256SUMS` (zip、イメージ、単体の 4 バイナリ)。確かめること: タグが HEAD にある、
`tools/version.sh` がタグそのものを言う (別のタグや dirty でない)、`dist/` の版ファイルがタグと一致、バイナリ 3 本の中に
タグの文字列が埋まっている (手でコピーした別物を弾く)。失敗したら作りかけの成果物は消す。

テスト: `sh tools/version_test.sh` (一時リポジトリでタグ上・タグ後・dirty・追跡外・タグ無し・git 無しの出力)。
