// beacon - tells the local network how this camera is doing: its IP, whether
// it is streaming to YouTube, the Wi-Fi signal, ... One small JSON message
// (status.go: Message), sent as a UDP broadcast every few seconds on each
// network interface. tools/beacon-listen on a PC on the same Wi-Fi / LAN shows
// them. See docs/beacon.md.
//
// Runs ON THE CAMERA, from the SD card (/mnt/mmcblk0p1/beacon, started by
// /etc/init.d/S95beacon). Built on the PC with build.sh.
//
//	beacon [-port 47474] [-interval 5s] [-command-port 47475] [-syslog] [-once]
//
// It also takes commands (flip the picture, ...) on -command-port, from
// tools/beacon-send: command.go. -command-port 0 turns that off.
//
// -once prints the message of each interface once to stdout and exits.
// -syslog sends the log to logread instead of stderr (the init script does).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/syslog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// target - where one interface's message goes
type target struct {
	ifname  string
	ip      net.IP // the interface's own address, carried in the message
	bcast   net.IP
	mac     string
	network *net.IPNet
}

func main() {
	port := flag.Int("port", 47474, "UDP port to broadcast to")
	interval := flag.Duration("interval", 5*time.Second, "time between messages")
	cmdPort := flag.Int("command-port", 47475, "UDP port to take commands on (0: take none)")
	once := flag.Bool("once", false, "print each interface's message once to stdout and exit")
	toSyslog := flag.Bool("syslog", false, "log to the syslog (logread) instead of stderr; the init script sets this")
	flag.Parse()
	log.SetFlags(0)
	if *toSyslog {
		if w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "beacon"); err == nil {
			log.SetOutput(w)
		} else {
			log.Printf("syslog not available (%v), logging to stderr", err)
		}
	}
	if *port < 1 || *port > 65535 {
		log.Fatalf("-port %d: not a UDP port", *port)
	}
	if *cmdPort < 0 || *cmdPort > 65535 {
		log.Fatalf("-command-port %d: not a UDP port", *cmdPort)
	}
	if *interval < time.Second {
		log.Fatalf("-interval %v: at least 1s", *interval)
	}

	// Any local port. The stdlib turns SO_BROADCAST on for UDP sockets, so
	// sending to a broadcast address needs nothing more.
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer conn.Close()

	host, _ := os.Hostname()
	build := parseBuildID(readFile("/etc/os-release"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if !*once {
		log.Printf("Started: broadcasting to port %d every %v", *port, *interval)
		if *cmdPort != 0 {
			cc, err := net.ListenUDP("udp4", &net.UDPAddr{Port: *cmdPort})
			if err != nil {
				log.Fatalf("cannot take commands on port %d: %v", *cmdPort, err)
			}
			defer cc.Close()
			log.Printf("Taking commands on port %d (from anyone on the LAN: docs/beacon.md)", *cmdPort)
			go serveCommands(cc, &commander{
				host:    host,
				status:  func(from net.IP) Message { return statusFor(host, build, from) },
				prudynt: prudyntctl,
			})
		}
	}
	var seq uint64
	var lastTargets string
	lastErr := map[string]string{}
	t := time.NewTicker(*interval)
	defer t.Stop()
	for {
		seq++
		targets := listTargets()
		// Log only changes: one line per send would fill the syslog ring
		if desc := describe(targets, *port); desc != lastTargets && !*once {
			log.Printf("%s", desc)
			lastTargets = desc
		}
		base := collect(host, build, seq)
		for _, tg := range targets {
			b, err := json.Marshal(base.on(tg))
			if err != nil {
				log.Fatalf("%v", err) // a bug: Message has only plain fields
			}
			if *once {
				fmt.Println(string(b))
				continue
			}
			_, err = conn.WriteToUDP(b, &net.UDPAddr{IP: tg.bcast, Port: *port})
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			if msg != lastErr[tg.ifname] {
				if msg != "" {
					log.Printf("cannot send on %s: %s", tg.ifname, msg)
				} else {
					log.Printf("sending on %s again", tg.ifname)
				}
				lastErr[tg.ifname] = msg
			}
		}
		if *once {
			if len(targets) == 0 {
				fmt.Fprintln(os.Stderr, "beacon: no interface with an IPv4 broadcast address")
				os.Exit(1)
			}
			return
		}
		select {
		case <-ctx.Done():
			log.Printf("Stopped")
			return
		case <-t.C:
		}
	}
}

// listTargets - every interface that is up, can broadcast and has an IPv4
// address, read again on each round (DHCP may change the address, Wi-Fi may
// come and go)
func listTargets() []target {
	ifaces, _ := net.Interfaces()
	var out []target
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if b := broadcastAddr(n); b != nil {
				out = append(out, target{ifname: ifi.Name, ip: n.IP.To4(), bcast: b, mac: ifi.HardwareAddr.String(), network: n})
				break // one message per interface
			}
		}
	}
	return out
}

// broadcastAddr - the directed broadcast address of an IPv4 network
// (192.168.11.29/24 -> 192.168.11.255); nil for IPv6 and for /31 and /32,
// which have none
func broadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	mask := n.Mask
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	if ip == nil || len(mask) != net.IPv4len {
		return nil
	}
	if ones, _ := net.IPMask(mask).Size(); ones > 30 {
		return nil
	}
	b := make(net.IP, net.IPv4len)
	for i := range b {
		b[i] = ip[i] | ^mask[i]
	}
	return b
}

func describe(targets []target, port int) string {
	if len(targets) == 0 {
		return "No interface with an IPv4 broadcast address (not connected?), waiting"
	}
	parts := make([]string, len(targets))
	for i, tg := range targets {
		parts[i] = fmt.Sprintf("%s %s -> %s:%d", tg.ifname, tg.ip, tg.bcast, port)
	}
	return "Sending on " + strings.Join(parts, ", ")
}
