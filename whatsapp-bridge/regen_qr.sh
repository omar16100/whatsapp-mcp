#!/bin/bash
# Regenerate the WhatsApp QR PNG from the latest rotating code in bridge.log
# until the bridge reports a successful login (or ~30 min timeout).
# Only re-renders the PNG when the code actually changes (low overhead).
# Env overrides for a second account, e.g. BRIDGE_LOG=bridge-2.log QR_OUT=wa_qr-2.png STATUS_OUT=wa_status-2.txt
# Relative paths resolve against BRIDGE_DIR (default: the directory holding this script).
BRIDGE_DIR="${BRIDGE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
cd "$BRIDGE_DIR" || exit 1
BRIDGE_LOG="${BRIDGE_LOG:-bridge.log}"
QR_OUT="${QR_OUT:-wa_qr.png}"
STATUS_OUT="${STATUS_OUT:-wa_status.txt}"

# QR images are pairing secrets: keep new files private to the user.
umask 077

# Absolute, symlink-resolved directory + file name (the file itself may not exist yet).
abs_path() {
  local dir
  dir=$(cd "$(dirname "$1")" 2>/dev/null && pwd -P) || return 1
  echo "$dir/$(basename "$1")"
}

log_abs=$(abs_path "$BRIDGE_LOG") || { echo "regen_qr.sh: bad BRIDGE_LOG path" >&2; exit 1; }
qr_abs=$(abs_path "$QR_OUT") || { echo "regen_qr.sh: bad QR_OUT path" >&2; exit 1; }
status_abs=$(abs_path "$STATUS_OUT") || { echo "regen_qr.sh: bad STATUS_OUT path" >&2; exit 1; }
if [ "$qr_abs" = "$log_abs" ] || [ "$status_abs" = "$log_abs" ] || [ "$qr_abs" = "$status_abs" ] ||
   { [ -e "$BRIDGE_LOG" ] && { [ "$QR_OUT" -ef "$BRIDGE_LOG" ] || [ "$STATUS_OUT" -ef "$BRIDGE_LOG" ]; }; }; then
  echo "regen_qr.sh: BRIDGE_LOG, QR_OUT and STATUS_OUT must be different files" >&2
  exit 1
fi
# mv into a directory (or a symlink to one) would drop the file inside it instead.
if [ -d "$QR_OUT" ] || [ -d "$STATUS_OUT" ]; then
  echo "regen_qr.sh: QR_OUT and STATUS_OUT must be file paths, not directories" >&2
  exit 1
fi

# Private scratch dirs next to each output (same filesystem, so mv is an atomic rename
# that replaces the destination entry instead of writing through a symlink).
qr_tmp_dir=$(mktemp -d "$(dirname "$qr_abs")/.regen_qr.XXXXXX") || exit 1
status_tmp_dir=$(mktemp -d "$(dirname "$status_abs")/.regen_qr.XXXXXX") || { rm -rf "$qr_tmp_dir"; exit 1; }
trap 'rm -rf "$qr_tmp_dir" "$status_tmp_dir"' EXIT

write_status() {
  if ! { echo "$1" > "$status_tmp_dir/status" && mv -f "$status_tmp_dir/status" "$STATUS_OUT"; }; then
    echo "regen_qr.sh: cannot write $STATUS_OUT" >&2
    exit 1
  fi
}

# Line number of the last log line containing $1 (0 if none).
last_line_of() {
  local n
  n=$(grep -anF "$1" "$BRIDGE_LOG" 2>/dev/null | tail -1 | cut -d: -f1)
  echo "${n:-0}"
}

max() { if [ "$1" -gt "$2" ]; then echo "$1"; else echo "$2"; fi; }

# Remove the rendered QR so a used, superseded or old code is never left on disk.
clear_qr() { rm -f "$QR_OUT"; last=""; }

clear_qr
write_status PENDING
render_failed=0
for i in $(seq 1 900); do
  # Pairing state of the current bridge session only (since its last startup line):
  # linked if a success/connected line is newer than the latest QR code and logout.
  start_line=$(last_line_of "Starting WhatsApp client")
  qr_line=$(last_line_of "WHATSAPP_QR_CODE>>>")
  ok_line=$(max "$(last_line_of "Successfully connected and authenticated")" "$(last_line_of "Connected to WhatsApp")")
  logout_line=$(last_line_of "Device logged out")
  bad_line=$(max "$qr_line" "$logout_line")
  if [ "$ok_line" -gt "$start_line" ] && [ "$ok_line" -gt "$bad_line" ]; then
    clear_qr
    write_status CONNECTED
    exit 0
  fi
  # Only a QR code printed after the latest startup, login and logout is still usable.
  RAW=""
  if [ "$qr_line" -gt "$(max "$start_line" "$(max "$ok_line" "$logout_line")")" ]; then
    RAW=$(grep -a "WHATSAPP_QR_CODE>>>" "$BRIDGE_LOG" 2>/dev/null | tail -1 | sed 's/.*WHATSAPP_QR_CODE>>>//')
  fi
  if [ -z "$RAW" ] && [ -n "$last" ]; then
    clear_qr
  fi
  if [ -n "$RAW" ] && [ "$RAW" != "$last" ]; then
    # segno picks the output format from the file extension, so the temp file ends in .png
    if uvx --from segno segno --scale 10 --border 3 --output "$qr_tmp_dir/qr.png" "$RAW" &&
       mv -f "$qr_tmp_dir/qr.png" "$QR_OUT"; then
      last="$RAW"
      render_failed=0
    else
      rm -f "$qr_tmp_dir/qr.png"
      if [ "$render_failed" -eq 0 ]; then
        echo "regen_qr.sh: failed to render or save the QR code (is uvx installed?)" >&2
      fi
      render_failed=1
    fi
  fi
  sleep 2
done
clear_qr
write_status TIMEOUT
