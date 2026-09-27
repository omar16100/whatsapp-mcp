package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// ---- Draft registry ----
//
// /api/edit and /api/revoke only act on messages that /api/send posted as drafts
// (request field "draft": true, which always targets the user's own self-chat).
// That keeps the draft tools from editing or deleting real messages in other
// chats, or unrelated notes in the self-chat.

const createDraftsTableSQL = `
	CREATE TABLE IF NOT EXISTS drafts (
		id TEXT,
		chat_jid TEXT,
		created_at TIMESTAMP,
		PRIMARY KEY (id, chat_jid)
	);
`

// StoreDraft records a message posted by the draft feature.
func (store *MessageStore) StoreDraft(id, chatJID string) error {
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO drafts (id, chat_jid, created_at) VALUES (?, ?, ?)",
		id, chatJID, time.Now(),
	)
	return err
}

// IsDraft reports whether (id, chatJID) was posted by the draft feature.
func (store *MessageStore) IsDraft(id, chatJID string) (bool, error) {
	var one int
	err := store.db.QueryRow("SELECT 1 FROM drafts WHERE id = ? AND chat_jid = ?", id, chatJID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// DeleteDraft forgets a draft after it has been revoked.
func (store *MessageStore) DeleteDraft(id, chatJID string) error {
	_, err := store.db.Exec("DELETE FROM drafts WHERE id = ? AND chat_jid = ?", id, chatJID)
	return err
}

// isOwnChatJID reports whether chat is the linked account's own chat, in either
// phone-number (s.whatsapp.net) or LID (lid) form.
func isOwnChatJID(chat, ownPN, ownLID types.JID) bool {
	chat = chat.ToNonAD()
	if !ownPN.IsEmpty() && chat.Server == types.DefaultUserServer && chat.User == ownPN.User {
		return true
	}
	if !ownLID.IsEmpty() && chat.Server == types.HiddenUserServer && chat.User == ownLID.User {
		return true
	}
	return false
}

// pickSelfChatJID returns the JID a message to the user's own chat is actually
// delivered to. After the account's LID migration whatsmeow rewrites phone-number
// destinations to their LID before sending, so the LID form is the real chat and
// must be used in the message keys of later edits and revokes.
func pickSelfChatJID(ownPN, ownLID types.JID, lidMigrated bool) (types.JID, error) {
	if ownPN.IsEmpty() {
		return types.EmptyJID, errors.New("not logged in (no device ID)")
	}
	if lidMigrated && !ownLID.IsEmpty() {
		return ownLID.ToNonAD(), nil
	}
	return ownPN.ToNonAD(), nil
}

// ---- Media file paths ----

// mediaChatDir returns the per-chat media directory under root. The chat JID must
// map to a single path component so it cannot point outside root.
func mediaChatDir(root, chatJID string) (string, error) {
	name := strings.ReplaceAll(chatJID, ":", "_")
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid chat JID for a media path")
	}
	return filepath.Join(root, name), nil
}

// safeIDComponent keeps only [A-Za-z0-9_-] from an ID so it is safe in a filename.
func safeIDComponent(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "msg"
	}
	return b.String()
}

// mediaHashPath is where a download is saved: <chatDir>/<hex sha256 of the
// plaintext>/<filename>. The directory is derived from the media content, so two
// different attachments with the same name never share a path, and concurrent
// downloads of the same media write identical bytes to the same file.
func mediaHashPath(chatDir, filename string, fileSHA256 []byte) string {
	return filepath.Join(chatDir, hex.EncodeToString(fileSHA256), filename)
}

// mediaReadCandidates lists where a message's media may already exist: the
// content-addressed path, then the plain <chatDir>/<filename> layout written by
// earlier versions (reused only if its hash matches).
func mediaReadCandidates(chatDir, filename string, fileSHA256 []byte) []string {
	return []string{
		mediaHashPath(chatDir, filename, fileSHA256),
		filepath.Join(chatDir, filename),
	}
}

// fileMatchesSHA256 reports whether path is a regular file (not a symlink) whose
// content hashes to want.
func fileMatchesSHA256(path string, want []byte) bool {
	if len(want) == 0 {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return bytes.Equal(h.Sum(nil), want)
}

// findCachedMedia returns the first candidate that already holds this message's
// media, verified by its plaintext SHA-256.
func findCachedMedia(candidates []string, fileSHA256 []byte) (string, bool) {
	for _, p := range candidates {
		if fileMatchesSHA256(p, fileSHA256) {
			return p, true
		}
	}
	return "", false
}

// writeFileAtomic writes data to a temp file in the target's directory and renames
// it into place, so an existing symlink at the final path is replaced rather than
// followed. Symlinked parent directories are not checked: the store directory is
// assumed to be trusted.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// checkDraftTarget returns "" when (chatJID, messageID) is a recorded draft in the
// user's own chat, otherwise the reason to refuse the edit/revoke.
func checkDraftTarget(store *MessageStore, chatJID, messageID string, ownPN, ownLID types.JID) string {
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return "invalid chat_jid"
	}
	if !isOwnChatJID(chat, ownPN, ownLID) {
		return "only drafts in your own self-chat can be edited or deleted"
	}
	ok, err := store.IsDraft(messageID, chatJID)
	if err != nil {
		return "draft lookup failed"
	}
	if !ok {
		return "message is not a draft posted by draft_message"
	}
	return ""
}
