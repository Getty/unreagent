# Code signing (self-signed)

`unreagent.exe` is signed with a **self-signed** certificate from
"conflict.industries digital GmbH". This removes the
"Unknown Publisher" warning **on machines that have imported the certificate
once** — ideal for your own team / known users.

## For users: import the certificate (once)

PowerShell **as Administrator**:

```powershell
powershell -ExecutionPolicy Bypass -File signing\import-cert.ps1
```

This imports `unreagent-codesign.cer` into "Trusted Root Certification
Authorities" and "Trusted Publishers". Afterwards the signed
`unreagent.exe` starts without a publisher warning.

> Trust notice: this makes the machine trust everything signed with this
> certificate. Only import it if you trust conflict.industries digital
> GmbH. For a broad public audience, a CA-issued OV/EV certificate is the
> clean way to go (no import needed then).

## Files

| File | In repo? | Purpose |
|---|---|---|
| `unreagent-codesign.cer` | ✅ yes (public) | import by users (DER) |
| `codesign.pem` | ✅ yes (public) | certificate for signing |
| `codesign.key` | ❌ **NEVER** (secret) | private key — local/vault only |

## For maintainers: signing

```bash
make win-signed         # builds + signs dist/unreagent.exe
# or an existing exe:
./scripts/sign-windows.sh dist/unreagent.exe
```

The **private key** `signing/codesign.key` is git-ignored and must be
stored securely (password manager/vault). Whoever holds it can sign in
the GmbH's name — treat it accordingly.

## For maintainers: generating/renewing the certificate

```bash
./scripts/gen-codesign-cert.sh   # reuses the existing key, otherwise a new RSA 3072 key
```

Writes `codesign.pem` (for signing) + `unreagent-codesign.cer` (for import). The
subject contains **only the CN** (`conflict.industries digital GmbH`): Windows
shows the full subject DN as "Verified Publisher" — an additional
`O`/`C` with the same value would make the name appear twice
("…GmbH, …GmbH, DE"). CN-only = the name appears exactly once.

> After a renewal, users must **re-import** the certificate
> (`import-cert.ps1`) — the thumbprint changes.
