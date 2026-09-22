package main

import (
	"os"
	"path/filepath"
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
		"unknown slot":   `{"slots":{"nope":{"template":"x"}}}`,
		"no slots":       `{"sources":{"m":{"type":"meminfo"}}}`,
		"unknown source": `{"sources":{"m":{"type":"memory"}},"slots":{"textfile":{"template":"x"}}}`,
		"broken json":    `{"slots":`,
		"trailing value": `{"slots":{"textfile":{"template":"x"}}} {"slots":{}}`,
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
