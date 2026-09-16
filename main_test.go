package main

import (
	"encoding/json"
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
