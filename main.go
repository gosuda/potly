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
	"strings"
	"sync"
)

type store struct {
	mu    sync.Mutex
	links map[string]string
}

func newStore() *store {
	return &store{links: make(map[string]string)}
}

func randCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:6]
}

func (s *store) save(target string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range 5 {
		code := randCode()
		if _, exists := s.links[code]; !exists {
			s.links[code] = target
			return code, nil
		}
	}
	return "", fmt.Errorf("could not allocate a short code")
}

func (s *store) get(code string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.links[code]
	return target, ok
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
	var target string
	switch r.Method {
	case http.MethodGet:
		target = r.URL.Query().Get("url")
	case http.MethodPost:
		var body struct {
			URL string `json:"url"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			shortenError(w, plain, http.StatusBadRequest, err.Error())
			return
		}
		target = body.URL
	default:
		shortenError(w, plain, http.StatusMethodNotAllowed, "GET or POST only")
		return
	}

	u, err := url.ParseRequestURI(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		shortenError(w, plain, http.StatusBadRequest, "url must be absolute http(s)")
		return
	}
	code, err := s.save(target)
	if err != nil {
		shortenError(w, plain, http.StatusInternalServerError, err.Error())
		return
	}
	bases := []string{publicBaseURL(r)}
	if activeRelayBaseURLs != nil {
		if extra := activeRelayBaseURLs(); len(extra) > 0 {
			bases = extra
		}
	}
	shortURLs := make([]string, len(bases))
	for i, base := range bases {
		shortURLs[i] = fmt.Sprintf("%s/%s", base, code)
	}
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

func (s *store) redirect(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/")
	if code == "" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(strings.ReplaceAll(indexHTML, "__BASE__", publicBaseURL(r))))
		return
	}
	target, ok := s.get(code)
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func newMux(s *store) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", s.shorten)
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
	flag.Parse()

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
