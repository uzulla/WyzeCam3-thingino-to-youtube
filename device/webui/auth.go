package main

// Sign-in with the camera's root password (the same one as Thingino's web
// UI and ssh), checked against /etc/shadow. A session is a random token in a
// cookie, kept in memory: a restart of webui means signing in again.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// shadowFile - where the password hash is read from (-shadow, for testing)
var shadowFile = "/etc/shadow"

const (
	sessionCookie  = "webui_session"
	loginUser      = "root"
	maxPasswordLen = 128
)

// loginDelay - served after a wrong password (a variable: tests shorten it)
var loginDelay = 1500 * time.Millisecond

type sessions struct {
	mu   sync.Mutex
	ttl  time.Duration
	live map[string]time.Time // token -> expiry
	// one password check at a time (the hash is slow on this CPU), and a
	// count of recent failures per address: after loginMaxFails in
	// loginFailWindow the address gets 429 at once, whatever it sends in
	// parallel. Guessing from the LAN is then bounded by the window.
	attempt sync.Mutex
	fails   map[string][]time.Time
}

const (
	loginMaxFails   = 5
	loginFailWindow = time.Minute
)

// tooManyFails - has addr failed too often lately? (also forgets old entries)
func (s *sessions) tooManyFails(addr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-loginFailWindow)
	for a, ts := range s.fails {
		var keep []time.Time
		for _, t := range ts {
			if t.After(cut) {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(s.fails, a)
		} else {
			s.fails[a] = keep
		}
	}
	return len(s.fails[addr]) >= loginMaxFails
}

func (s *sessions) noteFail(addr string) {
	s.mu.Lock()
	s.fails[addr] = append(s.fails[addr], time.Now())
	s.mu.Unlock()
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{ttl: ttl, live: map[string]time.Time{}, fails: map[string][]time.Time{}}
}

func (s *sessions) create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("no random numbers: %v", err)
	}
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for t, exp := range s.live {
		if exp.Before(now) {
			delete(s.live, t)
		}
	}
	s.live[tok] = now.Add(s.ttl)
	return tok
}

func (s *sessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.live[tok]
	if !ok {
		return false
	}
	if exp.Before(time.Now()) {
		delete(s.live, tok)
		return false
	}
	// sliding expiry: a phone that keeps using the app stays signed in
	s.live[tok] = time.Now().Add(s.ttl)
	return true
}

func (s *sessions) drop(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.live, tok)
}

// shadowHash - the password hash of user from /etc/shadow
func shadowHash(user string) (string, error) {
	f, err := os.Open(shadowFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 2 && fields[0] == user {
			return fields[1], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", errors.New("no such user in " + shadowFile)
}

// checkPassword - is this the root password? An empty hash field (no
// password set) accepts only an empty password, like login does.
func checkPassword(password string) (bool, error) {
	hash, err := shadowHash(loginUser)
	if err != nil {
		return false, err
	}
	if hash == "" {
		return password == "", nil
	}
	if strings.HasPrefix(hash, "!") || strings.HasPrefix(hash, "*") {
		return false, errors.New("the account is locked")
	}
	return sha512CryptVerify(password, hash)
}

func (s *server) sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// auth - the middleware for the API: 401 as JSON without a session
func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.sessions.valid(s.sessionToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "sign in first"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": s.sessions.valid(s.sessionToken(r))})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body, 4096); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(body.Password) > maxPasswordLen {
		// sha512-crypt hashes the password len(password) times: keep that small
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the password is too long"})
		return
	}
	addr := clientIP(r)
	// one check at a time (the hash takes a moment on this CPU). The failure
	// count is read and written inside the same lock, so parallel guesses
	// from one address cannot slip past the limit together. The delay after
	// a wrong password is served outside the lock, so a stream of wrong
	// guesses from one side does not hold the owner's correct sign-in back.
	s.sessions.attempt.Lock()
	if s.sessions.tooManyFails(addr) {
		s.sessions.attempt.Unlock()
		log.Printf("sign-in refused for %s: too many wrong passwords", addr)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many wrong passwords; wait a minute"})
		return
	}
	ok, err := checkPassword(body.Password)
	if err == nil && !ok {
		s.sessions.noteFail(addr)
	}
	s.sessions.attempt.Unlock()
	if err != nil {
		log.Printf("sign-in: cannot check the password: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "cannot check the password: " + err.Error()})
		return
	}
	if !ok {
		log.Printf("sign-in failed from %s", addr)
		time.Sleep(loginDelay)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "wrong password"})
		return
	}
	tok := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessions.ttl / time.Second),
	})
	log.Printf("signed in from %s", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": true})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.drop(s.sessionToken(r))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": false})
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// readJSON - decode a small JSON body (at most limit bytes)
func readJSON(r *http.Request, v any, limit int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("bad JSON body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(mustJSON(v))
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("%v", err) // a bug: only plain fields are encoded
	}
	return b
}
