# SD カードからの Wi-Fi 設定


上の SD カードモードと組み合わせると、**SD 1 枚に Wi-Fi 設定とストリームキーを入れて持ち運べる**
(車載で現地のモバイルルーターに繋ぎ替える、など)。方法は 2 つ。**併用はしないこと**
(両方あると A が後から上書きする)。

| | A. `uenv.txt` (Thingino 標準) | B. `wpa_supplicant.conf` (このリポジトリの追加スクリプト) |
|---|---|---|
| 書ける Wi-Fi | **1 つだけ** | **複数** (自宅 + モバイルルーター等。`priority` も使える) |
| インストール | 不要 | `device/install-wifi-from-sd.sh root@<camera-ip>` |
| SD に置くファイル | `uenv.txt` | `wpa_supplicant.conf` |

どちらも共通の注意:

- 読み込まれるのは**起動時だけ**。SD を挿しただけでは反映されないので再起動する
- 適用された設定はカメラ側に保存される。**SD を抜いても元の Wi-Fi には戻らない**
  (最後に適用されたものが残る)。繋がらなくなったら、SD のファイルを直して再起動すれば復旧できる
  (毎回 SD の内容が正になる)。**試す前に、確実に繋がる設定を書いた SD を用意しておくこと**
  (繋がらないと SSH でも直せない)
- **SD 上のパスワードは平文**。ストリームキーと同じく、SD を抜かれたら読まれる前提で使う

### A. `uenv.txt` — Wi-Fi が 1 つでよい場合 (Thingino 標準)

このリポジトリの機能ではなく Thingino 本体の機能 (`S38wpa_supplicant` の `credentials_from_card`)。
SD 直下に `uenv.txt` を置く (改行は LF):

```text
wlan_ssid=MyNetwork
wlan_pass=MyPassword
```

- カメラ側にはパスワードを PSK ハッシュにして保存する
- `wlan_ssid` / `wlan_pass` のどちらかが欠けていると適用されない (既存の設定のまま)
- 同じキーを複数行書くことはできない (値が壊れる)

### B. `wpa_supplicant.conf` — 複数の Wi-Fi を書きたい場合

```sh
device/install-wifi-from-sd.sh root@<camera-ip>    # 起動スクリプト /etc/init.d/S37wifi-from-sd を入れるだけ
```

`/etc/wpa_supplicant.conf` (Thingino の OS 設定) を書き換える機能なので、`install.sh` とは別の
任意のスクリプトにしてある。SD 直下に通常の `wpa_supplicant.conf` を置く:

```text
network={
        ssid="Home"
        psk="home-password"
        priority=10
}
network={
        ssid="Pocket-WiFi"
        psk="pocket-password"
        scan_ssid=1
}
```

- Thingino の Wi-Fi 起動 (`S38`) の直前に動き、内容が変わったときだけ書き換える
  (毎回フラッシュに書かない)。Windows の改行 (CRLF) は取り除く
- `ctrl_interface=` の行が無ければ Thingino 標準のヘッダを補う。`network={}` ブロックだけ書けばよい
- `psk=` は `wpa_passphrase <ssid> <password>` で作った 64 桁のハッシュでも可 (平文を置きたくない場合)
- **`psk=` の無いオープンな Wi-Fi は使えない** (Thingino が「未設定」と判断して設定用ポータルを
  起動してしまうため)。その場合ファイルは無視され、ログに理由が出る
- 適用前の設定は一度だけ `/etc/wpa_supplicant.conf.before-sd` に退避する
- ログ: `logread | grep wifi-from-sd`。やめるとき: `rm /etc/init.d/S37wifi-from-sd`
- Thingino を入れ直した/更新した後は、`install.sh` と同じく入れ直しが必要

> **実機では未確認の機能** (ソースの読解と PC 上の busybox でのテストに基づく)。特に A は、Wi-Fi が一度も設定されて
> いないカメラでは 1 回目の起動で繋がらず、もう一度再起動が要る可能性がある。詳細は [NOTES.md](../NOTES.md) の
> 「SD カードからの Wi-Fi 設定: 未検証の点」。
