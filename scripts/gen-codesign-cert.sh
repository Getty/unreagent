#!/usr/bin/env bash
# Erzeugt das self-signed Code-Signing-Zertifikat fuer unreagent.
#
# - Privater Schluessel: signing/codesign.key  (PRIVAT, gitignored, NIE committen)
#   Wird wiederverwendet, falls vorhanden; sonst neu erzeugt (RSA 3072).
# - Oeffentliches Zertifikat: signing/codesign.pem (PEM, zum Signieren)
#   und signing/unreagent-codesign.cer (DER, fuer den Windows-Import durch Nutzer).
#
# Der Subject enthaelt BEWUSST nur den CN (Firmenname). Windows zeigt als
# "Verifizierter Herausgeber" (UAC/SmartScreen) den kompletten Subject-DN —
# stuenden hier zusaetzlich O und C mit demselben Wert, erschiene der Name
# doppelt ("…GmbH, …GmbH, DE"). Nur-CN = der Name steht genau einmal.
#
#   ./scripts/gen-codesign-cert.sh
#
# Danach muessen Nutzer das neue Zertifikat einmalig neu importieren
# (signing/import-cert.ps1 als Admin) — Thumbprint hat sich geaendert.
set -euo pipefail
cd "$(dirname "$0")/.."

CN="conflict.industries digital GmbH"
DAYS=3650
KEY="signing/codesign.key"
PEM="signing/codesign.pem"
CER="signing/unreagent-codesign.cer"

command -v openssl >/dev/null || { echo "FEHLER: openssl nicht installiert" >&2; exit 1; }

if [ ! -f "$KEY" ]; then
  echo "Erzeuge neuen privaten Schluessel: $KEY (RSA 3072)"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$KEY"
  chmod 600 "$KEY"
else
  echo "Verwende vorhandenen Schluessel: $KEY"
fi

openssl req -x509 -new -key "$KEY" -sha256 -days "$DAYS" \
  -subj "/CN=$CN" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -addext "subjectKeyIdentifier=hash" \
  -addext "authorityKeyIdentifier=keyid" \
  -out "$PEM"

# DER-Variante fuer den Windows-Import.
openssl x509 -in "$PEM" -outform DER -out "$CER"

echo "geschrieben: $PEM + $CER"
openssl x509 -in "$PEM" -noout -subject -issuer -enddate
