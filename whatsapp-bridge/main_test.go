package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
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

func TestResolveSendJIDWith(t *testing.T) {
	ctx := context.Background()
	pn := types.NewJID("6598765432", types.DefaultUserServer)
	lid := types.NewJID("98765432101234", types.HiddenUserServer)
	group := types.NewJID("120363000000000000", types.GroupServer)
	calls := 0
	lookup := func(cached, fetched types.JID, fetchErr error) lidLookup {
		calls = 0
		return lidLookup{
			cached: func(context.Context, types.JID) (types.JID, error) { calls++; return cached, nil },
			fetch:  func(context.Context, types.JID) (types.JID, error) { calls++; return fetched, fetchErr },
		}
	}

	if got, err := resolveSendJIDWith(ctx, pn, false, lookup(lid, lid, nil)); err != nil || got != pn || calls != 0 {
		t.Fatalf("not migrated: got %s err=%v calls=%d, want PN unchanged and no lookups", got, err, calls)
	}
	if got, err := resolveSendJIDWith(ctx, group, true, lookup(lid, lid, nil)); err != nil || got != group || calls != 0 {
		t.Fatalf("group: got %s err=%v calls=%d, want unchanged", got, err, calls)
	}
	if got, err := resolveSendJIDWith(ctx, pn, true, lookup(lid, types.EmptyJID, nil)); err != nil || got != lid || calls != 1 {
		t.Fatalf("cache hit: got %s err=%v calls=%d", got, err, calls)
	}
	// Cache miss: fall back to the server query, like whatsmeow does.
	if got, err := resolveSendJIDWith(ctx, pn, true, lookup(types.EmptyJID, lid, nil)); err != nil || got != lid || calls != 2 {
		t.Fatalf("cache miss: got %s err=%v calls=%d, want LID via fetch", got, err, calls)
	}
	if _, err := resolveSendJIDWith(ctx, pn, true, lookup(types.EmptyJID, types.EmptyJID, nil)); err == nil {
		t.Fatalf("no LID anywhere must fail (whatsmeow would refuse the send too)")
	}
	if _, err := resolveSendJIDWith(ctx, pn, true, lookup(types.EmptyJID, lid, errors.New("offline"))); err == nil {
		t.Fatalf("fetch error must be returned")
	}
}

// fakeLogger records formatted log lines.
type fakeLogger struct{ lines *[]string }

func (f fakeLogger) add(msg string, args ...any) {
	*f.lines = append(*f.lines, fmt.Sprintf(msg, args...))
}
func (f fakeLogger) Warnf(msg string, args ...any)  { f.add(msg, args...) }
func (f fakeLogger) Errorf(msg string, args ...any) { f.add(msg, args...) }
func (f fakeLogger) Infof(msg string, args ...any)  { f.add(msg, args...) }
func (f fakeLogger) Debugf(msg string, args ...any) { f.add(msg, args...) }
func (f fakeLogger) Sub(string) waLog.Logger        { return f }

func TestRedactingLoggerStripsSignedURLs(t *testing.T) {
	var lines []string
	log := newRedactingLogger(fakeLogger{&lines})
	// Same shape as whatsmeow's download retry warning with a url.Error inside.
	transportErr := `Get "https://mmg.whatsapp.net/v/t62/f.enc?ccb=11-4&oh=SECRETOH&oe=6A77D5A9": EOF`
	log.Warnf("Failed to download media due to network error: %v, retrying in %s...", transportErr, "1s")
	log.Sub("Upload").Errorf("upload failed: %s", "Post \"https://upload.whatsapp.net/mms/document/x?auth=SECRETAUTH&token=y\": EOF")
	log.Infof("plain %d", 42)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "SECRETOH") || strings.Contains(joined, "SECRETAUTH") {
		t.Fatalf("signed URL tokens leaked into logs:\n%s", joined)
	}
	if len(lines) != 3 || lines[2] != "plain 42" {
		t.Fatalf("unexpected log lines: %q", lines)
	}
}

func TestGetEnvDefaults(t *testing.T) {
	t.Setenv("WA_TEST_STR", "")
	if got := getEnv("WA_TEST_STR", "store"); got != "store" {
		t.Fatalf("empty env: got %q want store", got)
	}
	t.Setenv("WA_TEST_STR", "store-2")
	if got := getEnv("WA_TEST_STR", "store"); got != "store-2" {
		t.Fatalf("set env: got %q want store-2", got)
	}
}

func TestParsePortEnv(t *testing.T) {
	valid := []struct {
		val  string
		want int
	}{
		{"", 8080},
		{"   ", 8080},
		{"8081", 8081},
		{" 8081 ", 8081},
		{"65535", 65535},
	}
	for _, c := range valid {
		t.Setenv("WA_TEST_PORT", c.val)
		got, err := parsePortEnv("WA_TEST_PORT", 8080)
		if err != nil || got != c.want {
			t.Errorf("parsePortEnv(%q) = %d, %v; want %d, nil", c.val, got, err, c.want)
		}
	}
	// Explicit but invalid values are errors, never a silent fallback to 8080.
	for _, bad := range []string{"abc", "0", "-1", "70000", "80 81", "8081x"} {
		t.Setenv("WA_TEST_PORT", bad)
		if got, err := parsePortEnv("WA_TEST_PORT", 8080); err == nil {
			t.Errorf("parsePortEnv(%q) = %d, want error", bad, got)
		}
	}
}

func TestValidateStoreDir(t *testing.T) {
	for _, ok := range []string{"store", "store-2", "/tmp/wa store/acct 1", "./a_b.c"} {
		if err := validateStoreDir(ok); err != nil {
			t.Errorf("validateStoreDir(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"store?mode=ro", "a#b", "store%20x"} {
		if err := validateStoreDir(bad); err == nil {
			t.Errorf("validateStoreDir(%q) should fail", bad)
		}
	}
}

func TestListenAPIBindsLoopbackAndFailsOnBusyPort(t *testing.T) {
	// Occupy an ephemeral loopback port (never the bridge's real port).
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot open a loopback listener: %v", err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	if _, err := listenAPI(port); err == nil {
		t.Fatalf("listenAPI on a busy port must fail")
	}

	busy.Close()
	ln, err := listenAPI(port)
	if err != nil {
		t.Skipf("port %d not reusable right away: %v", port, err)
	}
	defer ln.Close()
	if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Fatalf("API must bind loopback only, got %s", ip)
	}
}
