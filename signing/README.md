# Code-Signing (self-signed)

Die `unreagent.exe` wird mit einem **selbst-signierten** Zertifikat von
„conflict.industries digital GmbH" signiert. Das entfernt die
„Unbekannter Herausgeber"-Warnung **auf Maschinen, die das Zertifikat einmalig
importiert haben** — ideal fürs eigene Team / bekannte Nutzer.

## Für Nutzer: Zertifikat importieren (einmalig)

PowerShell **als Administrator**:

```powershell
powershell -ExecutionPolicy Bypass -File signing\import-cert.ps1
```

Das importiert `unreagent-codesign.cer` in „Vertrauenswürdige
Stammzertifizierungsstellen" und „Vertrauenswürdige Herausgeber". Danach startet
die signierte `unreagent.exe` ohne Herausgeber-Warnung.

> Vertrauens-Hinweis: Damit vertraut die Maschine allem, was mit diesem
> Zertifikat signiert ist. Nur importieren, wenn du conflict.industries digital
> GmbH vertraust. Für eine breite Öffentlichkeit ist ein CA-ausgestelltes
> OV/EV-Zertifikat der saubere Weg (dann ist kein Import nötig).

## Dateien

| Datei | Im Repo? | Zweck |
|---|---|---|
| `unreagent-codesign.cer` | ✅ ja (öffentlich) | Import durch Nutzer (DER) |
| `codesign.pem` | ✅ ja (öffentlich) | Zertifikat zum Signieren |
| `codesign.key` | ❌ **NIE** (geheim) | privater Schlüssel — nur lokal/Vault |

## Für Maintainer: signieren

```bash
make win-signed         # baut + signiert dist/unreagent.exe
# oder eine vorhandene exe:
./scripts/sign-windows.sh dist/unreagent.exe
```

Der **private Schlüssel** `signing/codesign.key` ist git-ignoriert und muss
sicher aufbewahrt werden (Passwort-Manager/Vault). Wer ihn besitzt, kann im
Namen der GmbH signieren — entsprechend behandeln.

## Für Maintainer: Zertifikat erzeugen/erneuern

```bash
./scripts/gen-codesign-cert.sh   # nutzt vorhandenen Key, sonst neuer RSA-3072-Key
```

Schreibt `codesign.pem` (signieren) + `unreagent-codesign.cer` (Import). Der
Subject enthält **nur den CN** (`conflict.industries digital GmbH`): Windows
zeigt als „Verifizierter Herausgeber" den kompletten Subject-DN — ein zusätzliches
`O`/`C` mit demselben Wert ließe den Namen doppelt erscheinen
(„…GmbH, …GmbH, DE"). Nur-CN = der Name steht genau einmal.

> Nach einer Erneuerung müssen Nutzer das Zertifikat **neu importieren**
> (`import-cert.ps1`) — der Thumbprint ändert sich.
