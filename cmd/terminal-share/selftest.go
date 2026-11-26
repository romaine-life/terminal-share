package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/romaine-life/terminal-share/internal/auth"
	"github.com/romaine-life/terminal-share/internal/server"
	"github.com/romaine-life/terminal-share/internal/store"
	"github.com/romaine-life/terminal-share/internal/testissuer"
	"github.com/romaine-life/terminal-share/web"
)

// What the image must serve for the page to draw: the page's own files and the engine
// the build stage put in static/vendor/, each with the type a browser needs.
var selfTestAssets = map[string]string{
	"/assets/share.js":     "text/javascript",
	"/assets/share-lib.js": "text/javascript",
	"/assets/share.css":    "text/css",
	"/assets/fonts/CaskaydiaMonoNerdFont-Regular.woff2": "font/woff2",
	"/assets/fonts/CaskaydiaMonoNerdFont-Bold.woff2":    "font/woff2",
	"/assets/vendor/rioterm/dist/index.js":              "text/javascript",
	"/assets/vendor/rioterm/wasm/librio_wasm.js":        "text/javascript",
	"/assets/vendor/rioterm/wasm/librio_wasm_bg.wasm":   "application/wasm",
}

// runSelfTest serves the built binary's routes on loopback, with an in-memory store and a
// stand-in for auth.romaine.life, and checks each: the image's check runs it read-only with
// no network beyond the host.
func runSelfTest() error {
	key, err := testissuer.NewKey("self-test")
	if err != nil {
		return err
	}
	jwks, err := serve(testissuer.NewJWKS(key))
	if err != nil {
		return err
	}
	srv := &server.Server{
		Store:     store.NewMemory(),
		Auth:      auth.New(jwks+"/jwks", nil, nil),
		PublicURL: "https://share.romaine.life",
		Static:    web.Static(),
	}
	base, err := serve(srv.Handler())
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}

	if err := expect(client, "GET", base+"/healthz", nil, "", 200, "text/plain"); err != nil {
		return err
	}
	for _, page := range []string{"/", "/s/abcdefghijklmnopqrstuvwxyz"} {
		if err := expect(client, "GET", base+page, nil, "", 200, "text/html"); err != nil {
			return err
		}
	}
	for path, ctype := range selfTestAssets {
		if err := expect(client, "GET", base+path, nil, "", 200, ctype); err != nil {
			return err
		}
	}
	if _, err := fs.Stat(web.Static(), "vendor/rioterm/package.json"); err != nil {
		return fmt.Errorf("the engine isn't embedded: %w", err)
	}

	snapshot := map[string]any{
		"cols": 20, "rows": 2, "vt": "\x1b[32mself-test\x1b[0m\r\n$ ", "title": "self-test",
		"theme": map[string]any{"foreground": "#cccccc", "background": "#000000", "cursor": "#ffffff",
			"palette": strings.Split("#000000 #800000 #008000 #808000 #000080 #800080 #008080 #c0c0c0 #808080 #ff0000 #00ff00 #ffff00 #0000ff #ff00ff #00ffff #ffffff", " ")},
		"font": map[string]any{"family": "CaskaydiaMono NF"},
	}
	body, _ := json.Marshal(snapshot)
	if err := expect(client, "POST", base+"/api/snapshots", body, "", 401, "application/json"); err != nil {
		return err
	}
	token := key.Sign(testissuer.Machine("nelsonpc", time.Now()))
	created, err := request(client, "POST", base+"/api/snapshots", body, token)
	if err != nil {
		return err
	}
	if created.status != 201 {
		return fmt.Errorf("POST /api/snapshots: HTTP %d: %s", created.status, created.body)
	}
	var answer struct{ ID, URL string }
	if err := json.Unmarshal(created.body, &answer); err != nil || answer.ID == "" {
		return fmt.Errorf("POST /api/snapshots answered %s", created.body)
	}
	read, err := request(client, "GET", base+"/api/snapshots/"+answer.ID, nil, "")
	if err != nil {
		return err
	}
	var record map[string]any
	if read.status != 200 || json.Unmarshal(read.body, &record) != nil || record["vt"] != snapshot["vt"] || record["device"] != "nelsonpc" {
		return fmt.Errorf("GET /api/snapshots/<id>: HTTP %d: %s", read.status, read.body)
	}
	return expect(client, "GET", base+"/api/snapshots/abcdefghijklmnopqrstuvwxyz", nil, "", 404, "application/json")
}

func serve(handler http.Handler) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go func() { _ = http.Serve(listener, handler) }()
	return "http://" + listener.Addr().String(), nil
}

type response struct {
	status int
	ctype  string
	body   []byte
}

func request(client *http.Client, method, url string, body []byte, token string) (response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return response{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return response{resp.StatusCode, resp.Header.Get("Content-Type"), data}, err
}

func expect(client *http.Client, method, url string, body []byte, token string, status int, ctype string) error {
	got, err := request(client, method, url, body, token)
	if err != nil {
		return err
	}
	if got.status != status || !strings.HasPrefix(got.ctype, ctype) || len(got.body) == 0 {
		return fmt.Errorf("%s %s: HTTP %d, %q, %d bytes; want HTTP %d, %s", method, url, got.status, got.ctype, len(got.body), status, ctype)
	}
	return nil
}
