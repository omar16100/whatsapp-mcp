"""Unit tests for qr_server.py helpers (no server is started).

Run: uv run --with segno --with pytest pytest whatsapp-bridge/test_qr_server.py
"""
import importlib
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import qr_server  # noqa: E402


START = "[Client INFO] Starting WhatsApp client (store=store, port=8080)...\n"


def test_is_connected_needs_success_after_latest_qr():
    old_login = START + "WHATSAPP_QR_CODE>>>A\nSuccessfully connected and authenticated!\n"
    assert qr_server.is_connected(old_login)
    # A new QR code after an old login means pairing is pending again.
    assert not qr_server.is_connected(old_login + "WHATSAPP_QR_CODE>>>B\n")
    assert not qr_server.is_connected("")
    assert qr_server.is_connected("Successfully connected and authenticated!\n")


def test_is_connected_is_scoped_to_current_session():
    old_login = START + "WHATSAPP_QR_CODE>>>A\nSuccessfully connected and authenticated!\n"
    # Bridge restarted, new QR not printed yet: the old login must not count.
    assert not qr_server.is_connected(old_login + START)
    # Restarted with an existing session: connected without any QR.
    assert qr_server.is_connected(old_login + START + "[Client INFO] Connected to WhatsApp\n")
    # Logged out after connecting.
    assert not qr_server.is_connected(START + "[Client INFO] Connected to WhatsApp\n"
                                      "[Client WARN] Device logged out, please scan QR code\n")
    # "Not connected" is not a success line.
    assert not qr_server.is_connected(START + "Not connected to WhatsApp\n")


def test_current_code_hides_stale_or_used_codes():
    old = START + "WHATSAPP_QR_CODE>>>OLD\n"
    assert qr_server.current_code(old) == "OLD"
    assert qr_server.current_code(old + START) is None  # previous session's code
    assert qr_server.current_code(old + "Successfully connected and authenticated!\n") is None
    # A code that was used to log in must not come back after a later logout.
    used = old + "Successfully connected and authenticated!\n[Client WARN] Device logged out\n"
    assert qr_server.current_code(used) is None
    assert qr_server.current_code(used + "WHATSAPP_QR_CODE>>>NEW\n") == "NEW"


def test_latest_code_returns_last_one():
    data = "x\nWHATSAPP_QR_CODE>>>first\nWHATSAPP_QR_CODE>>>second \n"
    assert qr_server.latest_code(data) == "second"
    assert qr_server.latest_code("no codes") is None


def test_is_allowed_host():
    for ok in ("127.0.0.1:8765", "localhost:8765", "LOCALHOST", "[::1]:8765", "127.0.0.1"):
        assert qr_server.is_allowed_host(ok), ok
    for bad in (None, "", "evil.example:8765", "127.0.0.1.nip.io:8765", "0.0.0.0:8765"):
        assert not qr_server.is_allowed_host(bad), bad


def test_bridge_log_resolves_relative_to_script_dir(monkeypatch):
    script_dir = os.path.dirname(os.path.abspath(qr_server.__file__))
    monkeypatch.setenv("BRIDGE_LOG", "bridge-2.log")
    mod = importlib.reload(qr_server)
    assert mod.BRIDGE_LOG == os.path.join(script_dir, "bridge-2.log")

    monkeypatch.setenv("BRIDGE_LOG", "/var/log/wa/bridge.log")
    assert importlib.reload(qr_server).BRIDGE_LOG == "/var/log/wa/bridge.log"

    monkeypatch.delenv("BRIDGE_LOG")
    assert importlib.reload(qr_server).BRIDGE_LOG == os.path.join(script_dir, "bridge.log")
