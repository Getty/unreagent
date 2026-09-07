#!/usr/bin/env bash
# Generates the self-signed code-signing certificate for unreagent.
#
# - Private key: signing/codesign.key  (PRIVATE, gitignored, NEVER commit)
#   Reused if present; otherwise generated anew (RSA 3072).
# - Public certificate: signing/codesign.pem (PEM, for signing)
#   and signing/unreagent-codesign.cer (DER, for users to import on Windows).
#
# The subject DELIBERATELY contains only the CN (company name). Windows shows
# the full subject DN as "Verified Publisher" (UAC/SmartScreen) — if O and C
# were added here with the same value, the name would appear twice
# ("…GmbH, …GmbH, DE"). CN-only = the name appears exactly once.
#
#   ./scripts/gen-codesign-cert.sh
#
# Afterwards users must re-import the new certificate once
# (signing/import-cert.ps1 as Admin) — the thumbprint has changed.
set -euo pipefail
cd "$(dirname "$0")/.."

CN="conflict.industries digital GmbH"
DAYS=3650
KEY="signing/codesign.key"
PEM="signing/codesign.pem"
CER="signing/unreagent-codesign.cer"

command -v openssl >/dev/null || { echo "ERROR: openssl not installed" >&2; exit 1; }

if [ ! -f "$KEY" ]; then
  echo "Generating new private key: $KEY (RSA 3072)"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$KEY"
  chmod 600 "$KEY"
else
  echo "Using existing key: $KEY"
fi

openssl req -x509 -new -key "$KEY" -sha256 -days "$DAYS" \
  -subj "/CN=$CN" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -addext "subjectKeyIdentifier=hash" \
  -addext "authorityKeyIdentifier=keyid" \
  -out "$PEM"

# DER variant for the Windows import.
openssl x509 -in "$PEM" -outform DER -out "$CER"

echo "written: $PEM + $CER"
openssl x509 -in "$PEM" -noout -subject -issuer -enddate
