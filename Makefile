# claude-profile developer tasks.
#
# Tests keep their temporary files under ./tmp, never the RAM-backed /tmp.

GO ?= go
export TMPDIR := $(CURDIR)/tmp
export GOTMPDIR := $(CURDIR)/tmp

.PHONY: test test-live vet fmt-check build

test: | tmp
	$(GO) test -race ./...

# Starts real systemd user scopes (needs a running user manager).
test-live: | tmp
	$(GO) test -race -tags systemdlive -count=1 ./internal/launch/

vet:
	$(GO) vet ./...

fmt-check:
	test -z "$$(gofmt -l .)"

build: | tmp
	$(GO) build -o claude-profile ./cmd/claude-profile

tmp:
	mkdir -p tmp
