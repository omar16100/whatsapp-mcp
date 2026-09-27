# Plan: publish cleanup of fork branches

Status: in progress, 27-09-2026.

## Goal
Merge the media-download/draft work and the multi-account env config into this fork's `main`
with a clean history and no machine-specific details.

## Decisions
- Replacement branches instead of rewriting the already-published
  `fix/media-download-403-and-draft-tools`: `fix/media-403-drafts-v2` (rebased onto fork `main`,
  author corrected) and `feat/multi-account-env-v2` (stacked on it).
- Upstream (lharries/whatsapp-mcp) is dormant; no upstream PR from this work.
- Unit tests only (Go tests, pytest with the bridge HTTP mocked); nothing talks to a running bridge.

## Steps
- [x] Rebase fix branch onto fork `main` (tree unchanged: fork `main` already contained a subset of
  the whatsmeow bump).
- [x] Replay the multi-account commit on top.
- [x] `regen_qr.sh` / `qr_server.py`: paths relative to the script dir (`BRIDGE_DIR`, `BRIDGE_LOG`).
- [x] Remove machine-specific notes from `todo.md` and docs; generic `c4model.md`.
- [x] README: clone URL points at this fork, fork note, upstream author's newsletter line marked as
  upstream's.
- [x] Code review round 1, findings applied: browser guard for the REST API, draft-only
  edit/revoke, LID self-chat JID, redacted download errors, media cache collisions, chat dir
  containment, strict port parsing with startup bind, store dir URI characters, QR helper fixes.
  Not applied: making the `WHATSAPP_QR_CODE>>>` line opt-in (the terminal QR art already prints
  the same pairing data to the same stdout; scanning it links the bridge to the scanner's own
  account, not the user's).
- [x] Code review round 2, findings applied: `go run .` in the README (the bridge now has several
  Go files), history-sync text logs gated, whatsmeow logger wrapped with URL redaction, upload
  errors redacted, content-addressed media paths (no overwrite between attachments), full LID
  resolution (cache, then `GetUserInfo`), QR helpers scoped to the current bridge session, private
  temp dir and alias checks in `regen_qr.sh`, `draft_recorded` surfaced by the MCP tool.
  Not applied: root-confined file operations for symlinked parent directories (the store dir is
  treated as trusted; documented).
- [x] Code review round 3: no blocker or major. Applied: `regen_qr.sh` refuses directory outputs,
  temp files beside each output, non-zero exit if the status file cannot be written; QR helpers
  ignore a code once a login or logout followed it. Left as minor follow-ups (see `todo.md`): QR
  expiry by time, a legacy file named like a content hash blocking that hash dir (fails safely).
- [x] Code review round 4 (narrow): no blocker or major; applied its minor (delete the QR PNG once
  its code is no longer valid).
- [ ] Tests, secret and personal-data scan, PRs.
- [ ] PRs into fork `main`, squash-merge fix first, then the multi-account branch.
