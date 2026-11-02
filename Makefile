SHELL := /bin/bash

VERSION    := 2.0.0
GO         ?= go
BIN        := mins
VERSIONS   := tools/versions.env
BUILD_HASH ?= $(shell (sha256sum $(VERSIONS) 2>/dev/null || shasum -a 256 $(VERSIONS)) | cut -c1-12)
LDFLAGS    := -s -w -X mins/internal/tools.BuildHash=$(BUILD_HASH)
TARGETS    := linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64
DIST       := tools/dist

.PHONY: all build test vet check fixtures integration parity release clean

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/mins

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: vet test

fixtures:
	bash tools/fixtures.sh

integration: build
	$(GO) test -tags=integration ./internal/integration

# Parity needs the v1.17.0 shell script, e.g.
#   make parity MINS_SH=~/shell-scripts/mins/mins.sh
parity: build
	@test -n "$(MINS_SH)" || { echo "set MINS_SH=/path/to/mins.sh" >&2; exit 2; }
	MINS_SH="$(MINS_SH)" $(GO) test -tags=integration -run 'TestParity' -v ./internal/integration

release:
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
