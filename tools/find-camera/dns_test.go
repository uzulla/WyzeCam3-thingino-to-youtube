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
	b = binary.BigEndian.AppendUint16(b, classIN|0x8000) // cache-flush bit, as cameras send
	b = binary.BigEndian.AppendUint32(b, 120)
	b = binary.BigEndian.AppendUint16(b, uint16(len(data)))
	return append(b, data...)
}

// answer - what a camera sends back, using name compression the way mDNS
// responders do: the instance name points at the service name in the question
func answer() []byte {
	msg := []byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 3} // response, 1 question, 1 answer, 3 additional
	msg = appendName(msg, service)                       // at offset 12
	msg = binary.BigEndian.AppendUint16(msg, typePTR)
	msg = binary.BigEndian.AppendUint16(msg, classIN)
	svc := []byte{0xC0, 12}
	inst := append([]byte{3}, "Cam"...) // "Cam" + pointer to the service name
	inst = append(inst, svc...)
	msg = append(msg, rr(svc, typePTR, inst)...)
	instAt := len(msg) - len(inst)
	instPtr := []byte{0xC0, byte(instAt)}
	srv := []byte{0, 0, 0, 0, 0, 80}
	srv = appendName(srv, "cam.local")
	msg = append(msg, rr(instPtr, typeSRV, srv)...)
	msg = append(msg, rr(instPtr, typeTXT, []byte("\x0bproduct=t31\x0dversion=ciao1"))...)
	msg = append(msg, rr(appendName(nil, "CAM.local"), typeA, []byte{192, 168, 1, 5})...)
	return msg
}

func TestBuildQuery(t *testing.T) {
	want := []byte("\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x09_thingino\x04_tcp\x05local\x00\x00\x0c\x00\x01")
	if got := buildQuery(service, typePTR); !bytes.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestParseResponse(t *testing.T) {
	recs, err := parseResponse(answer())
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("got %d records: %+v", len(recs), recs)
	}
	ptr, srv, txt, a := recs[0], recs[1], recs[2], recs[3]
	if ptr.typ != typePTR || ptr.name != service || ptr.target != "cam."+service {
		t.Errorf("PTR %+v", ptr)
	}
	if srv.name != "cam."+service || srv.target != "cam.local" || srv.port != 80 {
		t.Errorf("SRV %+v", srv)
	}
	if len(txt.txt) != 2 || txt.txt[0] != "product=t31" || txt.txt[1] != "version=ciao1" {
		t.Errorf("TXT %+v", txt)
	}
	if a.name != "cam.local" || !a.ip.Equal(net.IPv4(192, 168, 1, 5)) {
		t.Errorf("A %+v", a)
	}
}

func TestParseQueryAndBroken(t *testing.T) {
	if recs, err := parseResponse(buildQuery(service, typePTR)); err != nil || recs != nil {
		t.Errorf("a query must give nothing: %v %v", recs, err)
	}
	full := answer()
	for n := 12; n < len(full); n++ {
		if _, err := parseResponse(full[:n]); err == nil {
			t.Fatalf("truncated to %d bytes: no error", n)
		}
	}
	loop := []byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0xC0, 12}
	if _, err := parseResponse(loop); err == nil {
		t.Error("compression loop must be an error")
	}
}

func TestCameras(t *testing.T) {
	recs, _ := parseResponse(answer())
	from := net.IPv4(10, 0, 0, 9)
	// the same answer heard on two interfaces
	cams := cameras([]packet{{from, recs}, {from, recs}})
	if len(cams) != 1 || cams[0].name != "cam" || cams[0].ip != "192.168.1.5" || cams[0].info != "t31 ciao1" {
		t.Errorf("got %+v", cams)
	}
	// no A record: the address the answer came from
	cams = cameras([]packet{{from, recs[:3]}})
	if len(cams) != 1 || cams[0].ip != "10.0.0.9" {
		t.Errorf("without A: %+v", cams)
	}
	// a PTR for another service is not a camera
	other := []record{{name: "_http._tcp.local", typ: typePTR, target: "x._http._tcp.local"}}
	if cams = cameras([]packet{{from, other}}); len(cams) != 0 {
		t.Errorf("other service: %+v", cams)
	}
}
