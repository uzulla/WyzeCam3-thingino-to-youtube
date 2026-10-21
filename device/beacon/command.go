package main

// Commands: beacon also listens on a UDP port (47475) for small JSON commands
// (tools/beacon-send sends them) and answers each one to its sender.
//
// Nothing checks who sent a command: anyone on the same LAN can send one.
// So only the fixed commands below exist (no shell, no file writes), and
// what they change lasts only until prudynt restarts (the SD card's
// prudynt-osd.json is not touched; osd-config sends its settings again then).
//
//	{"type":"thingino-command","v":1,"id":"5f2c...","cmd":"image","args":{"vflip":"toggle"}}
//	-> {"type":"thingino-command-result","v":1,"id":"5f2c...","host":"ing-wyze-cam3-9793","cmd":"image","ok":true,"result":{"image":{"vflip":false}}}
//
// "host" in a command limits it to the camera of that name (for a command
// sent as a broadcast); without it, every camera that gets it runs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	cmdType    = "thingino-command"
	resultType = "thingino-command-result"
	cmdMaxSize = 4096
)

type Command struct {
	Type    string          `json:"type"`
	Version int             `json:"v"`
	ID      string          `json:"id"`             // chosen by the sender, sent back in the result
	Host    string          `json:"host,omitempty"` // only the camera of this name
	Cmd     string          `json:"cmd"`
	Args    json.RawMessage `json:"args,omitempty"`
}

type Result struct {
	Type    string `json:"type"`
	Version int    `json:"v"`
	ID      string `json:"id"`
	Host    string `json:"host"`
	Cmd     string `json:"cmd"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Result  any    `json:"result,omitempty"`
}

// commander - runs the commands. The fields are what the commands need, as
// values so that the tests can replace them.
type commander struct {
	host string
	// status - the beacon message for a sender at this address (for "ping")
	status func(from net.IP) Message
	// prudynt - sends a JSON request to prudynt, returns its answer
	prudynt func(ctx context.Context, body string) (string, error)
	// recent - the last few successful results, to answer a resent command.
	// No lock: only serveCommands' single goroutine calls handle. Running
	// commands concurrently would need one.
	recent []recentResult
}

type recentResult struct {
	key    string // sender address + id
	answer []byte
}

const recentMax = 32

// handle - the answer to one packet, or nil when there is none to give (not a
// command, or a command for another camera)
func (c *commander) handle(pkt []byte, from *net.UDPAddr) []byte {
	var cmd Command
	if json.Unmarshal(pkt, &cmd) != nil || cmd.Type != cmdType {
		return nil
	}
	if cmd.Host != "" && !strings.EqualFold(cmd.Host, c.host) {
		return nil
	}
	// UDP can lose the answer and the sender then sends the same command
	// again: answer it with the same result instead of running it twice (a
	// toggle would undo itself). Only successes are kept: a failed command
	// changed nothing (prudynt did not take it), so a resend after prudynt
	// is back runs it.
	key := from.IP.String() + " " + cmd.ID
	if cmd.ID != "" {
		for _, r := range c.recent {
			if r.key == key {
				return r.answer
			}
		}
	}
	res := Result{Type: resultType, Version: msgVersion, ID: cmd.ID, Host: c.host, Cmd: cmd.Cmd}
	out, err := c.run(cmd, from)
	if err != nil {
		res.Error = err.Error()
	} else {
		res.OK, res.Result = true, out
	}
	if cmd.Cmd != "ping" {
		what := strings.TrimSpace(cmd.Cmd + " " + string(cmd.Args))
		if err != nil {
			log.Printf("Command from %s: %s -> error: %v", from.IP, what, err)
		} else {
			log.Printf("Command from %s: %s -> ok", from.IP, what)
		}
	}
	answer, err := json.Marshal(res)
	if err != nil {
		// Not expected: the result holds only what prudynt's JSON decoded to
		log.Printf("cannot encode the result of %s: %v", cmd.Cmd, err)
		res.OK, res.Result, res.Error = false, nil, "cannot encode the result"
		answer, _ = json.Marshal(res)
		return answer
	}
	if res.OK && cmd.ID != "" {
		c.recent = append(c.recent, recentResult{key, answer})
		if len(c.recent) > recentMax {
			c.recent = c.recent[1:]
		}
	}
	return answer
}

func (c *commander) run(cmd Command, from *net.UDPAddr) (any, error) {
	if cmd.Version != 1 {
		return nil, fmt.Errorf("unsupported version %d (this beacon knows 1)", cmd.Version)
	}
	switch cmd.Cmd {
	case "ping":
		return c.status(from.IP), nil
	case "image":
		return c.image(cmd.Args)
	}
	return nil, fmt.Errorf("unknown command %q (known: ping, image)", cmd.Cmd)
}

// imageKeys - the settings of prudynt's "image" section the image command
// may change: the flips (the same ones prudynt-osd.json's "image" sets)
var imageKeys = map[string]bool{"vflip": true, "hflip": true}

// image - {"vflip": true | false | "toggle", "hflip": ...}. A toggle asks
// prudynt for the current value first. The result is prudynt's answer.
func (c *commander) image(raw json.RawMessage) (any, error) {
	var args map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || len(args) == 0 {
		return nil, errors.New(`args must be an object like {"vflip":"toggle"}`)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	want := map[string]bool{}
	for _, k := range keys {
		if !imageKeys[k] {
			return nil, fmt.Errorf("image: %q cannot be changed (only vflip, hflip)", k)
		}
		switch v := args[k].(type) {
		case bool:
			want[k] = v
		case string:
			if v != "toggle" {
				return nil, fmt.Errorf("image.%s: %q (true, false or \"toggle\")", k, v)
			}
			cur, err := c.imageValue(ctx, k)
			if err != nil {
				return nil, err
			}
			want[k] = !cur
		default:
			return nil, fmt.Errorf("image.%s: true, false or \"toggle\"", k)
		}
	}
	body, _ := json.Marshal(map[string]any{"image": want})
	ans, err := c.prudynt(ctx, string(body))
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	// "{}" is prudynt's answer to a request it did not take
	if json.Unmarshal([]byte(ans), &parsed) != nil || len(parsed) == 0 {
		return nil, fmt.Errorf("prudynt did not accept %s (answer: %q)", body, strings.TrimSpace(ans))
	}
	return parsed, nil
}

// imageValue - the current value of one flip, asked with a null (prudynt
// answers a null with the current value)
func (c *commander) imageValue(ctx context.Context, key string) (bool, error) {
	ans, err := c.prudynt(ctx, fmt.Sprintf(`{"image":{%q:null}}`, key))
	if err != nil {
		return false, err
	}
	var parsed struct {
		Image map[string]any `json:"image"`
	}
	if json.Unmarshal([]byte(ans), &parsed) == nil {
		if v, ok := parsed.Image[key].(bool); ok {
			return v, nil
		}
	}
	return false, fmt.Errorf("prudynt did not tell the current image.%s (answer: %q)", key, strings.TrimSpace(ans))
}

// prudyntctl - the request through prudynt's own client, as osd-config does
func prudyntctl(ctx context.Context, body string) (string, error) {
	out, err := exec.CommandContext(ctx, "prudyntctl", "json", body).Output()
	if err != nil {
		return "", fmt.Errorf("prudyntctl: %w (is prudynt running?)", err)
	}
	return string(out), nil
}

// limiter - at most perSecond packets in each second, for all senders
// together. Nothing checks who sends, and a UDP sender address is easy to
// fake, so a limit per sender would not hold against a flood; this one keeps
// a flood from running prudyntctl and filling the syslog ring without end.
type limiter struct {
	perSecond int
	second    int64 // the Unix second being counted
	count     int
	dropping  bool // logged that this second's excess is dropped
}

func (l *limiter) allow(now time.Time) bool {
	if s := now.Unix(); s != l.second {
		l.second, l.count, l.dropping = s, 0, false
	}
	l.count++
	if l.count <= l.perSecond {
		return true
	}
	if !l.dropping {
		log.Printf("More than %d command packets in a second - dropping the rest of this second", l.perSecond)
		l.dropping = true
	}
	return false
}

// serveCommands - answers the commands arriving on conn until it is closed.
// One at a time: they are rare and small.
func serveCommands(conn *net.UDPConn, c *commander) {
	buf := make([]byte, cmdMaxSize+1)
	lim := &limiter{perSecond: 10}
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if n > cmdMaxSize || !lim.allow(time.Now()) {
			continue // not one of ours, or too many
		}
		if answer := c.handle(buf[:n], from); answer != nil {
			conn.WriteToUDP(answer, from)
		}
	}
}

// statusFor - the beacon message as it would go out on the interface whose
// network the sender is on (else the first one)
func statusFor(host, build string, from net.IP) Message {
	m := collect(host, build, 0)
	targets := listTargets()
	for _, tg := range targets {
		if tg.network.Contains(from) {
			return m.on(tg)
		}
	}
	if len(targets) > 0 {
		return m.on(targets[0])
	}
	return m
}
