package main

// Changing the camera's password from the app: the root password in
// /etc/shadow, which ssh, Thingino's web UI and lapcam all check. The new
// hash is made here (sha512-crypt, the kind /etc/shadow has by default; the
// camera's busybox passwd may write another kind, which checkPassword would
// not read) and the line swapped in atomically. Like a sign-in, the current
// password is checked under the per-address limit. A flash from the SD card
// resets the password to Thingino's default (docs/sdcard.md): the app says
// so after a sign-in with that default and offers this form right away.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultPassword - Thingino's, and what a flash from the card leaves
const defaultPassword = "root"

// newSalt - 16 random characters of the crypt alphabet
func newSalt() (string, error) {
	b := make([]byte, sha512MaxSaltLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = cryptB64[int(c)%len(cryptB64)]
	}
	return string(out), nil
}

// setShadowHash - replace user's hash (and the "last changed" day) in the
// shadow file, keeping every other line as it is; written beside and renamed
func setShadowHash(user, hash string) error {
	b, err := os.ReadFile(shadowFile)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	found := false
	for i, line := range lines {
		f := strings.Split(line, ":")
		if len(f) < 2 || f[0] != user {
			continue
		}
		for len(f) < 9 {
			f = append(f, "")
		}
		f[1] = hash
		f[2] = fmt.Sprint(time.Now().Unix() / 86400)
		lines[i] = strings.Join(f, ":")
		found = true
	}
	if !found {
		return errors.New("no such user in " + shadowFile)
	}
	// the file's own mode, written beside it and synced before the rename: a
	// power cut (the usual way this camera is switched off) must never leave
	// an empty shadow file, which would lock everyone out
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(shadowFile); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(shadowFile), "."+filepath.Base(shadowFile)+".lapcam")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(strings.Join(lines, "\n") + "\n")
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return errors.Join(werr, serr, cerr)
	}
	if err := os.Rename(tmp, shadowFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(shadowFile)); err == nil {
		_ = d.Sync() // the rename itself
		_ = d.Close()
	}
	return nil
}

// handlePassword - POST {current, new}: the current password must be right,
// the new one anything up to 128 characters without ':' or line breaks. The signed-in sessions stay (they are the
// owner's); the saved copy follows at its next write.
func (s *server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &body, 4096); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if len(body.Current) > maxPasswordLen || len(body.New) > maxPasswordLen {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("a password has %d characters at most", maxPasswordLen)})
		return
	}
	// no rules on length or characters (the owner's choice); only what the
	// shadow file cannot hold, and the same upper bound as a sign-in has
	// (the hash costs CPU per character)
	if body.New == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the new password is empty"})
		return
	}
	if strings.ContainsAny(body.New, "\n\r:") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the password cannot contain line breaks or ':'"})
		return
	}
	addr := clientIP(r)
	s.sessions.attempt.Lock()
	if s.sessions.tooManyFails(addr) {
		s.sessions.attempt.Unlock()
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many wrong passwords; wait a minute"})
		return
	}
	ok, err := checkPassword(body.Current)
	if err == nil && !ok {
		s.sessions.noteFail(addr)
	}
	s.sessions.attempt.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "cannot check the password: " + err.Error()})
		return
	}
	if !ok {
		// 403, not 401: 401 means "not signed in" to the app (it would show
		// the sign-in screen and lose the form)
		time.Sleep(loginDelay)
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "the current password is wrong"})
		return
	}
	salt, err := newSalt()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	hash := sha512Crypt(body.New, salt, sha512DefRounds, false)
	if err := setShadowHash(loginUser, hash); err != nil {
		log.Printf("password: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not write the password: " + err.Error()})
		return
	}
	log.Printf("password changed from %s", addr)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
