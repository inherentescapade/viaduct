# Wails build wrapper for the viaduct desktop app.
#
# Newer Linux distros ship WebKit2GTK 4.1 instead of 4.0, which Wails only links
# against when you pass `-tags webkit2_41`. Rather than remember that flag every
# time, this Makefile adds it automatically *when 4.1 is the version present* —
# so it's on by default on modern Linux, and correctly absent on a 4.0 box,
# macOS, or Windows (where the tag would break the build).
#
# Usage:
#   make dev          # wails dev  (hot reload)
#   make build        # wails build (production binary -> build/bin/)
#   make build-debug  # wails build with the debug console
#   make doctor       # wails doctor (checks toolchain + webkit version)
#
# Override the detection if needed:  make build TAGS="-tags webkit2_41"

# Auto-detect WebKit2GTK 4.1; empty on systems without it (incl. non-Linux).
TAGS ?= $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo -tags webkit2_41)

.PHONY: dev build build-debug doctor

dev:
	wails dev $(TAGS)

build:
	wails build $(TAGS)

build-debug:
	wails build -debug $(TAGS)

doctor:
	wails doctor
