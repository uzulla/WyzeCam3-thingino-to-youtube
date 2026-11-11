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
	testServers.Store(srv, s)
	t.Cleanup(func() { testServers.Delete(srv); srv.Close() })
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
		{"GET", "/api/osd"}, {"POST", "/api/osd/element"}, {"POST", "/api/osd/feed"}, {"GET", "/api/osd/remotes"},
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
		SignedIn bool   `json:"signed_in"`
		Version  string `json:"version"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || me.SignedIn {
		t.Errorf("/api/me before sign-in: %d %+v", resp.StatusCode, me)
	}
	if me.Version != version {
		t.Errorf("/api/me version: %q, want %q (shown under the sign-in form)", me.Version, version)
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

	bad := []lapcamConfig{
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
	// a good save writes lapcam.json and the relay's file, and reports the change
	good := lapcamConfig{Profiles: []profile{{Name: "Track", RTMPURL: "rtmps://a.rtmps.youtube.com:443/live2", StreamKey: "new-key"}, {Name: "Home", RTMPURL: "rtmp://h/app", StreamKey: "k"}}, Active: "Track"}
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
	// a broken lapcam.json does not lock the page: a warning and a profile from the relay's file
	if err := os.WriteFile(rl.lapcamPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = rl.loadProfiles()
	if err != nil || loaded.Warning == "" || len(loaded.Profiles) != 1 || loaded.Profiles[0].StreamKey != "new-key" {
		t.Fatalf("broken lapcam.json: %+v %v", loaded, err)
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

func TestRelayConfigured(t *testing.T) {
	sd := t.TempDir()
	rl := newRelay(sd)
	// no youtube-relay.json on the card (and none in /etc on the test machine)
	if exists(relayEtcCfg) {
		t.Skip("/etc/youtube-relay.json exists on this machine")
	}
	if rl.configured() {
		t.Errorf("configured without a config file")
	}
	for _, body := range []string{`{}`, `{"stream_key": ""}`, `{"stream_key": "  "}`, `not json`} {
		if err := os.WriteFile(filepath.Join(sd, relayCfgName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if rl.configured() {
			t.Errorf("configured with %q", body)
		}
	}
	if err := os.WriteFile(filepath.Join(sd, relayCfgName), []byte(`{"stream_key": "abcd-1234"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !rl.configured() {
		t.Errorf("not configured with a stream key")
	}
}

func TestChangePassword(t *testing.T) {
	srv, _ := testServer(t)
	withShadow(t, "root:"+sha512Crypt("secret", "salty", 5000, false)+":1::::::\nnobody:*:1::::::\n")
	// sign in; the password is not the default
	resp := jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"secret"}`, nil, nil)
	var login struct {
		SignedIn        bool `json:"signed_in"`
		DefaultPassword bool `json:"default_password"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if !login.SignedIn || login.DefaultPassword {
		t.Fatalf("login: %+v", login)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	post := func(body string) int {
		return code(jsonReq(t, "POST", srv.URL+"/api/password", body, cookie, nil))
	}
	if c := post(`{"current":"wrong","new":"longenough"}`); c != http.StatusForbidden {
		t.Errorf("wrong current password: %d (403: 401 would send the app to the sign-in screen)", c)
	}
	if c := post(`{"current":"secret","new":""}`); c != http.StatusBadRequest {
		t.Errorf("empty new password: %d", c)
	}
	if c := post(`{"current":"secret","new":"with:colon1"}`); c != http.StatusBadRequest {
		t.Errorf("colon in password: %d", c)
	}
	if c := post(`{"current":"secret","new":"ab"}`); c != http.StatusOK {
		t.Fatalf("short password refused (no length rule wanted): %d", c)
	}
	if ok, err := checkPassword("ab"); err != nil || !ok {
		t.Errorf("short password does not verify: %v %v", ok, err)
	}
	if c := post(`{"current":"ab","new":"new-password-1"}`); c != http.StatusOK {
		t.Fatalf("change: %d", c)
	}
	// the file: root's hash changed and verifies, the other line is untouched
	b, _ := os.ReadFile(shadowFile)
	if !strings.Contains(string(b), "\nnobody:*:1::::::\n") {
		t.Errorf("other lines changed: %q", b)
	}
	if ok, err := checkPassword("new-password-1"); err != nil || !ok {
		t.Errorf("new password does not verify: %v %v", ok, err)
	}
	if ok, _ := checkPassword("secret"); ok {
		t.Errorf("old password still verifies")
	}
	// the session that made the change is still good
	if c := code(jsonReq(t, "GET", srv.URL+"/api/status", "", cookie, nil)); c != http.StatusOK {
		t.Errorf("session after the change: %d", c)
	}
	// signing in with the default password is flagged
	withShadow(t, "root:"+sha512Crypt("root", "salty", 5000, false)+":1::::::\n")
	resp = jsonReq(t, "POST", srv.URL+"/api/login", `{"password":"root"}`, nil, nil)
	_ = json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if !login.SignedIn || !login.DefaultPassword {
		t.Errorf("default password not flagged: %+v", login)
	}
	// without a session the endpoint is closed
	if c := post(``); c == http.StatusOK {
		t.Errorf("empty body accepted")
	}
	if c := code(jsonReq(t, "POST", srv.URL+"/api/password", `{"current":"root","new":"new-password-2"}`, nil, nil)); c != http.StatusUnauthorized {
		t.Errorf("password change without a session: %d", c)
	}
}

// The OSD tab's "picture from a URL" for an image slot: osd-feed.json gets
// url / interval_ms (and loses png), "" takes it away (the logo slot gets
// its logo back), bad input changes nothing
func TestOSDFeedPictureURL(t *testing.T) {
	srv, sd := testServer(t)
	v, _ := testServers.Load(srv)
	s := v.(*server)
	stub := filepath.Join(t.TempDir(), "S94osd-feed")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := osdFeedInit
	osdFeedInit = stub
	t.Cleanup(func() { osdFeedInit = old })
	feed := `{"slots":{"imagefile":{"png":"/mnt/mmcblk0p1/logo.png","fit":"contain"},"imagefile3":{"track":{"source":"racebox"}},"textfile":{"template":"x"}},"sources":{"racebox":{"type":"udp","listen":":5005"}}}`
	if err := os.WriteFile(filepath.Join(sd, osdFeedName), []byte(feed), 0o644); err != nil {
		t.Fatal(err)
	}
	ms := 500
	if err := s.feedEdit("imagefile3", nil, nil, nil, &feedPicture{URL: sp("http://192.168.11.113/jpg?w=320&h=240"), IntervalMs: &ms}); err != nil {
		t.Fatal(err)
	}
	slots, err := s.feedRead()
	if err != nil {
		t.Fatal(err)
	}
	if got := slots["imagefile3"]; got.URL != "http://192.168.11.113/jpg?w=320&h=240" || got.IntervalMs != 500 || !got.Track {
		t.Errorf("imagefile3: %+v", got)
	}
	// the picture element: a slot of its own, made on first use and removed when the URL is cleared
	if err := s.feedEdit("imagefile4", nil, nil, nil, &feedPicture{URL: sp("http://h/p.png")}); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.feedRead()
	if got := slots["imagefile4"]; got.URL != "http://h/p.png" || got.PNG != "" {
		t.Errorf("imagefile4 with a URL: %+v", got)
	}
	if err := s.feedEdit("imagefile4", nil, nil, nil, &feedPicture{URL: sp("")}); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.feedRead()
	if _, ok := slots["imagefile4"]; ok {
		t.Errorf("imagefile4 after clearing the URL must be gone: %+v", slots["imagefile4"])
	}
	// clearing the URL of a slot that also has a track keeps the track
	if err := s.feedEdit("imagefile3", nil, nil, nil, &feedPicture{URL: sp("")}); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.feedRead()
	if got := slots["imagefile3"]; got.URL != "" || !got.Track {
		t.Errorf("imagefile3 after clearing the URL: %+v", got)
	}
	// refused, and the file untouched
	before, _ := os.ReadFile(filepath.Join(sd, osdFeedName))
	bad := 50
	for name, pic := range map[string]feedPicture{
		"ftp":       {URL: sp("ftp://h/p")},
		"relative":  {URL: sp("/jpg")},
		"control":   {URL: sp("http://h/p\n")},
		"too long":  {URL: sp("http://h/" + strings.Repeat("a", 600))},
		"interval":  {URL: sp("http://h/p"), IntervalMs: &bad},
		"ms no url": {IntervalMs: &ms}, // imagefile3 has no URL any more
	} {
		if err := s.feedEdit("imagefile3", nil, nil, nil, &pic); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	if err := s.feedEdit("textfile", nil, nil, nil, &feedPicture{URL: sp("http://h/p")}); err == nil {
		t.Error("a text slot cannot have a picture")
	}
	after, _ := os.ReadFile(filepath.Join(sd, osdFeedName))
	if string(before) != string(after) {
		t.Error("a refused edit must not change the file")
	}
	// the interval alone keeps the URL the file has
	if err := s.feedEdit("imagefile4", nil, nil, nil, &feedPicture{URL: sp("http://h/p.png"), IntervalMs: &ms}); err != nil {
		t.Fatal(err)
	}
	two := 2000
	if err := s.feedEdit("imagefile4", nil, nil, nil, &feedPicture{IntervalMs: &two}); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.feedRead()
	if got := slots["imagefile4"]; got.URL != "http://h/p.png" || got.IntervalMs != 2000 {
		t.Errorf("interval alone: %+v", got)
	}
	// uploading a logo PNG into a slot that had a URL: the URL goes (one picture per slot)
	if err := s.feedEdit("imagefile", nil, nil, nil, &feedPicture{URL: sp("http://h/p.png"), IntervalMs: &ms}); err != nil {
		t.Fatal(err)
	}
	if err := s.feedSetPNG("imagefile", "/mnt/x/logo.png"); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.feedRead()
	if got := slots["imagefile"]; got.URL != "" || got.IntervalMs != 0 || got.PNG != "/mnt/x/logo.png" {
		t.Errorf("after feedSetPNG: %+v", got)
	}
}

func sp(s string) *string { return &s }
