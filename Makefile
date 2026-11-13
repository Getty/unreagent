BINARY := unreagent
PKG     := ./cmd/launcher
LDFLAGS := -s -w
SYSO    := cmd/launcher/resource_windows_amd64.syso

.PHONY: all windows win-signed linux resource fmt vet clean

all: windows linux

# Cross-compile Linux -> Windows .exe (the actual target).
# The .syso lives in Git LFS; in a clone without git-lfs it is a text pointer,
# which go build rejects with an unrelated-looking linker/gcc error. Catch that
# first and say what to do.
windows:
	@if head -n 1 $(SYSO) 2>/dev/null | grep -q '^version https://git-lfs.github.com/spec/'; then \
		echo "error: $(SYSO) is a Git LFS pointer, not the compiled resource." >&2; \
		echo "       Fetch the LFS objects, then rebuild: git lfs install && git lfs pull" >&2; \
		exit 1; \
	fi
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
