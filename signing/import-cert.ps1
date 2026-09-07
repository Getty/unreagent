# unreagent — import the code-signing certificate
#
# Imports the public certificate of "conflict.industries digital GmbH"
# into the Windows trust stores. Afterwards the signed unreagent.exe runs
# WITHOUT the "Unknown Publisher" warning and shows the GmbH as the publisher.
#
# RUN AS ADMINISTRATOR (right-click -> "Run with PowerShell" as
# Admin, or:  powershell -ExecutionPolicy Bypass -File import-cert.ps1 )
#
# Note: this makes you trust any code signed with this certificate.
# Only import it if you trust the source (conflict.industries digital GmbH).

$ErrorActionPreference = "Stop"
$cer = Join-Path $PSScriptRoot "unreagent-codesign.cer"

if (-not (Test-Path $cer)) {
    Write-Error "Certificate not found: $cer"
    exit 1
}

Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath $cer -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null

Write-Host "OK - 'conflict.industries digital GmbH' is now trusted."
Write-Host "The signed unreagent.exe now starts without a publisher warning."
