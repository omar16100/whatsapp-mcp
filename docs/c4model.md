# C4 Model: whatsapp-mcp

Architecture source of truth. Update on every container/component/data-flow change.

## Context
- User's MCP client (Claude Desktop, Claude Code, Cursor) -> MCP server (one entry per WhatsApp account).
- Each MCP server -> its own local WhatsApp bridge -> WhatsApp multi-device servers (the bridge is a
  linked device of one phone number).

## Containers
| Container | Tech | Runs as | Default port | Store |
|---|---|---|---|---|
| Bridge | Go, whatsmeow, SQLite (`whatsapp-bridge/main.go`) | long-running process, one per account (supervisor of your choice) | 127.0.0.1:8080 (`WHATSAPP_PORT`) | `whatsapp-bridge/store` (`WHATSAPP_STORE_DIR`) |
| MCP server | Python FastMCP over stdio, run with uv (`whatsapp-mcp-server/main.py`) | spawned by the MCP client | n/a | reads `<store>/messages.db` (`WHATSAPP_DB_PATH`) |
| QR page (on demand) | Python `http.server` + segno (`whatsapp-bridge/qr_server.py`) | manual, during pairing | 127.0.0.1:8765 (`QR_PORT`) | reads a bridge log (`BRIDGE_LOG`) |

A second account runs the same binary and code with different env values (example: store `store-2`,
port 8081, log `bridge-2.log`, QR port 8766).

## Components
- Bridge `main.go`: env config (`WHATSAPP_STORE_DIR`, `WHATSAPP_PORT`, `WHATSAPP_LOG_CONTENT`),
  session db `whatsapp.db`, message store `messages.db` (tables `chats`, `messages`, `drafts`),
  media files `<store>/<chat_jid>/<sha256>/<filename>`, REST `/api/send|download|edit|revoke`
  (loopback only, unauthenticated, port bound at startup before connecting).
- Bridge `api_guard.go`: wraps every REST handler; loopback `Host` only, no `Origin`,
  `application/json` only, 1 MiB body limit.
- Bridge `redact.go`: strips URL query strings (media/upload tokens) from errors and from all
  whatsmeow log lines.
- Bridge `drafts_media.go`: draft registry (edit/revoke only for recorded drafts in the own chat),
  self-chat JID selection (LID after migration), media path and cache helpers (content-addressed
  paths, SHA-256 verified reuse, no symlink at the file path).
- MCP `whatsapp.py`: env config (`WHATSAPP_API_URL`, `WHATSAPP_DB_PATH`); SQLite reads direct,
  writes via bridge REST; draft helpers `draft_to_self`, `edit_draft`, `delete_draft`.
- MCP `main.py`: tool definitions, including `draft_message`, `revise_draft`, `delete_draft`.
- `qr_server.py` / `regen_qr.sh`: render the latest `WHATSAPP_QR_CODE>>>` line from a bridge log
  (`qr_server.py` answers only loopback `Host` headers).

## Data flows
1. Incoming/history messages: WhatsApp -> bridge -> `<store>/messages.db`.
2. Read tools: MCP -> SQLite `WHATSAPP_DB_PATH` (no bridge call).
3. Send/edit/revoke/download: MCP -> `WHATSAPP_API_URL` -> bridge -> WhatsApp.
4. Drafts: MCP `draft_message` -> `/api/send` with recipient `self` and `draft: true` -> own
   self-chat, ID recorded in `drafts`. Nothing goes to the named recipient; the user forwards the
   draft. `revise_draft` / `delete_draft` -> `/api/edit` / `/api/revoke`, accepted only for recorded
   drafts.
5. Pairing: bridge log QR line -> `qr_server.py` page -> phone scan -> session saved in
   `<store>/whatsapp.db`.

## Isolation
Accounts share only the binary and Python code. Store dir, port, log file and MCP server entry are
per account.
