// Package server answers terminal-share's HTTP routes: the snapshot API, the page that
// draws a snapshot, and the health check.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/romaine-life/terminal-share/internal/auth"
	"github.com/romaine-life/terminal-share/internal/store"
)

const (
	// MaxBody is the largest snapshot a home PC may post.
	MaxBody = 1 << 20
	// Lifetime is how long a share's link works.
	Lifetime = 30 * 24 * time.Hour

	missingSentence = "This share has expired or never existed."
	// The page draws with WebAssembly, fetched from this origin like everything else.
	pageCSP = "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; font-src 'self'; " +
		"connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

// Authenticator turns a bearer token into the home PC it was minted for; a refusal is an
// *auth.Error.
type Authenticator interface {
	Device(ctx context.Context, token string) (string, error)
}

type Server struct {
	Store store.Store
	Auth  Authenticator
	// PublicURL is the origin share links name, such as https://share.romaine.life.
	PublicURL string
	// Static is the page's files: index.html, share.html and what they load.
	Static fs.FS
	// Log receives one line per request.
	Log io.Writer
	// Now is the clock; time.Now when nil.
	Now func() time.Time

	logMu sync.Mutex
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Handler is every route, each request logged.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/snapshots", s.createSnapshot)
	mux.HandleFunc("GET /api/snapshots/{id}", s.getSnapshot)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "There is no such API route.")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /{$}", s.page("index.html", false))
	mux.HandleFunc("GET /s/{id}", s.page("share.html", true))
	mux.Handle("GET /assets/", http.StripPrefix("/assets", s.assets()))
	return s.logged(mux)
}

// requestInfo is what a handler adds to its request's log line.
type requestInfo struct {
	device string
	err    string
}

type infoKey struct{}

func info(r *http.Request) *requestInfo {
	if i, ok := r.Context().Value(infoKey{}).(*requestInfo); ok {
		return i
	}
	return &requestInfo{}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// logged writes one line per request: method, path, status, duration, and the device
// for a share. A share's id is the key to it, so the line keeps only its first characters;
// tokens and snapshot content never reach the log. A passing health check isn't logged:
// the kubelet's probes would be most of the lines.
func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		ri := &requestInfo{}
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), infoKey{}, ri)))
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		if r.URL.Path == "/healthz" && sw.status == http.StatusOK {
			return
		}
		line := fmt.Sprintf("%s %s %s %d %dms", start.UTC().Format(time.RFC3339), r.Method, redactPath(r.URL.Path), sw.status, s.now().Sub(start).Milliseconds())
		if ri.device != "" {
			line += " device=" + ri.device
		}
		if ri.err != "" {
			line += fmt.Sprintf(" error=%q", ri.err)
		}
		if s.Log != nil {
			s.logMu.Lock()
			_, _ = fmt.Fprintln(s.Log, line)
			s.logMu.Unlock()
		}
	})
}

func redactPath(path string) string {
	for _, prefix := range []string{"/s/", "/api/snapshots/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok && len(rest) > 6 {
			return prefix + rest[:6] + "…"
		}
	}
	return path
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, sentence string) {
	writeJSON(w, status, map[string]string{"error": sentence})
}

func (s *Server) createSnapshot(w http.ResponseWriter, r *http.Request) {
	token, ok := bearer(r.Header.Get("Authorization"))
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "Sharing needs an auth.romaine.life machine token as Authorization: Bearer <token>.")
		return
	}
	device, err := s.Auth.Device(r.Context(), token)
	if err != nil {
		var refusal *auth.Error
		if !errors.As(err, &refusal) {
			refusal = &auth.Error{Status: http.StatusUnauthorized, Message: "The token is not a valid auth.romaine.life token."}
		}
		if refusal.Status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		info(r).err = refusal.Message
		writeError(w, refusal.Status, refusal.Message)
		return
	}
	info(r).device = device

	tooLarge := fmt.Sprintf("The snapshot is larger than %d MiB.", MaxBody>>20)
	if r.ContentLength > MaxBody {
		writeError(w, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "The body could not be read.")
		return
	}
	snapshot, err := parseSnapshot(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created := s.now().UTC().Truncate(time.Second)
	record := Record{
		ID:        newID(),
		CreatedAt: created.Format(time.RFC3339),
		ExpiresAt: created.Add(Lifetime).Format(time.RFC3339),
		Device:    device,
		Snapshot:  snapshot,
	}
	data, err := json.Marshal(record)
	if err != nil {
		info(r).err = err.Error()
		writeError(w, http.StatusInternalServerError, "The snapshot could not be stored; try again.")
		return
	}
	if err := s.Store.Put(r.Context(), record.ID, data); err != nil {
		info(r).err = err.Error()
		writeError(w, http.StatusInternalServerError, "The snapshot could not be stored; try again.")
		return
	}
	url := strings.TrimRight(s.PublicURL, "/") + "/s/" + record.ID
	w.Header().Set("Location", url)
	writeJSON(w, http.StatusCreated, map[string]string{"id": record.ID, "url": url, "expires_at": record.ExpiresAt})
}

func bearer(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusNotFound, missingSentence)
		return
	}
	data, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, missingSentence)
		return
	}
	if err != nil {
		info(r).err = err.Error()
		writeError(w, http.StatusInternalServerError, "The share could not be read; try again.")
		return
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		info(r).err = "stored record: " + err.Error()
		writeError(w, http.StatusInternalServerError, "The share could not be read; try again.")
		return
	}
	// The blob's lifecycle rule deletes it some time after it expires; until then the
	// server is what refuses it.
	expires, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil || !s.now().Before(expires) {
		writeError(w, http.StatusNotFound, missingSentence)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, record)
}

// page serves one of the static pages. A share's page fetches its snapshot itself, so it
// is the same file for every id, and it says so when the id is unknown.
func (s *Server) page(name string, share bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(s.Static, name)
		if err != nil {
			http.Error(w, "page missing", http.StatusInternalServerError)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", pageCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache")
		if share {
			h.Set("X-Robots-Tag", "noindex")
		}
		_, _ = w.Write(data)
	}
}

// Content types set here rather than from the system's table: the image has none, and
// WebAssembly must be application/wasm to compile while it streams.
var contentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".wasm":  "application/wasm",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

func (s *Server) assets() http.Handler {
	files := http.FileServerFS(s.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No directory listings.
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		for ext, ctype := range contentTypes {
			if strings.HasSuffix(r.URL.Path, ext) {
				w.Header().Set("Content-Type", ctype)
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
