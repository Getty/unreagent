BINARY := unreagent
PKG     := ./cmd/launcher
LDFLAGS := -s -w

.PHONY: all windows linux fmt vet clean

all: windows linux

# Cross-Compile Linux -> Windows .exe (das eigentliche Ziel)
windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY).exe $(PKG)

# Native Linux-Binary (für Entwicklung/Tests)
linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY) $(PKG)

# Windows-Ressource (Icon + Versionsinfo + Manifest) neu erzeugen.
# Benötigt: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
# Quelle: cmd/launcher/versioninfo.json + cmd/launcher/unreagent.manifest + assets/icon.ico
resource:
	goversioninfo -64 -o cmd/launcher/resource_windows_amd64.syso \
		-manifest cmd/launcher/unreagent.manifest cmd/launcher/versioninfo.json

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf dist
