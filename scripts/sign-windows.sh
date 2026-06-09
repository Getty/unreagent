#!/usr/bin/env bash
# Signiert eine Windows-.exe mit dem unreagent-Code-Signing-Zertifikat.
# Benoetigt: osslsigncode + den PRIVATEN Schluessel signing/codesign.key
# (nicht im Repo — lokal vorhalten/aus dem Vault wiederherstellen).
#
#   ./scripts/sign-windows.sh [pfad/zur/exe]   (default: dist/unreagent.exe)
set -euo pipefail
cd "$(dirname "$0")/.."

EXE="${1:-dist/unreagent.exe}"
KEY="signing/codesign.key"
CERT="signing/codesign.pem"

if [ ! -f "$KEY" ]; then
  echo "FEHLER: privater Schluessel fehlt: $KEY" >&2
  echo "  -> sicher aufbewahrten Schluessel hierher zuruecklegen (NICHT committen)." >&2
  exit 1
fi
command -v osslsigncode >/dev/null || { echo "FEHLER: osslsigncode nicht installiert" >&2; exit 1; }

osslsigncode sign \
  -certs "$CERT" -key "$KEY" \
  -n "unreagent" -i "https://github.com/Getty/unreagent" \
  -h sha256 -t http://timestamp.digicert.com \
  -in "$EXE" -out "$EXE.signed"
mv "$EXE.signed" "$EXE"
echo "signiert: $EXE"
osslsigncode verify "$EXE" 2>&1 | grep -E "Subject:|Message digest" | head -2
