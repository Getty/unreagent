BINARY := unreagent
PKG     := ./cmd/launcher
LDFLAGS := -s -w

.PHONY: all windows win-signed linux resource fmt vet clean

all: windows linux

# Cross-compile Linux -> Windows .exe (the actual target)
windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY).exe $(PKG)

# Build and sign the Windows .exe (needs signing/codesign.key + osslsigncode)
win-signed: windows
	./scripts/sign-windows.sh dist/$(BINARY).exe

# Native Linux binary (for development/tests)
linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY) $(PKG)

# Regenerate the Windows resource (icon + version info + manifest).
# Needs: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
# Source: cmd/launcher/versioninfo.json + cmd/launcher/unreagent.manifest + assets/icon.ico
resource:
	goversioninfo -64 -o cmd/launcher/resource_windows_amd64.syso \
		-manifest cmd/launcher/unreagent.manifest cmd/launcher/versioninfo.json

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf dist
