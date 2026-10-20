# カメラの IP アドレスを調べる

Web UI (`http://<camera-ip>/`) や SSH で入るにはカメラの IP が要るが、DHCP で割り当てられるので、
繋ぎ先 (自宅の Wi-Fi、モバイルルーター) が変わるたびに変わりうる。カメラに画面は無いので、
**同じ Wi-Fi / LAN に繋いだ PC やスマホから探す**。

どの方法も、カメラが流している mDNS (Bonjour) の案内を拾っている。Thingino のカメラは起動すると
`mdnsd` が自分のホスト名 (例 `ing-wyze-cam3-9793`) と、次のサービスを LAN に知らせる:

| サービス | 中身 |
|---|---|
| `_thingino._tcp` | **Thingino のカメラだけが出す**。TXT に機種 (`product=`) とファームの版 (`version=`) |
| `_http._tcp` | Web UI (ポート 80)。プリンタやルーターなども出すので、他の機器と混ざる |
| `_rtsp._tcp`、`_ssh._tcp` など | 映像 (RTSP)、SSH |

mDNS はルーターを越えないので、**別のネットワークからは探せない** (LTE のスマホからモバイルルーター配下の
カメラは見えない。同じ Wi-Fi に繋いでから探す)。

## 1. `find-camera` (このリポジトリ、Go)

PC に Go (1.25 以降) があれば、リポジトリのルートで:

```sh
go -C tools/find-camera run .
```

```text
ing-wyze-cam3-9793       192.168.11.29   http://192.168.11.29/  wyze_cam3_t31x_gc2053_atbm6031 ciao+da40db6
```

- 3 秒待って、答えた Thingino のカメラを 1 台 1 行で出す (名前、IP、Web UI の URL、機種とファームの版)。
  見つからなければその旨を出して終了コード 1
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
| macOS | `dns-sd -B _thingino._tcp` (`Ctrl+C` で止める) | `dns-sd -G v4 ing-wyze-cam3-9793.local` |
| Linux (Avahi) | `avahi-browse -rt _thingino._tcp` (IP まで出る) | `avahi-resolve -4 -n ing-wyze-cam3-9793.local` |
| Windows | 標準のコマンドでは一覧できない (1 か下記のアプリ) | `ping -4 ing-wyze-cam3-9793.local` |

名前が分かっていれば、ブラウザで `http://ing-wyze-cam3-9793.local/` と開くだけでもよい (macOS / iOS / Windows 10 以降 /
Linux の多くは `.local` の名前を引ける。Android はブラウザや端末によって引けない)。

## 3. スマホのアプリ

mDNS (Bonjour / DNS-SD) のサービスを一覧するアプリで `_thingino._tcp` を探すと、名前と IP が出る
(iOS なら「Discovery - DNS-SD Browser」、Android なら「Service Browser」など。どれも未確認の例)。
ネットワーク内の機器をスキャンするアプリ (Fing など) でも見つかる。

## 4. ルーターの管理画面

ルーターの「接続中の端末」「DHCP クライアント一覧」に、カメラのホスト名 (`ing-wyze-cam3-…`) で載る。
ルーター側で DHCP の固定割り当て (MAC アドレスに IP を結び付ける) をしておけば、そのルーターの下では毎回同じ IP になる。

## 5. 本体の Setup ボタン

PC もスマホも無い時は、カメラの Setup ボタンを**短く押してすぐ離す**と、スピーカーで IP を読み上げる
(カメラがインターネットに繋がっている時だけ)。長押しは Wi-Fi のリセットや工場出荷状態に戻す動作なので注意。
詳細は [setup-button.md](setup-button.md)。

## 見つからない時

- PC / スマホがカメラと**同じ Wi-Fi** に繋がっているか (ゲスト用 Wi-Fi やモバイル回線ではないか)
- ルーターの「AP アイソレーション」「プライバシーセパレーター」(端末同士の通信を禁止する設定) が有効だと、
  mDNS も Web UI も届かない
- カメラが Wi-Fi に繋がっていない (設定を誤った、など): [wifi-from-sd.md](wifi-from-sd.md) の SD カードで直す
- VPN を使っている PC は、VPN を切ると見つかることがある
