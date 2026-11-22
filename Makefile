SHELL := /bin/bash

VERSION    := 2.1.0
GO         ?= go
BIN        := mins
VERSIONS   := tools/versions.env
BUILD_HASH ?= $(shell (sha256sum $(VERSIONS) 2>/dev/null || shasum -a 256 $(VERSIONS)) | cut -c1-12)
LDFLAGS    := -s -w -X mins/internal/tools.BuildHash=$(BUILD_HASH)
TARGETS    := linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64
DIST       := tools/dist
TOOLCHAIN_TARGET ?= $(shell $(GO) env GOOS)_$(shell $(GO) env GOARCH)

.PHONY: all build test vet check fixtures integration parity release clean toolchain assets bundle licenses sources relink

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/mins

# Build the pinned native toolchain into tools/.build/prefix. This is slow
# (FFmpeg, libaom, libjxl, libvmaf, ViSQOL) and only needed for release
# binaries; development builds fall back to PATH tools.
toolchain:
	bash tools/fetch-sources.sh
	JOBS=$(or $(JOBS),$(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)) bash tools/build.sh

# Compress the built tools into internal/tools/assets/<target>/.
assets:
	bash tools/package-assets.sh $(TOOLCHAIN_TARGET)

# A self-contained binary with the host toolchain embedded.
bundle: assets build

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: vet test
	bash tools/check-licenses.sh

# Regenerate THIRD_PARTY_NOTICES.md from tools/licenses/inventory.tsv.
licenses:
	bash tools/gen-notices.sh

# Build the shared source archive from tools/.build/src. Release CI runs this
# on one runner; local use requires tools/fetch-sources.sh first.
sources:
	bash tools/collect-sources.sh sources $(VERSION)

# Build the target-specific LGPL relink archive from a completed toolchain.
relink:
	bash tools/collect-sources.sh relink $(TOOLCHAIN_TARGET) $(VERSION)

fixtures:
	bash tools/fixtures.sh

integration: build
	$(GO) test -tags=integration ./internal/integration

# Parity needs the v1.17.0 shell script, e.g.
#   make parity MINS_SH=~/shell-scripts/mins/mins.sh
parity: build
	@test -n "$(MINS_SH)" || { echo "set MINS_SH=/path/to/mins.sh" >&2; exit 2; }
	@test -f "$(MINS_SH)" || { echo "MINS_SH=$(MINS_SH) does not exist" >&2; exit 2; }
	MINS_SH="$(MINS_SH)" $(GO) test -tags=integration -run 'TestParity' -v ./internal/integration

release:
	@for t in $(TARGETS); do \
		d=$$(echo $$t | tr / -); \
		test -s internal/tools/assets/$$d/sha256sums.txt || { \
			echo "missing embedded assets for $$t (expected internal/tools/assets/$$d/sha256sums.txt)" >&2; \
			echo "build the toolchain on that platform and run tools/package-assets.sh $$d first" >&2; \
			exit 2; }; \
	done
	@rm -rf $(DIST)
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; \
		[ "$$os" = windows ] && ext=.exe; \
		out=$(DIST)/$$os-$$arch; \
		mkdir -p $$out; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath \
			-ldflags "$(LDFLAGS)" -o $$out/mins$$ext ./cmd/mins || exit 1; \
		cp LICENSE THIRD_PARTY_NOTICES.md $$out/; \
	done
	@bash tools/package-release.sh $(DIST) $(VERSION)

clean:
	rm -f $(BIN) $(BIN).exe
	rm -rf $(DIST) testdata/generated
