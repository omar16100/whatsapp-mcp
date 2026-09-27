"""Env-driven account config: defaults keep the original account, overrides select another bridge."""
import importlib

import whatsapp


def _reload(monkeypatch, **env):
    for k in ("WHATSAPP_API_URL", "WHATSAPP_DB_PATH"):
        monkeypatch.delenv(k, raising=False)
    for k, v in env.items():
        monkeypatch.setenv(k, v)
    return importlib.reload(whatsapp)


def test_defaults_point_at_original_bridge(monkeypatch):
    wa = _reload(monkeypatch)
    assert wa.WHATSAPP_API_BASE_URL == "http://127.0.0.1:8080/api"
    assert wa.MESSAGES_DB_PATH.endswith("whatsapp-bridge/store/messages.db")


def test_env_overrides_select_other_account(monkeypatch, tmp_path):
    db = str(tmp_path / "messages.db")
    wa = _reload(monkeypatch, WHATSAPP_API_URL="http://127.0.0.1:8081/api/", WHATSAPP_DB_PATH=db)
    assert wa.WHATSAPP_API_BASE_URL == "http://127.0.0.1:8081/api"
    assert wa.MESSAGES_DB_PATH == db


def test_empty_env_falls_back_to_defaults(monkeypatch):
    wa = _reload(monkeypatch, WHATSAPP_API_URL="", WHATSAPP_DB_PATH="")
    assert wa.WHATSAPP_API_BASE_URL == "http://127.0.0.1:8080/api"


def test_send_uses_configured_url(monkeypatch):
    from unittest.mock import patch, MagicMock
    wa = _reload(monkeypatch, WHATSAPP_API_URL="http://127.0.0.1:8081/api")
    resp = MagicMock(status_code=200)
    resp.json.return_value = {"success": True, "message": "ok"}
    with patch.object(wa.requests, "post", return_value=resp) as post:
        wa.send_message("6591234567", "hi")
    assert post.call_args.args[0] == "http://127.0.0.1:8081/api/send"


def test_db_errors_do_not_write_stdout(monkeypatch, tmp_path, capsys):
    # stdout is the MCP stdio channel; errors must go to the logger (stderr)
    wa = _reload(monkeypatch, WHATSAPP_DB_PATH=str(tmp_path))  # a dir: sqlite cannot open it
    assert wa.list_chats(limit=1) == []
    assert capsys.readouterr().out == ""


def teardown_module(_):
    import os
    os.environ.pop("WHATSAPP_API_URL", None)
    os.environ.pop("WHATSAPP_DB_PATH", None)
    importlib.reload(whatsapp)
