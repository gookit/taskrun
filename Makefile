# Development targets for github.com/gookit/taskrun.
#
# `make check` mirrors the Linux CI job; `make test-race` additionally needs a C toolchain.

GO ?= go
PKGS := ./...

.PHONY: help check build vet test fmt fmt-check test-go123 test-race cross examples cli

help:
	@echo "targets: check build vet test fmt fmt-check test-go123 test-race cross examples cli"

check: fmt-check build vet test

build:
	$(GO) build $(PKGS)

vet:
	$(GO) vet $(PKGS)

test:
	$(GO) test -count=1 $(PKGS)

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed for:"; echo "$$out"; exit 1; fi

# Go 1.23 is the declared minimum version.
test-go123:
	GOTOOLCHAIN=go1.23.12 $(GO) test -count=1 $(PKGS)

# The race detector needs cgo and a C compiler.
test-race:
	CGO_ENABLED=1 $(GO) test -race -count=1 $(PKGS)

cross:
	GOOS=linux $(GO) build $(PKGS)
	GOOS=darwin $(GO) build $(PKGS)

examples:
	$(GO) run ./examples/basic
	$(GO) run ./examples/host

cli:
	$(GO) run ./cmd/taskrun -config ./examples/basic.json -task hello
	$(GO) run ./cmd/taskrun -config ./examples/basic.json -task check -dry-run
