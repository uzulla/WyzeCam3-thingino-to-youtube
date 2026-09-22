# セルラー回線 (車載) での配信: 送信量と切断からの復帰の調査メモ

車載で、カメラ → 車内のモバイルルーター (Wi-Fi) → LTE → YouTube という構成を想定した調査の記録。
「通信品質がとても悪い」前提で、何が送信量を増やし、電波が途切れた時に何が起きて、どこにチューニングの余地があるかを、
実機 (`ciao+da40db6`、720p10 / CBR 1024kbps) で測った数字とともにまとめる。測定日は 2026-09-22。
設定の変え方そのものは [relay.md](relay.md)、症状別の対処は [troubleshooting.md](troubleshooting.md)。

## 結論 (先に)

| 論点 | 結論 |
|---|---|
| 送信量 | エンコーダの 1.05 Mbps に対し、素の FFmpeg + RTMPS は **1.77 Mbps (1.69 倍)** 送っていた。原因は FFmpeg の RTMP チャンク 128 バイト固定 (下記)。パッチで 1.13 Mbps、平文 RTMP なら 1.09 Mbps |
| TLS の有無 | パッチ後は送信量の差 3%、ffmpeg の CPU 差 0.3 ポイント、接続時のハンドシェイク差 0.1 秒以下。**通信品質への影響はほぼ無い**。代わりにストリームキーが経路上を平文で流れる |
| 短い途切れ (〜45 秒) | TCP の再送で回復し、ffmpeg は再起動しない。YouTube は約 45 秒の無送信でセッションを閉じる |
| 長い途切れ | 電波が戻ると次の再送に YouTube が RST を返し、ffmpeg が即終了 → supervisor が再起動。**復帰は電波が戻ってから約 20 秒** (実測)。何分も固まることはない |
| チューニング | 送信量はパッチ入り ffmpeg (既定) で対処済み。復帰時間は `-rw_timeout` で「長い圏外の後の最悪 2 分」を 10〜20 秒に縮められるが、短い途切れでの再起動が増える。必須ではない |

## 1. 送信量: 何がビットレートを膨らませるか

### 実測

`Ip6OutOctets` (`/proc/net/snmp6`) の 120 秒差分。この値は**ホスト全体の IPv6 送信量** (UDP なども含む) で、YouTube 専用では
ない。ここでは YouTube への接続が IPv6 で、他に IPv6 の TCP 接続が無いこと (`netstat -tn`。ssh は IPv4) を確認した上で、
IPv6 全体 ≒ YouTube 分と見なしている (IPv6 の UDP は `Udp6OutDatagrams` の差分で無いことを確かめられる)。
映像 + 音声は prudynt の RTSP を 30 秒録画して 1.046 Mbps (H.264 Main 10fps + AAC 16kHz mono)。

| 接続 | RTMP チャンク | YouTube への送信 | 映像 + 音声に対して | ffmpeg CPU |
|---|---|---|---|---|
| RTMPS (443) | 128 (素の FFmpeg) | 1771 kbps | 1.69 倍 | (前日 4.8%) |
| RTMPS (443) | 4096 (パッチ、既定) | 1126〜1128 kbps | 1.08 倍 | 2.5% |
| RTMPS (443) | 65536 | 1111 kbps | 1.06 倍 | — |
| 平文 RTMP (1935) | 4096 (パッチ、既定) | 1093〜1095 kbps | 1.04 倍 | 2.2% |
| 平文 RTMP (1935) | 128 (素の動作) | 1133 kbps | 1.08 倍 | 3.6% |

1 時間あたりの通信量に直すと、素の RTMPS 約 800MB → パッチ入り RTMPS 約 510MB → 平文 RTMP 約 490MB。

### 原因

FFmpeg の RTMP は送信チャンクが **128 バイト固定** で、チャンクごとにデータと 1 バイトの継続ヘッダを別々に書き出す。
RTMPS では書き込み 1 回が TLS レコード 1 つ (約 29 バイトの枝葉) になるので、128 バイトごとに約 58 バイトが乗り、
小さな書き込みが多い分 TCP/IPv6 ヘッダ (60 バイト以上/セグメント) の比率も上がる。平文 RTMP では TCP が小さな書き込みを
まとめるので目立たない。詳細と FFmpeg 側の該当箇所は [NOTES.md](../NOTES.md) の「RTMPS の送信量が映像の 1.7 倍だった」。

対処は `patches/thingino-ffmpeg-rtmp-chunk-size.diff` (接続直後にチャンクサイズ 4096 をサーバへ宣言する)。このリポジトリの
手順でビルドした ffmpeg には入っている。値は `ffmpeg_out_opts` の `-rtmp_chunk_size` で変えられる (128 で素の動作)。

### セルラーでの含意

- **回線の上限に近い時、1.7 倍の差はそのまま混雑によるドロップと遅延の増加になる。** パッチ入りの ffmpeg を使うこと
  (`ffmpeg -h protocol=rtmps | grep rtmp_chunk_size` で入っているか分かる)
- 平文 RTMP にする利点は送信量 3% と CPU 0.3 ポイント。**キーが経路上を平文で流れる**ことと引き換えになる
  ([relay.md](relay.md) の「ストリームキーの取り扱い」)。帯域を最優先するなら平文、そうでなければ RTMPS で差し支えない
- ポート 1935 を塞ぐ・絞る回線があり、443 はほぼ通る。平文 RTMP が不安定な時は、まず RTMPS に戻して比べる
- 送信量を本当に減らしたいなら、エンコーダのビットレート (`stream0.bitrate`) と fps を下げるのが桁違いに効く。これは
  Thingino の設定 (WebUI か `/etc/prudynt.json`) の話で、このリポジトリの範囲外

## 2. TLS は通信品質の悪さに対して不利か

**ほぼ無関係。** RTMP も RTMPS も TCP の上なので、パケットロスの回復は TCP の再送で、TLS の有無で変わらない。間接的な差は:

- 送るバイト数 (上記。パッチ後は 3%)
- 再接続時の TLS ハンドシェイク。実機から YouTube へ無効なキーで接続して測ると、rtmp と rtmps の差は **±100 ms 以内 (測定誤差)**。
  T31 での鍵交換も 1〜2 往復も、合わせて 0.1 秒以下。再接続全体 (約 15〜20 秒、下記) の 1〜2%
- パケット数。素の RTMPS は小さな TCP セグメントが多い (毎秒 330 個) ので「落ちる回数」は増えるが、1 個が小さいので
  再送するバイト数は同程度。パッチで解消

## 3. 電波が途切れた時に何が起きるか

### 前提: カメラから見た「上流断」

車載では、カメラ ↔ 車内ルーターの Wi-Fi は生きたまま、ルーターの上流 (LTE) だけが落ちる。カメラには**デフォルトルートが
残っている**ので、`send()` は即エラーにならない。パケットはルーターで捨てられるか ICMP Unreachable が返るが、Linux は
確立済みの TCP 接続では ICMP Unreachable を「ソフトエラー」として扱い (RFC 1122)、接続を切らずに再送を続ける。

即エラーになるのは**カメラ自身の経路が消えた時**だけ (Wi-Fi が切れた、インターフェースが落ちた、デフォルトルートが消えた)。
その場合は ffmpeg がすぐ終了して supervisor が再起動する。supervisor はデフォルトルートが戻るまで起動を待つ
([relay.md](relay.md) の「動作の仕組み」)。

### 実験: 上流断を 90 秒作った

YouTube の IPv6 帯 (`2404:6800::/32`) を blackhole 経路に、IPv4 のデフォルトルートを存在しないゲートウェイに向けて、
「ルーターまでは届くが先に出ない」状態を 90 秒間作った (LAN の ssh はそのまま。netwatch は無効を確認済み)。

| 経過 | 観測 |
|---|---|
| 0 秒 | 遮断。5 秒以内に送信バッファ (約 90KB) が満杯になり、ffmpeg は書き込みで止まる。TCP は再送を続ける。ffmpeg は終了しない |
| 約 45 秒 | ソケットが CLOSE_WAIT に。**YouTube が約 45 秒の無送信でセッションを閉じて FIN を送ってきた** (この実験では受信方向は生きていた) |
| 91 秒 | 経路を復旧 |
| 96 秒 | 再送が YouTube に届き、閉じ済みの接続なので RST が返る → ffmpeg が `Broken pipe` で終了 (rc=224) |
| 98〜103 秒 | supervisor が 2 秒待って ffmpeg を再起動 |
| **111 秒** | 送信再開。**復旧から約 20 秒** |

再起動から送信再開までの約 15 秒の内訳は、2 秒の待ち + RTSP 入力の解析 (約 5 秒) + 接続 + 起動処理。TLS の有無と無関係。

### 読み取れること

- **何分も固まることはない。** 電波が戻れば、次の再送に対して YouTube が RST を返す (セッションを閉じていれば) か、
  そのまま続行する (閉じていなければ)。どちらも数秒で決まる
- YouTube のセッション保持は約 45 秒。**それより短い途切れは再起動なしで回復する**のが最良の挙動で、今の構成はそうなる
- 電波復帰後の余分な待ちは「次の再送まで」。TCP の再送間隔は倍々に伸びる (0.2 秒 → … → 上限 120 秒) ので、
  **10 分を超えるような長い圏外の後は、復帰後に最長 2 分待つ可能性がある**。90 秒の実験では 5 秒だった
- 実際の圏外では受信も止まるので FIN は届かないが、復帰時の RST で同じ結果になる
- `tcp_retries2 = 15` (約 15 分で接続を諦める) が効くのは、15 分間ずっと圏外だった時だけで、それは圏外そのものの時間

## 4. チューニングの選択肢

| 項目 | 今の値 | 変える価値 |
|---|---|---|
| RTMP チャンクサイズ | 4096 (パッチ入り ffmpeg の既定) | 変えない。65536 にしても差は無い |
| RTMPS / 平文 RTMP | `rtmp_url` で選ぶ | 帯域最優先なら平文 (−3%)。キーが平文で流れる。1935 が通らない回線では RTMPS |
| `-rw_timeout` (出力の書き込みタイムアウト) | なし | **長い圏外の後の「最悪 2 分」を 10〜20 秒に縮める。** 代償は、その秒数を超える途切れで必ず再起動する (YouTube の 45 秒以内なら本来は再起動なしで続行できた)。車載で入れるなら **20 秒前後** (`ffmpeg_out_opts` に `-rw_timeout 20000000`。単位はマイクロ秒)。接続自体のタイムアウトは FFmpeg の既定で 5 秒 |
| `tcp_retries2` | 15 (Linux 既定) | 触らない。上記のとおり実害の源ではなく、下げると一時的な遅延で接続を捨てやすくなる |
| netwatch | 無効 ([netwatch.md](netwatch.md)) | **必須。** 有効だとゲートウェイへの ping が約 90 秒通らないだけでカメラが OS ごと再起動する。車内ルーターの上流断でも、ルーター自身が ping に応えれば再起動しないが、応えないルーターなら圏外のたびに再起動する |
| エンコーダのビットレート / fps | 1024kbps / 10fps | 送信量を減らす本命。Thingino 側の設定 |

## 5. 測り方 (再現用)

```sh
# IPv6 の送信量 (YouTube が IPv6 で、他に IPv6 の接続が無ければ ≒ YouTube 分。netstat -tn で確認してから)
a=$(awk '/^Ip6OutOctets/{print $2}' /proc/net/snmp6); sleep 120
b=$(awk '/^Ip6OutOctets/{print $2}' /proc/net/snmp6); echo "$(( (b-a)*8/120/1000 )) kbps"

# IPv4 の場合: IpExt の OutOctets には loopback (RTSP、ほぼ映像のビットレート) も入るので、lo の送信分を引く
out4() { grep '^IpExt:' /proc/net/netstat | awk 'NR==1{for(i=1;i<=NF;i++)h[i]=$i} NR==2{for(i=1;i<=NF;i++) if(h[i]=="OutOctets") print $i}'; }
lo() { awk '/^ *lo:/{print $10}' /proc/net/dev; }
a=$(out4); la=$(lo); sleep 120; b=$(out4); lb=$(lo)
echo "$(( ((b-a)-(lb-la))*8/120/1000 )) kbps"

# TCP 再送の回数 (2 時点の差分。0 なら再送なし)
retrans() { grep '^Tcp:' /proc/net/snmp | awk 'NR==1{for(i=1;i<=NF;i++)h[i]=$i} NR==2{for(i=1;i<=NF;i++) if(h[i]=="RetransSegs") print $i}'; }
r0=$(retrans); sleep 120; r1=$(retrans); echo "$((r1-r0)) retransmits"

# ffmpeg の CPU (120 秒)。/proc/<pid>/stat の utime + stime は USER_HZ (Linux では常に 100) 単位
p=$(pidof ffmpeg); set -- $(cat /proc/$p/stat); t0=$((${14}+${15})); sleep 120
set -- $(cat /proc/$p/stat); echo "$(( (${14}+${15}-t0) / 120 )).$(( ((${14}+${15}-t0) * 10 / 120) % 10 )) %"

# 映像 + 音声そのもののビットレート (PC から、ssh のポートフォワード経由。終わったらトンネルを閉じる)
ssh -f -N -o ExitOnForwardFailure=yes -L 18554:127.0.0.1:554 root@<camera-ip>
ffmpeg -rtsp_transport tcp -i rtsp://thingino:thingino@127.0.0.1:18554/ch0 -t 30 -c copy out.mkv
ffprobe -show_entries format=bit_rate out.mkv
pkill -f '18554:127.0.0.1:554'
```

上流断の再現 (配信が止まる。netwatch が無効なことを先に確認):

```sh
# 元のデフォルトルート (複数あれば全部) を丸ごと控え、中断 (Ctrl-C、ssh 切断) しても trap で必ず戻す。
# 偽の経路は metric を含めて明示的に消す (元の metric が違うと replace では消えず、偽の方が残る)
SAVED=$(ip -4 route show default)
BOGUS="default via 192.168.11.250 dev wlan0 metric 200"              # LAN 内の未使用アドレス (先に ping で確認)
restore() {
	ip route del $BOGUS 2>/dev/null
	echo "$SAVED" | while read -r r; do [ -n "$r" ] && ip route replace $r; done
	ip -6 route del blackhole 2404:6800::/32 2>/dev/null
}
trap restore EXIT INT TERM HUP
ip route replace $BOGUS
ip -6 route add blackhole 2404:6800::/32                            # YouTube の IPv6 帯
sleep 90
restore; trap - EXIT INT TERM HUP
logread | grep youtube-relay | tail
```

ssh が切れても戻るように、実際には上を 1 つのスクリプトにして `nohup` で回した。

偽のゲートウェイを `via` で指す IPv6 経路は、このカーネル (3.10) では選ばれず遮断にならなかった。`blackhole` 型を使う。

## 6. 測っていないこと

- 本物の LTE での実測 (パケットロス率、RTT の揺れ、帯域の変動)。ここでの実験は自宅の Wi-Fi 上で経路を操作した
- ルーターの上流復帰で **カメラの IP や DNS が変わる**場合の挙動 (今の構成では、ルーター配下のカメラのアドレスは変わらない
  想定)
- `-rw_timeout` を入れた状態での上流断の実測
- 上流断の間の ffmpeg → prudynt (RTSP) 側の挙動の詳細。ffmpeg が止まると prudynt の tap が溢れてフレームを捨て、
  復帰後にバッファ分をまとめて送るのは確認できたが、YouTube 側で映像がどう見えるか (遅延の増え方、追いつき方) は未確認
- 10 分を超える圏外の後の復帰時間 (再送間隔が 120 秒まで伸びた状態)
