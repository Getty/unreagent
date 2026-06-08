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

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf dist
