"""Fast unit tests for the draft-to-self helpers (bridge HTTP mocked)."""
from unittest.mock import patch, MagicMock

import whatsapp


def _resp(json_body, status=200):
    m = MagicMock()
    m.status_code = status
    m.json.return_value = json_body
    m.text = str(json_body)
    return m


def test_draft_to_self_posts_single_clean_message():
    ok_body = {"success": True, "message": "Message sent to self",
               "message_id": "MSG2", "chat_jid": "6591234567@s.whatsapp.net",
               "timestamp": "2026-07-10T12:00:00+08:00"}
    with patch("whatsapp.requests.post", return_value=_resp(ok_body)) as post:
        ok, info, ids = whatsapp.draft_to_self("+6598765432", "hello there")

    assert ok is True
    # Single clean post to the self-chat (no header message).
    assert post.call_count == 1
    payload = post.call_args.kwargs["json"]
    assert payload["recipient"] == "self"
    assert payload["message"] == "hello there"
    assert payload["draft"] is True  # bridge records it so only drafts can be edited/revoked
    assert "media_path" not in payload
    assert ids["message_id"] == "MSG2"
    assert ids["chat_jid"] == "6591234567@s.whatsapp.net"
    # Recipient context is surfaced to the caller, not injected into WhatsApp.
    assert "+6598765432" in info


def test_draft_to_self_with_media_sends_caption(tmp_path):
    f = tmp_path / "report.pdf"
    f.write_bytes(b"%PDF-1.4 test")
    ok_body = {"success": True, "message": "ok", "message_id": "M", "chat_jid": "j"}
    with patch("whatsapp.requests.post", return_value=_resp(ok_body)) as post:
        ok, info, ids = whatsapp.draft_to_self("6598765432", "see attached", str(f))

    assert ok is True
    payload = post.call_args.kwargs["json"]
    assert payload["media_path"] == str(f)
    assert payload["message"] == "see attached"  # caption preserved


def test_draft_to_self_rejects_missing_media(tmp_path):
    ok, info, ids = whatsapp.draft_to_self("659", "x", str(tmp_path / "nope.pdf"))
    assert ok is False
    assert ids is None
    assert "not found" in info.lower()


def test_draft_to_self_rejects_ogg_with_text(tmp_path):
    # .ogg is sent as a voice message, which has no caption: refuse instead of dropping text.
    f = tmp_path / "note.ogg"
    f.write_bytes(b"OggS")
    with patch("whatsapp.requests.post") as post:
        ok, info, ids = whatsapp.draft_to_self("6598765432", "some text", str(f))
    assert ok is False and ids is None
    assert "cannot carry text" in info
    post.assert_not_called()


def test_draft_to_self_ogg_without_text_is_allowed(tmp_path):
    f = tmp_path / "note.ogg"
    f.write_bytes(b"OggS")
    ok_body = {"success": True, "message": "ok", "message_id": "M", "chat_jid": "j"}
    with patch("whatsapp.requests.post", return_value=_resp(ok_body)) as post:
        ok, _, _ = whatsapp.draft_to_self("6598765432", "", str(f))
    assert ok is True
    assert post.call_args.kwargs["json"]["media_path"] == str(f)


def test_draft_to_self_surfaces_unrecorded_draft_warning():
    body = {"success": True, "message": "Message sent to self", "message_id": "M9",
            "chat_jid": "6591234567@s.whatsapp.net", "draft_recorded": False}
    with patch("whatsapp.requests.post", return_value=_resp(body)):
        ok, info, ids = whatsapp.draft_to_self("6598765432", "hi")
    assert ok is True  # delivered: the caller must not re-send
    assert "could not record" in info
    assert ids["message_id"] == "M9" and ids["draft_recorded"] is False


def test_regular_send_is_not_marked_draft():
    with patch("whatsapp.requests.post", return_value=_resp({"success": True, "message": "ok"})) as post:
        whatsapp.send_message("6598765432", "hi")
    assert "draft" not in post.call_args.kwargs["json"]


def test_edit_draft_hits_edit_endpoint():
    with patch("whatsapp.requests.post", return_value=_resp({"success": True, "message": "edited"})) as post:
        ok, info = whatsapp.edit_draft("6591234567@s.whatsapp.net", "MSG2", "revised")

    assert ok is True
    url = post.call_args.args[0]
    payload = post.call_args.kwargs["json"]
    assert url.endswith("/edit")
    assert payload == {"chat_jid": "6591234567@s.whatsapp.net", "message_id": "MSG2", "new_message": "revised"}


def test_delete_draft_hits_revoke_endpoint():
    with patch("whatsapp.requests.post", return_value=_resp({"success": True, "message": "revoked"})) as post:
        ok, info = whatsapp.delete_draft("6591234567@s.whatsapp.net", "MSG2")

    assert ok is True
    assert post.call_args.args[0].endswith("/revoke")
