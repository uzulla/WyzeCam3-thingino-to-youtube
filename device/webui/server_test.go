package main

// The HTTP surface, through the real router: every API route needs a
// session, writes must be same-site JSON, sign-in sets the cookie.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	sd := t.TempDir()
	// the relay's config, so nothing reaches /etc
	if err := os.WriteFile(filepath.Join(sd, relayCfgName), []byte(`{"stream_key":"k","rtmp_url":"rtmp://h/app","enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &server{
		sd:       sd,
		prudynt:  "http://127.0.0.1:1", // nothing listens: the preview must fail cleanly
		sessions: newSessions(time.Hour),
		relay:    newRelay(sd),
		rec:      newRecorder(),
	}
	s.status = newStatusPoller(s.relay, s.rec, time.Hour) // not running: current() returns the zero status
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return srv, sd
}

// code - the status of a response whose body is not needed (closed here)
func code(resp *http.Response) int {
	resp.Body.Close()
	return resp.StatusCode
}

func jsonReq(t *testing.T, method, url, body string, cookie *http.Cookie, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" || method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestRoutesNeedASession(t *testing.T) {
	srv, _ := testServer(t)
	routes := []struct{ method, path string }{
		{"GET", "/api/status"}, {"GET", "/api/preview.mjpeg"}, {"GET", "/api/preview.mp4"}, {"GET", "/api/snapshot.jpg"},
		{"POST", "/api/relay"}, {"GET", "/api/relay/config"}, {"POST", "/api/relay/config"},
		{"POST", "/api/record"}, {"POST", "/api/image"}, {"POST", "/api/reboot"}, {"GET", "/api/log"},
		{"GET", "/api/osd"}, {"POST", "/api/osd/element"}, {"POST", "/api/osd/feed"},
		{"GET", "/api/wifi"}, {"GET", "/api/wifi/scan"}, {"POST", "/api/wifi/networks"},
		{"GET", "/api/quality"}, {"POST", "/api/quality"},
		{"GET", "/api/recordings"}, {"GET", "/api/recordings/file?p=x.mp4"}, {"POST", "/api/recordings/zip"},
		{"GET", "/api/recordings/zip/0123456789abcdef"}, {"POST", "/api/recordings/delete"}, {"POST", "/api/recordings/settings"},
	}
	for _, r := range routes {
		resp := jsonReq(t, r.method, srv.URL+r.path, `{}`, nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d, want 401", r.method, r.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s %s: 401 body is %q, want JSON", r.method, r.path, ct)
		}
		resp.Body.Close()
	}
	// the logo upload is multipart, not JSON, and still needs a session
	req, _ := http.NewRequest("POST", srv.URL+"/api/osd/logo", strings.NewReader("x"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("logo upload without a session: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestGuardAndHeaders(t *testing.T) {
	srv, _ := testServer(t)
	// a plain form or no content type: refused before auth even looks
	req, _ := http.NewRequest("POST", srv.URL+"/api/relay", strings.NewReader("op=stop"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp0, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if code(resp0) != http.StatusUnsupportedMediaType {
		t.Errorf("form POST: %d, want 415", resp0.StatusCode)
	}
	// another site's page driving the API through the cookie
	if c := code(jsonReq(t, "POST", srv.URL+"/api/reboot", `{}`, nil, map[string]string{"Sec-Fetch-Site": "cross-site"})); c != http.StatusForbidden {
		t.Errorf("cross-site POST: %d, want 403", c)
	}
	// same-origin and none (address bar) pass the guard (then hit auth)
	for _, site := range []string{"same-origin", "none"} {
		if c := code(jsonReq(t, "POST", srv.URL+"/api/reboot", `{}`, nil, map[string]string{"Sec-Fetch-Site": site})); c != http.StatusUnauthorized {
			t.Errorf("%s POST: %d, want 401", site, c)
		}
	}
	// unknown API path is a JSON 404, not the page
	resp := jsonReq(t, "GET", srv.URL+"/api/nothing", "", nil, nil)
	if resp.StatusCode != http.StatusNotFound || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("unknown api: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
	// headers on every answer, the page included
	resp = jsonReq(t, "GET", srv.URL+"/", "", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page: %d", resp.StatusCode)
	}
	resp.Body.Close()
	for k, want := range map[string]string{"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	// GET /api/me is open and says "not signed in"
	resp = jsonReq(t, "GET", srv.URL+"/api/me", "", nil, nil)
	var me struct {
		SignedIn bool `json:"signed_in"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || me.SignedIn {
		t.Errorf("/api/me before sign-in: %d %+v", resp.StatusCode, me)
	}
}

func TestSignInFlow(t *testing.T) {
	srv, _ := testServer(t)
	withShadow(t, "root:"+sha512Crypt("secret", "salty", 5000, false)+":1::::::\n")
	old := loginDelay
	loginDelay = time.Millisecond
	t.Cleanup(func() { loginDelay = old })

	if c := code(jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"wrong"}`, nil, nil)); c != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", c)
	}
	if c := code(jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"`+strings.Repeat("a", 200)+`"}`, nil, nil)); c != http.StatusBadRequest {
		t.Errorf("200-byte password: %d, want 400", c)
	}
	if c := code(jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"secret","extra":1}`, nil, nil)); c != http.StatusBadRequest {
		t.Errorf("unknown field: %d, want 400", c)
	}
	resp := jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"secret"}`, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("right password: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || len(cookie.Value) != 64 {
		t.Fatalf("session cookie: %+v", cookie)
	}
	// the session opens the API
	if c := code(jsonReq(t, "GET", srv.URL+"/api/me", "", cookie, nil)); c != http.StatusOK {
		t.Errorf("/api/me with session: %d", c)
	}
	resp = jsonReq(t, "GET", srv.URL+"/api/relay/config", "", cookie, nil)
	var cfg struct {
		Profiles []profile `json:"profiles"`
		Active   string    `json:"active"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&cfg)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(cfg.Profiles) != 1 || cfg.Profiles[0].StreamKey != "k" || cfg.Active != "YouTube" {
		t.Errorf("relay config: %d %+v", resp.StatusCode, cfg)
	}
	// a bad op is the client's fault, not a server error, and runs nothing
	if c := code(jsonReq(t, "POST", srv.URL+"/api/relay", `{"op":"explode"}`, cookie, nil)); c != http.StatusBadRequest {
		t.Errorf("bad op: %d, want 400", c)
	}
	if c := code(jsonReq(t, "POST", srv.URL+"/api/record", `{"op":"start","x":1}`, cookie, nil)); c != http.StatusBadRequest {
		t.Errorf("unknown field on record: %d, want 400", c)
	}
	// the preview fails cleanly when prudynt is not there
	if c := code(jsonReq(t, "GET", srv.URL+"/api/snapshot.jpg", "", cookie, nil)); c != http.StatusBadGateway {
		t.Errorf("snapshot without prudynt: %d, want 502", c)
	}
	// sign out ends it
	if c := code(jsonReq(t, "POST", srv.URL+"/api/logout", `{}`, cookie, nil)); c != http.StatusOK {
		t.Errorf("logout: %d", c)
	}
	if c := code(jsonReq(t, "GET", srv.URL+"/api/status", "", cookie, nil)); c != http.StatusUnauthorized {
		t.Errorf("after logout: %d, want 401", c)
	}
}

func TestSaveProfiles(t *testing.T) {
	_, sd := testServer(t)
	rl := newRelay(sd)
	old := isMounted
	isMounted = func(string) bool { return true }
	t.Cleanup(func() { isMounted = old })

	bad := []webuiConfig{
		{},
		{Profiles: []profile{{Name: "A", RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: "B"},
		{Profiles: []profile{{Name: "A", RTMPURL: "http://h/app", StreamKey: "k"}}, Active: "A"},
		{Profiles: []profile{{Name: "A", RTMPURL: "rtmp://h/app", StreamKey: "k 1"}}, Active: "A"},
		{Profiles: []profile{{Name: "A", RTMPURL: "rtmp://h/app", StreamKey: "k\"1"}}, Active: "A"},
		{Profiles: []profile{{Name: "A", RTMPURL: "rtmp://h/app", StreamKey: ""}}, Active: "A"},
		{Profiles: []profile{{Name: "A", RTMPURL: "rtmp://h/app", StreamKey: "k"}, {Name: "A", RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: "A"},
		{Profiles: []profile{{Name: "line\nbreak", RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: "line\nbreak"},
		{Profiles: []profile{{Name: strings.Repeat("n", 41), RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: strings.Repeat("n", 41)},
	}
	for i, c := range bad {
		if _, err := rl.saveProfiles(c); err == nil {
			t.Errorf("case %d accepted: %+v", i, c)
		}
	}
	// a good save writes webui.json and the relay's file, and reports the change
	good := webuiConfig{Profiles: []profile{{Name: "Track", RTMPURL: "rtmps://a.rtmps.youtube.com:443/live2", StreamKey: "new-key"}, {Name: "Home", RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: "Track"}
	changed, err := rl.saveProfiles(good)
	if err != nil || !changed {
		t.Fatalf("good save: %v %v", changed, err)
	}
	relayCfg, _ := readJSONFile(filepath.Join(sd, relayCfgName))
	if relayCfg["stream_key"] != "new-key" || relayCfg["rtmp_url"] != "rtmps://a.rtmps.youtube.com:443/live2" || relayCfg["enabled"] != true {
		t.Fatalf("relay config after save: %v", relayCfg)
	}
	if changed, err := rl.saveProfiles(good); err != nil || changed {
		t.Fatalf("same save again must report no change: %v %v", changed, err)
	}
	loaded, err := rl.loadProfiles()
	if err != nil || len(loaded.Profiles) != 2 || loaded.Active != "Track" {
		t.Fatalf("reload: %+v %v", loaded, err)
	}
	// a broken webui.json does not lock the page: a warning and a profile from the relay's file
	if err := os.WriteFile(rl.webuiPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = rl.loadProfiles()
	if err != nil || loaded.Warning == "" || len(loaded.Profiles) != 1 || loaded.Profiles[0].StreamKey != "new-key" {
		t.Fatalf("broken webui.json: %+v %v", loaded, err)
	}
}

func TestSignInLimitHoldsUnderParallelGuesses(t *testing.T) {
	srv, _ := testServer(t)
	withShadow(t, "root:"+sha512Crypt("secret", "salty", 5000, false)+":1::::::\n")
	old := loginDelay
	loginDelay = 0
	t.Cleanup(func() { loginDelay = old })

	const guesses = 25
	codes := make(chan int, guesses)
	for i := 0; i < guesses; i++ {
		go func(i int) {
			codes <- code(jsonReq(t, "POST", srv.URL+"/api/login", fmt.Sprintf(`{"password":"wrong-%d"}`, i), nil, nil))
		}(i)
	}
	checked, refused := 0, 0
	for i := 0; i < guesses; i++ {
		switch c := <-codes; c {
		case http.StatusUnauthorized:
			checked++
		case http.StatusTooManyRequests:
			refused++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	// exactly loginMaxFails guesses were checked against the hash, all in
	// parallel from one address; the rest were refused at once
	if checked != loginMaxFails || refused != guesses-loginMaxFails {
		t.Fatalf("checked %d, refused %d; want %d and %d", checked, refused, loginMaxFails, guesses-loginMaxFails)
	}
	// and the right password is refused too until the window passes
	if c := code(jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"secret"}`, nil, nil)); c != http.StatusTooManyRequests {
		t.Fatalf("right password after the limit: %d, want 429", c)
	}
}
