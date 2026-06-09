# unreagent — Code-Signing-Zertifikat importieren
#
# Importiert das oeffentliche Zertifikat von "conflict.industries digital GmbH"
# in die Windows-Vertrauensspeicher. Danach laeuft die signierte unreagent.exe
# OHNE "Unbekannter Herausgeber"-Warnung und zeigt die GmbH als Herausgeber.
#
# ALS ADMINISTRATOR ausfuehren (Rechtsklick -> "Mit PowerShell ausfuehren" als
# Admin, oder:  powershell -ExecutionPolicy Bypass -File import-cert.ps1 )
#
# Hinweis: Damit vertraust du Code, der mit diesem Zertifikat signiert ist.
# Nur importieren, wenn du der Quelle (conflict.industries digital GmbH) vertraust.

$ErrorActionPreference = "Stop"
$cer = Join-Path $PSScriptRoot "unreagent-codesign.cer"

if (-not (Test-Path $cer)) {
    Write-Error "Zertifikat nicht gefunden: $cer"
    exit 1
}

Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null

Write-Host "OK - 'conflict.industries digital GmbH' ist jetzt vertrauenswuerdig."
Write-Host "Die signierte unreagent.exe startet nun ohne Herausgeber-Warnung."
