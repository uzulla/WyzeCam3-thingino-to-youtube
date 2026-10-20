// find-camera - list the Thingino cameras on the local network with their IP
// addresses. Every Thingino camera announces an "_thingino._tcp" service over
// mDNS (the same thing "dns-sd -B _thingino._tcp" shows); this asks for it on
// every network interface and prints who answered.
//
//	go -C tools/find-camera run .          (from the repository root)
//	go -C tools/find-camera run . -t 10s   (wait longer)
//
// Only the local network (same Wi-Fi / LAN) can be searched: mDNS does not
// cross routers. See docs/find-camera.md.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const service = "_thingino._tcp.local"

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// searcher - the socket of one network interface
type searcher struct {
	ifname string
	conn   *net.UDPConn
}

type packet struct {
	from    net.IP
	records []record
}

func main() {
	timeout := flag.Duration("t", 3*time.Second, "how long to wait for answers")
	verbose := flag.Bool("v", false, "show the interfaces used and every answer")
	flag.Parse()

	searchers := listen(*verbose)
	if len(searchers) == 0 {
		fmt.Fprintln(os.Stderr, "find-camera: no network interface to search on (not connected?)")
		os.Exit(2)
	}
	packets := make(chan packet, 64)
	deadline := time.Now().Add(*timeout)
	var wg sync.WaitGroup
	for _, s := range searchers {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			receive(c, deadline, packets, *verbose)
		}(s.conn)
	}
	go func() { wg.Wait(); close(packets) }()

	// mDNS is UDP: ask again a little later in case the first one was lost
	query := buildQuery(service, typePTR)
	go func() {
		for _, after := range []time.Duration{0, time.Second} {
			if after >= *timeout {
				break
			}
			time.Sleep(after)
			for _, s := range searchers {
				// Only with -v: a failed send ends as "no camera answered" like
				// a lost one, and one after the deadline (socket closed) is normal
				if _, err := s.conn.WriteToUDP(query, mdnsGroup); err != nil && *verbose {
					fmt.Fprintf(os.Stderr, "cannot send the query on %s: %v\n", s.ifname, err)
				}
			}
		}
	}()

	var all []packet
	for p := range packets {
		all = append(all, p)
	}
	cams := cameras(all)
	if len(cams) == 0 {
		fmt.Fprintf(os.Stderr, "find-camera: no Thingino camera answered within %v.\n", *timeout)
		fmt.Fprintln(os.Stderr, "Is this computer on the same Wi-Fi / LAN as the camera? (docs/find-camera.md)")
		os.Exit(1)
	}
	for _, c := range cams {
		line := fmt.Sprintf("%-24s %-15s http://%s/", c.name, c.ip, c.ip)
		if c.info != "" {
			line += "  " + c.info
		}
		fmt.Println(line)
	}
}

// listen - one socket joined to the mDNS group per interface that can do IPv4
// multicast. The stdlib sets SO_REUSEADDR (and SO_REUSEPORT where it exists)
// on these, so port 5353 can be shared with the OS's own mDNS responder.
func listen(verbose bool) []searcher {
	ifaces, _ := net.Interfaces()
	var out []searcher
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 || !hasIPv4(ifi) {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, mdnsGroup)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", ifi.Name, err)
			}
			continue
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "searching on %s\n", ifi.Name)
		}
		out = append(out, searcher{ifname: ifi.Name, conn: c})
	}
	return out
}

func hasIPv4(ifi *net.Interface) bool {
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return true
		}
	}
	return false
}

func receive(c *net.UDPConn, deadline time.Time, out chan<- packet, verbose bool) {
	defer c.Close()
	c.SetReadDeadline(deadline)
	buf := make([]byte, 9000)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return // the deadline
		}
		recs, err := parseResponse(buf[:n])
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "bad answer from %v: %v\n", from.IP, err)
			}
			continue
		}
		if len(recs) > 0 {
			out <- packet{from: from.IP, records: recs}
		}
	}
}

type camera struct {
	name string // the service instance name, the camera's hostname by default
	ip   string
	info string // product and firmware version from the TXT record
}

// cameras - the answers put together. The records of one camera can come in
// different packets (and every interface may hear the same answer), so the
// PTR (service -> instance), SRV (instance -> host), TXT (instance) and A
// (host -> address) records are collected first and joined afterwards. A
// camera that sent no A record is listed with the address it answered from.
func cameras(packets []packet) []camera {
	instances := map[string]net.IP{} // instance -> address its PTR came from
	host := map[string]string{}      // instance -> SRV target
	txt := map[string][]string{}     // instance -> TXT strings
	addr := map[string]net.IP{}      // host -> IPv4 address
	for _, p := range packets {
		for _, r := range p.records {
			switch {
			case r.typ == typePTR && r.name == service:
				if _, seen := instances[r.target]; !seen {
					instances[r.target] = p.from
				}
			case r.typ == typeSRV:
				host[r.name] = r.target
			case r.typ == typeTXT:
				txt[r.name] = r.txt
			case r.typ == typeA && r.ip.To4() != nil:
				addr[r.name] = r.ip
			}
		}
	}
	var out []camera
	for inst, from := range instances {
		ip := from
		if a, ok := addr[host[inst]]; ok {
			ip = a
		}
		out = append(out, camera{
			name: strings.TrimSuffix(inst, "."+service),
			ip:   ip.String(),
			info: describe(txt[inst]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// describe - "product version" from TXT strings like "product=..." "version=..."
func describe(txt []string) string {
	var parts []string
	for _, key := range []string{"product=", "version="} {
		for _, t := range txt {
			if strings.HasPrefix(t, key) {
				parts = append(parts, strings.TrimPrefix(t, key))
			}
		}
	}
	return strings.Join(parts, " ")
}
