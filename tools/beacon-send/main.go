// beacon-send - sends a command to the cameras' beacon (device/beacon) and
// shows their answers.
//
//	go -C tools/beacon-send run . image vflip=toggle                  (every camera on the LAN)
//	go -C tools/beacon-send run . -to 192.168.11.29 image vflip=true hflip=false
//	go -C tools/beacon-send run . -host ing-wyze-cam3-9793 ping
//
// Without -to the command is a broadcast: every camera that gets it runs it
// (-host limits it to one by name). Broadcasts do not cross routers: the PC
// must be on the same Wi-Fi / LAN. See docs/beacon.md.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Command struct {
	Type    string         `json:"type"`
	Version int            `json:"v"`
	ID      string         `json:"id"`
	Host    string         `json:"host,omitempty"`
	Cmd     string         `json:"cmd"`
	Args    map[string]any `json:"args,omitempty"`
}

type Result struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Host   string          `json:"host"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}

func main() {
	to := flag.String("to", "255.255.255.255", "where to send: a camera's IP or name, or a broadcast address")
	host := flag.String("host", "", "only the camera of this name runs the command")
	port := flag.Int("port", 47475, "the cameras' command port")
	wait := flag.Duration("t", 2*time.Second, "how long to wait for answers")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: beacon-send [-to ip] [-host name] <command> [key=value ...]")
		fmt.Fprintln(os.Stderr, "  commands: ping, image vflip=true|false|toggle hflip=true|false|toggle")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	cmd, err := build(flag.Args(), *host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon-send: %v\n", err)
		os.Exit(2)
	}
	dst, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(*to, strconv.Itoa(*port)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon-send: %v\n", err)
		os.Exit(2)
	}
	pkt, _ := json.Marshal(cmd)

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "beacon-send: %v\n", err)
		os.Exit(2)
	}
	defer conn.Close()
	// UDP: send again a little later in case the first one (or its answer)
	// was lost. The camera answers a resent id without running it again.
	go func() {
		for _, after := range []time.Duration{0, 700 * time.Millisecond} {
			if after >= *wait {
				break
			}
			time.Sleep(after)
			if _, err := conn.WriteToUDP(pkt, dst); err != nil {
				fmt.Fprintf(os.Stderr, "beacon-send: cannot send to %v: %v\n", dst, err)
			}
		}
	}()

	conn.SetReadDeadline(time.Now().Add(*wait))
	seen := map[string]bool{} // the second send gets a second answer
	ok, failed := 0, 0
	buf := make([]byte, 65536)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne := net.Error(nil); errors.As(err, &ne) && ne.Timeout() {
				break // the deadline
			}
			if errors.Is(err, net.ErrClosed) {
				break
			}
			continue // the deadline still ends the loop
		}
		var r Result
		if json.Unmarshal(buf[:n], &r) != nil || r.Type != "thingino-command-result" || r.ID != cmd.ID {
			continue
		}
		if seen[from.IP.String()] {
			continue
		}
		seen[from.IP.String()] = true
		if r.OK {
			ok++
			fmt.Printf("%-24s %-15s ok     %s\n", r.Host, from.IP, r.Result)
		} else {
			failed++
			fmt.Printf("%-24s %-15s error  %s\n", r.Host, from.IP, r.Error)
		}
	}
	switch {
	case ok+failed == 0:
		fmt.Fprintf(os.Stderr, "beacon-send: no camera answered within %v (same Wi-Fi / LAN? beacon running there? docs/beacon.md)\n", *wait)
		os.Exit(1)
	case ok == 0:
		os.Exit(3)
	}
}

// build - the command from the words after the options: a name, then
// key=value pairs (true / false / numbers as JSON values, the rest as text)
func build(words []string, host string) (Command, error) {
	id := make([]byte, 8)
	rand.Read(id)
	c := Command{Type: "thingino-command", Version: 1, ID: hex.EncodeToString(id), Host: host, Cmd: words[0]}
	for _, w := range words[1:] {
		k, v, found := strings.Cut(w, "=")
		if !found || k == "" {
			return Command{}, fmt.Errorf("%q: arguments are key=value", w)
		}
		if c.Args == nil {
			c.Args = map[string]any{}
		}
		c.Args[k] = value(v)
	}
	return c, nil
}

func value(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	return v
}
