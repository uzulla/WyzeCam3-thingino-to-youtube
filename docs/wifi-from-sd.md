# SD カードからの Wi-Fi 設定

コマンド例はリポジトリのルートで実行する前提。

配信設定の SD カードモード ([relay.md](relay.md)) と組み合わせると、**SD 1 枚に Wi-Fi 設定とストリームキーを入れて持ち運べる**
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
  (A は毎回、B は SD の内容が最後に適用した時から変わっていれば適用する)。
  **試す前に、確実に繋がる設定を書いた SD を用意しておくこと**
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
- 適用前の設定は一度だけ `/etc/wpa_supplicant.conf.before-sd` に退避する。最後に適用した内容の md5 を
  `/etc/wpa_supplicant.conf.sd-md5` に置き、これで「内容が変わったか」を判断する (wpa_supplicant 自身が
  動作中に `/etc/wpa_supplicant.conf` を書き換えるので、そのファイルとの比較では毎回「変わった」ことになる)。
  裏返すと、**SD のファイルを変えない限り、その間に WebUI や `wlan configure` でカメラ側で変えた Wi-Fi 設定は
  上書きしない** (SD のファイルを変えて再起動すれば SD が勝つ)
- ログ: `logread | grep wifi-from-sd`。やめるとき: `rm /etc/init.d/S37wifi-from-sd /etc/wpa_supplicant.conf.sd-md5`。
  元の Wi-Fi 設定に戻すなら `mv /etc/wpa_supplicant.conf.before-sd /etc/wpa_supplicant.conf` して再起動
- Thingino を入れ直した/更新した後は、`install.sh` と同じく入れ直しが必要

> B は実機で確認済み (2026-09-22、`ciao+da40db6`: 2 つの `network={}` を書いた SD で再起動 → 起動時に適用、
> 両方が `wpa_cli list_networks` に載り、元の Wi-Fi に接続。内容が同じなら次の起動では書き換えない)。
> **A は実機では未確認** (ソースの読解に基づく)。Wi-Fi が一度も設定されていないカメラでは 1 回目の起動で
> 繋がらず、もう一度再起動が要る可能性がある。詳細は [NOTES.md](../NOTES.md) の「SD カードからの Wi-Fi 設定」。
