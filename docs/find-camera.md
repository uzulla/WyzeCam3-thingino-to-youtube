# カメラの IP アドレスを調べる

lapcam の Web UI (`http://<camera-ip>/`) や SSH で入るにはカメラの IP が要るが、DHCP で割り当てられるので、
繋ぎ先 (自宅の Wi-Fi、モバイルルーター) が変わるたびに変わりうる。カメラに画面は無いので、
**同じ Wi-Fi / LAN に繋いだ PC やスマホから探す**。多くの場合は `http://lapcam-xxxx.local/` と開くだけで足りる。

どの方法も、カメラが流している mDNS (Bonjour) の案内を拾っている。カメラは起動すると `mdnsd` が自分のホスト名
(ファームウェアなら `lapcam-xxxx`、xxxx は MAC アドレス由来の 16 進 4 桁。[sdcard.md](sdcard.md)) と、次のサービスを LAN に知らせる:

| サービス | 中身 |
|---|---|
| `_lapcam._tcp` | **lapcam のカメラだけが出す** (ファームウェアに `/etc/mdns.d/lapcam.service` が入っている)。TXT に機種 (`product=`)、lapcam の版 (`version=`)、Thingino の版 (`thingino=`) |
| `_thingino._tcp` | Thingino のカメラなら全部出す (Thingino 標準) |
| `_http._tcp` | Web UI (ポート 80、lapcam)。プリンタやルーターなども出すので、他の機器と混ざる |
| `_rtsp._tcp`、`_ssh._tcp` など | 映像 (RTSP)、SSH |

mDNS はルーターを越えないので、**別のネットワークからは探せない** (LTE のスマホからモバイルルーター配下の
カメラは見えない。同じ Wi-Fi に繋いでから探す)。

素の Thingino に手で入れた個体は、`device/set-camera-name.sh` でホスト名を `lapcam` にし、同じ `_lapcam._tcp` を出す
([lapcam.md](lapcam.md#カメラの名前))。入れていなければ Thingino の既定の名前 (`ing-wyze-cam3-9793` のような) のまま。

## 1. `find-camera` (このリポジトリ、Go)

PC に Go (1.25 以降) があれば、リポジトリのルートで:

```sh
go -C tools/find-camera run .
```

```text
lapcam-9793              192.168.11.29   http://192.168.11.29/  wyze_cam3_t31x_gc2053_atbm6031 v2.1.0
```

- 3 秒待って、答えた lapcam のカメラを 1 台 1 行で出す (名前、IP、Web UI の URL、機種と lapcam の版。TXT の `product=` と `version=`)。
  見つからなければその旨を出して終了コード 1
- `_lapcam._tcp` を出していない素の Thingino のカメラは `-service _thingino._tcp` で探す
- `-t 10s` で待ち時間を延ばす (Wi-Fi の省電力で返事が遅いことがある。実測では 1 秒以内)。
  `-v` で探しているネットワークインターフェース、問い合わせを送れなかったインターフェース、壊れた返事を出す
- PC の IPv4 マルチキャストに対応したインターフェース (有線と Wi-Fi の両方、など) で探す。ループバックと、
  マルチキャストに対応しないインターフェースでは探さない (VPN のトンネルは、対応していれば対象になる)。
  外部のパッケージは使っていない (標準ライブラリのみ)
- 他の OS 用のバイナリ: `GOOS=windows GOARCH=amd64 go -C tools/find-camera build -o ../../dist/find-camera.exe .`
  (Linux は `GOOS=linux`、Mac は指定なし)。Go の入っていない PC にはこれを持っていく
- Windows では、初回の実行時にファイアウォールの許可を求められたら「プライベート ネットワーク」を許可する
  (受信を許可しないと返事が届かない)
- **動作を確認したのは macOS だけ**。Windows と Linux はビルドが通ることのみ確認していて、実行は未確認

## 2. OS の標準コマンド

| OS | 探す | 名前から IP を引く |
|---|---|---|
| macOS | `dns-sd -B _lapcam._tcp` (`Ctrl+C` で止める) | `dns-sd -G v4 lapcam-xxxx.local` |
| Linux (Avahi) | `avahi-browse -rt _lapcam._tcp` (IP まで出る) | `avahi-resolve -4 -n lapcam-xxxx.local` |
| Windows | 標準のコマンドでは一覧できない (1 か下記のアプリ) | `ping -4 lapcam-xxxx.local` |

名前が分かっていれば、ブラウザで `http://lapcam-xxxx.local/` と開くだけでもよい (macOS / iOS / Windows 10 以降 /
Linux の多くは `.local` の名前を引ける。Android はブラウザや端末によって引けない)。手で入れた個体は `lapcam.local`。

## 3. スマホのアプリ

mDNS (Bonjour / DNS-SD) のサービスを一覧するアプリで `_lapcam._tcp` (または `_thingino._tcp`) を探すと、名前と IP が出る
(iOS なら「Discovery - DNS-SD Browser」、Android なら「Service Browser」など。どれも未確認の例)。
ネットワーク内の機器をスキャンするアプリ (Fing など) でも見つかる。

## 4. ルーターの管理画面

ルーターの「接続中の端末」「DHCP クライアント一覧」に、カメラのホスト名 (`lapcam-xxxx`。Thingino の既定のままなら `ing-wyze-cam3-…`) で載る。
ルーター側で DHCP の固定割り当て (MAC アドレスに IP を結び付ける) をしておけば、そのルーターの下では毎回同じ IP になる。

## 5. 本体の Setup ボタン

PC もスマホも無い時は、カメラの Setup ボタンを**短く 1 回押す**と、0.7 秒ほど後にスピーカーで IP を読み上げる
(カメラがインターネットに繋がっている時だけ)。ダブルクリックは録画の開始・停止、5 秒長押しは Wi-Fi AP、
20 秒は工場出荷状態に戻す動作なので注意。詳細は [setup-button.md](setup-button.md)。

## 6. beacon

カメラの [beacon](beacon.md) が数秒ごとに IP を LAN にブロードキャストしているので、
`go -C tools/beacon-listen run .` で待つだけで分かる (配信の状態も一緒に出る)。

## 見つからない時

- PC / スマホがカメラと**同じ Wi-Fi** に繋がっているか (ゲスト用 Wi-Fi やモバイル回線ではないか)
- ルーターの「AP アイソレーション」「プライバシーセパレーター」(端末同士の通信を禁止する設定) が有効だと、
  mDNS も Web UI も届かない
- カメラが Wi-Fi に繋がっていない (設定を誤った、保存した Wi-Fi が無い): 本体ボタンを 5 秒押してカメラを AP にし、
  `lapcam-xxxx` に繋いで `http://100.64.1.1/` の Wi-Fi タブで直す ([lapcam.md](lapcam.md))。または SD カードの
  `wpa_supplicant.conf` を書き換える ([wifi-from-sd.md](wifi-from-sd.md))
- VPN を使っている PC は、VPN を切ると見つかることがある
