// beacon-listen - shows the messages the cameras' beacon (device/beacon)
// broadcasts on the local network: one line per message, with the camera's
// name, IP, stream state, Wi-Fi signal, ...
//
//	go -C tools/beacon-listen run .            (from the repository root; Ctrl+C stops)
//	go -C tools/beacon-listen run . -t 15s     (stop after 15 s)
//	go -C tools/beacon-listen run . -json      (the messages as received)
//
// Broadcasts do not cross routers: the PC must be on the same Wi-Fi / LAN.
// See docs/beacon.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Message - what device/beacon sends (its status.go). Only the fields shown
// here; a newer beacon may add more.
type Message struct {
	Type    string `json:"type"`
	Version int    `json:"v"`
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Build   string `json:"build"`
	UptimeS int64  `json:"uptime_s"`
	Stream  struct {
		State    string `json:"state"`
		ElapsedS *int64 `json:"elapsed_s"`
	} `json:"stream"`
	RSSI      *int    `json:"rssi_dbm"`
	MemFreeMB *int    `json:"mem_free_mb"`
	Load1     *string `json:"load1"`
}

func main() {
	port := flag.Int("port", 47474, "UDP port the beacons are sent to")
	limit := flag.Duration("t", 0, "stop after this long (0: until Ctrl+C)")
	raw := flag.Bool("json", false, "print the messages as received (JSON, one per line)")
	flag.Parse()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: *port})
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon-listen: %v (another beacon-listen running?)\n", err)
		os.Exit(2)
	}
	defer conn.Close()
	fmt.Fprintf(os.Stderr, "listening on UDP port %d (Ctrl+C to stop)\n", *port)
	if *limit > 0 {
		conn.SetReadDeadline(time.Now().Add(*limit))
	}
	seen := 0
	buf := make([]byte, 65536)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			fmt.Fprintf(os.Stderr, "beacon-listen: %v\n", err)
			os.Exit(2)
		}
		m, ok := parse(buf[:n])
		if !ok {
			continue // something else on this port
		}
		seen++
		if *raw {
			fmt.Println(strings.TrimSpace(string(buf[:n])))
		} else {
			fmt.Println(format(time.Now(), m, from.IP))
		}
	}
	if seen == 0 {
		fmt.Fprintf(os.Stderr, "beacon-listen: no beacon within %v.\n", *limit)
		fmt.Fprintln(os.Stderr, "Is this computer on the same Wi-Fi / LAN as the camera, and is beacon running there? (docs/beacon.md)")
		os.Exit(1)
	}
}

// parse - a beacon message, or false for anything else that reached the port
func parse(b []byte) (Message, bool) {
	var m Message
	if json.Unmarshal(b, &m) != nil || m.Type != "thingino-beacon" {
		return Message{}, false
	}
	return m, true
}

// format - one line: time, name, IP, stream state (and for how long), then
// the optional values that were in the message
func format(now time.Time, m Message, from net.IP) string {
	ip := m.IP
	if ip == "" {
		ip = from.String()
	}
	stream := m.Stream.State
	if m.Stream.ElapsedS != nil {
		stream += " " + dur(*m.Stream.ElapsedS)
	}
	parts := []string{
		now.Format("15:04:05"),
		fmt.Sprintf("%-24s", m.Host),
		fmt.Sprintf("%-15s", ip),
		fmt.Sprintf("%-20s", stream),
	}
	if m.RSSI != nil {
		parts = append(parts, fmt.Sprintf("wifi %ddBm", *m.RSSI))
	}
	parts = append(parts, "up "+dur(m.UptimeS))
	if m.MemFreeMB != nil {
		parts = append(parts, fmt.Sprintf("mem %dMB", *m.MemFreeMB))
	}
	if m.Load1 != nil {
		parts = append(parts, "load "+*m.Load1)
	}
	if m.Build != "" {
		parts = append(parts, m.Build)
	}
	return strings.Join(parts, "  ")
}

// dur - a short duration: 45s, 14m26s, 3h02m, 2d03h
func dur(s int64) string {
	switch {
	case s < 0:
		return "?"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	case s < 86400:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%dd%02dh", s/86400, s%86400/3600)
}
