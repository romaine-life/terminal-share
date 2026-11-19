package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/romaine-life/terminal-share/internal/auth"
	"github.com/romaine-life/terminal-share/internal/store"
	"github.com/romaine-life/terminal-share/internal/testissuer"
)

var (
	keyOnce sync.Once
	key     *testissuer.Key
)

type harness struct {
	handler http.Handler
	store   *store.Memory
	log     *bytes.Buffer
	now     time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if key, err = testissuer.NewKey("test"); err != nil {
			panic(err)
		}
	})
	jwks := httptest.NewServer(testissuer.NewJWKS(key))
	t.Cleanup(jwks.Close)
	h := &harness{store: store.NewMemory(), log: &bytes.Buffer{}, now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return h.now }
	srv := &Server{
		Store:     h.store,
		Auth:      auth.New(jwks.URL, jwks.Client(), clock),
		PublicURL: "https://share.romaine.life",
		Static: fstest.MapFS{
			"index.html": {Data: []byte("<h1>terminal-share</h1>")},
			"share.html": {Data: []byte("<h1>share</h1>")},
			"share.js":   {Data: []byte("export {};")},
		},
		Log: h.log,
		Now: clock,
	}
	h.handler = srv.Handler()
	return h
}

func (h *harness) token(device string) string { return key.Sign(testissuer.Machine(device, h.now)) }

func validBody() map[string]any {
	return map[string]any{
		"cols":  80,
		"rows":  24,
		"vt":    "\x1b[H\x1b[2J\x1b[1;32mnelson\x1b[0m:~$ ls\r\n docs",
		"title": "at:1 shell",
		"theme": map[string]any{
			"foreground": "#CCCCCC", "background": "#1e1e1e", "cursor": "#ffffff",
			"palette": []string{"#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
				"#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"},
		},
		"font": map[string]any{"family": "CaskaydiaMono NF"},
	}
}

func (h *harness) do(method, path string, body []byte, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h *harness) post(t *testing.T, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return h.do("POST", "/api/snapshots", data, token)
}

// errorOf checks the response is status with the JSON error shape, and returns its sentence.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body) != 1 || body["error"] == "" {
		t.Fatalf("not an {\"error\": ...} body: %s", rec.Body)
	}
	if !strings.HasSuffix(body["error"], ".") {
		t.Fatalf("error isn't a sentence: %q", body["error"])
	}
	return body["error"]
}

func TestRoundTrip(t *testing.T) {
	h := newHarness(t)
	rec := h.post(t, validBody(), h.token("nelsonpc"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body)
	}
	var created map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created["id"]
	if !validID(id) || len(id) != 26 {
		t.Fatalf("id %q is not 26 base32 characters", id)
	}
	if created["url"] != "https://share.romaine.life/s/"+id || rec.Header().Get("Location") != created["url"] {
		t.Fatalf("url %q, Location %q", created["url"], rec.Header().Get("Location"))
	}
	if created["expires_at"] != "2026-12-18T10:00:00Z" {
		t.Fatalf("expires_at %q, want created + 30 days", created["expires_at"])
	}

	got := h.do("GET", "/api/snapshots/"+id, nil, "")
	if got.Code != http.StatusOK {
		t.Fatalf("GET: HTTP %d: %s", got.Code, got.Body)
	}
	var record map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	want := validBody()
	for _, field := range []string{"vt", "title"} {
		if record[field] != want[field] {
			t.Fatalf("%s = %q, want %q", field, record[field], want[field])
		}
	}
	if record["cols"] != float64(80) || record["rows"] != float64(24) {
		t.Fatalf("cols, rows = %v, %v", record["cols"], record["rows"])
	}
	theme, _ := json.Marshal(record["theme"])
	wantTheme, _ := json.Marshal(want["theme"])
	if !bytes.Equal(theme, wantTheme) {
		t.Fatalf("theme = %s, want %s", theme, wantTheme)
	}
	if record["font"].(map[string]any)["family"] != "CaskaydiaMono NF" {
		t.Fatalf("font = %v", record["font"])
	}
	for field, value := range map[string]string{"id": id, "created_at": "2026-11-18T10:00:00Z", "expires_at": "2026-12-18T10:00:00Z", "device": "nelsonpc"} {
		if record[field] != value {
			t.Fatalf("%s = %v, want %s", field, record[field], value)
		}
	}
}

func TestIDsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := newID()
		if !validID(id) || seen[id] {
			t.Fatalf("id %q repeated or malformed", id)
		}
		seen[id] = true
	}
}

func TestAuthRefusals(t *testing.T) {
	h := newHarness(t)
	other, err := testissuer.NewKey("test") // the published key id, signed by another key
	if err != nil {
		t.Fatal(err)
	}
	notMachine := testissuer.Machine("nelsonpc", h.now)
	notMachine["purpose"] = "user"
	cases := []struct {
		name   string
		header string
		status int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"not bearer", "Basic bmVsc29uOnB3", http.StatusUnauthorized},
		{"forged signature", "Bearer " + other.Sign(testissuer.Machine("nelsonpc", h.now)), http.StatusUnauthorized},
		{"not a machine token", "Bearer " + key.Sign(notMachine), http.StatusForbidden},
		{"not a home PC", "Bearer " + h.token("nelson-laptop"), http.StatusForbidden},
	}
	body, _ := json.Marshal(validBody())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/snapshots", bytes.NewReader(body))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)
			errorOf(t, rec, tc.status)
		})
	}
	// Each home PC is accepted.
	for _, pc := range []string{"nelsonpc", "upstairspc", "treadmill", "nelsonlaptop"} {
		if rec := h.post(t, validBody(), h.token(pc)); rec.Code != http.StatusCreated {
			t.Fatalf("%s: HTTP %d: %s", pc, rec.Code, rec.Body)
		}
	}
}

func TestValidation(t *testing.T) {
	h := newHarness(t)
	token := h.token("nelsonpc")
	set := func(path string, value any) map[string]any {
		body := validBody()
		parts := strings.Split(path, ".")
		target := body
		for _, part := range parts[:len(parts)-1] {
			target = target[part].(map[string]any)
		}
		if value == nil {
			delete(target, parts[len(parts)-1])
		} else {
			target[parts[len(parts)-1]] = value
		}
		return body
	}
	palette := func(n int) []string {
		p := make([]string, n)
		for i := range p {
			p[i] = "#000000"
		}
		return p
	}
	cases := []struct {
		name string
		body any
		want string
	}{
		{"cols missing", set("cols", nil), "cols"},
		{"cols zero", set("cols", 0), "cols"},
		{"cols too wide", set("cols", 1001), "cols"},
		{"cols fractional", set("cols", 80.5), "cols"},
		{"cols a string", set("cols", "80"), "cols"},
		{"rows zero", set("rows", 0), "rows"},
		{"rows too tall", set("rows", 501), "rows"},
		{"vt missing", set("vt", nil), "vt"},
		{"vt empty", set("vt", ""), "vt"},
		{"title too long", set("title", strings.Repeat("x", 257)), "title"},
		{"theme missing", set("theme", nil), "theme"},
		{"foreground not hex", set("theme.foreground", "red"), "theme.foreground"},
		{"background short hex", set("theme.background", "#fff"), "theme.background"},
		{"cursor missing", set("theme.cursor", nil), "theme.cursor"},
		{"palette 15", set("theme.palette", palette(15)), "theme.palette"},
		{"palette 17", set("theme.palette", palette(17)), "theme.palette"},
		{"palette bad color", set("theme.palette", append(palette(15), "#gggggg")), "theme.palette[15]"},
		{"palette numbers", set("theme.palette", []int{1, 2}), "theme.palette"},
		{"font missing", set("font", nil), "font.family"},
		{"family empty", set("font.family", " "), "font.family"},
		{"family with quotes", set("font.family", `Mono", evil`), "font.family"},
		{"array body", []any{1}, "JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sentence := errorOf(t, h.post(t, tc.body, token), http.StatusBadRequest)
			if !strings.Contains(sentence, tc.want) {
				t.Fatalf("%q doesn't name %s", sentence, tc.want)
			}
		})
	}
	for name, raw := range map[string]string{"not JSON": "{", "two objects": "{} {}", "null": "null"} {
		t.Run(name, func(t *testing.T) {
			errorOf(t, h.do("POST", "/api/snapshots", []byte(raw), token), http.StatusBadRequest)
		})
	}
	// A title may be empty or absent.
	if rec := h.post(t, set("title", nil), token); rec.Code != http.StatusCreated {
		t.Fatalf("no title: HTTP %d: %s", rec.Code, rec.Body)
	}
}

func TestSizeLimit(t *testing.T) {
	h := newHarness(t)
	token := h.token("nelsonpc")
	body := validBody()
	body["vt"] = strings.Repeat("x", MaxBody)
	errorOf(t, h.post(t, body, token), http.StatusRequestEntityTooLarge)

	// Without a Content-Length the limit holds while reading.
	data, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/snapshots", struct{ *bytes.Reader }{bytes.NewReader(data)})
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	errorOf(t, rec, http.StatusRequestEntityTooLarge)

	// Just under the limit is accepted.
	body["vt"] = ""
	base, _ := json.Marshal(body)
	body["vt"] = strings.Repeat("x", MaxBody-len(base))
	data, _ = json.Marshal(body)
	if len(data) != MaxBody {
		t.Fatalf("body is %d bytes, want exactly %d", len(data), MaxBody)
	}
	if rec := h.do("POST", "/api/snapshots", data, token); rec.Code != http.StatusCreated {
		t.Fatalf("a body of exactly 1 MiB: HTTP %d: %s", rec.Code, rec.Body)
	}
}

func TestExpiredAndMissing(t *testing.T) {
	h := newHarness(t)
	rec := h.post(t, validBody(), h.token("nelsonpc"))
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"]

	h.now = h.now.Add(Lifetime - time.Second)
	if got := h.do("GET", "/api/snapshots/"+id, nil, ""); got.Code != http.StatusOK {
		t.Fatalf("a second before expiry: HTTP %d", got.Code)
	}
	h.now = h.now.Add(time.Second)
	for name, path := range map[string]string{
		"expired":   "/api/snapshots/" + id,
		"never was": "/api/snapshots/abcdefghijklmnopqrstuvwxyz",
		"malformed": "/api/snapshots/not-an-id",
	} {
		t.Run(name, func(t *testing.T) {
			if sentence := errorOf(t, h.do("GET", path, nil, ""), http.StatusNotFound); sentence != "This share has expired or never existed." {
				t.Fatalf("sentence %q", sentence)
			}
		})
	}
}

type failingStore struct{}

func (failingStore) Put(context.Context, string, []byte) error { return errors.New("storage down") }
func (failingStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("storage down")
}

func TestStoreFailureIsAServerError(t *testing.T) {
	h := newHarness(t)
	srv := &Server{Store: failingStore{}, Auth: authFunc(func(context.Context, string) (string, error) { return "nelsonpc", nil }), Log: h.log}
	h.handler = srv.Handler()
	errorOf(t, h.post(t, validBody(), "token"), http.StatusInternalServerError)
	errorOf(t, h.do("GET", "/api/snapshots/abcdefghijklmnopqrstuvwxyz", nil, ""), http.StatusInternalServerError)
	if !strings.Contains(h.log.String(), `error="storage down"`) {
		t.Fatalf("the log line doesn't say why: %s", h.log)
	}
}

type authFunc func(context.Context, string) (string, error)

func (f authFunc) Device(ctx context.Context, token string) (string, error) { return f(ctx, token) }

func TestLogLines(t *testing.T) {
	h := newHarness(t)
	token := h.token("nelsonpc")
	rec := h.post(t, validBody(), token)
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	h.do("GET", "/s/"+created["id"], nil, "")
	h.do("GET", "/healthz", nil, "")

	lines := strings.Split(strings.TrimSpace(h.log.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want a line for the POST and the page, none for the passing health check:\n%s", h.log)
	}
	if !strings.Contains(lines[0], "POST /api/snapshots 201 ") || !strings.HasSuffix(lines[0], " device=nelsonpc") {
		t.Fatalf("POST line: %s", lines[0])
	}
	if !strings.Contains(lines[1], "GET /s/"+created["id"][:6]+"… 200 ") {
		t.Fatalf("page line: %s", lines[1])
	}
	for _, secret := range []string{token, created["id"], "nelson\x1b", "docs"} {
		if strings.Contains(h.log.String(), secret) {
			t.Fatalf("the log holds %q", secret)
		}
	}
}

func TestPagesAndHealth(t *testing.T) {
	h := newHarness(t)
	for path, want := range map[string]string{"/": "terminal-share", "/s/abcdefghijklmnopqrstuvwxyz": "share", "/healthz": "ok"} {
		rec := h.do("GET", path, nil, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: HTTP %d: %s", path, rec.Code, rec.Body)
		}
	}
	if csp := h.do("GET", "/s/abcdefghijklmnopqrstuvwxyz", nil, "").Header().Get("Content-Security-Policy"); !strings.Contains(csp, "'wasm-unsafe-eval'") {
		t.Fatalf("share page CSP %q", csp)
	}
	if rec := h.do("GET", "/assets/share.js", nil, ""); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("share.js: HTTP %d, %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := h.do("GET", "/assets/", nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("asset directory listing: HTTP %d", rec.Code)
	}
	errorOf(t, h.do("GET", "/api/nothing", nil, ""), http.StatusNotFound)
}
