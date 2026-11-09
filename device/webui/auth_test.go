package main

import (
	"os"
	"path/filepath"
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
