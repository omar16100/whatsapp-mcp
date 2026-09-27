package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsAllowedAPIHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080":        true,
		"localhost:8080":        true,
		"LOCALHOST:8081":        true,
		"[::1]:8080":            true,
		"127.0.0.1":             true,
		"localhost":             true,
		"":                      false,
		"evil.example:8080":     false,
		"127.0.0.1.nip.io:8080": false,
		"0.0.0.0:8080":          false,
		"192.168.1.10:8080":     false,
	}
	for host, want := range cases {
		if got := isAllowedAPIHost(host); got != want {
			t.Errorf("isAllowedAPIHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// serveGuarded runs one request through guardAPI and reports the status and
// whether the wrapped handler ran (it decodes the body like the real handlers).
func serveGuarded(t *testing.T, req *http.Request) (int, bool) {
	t.Helper()
	called := false
	h := guardAPI(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var body map[string]any
		if err := decodeJSONBody(r, &body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code, called
}

func newAPIRequest(body, contentType string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/send", strings.NewReader(body))
	req.Host = "127.0.0.1:8080"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func TestGuardAPIAllowsLocalJSONClient(t *testing.T) {
	// Same shape as requests.post(url, json=payload) from the Python MCP server.
	code, called := serveGuarded(t, newAPIRequest(`{"recipient":"self","message":"hi"}`, "application/json"))
	if code != http.StatusOK || !called {
		t.Fatalf("local JSON client: code=%d called=%v, want 200 and called", code, called)
	}
	code, _ = serveGuarded(t, newAPIRequest(`{"a":1}`, "application/json; charset=utf-8"))
	if code != http.StatusOK {
		t.Fatalf("charset parameter should be accepted, got %d", code)
	}
}

func TestGuardAPIRejectsBrowserStyleRequests(t *testing.T) {
	withOrigin := newAPIRequest(`{"recipient":"x"}`, "application/json")
	withOrigin.Header.Set("Origin", "https://evil.example")

	textPlain := newAPIRequest(`{"recipient":"x"}`, "text/plain")
	formPost := newAPIRequest(`recipient=x`, "application/x-www-form-urlencoded")
	noType := newAPIRequest(`{"recipient":"x"}`, "")

	rebound := newAPIRequest(`{"recipient":"x"}`, "application/json")
	rebound.Host = "evil.example:8080"

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"origin header", withOrigin, http.StatusForbidden},
		{"text/plain simple request", textPlain, http.StatusUnsupportedMediaType},
		{"form post", formPost, http.StatusUnsupportedMediaType},
		{"missing content type", noType, http.StatusUnsupportedMediaType},
		{"dns rebinding host", rebound, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, called := serveGuarded(t, tc.req)
			if code != tc.want || called {
				t.Fatalf("code=%d called=%v, want %d and handler not called", code, called, tc.want)
			}
		})
	}
}

func TestGuardAPILimitsBodyAndTrailingData(t *testing.T) {
	big := `{"message":"` + strings.Repeat("a", maxAPIBodyBytes) + `"}`
	if code, _ := serveGuarded(t, newAPIRequest(big, "application/json")); code != http.StatusBadRequest {
		t.Fatalf("oversized body: got %d, want 400", code)
	}
	if code, _ := serveGuarded(t, newAPIRequest(`{"a":1}{"b":2}`, "application/json")); code != http.StatusBadRequest {
		t.Fatalf("trailing JSON: got %d, want 400", code)
	}
	if code, _ := serveGuarded(t, newAPIRequest(`{"a":1}`+"\n", "application/json")); code != http.StatusOK {
		t.Fatalf("trailing newline only: got %d, want 200", code)
	}
}

func TestRedactURLs(t *testing.T) {
	in := `failed to download: Get "https://mmg.whatsapp.net/v/t62.7118-24/f.enc?ccb=11-4&oh=SECRETTOKEN&oe=6A77D5A9&hash=abc": EOF`
	got := redactURLs(in)
	if strings.Contains(got, "SECRETTOKEN") || strings.Contains(got, "oe=") {
		t.Fatalf("query not redacted: %s", got)
	}
	if !strings.Contains(got, "https://mmg.whatsapp.net/v/t62.7118-24/f.enc?[redacted]") {
		t.Fatalf("host/path should be kept for diagnosis: %s", got)
	}
	if plain := "download failed with status code 403"; redactURLs(plain) != plain {
		t.Fatalf("text without URLs must be unchanged")
	}
}
