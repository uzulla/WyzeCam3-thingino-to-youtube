package main

// Sign-in with the camera's root password (the same one as Thingino's web
// UI and ssh), checked against /etc/shadow. A session is a random token in a
// cookie, kept in memory: a restart of lapcam means signing in again.

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
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
	sessionCookie  = "lapcam_session"
	loginUser      = "root"
	maxPasswordLen = 128
)

// loginDelay - served after a wrong password (a variable: tests shorten it)
var loginDelay = 1500 * time.Millisecond

type sessions struct {
	mu     sync.Mutex
	ttl    time.Duration
	live   map[string]time.Time // sha256(token) -> expiry: the token itself is only in the phone's cookie
	file   string               // where the sessions are kept across a restart of lapcam ("" = memory only)
	fileMu sync.Mutex           // one writer of that file at a time
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
	now := time.Now()
	for t, exp := range s.live {
		if exp.Before(now) {
			delete(s.live, t)
		}
	}
	s.live[tokenKey(tok)] = now.Add(s.ttl)
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.write(snap) // outside the lock: valid() must not wait for the card
	return tok
}

// tokenKey - what a token is stored under (memory and file): its SHA-256,
// so a copy of the file is not a set of usable cookies
func tokenKey(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// sessionFile - the saved form: the sessions and a digest of the root
// password hash they were made under (a changed password drops them all)
type sessionFile struct {
	Shadow   string           `json:"shadow"`
	Sessions map[string]int64 `json:"sessions"`
}

// shadowDigest - identifies the current root password (not the hash itself)
func shadowDigest() string {
	h, err := shadowHash(loginUser)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte("lapcam-shadow:" + h))
	return hex.EncodeToString(sum[:8])
}

// persistTo - keep the sessions in file (tmpfs: an update or restart of lapcam
// does not sign everyone out; a reboot does) and load the ones already there
func (s *sessions) persistTo(file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file = file
	fi, err := os.Stat(file)
	if err != nil || fi.Size() > 1<<20 {
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var saved sessionFile
	if json.Unmarshal(b, &saved) != nil {
		log.Printf("sessions: %s is not valid JSON, starting without saved sessions", file)
		return
	}
	if saved.Shadow != shadowDigest() {
		log.Printf("sessions: the root password changed, saved sessions dropped")
		return
	}
	now := time.Now()
	for key, exp := range saved.Sessions {
		if _, err := hex.DecodeString(key); err != nil || len(key) != 64 {
			continue
		}
		if t := time.Unix(exp, 0); t.After(now) {
			s.live[key] = t
		}
	}
}

// snapshotLocked - the live sessions as they go into the file (caller holds
// s.mu); nil when nothing is persisted
func (s *sessions) snapshotLocked() *sessionFile {
	if s.file == "" {
		return nil
	}
	out := &sessionFile{Shadow: shadowDigest(), Sessions: map[string]int64{}}
	for key, exp := range s.live {
		out.Sessions[key] = exp.Unix()
	}
	return out
}

// write - the snapshot to the file, tmp + rename, outside s.mu (a slow card
// must not hold every request's valid() check). Written on create and drop
// only: the sliding expiry of valid() is not saved, so after a restart a
// session is as old as its last create (at most ttl behind). Writers are
// serialized by fileMu so two snapshots cannot land out of order.
func (s *sessions) write(snap *sessionFile) {
	if snap == nil {
		return
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	b, err := json.Marshal(snap)
	if err != nil {
		log.Printf("sessions: cannot encode: %v", err)
		return
	}
	tmp := s.file + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		_ = os.Remove(tmp)
		log.Printf("sessions: cannot write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, s.file); err != nil {
		_ = os.Remove(tmp)
		log.Printf("sessions: cannot replace %s: %v", s.file, err)
	}
}

func (s *sessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	key := tokenKey(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.live[key]
	if !ok {
		return false
	}
	if exp.Before(time.Now()) {
		delete(s.live, key)
		return false
	}
	// sliding expiry: a phone that keeps using the app stays signed in
	s.live[key] = time.Now().Add(s.ttl)
	return true
}

func (s *sessions) drop(tok string) {
	s.mu.Lock()
	delete(s.live, tokenKey(tok))
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.write(snap)
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
	// the version is public (shown under the sign-in form), like Thingino's own UI shows its build
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": s.sessions.valid(s.sessionToken(r)), "version": version})
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
