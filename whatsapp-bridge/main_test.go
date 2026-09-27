package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Untrusted document filenames (doc.GetFileName()) must not be able to escape the
// per-chat media directory via path traversal.
func TestSanitizeMediaFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain name kept", "Report 2026.pdf", "Report 2026.pdf"},
		{"parent traversal stripped to base", "../../../../etc/passwd", "passwd"},
		{"absolute path stripped to base", "/etc/shadow", "shadow"},
		{"nested path stripped to base", "a/b/c.pdf", "c.pdf"},
		{"dotdot alone falls back", "..", "document_MSGID"},
		{"empty falls back", "", "document_MSGID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeMediaFilename(tc.in, "document", "MSGID")
			if got != tc.want {
				t.Fatalf("sanitizeMediaFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// The result must be a bare basename (no separators, no traversal).
			if strings.ContainsRune(got, filepath.Separator) || got == ".." {
				t.Fatalf("unsafe sanitized name: %q", got)
			}
		})
	}
}

// The query string of a WhatsApp media URL carries the ?ccb/oh/oe CDN auth tokens,
// which are the only authorization whatsmeow's downloader sends. extractDirectPathFromURL
// must preserve the path AND query verbatim; stripping the query causes HTTP 403.
func TestExtractDirectPathFromURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "full url with auth query is preserved verbatim",
			in:   "https://mmg.whatsapp.net/v/t62.7119-24/570664584_n.enc?ccb=11-4&oh=01_Q5Aa5A&oe=6A77D5A9&_nc_sid=5e03e0&mms3=true",
			want: "/v/t62.7119-24/570664584_n.enc?ccb=11-4&oh=01_Q5Aa5A&oe=6A77D5A9&_nc_sid=5e03e0&mms3=true",
		},
		{
			name: "percent-encoded query values are kept byte-for-byte",
			in:   "https://mmg.whatsapp.net/v/t62.7118-24/file.enc?oh=a%2Bb%2Fc%3D&oe=6A77D5A9",
			want: "/v/t62.7118-24/file.enc?oh=a%2Bb%2Fc%3D&oe=6A77D5A9",
		},
		{
			name: "full url without query yields bare path",
			in:   "https://mmg.whatsapp.net/v/t62.7118-24/file.enc",
			want: "/v/t62.7118-24/file.enc",
		},
		{
			name: "already-relative or unparseable input returned unchanged",
			in:   "/v/t62.7118-24/file.enc?ccb=11-4",
			want: "/v/t62.7118-24/file.enc?ccb=11-4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDirectPathFromURL(tc.in)
			if got != tc.want {
				t.Fatalf("extractDirectPathFromURL(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
			if len(got) == 0 || got[0] != '/' {
				t.Fatalf("result must start with '/': got %q", got)
			}
		})
	}
}
