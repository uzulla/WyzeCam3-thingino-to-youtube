package main

// A ".local" name in a picture URL (m5cam-06f808.local, say) resolved by
// mDNS here, because the camera's own resolver only knows DNS: an A query
// to 224.0.0.251:5353 on every interface (sent twice, half a second apart,
// as a multicast query is easily lost on Wi-Fi), the first usable A record
// from a responder wins. Results are cached; a fetch that fails forgets its
// host, so the next one asks again (the device may have a new address).
// Just enough of the DNS message format (RFC 1035 / 6762) for one question
// and the A records of the answers; the same code as tools/find-camera and
// lapcam. Anyone on the LAN can answer, so a message is read within fixed
// bounds and only a unicast address from port 5353 is believed.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	typeA   = 1
	classIN = 1

	mdnsWait   = 1500 * time.Millisecond // how long one lookup waits for an answer
	mdnsResend = 500 * time.Millisecond  // the query again after this, in case the first was lost
	mdnsCache  = 5 * time.Minute         // how long a found address is trusted
	maxNameLen = 255                     // RFC 1035
	maxRecords = 64                      // records read per message
)

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// isLocalName - a name mDNS is for (host.local), not an address
func isLocalName(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return strings.HasSuffix(host, ".local") && net.ParseIP(host) == nil
}

// localResolver - lookups with a cache; lookupFn is swapped in tests
type localResolver struct {
	mu       sync.Mutex
	cache    map[string]localEntry
	lookupFn func(ctx context.Context, host string) (net.IP, error)
}

type localEntry struct {
	ip net.IP
	at time.Time
}

var resolver = &localResolver{cache: map[string]localEntry{}, lookupFn: mdnsLookup}

// lookup - the address for host (lower case), from the cache while fresh
func (r *localResolver) lookup(ctx context.Context, host string) (net.IP, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	r.mu.Lock()
	e, ok := r.cache[host]
	r.mu.Unlock()
	if ok && time.Since(e.at) < mdnsCache {
		return e.ip, nil
	}
	ip, err := r.lookupFn(ctx, host)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[host] = localEntry{ip: ip, at: time.Now()}
	r.mu.Unlock()
	return ip, nil
}

// forget - drop host from the cache (after a failed fetch: ask again next time)
func (r *localResolver) forget(host string) {
	r.mu.Lock()
	delete(r.cache, strings.ToLower(strings.TrimSuffix(host, ".")))
	r.mu.Unlock()
}

// dialLocal - a DialContext for http.Transport that resolves .local hosts
// here and leaves everything else to the system
func dialLocal(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	host, port, err := net.SplitHostPort(addr)
	if err == nil && isLocalName(host) {
		ip, err := resolver.lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", host, err)
		}
		addr = net.JoinHostPort(ip.String(), port)
	}
	return d.DialContext(ctx, network, addr)
}

// mdnsLookup - an A query on every usable interface, the first usable A
// record for host in the answers (within mdnsWait, or ctx). Every socket
// is closed before this returns.
func mdnsLookup(ctx context.Context, host string) (net.IP, error) {
	q, err := buildQuery(host, typeA)
	if err != nil {
		return nil, err
	}
	conns := mdnsConns()
	if len(conns) == 0 {
		return nil, errors.New("mDNS: no network interface")
	}
	ctx, cancel := context.WithTimeout(ctx, mdnsWait)
	defer cancel()
	closeAll := func() {
		for _, c := range conns {
			c.Close()
		}
	}
	go func() { <-ctx.Done(); closeAll() }()
	found := make(chan net.IP, len(conns))
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
					continue // responders answer from 5353 (RFC 6762 6.7)
				}
				if ip := aRecordFor(buf[:n], host); ip != nil {
					found <- ip
					return
				}
			}
		}(c)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	send := func() {
		for _, c := range conns {
			c.WriteToUDP(q, mdnsGroup)
		}
	}
	send()
	resend := time.After(mdnsResend)
	for {
		select {
		case ip := <-found:
			cancel() // closes the sockets, the other readers end
			return ip, nil
		case <-resend:
			send()
			resend = nil
		case <-done:
			select {
			case ip := <-found:
				return ip, nil
			default:
			}
			return nil, fmt.Errorf("mDNS: no answer for %s within %v (not on this network?)", host, mdnsWait)
		}
	}
}

// mdnsConns - a multicast socket on every usable IPv4 interface
func mdnsConns() []*net.UDPConn {
	ifaces, _ := net.Interfaces()
	var out []*net.UDPConn
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if c, err := net.ListenMulticastUDP("udp4", ifi, mdnsGroup); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// usableAddr - an IPv4 address worth connecting to: unicast, not loopback,
// not "any", not multicast (a lying responder could name those)
func usableAddr(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && !ip4.IsLoopback() && !ip4.IsUnspecified() && !ip4.IsMulticast() && !ip4.Equal(net.IPv4bcast)
}

// --- DNS message format ---

// buildQuery - a query for one name and type (id 0, as mDNS queries have);
// an error for a name DNS cannot carry (an empty label or one over 63 bytes)
func buildQuery(name string, typ uint16) ([]byte, error) {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
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
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, typ)
	return binary.BigEndian.AppendUint16(b, classIN), nil
}

// aRecordFor - the usable IPv4 address in an A record for host (lower
// case) in a response, or nil (a query, another host, a broken message)
func aRecordFor(msg []byte, host string) net.IP {
	if len(msg) < 12 || msg[2]&0x80 == 0 {
		return nil
	}
	be := binary.BigEndian
	questions := int(be.Uint16(msg[4:]))
	rrs := int(be.Uint16(msg[6:])) + int(be.Uint16(msg[8:])) + int(be.Uint16(msg[10:]))
	off := 12
	for i := 0; i < questions && i < maxRecords; i++ {
		_, end, err := readName(msg, off)
		if err != nil {
			return nil
		}
		off = end + 4
	}
	for i := 0; i < rrs && i < maxRecords; i++ {
		name, end, err := readName(msg, off)
		if err != nil || end+10 > len(msg) {
			return nil
		}
		typ := be.Uint16(msg[end:])
		rd, rdlen := end+10, int(be.Uint16(msg[end+8:]))
		if rd+rdlen > len(msg) {
			return nil
		}
		if typ == typeA && rdlen == 4 && strings.ToLower(name) == host {
			if ip := append(net.IP(nil), msg[rd:rd+4]...); usableAddr(ip) {
				return ip
			}
		}
		off = rd + rdlen
	}
	return nil
}

// readName - the name at off, following compression pointers (only
// backwards, as RFC 1035 means them, so no loop and no blow-up), and the
// offset just after it in the message
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end, total := -1, 0
	for {
		if off >= len(msg) {
			return "", 0, errors.New("truncated DNS message")
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
				return "", 0, errors.New("truncated DNS message")
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
				return "", 0, errors.New("truncated DNS message")
			}
			if total += l + 1; total > maxNameLen {
				return "", 0, errors.New("DNS name too long")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
