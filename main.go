package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"io"
	"strings"
	"sync"

	"github.com/skip2/go-qrcode"
)

// version is overridden at release time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

type link struct {
	target string // redirect URL, or the payload text for secrets
	once   bool   // burn after the reveal click
	secret bool   // serve target as plain text instead of redirecting
}

type store struct {
	mu    sync.Mutex
	links map[string]link
}

func newStore() *store {
	return &store{links: make(map[string]link)}
}

func randCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:6]
}

func (s *store) save(l link) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range 5 {
		code := randCode()
		if _, exists := s.links[code]; !exists {
			s.links[code] = l
			return code, nil
		}
	}
	return "", fmt.Errorf("could not allocate a short code")
}

// saveSlug claims a caller-chosen slug; false means it is already taken.
func (s *store) saveSlug(slug string, l link) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.links[slug]; exists {
		return false
	}
	s.links[slug] = l
	return true
}

// slugs served by this mux itself must never be claimable.
var reservedSlugs = map[string]bool{"shorten": true, "relays": true, "qr": true, "s": true, "thumbnail.jpg": true}

func validSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 64 {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (s *store) get(code string) (link, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	return l, ok
}

// burn deletes a one-time link before it is served, so an aborted or
// replayed request cannot reveal it a second time.
func (s *store) burn(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, code)
}

func respondJSON(w http.ResponseWriter, status int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(obj)
}

// shorten serves both a JSON API (POST, for programs) and a plain-text
// GET API (GET /shorten?url=..., for a curl one-liner an agent can embed
// straight into its output without parsing JSON).
func (s *store) shorten(w http.ResponseWriter, r *http.Request) {
	plain := r.Method == http.MethodGet
	var target, slug string
	var once bool
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		target = q.Get("url")
		slug = q.Get("slug")
		once = q.Get("once") == "1" || q.Get("once") == "true"
	case http.MethodPost:
		var body struct {
			URL  string `json:"url"`
			Slug string `json:"slug"`
			Once bool   `json:"once"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			shortenError(w, plain, http.StatusBadRequest, err.Error())
			return
		}
		target = body.URL
		slug = body.Slug
		once = body.Once
	default:
		shortenError(w, plain, http.StatusMethodNotAllowed, "GET or POST only")
		return
	}

	u, err := url.ParseRequestURI(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		shortenError(w, plain, http.StatusBadRequest, "url must be absolute http(s)")
		return
	}

	var code string
	if slug != "" {
		if !validSlug(slug) {
			shortenError(w, plain, http.StatusBadRequest, "slug must be 1-64 letters, digits, '-' or '_'")
			return
		}
		if reservedSlugs[slug] {
			shortenError(w, plain, http.StatusBadRequest, "slug is reserved")
			return
		}
		if !s.saveSlug(slug, link{target: target, once: once}) {
			shortenError(w, plain, http.StatusConflict, "slug already taken")
			return
		}
		code = slug
	} else {
		code, err = s.save(link{target: target, once: once})
		if err != nil {
			shortenError(w, plain, http.StatusInternalServerError, err.Error())
			return
		}
	}
	shortURLs := s.shortURLs(r, code)
	if plain {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, u := range shortURLs {
			fmt.Fprintln(w, u)
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"code":       code,
		"short_url":  shortURLs[0],
		"short_urls": shortURLs,
	})
}

// activeRelayBaseURLs, when running under `-portal`, lists every currently
// connected relay's public base URL so /shorten can return one short link
// per portal instead of just the one the request happened to arrive through.
var activeRelayBaseURLs func() []string

// publicBaseURL assumes https, since `portal expose` relays this plain-http
// server's port as a public https:// tunnel and never forwards a scheme
// header (it's a raw TCP relay, not an HTTP-aware proxy). Only a direct hit
// on localhost is genuinely http.
func publicBaseURL(r *http.Request) string {
	host, _, _ := strings.Cut(r.Host, ":")
	scheme := "https"
	if r.TLS == nil && (host == "localhost" || host == "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func shortenError(w http.ResponseWriter, plain bool, status int, msg string) {
	if plain {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprintln(w, "error:", msg)
		return
	}
	respondJSON(w, status, map[string]string{"error": msg})
}

//go:embed index.html
var indexHTML string

// shortURLs returns one short link per currently connected relay, or a
// single localhost link in local-only mode.
func (s *store) shortURLs(r *http.Request, code string) []string {
	bases := []string{publicBaseURL(r)}
	if activeRelayBaseURLs != nil {
		if extra := activeRelayBaseURLs(); len(extra) > 0 {
			bases = extra
		}
	}
	out := make([]string, len(bases))
	for i, base := range bases {
		out[i] = fmt.Sprintf("%s/%s", base, code)
	}
	return out
}

// revealPage guards one-time links and secrets: chat previews and other
// GET bots stop here, and only a human following the link burns it.
const revealPage = `<!doctype html><meta charset="utf-8"><title>potly</title><p>This link works exactly once.</p><p><a href="/%s/reveal">Open it</a></p>`

func (s *store) redirect(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(strings.ReplaceAll(indexHTML, "__BASE__", publicBaseURL(r))))
		return
	}
	name, action, _ := strings.Cut(path, "/")
	l, ok := s.get(name)
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if l.once || l.secret {
		if action != "reveal" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, revealPage, name)
			return
		}
		s.burn(name)
		if l.secret {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, l.target)
			return
		}
	}
	http.Redirect(w, r, l.target, http.StatusFound)
}

// newSecret stores a POSTed text payload and hands back a URL that
// shows it exactly once: curl -d "$TOKEN" HOST/s
func (s *store) newSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		shortenError(w, true, http.StatusMethodNotAllowed, "POST only")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		shortenError(w, true, http.StatusBadRequest, err.Error())
		return
	}
	if len(payload) == 0 {
		shortenError(w, true, http.StatusBadRequest, "empty payload")
		return
	}
	code, err := s.save(link{target: string(payload), once: true, secret: true})
	if err != nil {
		shortenError(w, true, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, u := range s.shortURLs(r, code) {
		fmt.Fprintln(w, u)
	}
}

// maxQRURLLen bounds the qr endpoint's input; QR capacity itself is 2953
// bytes at the lowest error correction, and a longer payload never scans.
const maxQRURLLen = 2048

func qrHandler(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" || len(target) > maxQRURLLen {
		shortenError(w, true, http.StatusBadRequest, "url query parameter is required (max 2048 chars)")
		return
	}
	q, err := qrcode.New(target, qrcode.Medium)
	if err != nil {
		shortenError(w, true, http.StatusBadRequest, err.Error())
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		writeQRHTML(w, q)
		return
	}
	writeQR(w, q)
}

// writeQRHTML serves the code as a plain <img> page: pixel-exact
// rendering for a browser, since terminal line spacing puts gaps
// between module rows that scanners may not read.
func writeQRHTML(w http.ResponseWriter, q *qrcode.QRCode) {
	png, err := q.PNG(320)
	if err != nil {
		shortenError(w, true, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>potly</title><body style="margin:0;display:grid;place-items:center;height:100vh"><img alt="QR code" src="data:image/png;base64,%s"></body>`, base64.StdEncoding.EncodeToString(png))
}

// writeQR follows qrencode's ANSIUTF8 convention: light modules become
// white glyphs, dark modules stay black (the escape's own background).
// Only foreground colors carry the pattern, so the code survives
// renderers that ignore background colors, and reads as black-on-white
// on any terminal theme.
func writeQR(w http.ResponseWriter, q *qrcode.QRCode) {
	bm := q.Bitmap()
	for y := 0; y < len(bm); y += 2 {
		var row strings.Builder
		row.WriteString("\033[40;37;1m")
		for x := range bm[y] {
			top, bottom := bm[y][x], y+1 < len(bm) && bm[y+1][x]
			switch {
			case top && bottom:
				row.WriteRune(' ')
			case top:
				row.WriteRune('▄')
			case bottom:
				row.WriteRune('▀')
			default:
				row.WriteRune('█')
			}
		}
		row.WriteString("\033[0m\n")
		fmt.Fprint(w, row.String())
	}
}

func newMux(s *store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", s.shorten)
	mux.HandleFunc("/s", s.newSecret)
	mux.HandleFunc("/qr", qrHandler)
	mux.HandleFunc("/thumbnail.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(thumbnailJPEG)
	})
	mux.HandleFunc("/", s.redirect)
	return mux
}

func main() {
	portalMode := flag.Bool("portal", false, "expose via an embedded Portal tunnel instead of only listening on :8000")
	name := flag.String("name", "potly", "Portal app name used in public URLs (portal mode)")
	hide := flag.Bool("hide", false, "hide the app from Portal's public listing; the URL still works (portal mode)")
	relays := flag.String("relays", "", "comma-separated Portal relay URLs to expose through, e.g. https://gosunuts.xyz (portal mode)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	mux := newMux(newStore())
	if !*portalMode {
		http.ListenAndServe(":8000", mux)
		return
	}
	var relayURLs []string
	if *relays != "" {
		for _, r := range strings.Split(*relays, ",") {
			if r = strings.TrimSpace(r); r != "" {
				relayURLs = append(relayURLs, r)
			}
		}
	}
	if err := runPortal(mux, *name, *hide, relayURLs); err != nil {
		log.Fatal(err)
	}
}
