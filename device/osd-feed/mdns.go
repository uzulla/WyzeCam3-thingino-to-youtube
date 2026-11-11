package main

// A ".local" name in a picture URL (m5cam-06f808.local, say) resolved by
// mDNS here, because the camera's own resolver only knows DNS: an A query
// to 224.0.0.251:5353 on every interface, the first matching A record
// wins. Results are cached; a fetch that fails forgets its host, so the next
// one asks again (the device may have a new address). Just enough of the
// DNS message format (RFC 1035 / 6762) for one question and the A records
// of the answers; the same code as tools/find-camera and lapcam.

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

	mdnsWait  = 1500 * time.Millisecond // how long one lookup waits for an answer
	mdnsCache = 5 * time.Minute         // how long a found address is trusted
)

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// isLocalName - a name mDNS is for (host.local), not an address
func isLocalName(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".local") && net.ParseIP(host) == nil
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

// mdnsLookup - one A query on every usable interface, the first A record
// for host in the answers (within mdnsWait, or ctx)
func mdnsLookup(ctx context.Context, host string) (net.IP, error) {
	conns := mdnsConns()
	if len(conns) == 0 {
		return nil, errors.New("mDNS: no network interface")
	}
	deadline := time.Now().Add(mdnsWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	found := make(chan net.IP, len(conns))
	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			defer c.Close()
			c.SetReadDeadline(deadline)
			buf := make([]byte, 9000)
			for {
				n, _, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if ip := aRecordFor(buf[:n], host); ip != nil {
					found <- ip
					return
				}
			}
		}(c)
	}
	q := buildQuery(host, typeA)
	for _, c := range conns {
		c.WriteToUDP(q, mdnsGroup)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case ip := <-found:
		return ip, nil
	case <-done:
		select {
		case ip := <-found:
			return ip, nil
		default:
		}
		return nil, fmt.Errorf("mDNS: no answer for %s within %v (not on this network?)", host, mdnsWait)
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

// --- DNS message format ---

// buildQuery - a query for one name and type (id 0, as mDNS queries have)
func buildQuery(name string, typ uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], 1) // one question
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	b = append(b, 0)
	b = binary.BigEndian.AppendUint16(b, typ)
	return binary.BigEndian.AppendUint16(b, classIN)
}

// aRecordFor - the IPv4 address in an A record for host (lower case) in a
// response, or nil (a query, another host, a broken message)
func aRecordFor(msg []byte, host string) net.IP {
	if len(msg) < 12 || msg[2]&0x80 == 0 {
		return nil
	}
	be := binary.BigEndian
	questions := int(be.Uint16(msg[4:]))
	rrs := int(be.Uint16(msg[6:])) + int(be.Uint16(msg[8:])) + int(be.Uint16(msg[10:]))
	off := 12
	for i := 0; i < questions; i++ {
		_, end, err := readName(msg, off)
		if err != nil {
			return nil
		}
		off = end + 4
	}
	for i := 0; i < rrs; i++ {
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
			return append(net.IP(nil), msg[rd:rd+4]...)
		}
		off = rd + rdlen
	}
	return nil
}

// readName - the name at off, following compression pointers, and the
// offset just after it in the message
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	end := -1
	for jumps := 0; ; {
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
			if jumps++; jumps > 32 {
				return "", 0, errors.New("DNS name compression loop")
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)
		case l&0xC0 != 0:
			return "", 0, errors.New("bad DNS label")
		default:
			if off+1+l > len(msg) {
				return "", 0, errors.New("truncated DNS message")
			}
			labels = append(labels, string(msg[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
