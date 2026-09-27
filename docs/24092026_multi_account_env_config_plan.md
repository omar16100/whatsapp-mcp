# Plan: multiple WhatsApp accounts via env config

Status: done, 24-09-2026.

## Goal
Run a second WhatsApp account (second phone number) from the same checkout and binary, with its
own bridge, store and MCP server entry.

## Decisions
- One repo and one binary with per-account env config, instead of a second checkout.
- Defaults unchanged (`store`, port `8080`, `http://127.0.0.1:8080/api`), so an existing
  single-account setup needs no config change.
- Each extra account gets its own store dir, port, log file and MCP server entry, and pairs with a
  fresh QR code.

## Steps
- [x] Bridge env config (`WHATSAPP_STORE_DIR`, `WHATSAPP_PORT`) + Go tests.
- [x] MCP server env config (`WHATSAPP_API_URL`, `WHATSAPP_DB_PATH`) + stderr logging + pytest.
- [x] `qr_server.py` / `regen_qr.sh` env overrides for a per-account pairing page.
- [x] Review fixes: stdout `print()` -> logger (MCP stdio safety), redacted media upload log,
  `regen_qr.sh` env overrides.

## Example (second account)
```bash
# bridge (build once: CGO_ENABLED=1 go build -o whatsapp-bridge-bin .)
cd whatsapp-bridge
WHATSAPP_STORE_DIR=store-2 WHATSAPP_PORT=8081 ./whatsapp-bridge-bin > bridge-2.log 2>&1
# pairing page for that account (reads the QR line from bridge-2.log)
BRIDGE_LOG=bridge-2.log QR_PORT=8766 uv run --with segno python qr_server.py
```
MCP server entry for the second account: same command as the first, plus env
`WHATSAPP_API_URL=http://127.0.0.1:8081/api` and
`WHATSAPP_DB_PATH=<repo>/whatsapp-bridge/store-2/messages.db`.

## Untested
- Send/edit/revoke/download on a second account.
