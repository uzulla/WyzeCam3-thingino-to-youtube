# netwatch (ネットワーク監視による OS 再起動) の無効化

Thingino (`ciao+da40db6`) の `S52netwatch` と、ライブ配信ではそれを止める理由。
**ファームウェアでは無効にして焼き込んである** (`firmware/prepare.sh` が `/etc/thingino.json` の `netwatch.enabled` を
`false` にする) ので、SD カードから入れた個体では何もしなくてよい。`device/disable-netwatch.sh` は素の Thingino に
手で入れた個体のためのもの。

## netwatch とは

Thingino の `S52netwatch` は**デフォルトで有効**。デフォルトゲートウェイへ 30 秒毎に ping し、**3 回連続で失敗すると
カメラを再起動する**。プロセスの再起動ではなく **OS ごとの強制リブート** (`reboot -f`。効かなければ sysrq、それも
効かなければ watchdog デーモンを止めてあるのでハードウェアリセット) で、配信は一度切れる。2026-08 以前のファームにこの仕組みは無い。

## 無効にする理由

- ネットワーク断からの復帰は supervisor ([relay.md](relay.md)) が担う (デフォルトルートが戻るのを待って配信を再開)。
  OS ごと再起動しても復帰は早くならず、起動時間の分だけ配信の欠損が延びる
- ICMP に応答しないモバイルルーター / テザリングでは、Wi-Fi が正常でも約 90 秒毎に再起動して
  配信が成立しない ([cellular.md](cellular.md))

無効化の代償: Wi-Fi ドライバが固まって復帰しなくなった場合に自動では直らなくなる
(電源の入れ直しが必要)。無人・遠隔設置でそちらの方が困る場合は、無効化せず
`netwatch.target` を確実に ping に応答するホストにする手もある。

## 素の Thingino に手で入れる場合

```sh
# リポジトリのルートで
device/disable-netwatch.sh root@<camera-ip>
```

Thingino の OS 設定 (`/etc/thingino.json` の `netwatch.enabled`) を変えるものなので、
`install.sh` とは別のスクリプトにしてある (`install.sh` は OS の設定に触らない)。
設定は再起動後も残るが、**Thingino を入れ直した/更新した後は初期値 (有効) に戻ることがある**ので、
`install.sh` と合わせて実行し直す。手作業なら:

```sh
jct /etc/thingino.json set netwatch.enabled false && service restart netwatch
jct /etc/thingino.json set netwatch.enabled true  && service restart netwatch   # 元に戻す
```
