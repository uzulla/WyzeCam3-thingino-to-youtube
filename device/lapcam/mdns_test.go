package main

import (
	"bytes"
	"encoding/binary"
	"net"
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
	msg = appendName(msg, service)                       // at offset 12
	msg = binary.BigEndian.AppendUint16(msg, typePTR)
	msg = binary.BigEndian.AppendUint16(msg, classIN)
	svc := []byte{0xC0, 12}
	in := append([]byte{byte(len(inst))}, inst...)
	in = append(in, svc...)
	msg = append(msg, rr(svc, typePTR, in)...)
	instAt := len(msg) - len(in)
	instPtr := []byte{0xC0, byte(instAt)}
	srv := []byte{0, 0, 0, 0, 0, 80}
	srv = appendName(srv, hostname)
	msg = append(msg, rr(instPtr, typeSRV, srv)...)
	msg = append(msg, rr(instPtr, typeTXT, []byte(txt))...)
	msg = append(msg, rr(appendName(nil, hostname), typeA, ip.To4())...)
	return msg
}

func TestMDNSBuildQueryAndParse(t *testing.T) {
	want := []byte("\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x06_m5cam\x04_tcp\x05local\x00\x00\x0c\x00\x01")
	if got := buildQuery("_m5cam._tcp.local", typePTR); !bytes.Equal(got, want) {
		t.Errorf("got %q", got)
	}
	recs, err := parseResponse(mdnsAnswer("_m5cam._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x09id=445566\x0bpath=/image"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[0].target != "m5cam-445566._m5cam._tcp.local" || recs[1].port != 80 || recs[1].target != "m5cam-445566.local" ||
		len(recs[2].txt) != 2 || recs[2].txt[1] != "path=/image" || !recs[3].ip.Equal(net.IPv4(192, 168, 11, 113)) {
		t.Errorf("got %+v", recs)
	}
	if recs, err := parseResponse(buildQuery("_http._tcp.local", typePTR)); err != nil || recs != nil {
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
}

func TestRemotesFrom(t *testing.T) {
	from := net.IPv4(10, 0, 0, 9)
	m5, _ := parseResponse(mdnsAnswer("_m5cam._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x0bpath=/image"))
	m5http, _ := parseResponse(mdnsAnswer("_http._tcp.local", "m5cam-445566", "m5cam-445566.local", net.IPv4(192, 168, 11, 113), "\x0bpath=/image"))
	self, _ := parseResponse(mdnsAnswer("_http._tcp.local", "lapcam-9793", "lapcam-9793.local", net.IPv4(192, 168, 11, 101), ""))
	printer, _ := parseResponse(mdnsAnswer("_http._tcp.local", "Printer", "printer.local", net.IPv4(192, 168, 11, 50), ""))
	// heard twice (two queries), under both services; this camera itself left out; others after the m5cam
	got := remotesFrom([]mdnsPacket{{from, m5}, {from, m5http}, {from, self}, {from, printer}, {from, m5}}, "lapcam-9793")
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
	got = remotesFrom([]mdnsPacket{{from, m5[:3]}}, "")
	if len(got) != 1 || got[0].IP != "10.0.0.9" {
		t.Errorf("without A: %+v", got)
	}
	// a PTR for a service nobody asked for is nothing
	other := []record{{name: "_ssh._tcp.local", typ: typePTR, target: "x._ssh._tcp.local"}}
	if got = remotesFrom([]mdnsPacket{{from, other}}, ""); len(got) != 0 {
		t.Errorf("other service: %+v", got)
	}
}
