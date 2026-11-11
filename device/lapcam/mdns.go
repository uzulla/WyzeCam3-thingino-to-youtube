package main

// Finding other devices on the LAN by mDNS, for the OSD tab's remote camera:
// a browse for _m5cam._tcp (uzulla/m5camera-ble-wifi) and _http._tcp, with
// each answer's hostname, IPv4 address, port and TXT "path", so that the
// URL can be made from what is actually on the network. The camera cannot
// resolve .local names itself (Thingino has no mDNS resolver), which is why
// the address is what the list hands out. Just enough of the DNS message
// format (RFC 1035 as mDNS uses it, RFC 6762) is here: one question out,
// PTR / SRV / TXT / A records back (the same code as tools/find-camera).

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	typeA    = 1
	typePTR  = 12
	typeTXT  = 16
	typeAAAA = 28
	typeSRV  = 33
	classIN  = 1

	browseTime = 2 * time.Second
)

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// the services browsed, most specific first (a host found by several keeps
// the first one's kind)
var remoteServices = []string{"_m5cam._tcp.local", "_http._tcp.local"}

var errShort = errors.New("truncated DNS message")

type record struct {
	name string // lower case, no trailing dot
	typ  uint16
	// set according to typ
	target string   // PTR, SRV (lower case)
	port   uint16   // SRV
	txt    []string // TXT
	ip     net.IP   // A, AAAA
}

// remoteHost - one device found, as the OSD tab shows it
type remoteHost struct {
	Name string `json:"name"` // the service instance name (m5cam-445566)
	Host string `json:"host"` // its hostname (m5cam-445566.local)
	IP   string `json:"ip"`
	Port int    `json:"port"`
	Path string `json:"path,omitempty"` // TXT path=, where the picture is
	Kind string `json:"kind"`           // m5cam or http
}

type mdnsPacket struct {
	from    net.IP
	records []record
}

// browseRemotes - ask the LAN for remoteServices and collect the answers
// for browseTime (or until ctx is done). The camera's own services are
// left out.
func browseRemotes(ctx context.Context) ([]remoteHost, error) {
	conns := mdnsListen()
	if len(conns) == 0 {
		return nil, errors.New("no network interface to search on")
	}
	deadline := time.Now().Add(browseTime)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	packets := make(chan mdnsPacket, 64)
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			defer c.Close()
			c.SetReadDeadline(deadline)
			buf := make([]byte, 9000)
			for {
				n, from, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if recs, err := parseResponse(buf[:n]); err == nil && len(recs) > 0 {
					packets <- mdnsPacket{from: from.IP, records: recs}
				}
			}
		}(c)
	}
	go func() { wg.Wait(); close(packets) }()
	go func() {
		// twice, a second apart: a device that missed the first query (or
		// whose answer was lost) gets another chance
		for _, after := range []time.Duration{0, time.Second} {
			time.Sleep(after)
			for _, svc := range remoteServices {
				q := buildQuery(svc, typePTR)
				for _, c := range conns {
					c.WriteToUDP(q, mdnsGroup)
				}
			}
		}
	}()
	var all []mdnsPacket
	for p := range packets {
		all = append(all, p)
	}
	self, _ := os.Hostname()
	return remotesFrom(all, strings.ToLower(self)), nil
}

// mdnsListen - a multicast socket on every usable IPv4 interface (on the
// camera: wlan0)
func mdnsListen() []*net.UDPConn {
	ifaces, _ := net.Interfaces()
	var out []*net.UDPConn
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, mdnsGroup)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

// remotesFrom - the devices in the answers: one per service instance, with
// the address from its A record (or where the answer came from), leaving
// out this camera's own (self is its hostname, lower case)
func remotesFrom(packets []mdnsPacket, self string) []remoteHost {
	type inst struct {
		kind string
		from net.IP
	}
	instances := map[string]inst{} // instance name -> kind, address the PTR came from
	host := map[string]string{}    // instance -> SRV target
	port := map[string]int{}       // instance -> SRV port
	txt := map[string][]string{}   // instance -> TXT strings
	addr := map[string]net.IP{}    // host -> IPv4 address
	kindOf := map[string]string{}  // service name -> kind
	for _, svc := range remoteServices {
		kindOf[svc] = strings.TrimPrefix(strings.SplitN(svc, ".", 2)[0], "_")
	}
	for _, p := range packets {
		for _, r := range p.records {
			switch {
			case r.typ == typePTR && kindOf[r.name] != "":
				if _, seen := instances[r.target]; !seen {
					instances[r.target] = inst{kind: kindOf[r.name], from: p.from}
				}
			case r.typ == typeSRV:
				host[r.name] = r.target
				port[r.name] = int(r.port)
			case r.typ == typeTXT:
				txt[r.name] = r.txt
			case r.typ == typeA && r.ip.To4() != nil:
				addr[r.name] = r.ip
			}
		}
	}
	out := []remoteHost{}
	seen := map[string]bool{} // one line per host, the most specific service first
	for _, svc := range remoteServices {
		kind := kindOf[svc]
		for name, in := range instances {
			if in.kind != kind {
				continue
			}
			h := host[name]
			if h == "" {
				h = strings.TrimSuffix(name, "."+svc) + ".local"
			}
			if seen[h] || strings.TrimSuffix(h, ".local") == self {
				continue
			}
			seen[h] = true
			ip := in.from
			if a, ok := addr[h]; ok {
				ip = a
			}
			r := remoteHost{Name: strings.TrimSuffix(name, "."+svc), Host: h, IP: ip.String(), Port: port[name], Kind: kind}
			if r.Port == 0 {
				r.Port = 80
			}
			for _, t := range txt[name] {
				if strings.HasPrefix(t, "path=") {
					r.Path = strings.TrimPrefix(t, "path=")
				}
			}
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "m5cam"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// --- DNS message format (as tools/find-camera/dns.go) ---

// buildQuery - a query for one name and type (id 0, as mDNS queries have)
func buildQuery(name string, typ uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
	b = appendName(b, name)
	b = binary.BigEndian.AppendUint16(b, typ)
	return binary.BigEndian.AppendUint16(b, classIN)
}

// appendName - a name in DNS label form
func appendName(b []byte, name string) []byte {
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

// parseResponse - the answer, authority and additional records of a response.
// A query (our own, looped back to us, or another host's) gives none.
func parseResponse(msg []byte) ([]record, error) {
	if len(msg) < 12 {
		return nil, errShort
	}
	if msg[2]&0x80 == 0 {
		return nil, nil
	}
	be := binary.BigEndian
	questions := int(be.Uint16(msg[4:]))
	rrs := int(be.Uint16(msg[6:])) + int(be.Uint16(msg[8:])) + int(be.Uint16(msg[10:]))
	off := 12
	for i := 0; i < questions; i++ {
		_, end, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		off = end + 4 // type, class
	}
	var out []record
	for i := 0; i < rrs; i++ {
		name, end, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		if end+10 > len(msg) {
			return nil, errShort
		}
		r := record{name: strings.ToLower(name), typ: be.Uint16(msg[end:])}
		rd, rdlen := end+10, int(be.Uint16(msg[end+8:]))
		if rd+rdlen > len(msg) {
			return nil, errShort
		}
		data := msg[rd : rd+rdlen]
		switch r.typ {
		case typeA, typeAAAA:
			if len(data) == 4 || len(data) == 16 {
				r.ip = append(net.IP(nil), data...)
			}
		case typePTR:
			r.target, _, err = readName(msg, rd)
		case typeSRV:
			if len(data) < 7 {
				return nil, errShort
			}
			r.port = be.Uint16(data[4:])
			r.target, _, err = readName(msg, rd+6)
		case typeTXT:
			for p := 0; p < len(data); {
				l := int(data[p])
				if p+1+l > len(data) {
					return nil, errShort
				}
				r.txt = append(r.txt, string(data[p+1:p+1+l]))
				p += 1 + l
			}
		}
		if err != nil {
			return nil, err
		}
		r.target = strings.ToLower(r.target)
		out = append(out, r)
		off = rd + rdlen
	}
	return out, nil
}

// readName - the name at off, following compression pointers, and the offset
// just after it in the message
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; ; {
		if off >= len(msg) {
			return "", 0, errShort
		}
		l := int(msg[off])
		switch {
		case l == 0:
			if end < 0 {
				end = off + 1
			}
			return strings.Join(labels, "."), end, nil
		case l&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return "", 0, errShort
			}
			if end < 0 {
				end = off + 2
			}
			if jumps++; jumps > 32 {
				return "", 0, errors.New("DNS name compression loop")
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
		case l&0xC0 != 0:
			return "", 0, errors.New("bad DNS label")
		default:
			if off+1+l > len(msg) {
				return "", 0, errShort
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
