package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCfg(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "osd-feed.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigRejects(t *testing.T) {
	for name, body := range map[string]string{
		"unknown slot":    `{"slots":{"nope":{"template":"x"}}}`,
		"no slots":        `{"sources":{"m":{"type":"meminfo"}}}`,
		"unknown source":  `{"sources":{"m":{"type":"memory"}},"slots":{"textfile":{"template":"x"}}}`,
		"broken json":     `{"slots":`,
		"trailing value":  `{"slots":{"textfile":{"template":"x"}}} {"slots":{}}`,
		"odd image width": `{"slots":{"imagefile":{"png":"/x.png","width":201}}}`,
		"image too wide":  `{"slots":{"imagefile":{"png":"/x.png","width":1282}}}`,
		"negative height": `{"slots":{"imagefile":{"png":"/x.png","height":-2}}}`,
		"image no png":    `{"slots":{"imagefile":{"width":200}}}`,
		"image bad fit":   `{"slots":{"imagefile":{"png":"/x.png","fit":"cover"}}}`,
		"png on text":     `{"slots":{"textfile":{"template":"x","png":"/x.png"}}}`,
		"gball on text":   `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"textfile":{"template":"x","gball":{"source":"rb"}}}}`,
		"gball no source": `{"slots":{"imagefile":{"gball":{}}}}`,
		"gball unknown":   `{"slots":{"imagefile":{"gball":{"source":"rb"}}}}`,
		"gball tiny":      `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"imagefile":{"gball":{"source":"rb","size":8}}}}`,
		"gball max_g":     `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"imagefile":{"gball":{"source":"rb","max_g":100000}}}}`,
		"gball huge":      `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"imagefile":{"gball":{"source":"rb","size":100000}}}}`,
		"gball trail":     `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"imagefile":{"gball":{"source":"rb","trail":99}}}}`,
		"png_box no png":  `{"sources":{"rb":{"type":"udp","listen":":5005"}},"slots":{"imagefile":{"png_box":{"width":2,"height":2},"gball":{"source":"rb"}}}}`,
		"png_box empty":   `{"slots":{"imagefile":{"png":"/x.png","png_box":{"x":0,"y":0}}}}`,
	} {
		if _, err := loadConfig(writeCfg(t, body)); err == nil {
			t.Errorf("%s: must fail", name)
		}
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := loadConfig(writeCfg(t, `{"slots":{"textfile":{"template":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.interval() != 250*time.Millisecond || !c.clearOnExit() {
		t.Errorf("interval=%v clearOnExit=%v", c.interval(), c.clearOnExit())
	}
	c, _ = loadConfig(writeCfg(t, `{"interval_ms":1,"clear_on_exit":false,"slots":{"textfile":{"template":"x"}}}`))
	if c.interval() != 100*time.Millisecond || c.clearOnExit() {
		t.Errorf("interval must clamp to 100ms and clear_on_exit be false, got %v %v", c.interval(), c.clearOnExit())
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	for name, body := range map[string]string{
		"top level": `{"intervl_ms": 100, "slots": {"textfile": {"template": "x"}}}`,
		"in source": `{"sources": {"m": {"type": "meminfo", "interval": 5}}, "slots": {"textfile": {"template": "x"}}}`,
		"in slot":   `{"slots": {"textfile": {"template": "x", "cols_": 4}}}`,
	} {
		if _, err := loadConfig(writeCfg(t, body)); err == nil {
			t.Errorf("%s: misspelt key must be an error", name)
		}
	}
}

func TestLoadConfigAcceptsImageSlot(t *testing.T) {
	c, err := loadConfig(writeCfg(t, `{"slots":{"textfile":{"template":"x"},"imagefile":{"png":"/mnt/mmcblk0p1/logo.png","fit":"none","width":200,"height":200}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Slots["imagefile"].PNG == "" || c.Slots["imagefile"].Fit != "none" {
		t.Errorf("got %+v", c.Slots["imagefile"])
	}
}

func TestLoadConfigAcceptsGBall(t *testing.T) {
	c, err := loadConfig(writeCfg(t, `{"sources":{"racebox":{"type":"udp","listen":":5005","match":{"type":"racebox"},"stale_ms":500}},
	  "slots":{"imagefile":{"png":"/x.png","png_box":{"x":0,"y":168,"width":200,"height":74},
	    "gball":{"source":"racebox","x":20,"y":48,"size":160,"trail":0,"invert_lateral":true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := c.Slots["imagefile"].GBall
	if c.Sources["racebox"].IntervalMs != 100 {
		t.Errorf("udp must poll every 100 ms by default, got %d", c.Sources["racebox"].IntervalMs)
	}
	if g == nil || g.Size != 160 || *g.Trail != 0 || !g.InvertLateral || c.Sources["racebox"].Match["type"] != "racebox" {
		t.Errorf("got %+v", c.Slots["imagefile"])
	}
}

// The logo and the G-ball in prudynt's two image slots, each placed on its own
func TestLoadConfigAcceptsTwoImageSlots(t *testing.T) {
	c, err := loadConfig(writeCfg(t, `{"sources":{"racebox":{"type":"udp","listen":":5005"}},
	  "slots":{"imagefile":{"png":"/x.png"},"imagefile2":{"gball":{"source":"racebox"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Slots["imagefile"].PNG != "/x.png" || c.Slots["imagefile2"].GBall == nil {
		t.Errorf("got %+v", c.Slots)
	}
	if _, err := loadConfig(writeCfg(t, `{"slots":{"imagefile3":{"png":"/x.png"}}}`)); err == nil {
		t.Error("imagefile3 must be rejected: prudynt has two image slots")
	}
	if _, err := loadConfig(writeCfg(t, `{"slots":{"imagefile2":{}}}`)); err == nil || !strings.Contains(err.Error(), "slot imagefile2 needs") {
		t.Errorf("an empty imagefile2 must be rejected by its name, got %v", err)
	}
}
