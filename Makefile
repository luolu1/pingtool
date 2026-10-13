# PingTool build targets.
#
# The GUI uses github.com/lxn/walk which only builds for Windows, so the real
# binaries are cross compiled Windows executables. `go test ./...` still runs
# on the host platform because the probing engine has no platform dependency.
#
# The Windows binaries are built with Go 1.20 on purpose: it is the last Go
# release that supports Windows 7 / 8 / Server 2008 R2 / 2012 / 2012 R2.
# Newer toolchains stamp subsystem version 10.0, which makes Windows refuse to
# launch the program on those systems with the "若要找到适用于你的电脑的版本，
# 请咨询软件发布者" dialog. Go 1.20 stamps subsystem version 6.01 instead.

PKG     := .
LDFLAGS := -H windowsgui -s -w

# Force the Go 1.20 toolchain even when a newer `go` is installed; the go
# command downloads 1.20.14 on demand.
GO := GOTOOLCHAIN=go1.20.14 go

# rsrc embeds the .ico and the application manifest into the executable.
RSRC := $(shell command -v rsrc 2>/dev/null)

.PHONY: all build build-arm64 build-386 icon res test vet tidy clean dist

all: build

## Cross compile the 64-bit x86 Windows GUI executable.
build: res-amd64
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o PingTool-windows-amd64.exe $(PKG)
	@echo "built PingTool-windows-amd64.exe"

## Cross compile the 64-bit ARM Windows GUI executable.
build-arm64: res-arm64
	GOOS=windows GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o PingTool-windows-arm64.exe $(PKG)
	@echo "built PingTool-windows-arm64.exe"

## 32 bit build (works on 32/64 bit Windows).
build-386: res-386
	GOOS=windows GOARCH=386 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o PingTool-windows-386.exe $(PKG)
	@echo "built PingTool-windows-386.exe"

## Regenerate app.ico from source (no external tools required).
icon:
	$(GO) run ./tools/genicon -o app.ico

## Embed icon + manifest for one architecture into rsrc_windows_<arch>.syso.
res-%:
	@if [ -z "$(RSRC)" ]; then \
		echo "!! rsrc not found, $* build will lack icon/manifest."; \
		echo "!! install it with: go install github.com/akavel/rsrc@latest"; \
	else \
		rsrc -manifest pingtool.manifest -ico app.ico -arch $* -o rsrc_windows_$*.syso; \
	fi

res: res-amd64

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -f PingTool*.exe rsrc_windows_*.syso
	rm -rf dist

## Produce a release folder with all Windows builds.
dist: build build-arm64 build-386
	@mkdir -p dist
	cp PingTool-windows-amd64.exe PingTool-windows-arm64.exe PingTool-windows-386.exe dist/
	cp README.md dist/ 2>/dev/null || true
	@echo "artifacts in dist/"
