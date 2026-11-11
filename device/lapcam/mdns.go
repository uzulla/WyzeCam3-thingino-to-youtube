package main

// Finding other devices on the LAN by mDNS, for the OSD tab's remote camera:
// a browse for _m5cam._tcp (uzulla/m5camera-ble-wifi) and _http._tcp, with
// each answer's hostname, IPv4 address, port and TXT "path", so that the
// URL can be made from what is actually on the network. Just enough of the
// DNS message format (RFC 1035 as mDNS uses it, RFC 6762) is here: one
// question out, PTR / SRV / TXT / A records back (the same code as
// tools/find-camera and device/osd-feed).
//
// Anyone on the LAN can answer, so nothing from an answer is trusted as it
// is: hostnames must be plain .local names, paths must be paths, addresses
// must be unicast ones, and a message is read within fixed bounds (name
// length, pointer direction, record and packet counts).

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

	browseTime    = 2 * time.Second
	browseReuse   = 3 * time.Second // a second tap within this gets the last result
	maxPackets    = 256             // answers kept per browse (a flood is cut here)
	maxRecords    = 64              // records read per message
	maxNameLen    = 255             // RFC 1035
	maxPathLen    = 256
	remotePathDef = "/"
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
	ip     net.IP   // A (4 bytes)
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
	from    *net.UDPAddr
	records []record
}

// one browse at a time; a tap right after another gets the same answer
// instead of another two seconds of sockets and multicast
var browseMu struct {
	sync.Mutex
	at    time.Time
	hosts []remoteHost
	err   error
}

// browseRemotes - ask the LAN for remoteServices and collect the answers
// for browseTime (or until ctx is done). The camera's own services are
// left out.
func browseRemotes(ctx context.Context) ([]remoteHost, error) {
	browseMu.Lock()
	defer browseMu.Unlock()
	if time.Since(browseMu.at) < browseReuse {
		return browseMu.hosts, browseMu.err
	}
	hosts, err := browseOnce(ctx)
	browseMu.at, browseMu.hosts, browseMu.err = time.Now(), hosts, err
	return hosts, err
}

func browseOnce(ctx context.Context) ([]remoteHost, error) {
	conns, lerr := mdnsListen()
	if len(conns) == 0 {
		return nil, errors.New("no network interface to search on: " + lerr)
	}
	ctx, cancel := context.WithTimeout(ctx, browseTime)
	defer cancel()
	// the readers stop at the deadline, or at once when ctx is cancelled
	// (the browser went away): closing the sockets ends their reads
	go func() {
		<-ctx.Done()
		for _, c := range conns {
			c.Close()
		}
	}()
	packets := make(chan mdnsPacket, 64)
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			buf := make([]byte, 9000)
			for {
				n, from, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if from.Port != 5353 {
					continue // mDNS responders answer from 5353 (RFC 6762 6.7); anything else is noise
				}
				if recs, err := parseResponse(buf[:n]); err == nil && len(recs) > 0 {
					select {
					case packets <- mdnsPacket{from: from, records: recs}:
					case <-ctx.Done():
						return
					}
				}
			}
		}(c)
	}
	go func() { wg.Wait(); close(packets) }()
	go func() {
		// twice, a second apart: a device that missed the first query (or
		// whose answer was lost) gets another chance
		for _, after := range []time.Duration{0, time.Second} {
			select {
			case <-time.After(after):
			case <-ctx.Done():
				return
			}
			for _, svc := range remoteServices {
				q, err := buildQuery(svc, typePTR)
				if err != nil {
					continue
				}
				for _, c := range conns {
					c.WriteToUDP(q, mdnsGroup)
				}
			}
		}
	}()
	var all []mdnsPacket
	for p := range packets {
		if len(all) < maxPackets {
			all = append(all, p)
		}
	}
	self, _ := os.Hostname()
	return remotesFrom(all, strings.ToLower(self), ownAddrs()), nil
}

// mdnsListen - a multicast socket on every usable IPv4 interface (on the
// camera: wlan0), and the last error for the message when there is none
func mdnsListen() ([]*net.UDPConn, string) {
	ifaces, _ := net.Interfaces()
	var out []*net.UDPConn
	lastErr := "no interface is up"
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, mdnsGroup)
		if err != nil {
			lastErr = ifi.Name + ": " + err.Error()
			continue
		}
		out = append(out, c)
	}
	return out, lastErr
}

// ownAddrs - this camera's own IPv4 addresses (to leave itself out of the
// list whatever name it advertises under)
func ownAddrs() map[string]bool {
	out := map[string]bool{}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			out[n.IP.String()] = true
		}
	}
	return out
}

// remotesFrom - the devices in the answers: one per service instance with
// an SRV record naming a plain .local host, the address from that host's A
// record (or where the answer came from), leaving out this camera's own
// (self is its hostname, lower case; own its addresses)
func remotesFrom(packets []mdnsPacket, self string, own map[string]bool) []remoteHost {
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
					instances[r.target] = inst{kind: kindOf[r.name], from: p.from.IP}
				}
			case r.typ == typeSRV && validLocalHost(r.target):
				host[r.name] = r.target
				port[r.name] = int(r.port)
			case r.typ == typeTXT:
				txt[r.name] = r.txt
			case r.typ == typeA && usableAddr(r.ip):
				if old, ok := addr[r.name]; !ok || old.IsLinkLocalUnicast() {
					addr[r.name] = r.ip // a routable address beats a 169.254 one
				}
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
			if h == "" || seen[h] || strings.TrimSuffix(h, ".local") == self {
				continue // no SRV with a usable host: nothing to make a URL from
			}
			ip := in.from
			if a, ok := addr[h]; ok {
				ip = a
			}
			if !usableAddr(ip) || own[ip.String()] {
				continue
			}
			seen[h] = true
			r := remoteHost{Name: strings.TrimSuffix(name, "."+svc), Host: h, IP: ip.String(), Port: port[name], Kind: kind}
			if r.Port == 0 {
				r.Port = 80
			}
			for _, t := range txt[name] {
				if strings.HasPrefix(t, "path=") {
					r.Path = cleanPath(strings.TrimPrefix(t, "path="))
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

// validLocalHost - a plain mDNS hostname: labels of letters, digits and
// hyphens, ending in .local (anything else cannot become a URL's host)
func validLocalHost(h string) bool {
	if !strings.HasSuffix(h, ".local") || len(h) > maxNameLen {
		return false
	}
	for _, l := range strings.Split(strings.TrimSuffix(h, ".local"), ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// usableAddr - an IPv4 address a URL may point at: unicast, not this
// machine's loopback, not "any", not multicast
func usableAddr(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && !ip4.IsLoopback() && !ip4.IsUnspecified() && !ip4.IsMulticast() && !ip4.Equal(net.IPv4bcast)
}

// cleanPath - a TXT path= as a URL path: starts with "/", printable, no
// whitespace or fragment; "" when it is not that
func cleanPath(p string) string {
	if p == "" || p[0] != '/' || len(p) > maxPathLen {
		return ""
	}
	for _, c := range p {
		if c <= ' ' || c == 0x7f || c == '#' || c == '\\' {
			return ""
		}
	}
	return p
}

// --- DNS message format (as tools/find-camera/dns.go) ---

// buildQuery - a query for one name and type (id 0, as mDNS queries have);
// an error for a name DNS cannot carry (an empty label or one over 63 bytes)
func buildQuery(name string, typ uint16) ([]byte, error) {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
	var err error
	if b, err = appendName(b, name); err != nil {
		return nil, err
	}
	b = binary.BigEndian.AppendUint16(b, typ)
	return binary.BigEndian.AppendUint16(b, classIN), nil
}

// appendName - a name in DNS label form
func appendName(b []byte, name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	if len(name) > maxNameLen-1 {
		return nil, errors.New("DNS name too long: " + name)
	}
	for _, l := range strings.Split(name, ".") {
		if l == "" || len(l) > 63 {
			return nil, errors.New("bad DNS name: " + name)
		}
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0), nil
}

// parseResponse - the answer, authority and additional records of a response.
// A query (our own, looped back to us, or another host's) gives none. At
// most maxRecords records are read; what is beyond is left unread.
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
	for i := 0; i < questions && i < maxRecords; i++ {
		_, end, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		off = end + 4 // type, class
	}
	var out []record
	for i := 0; i < rrs && i < maxRecords; i++ {
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
		case typeA:
			if len(data) == 4 {
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

// readName - the name at off, following compression pointers (only
// backwards, as RFC 1035 means them, so no loop and no blow-up), and the
// offset just after it in the message
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end, total := -1, 0
	for {
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
			ptr := int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
			if ptr >= off {
				return "", 0, errors.New("DNS name pointer forwards")
			}
			off = ptr
		case l&0xC0 != 0:
			return "", 0, errors.New("bad DNS label")
		default:
			if off+1+l > len(msg) {
				return "", 0, errShort
			}
			if total += l + 1; total > maxNameLen {
				return "", 0, errors.New("DNS name too long")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
