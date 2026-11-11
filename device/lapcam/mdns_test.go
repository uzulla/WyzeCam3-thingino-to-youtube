package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

// rr - one resource record: name (already encoded), type, class, TTL, data
func rr(name []byte, typ uint16, data []byte) []byte {
	b := append([]byte(nil), name...)
	b = binary.BigEndian.AppendUint16(b, typ)
	b = binary.BigEndian.AppendUint16(b, classIN|0x8000) // cache-flush bit, as devices send
	b = binary.BigEndian.AppendUint32(b, 120)
	b = binary.BigEndian.AppendUint16(b, uint16(len(data)))
	return append(b, data...)
}

// mdnsAnswer - what an m5camera sends back for a _m5cam._tcp browse, with
// name compression the way responders use it
func mdnsAnswer(service, inst, hostname string, ip net.IP, txt string) []byte {
	msg := []byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 3} // response, 1 question, 1 answer, 3 additional
	msg = appendNameT(service)                           // at offset 12
	msg = append([]byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 3}, msg...)
	msg = binary.BigEndian.AppendUint16(msg, typePTR)
	msg = binary.BigEndian.AppendUint16(msg, classIN)
	svc := []byte{0xC0, 12}
	in := append([]byte{byte(len(inst))}, inst...)
	in = append(in, svc...)
	msg = append(msg, rr(svc, typePTR, in)...)
	instAt := len(msg) - len(in)
	instPtr := []byte{0xC0, byte(instAt)}
	srv := []byte{0, 0, 0, 0, 0, 80}
	srv = append(srv, appendNameT(hostname)...)
	msg = append(msg, rr(instPtr, typeSRV, srv)...)
	msg = append(msg, rr(instPtr, typeTXT, []byte(txt))...)
	msg = append(msg, rr(appendNameT(hostname), typeA, ip.To4())...)
	return msg
}

func TestMDNSBuildQueryAndParse(t *testing.T) {
	want := []byte("\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x06_m5cam\x04_tcp\x05local\x00\x00\x0c\x00\x01")
	if got, err := buildQuery("_m5cam._tcp.local", typePTR); err != nil || !bytes.Equal(got, want) {
		t.Errorf("got %q %v", got, err)
	}
	if _, err := buildQuery("a..local", typePTR); err == nil {
		t.Error("an empty label cannot be queried")
	}
	recs, err := parseResponse(mdnsAnswer("_m5cam._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x09id=445566\x0bpath=/image"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[0].target != "m5cam-445566._m5cam._tcp.local" || recs[1].port != 80 || recs[1].target != "m5cam-445566.local" ||
		len(recs[2].txt) != 2 || recs[2].txt[1] != "path=/image" || !recs[3].ip.Equal(net.IPv4(192, 168, 11, 113)) {
		t.Errorf("got %+v", recs)
	}
	q, _ := buildQuery("_http._tcp.local", typePTR)
	if recs, err := parseResponse(q); err != nil || recs != nil {
		t.Errorf("a query must give nothing: %v %v", recs, err)
	}
	full := mdnsAnswer("_m5cam._tcp.local", "x", "x.local", net.IPv4(1, 2, 3, 4), "")
	for n := 12; n < len(full); n++ {
		if _, err := parseResponse(full[:n]); err == nil {
			t.Fatalf("truncated to %d bytes: no error", n)
		}
	}
	if _, err := parseResponse([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xC0, 12}); err == nil {
		t.Error("compression loop must be an error")
	}
	// a pointer forwards (what a blow-up needs) and a name over 255 bytes are refused
	fwd := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xC0, 14, 1, 'a', 0}
	if _, err := parseResponse(fwd); err == nil {
		t.Error("a forward pointer must be an error")
	}
	long := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}
	for i := 0; i < 5; i++ {
		long = append(long, 63)
		long = append(long, strings.Repeat("a", 63)...)
	}
	long = append(long, 0)
	if _, err := parseResponse(long); err == nil {
		t.Error("a 320-byte name must be an error")
	}
	// a flood of records: only maxRecords are read, the rest ignored without error
	many := []byte{0, 0, 0x84, 0, 0, 0, 0xff, 0xff, 0, 0, 0, 0}
	for i := 0; i < 100; i++ {
		many = append(many, rr(appendNameT("h.local"), typeA, []byte{10, 0, 0, byte(i)})...)
	}
	recs, err = parseResponse(many)
	if err != nil || len(recs) != maxRecords {
		t.Errorf("flood: %d records, %v", len(recs), err)
	}
}

func appendNameT(name string) []byte { b, _ := appendName(nil, name); return b }

func TestRemotesFrom(t *testing.T) {
	from := net.IPv4(10, 0, 0, 9)
	m5, _ := parseResponse(mdnsAnswer("_m5cam._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x0bpath=/image"))
	m5http, _ := parseResponse(mdnsAnswer("_http._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x0bpath=/image"))
	self, _ := parseResponse(mdnsAnswer("_http._tcp.local", "lapcam-9793", "lapcam-9793.local", net.IPv4(192, 168, 11, 101), ""))
	printer, _ := parseResponse(mdnsAnswer("_http._tcp.local", "Printer", "printer.local", net.IPv4(192, 168, 11, 50), ""))
	// heard twice (two queries), under both services; this camera itself left out; others after the m5cam
	own := map[string]bool{"192.168.11.101": true}
	got := remotesFrom(pk(from, m5, m5http, self, printer, m5), "lapcam-9793", own)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "m5cam-445566" || got[0].Host != "m5cam-445566.local" || got[0].IP != "192.168.11.113" || got[0].Port != 80 || got[0].Path != "/image" || got[0].Kind != "m5cam" {
		t.Errorf("m5cam: %+v", got[0])
	}
	if got[1].Name != "printer" || got[1].IP != "192.168.11.50" || got[1].Kind != "http" || got[1].Path != "" {
		t.Errorf("printer: %+v", got[1])
	}
	// no A record: the address the answer came from
	got = remotesFrom(pk(from, m5[:3]), "", nil)
	if len(got) != 1 || got[0].IP != "10.0.0.9" {
		t.Errorf("without A: %+v", got)
	}
	// a PTR for a service nobody asked for is nothing; so is an instance without SRV
	other := []record{{name: "_ssh._tcp.local", typ: typePTR, target: "x._ssh._tcp.local"}}
	if got = remotesFrom(pk(from, other), "", nil); len(got) != 0 {
		t.Errorf("other service: %+v", got)
	}
	if got = remotesFrom(pk(from, m5[:1]), "", nil); len(got) != 0 {
		t.Errorf("PTR without SRV: %+v", got)
	}
	// this camera's own address under another name is left out too
	if got = remotesFrom(pk(from, printer), "", map[string]bool{"192.168.11.50": true}); len(got) != 0 {
		t.Errorf("own address: %+v", got)
	}
	// what a hostile answer may carry: a host that is not a plain .local name, a path that is
	// not a path, an address that is not one to connect to
	for name, pkt := range map[string][]byte{
		"host not local":  mdnsAnswer("_http._tcp.local", "x", "evil.example.com", net.IPv4(1, 2, 3, 4), ""),
		"host with slash": mdnsAnswer("_http._tcp.local", "x", "a/b.local", net.IPv4(1, 2, 3, 4), ""),
		"host with at":    mdnsAnswer("_http._tcp.local", "x", "a@b.local", net.IPv4(1, 2, 3, 4), ""),
	} {
		recs, _ := parseResponse(pkt)
		if got := remotesFrom(pk(from, recs), "", nil); len(got) != 0 {
			t.Errorf("%s: must be left out, got %+v", name, got)
		}
	}
	// an A record that lies (loopback, multicast) is ignored: the sender's own address is used instead
	for _, bad := range []net.IP{net.IPv4(127, 0, 0, 1), net.IPv4(224, 0, 0, 1), net.IPv4(0, 0, 0, 0)} {
		recs, _ := parseResponse(mdnsAnswer("_http._tcp.local", "x", "x.local", bad, ""))
		if got := remotesFrom(pk(from, recs), "", nil); len(got) != 1 || got[0].IP != "10.0.0.9" {
			t.Errorf("lying A %v: %+v", bad, got)
		}
	}
	for txt, want := range map[string]string{
		"\x0bpath=/image": "/image", "\x14path=@evil.example/x": "", "\x09path=/a b": "", "\x0dpath=/image#f": "", "\x05path=": "",
	} {
		recs, _ := parseResponse(mdnsAnswer("_m5cam._tcp.local", "x", "x.local", net.IPv4(10, 0, 0, 1), txt))
		if got := remotesFrom(pk(from, recs), "", nil); len(got) != 1 || got[0].Path != want {
			t.Errorf("txt %q: want path %q, got %+v", txt, want, got)
		}
	}
	// a routable address beats a link-local one for the same host
	recs, _ := parseResponse(mdnsAnswer("_m5cam._tcp.local", "x", "x.local", net.IPv4(169, 254, 1, 2), ""))
	recs2, _ := parseResponse(mdnsAnswer("_m5cam._tcp.local", "x", "x.local", net.IPv4(10, 0, 0, 7), ""))
	if got = remotesFrom(pk(from, recs, recs2), "", nil); len(got) != 1 || got[0].IP != "10.0.0.7" {
		t.Errorf("link-local vs routable: %+v", got)
	}
}

// pk - packets from one source
func pk(from net.IP, recs ...[]record) []mdnsPacket {
	out := []mdnsPacket{}
	for _, r := range recs {
		out = append(out, mdnsPacket{from: &net.UDPAddr{IP: from, Port: 5353}, records: r})
	}
	return out
}
