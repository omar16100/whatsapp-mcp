# whatsapp-mcp fork: change log

Fork of lharries/whatsapp-mcp. Upstream is unchanged; everything below is fork-only.

## 05-11-2025: whatsmeow context-aware API
- Bumped `go.mau.fi/whatsmeow`; its API now takes `context.Context`.
- Added `context.Background()` at the 5 call sites: `client.Download`, `sqlstore.New`,
  `container.GetFirstDevice`, `client.GetGroupInfo`, `client.Store.Contacts.GetContact`.

## 10-07-2026: media download 403 fix, REST hardening, draft tools
- whatsmeow bumped to `v0.0.0-20260622185415-5f04eac6dbbb` (WhatsApp rejected the upstream pin
  with `Client outdated (405)`).
- `WHATSAPP_QR_CODE>>>` line printed with each QR code so the pairing QR can be rendered as a PNG
  (see `whatsapp-bridge/qr_server.py`).
- Media download 403: `extractDirectPathFromURL` stripped the URL query string, dropping the CDN
  auth tokens (`oh`/`oe`). whatsmeow builds `https://{host}{directPath}&hash=...` assuming the
  direct path already carries its `?query`, so a query-less path returned HTTP 403. The query is
  now kept verbatim. Go table test in `whatsapp-bridge/main_test.go`.
  Only media whose CDN token is still valid can be recovered; long-expired history media still 403
  (would need a whatsmeow media-retry flow, not implemented).
- Path traversal: untrusted document filenames go through `sanitizeMediaFilename`, and the resolved
  path must stay under the chat directory. Test `TestSanitizeMediaFilename`.
- REST API bound to `127.0.0.1` (was all interfaces). `/api/*` is unauthenticated.
- Logs redacted: no message bodies, media URLs, direct paths or media keys.
- Draft-to-self MCP tools (`whatsapp-mcp-server/main.py`): `draft_message`, `revise_draft`,
  `delete_draft`. A draft is one clean message in the user's own self-chat; the user forwards it.
  Bridge: recipient `self`/`me` resolves to the own JID, `/api/send` returns
  `message_id`/`chat_jid`/`timestamp`, new `POST /api/edit` (BuildEdit, text only, about 20 min
  window) and `POST /api/revoke` (BuildRevoke). whatsmeow does not echo own sends, so drafts are not
  stored in `messages.db`. Tests: `whatsapp-mcp-server/test_drafts.py` (pytest dev dependency).
- Not changed: the `WHATSAPP_QR_CODE>>>` stdout line carries the raw pairing string (the same data
  the terminal QR art already shows).

## 24-09-2026: multiple accounts via env config
- Bridge: `WHATSAPP_STORE_DIR` / `WHATSAPP_PORT` (defaults `store` / `8080`); startup log prints
  both. Go tests `TestGetEnvDefaults`, `TestGetEnvInt`.
- MCP server: `WHATSAPP_API_URL` / `WHATSAPP_DB_PATH` (defaults unchanged), active config logged to
  stderr. All `print()` calls moved to the logger because stdout is the MCP stdio channel.
  Tests `whatsapp-mcp-server/test_config.py`.
- Media upload log redacted.
- `qr_server.py` (`BRIDGE_LOG`, `QR_PORT`) and `regen_qr.sh` (`BRIDGE_LOG`, `QR_OUT`, `STATUS_OUT`)
  env overrides so each account gets its own pairing page.
- `.gitignore`: per-account stores, logs, QR artifacts, built binary.
- Docs: `docs/index.md`, `docs/c4model.md`, `docs/24092026_multi_account_env_config_plan.md`.
- Untested: send/edit/revoke/download on a second account.

## 27-09-2026: publish cleanup and review fixes
- Branches replayed onto the fork's `main` with a corrected commit author.
- REST API browser guard (`whatsapp-bridge/api_guard.go`): loopback `Host` only (DNS rebinding),
  no `Origin` header, `application/json` only, 1 MiB body limit, no trailing JSON. Loopback binding
  alone did not stop a web page from POSTing a `text/plain` JSON body to `/api/send`.
- Drafts (`whatsapp-bridge/drafts_media.go`): `/api/send` with `"draft": true` (self only) records
  the message in a `drafts` table (response `draft_recorded`); `/api/edit` and `/api/revoke` refuse
  anything that is not a recorded draft in the user's own chat. `.ogg` drafts with text are refused.
- LID: after the account's LID migration, sends resolve phone-number destinations to the LID chat
  the same way whatsmeow does (cache, then `GetUserInfo`), and the self-chat uses the own LID, so
  the returned `chat_jid` is the real chat used by later edits/revokes.
- Media downloads: chat JID must be a single path component; new files are content-addressed
  (`<store>/<chat_jid>/<sha256>/<filename>`), so same-name attachments never overwrite each other;
  cached files (including the older `<store>/<chat_jid>/<filename>` layout) are reused only when
  their SHA-256 matches the message; a symlink at the file path is never served or written through
  (symlinked parent directories are not checked; the store dir is trusted).
- Logging: URL query strings (CDN/upload tokens) redacted from download/upload errors and from
  whatsmeow's own log lines (redacting logger wrapper). Message text (live and history sync) is only
  logged with `WHATSAPP_LOG_CONTENT=1`.
- README: `go run .` instead of `go run main.go` (the bridge now has several Go files).
- Env config: an explicit invalid `WHATSAPP_PORT` or a busy port stops the bridge at startup
  (was: silent fallback to 8080 / API missing); the port is bound before connecting to WhatsApp.
  `WHATSAPP_STORE_DIR` with `?`, `#` or `%` is refused (SQLite URI syntax).
- `regen_qr.sh` and `qr_server.py` resolve paths relative to the script directory instead of a
  hardcoded absolute path (`BRIDGE_DIR` / `BRIDGE_LOG` overrides). `regen_qr.sh`: `umask 077`,
  temp file ends in `.png` (segno picks the format from the extension, `.tmp` failed), render
  errors reported, private scratch dir for temp files, refuses output paths that alias the log.
  Both helpers only look at the current bridge session (since the last startup line): linked when
  a success/connected line is newer than the latest QR code and logout; a QR code is only used if
  no login or logout came after it. `qr_server.py` checks the `Host` header. `regen_qr.sh` refuses
  directory outputs, exits non-zero if the status file cannot be written, and deletes the QR PNG
  once its code is used, superseded or timed out.
- Tests: `api_guard_test.go`, `drafts_media_test.go`, `main_test.go` (Go);
  `whatsapp-mcp-server/test_drafts.py`, `test_config.py`, `whatsapp-bridge/test_qr_server.py`
  (pytest, run with `uv run --with segno --with pytest pytest whatsapp-bridge/test_qr_server.py`).
- Machine-specific notes removed from this file and the docs. README points at this fork.
- Plan: `docs/27092026_publish_cleanup_plan.md`.

## Pending
- (optional) whatsmeow media-retry flow to recover long-expired history media.
- (optional) gate the `WHATSAPP_QR_CODE>>>` line behind a flag.
- (optional) CI workflow (Go tests + pytest); the fork has none.
- (minor) QR codes are not expired by time in the helpers (whatsmeow rotates them; the bridge log
  has no expiry line).
- (minor) a legacy media file named exactly like a 64-hex content hash blocks that hash directory;
  the download then fails safely (nothing is overwritten).
- (minor) symlinked parent directories inside the store are not checked; the store dir is trusted.
