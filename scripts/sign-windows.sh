#!/usr/bin/env bash
# Signs a Windows .exe with the unreagent code-signing certificate.
# Requires: osslsigncode + the PRIVATE key signing/codesign.key
# (not in the repo — keep it locally / restore it from the vault).
#
#   ./scripts/sign-windows.sh [path/to/exe]   (default: dist/unreagent.exe)
set -euo pipefail
cd "$(dirname "$0")/.."

EXE="${1:-dist/unreagent.exe}"
KEY="signing/codesign.key"
CERT="signing/codesign.pem"

if [ ! -f "$KEY" ]; then
  echo "ERROR: private key missing: $KEY" >&2
  echo "  -> put the securely stored key back here (do NOT commit it)." >&2
  exit 1
fi
command -v osslsigncode >/dev/null || { echo "ERROR: osslsigncode not installed" >&2; exit 1; }

osslsigncode sign \
  -certs "$CERT" -key "$KEY" \
  -n "unreagent" -i "https://github.com/Getty/unreagent" \
  -h sha256 -t http://timestamp.digicert.com \
  -in "$EXE" -out "$EXE.signed"
mv "$EXE.signed" "$EXE"
echo "signed: $EXE"
# Informational only — verify always reports "failed" for self-signed certs
# (no CA chain); this must not abort the build.
osslsigncode verify "$EXE" 2>&1 | grep -E "Subject:|Message digest" | head -2 || true
