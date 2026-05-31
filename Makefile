SHELL := /bin/sh

GO ?= go
BIN_NAME ?= janus
PREFIX ?= /usr/local
BINDIR ?= $(shell $(GO) env GOBIN)

ifeq ($(BINDIR),)
BINDIR := $(shell $(GO) env GOPATH)/bin
endif

.PHONY: help test build build-all install install-prefix clean

help:
	@echo "Targets:"
	@echo "  test           Run Go tests"
	@echo "  build          Build ./bin/$(BIN_NAME)"
	@echo "  build-all      Build Linux, macOS, and Windows artifacts into ./dist"
	@echo "  install        Install $(BIN_NAME) with go install into $(BINDIR)"
	@echo "  install-prefix Install ./bin/$(BIN_NAME) into DESTDIR/PREFIX/bin"
	@echo "  clean          Remove generated binaries"

test:
	$(GO) test ./...

build:
	$(GO) build -o ./bin/$(BIN_NAME) ./cmd/janus

build-all:
	./scripts/build-all.sh

install:
	$(GO) install ./cmd/janus
	@echo "installed $(BIN_NAME) to $(BINDIR)/$(BIN_NAME)"

install-prefix: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 0755 "./bin/$(BIN_NAME)" "$(DESTDIR)$(PREFIX)/bin/$(BIN_NAME)"
	@echo "installed $(BIN_NAME) to $(DESTDIR)$(PREFIX)/bin/$(BIN_NAME)"

clean:
	rm -rf ./bin ./dist
