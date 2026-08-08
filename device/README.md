# device/ — カメラへインストールするファイル

Thingino 実機に置く supervisor 一式。`/etc` 以下は overlayfs でフラッシュに永続化されるため、
ファームウェアを書き換えずにインストールでき、再起動後も残ります。

| ファイル | インストール先 | 役割 |
|---|---|---|
| `youtube-relay` | `/usr/sbin/youtube-relay` | supervisor 本体 (ffmpeg を監視・再起動するループ) |
| `S93youtube-relay` | `/etc/init.d/S93youtube-relay` | 起動スクリプト (boot 時に自動開始) |
| `youtube-relay.json.example` | `/etc/youtube-relay.json` または SD カード直下 | 設定ファイル (下記「SD カードモード」参照) |

## 設定ファイルの探索順と「SD カードモード」

supervisor は常駐し、以下の順で設定を探します (10秒間隔でポーリング):

```text
1. /mnt/mmcblk0p1/youtube-relay.json   (SD カード。Thingino が自動マウント)
2. /etc/youtube-relay.json             (内蔵フラッシュ、chmod 600)
どちらも無い/無効 → 配信せず待機
```

これにより **SD カードが物理スイッチ**になります:

- `youtube-relay.json` を書いた SD を**挿す** → 自動マウント後、次のポーリングで配信開始
- SD を**抜く** → 配信中の監視 (15秒間隔) が設定消失を検知し、配信停止して待機に戻る
- 最小構成は `{"stream_key": "xxxx"}` の 1 行だけで動く (他はデフォルト値)
- `"enabled": false` を書くと明示的に無効化できる (デフォルトは有効)

ffmpeg バイナリとスクリプトは内蔵フラッシュに常設し、SD はキーだけを運ぶ分担を推奨。
Wyze Cam v3 の overlay (データパーティション) は 8.5MB で、ffmpeg 2.6MB は問題なく入る。

## 設計 (Thingino の流儀に準拠)

- **systemd はない**。busybox init が `/etc/init.d/S*` を番号順に実行する。`S93` は
  ネットワーク (S40前後) と prudynt (S31) の後
- クラッシュ・切断対応は **supervisor のシェルループ**が担う: ffmpeg 終了を検知して再起動、
  60秒以上動いていたら即時 (5秒)、即死を繰り返す場合は指数バックオフ (最大300秒)。
  `thingino-ha` パッケージの watchdog と同じ構造
- 起動前に**デフォルトゲートウェイへの ping で ネットワーク up を待ち**、NTP 同期フラグ
  (`/run/sync_success`) を最大60秒待つ
- `start-stop-daemon -N 10` で低優先度起動 — prudynt / ISP 処理と CPU を取り合わない
  (`telegrambot` と同じ流儀)
- ストリームキーが未設定なら**起動せず SKIP** (設定するまで無害)
- 有効/無効の切り替えは Thingino 標準の `service enable|disable youtube-relay`
  (init スクリプトの実行ビットで制御) か、JSON の `"enabled"` フラグ

## ffmpeg バイナリの設置場所

**`/tmp` は tmpfs (RAM) なので再起動で消える。PoC 専用であり、常設運用では使わないこと。**

Thingino はルート全体が overlayfs (SquashFS + JFFS2 データパーティション) で覆われており、
どこに書いてもフラッシュに永続化される。設置場所は以下から選ぶ:

| 場所 | 永続性 | 用途 | 備考 |
|---|---|---|---|
| `/usr/bin/ffmpeg` | overlay で永続 | **常設 (推奨)** | PATH も通る |
| `/mnt/mmcblk0p1/ffmpeg` | SD カード | フル SD 運用 / テスト | バイナリ+設定を SD で完結できる。SD を抜くと配信不可 |
| `/tmp/ffmpeg` | **再起動で消える** | PoC / テスト | 動作確認が終わったら /usr/bin へ移す |

- 事前に空き容量を確認する: `df -h /overlay`
  (Wyze Cam v3 実機ではデータパーティション 8.5MB 中 約8.2MB 空き。ffmpeg は 2.6MB なので余裕)
- supervisor の探索順は **一時的な場所が常設を上書きする** 設計:

  ```text
  /mnt/mmcblk0p1/ffmpeg → /tmp/ffmpeg → /usr/bin/ffmpeg → /usr/local/bin/ffmpeg
  ```

  新しいビルドを試すときは SD か /tmp に置くだけでよく、常設バイナリはそのまま残る。
  再起動 (/tmp が消える) や SD 抜去で自動的に常設版へ戻る。
  明示指定したい場合は設定の `ffmpeg_bin` にフルパスを書く (探索より優先)
- PoC で `/tmp/ffmpeg` に置いたバイナリを常設に昇格するには (実機上で):

  ```sh
  cp /tmp/ffmpeg /usr/bin/ffmpeg
  chmod +x /usr/bin/ffmpeg
  md5sum /tmp/ffmpeg /usr/bin/ffmpeg   # 一致を確認
  ```

## インストール手順

```sh
CAM=root@<camera-ip>

# 1. ffmpeg バイナリ (dist/ffmpeg) を永続領域へ
#    まず空き容量を確認 (2.6MB 必要。/overlay が JFFS2 データパーティション)
ssh $CAM 'df -h | grep -E "overlay|Filesystem"'
cat ../dist/ffmpeg | ssh $CAM 'cat > /usr/bin/ffmpeg && chmod +x /usr/bin/ffmpeg'
#    空きが足りない場合は SD カードに置き、設定の ffmpeg_bin をそのパスにする

# 2. スクリプト
cat youtube-relay      | ssh $CAM 'cat > /usr/sbin/youtube-relay && chmod +x /usr/sbin/youtube-relay'
cat S93youtube-relay   | ssh $CAM 'cat > /etc/init.d/S93youtube-relay && chmod +x /etc/init.d/S93youtube-relay'

# 3. 設定 — どちらか:
#  a) 内蔵に置く (据え置き運用。キーはカメラ上で直接編集)
cat youtube-relay.json.example | ssh $CAM 'cat > /etc/youtube-relay.json && chmod 600 /etc/youtube-relay.json'
ssh $CAM 'vi /etc/youtube-relay.json'    # stream_key を記入
#  b) SD カードに置く (スタンドアロン運用)
#     PC で SD 直下に youtube-relay.json を書いてカメラに挿すだけ。手順 4 は不要

# 4. ファーム更新時のバックアップ対象に登録
ssh $CAM 'grep -q youtube-relay /etc/cfg-backup.list || echo /etc/youtube-relay.json >> /etc/cfg-backup.list'

# 5. 起動
ssh $CAM '/etc/init.d/S93youtube-relay start'

# 状態確認・ログ
ssh $CAM '/etc/init.d/S93youtube-relay status'
ssh $CAM 'logread | grep youtube-relay | tail -20'
```

## 運用

```sh
service stop youtube-relay      # 配信停止 (supervisor ごと止まる)
service start youtube-relay     # 配信開始
service disable youtube-relay   # boot 時の自動開始を無効化
service enable youtube-relay    # 有効化
```

設定変更 (`/etc/youtube-relay.json` 編集) 後は `service restart youtube-relay`。

## ストリームキーの取り扱い

- 内蔵運用 (`/etc/youtube-relay.json`) は **chmod 600**。物理アクセスされない限り漏れない
- **SD カードモードはトレードオフを理解して使う**: FAT にパーミッションは無く、カードを
  抜かれればキーは読まれる。車載等でカメラごと盗まれるリスクと大差ない場面では実用的だが、
  キーだけ守りたい据え置き用途では内蔵運用を選ぶ
- **ffmpeg プロセスの引数 (`ps` / `/proc/*/cmdline`) には、ストリームキー入りの RTMPS URL と
  RTSP の認証情報がそのまま載る**。Thingino は全プロセス root のシングルユーザー機なので
  ローカルでの実害はないが、`ps` 出力を含むログ・スクリーンショット・サポート依頼の貼り付けに
  キーが写り得る点に注意 (supervisor 自身の syslog 出力だけは REDACTED 表記にしてある)
- 漏洩したら YouTube Studio でキーを再生成し、JSON を更新して restart
