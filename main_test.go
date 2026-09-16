package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestShortenAndRedirect(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"https://example.com/long/path"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shorten status = %d", resp.StatusCode)
	}
	var out struct{ Code, ShortURL string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Code == "" {
		t.Fatal("no code returned")
	}

	resp, err = client.Get(srv.URL + "/" + out.Code)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect status = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://example.com/long/path" {
		t.Fatalf("Location = %q", loc)
	}

	resp, _ = client.Get(srv.URL + "/doesnotexist")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing code status = %d", resp.StatusCode)
	}

	resp, _ = client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"not-a-url"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid url status = %d", resp.StatusCode)
	}
}

func TestShortenGET(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/shorten?url=" + url.QueryEscape("https://example.com/dashboard/very/long/path"))
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	line, _ := io.ReadAll(resp.Body)
	got := strings.TrimSpace(string(line))
	if !strings.HasPrefix(got, srv.URL+"/") {
		t.Fatalf("body = %q", got)
	}

	resp, _ = http.Get(srv.URL + "/shorten?url=not-a-url")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid url status = %d", resp.StatusCode)
	}
}

func TestCustomSlug(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"https://example.com/me","slug":"portfolio"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("slug shorten status = %d", resp.StatusCode)
	}
	var out struct {
		Code     string `json:"code"`
		ShortURL string `json:"short_url"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Code != "portfolio" || !strings.HasSuffix(out.ShortURL, "/portfolio") {
		t.Fatalf("code/short_url = %q/%q", out.Code, out.ShortURL)
	}

	resp, err = client.Get(srv.URL + "/portfolio")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://example.com/me" {
		t.Fatalf("redirect = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, _ = client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"https://example.com/other","slug":"portfolio"}`))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate slug status = %d", resp.StatusCode)
	}

	for _, bad := range []string{"has space", "slash/slug", "shorten", strings.Repeat("x", 65)} {
		body := fmt.Sprintf(`{"url":"https://example.com/x","slug":%q}`, bad)
		resp, _ = client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(body))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("slug %q status = %d, want 400", bad, resp.StatusCode)
		}
	}

	resp, err = http.Get(srv.URL + "/shorten?url=https://example.com/cv&slug=cv")
	if err != nil {
		t.Fatal(err)
	}
	line, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(line), "/cv") {
		t.Fatalf("GET slug body = %q", line)
	}

	for _, reserved := range []string{"qr", "shorten"} {
		body := fmt.Sprintf(`{"url":"https://example.com/x","slug":%q}`, reserved)
		resp, _ = client.Post(srv.URL+"/shorten", "application/json", strings.NewReader(body))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("reserved slug %q status = %d, want 400", reserved, resp.StatusCode)
		}
	}
}

func TestQR(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/qr?url=" + url.QueryEscape("https://example.com/abc"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("qr status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	// unicode half-block renderer emits at least two of the three block glyphs
	blocks := strings.Count(string(body), "█") + strings.Count(string(body), "▀") + strings.Count(string(body), "▄")
	if blocks < 2 {
		t.Fatalf("qr body has %d block glyphs, want a rendered code: %q", blocks, body)
	}

	resp, _ = http.Get(srv.URL + "/qr")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing url status = %d, want 400", resp.StatusCode)
	}

	resp, _ = http.Get(srv.URL + "/qr?url=" + url.QueryEscape(strings.Repeat("x", maxQRURLLen+1)))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized url status = %d, want 400", resp.StatusCode)
	}
}

func TestOneTimeLink(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(srv.URL + "/shorten?url=https://example.com/once&once=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	code := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(body)), srv.URL+"/"))
	if code == "" || strings.Contains(code, "/") {
		t.Fatalf("unexpected shorten response %q", body)
	}

	// the first open is a confirm page so chat previews do not burn the link
	resp, err = client.Get(srv.URL + "/" + code)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm page status = %d", resp.StatusCode)
	}
	page, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(page), `href="/`+code+`/reveal"`) {
		t.Fatalf("confirm page missing reveal link: %q", page)
	}

	resp, err = client.Get(srv.URL + "/" + code + "/reveal")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://example.com/once" {
		t.Fatalf("reveal = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// burned after the single reveal
	resp, _ = client.Get(srv.URL + "/" + code)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-burn status = %d", resp.StatusCode)
	}
	resp, _ = client.Get(srv.URL + "/" + code + "/reveal")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-burn reveal status = %d", resp.StatusCode)
	}
}

func TestOneTimeSecret(t *testing.T) {
	srv := httptest.NewServer(newMux(newStore()))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Post(srv.URL+"/s", "text/plain", strings.NewReader("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("secret post status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	code := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(body)), srv.URL+"/"))
	if code == "" || strings.Contains(code, "/") {
		t.Fatalf("unexpected secret response %q", body)
	}

	resp, _ = client.Get(srv.URL + "/s")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /s status = %d", resp.StatusCode)
	}

	resp, _ = client.Get(srv.URL + "/" + code)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("secret confirm page status = %d", resp.StatusCode)
	}
	resp, err = client.Get(srv.URL + "/" + code + "/reveal")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("secret reveal status = %d", resp.StatusCode)
	}
	payload, _ := io.ReadAll(resp.Body)
	if string(payload) != "hunter2" {
		t.Fatalf("revealed payload = %q", payload)
	}
	resp, _ = client.Get(srv.URL + "/" + code + "/reveal")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second reveal status = %d", resp.StatusCode)
	}

	resp, _ = client.Post(srv.URL+"/s", "text/plain", strings.NewReader(""))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty payload status = %d", resp.StatusCode)
	}
}
