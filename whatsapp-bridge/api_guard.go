package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
)

// maxAPIBodyBytes caps REST request bodies. Requests only carry short JSON
// (recipient, text, a local file path), never file contents.
const maxAPIBodyBytes = 1 << 20

// isAllowedAPIHost reports whether a request's Host header names the loopback
// interface. Checking the literal Host value (no DNS lookup) defeats DNS
// rebinding, where a web page's own hostname is re-pointed at 127.0.0.1.
func isAllowedAPIHost(hostHeader string) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// guardAPI wraps a REST handler with checks that stop a web page from driving the
// unauthenticated loopback API from the user's browser:
//   - the Host header must be a loopback name (DNS rebinding),
//   - requests carrying an Origin header are rejected (browsers always send one on
//     cross-origin POSTs; the Python MCP client never does),
//   - the body must be declared application/json, which a cross-origin page cannot
//     send without a CORS preflight this server never approves,
//   - the body is size-limited.
func guardAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAllowedAPIHost(r.Host) {
			fmt.Printf("Rejected API request to %s: non-loopback Host header\n", r.URL.Path)
			http.Error(w, "Forbidden host", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") != "" {
			fmt.Printf("Rejected API request to %s: browser Origin header present\n", r.URL.Path)
			http.Error(w, "Cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAPIBodyBytes)
		next(w, r)
	}
}

// decodeJSONBody decodes exactly one JSON value from the request body and rejects
// trailing data.
func decodeJSONBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON body")
	}
	return nil
}
