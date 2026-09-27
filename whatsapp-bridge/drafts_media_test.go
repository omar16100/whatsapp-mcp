package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

var (
	testOwnPN  = types.NewJID("6591234567", types.DefaultUserServer)
	testOwnLID = types.NewJID("123456789012345", types.HiddenUserServer)
)

func openTestStore(t *testing.T) *MessageStore {
	t.Helper()
	store, err := openMessageStore(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("openMessageStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestDraftRegistry(t *testing.T) {
	store := openTestStore(t)
	chat := testOwnPN.String()
	if ok, err := store.IsDraft("M1", chat); err != nil || ok {
		t.Fatalf("empty registry: ok=%v err=%v", ok, err)
	}
	if err := store.StoreDraft("M1", chat); err != nil {
		t.Fatalf("StoreDraft: %v", err)
	}
	if ok, _ := store.IsDraft("M1", chat); !ok {
		t.Fatalf("recorded draft not found")
	}
	if ok, _ := store.IsDraft("M1", "6598765432@s.whatsapp.net"); ok {
		t.Fatalf("draft must be keyed by chat as well as id")
	}
	if err := store.DeleteDraft("M1", chat); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if ok, _ := store.IsDraft("M1", chat); ok {
		t.Fatalf("deleted draft still present")
	}
}

func TestCheckDraftTarget(t *testing.T) {
	store := openTestStore(t)
	other := "6598765432@s.whatsapp.net"
	_ = store.StoreDraft("D1", testOwnPN.String())
	_ = store.StoreDraft("D2", testOwnLID.String())
	_ = store.StoreDraft("X1", other) // cannot happen via the API; must still be refused

	cases := []struct {
		name, chat, id string
		allowed        bool
	}{
		{"recorded draft in own PN chat", testOwnPN.String(), "D1", true},
		{"recorded draft in own LID chat", testOwnLID.String(), "D2", true},
		{"unrecorded message in own chat", testOwnPN.String(), "NOTE1", false},
		{"message in another chat", other, "X1", false},
		{"group chat", "120363000000000000@g.us", "D1", false},
		{"unparseable jid", "not a jid@@", "D1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := checkDraftTarget(store, tc.chat, tc.id, testOwnPN, testOwnLID)
			if (reason == "") != tc.allowed {
				t.Fatalf("checkDraftTarget(%q, %q) = %q, allowed want %v", tc.chat, tc.id, reason, tc.allowed)
			}
		})
	}
}

func TestIsOwnChatJID(t *testing.T) {
	device := types.JID{User: testOwnPN.User, Server: types.DefaultUserServer, Device: 12}
	cases := []struct {
		chat types.JID
		want bool
	}{
		{testOwnPN, true},
		{device, true},
		{testOwnLID, true},
		{types.NewJID("6598765432", types.DefaultUserServer), false},
		{types.NewJID(testOwnPN.User, types.GroupServer), false},
		{types.NewJID(testOwnLID.User, types.DefaultUserServer), false},
	}
	for _, tc := range cases {
		if got := isOwnChatJID(tc.chat, testOwnPN, testOwnLID); got != tc.want {
			t.Errorf("isOwnChatJID(%s) = %v, want %v", tc.chat, got, tc.want)
		}
	}
	if isOwnChatJID(testOwnPN, types.EmptyJID, types.EmptyJID) {
		t.Errorf("no own identity must never match")
	}
}

func TestPickSelfChatJID(t *testing.T) {
	adPN := types.JID{User: testOwnPN.User, Server: types.DefaultUserServer, Device: 7}
	adLID := types.JID{User: testOwnLID.User, Server: types.HiddenUserServer, Device: 7}

	if got, err := pickSelfChatJID(adPN, adLID, false); err != nil || got != testOwnPN {
		t.Fatalf("not migrated: got %s err=%v, want %s", got, err, testOwnPN)
	}
	if got, err := pickSelfChatJID(adPN, adLID, true); err != nil || got != testOwnLID {
		t.Fatalf("migrated: got %s err=%v, want %s", got, err, testOwnLID)
	}
	if got, err := pickSelfChatJID(adPN, types.EmptyJID, true); err != nil || got != testOwnPN {
		t.Fatalf("migrated without LID: got %s err=%v, want %s", got, err, testOwnPN)
	}
	if _, err := pickSelfChatJID(types.EmptyJID, adLID, true); err == nil {
		t.Fatalf("logged out must be an error")
	}
}

func TestMediaChatDir(t *testing.T) {
	root := filepath.Join("store")
	dir, err := mediaChatDir(root, "6591234567:12@s.whatsapp.net")
	if err != nil || dir != filepath.Join(root, "6591234567_12@s.whatsapp.net") {
		t.Fatalf("normal jid: dir=%q err=%v", dir, err)
	}
	for _, bad := range []string{"", ".", "..", "../x", "a/b", `a\b`, "../../etc"} {
		if _, err := mediaChatDir(root, bad); err == nil {
			t.Errorf("mediaChatDir(%q) should be rejected", bad)
		}
	}
}

func TestSafeIDComponent(t *testing.T) {
	cases := map[string]string{
		"3EB0A1B2C3D4":   "3EB0A1B2C3D4",
		"../../x":        "x",
		"a/b\\c:d":       "abcd",
		"":               "msg",
		"...":            "msg",
		"id_with-dash_1": "id_with-dash_1",
	}
	for in, want := range cases {
		if got := safeIDComponent(in); got != want {
			t.Errorf("safeIDComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// Two messages whose media share a filename must not get each other's file, and a
// new download must never replace another attachment's file.
func TestMediaCacheCollision(t *testing.T) {
	dir := t.TempDir()
	contentA, contentB := []byte("document A"), []byte("document B")

	// Message A was downloaded by an earlier version under the plain name.
	candA := mediaReadCandidates(dir, "report.pdf", sum(contentA))
	legacy := filepath.Join(dir, "report.pdf")
	if candA[1] != legacy {
		t.Fatalf("second candidate should be the legacy plain path, got %q", candA[1])
	}
	if err := os.WriteFile(legacy, contentA, 0644); err != nil {
		t.Fatal(err)
	}
	if p, ok := findCachedMedia(candA, sum(contentA)); !ok || p != legacy {
		t.Fatalf("message A should hit its legacy file, got %q %v", p, ok)
	}

	// Message B has the same filename but different content: no cache hit, and its
	// download goes to a content-addressed path, leaving A's file alone.
	candB := mediaReadCandidates(dir, "report.pdf", sum(contentB))
	if p, ok := findCachedMedia(candB, sum(contentB)); ok {
		t.Fatalf("message B must not reuse A's file, got %q", p)
	}
	target := candB[0]
	if filepath.Base(target) != "report.pdf" || filepath.Dir(filepath.Dir(target)) != dir {
		t.Fatalf("unexpected write path %q", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, contentB, 0644); err != nil {
		t.Fatal(err)
	}
	if p, ok := findCachedMedia(candB, sum(contentB)); !ok || p != target {
		t.Fatalf("message B should now hit its own file, got %q %v", p, ok)
	}
	if got, _ := os.ReadFile(legacy); string(got) != string(contentA) {
		t.Fatalf("message A's file was modified")
	}

	// Different content never maps to the same write path; same content does.
	if candA[0] == candB[0] {
		t.Fatalf("different media must not share a write path")
	}
	if again := mediaReadCandidates(dir, "report.pdf", sum(contentB)); again[0] != candB[0] {
		t.Fatalf("same media must map to the same path")
	}

	// Unknown hash never matches (cannot prove the file belongs to the message).
	if _, ok := findCachedMedia(candA, nil); ok {
		t.Fatalf("empty hash must not match")
	}
}

func TestMediaCacheRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	content := []byte("outside file")
	if err := os.WriteFile(outside, content, 0644); err != nil {
		t.Fatal(err)
	}
	cand := mediaReadCandidates(dir, "a.pdf", sum(content))
	if err := os.Symlink(outside, cand[1]); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok := findCachedMedia(cand, sum(content)); ok {
		t.Fatalf("a symlink must not be served as cached media")
	}

	// Writing over a symlink replaces the link and leaves its target alone.
	if err := writeFileAtomic(cand[1], []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(cand[1]); !info.Mode().IsRegular() {
		t.Fatalf("target should now be a regular file")
	}
	if got, _ := os.ReadFile(outside); string(got) != string(content) {
		t.Fatalf("symlink target was written through")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".download-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
