package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withShadow - point checkPassword at a temporary shadow file for one test
func withShadow(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shadow")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	old := shadowFile
	shadowFile = path
	t.Cleanup(func() { shadowFile = old })
}

func TestCheckPassword(t *testing.T) {
	hash := sha512Crypt("camera-pass", "testsalt", 5000, false)
	withShadow(t, "root:"+hash+":1::::::\nnobody:*:1::::::\n")
	if ok, err := checkPassword("camera-pass"); err != nil || !ok {
		t.Fatalf("right password: %v %v", ok, err)
	}
	if ok, _ := checkPassword("camera-pass "); ok {
		t.Fatal("trailing space accepted")
	}
	if ok, _ := checkPassword(""); ok {
		t.Fatal("empty password accepted against a real hash")
	}

	withShadow(t, "root::1::::::\n")
	if ok, err := checkPassword(""); err != nil || !ok {
		t.Fatalf("empty hash must accept the empty password only: %v %v", ok, err)
	}
	if ok, _ := checkPassword("x"); ok {
		t.Fatal("empty hash accepted a password")
	}

	withShadow(t, "root:!:1::::::\n")
	if ok, err := checkPassword("x"); ok || err == nil {
		t.Fatal("locked account did not fail")
	}

	withShadow(t, "root:$1$abc$def:1::::::\n")
	if ok, err := checkPassword("x"); ok || err == nil {
		t.Fatal("a non-$6$ hash must be an error, not a silent refusal")
	}

	withShadow(t, "admin:x:1::::::\n")
	if ok, err := checkPassword("x"); ok || err == nil {
		t.Fatal("missing root entry did not fail")
	}
}

func TestSessions(t *testing.T) {
	s := newSessions(80 * time.Millisecond)
	if s.valid("") || s.valid("nope") {
		t.Fatal("unknown tokens accepted")
	}
	tok := s.create()
	if !s.valid(tok) {
		t.Fatal("fresh token refused")
	}
	// sliding: use keeps it alive past the ttl
	time.Sleep(50 * time.Millisecond)
	if !s.valid(tok) {
		t.Fatal("token refused before the ttl")
	}
	time.Sleep(50 * time.Millisecond)
	if !s.valid(tok) {
		t.Fatal("use did not slide the expiry")
	}
	time.Sleep(120 * time.Millisecond)
	if s.valid(tok) {
		t.Fatal("expired token accepted")
	}
	tok2 := s.create()
	s.drop(tok2)
	if s.valid(tok2) {
		t.Fatal("dropped token accepted")
	}
	if a, b := s.create(), s.create(); a == b || len(a) != 64 {
		t.Fatalf("tokens not random/64 hex: %q %q", a, b)
	}
}

func TestSessionsPersist(t *testing.T) {
	withShadow(t, "root:"+sha512Crypt("pw", "salt", 5000, false)+":1::::::\n")
	dir := t.TempDir()
	file := filepath.Join(dir, "sessions.json")
	a := newSessions(time.Hour)
	a.persistTo(file)
	tok := a.create()
	gone := a.create()
	a.drop(gone)
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("session file: %v %v", fi, err)
	}
	if _, err := os.Stat(file + ".new"); err == nil {
		t.Fatal("temp file left behind")
	}
	b, _ := os.ReadFile(file)
	if strings.Contains(string(b), tok) {
		t.Fatal("the token itself is in the file; only its hash may be")
	}
	fresh := newSessions(time.Hour)
	fresh.persistTo(file)
	if !fresh.valid(tok) {
		t.Fatal("a saved session was not loaded")
	}
	if fresh.valid(gone) {
		t.Fatal("a dropped session came back")
	}
	// expired, malformed and foreign entries are skipped; the rest loads
	raw := `{"shadow":"` + shadowDigest() + `","sessions":{"` + strings.Repeat("a", 64) + `":1,"short":9999999999,"` + strings.Repeat("b", 64) + `":9999999999}}`
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newSessions(time.Hour)
	c.persistTo(file)
	if len(c.live) != 1 {
		t.Fatalf("loaded %d sessions from %s, want 1", len(c.live), raw)
	}
	// a changed root password drops them all; broken JSON starts empty
	if err := os.WriteFile(file, []byte(`{"shadow":"other","sessions":{"`+strings.Repeat("b", 64)+`":9999999999}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := newSessions(time.Hour)
	d.persistTo(file)
	if len(d.live) != 0 {
		t.Fatal("sessions from another password were loaded")
	}
	if err := os.WriteFile(file, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newSessions(time.Hour)
	e.persistTo(file)
	if len(e.live) != 0 || e.create() == "" {
		t.Fatal("a broken file must not stop the sessions from working")
	}
}
