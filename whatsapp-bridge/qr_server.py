#!/usr/bin/env python3
"""Tiny dynamic server for WhatsApp linking.

Serves a page whose <img> polls /qr.png (browser-side timer). Each /qr.png
request reads the LATEST rotating QR code from bridge.log and renders it fresh
with segno. No server-side sleeping involved. /status reports link state.
"""
import http.server
import os
import socketserver
import io
import re
import segno

# Env overrides let one QR page per account run (e.g. BRIDGE_LOG=bridge-2.log QR_PORT=8766).
# A relative BRIDGE_LOG resolves against this script's directory (the bridge dir).
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
_bridge_log = os.getenv("BRIDGE_LOG") or "bridge.log"
BRIDGE_LOG = _bridge_log if os.path.isabs(_bridge_log) else os.path.join(SCRIPT_DIR, _bridge_log)
PORT = int(os.getenv("QR_PORT", "8765"))

PAGE = """<!doctype html>
<html><head><meta charset="utf-8"><title>WhatsApp Link QR</title>
<style>
body{background:#0b141a;color:#e9edef;font-family:-apple-system,sans-serif;text-align:center;margin-top:4vh}
img{width:380px;height:380px;image-rendering:pixelated;background:#fff;padding:12px;border-radius:12px}
.ok{color:#25d366;font-size:1.5rem;margin-top:1rem}
code{color:#8696a0}
</style></head>
<body>
<h2 id="title">Scan to link WhatsApp</h2>
<p>WhatsApp -> <b>Settings -> Linked Devices -> Link a Device</b></p>
<img id="q" src="/qr.png" alt="loading QR...">
<p><code>Auto-refreshing every 2s. Leave this open until linked.</code></p>
<div id="status"></div>
<script>
function tick(){
  document.getElementById('q').src='/qr.png?t='+Date.now();
  fetch('/status?t='+Date.now()).then(function(r){return r.text()}).then(function(t){
    if(t.trim()==='CONNECTED'){
      document.getElementById('title').textContent='✓ Linked';
      document.getElementById('q').style.display='none';
      document.getElementById('status').innerHTML='<div class="ok">Connected & authenticated. You can close this tab.</div>';
      clearInterval(window._iv);
    }
  }).catch(function(){});
}
window._iv=setInterval(tick,2000); tick();
</script></body></html>"""


def read_log():
    try:
        with open(BRIDGE_LOG, "rb") as f:
            return f.read().decode("utf-8", "ignore")
    except FileNotFoundError:
        return ""


def latest_code(data):
    codes = re.findall(r"WHATSAPP_QR_CODE>>>(.*)", data)
    return codes[-1].strip() if codes else None


START_MARKER = "Starting WhatsApp client"
SUCCESS_MARKERS = ("Successfully connected and authenticated", "Connected to WhatsApp")
QR_MARKER = "WHATSAPP_QR_CODE>>>"
LOGOUT_MARKER = "Device logged out"
RESET_MARKERS = (QR_MARKER, LOGOUT_MARKER)


def current_session(data):
    """Log text since the bridge's latest startup line (the whole log if none)."""
    start = data.rfind(START_MARKER)
    return data if start == -1 else data[start:]


def is_connected(data):
    """Linked only if, within the current bridge session, a success/connected line
    is newer than the latest QR code and logout. An old login earlier in the same
    log, or a previous session, does not count."""
    session = current_session(data)
    ok_at = max(session.rfind(m) for m in SUCCESS_MARKERS)
    return ok_at != -1 and ok_at > max(session.rfind(m) for m in RESET_MARKERS)


def current_code(data):
    """The QR code to show: the latest one of the current session, and only if no
    login or logout happened after it (a used or superseded code is not served)."""
    if is_connected(data):
        return None
    session = current_session(data)
    qr_at = session.rfind(QR_MARKER)
    if qr_at == -1:
        return None
    after_qr = session[qr_at:]
    if any(m in after_qr for m in SUCCESS_MARKERS) or LOGOUT_MARKER in after_qr:
        return None
    return latest_code(session)


def is_allowed_host(host_header):
    """Only serve requests addressed to loopback names. Checking the literal Host
    header blocks DNS rebinding (a web page re-pointing its own name at 127.0.0.1)."""
    host = (host_header or "").strip().lower()
    if host.startswith("["):
        host = host[1:].split("]", 1)[0]
    elif host.count(":") == 1:
        host = host.split(":", 1)[0]
    return host in ("127.0.0.1", "localhost", "::1")


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, ctype, body):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_GET(self):
        if not is_allowed_host(self.headers.get("Host")):
            self._send(403, "text/plain", b"forbidden host")
            return
        path = self.path.split("?")[0]
        data = read_log()
        if path in ("/", "/index.html"):
            self._send(200, "text/html; charset=utf-8", PAGE.encode())
        elif path == "/qr.png":
            code = current_code(data)
            if not code:
                self._send(204, "text/plain", b"")
                return
            buf = io.BytesIO()
            segno.make(code, error="l").save(buf, kind="png", scale=10, border=3)
            self._send(200, "image/png", buf.getvalue())
        elif path == "/status":
            self._send(200, "text/plain", b"CONNECTED" if is_connected(data) else b"PENDING")
        else:
            self._send(404, "text/plain", b"not found")


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    with Server(("127.0.0.1", PORT), Handler) as httpd:
        print(f"QR server on http://127.0.0.1:{PORT}/ (log={BRIDGE_LOG})")
        httpd.serve_forever()
